package export_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	learnermodel "github.com/altnovatrail/learner-model"
	"github.com/altnovatrail/module-kit/modulepackage"
	"github.com/altnovatrail/spec"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	observationv1 "github.com/altnovatrail/spec/contracts/novatrail/observation/v1"
	paperv1 "github.com/altnovatrail/spec/contracts/novatrail/paper/v1"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/export"
	"github.com/altnovatrail/api/internal/model"
	"github.com/altnovatrail/api/internal/packages/packagestest"
	"github.com/altnovatrail/api/internal/store"
	"github.com/altnovatrail/api/internal/store/storetest"
)

func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }

// schemas is the schemas of the part export/ of spec's tag, by the file of an
// export each holds.
func schemas(t *testing.T) map[string]*jsonschema.Schema {
	t.Helper()
	compiler := jsonschema.NewCompiler()
	ids := map[string]string{}
	entries, err := fs.ReadDir(spec.Export, "schemas")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		data, err := fs.ReadFile(spec.Export, "schemas/"+e.Name())
		if err != nil {
			t.Fatal(err)
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		id := doc.(map[string]any)["$id"].(string)
		if err := compiler.AddResource(id, doc); err != nil {
			t.Fatal(err)
		}
		ids[e.Name()] = id
	}
	out := map[string]*jsonschema.Schema{}
	for _, name := range []string{"account.schema.json", "profile.schema.json"} {
		s, err := compiler.Compile(ids[name])
		if err != nil {
			t.Fatal(err)
		}
		out[name] = s
	}
	return out
}

// answer is a submission's answer of one item of one slot.
func answer(wrong int32, verdict, value string) []byte {
	data, _ := proto.Marshal(&v1.SubmitWorkResponse{WrongAnswers: wrong, Items: []*v1.ItemVerdict{{Verdict: verdict,
		AnswerSlots: []*v1.AnswerSlotVerdict{{AnswerSlot: &observationv1.AnswerSlotPlace{Item: 1, Slot: 1}, Verdict: verdict, Value: &value}}}}})
	return data
}

var choices *modulepackage.Package

