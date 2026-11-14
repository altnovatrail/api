package learning_test

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/learning"
	"github.com/altnovatrail/api/internal/legal"
	"github.com/altnovatrail/api/internal/packages/packagestest"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// signature is what the picture of a stored work begins with: PNG's.
const signature = "\x89PNG\r\n\x1a\n"

// storedWork is a synthetic work of the attempt with the history, one
// submission and a picture.
func storedWork(attempt, history string) *v1.StoredWork {
	return &v1.StoredWork{AttemptId: attempt, History: []byte(history),
		Submissions: []*v1.WorkSubmission{{Context: []byte(`{"answer_slot":1}`), HistoryLength: 2}},
		Picture:     []byte(signature + "the strokes")}
}

// sizeOf is the size of the work, as StoredWork counts it.
func sizeOf(work *v1.StoredWork) int64 {
	size := len(work.GetHistory()) + len(work.GetPicture())
	for _, s := range work.GetSubmissions() {
		size += len(s.GetContext())
	}
	return int64(size)
}

// storage switches the storage of the profile's works on, to the current
// storage-consent, or off, as the adult who decides it.
func (w *world) storage(t *testing.T, l learner, profile string, on bool) {
	t.Helper()
	req := &v1.SetWorkStorageRequest{ProfileId: profile, Action: v1.Action_ACTION_ADULT, Enabled: on}
	if on {
		req.Text = legal.Spec().On(time.Now()).Refs(legal.StorageConsent)[0]
	}
	if _, err := w.accounts.SetWorkStorage(l.ctx, req); err != nil {
		t.Fatalf("SetWorkStorage %v: %v", on, err)
	}
}

func (w *world) put(l learner, profile string, work *v1.StoredWork) (*v1.StoredWorkInfo, error) {
	res, err := w.works.PutWork(l.ctx, &v1.PutWorkRequest{ProfileId: profile, Work: work})
	return res.GetWork(), err
}

func (w *world) list(t *testing.T, l learner, profile string) []string {
	t.Helper()
	res, err := w.works.ListWorks(l.ctx, &v1.ListWorksRequest{ProfileId: profile, Action: v1.Action_ACTION_LEARNING})
	if err != nil {
		t.Fatal(err)
	}
	var attempts []string
	for _, work := range res.GetWorks() {
		attempts = append(attempts, work.GetAttemptId())
	}
	return attempts
}

func (w *world) erase(l learner, profile, attempt string, action v1.Action) error {
	_, err := w.works.DeleteWork(l.ctx, &v1.DeleteWorkRequest{ProfileId: profile, AttemptId: attempt, Action: action})
	return err
}

