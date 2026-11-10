package sweep

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/altnovatrail/api/internal/logging"
	"github.com/altnovatrail/api/internal/model"
	"github.com/altnovatrail/api/internal/packages"
	"github.com/altnovatrail/api/internal/store"
)

// unchecked is the reason the job closes an attempt and ends a lesson run with
// when the image no longer gives its package (R07): the schema's word for both.
const unchecked = store.RunUnchecked

// AfterJob runs the passes of the job after a rollout (R07, R51).
type AfterJob struct {
	job
}

// After is the job after a rollout on db: it closes "unchecked" the open
// attempts and lesson runs on the packages the image of set no longer gives —
// withdrawn, or out of the window — and writes the evidence the closed attempts
// owe (R49). It needs neither the rights nor the sign-in service.
func After(db DB, set *packages.Set, m *model.Model, log *slog.Logger) *AfterJob {
	return &AfterJob{job{db: db, model: m, gone: set.NotGiven(), log: log, moment: logging.MomentAfter}}
}

// Run runs one pass of the job after a rollout at now, until ctx ends, as the
// daily job runs its own: the steps of logging.AfterSteps, a line for each.
func (a *AfterJob) Run(ctx context.Context, now time.Time) Report {
	return a.pass(ctx, now, logging.AfterSteps, map[string]stepFunc{
		logging.StepGoneAttempts: a.goneAttempts,
		logging.StepGoneRuns:     a.goneRuns,
	})
}

// goneAttempts closes "unchecked" the open attempts on the packages the image no
// longer gives, one at a time, each in a transaction of its own that holds the
// rows as a request holds them — the account FOR KEY SHARE, the attempt's run,
// then the attempt — so that the job and a request on the same attempt wait for
// each other rather than lock each other out. A trigger deletes the words of
// its hints and ends its run (R47, R48), and the evidence the profile owes is
// written in the same transaction, as a close by a request writes it (R49): the
// attempt on a package out of the window gets none, its package being no longer
// in the image. One that a request closed meanwhile, or whose account or profile
// went, counts as nothing done.
func (j *job) goneAttempts(ctx context.Context, now time.Time, step *Step) {
	step.Finished = len(j.gone) == 0 || pages(ctx, j, step,
		func(after string) ([]store.OpenOn, error) {
			return store.OpenAttemptsOn(ctx, j.db, j.gone, after, batch)
		},
		func(o store.OpenOn) string { return o.ID },
		func(o store.OpenOn) (bool, error) {
			return j.closeGone(ctx, o, func(tx pgx.Tx) (bool, error) {
				if err := store.HoldAttemptRun(ctx, tx, o.ProfileID, o.ID); err != nil {
					return false, err
				}
				return j.closeAttempt(ctx, tx, o.ProfileID, o.ID, now)
			})
		})
}

// goneRuns ends "unchecked" the open lesson runs on the packages the image no
// longer gives, as goneAttempts closes attempts: the run held first, then its
// open attempt, if goneAttempts did not get to it, closed with it; a run whose
// attempt was closed otherwise meanwhile ends by itself. No engine's choice is
// made: the end is given to nobody, as that of a run OpenLesson ends for another
// choice (R50).
func (j *job) goneRuns(ctx context.Context, now time.Time, step *Step) {
	step.Finished = len(j.gone) == 0 || pages(ctx, j, step,
		func(after string) ([]store.OpenOn, error) {
			return store.OpenRunsOn(ctx, j.db, j.gone, after, batch)
		},
		func(o store.OpenOn) string { return o.ID },
		func(o store.OpenOn) (bool, error) {
			return j.closeGone(ctx, o, func(tx pgx.Tx) (bool, error) {
				run, err := store.HoldLessonRun(ctx, tx, o.ProfileID, o.ID)
				if err != nil || run.EndReason != nil {
					return false, err
				}
				attempt, open, err := store.RunAttempt(ctx, tx, o.ProfileID, o.ID)
				if err != nil {
					return false, err
				}
				if open {
					// The trigger ends the run with its attempt.
					if closed, err := j.closeAttempt(ctx, tx, o.ProfileID, attempt.ID, now); err != nil || closed {
						return closed, err
					}
				}
				return true, store.EndLessonRun(ctx, tx, o.ProfileID, o.ID, unchecked, now)
			})
		})
}

// closeAttempt holds the profile's attempt and, if it is still open, closes it
// "unchecked" at now and writes the evidence the profile owes, in tx; it says
// whether it closed it. The daily job closes abandoned attempts with no hold of
// their run, so an attempt seen open is held and read again before its close.
func (j *job) closeAttempt(ctx context.Context, tx pgx.Tx, profileID, attemptID string, now time.Time) (bool, error) {
	row, err := store.HoldAttempt(ctx, tx, profileID, attemptID)
	if err != nil || row.CloseReason != nil {
		return false, err
	}
	if err := store.CloseAttempt(ctx, tx, profileID, attemptID, unchecked, now); err != nil {
		return false, err
	}
	_, err = j.model.Settle(ctx, tx, profileID, now)
	return err == nil, err
}

// closeGone runs fn under the account of o and says whether it closed what it
// was given: an account marked or gone, a profile or a row gone since o was
// chosen, is nothing done and no failure.
func (j *job) closeGone(ctx context.Context, o store.OpenOn, fn func(tx pgx.Tx) (bool, error)) (bool, error) {
	closed := false
	err := store.WriteUnderAccount(ctx, j.db, o.AccountID, func(tx pgx.Tx) error {
		var err error
		closed, err = fn(tx)
		return err
	})
	switch {
	case errors.Is(err, store.ErrAccountDeleting), errors.Is(err, store.ErrNoAccount),
		errors.Is(err, store.ErrNoProfile), errors.Is(err, store.ErrNotFound):
		return false, nil
	case err != nil:
		return false, err
	}
	return closed, nil
}
