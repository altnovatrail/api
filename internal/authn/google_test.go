package authn_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"
)

// project is the Firebase project of the tests' Google.
const project = "novatrail-test"

// keyID names the key of the tests' Google in a token's header.
const keyID = "test-key"

// The paths of the tests' Google: Google's keys for ID tokens, and the Identity
// Toolkit's calls of the adapter, as the Admin SDK v4.22.0 makes them.
const (
	keysPath     = "www.googleapis.com/robot/v1/metadata/x509/securetoken@system.gserviceaccount.com"
	toolkitPath  = "identitytoolkit.googleapis.com/v1/projects/" + project
	usersPerPage = 2
)

// fakeGoogle is a Google of the tests' own, over TLS on Google's names: the SDK
// verifies a token's signature by the key it serves and asks it of the user, as
// it would Google. The SDK takes Google's keys with a client it builds from a
// clone of http.DefaultTransport, which no option reaches, so TestMain points
// that transport here; it dials nothing else and trusts no other certificate, so
// no test reaches the network.
type fakeGoogle struct {
	transport *http.Transport
	key       *rsa.PrivateKey
	// stranger signs tokens no key of the tests' Google verifies.
	stranger *rsa.PrivateKey

	mu    sync.Mutex
	users map[string]*fakeUser
	// calls is the Identity Toolkit's calls, as "lookup:<uid>" and the like.
	calls       []string
	keysFetched int
	// keysFailFrom is the first fetch of the keys that fails; 0, none.
	keysFailFrom int
	// keysMaxAge is how long the SDK may keep the keys, in seconds.
	keysMaxAge int
	failRevoke bool
	failDelete bool
	failList   bool
}

// fakeUser is a user of the tests' Google.
type fakeUser struct {
	disabled bool
	// validSince is when the user's tokens were last revoked, in seconds.
	validSince int64
	// lastLoginAt is in milliseconds; lastRefreshAt is RFC 3339, as Google has them.
	lastLoginAt   int64
	lastRefreshAt string
	// createdAt is in milliseconds, as Google has it.
	createdAt int64
	// broken makes a lookup of the user fail.
	broken bool
}

// google is the package's Google, which TestMain starts.
var google *fakeGoogle

func TestMain(m *testing.M) {
	server, err := startGoogle()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	server.Close()
	os.Exit(code)
}

func startGoogle() (*httptest.Server, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	stranger, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "the tests' Google"},
		DNSNames:     []string{"www.googleapis.com", "identitytoolkit.googleapis.com"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}

	g := &fakeGoogle{key: key, stranger: stranger, keysMaxAge: 3600}
	g.reset()
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+keysPath, func(w http.ResponseWriter, _ *http.Request) { g.keys(w, certPEM) })
	mux.HandleFunc("POST "+toolkitPath+"/accounts:lookup", g.lookup)
	mux.HandleFunc("POST "+toolkitPath+"/accounts:update", g.revoke)
	mux.HandleFunc("POST "+toolkitPath+"/accounts:delete", g.remove)
	mux.HandleFunc("GET "+toolkitPath+"/accounts:batchGet", g.batchGet)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "the tests' Google has no "+r.Host+r.URL.Path, http.StatusNotFound)
	})
	server := httptest.NewUnstartedServer(mux)
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
		MinVersion:   tls.VersionTLS12,
	}
	server.StartTLS()

	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	address := server.Listener.Addr().String()
	g.transport = &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, address)
		},
	}
	http.DefaultTransport = g.transport
	google = g
	return server, nil
}

// reset forgets what the last test did.
func (g *fakeGoogle) reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.users = map[string]*fakeUser{}
	g.calls = nil
	g.keysFetched, g.keysFailFrom, g.keysMaxAge = 0, 0, 3600
	g.failRevoke, g.failDelete, g.failList = false, false, false
}

// at sets the tests' Google up for one test, which may not run in parallel with
// another: there is one Google.
func at(t *testing.T) *fakeGoogle {
	t.Helper()
	google.reset()
	t.Cleanup(google.reset)
	return google
}

// set changes how the tests' Google answers, under its lock: its handlers read it
// on goroutines of their own.
func (g *fakeGoogle) set(change func(*fakeGoogle)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	change(g)
}

func (g *fakeGoogle) fetched() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.keysFetched
}

func (g *fakeGoogle) addUser(uid string, u fakeUser) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.users[uid] = &u
}

func (g *fakeGoogle) has(uid string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, there := g.users[uid]
	return there
}

func (g *fakeGoogle) callsMade() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.calls)
}