// kept is how many works of the profile the database keeps.
func (w *world) kept(t *testing.T, profile string) int {
	t.Helper()
	var n int
	if err := w.pool.QueryRow(t.Context(), `SELECT count(*) FROM works WHERE profile_id = $1`, profile).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A work is kept as it was put, with its kind by the attempt's task and when it
// was kept, and with no field the contract does not name; the last one put
// stands. It is listed newest first, read whole, by the learner and by the adult
// with can_manage, and erased by the adult who decides the storage, a repeat
// too; neither its history nor its picture reaches the log (R113 spec; R70).
func TestAWorkIsKeptListedReadAndErased(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	w.storage(t, l, l.adult, true)
	digest := string(w.current.Digest)
	apples := w.attempt(t, l.adult, digest, "apples")
	story := w.attempt(t, l.adult, digest, "story")
	sent := storedWork(apples, `[{"mark":"of the history"}]`)
	sent.Kind, sent.StoredAt = v1.WorkKind_WORK_KIND_ESSAY, timestamppb.New(time.Unix(0, 0))
	sent.ProtoReflect().SetUnknown(protowire.AppendBytes(protowire.AppendTag(nil, 99, protowire.BytesType), []byte("not named")))
	before := time.Now().Add(-time.Second)
	first, err := w.put(l, l.adult, sent)
	if err != nil || first.GetAttemptId() != apples || first.GetKind() != v1.WorkKind_WORK_KIND_HANDWRITING ||
		first.GetSizeBytes() != sizeOf(sent) || first.GetStoredAt().AsTime().Before(before) {
		t.Fatalf("PutWork = %v, %v; want a handwriting of %d bytes kept now", first, err, sizeOf(sent))
	}
	w.clock.move(time.Minute)
	essay, err := w.put(l, l.adult, storedWork(story, `[]`))
	if err != nil || essay.GetKind() != v1.WorkKind_WORK_KIND_ESSAY {
		t.Fatalf("PutWork of the story = %v, %v; want an essay", essay, err)
	}
	listed, err := w.works.ListWorks(l.ctx, &v1.ListWorksRequest{ProfileId: l.adult, Action: v1.Action_ACTION_LEARNING})
	if err != nil || len(listed.GetWorks()) != 2 || !proto.Equal(listed.GetWorks()[0], essay) || !proto.Equal(listed.GetWorks()[1], first) {
		t.Fatalf("ListWorks = %v, %v; want the essay, then the handwriting", listed, err)
	}
	got, err := w.works.GetWork(l.ctx, &v1.GetWorkRequest{ProfileId: l.adult, AttemptId: apples, Action: v1.Action_ACTION_LEARNING})
	want := storedWork(apples, `[{"mark":"of the history"}]`)
	want.Kind, want.StoredAt = v1.WorkKind_WORK_KIND_HANDWRITING, first.GetStoredAt()
	if err != nil || !proto.Equal(got.GetWork(), want) {
		t.Fatalf("GetWork = %v, %v; want %v, with no field the contract does not name", got, err, want)
	}

	// The last one put stands, and is the newest.
	w.clock.move(time.Minute)
	if _, err := w.put(l, l.adult, storedWork(apples, `[1, 2]`)); err != nil {
		t.Fatal(err)
	}
	if got := w.list(t, l, l.adult); len(got) != 2 || got[0] != apples || got[1] != story {
		t.Errorf("the works = %v, want the handwriting put again first", got)
	}
	got, err = w.works.GetWork(l.ctx, &v1.GetWorkRequest{ProfileId: l.adult, AttemptId: apples, Action: v1.Action_ACTION_ADULT})
	if err != nil || string(got.GetWork().GetHistory()) != `[1, 2]` || !got.GetWork().GetStoredAt().AsTime().After(first.GetStoredAt().AsTime()) {
		t.Errorf("GetWork by the adult = %v, %v; want the last one put", got, err)
	}
	if res, err := w.works.ListWorks(l.ctx, &v1.ListWorksRequest{ProfileId: l.adult, Action: v1.Action_ACTION_ADULT}); err != nil || len(res.GetWorks()) != 2 {
		t.Errorf("ListWorks by the adult = %v, %v; want both", res, err)
	}

	// An adult without can_manage is refused with no Refusal, and an action not
	// named is malformed.
	w.denyManage.Store(true)
	_, err = w.works.ListWorks(l.ctx, &v1.ListWorksRequest{ProfileId: l.adult, Action: v1.Action_ACTION_ADULT})
	refusedAs(t, err, connect.CodePermissionDenied, v1.Cause_CAUSE_UNSPECIFIED)
	_, err = w.works.GetWork(l.ctx, &v1.GetWorkRequest{ProfileId: l.adult, AttemptId: apples, Action: v1.Action_ACTION_ADULT})
	refusedAs(t, err, connect.CodePermissionDenied, v1.Cause_CAUSE_UNSPECIFIED)
	refusedAs(t, w.erase(l, l.adult, apples, v1.Action_ACTION_ADULT), connect.CodePermissionDenied, v1.Cause_CAUSE_UNSPECIFIED)
	w.denyManage.Store(false)
	_, err = w.works.ListWorks(l.ctx, &v1.ListWorksRequest{ProfileId: l.adult})
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	refusedAs(t, w.erase(l, l.adult, apples, v1.Action_ACTION_UNSPECIFIED), connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)

	// The adult decides the storage of the owner's profile, and erases.
	refusedAs(t, w.erase(l, l.adult, apples, v1.Action_ACTION_LEARNING), connect.CodePermissionDenied, v1.Cause_CAUSE_ADULT_NEEDED)
	for range 2 {
		if err := w.erase(l, l.adult, apples, v1.Action_ACTION_ADULT); err != nil {
			t.Fatalf("DeleteWork = %v, want erased", err)
		}
	}
	_, err = w.works.GetWork(l.ctx, &v1.GetWorkRequest{ProfileId: l.adult, AttemptId: apples, Action: v1.Action_ACTION_LEARNING})
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND)
	if got := w.list(t, l, l.adult); len(got) != 1 || got[0] != story {
		t.Errorf("the works after the erasure = %v, want the essay", got)
	}
	other := w.attempt(t, l.child, digest, "apples")
	for _, attempt := range []string{"not an ID", "00000000-0000-0000-0000-000000000000", other} {
		refusedAs(t, w.erase(l, l.adult, attempt, v1.Action_ACTION_ADULT), connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND)
		_, err := w.works.GetWork(l.ctx, &v1.GetWorkRequest{ProfileId: l.adult, AttemptId: attempt, Action: v1.Action_ACTION_LEARNING})
		refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND)
	}

	log := w.log.String()
	if strings.Contains(log, "of the history") || strings.Contains(log, "answer_slot") ||
		strings.Contains(log, base64.StdEncoding.EncodeToString(sent.GetPicture())) || strings.Contains(log, "the strokes") {
		t.Errorf("the log holds a stored work: %s", log)
	}
	if !strings.Contains(log, `"method":"/novatrail.api.v1.WorkService/PutWork","code":"ok"`) {
		t.Errorf("the log has no line of PutWork: %s", log)
	}
}

