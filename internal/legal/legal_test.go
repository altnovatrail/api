package legal_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"github.com/altnovatrail/spec"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"
	"github.com/altnovatrail/spec/types/legalv3"

	"github.com/altnovatrail/api/internal/consentage"
	"github.com/altnovatrail/api/internal/legal"
)

func partOf(passport []byte, files map[string]string) legal.Part {
	part := legal.Part{Passport: passport, Files: map[string][]byte{}}
	for path, text := range files {
		part.Files[path] = []byte(text)
	}
	return part
}

// at is the moment of an RFC 3339 time.
func at(t *testing.T, moment string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, moment)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

// Every vector of reading of the part gives what section 5 gives on its day:
// the current and the next versions of the texts of a part accepted, or the
// step that refuses it — and, for a rule, the names it gives. The names of the
// schema's refusal are its library's, and only the step is held to the vector.
// api serves no set, so the versions of the sets are not held.
func TestTheVectorsOfReading(t *testing.T) {
	t.Parallel()

	entries, err := fs.ReadDir(spec.Legal, "vectors/reading")
	if err != nil {
		t.Fatal(err)
	}
	cases, refusedByOperator := 0, 0
	for _, entry := range entries {
		data, err := fs.ReadFile(spec.Legal, "vectors/reading/"+entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		var vector legalv3.ReadingVector
		if err := consentage.FromYAML(data, &vector); err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
		for i, c := range vector.Cases {
			cases++
			var previous *legal.Part
			if c.Previous != nil {
				p := partOf(c.Previous.Passport, c.Previous.Files)
				previous = &p
			}
			passport, err := legal.Read(partOf(c.Passport, c.Files), previous, c.Today)
			where := fmt.Sprintf("%s case %d", entry.Name(), i)
			var refusal *legal.Refusal
			switch {
			case c.RefusedBy == nil && err != nil:
				t.Errorf("%s: refused %v, want it accepted", where, err)
			case c.RefusedBy == nil:
				current, next := legal.Versions(passport, c.Today)
				if !maps.Equal(current, map[string]int64(c.Current.Texts)) || c.Next == nil ||
					!maps.Equal(next, map[string]int64(c.Next.Texts)) {
					t.Errorf("%s: on %s the current versions are %v and the next %v, want %v and %v",
						where, c.Today, current, next, c.Current.Texts, c.Next)
				}
			case !errors.As(err, &refusal):
				t.Errorf("%s: %v, want refused by %s", where, err, *c.RefusedBy)
			case refusal.Step != string(*c.RefusedBy):
				t.Errorf("%s: refused by %s %v, want %s %v", where, refusal.Step, refusal.Names, *c.RefusedBy, c.Names)
			case refusal.Step != legal.StepSchema && !slices.Equal(refusal.Names, c.Names):
				t.Errorf("%s: %s names %v, want %v", where, refusal.Step, refusal.Names, c.Names)
			}
			if c.RefusedBy != nil && *c.RefusedBy == legalv3.CaseRefusedByLegalOperator {
				refusedByOperator++
			}
		}
	}
	if cases == 0 || refusedByOperator == 0 {
		t.Fatalf("%d cases read, %d of legal.operator: want both", cases, refusedByOperator)
	}
}

// The texts of the tag read, and GetLegalTexts gives those of the place app, the
// current and the next, by ID, with the summary and the translation in the
// language asked, by the lookup of RFC 4647, and English otherwise.
func TestTheTextsOfTheTag(t *testing.T) {
	t.Parallel()

	before := at(t, "2026-11-20T12:00:00Z")
	texts, err := legal.Load(before)
	if err != nil {
		t.Fatal(err)
	}
	service := legal.NewService(texts, legal.WithClock(func() time.Time { return before }))
	for _, tc := range []struct {
		tag, summary, translation string
	}{
		{"", "en", ""},
		{"en-US", "en", ""},
		{"ru", "ru", "ru"},
		{"RU-ru", "ru", "ru"},
		{"fr-CA", "en", ""},
	} {
		answer, err := service.GetLegalTexts(context.Background(), &v1.GetLegalTextsRequest{InterfaceLanguage: tc.tag})
		if err != nil {
			t.Fatalf("%q: %v", tc.tag, err)
		}
		for which, list := range map[string][]*v1.LegalText{"texts": answer.GetTexts(), "next_texts": answer.GetNextTexts()} {
			var ids []string
			for _, text := range list {
				ids = append(ids, text.GetId())
				if text.GetSummaryLanguage() != tc.summary || text.GetSummary() == "" || !strings.HasPrefix(text.GetBody(), "# ") {
					t.Errorf("%q: %s %s gives %q in %q, want a summary in %q", tc.tag, which, text.GetId(), text.GetSummary(),
						text.GetSummaryLanguage(), tc.summary)
				}
				if text.GetId() == legal.PrivacyPolicy && text.GetTranslationLanguage() != tc.translation {
					t.Errorf("%q: the translation of the policy in %s is in %q, want %q", tc.tag, which,
						text.GetTranslationLanguage(), tc.translation)
				}
				if text.GetTranslationLanguage() == "" && text.GetTranslation() != "" {
					t.Errorf("%q: %s %s has a translation of no language", tc.tag, which, text.GetId())
				}
			}
			if !slices.IsSorted(ids) {
				t.Errorf("%q: %s %v, want them by ID", tc.tag, which, ids)
			}
		}
		var ids []string
		for _, text := range answer.GetTexts() {
			ids = append(ids, text.GetId())
		}
		if !slices.Contains(ids, legal.PrivacyPolicy) || !slices.Contains(ids, legal.TermsOfUse) {
			t.Errorf("%q: texts %v, want the policy and the terms", tc.tag, ids)
		}
	}
	if _, err := service.GetLegalTexts(context.Background(), &v1.GetLegalTextsRequest{InterfaceLanguage: "not a tag"}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("a tag that is none: %v, want invalid_argument", err)
	}
}

// Version 3 of the privacy policy of the tag applies from 2026-11-27: the day
// before, GetLegalTexts gives it as the next version, kept no longer than until
// midnight UTC, and a request that names it is refused, the current one being
// version 2; from that day on it is the current version (R130 spec).
func TestThePolicyOfTheTagByTheDay(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		moment          string
		current, next   int32
		keptFor         time.Duration
		header          string
		namesTwo, three bool
	}{
		{"2026-11-26T12:00:00Z", 2, 3, time.Hour, "public, max-age=3600", true, false},
		{"2026-11-26T23:30:00Z", 2, 3, 30 * time.Minute, "public, max-age=1800", true, false},
		{"2026-11-26T23:59:59.5Z", 2, 3, 500 * time.Millisecond, "public, max-age=0", true, false},
		{"2026-11-27T00:00:00Z", 3, 0, time.Hour, "public, max-age=3600", false, true},
		{"2026-11-27T01:00:00+02:00", 2, 3, time.Hour, "public, max-age=3600", true, false},
	} {
		now := at(t, tc.moment)
		day := legal.Spec().On(now)
		if v, ok := day.Current(legal.PrivacyPolicy); !ok || v != tc.current {
			t.Errorf("%s: the current policy = %d, %v; want %d", tc.moment, v, ok, tc.current)
		}
		var next []string
		for _, text := range day.ListNext("") {
			next = append(next, fmt.Sprintf("%s %d %s", text.GetId(), text.GetVersion(), text.GetInForceSince()))
		}
		want := []string{}
		if tc.next != 0 {
			want = []string{fmt.Sprintf("%s %d 2026-11-27", legal.PrivacyPolicy, tc.next)}
		}
		if !slices.Equal(next, want) {
			t.Errorf("%s: the next texts = %v, want %v", tc.moment, next, want)
		}
		if got := day.KeptFor(); got != tc.keptFor {
			t.Errorf("%s: kept for %v, want %v", tc.moment, got, tc.keptFor)
		}
		terms, _ := day.Current(legal.TermsOfUse)
		ref := func(version int32) []*v1.LegalTextRef {
			return []*v1.LegalTextRef{{Id: legal.PrivacyPolicy, Version: version}, {Id: legal.TermsOfUse, Version: terms}}
		}
		two := day.Names(ref(2), legal.PrivacyPolicy, legal.TermsOfUse)
		three := day.Names(ref(3), legal.PrivacyPolicy, legal.TermsOfUse)
		if two != tc.namesTwo || three != tc.three {
			t.Errorf("%s: a request may name version 2: %v, version 3: %v; want %v and %v", tc.moment,
				two, three, tc.namesTwo, tc.three)
		}

		server := connect.NewServer()
		apiv1connect.RegisterLegalServiceHandler(server, legal.NewService(legal.Spec(), legal.WithClock(func() time.Time { return now })))
		mux := http.NewServeMux()
		connecthttp.Mount(mux, server)
		httpServer := httptest.NewServer(mux)
		res, err := http.Post(httpServer.URL+apiv1connect.LegalServiceGetLegalTextsProcedure, "application/json", strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		httpServer.Close()
		if res.StatusCode != http.StatusOK || res.Header.Get("Cache-Control") != tc.header {
			t.Errorf("%s: GetLegalTexts = %d, Cache-Control %q; want %q", tc.moment, res.StatusCode,
				res.Header.Get("Cache-Control"), tc.header)
		}
	}
}

// A request names the current version of each text it is given to that has
// one, and no other text.
func TestARequestNamesTheCurrentVersions(t *testing.T) {
	t.Parallel()

	now := time.Now()
	texts, err := legal.Load(now)
	if err != nil {
		t.Fatal(err)
	}
	day := texts.On(now)
	policy, _ := day.Current(legal.PrivacyPolicy)
	terms, _ := day.Current(legal.TermsOfUse)
	ref := func(id string, version int32) *v1.LegalTextRef { return &v1.LegalTextRef{Id: id, Version: version} }
	for name, tc := range map[string]struct {
		refs []*v1.LegalTextRef
		want bool
	}{
		"both current":   {[]*v1.LegalTextRef{ref(legal.TermsOfUse, terms), ref(legal.PrivacyPolicy, policy)}, true},
		"one left out":   {[]*v1.LegalTextRef{ref(legal.PrivacyPolicy, policy)}, false},
		"an old version": {[]*v1.LegalTextRef{ref(legal.TermsOfUse, terms), ref(legal.PrivacyPolicy, policy-1)}, false},
		"named twice":    {[]*v1.LegalTextRef{ref(legal.PrivacyPolicy, policy), ref(legal.PrivacyPolicy, policy)}, false},
		"another text": {[]*v1.LegalTextRef{ref(legal.TermsOfUse, terms), ref(legal.PrivacyPolicy, policy),
			ref(legal.StorageConsent, 1)}, false},
		"none": {nil, false},
	} {
		if got := day.Names(tc.refs, legal.PrivacyPolicy, legal.TermsOfUse); got != tc.want {
			t.Errorf("%s: Names = %v, want %v", name, got, tc.want)
		}
	}
	storage, _ := day.Current(legal.StorageConsent)
	if !day.NamesOne(ref(legal.StorageConsent, storage), legal.StorageConsent) || day.NamesOne(nil, legal.StorageConsent) {
		t.Error("NamesOne does not hold a text with a current version to it")
	}
	if !day.NamesOne(nil, "no-such-text") || day.NamesOne(ref("no-such-text", 1), "no-such-text") {
		t.Error("NamesOne does not let a text with no current version go unnamed")
	}
}

// copyOf is a copy's folder of the tag's texts, VERSION, legal.yaml and texts/.
func copyOf(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	err := fs.WalkDir(spec.Legal, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path != "." && path != "VERSION" && path != "legal.yaml" && path != "texts" && !strings.HasPrefix(path, "texts/") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dir, path), 0o755)
		}
		data, err := fs.ReadFile(spec.Legal, path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, path), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// edit replaces old with new in the file name of dir, once, and fails when the
