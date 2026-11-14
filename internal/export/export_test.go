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

	"github.com/altnovatrail/module-kit/modulepackage"
	"github.com/altnovatrail/spec"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	observationv1 "github.com/altnovatrail/spec/contracts/novatrail/observation/v1"
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
// state, a lesson run, a choice, the storage of works, two stored works and a
// problem report, and of its account, is api.zip whose every file passes its
// schema of spec's part export/, version 2, and keeps its rules of the layout and
// the .proto files (R135 spec). A stored work is written newest first, its
// history and contexts as the JSON they are and its picture in base64 (R70).
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
	exec(`INSERT INTO problem_reports (account_id, profile_id, attempt_id, reply, rung, words, kind, text, created_at)
		VALUES ($1, $2, $3, 'rung', 1, 'The words of the rung.', 'unclear', '', now())`, account, profile, attempt)
	exec(`INSERT INTO work_storage_consents (profile_id, giver, text_version, on_at, off_at) VALUES ($1, 'owner', 1, now(), now())`, profile)
	// Two stored works, the one of the attempt that left the image the newer.
	for i, a := range []string{attempt, gone} {
		work, err := proto.Marshal(&v1.StoredWork{AttemptId: a, History: []byte(`[{"kind":"write","t":1}]`),
			Submissions: []*v1.WorkSubmission{{Context: []byte(`{"answer_slot":1}`), HistoryLength: 1}}, Picture: []byte("\x89PNG\r\n\x1a\n")})
		if err != nil {
			t.Fatal(err)
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
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		_, err := m.Settle(ctx, tx, profile, time.Now())
		return err
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
		`"works":[{"attempt_id":"` + gone + `","kind":"handwriting"`, `"history":[{"kind":"write","t":1}]`,
		`"submissions":[{"context":{"answer_slot":1},"history_length":1}]`, `"picture":"iVBORw0KGgo="`} {
		if !strings.Contains(text, has) {
			t.Errorf("the profile's file holds no %s:\n%s", has, text)
		}
	}
	if strings.Contains(text, `"seed"`) {
		t.Error("the profile's file holds the seed of an attempt")
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
		"work":        `INSERT INTO works (profile_id, attempt_id, kind, size_bytes, work, stored_at) SELECT $1, id, 'drawing', 1, '\xff', now() FROM attempts WHERE profile_id = $1 AND $2 <> ''`,
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