// A work is refused by its size, 4 MiB at most, before its form, which the
// contract names; the work kept before stays. A work of an attempt that is not
// the profile's is "not found", one put while the storage is off "storage off",
// and one of a package whose digest the index does not know is unavailable.
func TestAWorkIsRefusedBySizeFormAndState(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	digest := string(w.current.Digest)
	a := w.attempt(t, l.adult, digest, "apples")
	_, err := w.put(l, l.adult, storedWork(a, `[]`))
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_STORAGE_OFF)
	w.storage(t, l, l.adult, true)

	largest := storedWork(a, `[]`)
	largest.Picture = append(largest.Picture, make([]byte, learning.MaxWork-sizeOf(largest))...)
	if info, err := w.put(l, l.adult, largest); err != nil || info.GetSizeBytes() != learning.MaxWork {
		t.Fatalf("PutWork of 4 MiB = %v, %v; want it kept", info, err)
	}
	for name, over := range map[string]func(*v1.StoredWork){
		"a picture a byte over": func(work *v1.StoredWork) { work.Picture = append(work.Picture, 0) },
		"a context more": func(work *v1.StoredWork) {
			work.Submissions = append(work.Submissions, &v1.WorkSubmission{Context: []byte(`{}`)})
		},
		"a malformed one over": func(work *v1.StoredWork) { work.History = []byte("[ \n") },
	} {
		work := proto.Clone(largest).(*v1.StoredWork)
		over(work)
		_, err := w.put(l, l.adult, work)
		t.Run(name, func(t *testing.T) { refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_WORK_TOO_LARGE) })
	}
	got, err := w.works.GetWork(l.ctx, &v1.GetWorkRequest{ProfileId: l.adult, AttemptId: a, Action: v1.Action_ACTION_LEARNING})
	if err != nil || sizeOf(got.GetWork()) != learning.MaxWork {
		t.Errorf("GetWork after the refusals = %v; want the work of 4 MiB", err)
	}

	for name, malform := range map[string]func(*v1.StoredWork){
		"a history not JSON":   func(work *v1.StoredWork) { work.History = []byte(`[`) },
		"a history an object":  func(work *v1.StoredWork) { work.History = []byte(`{}`) },
		"no history":           func(work *v1.StoredWork) { work.History = nil },
		"a history not UTF-8":  func(work *v1.StoredWork) { work.History = []byte("[\"\xff\"]") },
		"a context an array":   func(work *v1.StoredWork) { work.Submissions[0].Context = []byte(`[]`) },
		"a context not JSON":   func(work *v1.StoredWork) { work.Submissions[0].Context = []byte(`{"a"`) },
		"a context not UTF-8":  func(work *v1.StoredWork) { work.Submissions[0].Context = []byte("{\"a\":\"\xff\"}") },
		"a count below zero":   func(work *v1.StoredWork) { work.Submissions[0].HistoryLength = -1 },
		"no picture":           func(work *v1.StoredWork) { work.Picture = nil },
		"a picture not of PNG": func(work *v1.StoredWork) { work.Picture = []byte("GIF89a, the strokes") },
	} {
		work := storedWork(a, `[]`)
		malform(work)
		_, err := w.put(l, l.adult, work)
		t.Run(name, func(t *testing.T) { refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED) })
	}
	if _, err := w.put(l, l.adult, &v1.StoredWork{AttemptId: a, History: []byte(" [] "), Picture: []byte(signature)}); err != nil {
		t.Errorf("PutWork of a work with no submission = %v, want it kept", err)
	}

	other := w.attempt(t, l.child, digest, "apples")
	for _, attempt := range []string{"", "not an ID", "00000000-0000-0000-0000-000000000000", other} {
		_, err := w.put(l, l.adult, storedWork(attempt, `[]`))
		refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND)
	}
	_, err = w.put(l, l.adult, storedWork(w.attempt(t, l.adult, unknown, "apples"), `[]`))
	refusedAs(t, err, connect.CodeUnavailable, v1.Cause_CAUSE_PACKAGE_UNKNOWN)
	waiting := w.child(t, l, "under_13", false)
	_, err = w.put(l, waiting, storedWork(a, `[]`))
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CONSENT)
	_, err = w.works.ListWorks(l.ctx, &v1.ListWorksRequest{ProfileId: waiting, Action: v1.Action_ACTION_LEARNING})
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CONSENT)
}