// file does not hold old.
func edit(t *testing.T, dir, name, old, new string) {
	t.Helper()
	path := filepath.Join(dir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), old) {
		t.Fatalf("%s holds no %q", name, old)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), old, new, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A copy's folder is read by the same rules on the day it is read, and refused
// when it holds more than VERSION, legal.yaml and texts/, is of another version,
// or breaks a rule — legal.operator among them: a policy without the section
// "Who we are", or none whose day has come.
func TestACopysFolder(t *testing.T) {
	t.Parallel()

	now := at(t, "2026-11-20T12:00:00Z")
	if _, err := legal.LoadDir(copyOf(t), now); err != nil {
		t.Fatalf("the copy of the tag's texts: %v", err)
	}
	for name, spoil := range map[string]func(dir string) error{
		"a file more": func(dir string) error { return os.WriteFile(filepath.Join(dir, "README.md"), []byte("#"), 0o644) },
		"another version": func(dir string) error {
			return os.WriteFile(filepath.Join(dir, "VERSION"), []byte("2\n"), 0o644)
		},
		"no passport": func(dir string) error { return os.Remove(filepath.Join(dir, "legal.yaml")) },
		"no YAML": func(dir string) error {
			return os.WriteFile(filepath.Join(dir, "legal.yaml"), []byte("texts: ["), 0o644)
		},
		"a link": func(dir string) error {
			return os.WriteFile(filepath.Join(dir, "texts/terms-of-use/1.md"), []byte("# Terms\n\nSee https://example.org.\n"), 0o644)
		},
	} {
		dir := copyOf(t)
		if err := spoil(dir); err != nil {
			t.Fatal(err)
		}
		if _, err := legal.LoadDir(dir, now); err == nil {
			t.Errorf("%s: LoadDir = nil error, want a refusal", name)
		}
	}
	if _, err := legal.LoadDir(filepath.Join(t.TempDir(), "none"), now); err == nil {
		t.Error("a folder that is not there: nil error, want a refusal")
	}

	noSection := copyOf(t)
	edit(t, noSection, "texts/privacy-policy/1.md", "## Who we are", "## Who We Are")
	var refusal *legal.Refusal
	if _, err := legal.LoadDir(noSection, now); !errors.As(err, &refusal) || refusal.Step != legal.RuleOperator ||
		!slices.Equal(refusal.Names, []string{"texts/privacy-policy/1.md"}) {
		t.Errorf("a policy without the section: %v, want legal.operator naming its file", err)
	}
	if _, err := legal.LoadDir(copyOf(t), at(t, "2026-11-09T23:59:59Z")); !errors.As(err, &refusal) ||
		refusal.Step != legal.RuleOperator || !slices.Equal(refusal.Names, []string{"texts.privacy-policy"}) {
		t.Errorf("a policy whose day has not come: %v, want legal.operator naming the policy", err)
	}
}

