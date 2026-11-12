package store_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/altnovatrail/spec/types/exportv1"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/altnovatrail/api/internal/store"
	"github.com/altnovatrail/api/internal/store/storetest"
)

// rich is an account with its owner's profile and a child's consent, the
// texts the account was made on, and a profile with every record the export
// reads: a connected module, a choice, an attempt with a rung and submissions,
// one with an observation, its evidence and state, a lesson run, the storage of
// works and a problem report.
type rich struct {
	pool                      *pgxpool.Pool
	account, profile, attempt string
}

func newRich(t *testing.T) rich {
	t.Helper()
	pool := storetest.DB(t)
	ctx := t.Context()
	r := rich{pool: pool}
	if err := pool.QueryRow(ctx, `INSERT INTO accounts (auth_uid, last_request_on, adult_stated_at, tariff, utc_offset_minutes, day_ends_at)
		VALUES ('uid-rich', now(), now(), 'closed_slice', 60, now()) RETURNING id`).Scan(&r.account); err != nil {
		t.Fatal(err)
	}
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := store.AcceptTexts(ctx, tx, r.account, []store.TextRef{{ID: "privacy-policy", Version: 2}, {ID: "terms-of-use", Version: 1}}, time.Now()); err != nil {
			return err
		}
		grade := int16(4)
		p, err := store.CreateOwnerProfile(ctx, tx, r.account, store.Profile{Pseudonym: "Owl", Country: "US", VoicingSpeed: "normal",
			DefaultMode: "by_steps", Grade: &grade})
		r.profile = p.ID
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO consents (account_id, subject, state, age_category, country, giver, applied, text_version)
		VALUES ($1, 'child', 'waiting', 'under_13', 'US', 'parent', 13, 1)`, r.account)
	exec(`INSERT INTO consents (account_id, subject, state, age_category, country, giver, applied, text_version)
		VALUES ($1, 'child', 'waiting', 'from_consent_to_18', 'US', 'learner', 13, 1)`, r.account)
	exec(`INSERT INTO connected_modules (profile_id, module_id) VALUES ($1, 'example/m')`, r.profile)
	var choice, run string
	if err := pool.QueryRow(ctx, `INSERT INTO engine_choices (profile_id, kind, module_id, task_id, reason_code, engine_version,
			learner_model_version, modules_release, format_window, last_seq, digests, engine_digests, age_category, pool_limited,
			grade, map_skill)
		VALUES ($1, 'task', 'example/m', 't1', 9, 'v0.1.6', 'v0.1.4', 'v0.1.0', '0.4', 3, ARRAY[$2, $3], ARRAY[$2], 'adult', true,
			4, 'example/m/skill/s')
		RETURNING id`, r.profile, digest, "sha256:"+strings.Repeat("b", 64)).Scan(&choice); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO attempts (profile_id, package_digest, module_id, module_version, module_format_version,
			module_kit_version, task_id, seed, instance, choice_id, wrong_answers, step_techniques_at, requested_techniques_at, leading_question_at)
		VALUES ($1, $2, 'example/m', '0.1.0', '0.4', 'v0.3.0', 't1', 7, 0, $3, 1, now(), now(), now()) RETURNING id`,
		r.profile, digest, choice).Scan(&r.attempt); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO submissions (profile_id, attempt_id, number, answer, observation) VALUES ($1, $2, 1, '\x01', '\x02'), ($1, $2, 2, '\x03', NULL)`,
		r.profile, r.attempt)
	exec(`INSERT INTO attempt_rungs (profile_id, attempt_id, rung, source, explained_at, explained_source)
		VALUES ($1, $2, 1, 'model', now(), 'fallback')`, r.profile, r.attempt)
	exec(`UPDATE attempts SET close_reason = 'solution_shown', closed_at = now() WHERE id = $1`, r.attempt)
	exec(`INSERT INTO evidence (profile_id, seq, attempt_id, package_digest, module_id, evidence) VALUES ($1, 1, $2, $3, 'example/m', '\x04')`,
		r.profile, r.attempt, digest)
	exec(`UPDATE skill_states SET state = '\x05', learner_model_version = 'v0.1.4', modules_release = 'v0.1.0', format_window = '0.4',
		last_seq = 1 WHERE profile_id = $1`, r.profile)
	if err := pool.QueryRow(ctx, `WITH c AS (INSERT INTO engine_choices (profile_id, kind, module_id, lesson_id) VALUES ($1, 'lesson', 'example/m', 'l1')
			RETURNING id)
		INSERT INTO lesson_runs (profile_id, lesson_id, module_id, module_version, package_digest, choice_id, end_reason, ended_at)
		SELECT $1, 'example/m/lesson/l1', 'example/m', '0.1.0', $2, id, 'passed', now() FROM c RETURNING id`, r.profile, digest).Scan(&run); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO work_storage_consents (profile_id, giver, text_version, on_at, off_at) VALUES ($1, 'owner', 1, now(), now())`, r.profile)
	exec(`INSERT INTO account_quota (account_id, day_start, day_end, spent_micro_usd, reserved_micro_usd)
		VALUES ($1, '2026-11-12T05:00:00Z', '2026-11-13T05:00:00Z', 21000, 9343)`, r.account)
	exec(`INSERT INTO profile_quota (profile_id, day_start, day_end, spent_micro_usd) VALUES ($1, '2026-11-12T05:00:00Z', '2026-11-13T05:00:00Z', 21000)`,
		r.profile)
	exec(`INSERT INTO problem_reports (account_id, profile_id, attempt_id, reply, rung, words, kind, text, created_at, reviewed_at)
		VALUES ($1, $2, $3, 'rung', 1, 'Words.', 'unclear', '', now(), now())`, r.account, r.profile, r.attempt)
	return r
}