// Switching the storage off erases the profile's works at once, and the
// deletion of the profile or of the account takes them with it (R113 spec; R21).
func TestTheWorksGoWithTheStorageAndTheProfile(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	digest := string(w.current.Digest)
	a := w.attempt(t, l.adult, digest, "apples")
	w.storage(t, l, l.adult, true)
	if _, err := w.put(l, l.adult, storedWork(a, `[]`)); err != nil {
		t.Fatal(err)
	}
	w.storage(t, l, l.adult, false)
	if got, n := w.list(t, l, l.adult), w.kept(t, l.adult); len(got) != 0 || n != 0 {
		t.Errorf("the works with the storage off = %v, %d kept; want none", got, n)
	}
	_, err := w.put(l, l.adult, storedWork(a, `[]`))
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_STORAGE_OFF)
	w.storage(t, l, l.adult, true)
	if got := w.list(t, l, l.adult); len(got) != 0 {
		t.Errorf("the works with the storage on again = %v, want none", got)
	}
	if _, err := w.put(l, l.adult, storedWork(a, `[]`)); err != nil {
		t.Fatal(err)
	}
	if _, err := w.accounts.DeleteProfile(l.ctx, &v1.DeleteProfileRequest{ProfileId: l.adult}); err != nil {
		t.Fatal(err)
	}
	if n := w.kept(t, l.adult); n != 0 {
		t.Errorf("the deleted profile's works = %d, want none", n)
	}

	m := w.learner(t)
	b := w.attempt(t, m.adult, digest, "apples")
	w.storage(t, m, m.adult, true)
	if _, err := w.put(m, m.adult, storedWork(b, `[]`)); err != nil {
		t.Fatal(err)
	}
	if _, err := w.accounts.DeleteAccount(m.ctx, &v1.DeleteAccountRequest{}); err != nil {
		t.Fatal(err)
	}
	if n := w.kept(t, m.adult); n != 0 {
		t.Errorf("the deleted account's works = %d, want none", n)
	}
}