// A text whose one version in force applies from a later day has no current
// version: GetLegalTexts gives it among the next texts alone, a request names
// it not, and a next version further than the next midnight keeps the answer
// for an hour.
func TestATextThatAppliesLater(t *testing.T) {
	t.Parallel()

	dir := copyOf(t)
	edit(t, dir, "legal.yaml", "  terms-of-use:\n    place: app\n    versions:\n      - version: 1\n        status: in_force\n        in_force_since: \"2026-11-10\"",
		"  terms-of-use:\n    place: app\n    versions:\n      - version: 1\n        status: in_force\n        in_force_since: \"2026-12-01\"")
	now := at(t, "2026-11-27T23:30:00Z")
	texts, err := legal.LoadDir(dir, now)
	if err != nil {
		t.Fatal(err)
	}
	day := texts.On(now)
	if _, ok := day.Current(legal.TermsOfUse); ok {
		t.Error("the terms are current before their day")
	}
	var next []string
	for _, text := range day.ListNext("") {
		next = append(next, text.GetId())
	}
	if !slices.Equal(next, []string{legal.TermsOfUse}) || day.KeptFor() != time.Hour {
		t.Errorf("the next texts = %v, kept for %v; want the terms alone, an hour", next, day.KeptFor())
	}
	for _, text := range day.List("") {
		if text.GetId() == legal.TermsOfUse {
			t.Error("the terms are among the current texts")
		}
	}
	policy, _ := day.Current(legal.PrivacyPolicy)
	if !day.Names([]*v1.LegalTextRef{{Id: legal.PrivacyPolicy, Version: policy}}, legal.PrivacyPolicy, legal.TermsOfUse) ||
		len(day.Refs(legal.PrivacyPolicy, legal.TermsOfUse)) != 1 ||
		day.Names([]*v1.LegalTextRef{{Id: legal.PrivacyPolicy, Version: policy}, {Id: legal.TermsOfUse, Version: 1}},
			legal.PrivacyPolicy, legal.TermsOfUse) {
		t.Error("a request names the terms before their day")
	}
	if on := texts.On(at(t, "2026-12-01T00:00:00Z")); len(on.ListNext("")) != 0 {
		t.Errorf("on their day the terms are still next: %v", on.ListNext(""))
	}
}

