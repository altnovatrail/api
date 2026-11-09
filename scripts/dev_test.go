// The tests of scripts/dev.sh, the run on the dev projects (R39): its loader of the dev file, and
// the custom token it signs. They run the script's own functions in bash, with jq and openssl, as
// the devcontainer and its toolchain stage, where CI runs the tests, have them.
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

// The custom token carries Firebase's claims for one made with a JWT library, and its RS256
// signature holds under the service account's public key.
func TestTheCustomTokenIsFirebases(t *testing.T) {
	t.Parallel()

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

	stdout, stderr, err := bash(t, []string{"API_DEV_UID=dev-synthetic"}, `custom_token "`+path+`"`)
	if err != nil {
		t.Fatalf("custom_token: %v, %s", err, stderr)
	}
	parts := strings.Split(strings.TrimSpace(stdout), ".")
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
		claims.UID != "dev-synthetic" || claims.Iat < now-60 || claims.Iat > now || claims.Exp != claims.Iat+3600 {
		t.Errorf("claims = %+v, want Firebase's for a custom token of dev-synthetic, an hour long", claims)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], decode(parts[2])); err != nil {
		t.Errorf("the signature does not hold: %v", err)
	}

	// A key that is no key fails, and says so.
	if err := os.WriteFile(path, []byte(`{"client_email":"sa@synthetic.example","private_key":"none"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, stderr, err := bash(t, nil, `custom_token "`+path+`"`); err == nil || !strings.Contains(stderr, "would not sign") {
		t.Errorf("custom_token with no key = %q, %v; want a refusal", stderr, err)
	}
}
