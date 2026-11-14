package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/altnovatrail/spec/types/exportv2"
	"github.com/jackc/pgx/v5"
)

// The export of the data of a profile or an account, as the database keeps it
// (R52; R135 spec): the files of api.zip by spec's part export/, of whatever
// this database holds of them, read in the snapshot of one transaction. What
// the record of protobuf holds — the evidence, the state, the observation — is
// given as its bytes, which the package export writes as JSON; what the package
// of a module tells of an attempt, it adds.

// ExportFormat is the integer of export/VERSION of spec's tag (R135 spec).
const ExportFormat = 2

// timeOf is t in the form of the export: RFC 3339 in UTC.
func timeOf(t time.Time) exportv2.Time { return exportv2.Time(t.UTC().Format(time.RFC3339)) }

func timeOrNil(t *time.Time) *exportv2.Time {
	if t == nil {
		return nil
	}
	v := timeOf(*t)
	return &v
}

func dayOf(t time.Time) exportv2.Day { return exportv2.Day(t.UTC().Format("2006-01-02")) }

// ExportAccount is the account's part of the export: its fields that are no
// secret and every consent of it, in full, and the IDs of its profiles.
func ExportAccount(ctx context.Context, tx pgx.Tx, accountID string) (exportv2.Account, error) {
	a := exportv2.Account{Format: ExportFormat, AccountID: exportv2.ID(accountID), ProfileIds: []exportv2.ID{},
		Consents: []exportv2.Consent{}, QuotaCounters: []exportv2.QuotaCounter{}}
	var stated, lastOn time.Time
	var offset *int64
	var dayEnds *time.Time
	err := tx.QueryRow(ctx, `SELECT adult_stated_at, tariff, utc_offset_minutes, day_ends_at, last_request_on
		FROM accounts WHERE id = $1`, accountID).Scan(&stated, &a.Tariff, &offset, &dayEnds, &lastOn)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return exportv2.Account{}, ErrNoAccount
	case err != nil:
		return exportv2.Account{}, fmt.Errorf("store: read the account: %w", err)
	}
	a.AdultStatedAt, a.UtcOffsetMinutes, a.DayEndsAt, a.LastRequestOn = timeOf(stated), offset, timeOrNil(dayEnds), dayOf(lastOn)
	rows, err := tx.Query(ctx, `SELECT id FROM profiles WHERE account_id = $1 ORDER BY owner DESC, created_at, id`, accountID)
	if err != nil {
		return exportv2.Account{}, fmt.Errorf("store: read the profiles: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return exportv2.Account{}, fmt.Errorf("store: read the profiles: %w", err)
	}
	for _, id := range ids {
		a.ProfileIds = append(a.ProfileIds, exportv2.ID(id))
	}
	if a.Consents, err = exportConsents(ctx, tx, `c.account_id = $1`, accountID); err != nil {
		return exportv2.Account{}, err
	}
	if a.QuotaCounters, err = exportCounters(ctx, tx, "account_quota", "account_id", accountID); err != nil {
		return exportv2.Account{}, err
	}
	return a, nil
}

