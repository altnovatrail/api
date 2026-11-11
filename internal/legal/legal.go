// Package legal is the legal texts api serves (R52; R130, R136 spec): the part
// legal/ of spec at the tag api pins, spec.Legal, or a self-hosted copy's folder
// of its operator's texts, read once at the start by the rules of
// legal/README.md, section 5. It answers LegalService.GetLegalTexts, and tells
// whether a request that gives a consent names the current versions of the
// texts it is given to.
package legal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
	"sync"

	"connectrpc.com/connect/v2"
	"github.com/altnovatrail/spec"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"
	"github.com/altnovatrail/spec/types/legalv2"
	"golang.org/x/text/language"

	"github.com/altnovatrail/api/internal/consentage"
)

// The texts the requests of a consent name (contracts README, section 15).
const (
	PrivacyPolicy  = "privacy-policy"
	TermsOfUse     = "terms-of-use"
	ParentConsent  = "parent-consent"
	TeenConsent    = "teen-consent"
	StorageConsent = "storage-consent"
)

// cacheFor is how long the answer of GetLegalTexts is kept: an hour at most, by
// the contract. The texts change only with a new image.
const cacheFor = "public, max-age=3600"

// english is the language of every text in force, and of a summary asked for in
// a language the version has none in.
const english = "en"

// Text is the current version of a text.
type Text struct {
	ID           string
	Version      int32
	InForceSince string
	Place        string
	Body         string
	// Summaries is the gist of the version, by the tag of its language.
	Summaries map[string]string
	// Translations is the translations of the version, by the tag of their
	// language, other than English.
	Translations map[string]string
}

// Texts is the current version of every text of a part that has one.
type Texts struct {
	current map[string]Text
}

// Service is LegalService on the texts.
type Service struct {
	apiv1connect.UnimplementedLegalServiceHandler
	texts *Texts
}

// NewService is LegalService on texts.
func NewService(texts *Texts) *Service { return &Service{texts: texts} }

// Load is the texts of spec's Go module at the tag api pins, read as section 5
// reads a part: spec checked them before its release, and the same rules hold
// them here, but legal.in-force-unchanged, which needs the release before.
func Load() (*Texts, error) {
	part, err := partOf(spec.Legal)
	if err != nil {
		return nil, err
	}
	return newTexts(part)
}

// LoadDir is the texts of a self-hosted copy's folder at dir, which holds
// VERSION, legal.yaml and texts/ alone, its VERSION that of the part of spec's
// tag, and keeps the rules of section 5 but legal.in-force-unchanged: the copy's
// operator keeps its own history (legal/README.md, section 8).
func LoadDir(dir string) (*Texts, error) {
	fsys := os.DirFS(dir)
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("legal: read the folder of texts: %w", err)
	}
	for _, e := range entries {
		switch e.Name() {
		case "VERSION", "legal.yaml", "texts":
		default:
			return nil, fmt.Errorf("legal: the folder of texts holds %s: only VERSION, legal.yaml and texts/", e.Name())
		}
	}
	want, err := readFile(spec.Legal, "VERSION")
	if err != nil {
		return nil, err
	}
	got, err := readFile(fsys, "VERSION")
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want)) {
		return nil, fmt.Errorf("legal: the folder of texts is of version %s, and this release reads %s",
			bytes.TrimSpace(got), bytes.TrimSpace(want))
	}
	part, err := partOf(fsys)
	if err != nil {
		return nil, err
	}
	return newTexts(part)
}

// partOf is the part in fsys: legal.yaml as a JSON value, and every file under
// texts/.
func partOf(fsys fs.FS) (Part, error) {
	data, err := readFile(fsys, "legal.yaml")
	if err != nil {
		return Part{}, err
	}
	var passport any
	if err := consentage.FromYAML(data, &passport); err != nil {
		return Part{}, fmt.Errorf("legal: legal.yaml is no YAML: %w", err)
	}
	raw, err := jsonOf(passport)
	if err != nil {
		return Part{}, err
	}
	files := map[string][]byte{}
	err = fs.WalkDir(fsys, "texts", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(fsys, path)
		files[path] = data
		return err
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Part{}, fmt.Errorf("legal: read the texts: %w", err)
	}
	return Part{Passport: raw, Files: files}, nil
}

// newTexts reads the part and keeps the current version of every text.
func newTexts(part Part) (*Texts, error) {
	passport, err := Read(part, nil)
	if err != nil {
		return nil, err
	}
	t := &Texts{current: map[string]Text{}}
	for id, text := range passport.Texts {
		var current *legalv2.TextVersion
		for i, v := range text.Versions {
			if v.Status == legalv2.StatusStatusInForce && (current == nil || v.Version > current.Version) {
				current = &text.Versions[i]
			}
		}
		if current == nil {
			continue
		}
		since := ""
		if current.InForceSince != nil {
			since = *current.InForceSince
		}
		cur := Text{
			ID: string(id), Version: int32(current.Version), InForceSince: since, Place: string(text.Place),
			Body:         string(part.Files[fileOf(string(id), current.Version, "")]),
			Summaries:    map[string]string{english: string(current.Summary.En), "ru": string(current.Summary.Ru)},
			Translations: map[string]string{},
		}
		for _, tag := range current.Translations {
			cur.Translations[string(tag)] = string(part.Files[fileOf(string(id), current.Version, string(tag))])
		}
		t.current[string(id)] = cur
	}
	return t, nil
}