func (g *fakeGoogle) keys(w http.ResponseWriter, certPEM []byte) {
	g.mu.Lock()
	g.keysFetched++
	failed := g.keysFailFrom != 0 && g.keysFetched >= g.keysFailFrom
	maxAge := g.keysMaxAge
	g.mu.Unlock()
	if failed {
		http.Error(w, "no keys", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(maxAge))
	_ = json.NewEncoder(w).Encode(map[string]string{keyID: string(certPEM)})
}

// refuse answers as the Identity Toolkit refuses: by a code in the message.
func refuse(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": status, "message": code}})
}

func (g *fakeGoogle) lookup(w http.ResponseWriter, r *http.Request) {
	var asked struct {
		LocalID []string `json:"localId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&asked); err != nil || len(asked.LocalID) != 1 {
		refuse(w, http.StatusBadRequest, "INVALID_ARGUMENT")
		return
	}
	uid := asked.LocalID[0]
	g.mu.Lock()
	g.calls = append(g.calls, "lookup:"+uid)
	u, there := g.users[uid]
	var found []any
	if there && !u.broken {
		found = append(found, g.record(uid, u))
	}
	g.mu.Unlock()
	if there && u.broken {
		refuse(w, http.StatusInternalServerError, "INTERNAL_ERROR")
		return
	}
	// A user who is not there is an answer with no users, as Google gives it.
	_ = json.NewEncoder(w).Encode(map[string]any{"users": found})
}

// record is a user as Google gives one: with an email, which api does not read.
func (g *fakeGoogle) record(uid string, u *fakeUser) map[string]any {
	record := map[string]any{
		"localId":  uid,
		"email":    uid + "@example.com",
		"disabled": u.disabled,
	}
	if u.validSince != 0 {
		record["validSince"] = strconv.FormatInt(u.validSince, 10)
	}
	if u.lastLoginAt != 0 {
		record["lastLoginAt"] = strconv.FormatInt(u.lastLoginAt, 10)
	}
	if u.lastRefreshAt != "" {
		record["lastRefreshAt"] = u.lastRefreshAt
	}
	if u.createdAt != 0 {
		record["createdAt"] = strconv.FormatInt(u.createdAt, 10)
	}
	return record
}

func (g *fakeGoogle) revoke(w http.ResponseWriter, r *http.Request) {
	var asked struct {
		LocalID    string `json:"localId"`
		ValidSince string `json:"validSince"`
	}
	if err := json.NewDecoder(r.Body).Decode(&asked); err != nil {
		refuse(w, http.StatusBadRequest, "INVALID_ARGUMENT")
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, "revoke:"+asked.LocalID)
	u, there := g.users[asked.LocalID]
	switch {
	case g.failRevoke:
		refuse(w, http.StatusInternalServerError, "INTERNAL_ERROR")
	case !there:
		refuse(w, http.StatusBadRequest, "USER_NOT_FOUND")
	default:
		u.validSince, _ = strconv.ParseInt(asked.ValidSince, 10, 64)
		_ = json.NewEncoder(w).Encode(map[string]any{"localId": asked.LocalID})
	}
}

func (g *fakeGoogle) remove(w http.ResponseWriter, r *http.Request) {
	var asked struct {
		LocalID string `json:"localId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&asked); err != nil {
		refuse(w, http.StatusBadRequest, "INVALID_ARGUMENT")
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, "delete:"+asked.LocalID)
	_, there := g.users[asked.LocalID]
	switch {
	case g.failDelete:
		refuse(w, http.StatusInternalServerError, "INTERNAL_ERROR")
	case !there:
		refuse(w, http.StatusBadRequest, "USER_NOT_FOUND")
	default:
		delete(g.users, asked.LocalID)
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}
}

// batchGet gives the users usersPerPage at a time, in the order of their UIDs.
func (g *fakeGoogle) batchGet(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, "list:"+r.URL.Query().Get("nextPageToken"))
	if g.failList {
		refuse(w, http.StatusInternalServerError, "INTERNAL_ERROR")
		return
	}
	uids := make([]string, 0, len(g.users))
	for uid := range g.users {
		uids = append(uids, uid)
	}
	slices.Sort(uids)
	from, _ := strconv.Atoi(r.URL.Query().Get("nextPageToken"))
	to := min(from+usersPerPage, len(uids))
	page := map[string]any{}
	var records []any
	for _, uid := range uids[from:to] {
		records = append(records, g.record(uid, g.users[uid]))
	}
	page["users"] = records
	if to < len(uids) {
		page["nextPageToken"] = strconv.Itoa(to)
	}
	_ = json.NewEncoder(w).Encode(page)
}

// claims are a token's claims, which a test changes for the token it needs.
type claims map[string]any

// claimsOf are the claims of a token Firebase Auth issues for uid now, signed in
// at authTime: with an email, which api does not read.
func claimsOf(uid string, authTime time.Time) claims {
	now := time.Now().Unix()
	return claims{
		"iss":       "https://securetoken.google.com/" + project,
		"aud":       project,
		"sub":       uid,
		"user_id":   uid,
		"iat":       now,
		"exp":       now + 3600,
		"auth_time": authTime.Unix(),
		"email":     uid + "@example.com",
		"firebase":  map[string]any{"sign_in_provider": "google.com"},
	}
}

// sign is the ID token of these claims, signed by key under the key ID kid.
func sign(t *testing.T, key *rsa.PrivateKey, kid string, c claims) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": "RS256", "kid": kid, "typ": "JWT"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	signed := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(signed))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return signed + "." + base64.RawURLEncoding.EncodeToString(signature)
}

// token is the ID token Firebase Auth issues for uid, signed in at authTime.
func (g *fakeGoogle) token(t *testing.T, uid string, authTime time.Time) string {
	t.Helper()
	return sign(t, g.key, keyID, claimsOf(uid, authTime))
}
