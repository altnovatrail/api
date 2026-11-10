package catalog

import (
	"context"
	"errors"
	"slices"

	"connectrpc.com/connect/v2"
	"github.com/altnovatrail/module-kit/modulepackage"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// The services the catalog gives, as the contract names them.
var (
	_ apiv1connect.PackageServiceHandler = (*PackageService)(nil)
	_ apiv1connect.ModuleServiceHandler  = (*ModuleService)(nil)
)

// DB is what ModuleService reads and writes the modules of a profile in: the
// pool of the database api.
type DB interface {
	rpc.DB
	store.Lister
}

// ModuleService gives the modules of a learner (R07; contract, ModuleService):
// each method is a learning request, with the right can_learn, which the checks
// hold before it, and a profile whose consent is not confirmed is refused. A
// module suits a learner by the audience of its current version alone: the
// explanation language chooses nothing (R92 spec).
type ModuleService struct {
	c  *Catalog
	db DB
}

// Modules is the ModuleService of the catalog, on db.
func (c *Catalog) Modules(db DB) *ModuleService { return &ModuleService{c: c, db: db} }

// ListCatalog gives the modules of the pinned index whose current version suits
// the profile, connected ones too, by their IDs. An ID is lowercase ASCII by
// the format's schema, so the order of its bytes is that of its UTF-16 code
// units, as the contract asks.
func (s *ModuleService) ListCatalog(ctx context.Context, req *v1.ListCatalogRequest) (*v1.ListCatalogResponse, error) {
	category, err := s.learner(ctx, req.GetProfileId())
	if err != nil {
		return nil, err
	}
	var modules []*v1.CatalogModule
	for _, id := range s.c.set.Modules() {
		if pkg, ok := s.c.current(id, category); ok {
			modules = append(modules, &v1.CatalogModule{ModuleId: string(id), PackageDigest: string(pkg.Digest)})
		}
	}
	return &v1.ListCatalogResponse{Modules: modules}, nil
}

// ConnectModule connects a module that the index names and whose current version
// suits the profile. A module connected already stays as it is, and its repeat
// gets the same answer, the modules connected, even when the module has lost
// its current version meanwhile: the request repeats by the profile's state.
func (s *ModuleService) ConnectModule(ctx context.Context, req *v1.ConnectModuleRequest) (*v1.ConnectModuleResponse, error) {
	category, err := s.learner(ctx, req.GetProfileId())
	if err != nil {
		return nil, err
	}
	id := formatv0.ModuleID(req.GetModuleId())
	if code, cause, refused := s.c.unconnectable(id, category); refused {
		rows, err := store.ConnectedModules(ctx, s.db, req.GetProfileId())
		if err != nil {
			return nil, rpc.Failed(ctx, err)
		}
		if !slices.ContainsFunc(rows, func(m store.ConnectedModule) bool { return m.ModuleID == string(id) }) {
			return nil, rpc.Refuse(ctx, code, cause)
		}
		return &v1.ConnectModuleResponse{Modules: s.c.connected(rows, category)}, nil
	}
	rows, err := s.change(ctx, req.GetProfileId(), func(tx pgx.Tx) error {
		return store.ConnectModule(ctx, tx, req.GetProfileId(), string(id))
	})
	if err != nil {
		return nil, err
	}
	return &v1.ConnectModuleResponse{Modules: s.c.connected(rows, category)}, nil
}

// unconnectable says why the module id cannot be connected to a learner of
// category, by the code and cause of its refusal: the index does not name it,
// or it has no current version for the learner.
func (c *Catalog) unconnectable(id formatv0.ModuleID, category formatv0.AgeCategory) (connect.Code, v1.Cause, bool) {
	if !c.set.Names(id) {
		return connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND, true
	}
	if _, ok := c.current(id, category); !ok {
		return connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CURRENT_VERSION, true
	}
	return 0, v1.Cause_CAUSE_UNSPECIFIED, false
}

// DisconnectModule disconnects a module; one not connected changes nothing. The
// evidence of the module stays in the profile's journal. The answer is the
// modules connected.
func (s *ModuleService) DisconnectModule(ctx context.Context, req *v1.DisconnectModuleRequest) (*v1.DisconnectModuleResponse, error) {
	category, err := s.learner(ctx, req.GetProfileId())
	if err != nil {
		return nil, err
	}
	rows, err := s.change(ctx, req.GetProfileId(), func(tx pgx.Tx) error {
		return store.DisconnectModule(ctx, tx, req.GetProfileId(), req.GetModuleId())
	})
	if err != nil {
		return nil, err
	}
	return &v1.DisconnectModuleResponse{Modules: s.c.connected(rows, category)}, nil
}

// ListConnectedModules gives the modules connected to the profile, in the order
// they were connected, each with the digest of its current version, or with
// none when it has none for the learner.
func (s *ModuleService) ListConnectedModules(ctx context.Context, req *v1.ListConnectedModulesRequest) (*v1.ListConnectedModulesResponse, error) {
	category, err := s.learner(ctx, req.GetProfileId())
	if err != nil {
		return nil, err
	}
	rows, err := store.ConnectedModules(ctx, s.db, req.GetProfileId())
	if err != nil {
		return nil, rpc.Failed(ctx, err)
	}
	return &v1.ListConnectedModulesResponse{Modules: s.c.connected(rows, category)}, nil
}

// learner is the age category of the profile, read with its consent in one
// query: a consent not confirmed is refused as rpc.RequireConsent refuses it,
// "no consent" (R116 spec).
func (s *ModuleService) learner(ctx context.Context, profileID string) (formatv0.AgeCategory, error) {
	caller, _ := rpc.CallerFrom(ctx)
	l, err := store.LearnerOf(ctx, s.db, caller.AccountID, profileID)
	if err != nil {
		return "", refused(ctx, err)
	}
	if !l.Consented {
		return "", rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CONSENT)
	}
	return formatv0.AgeCategory(l.AgeCategory), nil
}

