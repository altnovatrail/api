package legal_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	"github.com/altnovatrail/spec"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/types/legalv2"

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

// Every vector of reading of the part gives what section 5 gives: the current
// versions of a part accepted, or the step that refuses it — and, for a rule,
// the names it gives. The names of the schema's refusal are its library's, and
// only the step is held to the vector.
func TestTheVectorsOfReading(t *testing.T) {
	t.Parallel()

	entries, err := fs.ReadDir(spec.Legal, "vectors/reading")
	if err != nil {
		t.Fatal(err)
	}
	cases := 0
	for _, entry := range entries {
		data, err := fs.ReadFile(spec.Legal, "vectors/reading/"+entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		var vector legalv2.ReadingVector
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
			passport, err := legal.Read(partOf(c.Passport, c.Files), previous)
			where := fmt.Sprintf("%s case %d", entry.Name(), i)
			var refusal *legal.Refusal
			switch {
			case c.RefusedBy == nil && err != nil:
				t.Errorf("%s: refused %v, want it accepted", where, err)
			case c.RefusedBy == nil:
				for id, want := range c.Current.Texts {
					if got := currentOf(passport.Texts[legalv2.ID(id)].Versions); got != want {
						t.Errorf("%s: the current version of %s = %d, want %d", where, id, got, want)
					}
				}
			case !errors.As(err, &refusal):
				t.Errorf("%s: %v, want refused by %s", where, err, *c.RefusedBy)
			case refusal.Step != string(*c.RefusedBy):
				t.Errorf("%s: refused by %s %v, want %s %v", where, refusal.Step, refusal.Names, *c.RefusedBy, c.Names)
			case refusal.Step != legal.StepSchema && !slices.Equal(refusal.Names, c.Names):
				t.Errorf("%s: %s names %v, want %v", where, refusal.Step, refusal.Names, c.Names)
			}
		}
	}
	if cases == 0 {
		t.Fatal("no case was read")
	}
}

func currentOf(versions []legalv2.TextVersion) int64 {
	var current int64
	for _, v := range versions {
		if v.Status == legalv2.StatusStatusInForce && v.Version > current {
			current = v.Version
		}
	}
	return current
}

// The texts of the tag read, and GetLegalTexts gives those of the place app, by
// ID, with the summary and the translation in the language asked, by the lookup
// of RFC 4647, and English otherwise, kept for an hour.
func TestTheTextsOfTheTag(t *testing.T) {
	t.Parallel()

	texts, err := legal.Load()
	if err != nil {
		t.Fatal(err)
	}
	service := legal.NewService(texts)
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
		var ids []string
		for _, text := range answer.GetTexts() {
			ids = append(ids, text.GetId())
			if text.GetSummaryLanguage() != tc.summary || text.GetSummary() == "" || !strings.HasPrefix(text.GetBody(), "# ") {
				t.Errorf("%q: %s gives %q in %q, want a summary in %q", tc.tag, text.GetId(), text.GetSummary(),
					text.GetSummaryLanguage(), tc.summary)
			}
			if text.GetId() == legal.PrivacyPolicy && text.GetTranslationLanguage() != tc.translation {
				t.Errorf("%q: the translation of the policy is in %q, want %q", tc.tag, text.GetTranslationLanguage(), tc.translation)
			}
			if text.GetTranslationLanguage() == "" && text.GetTranslation() != "" {
				t.Errorf("%q: %s has a translation of no language", tc.tag, text.GetId())
			}
		}
		if !slices.IsSorted(ids) || !slices.Contains(ids, legal.PrivacyPolicy) || !slices.Contains(ids, legal.TermsOfUse) {
			t.Errorf("%q: texts %v, want the policy and the terms, by ID", tc.tag, ids)
		}
	}
	if _, err := service.GetLegalTexts(context.Background(), &v1.GetLegalTextsRequest{InterfaceLanguage: "not a tag"}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("a tag that is none: %v, want invalid_argument", err)
	}
	if v, ok := texts.Current(legal.PrivacyPolicy); !ok || v < 1 {
		t.Errorf("Current(privacy-policy) = %d, %v; want a version in force", v, ok)
	}
}

// A request names the current version of each text in force it is given to,
// and no other text.
func TestARequestNamesTheCurrentVersions(t *testing.T) {
	t.Parallel()

	texts, err := legal.Load()
	if err != nil {
		t.Fatal(err)
	}
	policy, _ := texts.Current(legal.PrivacyPolicy)
	terms, _ := texts.Current(legal.TermsOfUse)
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
		if got := texts.Names(tc.refs, legal.PrivacyPolicy, legal.TermsOfUse); got != tc.want {
			t.Errorf("%s: Names = %v, want %v", name, got, tc.want)
		}
	}
	storage, _ := texts.Current(legal.StorageConsent)
	if !texts.NamesOne(ref(legal.StorageConsent, storage), legal.StorageConsent) || texts.NamesOne(nil, legal.StorageConsent) {
		t.Error("NamesOne does not hold a text in force to its current version")
	}
	if !texts.NamesOne(nil, "no-such-text") || texts.NamesOne(ref("no-such-text", 1), "no-such-text") {
		t.Error("NamesOne does not let a text with no version in force go unnamed")
	}
}

// A copy's folder is read by the same rules, and refused when it holds more than
// VERSION, legal.yaml and texts/, is of another version, or breaks a rule.
func TestACopysFolder(t *testing.T) {
	t.Parallel()

	copyOf := func(t *testing.T) string {
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
	dir := copyOf(t)
	if _, err := legal.LoadDir(dir); err != nil {
		t.Fatalf("the copy of the tag's texts: %v", err)
	}
	for name, spoil := range map[string]func(dir string) error{
		"a file more": func(dir string) error { return os.WriteFile(filepath.Join(dir, "README.md"), []byte("#"), 0o644) },
		"another version": func(dir string) error {
			return os.WriteFile(filepath.Join(dir, "VERSION"), []byte("1\n"), 0o644)
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
		if _, err := legal.LoadDir(dir); err == nil {
			t.Errorf("%s: LoadDir = nil error, want a refusal", name)
		}
	}
	if _, err := legal.LoadDir(filepath.Join(t.TempDir(), "none")); err == nil {
		t.Error("a folder that is not there: nil error, want a refusal")
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

// The texts of the tag are read once, and a request names their current
// versions by Refs; a passport that is no JSON is refused by the schema's step.
func TestTheTextsOfTheTagAndTheirRefs(t *testing.T) {
	t.Parallel()

	texts := legal.Spec()
	if texts != legal.Spec() {
		t.Error("Spec reads the texts more than once")
	}
	refs := texts.Refs(legal.TermsOfUse, "no-such-text", legal.PrivacyPolicy)
	if len(refs) != 2 || refs[0].GetId() != legal.TermsOfUse || refs[1].GetId() != legal.PrivacyPolicy ||
		!texts.Names(refs, legal.PrivacyPolicy, legal.TermsOfUse) {
		t.Errorf("Refs = %v, want the two texts in force, in the order asked", refs)
	}
	_, err := legal.Read(legal.Part{Passport: []byte("{")}, nil)
	var refusal *legal.Refusal
	if !errors.As(err, &refusal) || refusal.Step != legal.StepSchema || !strings.Contains(err.Error(), "breaks schema") {
		t.Errorf("a passport that is no JSON = %v, want the schema's refusal", err)
	}
}
