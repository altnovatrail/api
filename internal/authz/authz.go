// Package authz is api's rights: OpenFGA, a library in api's own process, on its
// store in the database openfga (R04, R31). The model is section 3.1's, which the
// binary embeds; migrate puts it in the store, and the server only checks that the
// store holds it. Code asks a right and writes the tuple owner through Rights.
package authz

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/openfga/openfga/pkg/logger"
	"github.com/openfga/openfga/pkg/server"
	"github.com/openfga/openfga/pkg/storage/postgres"
	"github.com/openfga/openfga/pkg/storage/sqlcommon"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/altnovatrail/api/internal/store"
)

// storeName is the name of api's store in OpenFGA.
const storeName = "api"

// The refusals of Open, each of which migrate puts right.
var (
	// ErrNoStore is a database openfga with no store of api in it.
	ErrNoStore = errors.New("authz: no store api: run api migrate")
	// ErrModelDiffers is a store whose latest model is not the one api embeds.
	ErrModelDiffers = errors.New("authz: the store's model is not api's: run api migrate")
	// errManyStores is more than one store of api, which migrate never makes.
	errManyStores = errors.New("authz: more than one store api")
)

// A Right is what a request asks on a learner's profile (architecture 3.1).
type Right string

// The rights of the model.
const (
	// CanLearn is for the learning requests.
	CanLearn Right = "can_learn"
	// CanManage is for an adult's actions on the profile.
	CanManage Right = "can_manage"
	// CanView is for reading the progress.
	CanView Right = "can_view"
)

// pageSize is how many tuples a Read or a Write takes at once: OpenFGA's limit.
const pageSize = 100

// Rights is the port of api's rights, on the store api with its model.
type Rights struct {
	srv   *server.Server
	pool  *pgxpool.Pool
	store string
	model string
}

// Open opens the rights on the database at url — the pooler's, for serve — and
// finds the store and, among its models, the one api embeds. It writes neither: a
// store that is missing, or holds no such model, is a migrate that did not run,
// and Open refuses it (R31). The model is found among all of the store's, not
// only as the latest, so that a revision still serving while a newer one rolls out
// — or one rolled back to — starts on its own model.
func Open(ctx context.Context, url string) (*Rights, error) {
	srv, pool, err := newServer(ctx, url)
	if err != nil {
		return nil, err
	}
	rights, err := open(ctx, srv)
	if err != nil {
		srv.Close()
		return nil, err
	}
	rights.pool = pool
	return rights, nil
}

func open(ctx context.Context, srv *server.Server) (*Rights, error) {
	storeID, err := findStore(ctx, srv)
	if err != nil {
		return nil, err
	}
	model, err := findModel(ctx, srv, storeID)
	if err != nil {
		return nil, err
	}
	if model == "" {
		return nil, ErrModelDiffers
	}
	return &Rights{srv: srv, store: storeID, model: model}, nil
}

// Close closes the server, and its datastore with it.
func (r *Rights) Close() { r.srv.Close() }

// newServer is OpenFGA on its Postgres datastore at url. The pool is api's own,
// opened and pinged under ctx, so that a stop asked for while the database wakes
// is not waited out; the datastore closes it. Its logger is a noop: OpenFGA logs by
// zap, with the IDs of stores, models and requests in its fields, which api's log
// would drop (R26). Its caches stay off, as they are by default, so a check sees
// the write before it. The pool comes back too, for what OpenFGA does not do
// itself: clearing its changelog.
func newServer(ctx context.Context, url string) (*server.Server, *pgxpool.Pool, error) {
	pool, err := store.Open(ctx, url)
	if err != nil {
		return nil, nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("authz: reach the database: %w", err)
	}
	datastore, err := postgres.NewWithDB(pool, nil, sqlcommon.NewConfig(sqlcommon.WithLogger(logger.NewNoopLogger())))
	if err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("authz: open the datastore: %w", err)
	}
	srv, err := server.NewServerWithOpts(
		server.WithDatastore(datastore),
		server.WithLogger(logger.NewNoopLogger()),
		server.WithContinuationTokenSerializer(sqlcommon.NewSQLContinuationTokenSerializer()),
	)
	if err != nil {
		datastore.Close()
		return nil, nil, fmt.Errorf("authz: start OpenFGA: %w", err)
	}
	return srv, pool, nil
}