// The export of a profile with an attempt, its submissions, its evidence and its
// state, an essay with the marks of its feedback by number (R15), a lesson run,
// a choice, the storage of works, two stored works, a problem report, an
// adult's correction of a level — named by its correction_id (R157 spec) —, the
// open attempts of a page from paper, with where each task's key came from but
// not the key, and a closed one, which api keeps nothing of, and of its
// account, is api.zip whose every file passes its schema of spec's part export/
// and keeps its rules of the layout and the .proto files (R135 spec). A stored
// work is written newest first, named without its bytes (R70, R84).
func TestTheExportPassesItsSchemas(t *testing.T) {
	t.Parallel()

	choices = packagestest.Own(t, "choices")
	set := packagestest.Set(t, packagestest.Entry{Pkg: choices, Release: "v0.1.0"})
	m, err := model.New(set, "v0.1.6")
	if err != nil {
		t.Fatal(err)
	}
	x, err := export.New(set, m)
	if err != nil {
		t.Fatal(err)
	}
	pool := storetest.DB(t)
	ctx := t.Context()
	var account, profile, consent string
	if err := pool.QueryRow(ctx, `INSERT INTO accounts (auth_uid, last_request_on, adult_stated_at, tariff) VALUES ('uid-synthetic', now(), now(), 'closed_slice') RETURNING id`).Scan(&account); err != nil {
		t.Fatal(err)
	}
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := store.AcceptTexts(ctx, tx, account, []store.TextRef{{ID: "privacy-policy", Version: 2}, {ID: "terms-of-use", Version: 1}}, time.Now()); err != nil {
			return err
		}
		p, err := store.CreateOwnerProfile(ctx, tx, account, store.Profile{Pseudonym: "Owl", Country: "US", VoicingSpeed: "normal", DefaultMode: "by_steps"})
		profile = p.ID
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO consents (account_id, subject, state, age_category, country, giver, applied, text_version)
		VALUES ($1, 'child', 'waiting', 'under_13', 'US', 'parent', 13, 1) RETURNING id`, account).Scan(&consent); err != nil {
		t.Fatal(err)
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO connected_modules (profile_id, module_id) VALUES ($1, $2)`, profile, string(choices.View.Manifest.ID))
	var choice, attempt, run string
	if err := pool.QueryRow(ctx, `INSERT INTO engine_choices (profile_id, kind, module_id, task_id, reason_code, engine_version,
			learner_model_version, modules_release, format_window, last_seq, digests, engine_digests, age_category, pool_limited)
		VALUES ($1, 'task', $2, 'pick', 2, 'v0.1.6', 'v0.1.4', 'v0.1.0', '0.4', 0, ARRAY[$3], ARRAY[$3], 'adult', false) RETURNING id`,
		profile, string(choices.View.Manifest.ID), string(choices.Digest)).Scan(&choice); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO attempts (profile_id, package_digest, module_id, module_version, module_format_version,
			module_kit_version, task_id, seed, choice_id, wrong_answers, step_techniques_at, requested_techniques_at, leading_question_at)
		VALUES ($1, $2, $3, $4, '0.4', 'v0.3.0', 'pick', 7, $5, 1, now(), now(), now()) RETURNING id`,
		profile, string(choices.Digest), string(choices.View.Manifest.ID), string(choices.Inventory.Version), choice).Scan(&attempt); err != nil {
		t.Fatal(err)
	}
	for i, a := range [][]byte{answer(1, "not_yet", "five"), answer(1, "correct", "three and four")} {
		exec(`INSERT INTO submissions (profile_id, attempt_id, number, answer) VALUES ($1, $2, $3, $4)`, profile, attempt, i+1, a)
	}
	exec(`INSERT INTO attempt_rungs (profile_id, attempt_id, rung, source) VALUES ($1, $2, 1, 'fallback')`, profile, attempt)
	// An attempt on a version that left the image, read by AI: an item never
	// verdicted, and an observation, with no tap.
	var gone string
	if err := pool.QueryRow(ctx, `INSERT INTO attempts (profile_id, package_digest, module_id, module_version, module_format_version,
			module_kit_version, task_id, seed, previous_attempt_id, close_reason, closed_at)
		VALUES ($1, $2, $3, '0.0.1', '0.4', 'v0.3.0', 'pick', 7, $4, 'unchecked', now()) RETURNING id`,
		profile, "sha256:"+strings.Repeat("c", 64), string(choices.View.Manifest.ID), attempt).Scan(&gone); err != nil {
		t.Fatal(err)
	}
	observation, err := proto.Marshal(&observationv1.Observation{})
	if err != nil {
		t.Fatal(err)
	}
	unreadable, _ := proto.Marshal(&v1.SubmitWorkResponse{Items: []*v1.ItemVerdict{{Verdict: "unreadable",
		AnswerSlots: []*v1.AnswerSlotVerdict{{AnswerSlot: &observationv1.AnswerSlotPlace{Item: 1, Slot: 1}, Verdict: "unreadable", Reason: "illegible"}}}},
		Outcome: &v1.Outcome{Cause: v1.Cause_CAUSE_AI_FAILURE}})
	exec(`INSERT INTO submissions (profile_id, attempt_id, number, answer, observation) VALUES ($1, $2, 1, $3, $4)`, profile, gone, unreadable, observation)
	exec(`UPDATE attempts SET close_reason = 'correct', closed_at = now() WHERE id = $1`, attempt)
	// An essay closed "done": feedback with the marks of its two criteria, and a
	// sheet that could not be read, each with its answer of no text (R15).
	var essay string
	if err := pool.QueryRow(ctx, `INSERT INTO attempts (profile_id, package_digest, module_id, module_version, module_format_version,
			module_kit_version, task_id, seed, previous_attempt_id, close_reason, closed_at)
		VALUES ($1, $2, $3, $4, '0.4', 'v0.3.0', 'story', 7, $5, 'done', now()) RETURNING id`,
		profile, string(choices.Digest), string(choices.View.Manifest.ID), string(choices.Inventory.Version), attempt).Scan(&essay); err != nil {
		t.Fatal(err)
	}
	fed, _ := proto.Marshal(&v1.SubmitEssayResponse{End: v1.SubmissionEnd_SUBMISSION_END_FEEDBACK})
	blank, _ := proto.Marshal(&v1.SubmitEssayResponse{End: v1.SubmissionEnd_SUBMISSION_END_UNREADABLE, Reason: "blank"})
	exec(`INSERT INTO submissions (profile_id, attempt_id, number, answer, essay_end, marks, words_source)
		VALUES ($1, $2, 1, $3, 'feedback', ARRAY['met', 'not_met'], 'model'), ($1, $2, 2, $4, 'unreadable', NULL, NULL)`,
		profile, essay, fed, blank)
	exec(`INSERT INTO problem_reports (account_id, profile_id, attempt_id, reply, rung, words, kind, text, created_at)
		VALUES ($1, $2, $3, 'rung', 1, 'The words of the rung.', 'unclear', '', now())`, account, profile, attempt)
	exec(`INSERT INTO work_storage_consents (profile_id, giver, text_version, on_at, off_at) VALUES ($1, 'owner', 1, now(), now())`, profile)
	// Two stored works, the one of the attempt that left the image the newer. The
	// newer one's bytes are no message at all: the export names a work by its
	// columns and never reads them (R84).
	for i, a := range []string{attempt, gone} {
		work, err := proto.Marshal(&v1.StoredWork{AttemptId: a, History: []byte(`[{"kind":"write","t":1}]`),
			Submissions: []*v1.WorkSubmission{{Context: []byte(`{"answer_slot":1}`), HistoryLength: 1}}, Picture: []byte("\x89PNG\r\n\x1a\n")})
		if err != nil {
			t.Fatal(err)
		}
		if a == gone {
			work = []byte("\xff")
		}
		exec(`INSERT INTO works (profile_id, attempt_id, kind, size_bytes, work, stored_at)
			VALUES ($1, $2, 'handwriting', 50, $3, now() - make_interval(mins => $4))`, profile, a, work, 1-i)
	}
	if err := pool.QueryRow(ctx, `WITH c AS (INSERT INTO engine_choices (profile_id, kind, module_id, lesson_id)
			VALUES ($1, 'lesson', $2, 'basics') RETURNING id)
		INSERT INTO lesson_runs (profile_id, lesson_id, module_id, module_version, package_digest, choice_id)
		SELECT $1, $2 || '/lesson/basics', $2, $3, $4, id FROM c RETURNING id`,
		profile, string(choices.View.Manifest.ID), string(choices.Inventory.Version), string(choices.Digest)).Scan(&run); err != nil {
		t.Fatal(err)
	}
	// An adult's correction of the level, a fact of the journal of no attempt (R89).
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		s, err := m.Settle(ctx, tx, profile, time.Now())
		if err != nil {
			return err
		}
		_, err = m.Correct(ctx, tx, profile, s, choices, learnermodel.BandRef{Place: 1, ID: "grades-1-2"}, time.Now())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var correction string
	if err := pool.QueryRow(ctx, `SELECT correction_id FROM evidence WHERE profile_id = $1 AND attempt_id IS NULL`, profile).Scan(&correction); err != nil {
		t.Fatal(err)
	}
	// A page of three tasks: the first checked once, its frame of four tasks keyed
	// by the solver, with no answer, by AI, and by a provenance this build knows
	// not, read with its observation and a rung; the second begun alone; the third
	// closed, which leaves nothing to export.
	var page []string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) (err error) {
		page, err = store.BeginPaperAttempts(ctx, tx, profile, "page-1", 3, time.Now())
		if err != nil {
			return err
		}
		key, _ := proto.Marshal(&paperv1.Key{Tasks: []*paperv1.TaskKey{
			{Statement: "3 + 4 = □", Provenance: paperv1.Provenance_PROVENANCE_SOLVER,
				Answer: &paperv1.Answer{Kind: &paperv1.Answer_Number{Number: &paperv1.Number{Value: "7"}}}},
			{Statement: "Draw a cat."},
			{Statement: "Ann has 3 stamps and gets 4 more. How many has she?", Provenance: paperv1.Provenance_PROVENANCE_AI,
				Answer: &paperv1.Answer{Kind: &paperv1.Answer_Number{Number: &paperv1.Number{Value: "7"}}}},
			{Statement: "5 + 2 = □", Answer: &paperv1.Answer{Kind: &paperv1.Answer_Number{Number: &paperv1.Number{Value: "7"}}}},
		}})
		if err := store.SetPaperKey(ctx, tx, profile, page[0], key); err != nil {
			return err
		}
		reading, _ := proto.Marshal(&paperv1.Reading{Tasks: []*paperv1.ReadTask{{Statement: "9 - 6 = □", Formula: "9-6"}}})
		if err := store.SetPaperReading(ctx, tx, profile, page[1], reading); err != nil {
			return err
		}
		if _, err := store.Submit(ctx, tx, profile, page[0], answer(1, "not_yet", "8"), observation, true, time.Now()); err != nil {
			return err
		}
		if err := store.WriteRung(ctx, tx, profile, page[0], 1, store.Words{Source: store.SourceModel, Text: "Count again."}); err != nil {
			return err
		}
		if _, err := store.Submit(ctx, tx, profile, page[2], answer(1, "not_yet", "2"), observation, true, time.Now()); err != nil {
			return err
		}
		return store.CloseAttempt(ctx, tx, profile, page[2], "left", time.Now())
	}); err != nil {
		t.Fatal(err)
	}

	checks := schemas(t)
	var part, accountPart []byte
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var err error
		if part, err = x.Profile(ctx, tx, account, profile); err != nil {
			return err
		}
		accountPart, err = x.Account(ctx, tx, account)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	files := unzip(t, part)
	want := []string{"profiles/" + profile + ".json", "proto/novatrail/learnermodel/v1/common.proto",
		"proto/novatrail/learnermodel/v1/evidence.proto", "proto/novatrail/learnermodel/v1/state.proto",
		"proto/novatrail/observation/v1/observation.proto"}
	if got := slices.Sorted(keys(files)); !slices.Equal(got, want) {
		t.Errorf("the export of the profile holds %v, want %v", got, want)
	}
	var got map[string]any
	if err := json.Unmarshal(files["profiles/"+profile+".json"], &got); err != nil {
		t.Fatal(err)
	}
	if err := checks["profile.schema.json"].Validate(got); err != nil {
		t.Errorf("the profile's file breaks its schema: %v\n%s", err, files["profiles/"+profile+".json"])
	}
	text := string(files["profiles/"+profile+".json"])
	for _, has := range []string{`"traps":["` + string(choices.View.Manifest.ID) + `/trap/`, `"reason_code":"new_skill"`,
		`"problem_reports":[{`, `"storage_consents":[{`, `"evidence":[{`, `"tapped_choices":[{`, `"leading_question_on_request":true`,
		`"works":[{"attempt_id":"` + gone + `","kind":"handwriting","stored_at":"`, `"size_bytes":50},{"attempt_id":"` + attempt + `"`,
		`"criterion_marks":["met","not_met"],"feedback_source":"model"}`, `"end":"unreadable"}]`,
		`{"seq":2,"correction_id":"` + correction + `","package_digest":"` + string(choices.Digest) + `"`, `"level_set_by_adult":true`,
		`{"attempt_id":"` + page[0] + `"`, `"key_sources":["solver","none","ai","none"],"wrong_answers":1,"rungs":[{"rung":1,"source":"model","words":"Count again."`,
		`{"attempt_id":"` + page[1] + `","begun_at":"`} {
		if !strings.Contains(text, has) {
			t.Errorf("the profile's file holds no %s:\n%s", has, text)
		}
	}
	if strings.Contains(text, `"seed"`) {
		t.Error("the profile's file holds the seed of an attempt")
	}
	// The key of a task from paper and the reading of its statement never reach
	// the device, and a closed attempt of paper is not kept (R05 spec; R101). The
	// formula is matched as a JSON string: bare, "9-6" occurs in random UUIDs.
	if strings.Contains(text, "3 + 4") || strings.Contains(text, "9 - 6") || strings.Contains(text, `"9-6"`) || strings.Contains(text, page[2]) || strings.Count(text, `"key_sources"`) != 1 {
		t.Error("the profile's file holds a key or a reading of a task from paper, or its closed attempt, or key sources before a check")
	}
	// The works are named without their bytes: app reads each by GetWork (R151 spec).
	if strings.Contains(text, `"history"`) || strings.Contains(text, `"picture"`) {
		t.Error("the profile's file holds the bytes of a stored work")
	}
	accountFiles := unzip(t, accountPart)
	var a map[string]any
	if err := json.Unmarshal(accountFiles["account.json"], &a); err != nil {
		t.Fatal(err)
	}
	if err := checks["account.schema.json"].Validate(a); err != nil {
		t.Errorf("the account's file breaks its schema: %v\n%s", err, accountFiles["account.json"])
	}
	if !strings.Contains(string(accountFiles["account.json"]), `"request_code":"`) || accountFiles["profiles/"+profile+".json"] == nil {
		t.Errorf("the account's export holds %v", slices.Sorted(keys(accountFiles)))
	}
	if again, err := export.New(set, m); err != nil || again == nil {
		t.Errorf("a second exporter = %v, %v", again, err)
	}
	_ = run
}

