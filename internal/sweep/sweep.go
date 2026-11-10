// Package sweep is the daily job of the terms of keeping (R05, R21, R38, R43):
// it finishes the deletions the requests left unfinished and deletes what is
// kept past its term — accounts and profiles idle for 12 months, users of the
// sign-in service with no account for 30 days, dangling tuples, the minimal
// records of consents for children 5 years after the deletion, and the records
// of keys and OpenFGA's changelog after a day — and closes what a learner
// abandoned: an attempt with no action for 7 days, and a lesson run with none
// for 30 (R10, R13). A pass keeps no state for the next: each
// step chooses its work again by a query, so a pass cut short is taken up by the
// next one. Its log is counts alone.
package sweep

import (
	"context"
	"errors"
	"iter"
	"log/slog"
	"time"

	"github.com/altnovatrail/api/internal/authn"
	"github.com/altnovatrail/api/internal/authz"
	"github.com/altnovatrail/api/internal/deletion"
	"github.com/altnovatrail/api/internal/logging"
	"github.com/altnovatrail/api/internal/store"
)

// The terms (R05, R17, R21; П-9; R117 spec). An account and a profile are
// deleted after 12 months without a request or activity, by the date of their
// row; a user with no account after 30 days with no sign-in, refresh of a token
// or creation; the minimal record of a consent for a child 5 years after the
// deletion of its account or profile; the records of keys and the changelog after
// a day, so that neither is kept for more than two. An attempt with no action for
// 7 days is closed "abandoned", and a lesson run with none for 30 days ends so
// (R10, R13).
const (
	attemptsTerm       = 7 * 24 * time.Hour
	runsTerm           = 30 * 24 * time.Hour
	usersTerm          = 30 * 24 * time.Hour
	consentRecordYears = 5
	keysTerm           = 24 * time.Hour
	changelogTerm      = 24 * time.Hour
)

// batch is how many accounts, profiles and tuples a step takes at a time (R38).
const batch = 100

// DB is the database api: a pool.
type DB interface {
	store.Beginner
	store.Querier
	store.Lister
}

// Rights is what the job asks of the rights: authz.Rights.
type Rights interface {
	deletion.Rights
	Tuples(ctx context.Context) iter.Seq2[authz.Owned, error]
	DropChangelog(ctx context.Context, before time.Time) (int64, error)
}

// Signin is what the job asks of the sign-in service: authn.Port.
type Signin interface {
	deletion.Signin
	Users(ctx context.Context) iter.Seq2[authn.User, error]
}

// Sweeper runs the passes of the job.
type Sweeper struct {
	db      DB
	rights  Rights
	signin  Signin
	deleter *deletion.Deleter
	settle  time.Duration
	log     *slog.Logger
}

// New is the job on db, rights and signin. settle is the longest a request may
// run: a mark or a tuple younger than that may be a request's still going, and
// the job leaves it.
func New(db DB, rights Rights, signin Signin, settle time.Duration, log *slog.Logger) *Sweeper {
	return &Sweeper{
		db: db, rights: rights, signin: signin, deleter: deletion.New(db, rights, signin),
		settle: settle, log: log,
	}
}

// Step is what a step of a pass did: how many it did and failed, and whether it
// went to its end — not cut short by the pass's time, and its own queries done.
type Step struct {
	Name     string
	Done     int64
	Failed   int64
	Finished bool
}

// Report is the steps of a pass, in their order.
type Report []Step

// Clean says whether the pass did all it had to: every step to its end, with no
// failure. A term that depends on the job runs to its next clean pass (R21).
func (r Report) Clean() bool {
	for _, step := range r {
		if !step.Finished || step.Failed > 0 {
			return false
		}
	}
	return true
}

// Run runs one pass at now, until ctx ends: the steps in their order, so that
// the tuples and the changelog the deletions leave go in the same pass. A step
// that fails goes on to the next item, and the next step runs all the same; once
// ctx is done, every step left stops before its first item.
func (s *Sweeper) Run(ctx context.Context, now time.Time) Report {
	steps := []struct {
		name string
		run  func(ctx context.Context, now time.Time, step *Step)
	}{
		{logging.StepMarks, s.marks},
		{logging.StepIdleAccounts, s.idleAccounts},
		{logging.StepIdleProfiles, s.idleProfiles},
		{logging.StepAbandonedAttempts, s.abandonedAttempts},
		{logging.StepAbandonedRuns, s.abandonedRuns},
		{logging.StepUsers, s.users},
		{logging.StepTuples, s.tuples},
		{logging.StepConsentRecords, s.consentRecords},
		{logging.StepKeys, s.keys},
		{logging.StepChangelog, s.changelog},
	}
	report := make(Report, 0, len(steps))
	for _, each := range steps {
		step := Step{Name: each.name}
		if ctx.Err() == nil {
			each.run(ctx, now, &step)
		}
		s.log.Info(logging.EventSwept, logging.FieldStep, step.Name, logging.FieldDone, step.Done,
			logging.FieldFailed, step.Failed, logging.FieldFinished, step.Finished)
		report = append(report, step)
	}
	return report
}