// findStore is the ID of the one store api.
func findStore(ctx context.Context, srv *server.Server) (string, error) {
	listed, err := srv.ListStores(ctx, &openfgav1.ListStoresRequest{Name: storeName, PageSize: wrapperspb.Int32(2)})
	if err != nil {
		return "", fmt.Errorf("authz: list the stores: %w", err)
	}
	switch stores := listed.GetStores(); len(stores) {
	case 0:
		return "", ErrNoStore
	case 1:
		return stores[0].GetId(), nil
	default:
		return "", errManyStores
	}
}

// findModel is the ID of the store's model that says what the embedded one says,
// or empty when the store has none such: the models are read page by page, newest
// first, so the newest such is found.
func findModel(ctx context.Context, srv *server.Server, storeID string) (string, error) {
	want := embedded()
	token := ""
	for {
		read, err := srv.ReadAuthorizationModels(ctx, &openfgav1.ReadAuthorizationModelsRequest{
			StoreId: storeID, PageSize: wrapperspb.Int32(pageSize), ContinuationToken: token,
		})
		if err != nil {
			return "", fmt.Errorf("authz: read the models: %w", err)
		}
		for _, model := range read.GetAuthorizationModels() {
			if sameModel(model, want) {
				return model.GetId(), nil
			}
		}
		if token = read.GetContinuationToken(); token == "" {
			return "", nil
		}
	}
}

// user and learner are the objects of the model: an account by its ID, and a
// learner's profile by its ID.
func user(accountID string) string    { return "user:" + accountID }
func learner(profileID string) string { return "learner:" + profileID }

// Can says whether the account has the right on the profile. A profile that is
// not there, and one that is another account's, both say no, so that the answer
// tells nothing of a profile the account does not own (architecture 3.1).
func (r *Rights) Can(ctx context.Context, accountID string, right Right, profileID string) (bool, error) {
	switch right {
	case CanLearn, CanManage, CanView:
	default:
		return false, fmt.Errorf("authz: no right %q in the model", right)
	}
	checked, err := r.srv.Check(ctx, &openfgav1.CheckRequest{
		StoreId:              r.store,
		AuthorizationModelId: r.model,
		TupleKey:             &openfgav1.CheckRequestTupleKey{User: user(accountID), Relation: string(right), Object: learner(profileID)},
		// The caches are off, so every check is consistent; this says so too.
		Consistency: openfgav1.ConsistencyPreference_HIGHER_CONSISTENCY,
	})
	if err != nil {
		return false, fmt.Errorf("authz: check: %w", err)
	}
	return checked.GetAllowed(), nil
}

// WriteOwner writes the tuple: the account owns the profile. A repeat is no
// error. It is written inside the transaction of the database api that makes the
// profile, before its COMMIT (architecture 3.1; R31).
func (r *Rights) WriteOwner(ctx context.Context, accountID, profileID string) error {
	_, err := r.srv.Write(ctx, &openfgav1.WriteRequest{
		StoreId:              r.store,
		AuthorizationModelId: r.model,
		Writes: &openfgav1.WriteRequestWrites{
			TupleKeys:   []*openfgav1.TupleKey{{User: user(accountID), Relation: "owner", Object: learner(profileID)}},
			OnDuplicate: "ignore",
		},
	})
	if err != nil {
		return fmt.Errorf("authz: write the owner: %w", err)
	}
	return nil
}

// DeleteOwner deletes the tuple of the profile, after the transaction that
// deleted it (R21). A tuple that is not there is no error.
func (r *Rights) DeleteOwner(ctx context.Context, accountID, profileID string) error {
	return r.DeleteOwned(ctx, accountID, []string{profileID})
}