// exportConsents is the consents the condition on c matches, in full, by when
// they were asked for: the owner's with the texts the account was created on, a
// parent's with parent-consent and the learner's with teen-consent, each by the
// version the consent names.
func exportConsents(ctx context.Context, tx pgx.Tx, cond string, args ...any) ([]exportv2.Consent, error) {
	rows, err := tx.Query(ctx, `SELECT c.id, c.giver, c.state, c.request_code, c.form_state, c.age_category, c.country,
			c.method, c.requested_at, c.confirmed_at, c.text_version, c.account_id
		FROM consents c WHERE `+cond+` ORDER BY c.requested_at, c.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: read the consents: %w", err)
	}
	type row struct {
		c         exportv2.Consent
		version   *int32
		accountID string
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var out row
		var requested time.Time
		var confirmed *time.Time
		var form *string
		err := r.Scan(&out.c.ConsentID, &out.c.Giver, &out.c.State, &out.c.RequestCode, &form, &out.c.AgeCategory,
			&out.c.Country, &out.c.Method, &requested, &confirmed, &out.version, &out.accountID)
		out.c.RequestedAt, out.c.ConfirmedAt = timeOf(requested), timeOrNil(confirmed)
		if form != nil {
			f := exportv2.ConsentForm(*form)
			out.c.Form = &f
		}
		return out, err
	})
	if err != nil {
		return nil, fmt.Errorf("store: read the consents: %w", err)
	}
	consents := make([]exportv2.Consent, 0, len(list))
	for _, r := range list {
		r.c.Texts = []exportv2.TextRef{}
		switch {
		case r.c.Giver == exportv2.ConsentGiverOwner:
			texts, err := tx.Query(ctx, `SELECT text_id, version FROM account_texts WHERE account_id = $1 ORDER BY text_id`, r.accountID)
			if err != nil {
				return nil, fmt.Errorf("store: read the texts accepted: %w", err)
			}
			refs, err := pgx.CollectRows(texts, func(t pgx.CollectableRow) (exportv2.TextRef, error) {
				var ref exportv2.TextRef
				err := t.Scan(&ref.ID, &ref.Version)
				return ref, err
			})
			if err != nil {
				return nil, fmt.Errorf("store: read the texts accepted: %w", err)
			}
			r.c.Texts = append(r.c.Texts, refs...)
		case r.version != nil && r.c.Giver == exportv2.ConsentGiverParent:
			r.c.Texts = append(r.c.Texts, exportv2.TextRef{ID: "parent-consent", Version: int64(*r.version)})
		case r.version != nil:
			r.c.Texts = append(r.c.Texts, exportv2.TextRef{ID: "teen-consent", Version: int64(*r.version)})
		}
		consents = append(consents, r.c)
	}
	return consents, nil
}

// ExportedAttempt is an attempt as the export reads it, with the rows the
// package of its module turns into what it tells: its task, its instance and
// the bytes of the answers of its submissions.
type ExportedAttempt struct {
	exportv2.Attempt
	Owed Owed
	// LeadingQuestionGiven says the task's leading question was given on
	// request, which its package words.
	LeadingQuestionGiven bool
}

// ExportedProfile is a profile's part of the export as the database holds it,
// with the bytes of its records of protobuf: each piece of evidence, by the
// order of the log, the state, each submission's observation, by the attempt
// and the number of the submission, and each stored work, newest first.
type ExportedProfile struct {
	Profile      exportv2.Profile
	Attempts     []ExportedAttempt
	Evidence     [][]byte
	State        []byte
	Observations map[string]map[int64][]byte
	Works        []ExportedWork
}

// ExportedWork is a stored work as the export names it, with the bytes it keeps:
// the contract's StoredWork, which the package export writes.
type ExportedWork struct {
	Work  exportv2.Work
	Bytes []byte
}

// ExportProfile is the profile's part of the export, of the account's profile,
// or ErrNoProfile.
func ExportProfile(ctx context.Context, tx pgx.Tx, accountID, profileID string) (ExportedProfile, error) {
	p, err := ProfileWithConsent(ctx, tx, accountID, profileID)
	if err != nil {
		return ExportedProfile{}, err
	}
	var activeOn time.Time
	if err := tx.QueryRow(ctx, `SELECT active_on FROM profiles WHERE id = $1`, profileID).Scan(&activeOn); err != nil {
		return ExportedProfile{}, fmt.Errorf("store: read the profile: %w", err)
	}
	out := exportv2.Profile{
		Format: ExportFormat, ProfileID: exportv2.ID(p.ID), Owner: p.Owner, Pseudonym: p.Pseudonym, Avatar: p.Avatar,
		AgeCategory: exportv2.AgeCategory(p.AgeCategory), Country: p.Country,
		PresentationStyle: exportv2.ProfilePresentationStyle{Voicing: exportv2.ProfilePresentationStyleVoicing{
			Enabled: p.VoicingEnabled, Speed: exportv2.ProfilePresentationStyleVoicingSpeed(p.VoicingSpeed)}},
		DefaultMode: exportv2.ProfileDefaultMode(p.DefaultMode), InterfaceLanguage: p.InterfaceLanguage,
		AskBeforeShowing: p.AskBeforeShowing, ActiveOn: dayOf(activeOn), WorkStorage: p.WorkStorage,
		ConnectedModules: []exportv2.ConnectedModule{}, LessonRuns: []exportv2.LessonRun{}, Attempts: []exportv2.Attempt{},
		Choices: []exportv2.Choice{}, Evidence: []exportv2.EvidenceEntry{}, Works: []exportv2.Work{},
		StorageConsents: []exportv2.StorageConsent{}, ProblemReports: []exportv2.ProblemReport{},
		QuotaCounters: []exportv2.QuotaCounter{},
	}
	if p.AgeStatedOn != nil {
		d := dayOf(*p.AgeStatedOn)
		out.AgeStatedOn = &d
	}
	if p.Grade != nil {
		g := int64(*p.Grade)
		out.Grade = &g
	}
	if p.Consent.Applied != 0 {
		applied := int64(p.Consent.Applied)
		out.ConsentAge = &applied
	}
	consents, err := exportConsents(ctx, tx, `c.profile_id = $1`, profileID)
	if err != nil {
		return ExportedProfile{}, err
	}
	if len(consents) == 1 {
		out.Consent = consents[0]
	}
	e := ExportedProfile{Observations: map[string]map[int64][]byte{}}
	steps := []func() error{
		func() error { return exportModules(ctx, tx, profileID, &out) },
		func() error { return exportRuns(ctx, tx, profileID, &out) },
		func() error { return exportChoices(ctx, tx, profileID, &out) },
		func() error { return exportStorage(ctx, tx, profileID, &out) },
		func() error { return exportReports(ctx, tx, profileID, &out) },
		func() error { return exportAttempts(ctx, tx, profileID, &e) },
		func() error { return exportJournal(ctx, tx, profileID, &out, &e) },
		func() error { return exportWorks(ctx, tx, profileID, &e) },
		func() (err error) {
			out.QuotaCounters, err = exportCounters(ctx, tx, "profile_quota", "profile_id", profileID)
			return err
		},
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return ExportedProfile{}, err
		}
	}
	e.Profile = out
	return e, nil
}

func exportModules(ctx context.Context, tx pgx.Tx, profileID string, out *exportv2.Profile) error {
	rows, err := tx.Query(ctx, `SELECT module_id, connected_at FROM connected_modules WHERE profile_id = $1
		ORDER BY connected_at, module_id`, profileID)
	if err != nil {
		return fmt.Errorf("store: read the modules: %w", err)
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (exportv2.ConnectedModule, error) {
		var m exportv2.ConnectedModule
		var at time.Time
		err := r.Scan(&m.ModuleID, &at)
		m.ConnectedAt = timeOf(at)
		return m, err
	})
	if err != nil {
		return fmt.Errorf("store: read the modules: %w", err)
	}
	out.ConnectedModules = append(out.ConnectedModules, list...)
	return nil
}

func exportRuns(ctx context.Context, tx pgx.Tx, profileID string, out *exportv2.Profile) error {
	rows, err := tx.Query(ctx, `SELECT id, lesson_id, module_id, module_version, package_digest, choice_id, started_at,
			last_action_at, end_reason, ended_at
		FROM lesson_runs WHERE profile_id = $1 ORDER BY started_at, id`, profileID)
	if err != nil {
		return fmt.Errorf("store: read the lesson runs: %w", err)
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (exportv2.LessonRun, error) {
		var run exportv2.LessonRun
		var begun, last time.Time
		var ended *time.Time
		err := r.Scan(&run.LessonRunID, &run.Lesson, &run.ModuleID, &run.ModuleVersion, &run.PackageDigest, &run.ChoiceID,
			&begun, &last, &run.EndReason, &ended)
		run.BegunAt, run.LastActionAt, run.EndedAt = timeOf(begun), timeOf(last), timeOrNil(ended)
		return run, err
	})
	if err != nil {
		return fmt.Errorf("store: read the lesson runs: %w", err)
	}
	out.LessonRuns = append(out.LessonRuns, list...)
	return nil
}

// reasonCodes is the reason code of a choice, by its number in the contract,
// as the export names it.
var reasonCodes = map[int16]exportv2.ChoiceReasonCode{
	1: exportv2.ChoiceReasonCodeConsolidation, 2: exportv2.ChoiceReasonCodeNewSkill, 3: exportv2.ChoiceReasonCodeQueue,
	4: exportv2.ChoiceReasonCodeRepetition, 5: exportv2.ChoiceReasonCodeProbe, 6: exportv2.ChoiceReasonCodeSkillChosen,
	7: exportv2.ChoiceReasonCodeAuthorOrder, 8: exportv2.ChoiceReasonCodeOpenRun, 9: exportv2.ChoiceReasonCodeRotation,
}

// exportChoices is the engine's choices of the profile that gave a task or a
// lesson and were written with their reason code: a choice of nothing is not
// exported, and one written before the codes has none to give (R50).
func exportChoices(ctx context.Context, tx pgx.Tx, profileID string, out *exportv2.Profile) error {
	rows, err := tx.Query(ctx, `SELECT id, kind, chosen_at, module_id, task_id, lesson_id, reason_code, engine_version,
			learner_model_version, coalesce(digests, '{}'), coalesce(engine_digests, '{}'), modules_release, last_seq,
			grade, coalesce(pool_limited, false), map_skill, age_category
		FROM engine_choices WHERE profile_id = $1 AND kind IN ('task', 'lesson') AND reason_code IS NOT NULL
		ORDER BY chosen_at, id`, profileID)
	if err != nil {
		return fmt.Errorf("store: read the choices: %w", err)
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (exportv2.Choice, error) {
		var c exportv2.Choice
		var at time.Time
		var module, task, lesson *string
		var code int16
		var digests, engineDigests []string
		var release *string
		var lastSeq *int64
		var grade *int16
		var category *string
		err := r.Scan(&c.ChoiceID, &c.Kind, &at, &module, &task, &lesson, &code, &c.EngineVersion, &c.LearnerModelVersion,
			&digests, &engineDigests, &release, &lastSeq, &grade, &c.PoolWithoutAi, &c.Skill, &category)
		c.ChosenAt, c.ReasonCode = timeOf(at), reasonCodes[code]
		if module != nil && task != nil {
			full := *module + "/task/" + *task
			c.Task = &full
		}
		if module != nil && lesson != nil {
			full := *module + "/lesson/" + *lesson
			c.Lesson = &full
		}
		c.PackageDigests = []exportv2.ChoicePackageDigest{}
		engine := map[string]bool{}
		for _, d := range engineDigests {
			engine[d] = true
		}
		for _, d := range digests {
			c.PackageDigests = append(c.PackageDigests, exportv2.ChoicePackageDigest{PackageDigest: d, EngineModule: engine[d]})
		}
		if release != nil {
			c.ModulesRelease = *release
		}
		if lastSeq != nil {
			c.LastEvidenceSeq = *lastSeq
		}
		if grade != nil {
			g := int64(*grade)
			c.Grade = &g
		}
		if category != nil {
			c.AgeCategory = exportv2.AgeCategory(*category)
		}
		return c, err
	})
	if err != nil {
		return fmt.Errorf("store: read the choices: %w", err)
	}
	out.Choices = append(out.Choices, list...)
	return nil
}

func exportStorage(ctx context.Context, tx pgx.Tx, profileID string, out *exportv2.Profile) error {
	rows, err := tx.Query(ctx, `SELECT giver, text_version, on_at, off_at FROM work_storage_consents
		WHERE profile_id = $1 ORDER BY on_at, id`, profileID)
	if err != nil {
		return fmt.Errorf("store: read the storage of works: %w", err)
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (exportv2.StorageConsent, error) {
		var s exportv2.StorageConsent
		var version *int32
		var on time.Time
		var off *time.Time
		err := r.Scan(&s.Giver, &version, &on, &off)
		s.GivenAt, s.WithdrawnAt, s.Texts = timeOf(on), timeOrNil(off), []exportv2.TextRef{}
		if version != nil {
			s.Texts = append(s.Texts, exportv2.TextRef{ID: "storage-consent", Version: int64(*version)})
		}
		return s, err
	})
	if err != nil {
		return fmt.Errorf("store: read the storage of works: %w", err)
	}
	out.StorageConsents = append(out.StorageConsents, list...)
	return nil
}

// exportWorks reads the profile's stored works, newest first, with their bytes,
// which the package export writes: there are none while the storage is off.
func exportWorks(ctx context.Context, tx pgx.Tx, profileID string, e *ExportedProfile) error {
	rows, err := tx.Query(ctx, `SELECT attempt_id, kind, stored_at, work FROM works WHERE profile_id = $1
		ORDER BY stored_at DESC, attempt_id`, profileID)
	if err != nil {
		return fmt.Errorf("store: read the works: %w", err)
	}
	works, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ExportedWork, error) {
		var w ExportedWork
		var at time.Time
		err := r.Scan(&w.Work.AttemptID, &w.Work.Kind, &at, &w.Bytes)
		w.Work.StoredAt = timeOf(at)
		return w, err
	})
	if err != nil {
		return fmt.Errorf("store: read the works: %w", err)
	}
	e.Works = works
	return nil
}

// ReviewWithin is the term the operator reviews a problem report in (R123
// spec).
const ReviewWithin = 3 * 24 * time.Hour

func exportReports(ctx context.Context, tx pgx.Tx, profileID string, out *exportv2.Profile) error {
	rows, err := tx.Query(ctx, `SELECT id, attempt_id, reply, rung, words, kind, text, created_at, reviewed_at
		FROM problem_reports WHERE profile_id = $1 AND attempt_id IS NOT NULL ORDER BY created_at, id`, profileID)
	if err != nil {
		return fmt.Errorf("store: read the reports: %w", err)
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (exportv2.ProblemReport, error) {
		var p exportv2.ProblemReport
		var rung *int16
		var at time.Time
		var reviewed *time.Time
		err := r.Scan(&p.ReportID, &p.AttemptID, &p.Reply, &rung, &p.Words, &p.Kind, &p.Text, &at, &reviewed)
		p.ReportedAt, p.ReviewBy, p.ReviewedAt = timeOf(at), timeOf(at.Add(ReviewWithin)), timeOrNil(reviewed)
		if rung != nil {
			v := int64(*rung)
			p.Rung = &v
		}
		return p, err
	})
	if err != nil {
		return fmt.Errorf("store: read the reports: %w", err)
	}
	out.ProblemReports = append(out.ProblemReports, list...)
	return nil
}

// exportAttempts is the profile's attempts, without the seed, each with its
// rungs and submissions, and the rows its package turns into what it tells.
func exportAttempts(ctx context.Context, tx pgx.Tx, profileID string, e *ExportedProfile) error {
	rows, err := tx.Query(ctx, `SELECT id, package_digest, module_id, module_version, module_format_version,
			module_kit_version, task_id, instance, choice_id, lesson_run_id, segment, previous_attempt_id, started_at,
			last_action_at, close_reason, closed_at, wrong_answers, step_techniques_at, requested_techniques_at,
			leading_question_at
		FROM attempts WHERE profile_id = $1 ORDER BY started_at, id`, profileID)
	if err != nil {
		return fmt.Errorf("store: read the attempts: %w", err)
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ExportedAttempt, error) {
		var a ExportedAttempt
		var instance, segment *int
		var run *string
		var begun, last time.Time
		var closed, stepAt, requestedAt, questionAt *time.Time
		var closeReason *string
		var task string
		err := r.Scan(&a.AttemptID, &a.PackageDigest, &a.ModuleID, &a.ModuleVersion, &a.ModuleFormatVersion,
			&a.ModuleKitVersion, &task, &instance, &a.ChoiceID, &run, &segment, &a.PreviousAttemptID, &begun, &last,
			&closeReason, &closed, &a.WrongAnswers, &stepAt, &requestedAt, &questionAt)
		a.Task = a.ModuleID + "/task/" + task
		a.BegunAt, a.LastActionAt, a.ClosedAt = timeOf(begun), timeOf(last), timeOrNil(closed)
		a.StepTechniquesAt = timeOrNil(stepAt)
		if instance != nil {
			v := int64(*instance)
			a.Instance = &v
		}
		if run != nil && segment != nil {
			a.Segment = &exportv2.AttemptSegment{LessonRunID: exportv2.ID(*run), Segment: int64(*segment)}
		}
		if closeReason != nil {
			reason := exportv2.AttemptCloseReason(*closeReason)
			a.CloseReason = &reason
			// The solution shown closes the attempt (R10).
			if reason == exportv2.AttemptCloseReasonSolutionShown {
				a.SolutionShownAt = a.ClosedAt
			}
		}
		a.StepTechniqueIds, a.RequestedTechniqueIds, a.Traps = []string{}, []string{}, []string{}
		a.Rungs, a.Submissions = []exportv2.Rung{}, []exportv2.Submission{}
		a.Owed = Owed{ID: string(a.AttemptID), PackageDigest: a.PackageDigest, ModuleID: a.ModuleID, TaskID: task,
			Instance: instance, WrongAnswers: int(a.WrongAnswers), LastActionAt: last, StepTechniques: stepAt != nil,
			RequestedTechniques: requestedAt != nil}
		if closeReason != nil {
			a.Owed.CloseReason = *closeReason
		}
		a.LeadingQuestionGiven = questionAt != nil
		return a, err
	})
	if err != nil {
		return fmt.Errorf("store: read the attempts: %w", err)
	}
	if err := exportAttemptRows(ctx, tx, profileID, list, e); err != nil {
		return err
	}
	e.Attempts = list
	return nil
}

// exportAttemptRows reads the rungs and the submissions of the profile's
// attempts, in two queries, and gives each attempt of list its own.
func exportAttemptRows(ctx context.Context, tx pgx.Tx, profileID string, list []ExportedAttempt, e *ExportedProfile) error {
	at := map[exportv2.ID]*ExportedAttempt{}
	for i := range list {
		at[list[i].AttemptID] = &list[i]
	}
	rows, err := tx.Query(ctx, `SELECT attempt_id, rung, source, words, explained_at IS NOT NULL, explained_source
		FROM attempt_rungs WHERE profile_id = $1 ORDER BY attempt_id, rung`, profileID)
	if err != nil {
		return fmt.Errorf("store: read the rungs: %w", err)
	}
	type rung struct {
		attempt exportv2.ID
		exportv2.Rung
	}
	rungs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (rung, error) {
		var x rung
		err := r.Scan(&x.attempt, &x.Rung.Rung, &x.Source, &x.Words, &x.ExplainedDifferently, &x.ExplainedDifferentlySource)
		return x, err
	})
	if err != nil {
		return fmt.Errorf("store: read the rungs: %w", err)
	}
	for _, x := range rungs {
		if a := at[x.attempt]; a != nil {
			a.Rungs = append(a.Rungs, x.Rung)
		}
	}
	rows, err = tx.Query(ctx, `SELECT attempt_id, number, submitted_at, answer, observation FROM submissions
		WHERE profile_id = $1 ORDER BY attempt_id, number`, profileID)
	if err != nil {
		return fmt.Errorf("store: read the submissions: %w", err)
	}
	type submission struct {
		attempt     exportv2.ID
		number      int64
		at          time.Time
		answer      []byte
		observation []byte
	}
	subs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (submission, error) {
		var s submission
		err := r.Scan(&s.attempt, &s.number, &s.at, &s.answer, &s.observation)
		return s, err
	})
	if err != nil {
		return fmt.Errorf("store: read the submissions: %w", err)
	}
	for _, s := range subs {
		a := at[s.attempt]
		if a == nil {
			continue
		}
		a.Submissions = append(a.Submissions, exportv2.Submission{Number: s.number, SubmittedAt: timeOf(s.at)})
		a.Owed.Answers = append(a.Owed.Answers, s.answer)
		if s.observation != nil {
			if e.Observations[string(s.attempt)] == nil {
				e.Observations[string(s.attempt)] = map[int64][]byte{}
			}
			e.Observations[string(s.attempt)][s.number] = s.observation
		}
	}
	return nil
}

// exportJournal is the profile's log of evidence, in its order, and its state
// with the mark it was computed on.
func exportJournal(ctx context.Context, tx pgx.Tx, profileID string, out *exportv2.Profile, e *ExportedProfile) error {
	rows, err := tx.Query(ctx, `SELECT seq, attempt_id, package_digest, module_id, recorded_at, evidence FROM evidence
		WHERE profile_id = $1 ORDER BY seq`, profileID)
	if err != nil {
		return fmt.Errorf("store: read the evidence: %w", err)
	}
	type entry struct {
		exportv2.EvidenceEntry
		bytes []byte
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (entry, error) {
		var x entry
		var at time.Time
		err := r.Scan(&x.Seq, &x.AttemptID, &x.PackageDigest, &x.ModuleID, &at, &x.bytes)
		x.RecordedAt = timeOf(at)
		return x, err
	})
	if err != nil {
		return fmt.Errorf("store: read the evidence: %w", err)
	}
	for _, x := range list {
		out.Evidence = append(out.Evidence, x.EvidenceEntry)
		e.Evidence = append(e.Evidence, x.bytes)
	}
	var release, window, version *string
	err = tx.QueryRow(ctx, `SELECT state, learner_model_version, modules_release, format_window, last_seq
		FROM skill_states WHERE profile_id = $1`, profileID).Scan(&e.State, &version, &release, &window, &out.State.LastSeq)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return fmt.Errorf("store: read the state: %w", err)
	}
	for _, s := range []struct {
		from *string
		into *string
	}{{version, &out.State.LearnerModelVersion}, {release, &out.State.ModulesRelease}, {window, &out.State.FormatWindow}} {
		if s.from != nil {
			*s.into = *s.from
		}
	}
	out.State.Modules = []exportv2.StateEntryModule{}
	return nil
}

// exportCounters is every counter of the quota the table keeps of the owner of
// column, by the beginning of its day (R12): spent and in reserve, in whole
// micro-dollars, as the database keeps them.
func exportCounters(ctx context.Context, tx pgx.Tx, table, column, id string) ([]exportv2.QuotaCounter, error) {
	rows, err := tx.Query(ctx, `SELECT day_start, spent_micro_usd, reserved_micro_usd FROM `+table+`
		WHERE `+column+` = $1 ORDER BY day_start`, id)
	if err != nil {
		return nil, fmt.Errorf("store: read the counters of the quota: %w", err)
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (exportv2.QuotaCounter, error) {
		var c exportv2.QuotaCounter
		var start time.Time
		err := r.Scan(&start, &c.SpentMicroUsd, &c.ReservedMicroUsd)
		c.DayStart = timeOf(start)
		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("store: read the counters of the quota: %w", err)
	}
	return append([]exportv2.QuotaCounter{}, list...), nil
}