// change runs fn under the caller's account, which a deletion marks (R21), and
// reads the modules connected in the same transaction, so that the answer is
// what the change left.
func (s *ModuleService) change(ctx context.Context, profileID string, fn func(pgx.Tx) error) ([]store.ConnectedModule, error) {
	caller, _ := rpc.CallerFrom(ctx)
	var rows []store.ConnectedModule
	err := store.WriteUnderAccount(ctx, s.db, caller.AccountID, func(tx pgx.Tx) error {
		if err := fn(tx); err != nil {
			return err
		}
		var err error
		rows, err = store.ConnectedModules(ctx, tx, profileID)
		return err
	})
	if err != nil {
		return nil, refused(ctx, err)
	}
	return rows, nil
}

// connected is the modules connected in the contract's words, each with the
// digest of its current version for a learner of category.
func (c *Catalog) connected(rows []store.ConnectedModule, category formatv0.AgeCategory) []*v1.ConnectedModule {
	modules := make([]*v1.ConnectedModule, 0, len(rows))
	for _, row := range rows {
		m := &v1.ConnectedModule{ModuleId: row.ModuleID, ConnectedAt: timestamppb.New(row.ConnectedAt)}
		if pkg, ok := c.current(formatv0.ModuleID(row.ModuleID), category); ok {
			m.PackageDigest = string(pkg.Digest)
		}
		modules = append(modules, m)
	}
	return modules
}

// current is the package of the current version of the module id for a learner
// of category: none when the module has no current version, or one whose
// audience the learner is not in — the version is one for every learner, and an
// older one is never given instead (R07).
func (c *Catalog) current(id formatv0.ModuleID, category formatv0.AgeCategory) (*modulepackage.Package, bool) {
	pkg, ok := c.set.Current(id)
	if !ok || !slices.Contains(pkg.View.Manifest.Audience.AgeCategories, category) {
		return nil, false
	}
	return pkg, true
}

// refused answers what a request on a profile met after the checks: the profile
// or the account gone meanwhile, or a failure.
func refused(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, store.ErrNoProfile):
		return rpc.Refuse(ctx, connect.CodeNotFound, v1.Cause_CAUSE_NO_PROFILE)
	case errors.Is(err, store.ErrAccountDeleting), errors.Is(err, store.ErrNoAccount):
		return rpc.AccountGone(ctx)
	}
	return rpc.Failed(ctx, err)
}