// What legal.markdown takes and refuses.
func TestTheMarkdownOfAText(t *testing.T) {
	t.Parallel()

	for source, want := range map[string]bool{
		"# Title\n\nWords.\n":                          true,
		"# Title\n\n[x][y] is plain text.\n":           true,
		"## Title\n":                                   false,
		"Words first.\n\n# Title\n":                    false,
		"# Title\n\n<b>bold</b>\n":                     false,
		"# Title\n\n<div>\nblock\n</div>\n":            false,
		"# Title\n\n![a picture](p.png)\n":             false,
		"# Title\n\n[a link](https://x.org)\n":         false,
		"# Title\n\n<https://x.org>\n":                 false,
		"# Title\n\nwww.example.org\n":                 false,
		"# Title\n\nwrite to a@example.org\n":          false,
		"# Title\n\n[y]: https://x.org\n":              false,
		"# Title\n\n| a | b |\n|---|---|\n| 1 | 2 |\n": true,
	} {
		if got := legal.Markdown([]byte(source)); got != want {
			t.Errorf("Markdown(%q) = %v, want %v", source, got, want)
		}
	}
}

// What the section "Who we are" of legal.operator is: a heading of the second
// level whose text is exactly that, not inside another block.
func TestTheSectionWhoWeAre(t *testing.T) {
	t.Parallel()

	for source, want := range map[string]bool{
		"# Policy\n\n## Who we are\n\nALT EdTech LLC.\n": true,
		"# Policy\n\n## Who we are ##\n":                 true,
		"# Policy\n\nWho we are\n----------\n":           true,
		"# Policy\n\n## Who We Are\n":                    false,
		"# Policy\n\n### Who we are\n":                   false,
		"# Policy\n\n## *Who we are*\n":                  false,
		"# Policy\n\n## Who we are, and how\n":           false,
		"# Policy\n\n> ## Who we are\n":                  false,
		"# Policy\n\nWho we are, in a line.\n":           false,
		// As spec's reader, micromark, reads them, 2026-11-14.
		"# Policy\n\n## Who&#32;we are\n":       true,
		"# Policy\n\n## &#x57;ho we&#x20;are\n": true,
		"# Policy\n\n## Who&nbsp;we are\n":      false,
		"# Policy\n\n## Who we are&amp;\n":      false,
		"# Policy\n\n## \\&#87;ho we are\n":     false,
		"# Policy\n\n## Who we are\\!\n":        false,
		"# Policy\n\n## Who we are www.\n":      false,
	} {
		if got := legal.WhoWeAre([]byte(source)); got != want {
			t.Errorf("WhoWeAre(%q) = %v, want %v", source, got, want)
		}
	}
}