// A profile that is none, or another account's, is "no profile" at each method,
// before anything is read of it (R04).
func TestTheWorksOfAProfileNotTheAccountsAreNoProfile(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	other := w.learner(t)
	attempt := w.attempt(t, other.adult, string(w.current.Digest), "apples")
	adult := v1.Action_ACTION_ADULT
	for _, profile := range []string{"00000000-0000-0000-0000-000000000000", other.adult} {
		for name, call := range map[string]func() error{
			"PutWork": func() error {
				_, err := w.put(l, profile, storedWork(attempt, `[]`))
				return err
			},
			"ListWorks": func() error {
				_, err := w.works.ListWorks(l.ctx, &v1.ListWorksRequest{ProfileId: profile, Action: adult})
				return err
			},
			"GetWork": func() error {
				_, err := w.works.GetWork(l.ctx, &v1.GetWorkRequest{ProfileId: profile, AttemptId: attempt, Action: adult})
				return err
			},
			"DeleteWork": func() error { return w.erase(l, profile, attempt, adult) },
		} {
			t.Run(name, func(t *testing.T) { refusedAs(t, call(), connect.CodeNotFound, v1.Cause_CAUSE_NO_PROFILE) })
		}
	}
}

// At a profile whose learner gave their own consent the learner decides the
// storage, and erases a work themselves: the adult is told so.
func TestALearnerWhoConsentsThemselvesErasesTheirWork(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	teen := w.child(t, l, "from_consent_to_18", true)
	w.exec(t, `UPDATE consents SET giver = 'learner', request_code = NULL, form_state = NULL WHERE profile_id = $1`, teen)
	version := int32(1)
	if err := pgx.BeginFunc(t.Context(), w.pool, func(tx pgx.Tx) error {
		return store.SwitchWorkStorageOn(t.Context(), tx, teen, store.GiverLearner, &version, time.Now())
	}); err != nil {
		t.Fatal(err)
	}
	a := w.attempt(t, teen, string(w.current.Digest), "apples")
	if _, err := w.put(l, teen, storedWork(a, `[]`)); err != nil {
		t.Fatal(err)
	}
	refusedAs(t, w.erase(l, teen, a, v1.Action_ACTION_ADULT), connect.CodePermissionDenied, v1.Cause_CAUSE_LEARNER_DECIDES)
	if err := w.erase(l, teen, a, v1.Action_ACTION_LEARNING); err != nil {
		t.Fatalf("DeleteWork by the learner = %v, want erased", err)
	}
	if got := w.list(t, l, teen); len(got) != 0 {
		t.Errorf("the works after the erasure = %v, want none", got)
	}
}