// failed counts a failure of an item and logs its kind, and says whether the step
// goes on: a failure that is the pass's time running out is no failure of the
// item, and stops the step.
func (s *Sweeper) failed(ctx context.Context, step *Step, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	step.Failed++
	s.log.Error(logging.EventFailure, logging.FieldMoment, logging.MomentSweep, logging.FieldStep, step.Name,
		logging.FieldErrorKind, logging.ErrorKind(err))
	return true
}

// pages runs each on every item that choose gives, a page at a time past the last
// one seen, until a page comes short, ctx ends or a choice fails, and counts the
// items each says it did. It says whether it went to the end. key is an item's
// place in the order of the pages.
func pages[T any](ctx context.Context, s *Sweeper, step *Step, choose func(after string) ([]T, error),
	key func(T) string, each func(T) (bool, error)) bool {
	after := ""
	for {
		items, err := choose(after)
		if err != nil {
			s.failed(ctx, step, err)
			return false
		}
		for _, item := range items {
			if ctx.Err() != nil {
				return false
			}
			done, err := each(item)
			if err != nil {
				if !s.failed(ctx, step, err) {
					return false
				}
				continue
			}
			if done {
				step.Done++
			}
		}
		if len(items) < batch {
			return true
		}
		after = key(items[len(items)-1])
	}
}

// marks finishes the deletions of the accounts marked longer ago than a request
// may run: steps 2–4 (R21; architecture 3.10).
func (s *Sweeper) marks(ctx context.Context, now time.Time, step *Step) {
	before := now.Add(-s.settle)
	step.Finished = pages(ctx, s, step,
		func(after string) ([]store.AccountRef, error) {
			return store.MarkedBefore(ctx, s.db, before, after, batch)
		},
		func(ref store.AccountRef) string { return ref.ID },
		func(ref store.AccountRef) (bool, error) { return true, s.deleter.Finish(ctx, ref.ID, ref.UID) })
}

// idleAccounts deletes the accounts with no request for 12 months, as their owner
// would, with no fresh sign-in (П-9). One that came back since it was chosen is
// kept, and counts as nothing done.
func (s *Sweeper) idleAccounts(ctx context.Context, now time.Time, step *Step) {
	before := now.UTC().AddDate(-1, 0, 0)
	step.Finished = pages(ctx, s, step,
		func(after string) ([]store.AccountRef, error) {
			return store.IdleAccounts(ctx, s.db, before, after, batch)
		},
		func(ref store.AccountRef) string { return ref.ID },
		func(ref store.AccountRef) (bool, error) { return s.deleter.IdleAccount(ctx, ref.ID, ref.UID, before) })
}

// idleProfiles deletes the profiles with no activity for 12 months (П-9). One
// that came back since it was chosen is kept, and counts as nothing done; so does
// one whose account was marked or deleted since: it goes with the account.
func (s *Sweeper) idleProfiles(ctx context.Context, now time.Time, step *Step) {
	before := now.UTC().AddDate(-1, 0, 0)
	step.Finished = pages(ctx, s, step,
		func(after string) ([]store.ProfileRef, error) {
			return store.IdleProfiles(ctx, s.db, before, after, batch)
		},
		func(ref store.ProfileRef) string { return ref.ID },
		func(ref store.ProfileRef) (bool, error) {
			deleted, err := s.deleter.IdleProfile(ctx, ref.AccountID, ref.ID, before)
			if errors.Is(err, store.ErrAccountDeleting) || errors.Is(err, store.ErrNoAccount) {
				return false, nil
			}
			return deleted, err
		})
}

// users removes the users of the sign-in service that have no account of api
// and have neither signed in, refreshed a token nor been made for 30 days
// (R21, R38): one who signed in with no invite code, or again after a deletion.
func (s *Sweeper) users(ctx context.Context, now time.Time, step *Step) {
	before := now.Add(-usersTerm)
	for user, err := range s.signin.Users(ctx) {
		if err != nil {
			s.failed(ctx, step, err)
			return
		}
		if !latest(user).Before(before) {
			continue
		}
		if ctx.Err() != nil {
			return
		}
		removed, err := s.removeUser(ctx, user.UID)
		if err != nil {
			if !s.failed(ctx, step, err) {
				return
			}
			continue
		}
		if removed {
			step.Done++
		}
	}
	step.Finished = true
}