// The export reads a profile and its account in full, as the files of api.zip
// hold them, with the bytes of the records of protobuf beside them (R135 spec).
func TestTheExportReadsAProfileAndItsAccount(t *testing.T) {
	t.Parallel()

	r := newRich(t)
	var e store.ExportedProfile
	var a exportv1.Account
	err := pgx.BeginFunc(t.Context(), r.pool, func(tx pgx.Tx) error {
		var err error
		if e, err = store.ExportProfile(t.Context(), tx, r.account, r.profile); err != nil {
			return err
		}
		a, err = store.ExportAccount(t.Context(), tx, r.account)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	p := e.Profile
	if p.Format != store.ExportFormat || p.ProfileID != exportv1.ID(r.profile) || !p.Owner || p.Grade == nil || *p.Grade != 4 ||
		p.Consent.Giver != exportv1.ConsentGiverOwner || len(p.Consent.Texts) != 2 {
		t.Errorf("the profile = %+v", p)
	}
	if len(p.ConnectedModules) != 1 || len(p.LessonRuns) != 1 || p.LessonRuns[0].EndReason == nil || len(p.StorageConsents) != 1 ||
		p.StorageConsents[0].WithdrawnAt == nil || len(p.ProblemReports) != 1 || p.ProblemReports[0].Rung == nil || p.ProblemReports[0].ReviewedAt == nil {
		t.Errorf("the records of the profile = %+v", p)
	}
	if len(p.Choices) != 1 || p.Choices[0].ReasonCode != exportv1.ChoiceReasonCodeRotation || p.Choices[0].Task == nil ||
		*p.Choices[0].Task != "example/m/task/t1" || len(p.Choices[0].PackageDigests) != 2 || !p.Choices[0].PackageDigests[0].EngineModule ||
		p.Choices[0].Grade == nil || !p.Choices[0].PoolWithoutAi || p.Choices[0].Skill == nil {
		t.Errorf("the choices = %+v", p.Choices)
	}
	if len(e.Attempts) != 1 {
		t.Fatalf("attempts = %+v", e.Attempts)
	}
	at := e.Attempts[0]
	if at.Task != "example/m/task/t1" || at.Instance == nil || at.SolutionShownAt == nil || at.StepTechniquesAt == nil ||
		!at.LeadingQuestionGiven || len(at.Rungs) != 1 || !at.Rungs[0].ExplainedDifferently || len(at.Submissions) != 2 ||
		len(at.Owed.Answers) != 2 || !at.Owed.StepTechniques || !at.Owed.RequestedTechniques || at.Owed.CloseReason != "solution_shown" {
		t.Errorf("the attempt = %+v", at)
	}
	if len(e.Observations[r.attempt]) != 1 || len(e.Evidence) != 1 || len(p.Evidence) != 1 || string(e.State) != "\x05" ||
		p.State.LastSeq != 1 || p.State.LearnerModelVersion != "v0.1.4" {
		t.Errorf("the records of protobuf = %d observations, %d evidence, state %q, %+v", len(e.Observations[r.attempt]), len(e.Evidence), e.State, p.State)
	}
	if a.AccountID != exportv1.ID(r.account) || a.Tariff != "closed_slice" || a.UtcOffsetMinutes == nil || a.DayEndsAt == nil ||
		len(a.ProfileIds) != 1 || len(a.Consents) != 3 || len(a.QuotaCounters) != 1 || a.QuotaCounters[0].DayStart != "2026-11-12T05:00:00Z" ||
		a.QuotaCounters[0].SpentMicroUsd != 21000 || a.QuotaCounters[0].ReservedMicroUsd != 9343 || len(p.QuotaCounters) != 1 ||
		p.QuotaCounters[0].SpentMicroUsd != 21000 {
		t.Errorf("the account = %+v", a)
	}
	var parent, learner bool
	for _, c := range a.Consents {
		switch {
		case c.Giver == exportv1.ConsentGiverParent && c.RequestCode != nil && c.Form != nil && len(c.Texts) == 1 && c.Texts[0].ID == "parent-consent":
			parent = true
		case c.Giver == exportv1.ConsentGiverLearner && c.RequestCode == nil && len(c.Texts) == 1 && c.Texts[0].ID == "teen-consent":
			learner = true
		}
	}
	if !parent || !learner {
		t.Errorf("the consents of the account = %+v", a.Consents)
	}
	err = pgx.BeginFunc(t.Context(), r.pool, func(tx pgx.Tx) error {
		if _, err := store.ExportProfile(t.Context(), tx, r.account, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, store.ErrNoProfile) {
			t.Errorf("the export of no profile = %v", err)
		}
		if _, err := store.ExportAccount(t.Context(), tx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, store.ErrNoAccount) {
			t.Errorf("the export of no account = %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Each statement of the export that fails is the store's failure.
func TestAFailedStatementOfTheExportIsAFailure(t *testing.T) {
	t.Parallel()

	r := newRich(t)
	for _, failOn := range []string{"FROM accounts WHERE id", "SELECT id FROM profiles WHERE account_id", "FROM consents c",
		"FROM account_texts", "SELECT active_on", "FROM connected_modules", "FROM lesson_runs WHERE", "FROM engine_choices WHERE",
		"FROM work_storage_consents", "FROM problem_reports", "FROM attempts WHERE", "FROM attempt_rungs", "FROM submissions",
		"FROM evidence", "FROM skill_states"} {
		err := pgx.BeginFunc(t.Context(), &flaky{Pool: r.pool, failOn: failOn}, func(tx pgx.Tx) error {
			if _, err := store.ExportProfile(t.Context(), tx, r.account, r.profile); err != nil {
				return err
			}
			_, err := store.ExportAccount(t.Context(), tx, r.account)
			return err
		})
		if !errors.Is(err, errDown) {
			t.Errorf("failing on %q = %v, want the failure", failOn, err)
		}
	}
}

// The consents of the contract of spec's epic E4 in the store: a code of a
// request unique among those that wait, a form sent, the learner's own consent,
// the age given again, the storage of works and a problem report.
func TestTheAgreementsInTheStore(t *testing.T) {
	t.Parallel()

	r := newRich(t)
	ctx := t.Context()
	var waiting store.ChildConsent
	var learner store.Consent
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		v := int32(1)
		var err error
		if waiting, err = store.AskChildConsent(ctx, tx, r.account, store.NewConsent{AgeCategory: "under_13", Country: "US",
			Applied: 13, Giver: store.GiverParent, RequestCode: "ZZZZZ-ZZZZZ", TextVersion: &v}); err != nil {
			return err
		}
		if _, err := store.AskChildConsent(ctx, tx, r.account, store.NewConsent{AgeCategory: "under_13", Country: "US",
			Applied: 13, Giver: store.GiverParent, RequestCode: "ZZZZZ-ZZZZZ"}); !errors.Is(err, store.ErrCodeTaken) {
			t.Errorf("a code taken = %v, want ErrCodeTaken", err)
		}
		c, err := store.ConsentFor(ctx, tx, r.account, waiting.ID)
		if err != nil {
			return err
		}
		if err := store.PendForm(ctx, tx, c.ID+"-scan", "image/png", time.Now()); err != nil {
			return err
		}
		if err := store.SendForm(ctx, tx, c, c.ID+"-scan"); err != nil {
			return err
		}
		teen, err := store.AskChildConsent(ctx, tx, r.account, store.NewConsent{AgeCategory: "from_consent_to_18", Country: "US",
			Applied: 13, Giver: store.GiverLearner})
		if err != nil {
			return err
		}
		held, err := store.ConsentFor(ctx, tx, r.account, teen.ID)
		if err != nil {
			return err
		}
		if _, err := store.CreateChildProfile(ctx, tx, r.account, held, store.Profile{Pseudonym: "Fox", VoicingSpeed: "normal",
			DefaultMode: "by_steps", AskBeforeShowing: true}); err != nil {
			return err
		}
		learner, err = store.ConsentFor(ctx, tx, r.account, teen.ID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if forms, err := store.SentForms(ctx, r.pool); err != nil || len(forms) != 1 || forms[0].RequestCode != "ZZZZZ-ZZZZZ" {
		t.Errorf("SentForms = %+v, %v", forms, err)
	}
	if err := store.JudgeForm(ctx, r.pool, "ZZZZZ-ZZZZZ", true); err != nil {
		t.Errorf("JudgeForm = %v", err)
	}
	if err := store.JudgeForm(ctx, r.pool, "ZZZZZ-ZZZZZ", false); !errors.Is(err, store.ErrNoForm) {
		t.Errorf("JudgeForm of no form = %v, want ErrNoForm", err)
	}
	err = pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if given, err := store.GiveOwnConsent(ctx, tx, learner.ProfileID, nil); err != nil || !given {
			t.Errorf("GiveOwnConsent = %v, %v", given, err)
		}
		c, err := store.ConsentOfProfile(ctx, tx, r.account, learner.ProfileID)
		if err != nil {
			return err
		}
		if err := store.RestateAge(ctx, tx, c, store.Restated{AgeCategory: "from_consent_to_18", Country: "US", Applied: 13}); err != nil {
			return err
		}
		if err := store.RestateAge(ctx, tx, c, store.Restated{AgeCategory: "under_13", Country: "US", Applied: 13,
			Giver: store.GiverParent, RequestCode: "YYYYY-YYYYY"}); err != nil {
			return err
		}
		if _, err := store.AskChildConsent(ctx, tx, r.account, store.NewConsent{AgeCategory: "under_13", Country: "US", Applied: 13,
			Giver: store.GiverParent, RequestCode: "XXXXX-XXXXX"}); err != nil {
			return err
		}
		if c, err = store.ConsentOfProfile(ctx, tx, r.account, learner.ProfileID); err != nil {
			return err
		}
		if err := store.RestateAge(ctx, tx, c, store.Restated{AgeCategory: "under_13", Country: "US", Applied: 13,
			Giver: store.GiverParent, RequestCode: "XXXXX-XXXXX"}); !errors.Is(err, store.ErrCodeTaken) {
			t.Errorf("a code taken by the age given again = %v, want ErrCodeTaken", err)
		}
		if cancelled, err := store.CancelChildConsent(ctx, tx, r.account, c.ID); err != nil || cancelled {
			t.Errorf("CancelChildConsent of a profile's = %v, %v; want nothing taken back", cancelled, err)
		}
		if _, on, err := store.WorkStorageOn(ctx, tx, r.profile); err != nil || on {
			t.Errorf("WorkStorageOn = %v, %v; want off", on, err)
		}
		if err := store.SwitchWorkStorageOn(ctx, tx, r.profile, store.GiverOwner, nil, time.Now()); err != nil {
			return err
		}
		if giver, on, err := store.WorkStorageOn(ctx, tx, r.profile); err != nil || !on || giver != store.GiverOwner {
			t.Errorf("WorkStorageOn = %s %v, %v; want on by the owner", giver, on, err)
		}
		if err := store.SwitchWorkStorageOff(ctx, tx, r.profile, time.Now()); err != nil {
			return err
		}
		if there, err := store.AttemptOfProfile(ctx, tx, r.profile, r.attempt); err != nil || !there {
			t.Errorf("AttemptOfProfile = %v, %v", there, err)
		}
		_, err = store.WriteReport(ctx, tx, r.account, r.profile, store.Report{AttemptID: r.attempt, Reply: "correct_answer",
			Words: "w", Kind: "other", CreatedAt: time.Now()})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	reports, err := store.ReportsToReview(ctx, r.pool, 72*time.Hour)
	if err != nil || len(reports) != 1 || reports[0].ReviewBy.Sub(reports[0].CreatedAt) != 72*time.Hour {
		t.Fatalf("ReportsToReview = %+v, %v", reports, err)
	}
	if err := store.ReviewReport(ctx, r.pool, reports[0].ID); err != nil {
		t.Errorf("ReviewReport = %v", err)
	}
	if err := store.ReviewReport(ctx, r.pool, reports[0].ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("ReviewReport again = %v, want ErrNotFound", err)
	}
	if n, err := store.DropReports(ctx, r.pool, time.Now().Add(time.Hour)); err != nil || n != 2 {
		t.Errorf("DropReports = %d, %v; want the two reviewed", n, err)
	}
	if _, err := r.pool.Exec(ctx, `UPDATE consent_forms SET consent_id = NULL, keep_until = now()::date`); err != nil {
		t.Fatal(err)
	}
	objects, err := store.FormsPastTerm(ctx, r.pool, time.Now(), "", 10)
	if err != nil || len(objects) != 1 {
		t.Fatalf("FormsPastTerm = %v, %v", objects, err)
	}
	if err := store.DropForm(ctx, r.pool, objects[0]); err != nil || count(t, r.pool, "consent_forms") != 0 {
		t.Errorf("DropForm = %v", err)
	}
}

// Each statement of the agreements and of the operator that fails is the
// store's failure.
func TestAFailedStatementOfTheAgreementsIsAFailure(t *testing.T) {
	t.Parallel()

	r := newRich(t)
	ctx := t.Context()
	var waiting string
	if err := r.pool.QueryRow(ctx, `SELECT id FROM consents WHERE giver = 'parent' LIMIT 1`).Scan(&waiting); err != nil {
		t.Fatal(err)
	}
	c := store.Consent{ChildConsent: store.ChildConsent{ID: waiting}, ProfileID: r.profile}
	for _, tc := range []struct {
		failOn string
		run    func(tx pgx.Tx) error
	}{
		{"INSERT INTO consents", func(tx pgx.Tx) error {
			_, err := store.AskChildConsent(ctx, tx, r.account, store.NewConsent{AgeCategory: "under_13", Country: "US", Applied: 13, Giver: store.GiverLearner})
			return err
		}},
		{"FROM consents WHERE", func(tx pgx.Tx) error {
			_, err := store.ConsentOfProfile(ctx, tx, r.account, r.profile)
			return err
		}},
		{"INSERT INTO child_consent_records", func(tx pgx.Tx) error {
			_, err := store.CancelChildConsent(ctx, tx, r.account, c.ID)
			return err
		}},
		{"DELETE FROM consents", func(tx pgx.Tx) error {
			_, err := store.CancelChildConsent(ctx, tx, r.account, c.ID)
			return err
		}},
		{"INSERT INTO consent_forms", func(tx pgx.Tx) error { return store.PendForm(ctx, tx, "o", "image/png", time.Now()) }},
		{"SET consent_id = NULL", func(tx pgx.Tx) error { return store.SendForm(ctx, tx, c, "o") }},
		{"SET consent_id = $2", func(tx pgx.Tx) error { return store.SendForm(ctx, tx, c, "o") }},
		{"SET form_state = 'sent'", func(tx pgx.Tx) error {
			if err := store.PendForm(ctx, tx, "o", "image/png", time.Now()); err != nil {
				return err
			}
			return store.SendForm(ctx, tx, c, "o")
		}},
		{"SET state = 'confirmed'", func(tx pgx.Tx) error {
			_, err := store.GiveOwnConsent(ctx, tx, r.profile, nil)
			return err
		}},
		{"UPDATE profiles SET age_category", func(tx pgx.Tx) error {
			return store.RestateAge(ctx, tx, c, store.Restated{AgeCategory: "adult", Country: "US"})
		}},
		{"applied = nullif($4, 0) WHERE id", func(tx pgx.Tx) error {
			return store.RestateAge(ctx, tx, c, store.Restated{AgeCategory: "adult", Country: "US"})
		}},
		{"INSERT INTO child_consent_records", func(tx pgx.Tx) error {
			return store.RestateAge(ctx, tx, c, store.Restated{AgeCategory: "adult", Country: "US", Giver: store.GiverLearner})
		}},
		{"INSERT INTO consents (account_id, profile_id", func(tx pgx.Tx) error {
			return store.RestateAge(ctx, tx, c, store.Restated{AgeCategory: "adult", Country: "US", Giver: store.GiverLearner})
		}},
		{"DELETE FROM consents WHERE id", func(tx pgx.Tx) error {
			return store.RestateAge(ctx, tx, c, store.Restated{AgeCategory: "adult", Country: "US", Giver: store.GiverLearner})
		}},
		{"FROM work_storage_consents", func(tx pgx.Tx) error {
			_, _, err := store.WorkStorageOn(ctx, tx, r.profile)
			return err
		}},
		{"UPDATE work_storage_consents", func(tx pgx.Tx) error {
			return store.SwitchWorkStorageOn(ctx, tx, r.profile, store.GiverOwner, nil, time.Now())
		}},
		{"INSERT INTO work_storage_consents", func(tx pgx.Tx) error {
			return store.SwitchWorkStorageOn(ctx, tx, r.profile, store.GiverOwner, nil, time.Now())
		}},
		{"SET work_storage = true", func(tx pgx.Tx) error {
			return store.SwitchWorkStorageOn(ctx, tx, r.profile, store.GiverOwner, nil, time.Now())
		}},
		{"UPDATE work_storage_consents", func(tx pgx.Tx) error { return store.SwitchWorkStorageOff(ctx, tx, r.profile, time.Now()) }},
		{"SET work_storage = false", func(tx pgx.Tx) error { return store.SwitchWorkStorageOff(ctx, tx, r.profile, time.Now()) }},
		{"INSERT INTO account_texts", func(tx pgx.Tx) error {
			return store.AcceptTexts(ctx, tx, r.account, []store.TextRef{{ID: "x", Version: 1}}, time.Now())
		}},
		{"INSERT INTO problem_reports", func(tx pgx.Tx) error {
			_, err := store.WriteReport(ctx, tx, r.account, r.profile, store.Report{})
			return err
		}},
		{"FROM attempts WHERE", func(tx pgx.Tx) error {
			_, err := store.AttemptOfProfile(ctx, tx, r.profile, r.attempt)
			return err
		}},
		{"FROM consents c", func(tx pgx.Tx) error {
			_, err := store.SentForms(ctx, tx)
			return err
		}},
		{"UPDATE consents", func(tx pgx.Tx) error { return store.JudgeForm(ctx, tx, "ABCDE-FGHJK", true) }},
		{"FROM problem_reports", func(tx pgx.Tx) error {
			_, err := store.ReportsToReview(ctx, tx, time.Hour)
			return err
		}},
		{"UPDATE problem_reports", func(tx pgx.Tx) error { return store.ReviewReport(ctx, tx, r.attempt) }},
		{"DELETE FROM problem_reports", func(tx pgx.Tx) error {
			_, err := store.DropReports(ctx, tx, time.Now())
			return err
		}},
		{"FROM consent_forms", func(tx pgx.Tx) error {
			_, err := store.FormsPastTerm(ctx, tx, time.Now(), "", 1)
			return err
		}},
		{"DELETE FROM consent_forms", func(tx pgx.Tx) error { return store.DropForm(ctx, tx, "o") }},
		{"FROM profiles WHERE id", func(tx pgx.Tx) error {
			_, err := store.ProfileWithConsent(ctx, tx, r.account, r.profile)
			return err
		}},
		{"FROM consents WHERE profile_id", func(tx pgx.Tx) error {
			_, err := store.ProfileWithConsent(ctx, tx, r.account, r.profile)
			return err
		}},
	} {
		err := pgx.BeginFunc(ctx, &flaky{Pool: r.pool, failOn: tc.failOn}, tc.run)
		if !errors.Is(err, errDown) {
			t.Errorf("failing on %q = %v, want the failure", tc.failOn, err)
		}
	}
}