// A work of an attempt whose package the image holds no longer — out of the
// window of format versions — keeps the kind of the work kept of it before, and
// with none is refused.
func TestAWorkOfAPackageOutOfTheWindowKeepsItsKind(t *testing.T) {
	t.Parallel()

	built(t)
	const old = formatv0.Digest("sha256:00000000000000000000000000000000000000000000000000000000000000a1")
	set := packagestest.Set(t, packagestest.Entry{Pkg: current, Release: "v0.2.0"},
		packagestest.Entry{ID: "example/old", Version: "0.1.0", Format: "0.3", Digest: old, Release: "v0.1.0"})
	// The second world cannot read the kind kept.
	for _, failOn := range []string{"", "SELECT kind FROM works"} {
		var wrap []func(*pgxpool.Pool) rpc.DB
		if failOn != "" {
			wrap = append(wrap, func(p *pgxpool.Pool) rpc.DB { return &failing{Pool: p, failOn: failOn} })
		}
		w := worldOn(t, set, gate(t, "", config.RungFull), nil, wrap...)
		l := w.learner(t)
		w.storage(t, l, l.adult, true)
		a := w.attempt(t, l.adult, string(old), "apples")
		_, err := w.put(l, l.adult, storedWork(a, `[]`))
		if failOn != "" {
			refusedAs(t, err, connect.CodeUnavailable, v1.Cause_CAUSE_UNSPECIFIED)
			continue
		}
		refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_UNSPECIFIED)
		w.exec(t, `INSERT INTO works (profile_id, attempt_id, kind, size_bytes, work, stored_at)
			VALUES ($1, $2, 'essay', 0, '', now())`, l.adult, a)
		if info, err := w.put(l, l.adult, storedWork(a, `[]`)); err != nil || info.GetKind() != v1.WorkKind_WORK_KIND_ESSAY {
			t.Errorf("PutWork again = %v, %v; want the essay it was", info, err)
		}
	}
}

// Each statement of a request on the works that fails makes that request
// unavailable, and so does a work kept whose bytes cannot be read.
func TestTheWorksOnADatabaseThatFailsAreUnavailable(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ failOn, request string }{
		{"SELECT package_digest", "PutWork"}, {"SELECT work_storage", "PutWork"}, {"INSERT INTO works", "PutWork"},
		{"SELECT attempt_id, kind", "ListWorks"}, {"SELECT kind, size_bytes", "GetWork"},
		{"SELECT giver", "DeleteWork"}, {"SELECT EXISTS", "DeleteWork"}, {"DELETE FROM works", "DeleteWork"},
	} {
		t.Run(tc.failOn, func(t *testing.T) {
			t.Parallel()

			w := newWorld(t, func(p *pgxpool.Pool) rpc.DB { return &failing{Pool: p, failOn: tc.failOn} })
			l := w.learner(t)
			w.storage(t, l, l.adult, true)
			a := w.attempt(t, l.adult, string(w.current.Digest), "apples")
			requests := map[string]func() error{
				"PutWork": func() error {
					_, err := w.put(l, l.adult, storedWork(a, `[]`))
					return err
				},
				"ListWorks": func() error {
					_, err := w.works.ListWorks(l.ctx, &v1.ListWorksRequest{ProfileId: l.adult, Action: v1.Action_ACTION_LEARNING})
					return err
				},
				"GetWork": func() error {
					_, err := w.works.GetWork(l.ctx, &v1.GetWorkRequest{ProfileId: l.adult, AttemptId: a, Action: v1.Action_ACTION_LEARNING})
					return err
				},
				"DeleteWork": func() error { return w.erase(l, l.adult, a, v1.Action_ACTION_ADULT) },
			}
			refusedAs(t, requests[tc.request](), connect.CodeUnavailable, v1.Cause_CAUSE_UNSPECIFIED)
		})
	}

	w := newWorld(t)
	l := w.learner(t)
	a := w.attempt(t, l.adult, string(w.current.Digest), "apples")
	w.exec(t, `INSERT INTO works (profile_id, attempt_id, kind, size_bytes, work, stored_at)
		VALUES ($1, $2, 'handwriting', 1, '\xff'::bytea, now())`, l.adult, a)
	_, err := w.works.GetWork(l.ctx, &v1.GetWorkRequest{ProfileId: l.adult, AttemptId: a, Action: v1.Action_ACTION_LEARNING})
	refusedAs(t, err, connect.CodeUnavailable, v1.Cause_CAUSE_UNSPECIFIED)
}