// The .proto of the observation the export carries is that of the contracts at
// the version go.mod takes. It is read from that module, which every build has
// in its module cache, and not from the submodule: CI checks out none.
func TestTheObservationsProtoIsTheContracts(t *testing.T) {
	t.Parallel()

	ours, err := os.ReadFile("proto/novatrail/observation/v1/observation.proto")
	if err != nil {
		t.Fatal(err)
	}
	dir, err := exec.CommandContext(t.Context(), "go", "list", "-m", "-f", "{{.Dir}}", "github.com/altnovatrail/spec/contracts").Output()
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(dir)), "novatrail", "observation", "v1", "observation.proto"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ours, theirs) {
		t.Error("internal/export/proto/novatrail/observation/v1/observation.proto is not that of the contracts go.mod takes: copy it again")
	}
}

func unzip(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		files[f.Name] = b
	}
	return files
}

func keys[V any](m map[string]V) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

var _ *pgxpool.Pool

// A record that is no message of its .proto, and a database that fails, fail
// the export rather than give one in part.
func TestARecordThatIsNoMessageFailsTheExport(t *testing.T) {
	t.Parallel()

	built := packagestest.Own(t, "choices")
	set := packagestest.Set(t, packagestest.Entry{Pkg: built, Release: "v0.1.0"})
	m, err := model.New(set, "v0.1.6")
	if err != nil {
		t.Fatal(err)
	}
	x, err := export.New(set, m)
	if err != nil {
		t.Fatal(err)
	}
	pool := storetest.DB(t)
	ctx := t.Context()
	for name, spoil := range map[string]string{
		"evidence":    `INSERT INTO evidence (profile_id, seq, attempt_id, package_digest, module_id, evidence) SELECT $1, 1, id, $2, 'example/m', '\xff' FROM attempts WHERE profile_id = $1`,
		"state":       `UPDATE skill_states SET state = '\xff' WHERE profile_id = $1 AND $2 <> ''`,
		"answer":      `INSERT INTO submissions (profile_id, attempt_id, number, answer) SELECT $1, id, 1, '\xff' FROM attempts WHERE profile_id = $1 AND $2 <> ''`,
		"observation": `INSERT INTO submissions (profile_id, attempt_id, number, answer, observation) SELECT $1, id, 1, '', '\xff' FROM attempts WHERE profile_id = $1 AND $2 <> ''`,
		"paper key":   `INSERT INTO attempts (profile_id, kind, paper_key) SELECT $1, 'paper', '\xff' WHERE $2 <> ''`,
		"paper answer": `WITH a AS (INSERT INTO attempts (profile_id, kind) SELECT $1, 'paper' WHERE $2 <> '' RETURNING id)
			INSERT INTO submissions (profile_id, attempt_id, number, answer) SELECT $1, id, 1, '\xff' FROM a`,
	} {
		var account, profile string
		if err := pool.QueryRow(ctx, `INSERT INTO accounts (auth_uid, last_request_on, adult_stated_at, tariff)
			VALUES ($1, now(), now(), 'closed_slice') RETURNING id`, "uid-"+name).Scan(&account); err != nil {
			t.Fatal(err)
		}
		if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			p, err := store.CreateOwnerProfile(ctx, tx, account, store.Profile{Pseudonym: "Owl", Country: "US", VoicingSpeed: "normal", DefaultMode: "by_steps"})
			profile = p.ID
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `WITH c AS (INSERT INTO engine_choices (profile_id, kind, module_id, task_id)
				VALUES ($1, 'task', 'example/m', 'pick') RETURNING id)
			INSERT INTO attempts (profile_id, package_digest, module_id, module_version, module_format_version,
				module_kit_version, task_id, seed, choice_id)
			SELECT $1, $2, 'example/m', '0.1.0', '0.4', 'v0.3.0', 'pick', 1, id FROM c`,
			profile, string(built.Digest)); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, spoil, profile, string(built.Digest)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			if _, err := x.Profile(ctx, tx, account, profile); err == nil {
				t.Errorf("%s: the export of the profile = nil error", name)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			if _, err := x.Account(ctx, tx, account); err == nil {
				t.Errorf("%s: the export of the account = nil error", name)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := x.Profile(ctx, tx, "00000000-0000-0000-0000-000000000000", "00000000-0000-0000-0000-000000000000"); err == nil {
		t.Error("the export of a profile on a transaction closed: nil error")
	}
	if _, err := x.Account(ctx, tx, "00000000-0000-0000-0000-000000000000"); err == nil {
		t.Error("the export of an account on a transaction closed: nil error")
	}
}
