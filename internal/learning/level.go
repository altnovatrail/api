package learning

import (
	"context"
	"slices"
	"time"

	"connectrpc.com/connect/v2"
	learnermodel "github.com/altnovatrail/learner-model"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/altnovatrail/api/internal/model"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// SetModuleLevel is an adult's correction of the level of a connected module,
// "Easier" or "Harder" in the section for adults, with the right can_manage,
// which the checks hold, at every profile of the account. The profile's consent
// is confirmed and its age needs no restating, as for a learning request: the
// fact is processing of the learner's data. After the evidence the profile owes
// is settled, the band named is the level's own — nothing is appended, and the
// answer is the level as it is —, the band next to it in the order of the bands
// of the current version's manifest, or, while the level stands below the
// lowest band, the lowest; any other is CAUSE_BAND_NOT_NEXT. The correction
// goes to the journal of evidence, and learner-model sets the level in that
// band in one step. It repeats by the state of the profile, with no idempotency
// key.
func (p *ProgressService) SetModuleLevel(ctx context.Context, req *v1.SetModuleLevelRequest) (*v1.SetModuleLevelResponse, error) {
	s := p.s
	if req.GetBand() == "" {
		return nil, rpc.Malformed("band")
	}
	profileID := req.GetProfileId()
	category, err := s.learner(ctx, profileID)
	if err != nil {
		return nil, err
	}
	var answer *v1.SetModuleLevelResponse
	err = s.write(ctx, func(tx pgx.Tx, now time.Time) error {
		connected, err := store.ConnectedModules(ctx, tx, profileID)
		if err != nil {
			return err
		}
		module := formatv0.ModuleID(req.GetModuleId())
		if !slices.ContainsFunc(connected, func(c store.ConnectedModule) bool { return c.ModuleID == string(module) }) {
			return store.ErrNotFound
		}
		pkg, ok := s.set.OfferedFor(module, category)
		if !ok {
			return rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CURRENT_VERSION)
		}
		settled, err := s.model.Settle(ctx, tx, profileID, now)
		if err != nil {
			return err
		}
		read, err := s.model.Read(settled, now, s.engineIDs(connected, category))
		if err != nil {
			return err
		}
		level, set, err := s.levelOf(ctx, tx, profileID, read, string(module))
		if err != nil {
			return err
		}
		target, ok := nextBand(pkg.View.Manifest.Bands, level, req.GetBand())
		switch {
		case !ok:
			return rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_BAND_NOT_NEXT)
		case target == level:
			answer = &v1.SetModuleLevelResponse{LevelBand: level.ID, LevelSetAt: set}
			return nil
		}
		at := now.Truncate(time.Second)
		if _, err := s.model.Correct(ctx, tx, profileID, settled, pkg, target, at); err != nil {
			return err
		}
		answer = &v1.SetModuleLevelResponse{LevelBand: target.ID, LevelSetAt: timestamppb.New(at)}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return answer, nil
}

// nextBand is the band named id that a correction may set the level in, by the
// bands of the manifest, in order: the band of the level itself, the one before
// it or the one after it; while the level stands below the lowest band, the
// zero BandRef, the lowest band alone. A band the manifest does not name, or
// one further off, is none, false.
func nextBand(bands []formatv0.Band, level learnermodel.BandRef, id string) (learnermodel.BandRef, bool) {
	i := slices.IndexFunc(bands, func(b formatv0.Band) bool { return string(b.ID) == id })
	if i < 0 {
		return learnermodel.BandRef{}, false
	}
	place := int32(i + 1)
	if place > level.Place+1 || place < level.Place-1 {
		return learnermodel.BandRef{}, false
	}
	return learnermodel.BandRef{Place: place, ID: id}, true
}

// levelOf is the band of the level of the module as read gives it — the zero
// BandRef while the level stands below the lowest band — and when an adult last
// set it, nil when no adult has.
func (s *Service) levelOf(ctx context.Context, tx pgx.Tx, profileID string, read learnermodel.Readout, module string) (learnermodel.BandRef, *timestamppb.Timestamp, error) {
	var band learnermodel.BandRef
	if i := slices.IndexFunc(read.Modules, func(m learnermodel.ModuleReadout) bool { return m.Module == module }); i >= 0 && read.Modules[i].HasLevelBand {
		band = read.Modules[i].LevelBand
	}
	levels, err := model.Levels(ctx, tx, profileID, module)
	if err != nil || len(levels) == 0 {
		return band, nil, err
	}
	return band, timestamppb.New(levels[0].SetAt), nil
}

// engineIDs is the IDs of the modules of the engine for a learner of category,
// of those connected, as engineOf gives them.
func (s *Service) engineIDs(connected []store.ConnectedModule, category formatv0.AgeCategory) []string {
	var ids []string
	for _, pkg := range s.engineOf(connected, category) {
		ids = append(ids, string(pkg.View.Manifest.ID))
	}
	return ids
}
