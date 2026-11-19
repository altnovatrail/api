// The tests of scripts/dev.sh, the run on the dev projects (R39): its loader of the dev file, the
// custom token it signs, how it puts a run on the stub of tutor or on the models, and how it tells a
// task AI reads, a workbook page and an essay, sends the boxes of the page, sums the counters of the
// quota, gives the check's account its tariff and makes the synthetic child of `just dev-app` once.
// They run the script's own functions in bash, with jq and openssl, as the devcontainer and its
// toolchain stage, where CI runs the tests, have them.
package scripts_test

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// bash runs script in bash with dev.sh sourced, and returns its stdout and stderr.
func bash(t *testing.T, env []string, script string) (string, string, error) {
	t.Helper()

	cmd := exec.Command("bash", "-c", "source ./dev.sh && "+script)
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// The loader takes API_* lines as they are, never runs them, and refuses what it must, naming a line
// by its number and never its text.
func TestTheDevFileIsReadAsWritten(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	good := filepath.Join(dir, "dev.env")
	if err := os.WriteFile(good, []byte("# a comment\n\nAPI_DEV_UID=dev-$(touch pwned)\r\nAPI_LOG_LEVEL=debug=really\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := bash(t, []string{"API_DEV_CONFIG=" + good}, `load && printf '%s|%s' "$API_DEV_UID" "$API_LOG_LEVEL"`)
	if err != nil || stdout != "dev-$(touch pwned)|debug=really" {
		t.Errorf("load = %q, %q, %v; want the values as written", stdout, stderr, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "pwned")); err == nil {
		t.Error("a value was run")
	}

	tree := filepath.Join(dir, "repo", "deep")
	if err := os.MkdirAll(tree, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "repo", ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	inTree := filepath.Join(tree, "dev.env")
	bad := filepath.Join(dir, "bad.env")
	other := filepath.Join(dir, "other.env")
	for path, content := range map[string]string{
		inTree: "API_LOG_LEVEL=debug\n",
		bad:    "API_DATABASE_URL=postgres://u:secret-value@h/db\nnot a line secret-value\n",
		other:  "PATH=/nowhere\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name, config, says string
	}{
		{"no file", filepath.Join(dir, "missing.env"), "no configuration at"},
		{"a file in a working tree", inTree, "is in a git working tree"},
		{"a line that is no variable", bad, "line 2, is not API_NAME=VALUE"},
		{"a variable not of api", other, "line 1, is not API_NAME=VALUE"},
	} {
		_, stderr, err := bash(t, []string{"API_DEV_CONFIG=" + tc.config}, "load")
		if err == nil || !strings.Contains(stderr, tc.says) || strings.Contains(stderr, "secret-value") {
			t.Errorf("%s: load = %q, %v; want a refusal saying %q and no value", tc.name, stderr, err, tc.says)
		}
	}
}

// serviceAccount writes a synthetic key of a service account to a file and returns its path and
// its key.
func serviceAccount(t *testing.T) (string, *rsa.PrivateKey) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	account, err := json.Marshal(map[string]string{
		"type":         "service_account",
		"client_email": "sa@synthetic.example",
		"private_key":  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "key.json")
	if err := os.WriteFile(path, account, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, key
}

// firebases fails the test unless token is a custom token of uid with Firebase's claims for one
// made with a JWT library, an hour long, whose RS256 signature holds under key's public half.
func firebases(t *testing.T, token, uid string, key *rsa.PrivateKey) {
	t.Helper()

	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		t.Fatalf("the token has %d parts, want 3", len(parts))
	}
	decode := func(part string) []byte {
		t.Helper()
		raw, err := base64.RawURLEncoding.DecodeString(part)
		if err != nil {
			t.Fatalf("a part of the token is not base64url with no padding: %v", err)
		}
		return raw
	}
	var header map[string]string
	var claims struct {
		Iss, Sub, Aud, UID string
		Iat, Exp           int64
	}
	if err := json.Unmarshal(decode(parts[0]), &header); err != nil || header["alg"] != "RS256" {
		t.Errorf("header = %v, %v; want RS256", header, err)
	}
	if err := json.Unmarshal(decode(parts[1]), &claims); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	if claims.Iss != "sa@synthetic.example" || claims.Sub != claims.Iss ||
		claims.Aud != "https://identitytoolkit.googleapis.com/google.identity.identitytoolkit.v1.IdentityToolkit" ||
		claims.UID != uid || claims.Iat < now-60 || claims.Iat > now || claims.Exp != claims.Iat+3600 {
		t.Errorf("claims = %+v, want Firebase's for a custom token of %s, an hour long", claims, uid)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], decode(parts[2])); err != nil {
		t.Errorf("the signature does not hold: %v", err)
	}
}

// The custom token carries Firebase's claims for one made with a JWT library, for the account it is
// asked for, and its RS256 signature holds under the service account's public key.
func TestTheCustomTokenIsFirebases(t *testing.T) {
	t.Parallel()

	path, key := serviceAccount(t)
	stdout, stderr, err := bash(t, nil, `custom_token "`+path+`" dev-synthetic`)
	if err != nil {
		t.Fatalf("custom_token: %v, %s", err, stderr)
	}
	firebases(t, stdout, "dev-synthetic", key)

	// A key that is no key fails, and says so.
	if err := os.WriteFile(path, []byte(`{"client_email":"sa@synthetic.example","private_key":"none"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, stderr, err := bash(t, nil, `custom_token "`+path+`" dev-synthetic`); err == nil || !strings.Contains(stderr, "would not sign") {
		t.Errorf("custom_token with no key = %q, %v; want a refusal", stderr, err)
	}
}

// The token for app lies in its folder as custom-token, its owner's alone, and a new one takes the
// old one's place by a rename, leaving nothing else behind; a folder that is not there — the volume
// not mounted — is refused, not made (R67).
func TestTheTokenIsLaidForApp(t *testing.T) {
	t.Parallel()

	path, key := serviceAccount(t)
	dir := t.TempDir()
	env := []string{"API_FIREBASE_CREDENTIALS_FILE=" + path}
	laid := filepath.Join(dir, "custom-token")
	// The second token is of another account, so that it differs from the first within one second.
	for _, uid := range []string{"dev-app", "dev-other"} {
		if _, stderr, err := bash(t, env, `lay_token "`+dir+`" `+uid); err != nil {
			t.Fatalf("lay_token: %v, %s", err, stderr)
		}
		info, err := os.Stat(laid)
		if err != nil {
			t.Fatal(err)
		}
		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Errorf("custom-token has the mode %o, want 600", mode)
		}
		token, err := os.ReadFile(laid)
		if err != nil {
			t.Fatal(err)
		}
		firebases(t, string(token), uid, key)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 1 {
		t.Errorf("the folder holds %v, %v; want custom-token alone", entries, err)
	}

	missing := filepath.Join(dir, "not-mounted")
	if _, stderr, err := bash(t, env, `lay_token "`+missing+`" dev-app`); err == nil || !strings.Contains(stderr, "Rebuild Container") {
		t.Errorf("lay_token into no folder = %q, %v; want a refusal naming Rebuild Container", stderr, err)
	}
	if _, err := os.Stat(missing); err == nil {
		t.Error("lay_token made the folder")
	}
	if _, stderr, err := bash(t, nil, `unset API_FIREBASE_CREDENTIALS_FILE; lay_token "`+dir+`" dev-app`); err == nil || !strings.Contains(stderr, "API_FIREBASE_CREDENTIALS_FILE") {
		t.Errorf("lay_token with no key = %q, %v; want a refusal naming the key", stderr, err)
	}
}

// The check knows a task AI reads by its answer slot written anywhere, however deep in the open view
// it lies, and a task checked by tap from it.
func TestATaskAIReadsIsToldFromOneByTap(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, tc := range []struct {
		name, task string
		reads      bool
	}{
		{"an item's slot", `{"items":[{"answer_slots":[{"id":"a","binding":"anywhere"}]}]}`, true},
		{"a slot in a line", `{"items":[{"line":[{"answer_slot":{"id":"a","binding":"anywhere"}}]}]}`, true},
		{"slots by tap", `{"items":[{"answer_slots":[{"id":"a","binding":"choice","options":["1","2"]}]}]}`, false},
		{"no slots", `{"items":[]}`, false},
	} {
		start, err := json.Marshal(map[string]any{"attempt_id": "x", "task": map[string]any{"json": []byte(tc.task)}})
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "-")+".json")
		if err := os.WriteFile(path, start, 0o600); err != nil {
			t.Fatal(err)
		}
		_, stderr, err := bash(t, nil, `handwritten "`+path+`"`)
		if (err == nil) != tc.reads {
			t.Errorf("%s: handwritten = %v, %s; want AI reads it %v", tc.name, err, stderr, tc.reads)
		}
	}
}

// The check knows a workbook page by its canvas, and sends a crop in every box of it, by the places
// of the item and of the box among the item's boxes, from 1, past the text of the line (R81).
func TestAWorkbookPageIsToldAndEachOfItsBoxesSent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write := func(name, task string) string {
		t.Helper()
		start, err := json.Marshal(map[string]any{"attempt_id": "x", "task": map[string]any{"json": []byte(task)}})
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, name+".json")
		if err := os.WriteFile(path, start, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	book := write("book", `{"canvas":{"kind":"workbook"},"items":[{"line":[{"text":"3 +"},{"binding":"box"},{"text":"="},{"binding":"box"}]},`+
		`{"line":[{"text":"no box"}]},{"line":[{"binding":"box"},{"text":"× 2"}]}]}`)
	sheet := write("sheet", `{"canvas":{"kind":"handwriting_sheet"},"items":[{"answer_slots":[{"id":"a","binding":"anywhere"}]}]}`)
	if _, stderr, err := bash(t, nil, `on_workbook "`+book+`"`); err != nil {
		t.Errorf("on_workbook of a workbook page = %v, %s; want it told", err, stderr)
	}
	if _, _, err := bash(t, nil, `on_workbook "`+sheet+`"`); err == nil {
		t.Error("on_workbook of a sheet told it a workbook page")
	}
	stdout, stderr, err := bash(t, nil, `page "`+book+`" CROP`)
	want := `[{"answer_slot":{"item":1,"slot":1},"crop":"CROP"},{"answer_slot":{"item":1,"slot":2},"crop":"CROP"},` +
		`{"answer_slot":{"item":3,"slot":1},"crop":"CROP"}]`
	if err != nil || strings.TrimSpace(stdout) != want {
		t.Errorf("page = %q, %q, %v; want %s", stdout, stderr, err, want)
	}
}

// The check knows an essay by its answer slot bound to the sheet, and tells it from a task AI reads
// anywhere on the sheet and from one checked by tap (R82).
func TestAnEssayIsTold(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, tc := range []struct {
		name, task string
		essay      bool
	}{
		{"an essay", `{"canvas":{"kind":"handwriting_sheet"},"items":[{"answer_slots":[{"id":"a","binding":"sheet"}]}]}`, true},
		{"a sheet read anywhere", `{"canvas":{"kind":"handwriting_sheet"},"items":[{"answer_slots":[{"id":"a","binding":"anywhere"}]}]}`, false},
		{"slots by tap", `{"items":[{"answer_slots":[{"id":"a","binding":"choice","options":["1","2"]}]}]}`, false},
	} {
		start, err := json.Marshal(map[string]any{"attempt_id": "x", "task": map[string]any{"json": []byte(tc.task)}})
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "-")+".json")
		if err := os.WriteFile(path, start, 0o600); err != nil {
			t.Fatal(err)
		}
		_, stderr, err := bash(t, nil, `on_essay "`+path+`"`)
		if (err == nil) != tc.essay {
			t.Errorf("%s: on_essay = %v, %s; want an essay %v", tc.name, err, stderr, tc.essay)
		}
	}
}

// What an export's file holds of the quota is its counters summed over their days, and nothing when
// it holds none.
func TestTheCountersOfAnExportAreSummed(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, tc := range []struct {
		name, file, want string
	}{
		{"two days", `{"quota_counters":[{"day_start":"2026-11-11T00:00:00Z","spent_micro_usd":9300,"reserved_micro_usd":0},` +
			`{"day_start":"2026-11-12T00:00:00Z","spent_micro_usd":14000,"reserved_micro_usd":18200}]}`, "23300 18200"},
		{"no counter", `{"quota_counters":[]}`, "0 0"},
		{"no field", `{}`, "0 0"},
	} {
		path := filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "-")+".json")
		if err := os.WriteFile(path, []byte(tc.file), 0o600); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, err := bash(t, nil, `counted "`+path+`"`)
		if err != nil || strings.TrimSpace(stdout) != tc.want {
			t.Errorf("%s: counted = %q, %q, %v; want %q", tc.name, stdout, stderr, err, tc.want)
		}
	}
}

// `just dev` runs on the stub of tutor with no key of platform AI from the dev file, and `just
// dev-ai` and the check with the key and no stub (R66).
func TestARunIsOnTheStubOrOnTheModels(t *testing.T) {
	t.Parallel()

	env := []string{"API_ANTHROPIC_KEY=sk-dev", "API_ANTHROPIC_KEY_FILE=/run/secrets/anthropic", "API_TUTOR_STUB=1"}
	show := `printf '%s|%s|%s' "${API_ANTHROPIC_KEY-unset}" "${API_ANTHROPIC_KEY_FILE-unset}" "${API_TUTOR_STUB-unset}"`
	for fn, want := range map[string]string{
		"stubbed":  "unset|unset|1",
		"modelled": "sk-dev|/run/secrets/anthropic|unset",
	} {
		stdout, stderr, err := bash(t, env, fn+" && "+show)
		if err != nil || stdout != want {
			t.Errorf("%s = %q, %q, %v; want %s", fn, stdout, stderr, err, want)
		}
	}
	// The stub is exported to the binary.
	stdout, _, err := bash(t, nil, `stubbed && bash -c 'printf %s "$API_TUTOR_STUB"'`)
	if err != nil || stdout != "1" {
		t.Errorf("the binary sees API_TUTOR_STUB = %q, %v; want 1", stdout, err)
	}
}

// The check's account gets the tariff dev_check (R55): an account whose code <UID>.code beside the
// dev file keeps by `api tariff`, one with no code kept deleted and made anew, and a new account's
// code kept in the file, readable by its owner alone. A tariff refused, or a deletion not finished,
// stops the check, naming why.
func TestTheChecksAccountIsOnTheTariffOfDevCheck(t *testing.T) {
	t.Parallel()

	const kept, made = "0123-4567-89AB-CDEF", "FEDC-BA98-7654-3210"
	// The steps the server and the binary would take, as lines of the file calls: GetAccount answers
	// $ACCOUNT, and $GONE once the account is deleted.
	fakes := `work=$DIR; binary=$DIR/api
call() {
    echo "$1" >> "$DIR/calls"; status=200
    case $1 in GetAccount) status=$ACCOUNT ;; DeleteAccount) ACCOUNT=$GONE ;; esac
}
id_token() { echo "id_token $1" >> "$DIR/calls"; printf new-token; }
owner_account() { echo "owner_account $1" >> "$DIR/calls"; [[ -z $MADE ]] || keep_code "$MADE" "$2"; }
check_account && printf '%s' "${token:-}"`
	// world is a folder with the dev file's place, the fake binary and the code kept, if any.
	world := func(t *testing.T, file, binary string) (dir, code string) {
		t.Helper()
		dir = t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "api"), []byte(binary), 0o700); err != nil {
			t.Fatal(err)
		}
		code = filepath.Join(dir, "dev-author.code")
		if file != "" {
			if err := os.WriteFile(code, []byte(file+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return dir, code
	}
	recording := "#!/bin/sh\necho \"$@\" >> \"$DIR/calls\"\n"
	env := func(dir, account, gone, made string) []string {
		return []string{"DIR=" + dir, "API_DEV_CONFIG=" + filepath.Join(dir, "dev.env"), "API_DEV_UID=",
			"ACCOUNT=" + account, "GONE=" + gone, "MADE=" + made}
	}
	for _, tc := range []struct {
		name, account, file, made string
		calls                     []string
		keeps, token              string
	}{
		{"a code kept", "200", kept, "", []string{"GetAccount", "tariff -tariff dev_check " + kept, "owner_account dev_check"}, kept, ""},
		{"no code kept", "200", "", made,
			[]string{"GetAccount", "DeleteAccount", "id_token dev-author", "GetAccount", "owner_account dev_check"}, made, "new-token"},
		{"no account", "403", "", made, []string{"GetAccount", "owner_account dev_check"}, made, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir, code := world(t, tc.file, recording)
			stdout, stderr, err := bash(t, env(dir, tc.account, "403", tc.made), fakes)
			if err != nil {
				t.Fatalf("check_account: %v, %s", err, stderr)
			}
			if last := stdout[strings.LastIndex(stdout, "\n")+1:]; last != tc.token {
				t.Errorf("the token after = %q, want %q", last, tc.token)
			}
			calls, err := os.ReadFile(filepath.Join(dir, "calls"))
			if err != nil || strings.TrimSpace(string(calls)) != strings.Join(tc.calls, "\n") {
				t.Errorf("the steps = %q (%v), want %q", calls, err, tc.calls)
			}
			got, err := os.ReadFile(code)
			if err != nil || strings.TrimSpace(string(got)) != tc.keeps {
				t.Errorf("the file keeps %q (%v), want %q", got, err, tc.keeps)
			}
			if info, err := os.Stat(code); err != nil {
				t.Errorf("the file: %v", err)
			} else if mode := info.Mode().Perm(); mode != 0o600 {
				t.Errorf("the file's mode = %o, want 600", mode)
			}
		})
	}

	dir, code := world(t, kept, "#!/bin/sh\necho no account redeemed this code >&2\nexit 1\n")
	_, stderr, err := bash(t, env(dir, "200", "403", ""), fakes)
	if err == nil || !strings.Contains(stderr, "no account redeemed") || !strings.Contains(stderr, code) {
		t.Errorf("check_account with the tariff refused = %q, %v; want a failure naming the refusal and the file", stderr, err)
	}
	dir, _ = world(t, "", recording)
	_, stderr, err = bash(t, env(dir, "200", "401", made), fakes)
	if err == nil || !strings.Contains(stderr, "not finished") || !strings.Contains(stderr, "just dev sweep") {
		t.Errorf("check_account with the deletion not finished = %q, %v; want a failure naming the sweep", stderr, err)
	}
}

// The run for app has one synthetic child under 13 (R67): a child of the pseudonym the account
// holds who learns is taken as it is, and with none — or only one who has turned 13 or waits for a
// consent — the consents with no profile are taken back and a child of 9 is made by a parent's
// consent sent with its form and confirmed by the operator's command, then a profile. Its ID is
// printed either way. A consent the command does not confirm stops the run.
func TestTheSyntheticChildOfDevAppIsMadeOnce(t *testing.T) {
	t.Parallel()

	// The steps the server and the binary would take, as lines of the file calls: GetAccount answers
	// $ACCOUNT, and each body sent is kept in a file of its method.
	fakes := `work=$DIR; binary=$DIR/api; who=dev-app
call() {
    echo "$1" >> "$DIR/calls"; status=200
    cat > "$DIR/$1.json"
    case $1 in
    GetAccount) printf '%s' "$ACCOUNT" ;;
    RequestChildConsent) printf '{"consent":{"consent_id":"consent-new","request_code":"ABCDE-FGHJK"}}' ;;
    CreateChildProfile) printf '{"profile":{"profile_id":"child-new"}}' ;;
    *) printf '{}' ;;
    esac > "$work/answer.json"
}
app_child && printf '|%s' "$child"`
	texts := `{"texts":[{"id":"privacy-policy","version":"3"},{"id":"parent-consent","version":"2"}]}`
	// account is GetAccount's answer: the owner, the profiles given and the consents with no profile.
	account := func(consents string, profiles ...string) string {
		return `{"account":{"account_id":"a","child_consents":[` + consents + `],"profiles":[` +
			`{"profile_id":"owner","owner":true,"pseudonym":"Dev","age_category":"adult","consent":{"need":"CONSENT_NEED_NONE"}}` +
			strings.Join(append([]string{""}, profiles...), ",") + `]}}`
	}
	child := func(id, name, category, need string) string {
		return `{"profile_id":"` + id + `","pseudonym":"` + name + `","age_category":"` + category + `","consent":{"need":"` + need + `"}}`
	}
	const learns, waits = "CONSENT_NEED_NONE", "CONSENT_NEED_PARENT"
	world := func(t *testing.T, binary string) string {
		t.Helper()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "api"), []byte(binary), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "texts.json"), []byte(texts), 0o600); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	confirming := "#!/bin/sh\necho \"$@\" >> \"$DIR/calls\"\necho \"$3 confirmed\"\n"
	made := []string{"GetAccount", "RequestChildConsent", "SendConsentForm", "consents confirm ABCDE-FGHJK", "CreateChildProfile"}
	for _, tc := range []struct {
		name, account, child string
		calls                []string
	}{
		{"a child there", account(`{"consent_id":"consent-app"}`, child("child-there", "Dev child", "under_13", learns)), "child-there",
			[]string{"GetAccount"}},
		{"no child", account(""), "child-new", made},
		{"another child alone", account("", child("child-other", "Another", "under_13", learns)), "child-new", made},
		{"the child turned 13", account("", child("child-grown", "Dev child", "from_13_to_consent", learns)), "child-new", made},
		{"the child waits for a consent", account("", child("child-waits", "Dev child", "under_13", waits)), "child-new", made},
		{"consents left", account(`{"consent_id":"consent-old"},{"consent_id":"consent-older"}`), "child-new",
			append([]string{"GetAccount", "CancelChildConsent", "CancelChildConsent"}, made[1:]...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := world(t, confirming)
			stdout, stderr, err := bash(t, []string{"DIR=" + dir, "ACCOUNT=" + tc.account}, fakes)
			if err != nil {
				t.Fatalf("app_child: %v, %s", err, stderr)
			}
			if !strings.HasSuffix(stdout, "|"+tc.child) || !strings.Contains(stdout, "is the profile "+tc.child+"\n") {
				t.Errorf("app_child printed %q, want the profile %s named and left", stdout, tc.child)
			}
			calls, err := os.ReadFile(filepath.Join(dir, "calls"))
			if err != nil || strings.TrimSpace(string(calls)) != strings.Join(tc.calls, "\n") {
				t.Errorf("the steps = %q (%v), want %q", calls, err, tc.calls)
			}
			if len(tc.calls) == 1 {
				return
			}
			var request struct {
				Age     int               `json:"age_years"`
				Country string            `json:"country"`
				Text    map[string]string `json:"parent_consent_text"`
			}
			if body, err := os.ReadFile(filepath.Join(dir, "RequestChildConsent.json")); err != nil || json.Unmarshal(body, &request) != nil ||
				request.Age != 9 || request.Country != "US" || request.Text["id"] != "parent-consent" || request.Text["version"] != "2" {
				t.Errorf("RequestChildConsent got %+v (%v), want a child of 9 in US and the parent's consent text", request, err)
			}
			var profile struct {
				Consent  string            `json:"consent_id"`
				Settings map[string]string `json:"settings"`
			}
			if body, err := os.ReadFile(filepath.Join(dir, "CreateChildProfile.json")); err != nil || json.Unmarshal(body, &profile) != nil ||
				profile.Consent != "consent-new" || profile.Settings["pseudonym"] != "Dev child" {
				t.Errorf("CreateChildProfile got %+v (%v), want the consent confirmed and the pseudonym Dev child", profile, err)
			}
		})
	}

	dir := world(t, "#!/bin/sh\necho \"no consent of $3 waits with its form sent\"\nexit 1\n")
	_, stderr, err := bash(t, []string{"DIR=" + dir, "ACCOUNT=" + account("")}, fakes)
	if err == nil || !strings.Contains(stderr, "consents confirm failed") || !strings.Contains(stderr, "no consent of ABCDE-FGHJK") {
		t.Errorf("app_child with the consent not confirmed = %q, %v; want a failure naming why", stderr, err)
	}
	if calls, _ := os.ReadFile(filepath.Join(dir, "calls")); strings.Contains(string(calls), "CreateChildProfile") {
		t.Errorf("app_child made a profile with the consent not confirmed: %q", calls)
	}
}

// The run for app is on the stub of tutor with the facts of a child, and with API_DEV_APP_AI=1 on
// the models with the dev file's key and facts, which it leaves as they are (R67).
func TestTheRunForAppGoesOnTheStubOrOnTheModels(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	facts := filepath.Join(dir, "facts.yaml")
	show := `app_mode "` + facts + `"; echo "$app_on|${API_TUTOR_STUB:-}|${API_ANTHROPIC_KEY_FILE:-}|$API_ADMISSION_FACTS_FILE"`
	devFile := []string{"API_ANTHROPIC_KEY_FILE=/dev/key", "API_ADMISSION_FACTS_FILE=/dev/facts.yaml"}
	stdout, _, err := bash(t, devFile, show)
	if err != nil || stdout != "on the stub of tutor|1||"+facts+"\n" {
		t.Errorf("the stub: %q (%v)", stdout, err)
	}
	if written, err := os.ReadFile(facts); err != nil || !strings.Contains(string(written), "under_13") {
		t.Errorf("the facts of the stub = %q (%v), want a child's", written, err)
	}
	stdout, _, err = bash(t, append(devFile, "API_DEV_APP_AI=1", "API_TUTOR_STUB=1"), show)
	if err != nil || stdout != "on the models, with the dev file's key and facts||/dev/key|/dev/facts.yaml\n" {
		t.Errorf("the models: %q (%v)", stdout, err)
	}
}