// The texts of the tag are read once, and a request names their current
// versions by Refs; a passport that is no JSON is refused by the schema's step,
// and a day that is none is no refusal of the part.
func TestTheTextsOfTheTagAndTheirRefs(t *testing.T) {
	t.Parallel()

	texts := legal.Spec()
	if texts != legal.Spec() {
		t.Error("Spec reads the texts more than once")
	}
	day := texts.On(time.Now())
	refs := day.Refs(legal.TermsOfUse, "no-such-text", legal.PrivacyPolicy)
	if len(refs) != 2 || refs[0].GetId() != legal.TermsOfUse || refs[1].GetId() != legal.PrivacyPolicy ||
		!day.Names(refs, legal.PrivacyPolicy, legal.TermsOfUse) {
		t.Errorf("Refs = %v, want the two current texts, in the order asked", refs)
	}
	_, err := legal.Read(legal.Part{Passport: []byte("{")}, nil, "2026-11-20")
	var refusal *legal.Refusal
	if !errors.As(err, &refusal) || refusal.Step != legal.StepSchema || !strings.Contains(err.Error(), "breaks schema") {
		t.Errorf("a passport that is no JSON = %v, want the schema's refusal", err)
	}
	for _, none := range []string{"", "2026-11-31", "20261120", "2026-11-20T00:00:00Z"} {
		if _, err := legal.Read(legal.Part{Passport: []byte("{}")}, nil, none); err == nil || errors.As(err, &refusal) {
			t.Errorf("a day %q: %v, want an error that is no refusal", none, err)
		}
	}
	if got := legal.DayOf(at(t, "2026-11-27T01:00:00+02:00")); got != "2026-11-26" {
		t.Errorf("DayOf = %s, want the day in UTC, 2026-11-26", got)
	}
}