// Current is the number of the current version of the text id, and whether it
// has one.
func (t *Texts) Current(id string) (int32, bool) {
	c, ok := t.current[id]
	return c.Version, ok
}

// Names says whether refs names exactly the current version of each of ids that
// has one, and no other text: a text with no version in force is named by no
// request, and a version that is not current, a text in force left out, a text
// named twice or one that is none of ids is CAUSE_TEXT_NOT_CURRENT (contracts
// README, section 15).
func (t *Texts) Names(refs []*v1.LegalTextRef, ids ...string) bool {
	want := map[string]int32{}
	for _, id := range ids {
		if version, ok := t.Current(id); ok {
			want[id] = version
		}
	}
	if len(refs) != len(want) {
		return false
	}
	seen := map[string]bool{}
	for _, ref := range refs {
		version, ok := want[ref.GetId()]
		if !ok || seen[ref.GetId()] || ref.GetVersion() != version {
			return false
		}
		seen[ref.GetId()] = true
	}
	return true
}

// NamesOne is Names for a request that names a text by one field, ref, absent
// while the text has no version in force.
func (t *Texts) NamesOne(ref *v1.LegalTextRef, id string) bool {
	if ref == nil {
		return t.Names(nil, id)
	}
	return t.Names([]*v1.LegalTextRef{ref}, id)
}

// GetLegalTexts gives the current version of every text whose place is app, by
// ID in the order of code units: its body, its summary in the language asked —
// or else in English — and its translation into it, when the version has one,
// with no English in its place (contracts README, section 15). The answer is
// kept for an hour. A tag that is no BCP 47 is the one refusal.
func (s *Service) GetLegalTexts(ctx context.Context, req *v1.GetLegalTextsRequest) (*v1.GetLegalTextsResponse, error) {
	tag := req.GetInterfaceLanguage()
	if tag != "" {
		if _, err := language.Parse(tag); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, "interface_language")
		}
	}
	if info, ok := connect.CallInfoForServerContext(ctx); ok {
		info.ResponseHeader().Set("Cache-Control", cacheFor)
	}
	return &v1.GetLegalTextsResponse{Texts: s.texts.List(tag)}, nil
}

// List is the texts of the place app in the language tag, as GetLegalTexts
// gives them.
func (t *Texts) List(tag string) []*v1.LegalText {
	var ids []string
	for id, text := range t.current {
		if text.Place == string(legalv2.TextPlaceApp) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	texts := make([]*v1.LegalText, 0, len(ids))
	for _, id := range ids {
		text := t.current[id]
		summaryLanguage := lookup(tag, text.Summaries)
		if summaryLanguage == "" {
			summaryLanguage = english
		}
		out := &v1.LegalText{Id: id, Version: text.Version, InForceSince: text.InForceSince, Body: text.Body,
			Summary: text.Summaries[summaryLanguage], SummaryLanguage: summaryLanguage}
		if translated := lookup(tag, text.Translations); translated != "" {
			out.Translation, out.TranslationLanguage = text.Translations[translated], translated
		}
		texts = append(texts, out)
	}
	return texts
}

// lookup is the key of by that the tag finds by the lookup of RFC 4647: the
// tag, then the tag cut by its last subtag, with no regard to case — ru-RU finds
// ru —; empty when none does.
func lookup[V any](tag string, by map[string]V) string {
	for tag != "" {
		for key := range by {
			if strings.EqualFold(key, tag) {
				return key
			}
		}
		cut := strings.LastIndex(tag, "-")
		if cut < 0 {
			break
		}
		tag = tag[:cut]
	}
	return ""
}

func readFile(fsys fs.FS, name string) ([]byte, error) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, fmt.Errorf("legal: read %s: %w", name, err)
	}
	return data, nil
}

// jsonOf is the JSON of a value YAML read.
func jsonOf(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("legal: legal.yaml: %w", err)
	}
	return data, nil
}

// specTexts is the texts of the tag, read once.
var specTexts = sync.OnceValues(Load)

// Spec is the texts of the tag api pins, spec.Legal. They are embedded in the
// binary and read by the tests of this package, so that they cannot fail to
// read.
func Spec() *Texts {
	t, err := specTexts()
	if err != nil {
		panic(err)
	}
	return t
}

// Refs is the reference to the current version of each of ids that has one, as
// a request that gives a consent to them names them.
func (t *Texts) Refs(ids ...string) []*v1.LegalTextRef {
	var refs []*v1.LegalTextRef
	for _, id := range ids {
		if version, ok := t.Current(id); ok {
			refs = append(refs, &v1.LegalTextRef{Id: id, Version: version})
		}
	}
	return refs
}