// DeleteOwned deletes the account's tuples of the profiles, as many at once as
// OpenFGA takes, skipping those that are not there (R21; R27 canvas-api).
func (r *Rights) DeleteOwned(ctx context.Context, accountID string, profileIDs []string) error {
	for start := 0; start < len(profileIDs); start += pageSize {
		batch := profileIDs[start:min(start+pageSize, len(profileIDs))]
		keys := make([]*openfgav1.TupleKeyWithoutCondition, 0, len(batch))
		for _, profileID := range batch {
			keys = append(keys, &openfgav1.TupleKeyWithoutCondition{User: user(accountID), Relation: "owner", Object: learner(profileID)})
		}
		_, err := r.srv.Write(ctx, &openfgav1.WriteRequest{
			StoreId:              r.store,
			AuthorizationModelId: r.model,
			Deletes:              &openfgav1.WriteRequestDeletes{TupleKeys: keys, OnMissing: "ignore"},
		})
		if err != nil {
			return fmt.Errorf("authz: delete the owners: %w", err)
		}
	}
	return nil
}

// OwnedProfiles is the IDs of the profiles the account owns by its tuples, which
// the deletion of an account reads them by (R21).
func (r *Rights) OwnedProfiles(ctx context.Context, accountID string) ([]string, error) {
	var profiles []string
	token := ""
	for {
		read, err := r.srv.Read(ctx, &openfgav1.ReadRequest{
			StoreId:           r.store,
			TupleKey:          &openfgav1.ReadRequestTupleKey{User: user(accountID), Relation: "owner", Object: "learner:"},
			PageSize:          wrapperspb.Int32(pageSize),
			ContinuationToken: token,
			Consistency:       openfgav1.ConsistencyPreference_HIGHER_CONSISTENCY,
		})
		if err != nil {
			return nil, fmt.Errorf("authz: read the owners: %w", err)
		}
		for _, tuple := range read.GetTuples() {
			profiles = append(profiles, tuple.GetKey().GetObject()[len("learner:"):])
		}
		if token = read.GetContinuationToken(); token == "" {
			return profiles, nil
		}
	}
}

// Owned is a tuple of the store: the account owns the profile, since Written.
type Owned struct {
	AccountID string
	ProfileID string
	Written   time.Time
}

// Tuples is every tuple of the store by which an account owns a profile, a page
// at a time, for the daily job that finds those left dangling (R21). A tuple of
// any other kind, which the model has none of yet, is not given. An error ends
// the list.
func (r *Rights) Tuples(ctx context.Context) iter.Seq2[Owned, error] {
	return func(yield func(Owned, error) bool) {
		token := ""
		for {
			read, err := r.srv.Read(ctx, &openfgav1.ReadRequest{
				StoreId:           r.store,
				PageSize:          wrapperspb.Int32(pageSize),
				ContinuationToken: token,
				Consistency:       openfgav1.ConsistencyPreference_HIGHER_CONSISTENCY,
			})
			if err != nil {
				yield(Owned{}, fmt.Errorf("authz: read the tuples: %w", err))
				return
			}
			for _, tuple := range read.GetTuples() {
				key := tuple.GetKey()
				accountID, isUser := strings.CutPrefix(key.GetUser(), "user:")
				profileID, isLearner := strings.CutPrefix(key.GetObject(), "learner:")
				if !isUser || !isLearner || key.GetRelation() != "owner" {
					continue
				}
				owned := Owned{AccountID: accountID, ProfileID: profileID, Written: tuple.GetTimestamp().AsTime()}
				if !yield(owned, nil) {
					return
				}
			}
			if token = read.GetContinuationToken(); token == "" {
				return
			}
		}
	}
}

// changelogBatch is how many rows of the changelog one DELETE takes, so that no
// statement holds the table long.
const changelogBatch = 1000

// DropChangelog deletes the store's changelog written before before, a batch at
// a time, and says how many rows went. OpenFGA keeps it for ever and does not
// clear it; it holds who owned which profile, so the daily job keeps it no longer
// than two days (R05; R27 canvas-api). A stop between batches keeps what went.
func (r *Rights) DropChangelog(ctx context.Context, before time.Time) (int64, error) {
	var dropped int64
	for {
		tag, err := r.pool.Exec(ctx, `DELETE FROM changelog WHERE ctid IN (
			SELECT ctid FROM changelog WHERE store = $1 AND inserted_at < $2 LIMIT $3)`,
			r.store, before, changelogBatch)
		if err != nil {
			return dropped, fmt.Errorf("authz: clear the changelog: %w", err)
		}
		dropped += tag.RowsAffected()
		if tag.RowsAffected() < changelogBatch {
			return dropped, nil
		}
	}
}