// removeUser removes the user unless they have an account, and says whether it
// did. The account is asked for right before the removal, not with the list: a
// list of many pages is minutes old by its end, and a user who came back in them
// and made an account keeps it.
func (s *Sweeper) removeUser(ctx context.Context, uid string) (bool, error) {
	have, err := store.HaveAccounts(ctx, s.db, []string{uid})
	if err != nil || have[uid] {
		return false, err
	}
	return true, s.signin.RemoveUser(ctx, uid)
}

// latest is the last time the user was seen: made, signed in or refreshed.
func latest(user authn.User) time.Time {
	last := user.Created
	for _, seen := range []time.Time{user.LastSignIn, user.LastRefresh} {
		if seen.After(last) {
			last = seen
		}
	}
	return last
}

// tuples deletes the dangling tuples: written longer ago than a request may run,
// whose profile is not there or is another account's (R21, R38). A younger one
// may be a profile's whose transaction is still open, written before its COMMIT.
func (s *Sweeper) tuples(ctx context.Context, now time.Time, step *Step) {
	before := now.Add(-s.settle)
	var chosen []authz.Owned
	drop := func() bool {
		if len(chosen) == 0 {
			return true
		}
		ids := make([]string, 0, len(chosen))
		for _, tuple := range chosen {
			ids = append(ids, tuple.ProfileID)
		}
		there, err := store.ProfilesThere(ctx, s.db, ids)
		if err != nil {
			s.failed(ctx, step, err)
			return false
		}
		dangling := map[string][]string{}
		for _, tuple := range chosen {
			if there[tuple.ProfileID] != tuple.AccountID {
				dangling[tuple.AccountID] = append(dangling[tuple.AccountID], tuple.ProfileID)
			}
		}
		for accountID, profiles := range dangling {
			if ctx.Err() != nil {
				return false
			}
			if err := s.rights.DeleteOwned(ctx, accountID, profiles); err != nil {
				if !s.failed(ctx, step, err) {
					return false
				}
				continue
			}
			step.Done += int64(len(profiles))
		}
		chosen = chosen[:0]
		return true
	}
	for tuple, err := range s.rights.Tuples(ctx) {
		if err != nil {
			s.failed(ctx, step, err)
			return
		}
		if !tuple.Written.Before(before) {
			continue
		}
		if chosen = append(chosen, tuple); len(chosen) == batch && !drop() {
			return
		}
	}
	step.Finished = drop()
}

// consentRecords erases the minimal records of consents for children 5 years
// after their account or profile was deleted, by the calendar of UTC, in which
// the date of deletion is written: a record of exactly 5 years is kept (R117
// spec; R21). The record holds no ID of the account
// or the profile, so the term runs from its own date of deletion.
func (s *Sweeper) consentRecords(ctx context.Context, now time.Time, step *Step) {
	dropped, err := store.DropConsentRecords(ctx, s.db, now.UTC().AddDate(-consentRecordYears, 0, 0))
	step.Done = dropped
	if err != nil {
		s.failed(ctx, step, err)
		return
	}
	step.Finished = true
}

// abandonedAttempts closes the attempts with no action for 7 days (R10). The
// words of their hints, and the evidence of one with wrong answers, are what the
// tasks that keep them add to the close (T37, T39).
func (s *Sweeper) abandonedAttempts(ctx context.Context, now time.Time, step *Step) {
	closed, err := store.CloseAbandonedAttempts(ctx, s.db, now.Add(-attemptsTerm), now)
	step.Done = closed
	if err != nil {
		s.failed(ctx, step, err)
		return
	}
	step.Finished = true
}

// abandonedRuns ends the lesson runs with no action for 30 days (R13).
func (s *Sweeper) abandonedRuns(ctx context.Context, now time.Time, step *Step) {
	ended, err := store.EndAbandonedRuns(ctx, s.db, now.Add(-runsTerm), now)
	step.Done = ended
	if err != nil {
		s.failed(ctx, step, err)
		return
	}
	step.Finished = true
}

// keys deletes the records of idempotency keys older than a day (R17).
func (s *Sweeper) keys(ctx context.Context, now time.Time, step *Step) {
	dropped, err := store.DropKeys(ctx, s.db, now.Add(-keysTerm))
	step.Done = dropped
	if err != nil {
		s.failed(ctx, step, err)
		return
	}
	step.Finished = true
}

// changelog deletes OpenFGA's changelog older than a day (R05; R27 canvas-api).
func (s *Sweeper) changelog(ctx context.Context, now time.Time, step *Step) {
	dropped, err := s.rights.DropChangelog(ctx, now.Add(-changelogTerm))
	step.Done = dropped
	if err != nil {
		s.failed(ctx, step, err)
		return
	}
	step.Finished = true
}
