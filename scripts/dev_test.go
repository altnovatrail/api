// The tests of scripts/dev.sh, the run on the dev projects (R39): its loader of the dev file, the
// custom token it signs, how it puts a run on the stub of tutor or on the models, and how it tells a
// task AI reads and sums the counters of the quota. They run the script's own functions in bash, with
// jq and openssl, as the devcontainer and its toolchain stage, where CI runs the tests, have them.
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
