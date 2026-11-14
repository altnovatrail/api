// Package legal is the legal texts api serves (R52; R130, R136 spec): the part
// legal/ of spec at the tag api pins, spec.Legal, or a self-hosted copy's folder
// of its operator's texts, read once at the start by the rules of
// legal/README.md, section 5. A version in force applies from its day, 00:00
// UTC, and the texts are asked on a day, the day of each request in UTC, which
// gives the current and the next version of each text (section 2). It answers
// LegalService.GetLegalTexts, and tells whether a request that gives a consent
// names the current versions of the texts it is given to.
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
	"time"

	"connectrpc.com/connect/v2"
	"github.com/altnovatrail/spec"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"
	"github.com/altnovatrail/spec/types/legalv3"
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

// keptFor is the longest the answer of GetLegalTexts is kept, by the contract:
// an hour, and no longer than until the day of a next version it gives comes.
// Otherwise the texts change only with a new image.
const keptFor = time.Hour

// english is the language of every text in force, and of a summary asked for in
// a language the version has none in.
const english = "en"

// Text is a version in force of a text.
type Text struct {
	ID      string
	Version int32
	// InForceSince is the day from which the version applies, YYYY-MM-DD.
	InForceSince string
	Place        string
	Body         string
	// Summaries is the gist of the version, by the tag of its language.
	Summaries map[string]string
	// Translations is the translations of the version, by the tag of their
	// language, other than English.
	Translations map[string]string
}

// Texts is the versions in force of every text of a part, which a day sorts
// into the current and the next version of each.
type Texts struct {
	passport legalv3.Legal
	// inForce is the versions in force, by the ID of their text and by number.
	inForce map[string]map[int64]Text
}

// Day is the texts on a day: the current version of every text that has one,
// and the next version of every text that has one.
type Day struct {
	at            time.Time
	current, next map[string]Text
}

// Service is LegalService on the texts.
type Service struct {
	apiv1connect.UnimplementedLegalServiceHandler
	texts *Texts
	now   func() time.Time
}

// Option sets up a Service.
type Option func(*Service)

// WithClock gives the service its clock, time.Now by default: a request is
// answered on the day of its time in UTC.
func WithClock(now func() time.Time) Option { return func(s *Service) { s.now = now } }

// NewService is LegalService on texts.
func NewService(texts *Texts, opts ...Option) *Service {
	s := &Service{texts: texts, now: time.Now}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// DayOf is the day of at in UTC, YYYY-MM-DD, as a part is read on it.
func DayOf(at time.Time) string { return at.UTC().Format(time.DateOnly) }

// Load is the texts of spec's Go module at the tag api pins, read on the day of
// at as section 5 reads a part: spec checked them before its release, and the
// same rules hold them here, but legal.in-force-unchanged, which needs the
// release before.
func Load(at time.Time) (*Texts, error) {
	part, err := partOf(spec.Legal)
	if err != nil {
		return nil, err
	}
	return newTexts(part, DayOf(at))
}

// LoadDir is the texts of a self-hosted copy's folder at dir, read on the day of
// at, which holds VERSION, legal.yaml and texts/ alone, its VERSION that of the
// part of spec's tag, and keeps the rules of section 5 but
// legal.in-force-unchanged: the copy's operator keeps its own history
// (legal/README.md, section 8). A folder whose privacy policy applies from no
// day that has come breaks legal.operator.
func LoadDir(dir string, at time.Time) (*Texts, error) {
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
	return newTexts(part, DayOf(at))
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

// newTexts reads the part on the day today and keeps every version in force of
// every text.
func newTexts(part Part, today string) (*Texts, error) {
	passport, err := Read(part, nil, today)
	if err != nil {
		return nil, err
	}
	t := &Texts{passport: passport, inForce: map[string]map[int64]Text{}}
	for id, text := range passport.Texts {
		versions := map[int64]Text{}
		for _, v := range text.Versions {
			if v.Status != legalv3.StatusStatusInForce {
				continue
			}
			kept := Text{
				ID: string(id), Version: int32(v.Version), InForceSince: dayOf(v.InForceSince), Place: string(text.Place),
				Body:         string(part.Files[fileOf(string(id), v.Version, "")]),
				Summaries:    map[string]string{english: string(v.Summary.En), "ru": string(v.Summary.Ru)},
				Translations: map[string]string{},
			}
			for _, tag := range v.Translations {
				kept.Translations[string(tag)] = string(part.Files[fileOf(string(id), v.Version, string(tag))])
			}
			versions[v.Version] = kept
		}
		t.inForce[string(id)] = versions
	}
	return t, nil
}

// pick is the current and the next version of a text on the day today, of its
// versions: the current the version in force with the highest number whose day
// has come, and the next the one that becomes current on the nearest day still
// to come, the highest number among those of that day (legal/README.md,
// section 2). 0 is none. YYYY-MM-DD compares as text.
func pick(versions []legalv3.TextVersion, today string) (current, next int64) {
	nearest := ""
	for _, v := range versions {
		if v.Status != legalv3.StatusStatusInForce {
			continue
		}
		switch day := dayOf(v.InForceSince); {
		case day <= today:
			current = max(current, v.Version)
		case nearest == "" || day < nearest:
			nearest, next = day, v.Version
		case day == nearest:
			next = max(next, v.Version)
		}
	}
	return current, next
}

// Versions is the number of the current and of the next version of every text
// of the passport that has one, by ID, on the day today, as a reader of section
// 5 gives them.
func Versions(passport legalv3.Legal, today string) (current, next map[string]int64) {
	current, next = map[string]int64{}, map[string]int64{}
	for id, text := range passport.Texts {
		c, n := pick(text.Versions, today)
		if c != 0 {
			current[string(id)] = c
		}
		if n != 0 {
			next[string(id)] = n
		}
	}
	return current, next
}

// On is the texts on the day of at in UTC.
func (t *Texts) On(at time.Time) Day {
	d := Day{at: at, current: map[string]Text{}, next: map[string]Text{}}
	current, next := Versions(t.passport, DayOf(at))
	for id, version := range current {
		d.current[id] = t.inForce[id][version]
	}
	for id, version := range next {
		d.next[id] = t.inForce[id][version]
	}
	return d
}

// Current is the number of the current version of the text id, and whether it
// has one.
func (d Day) Current(id string) (int32, bool) {
	c, ok := d.current[id]
	return c.Version, ok
}

// Names says whether refs names exactly the current version of each of ids that
// has one, and no other text: a text with no current version is named by no
// request, and a version that is not current — a next version before its day
// among them —, a text with a current version left out, a text named twice or
// one that is none of ids is CAUSE_TEXT_NOT_CURRENT (contracts README, section
// 15).
func (d Day) Names(refs []*v1.LegalTextRef, ids ...string) bool {
	want := map[string]int32{}
	for _, id := range ids {
		if version, ok := d.Current(id); ok {
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
// while the text has no current version.
func (d Day) NamesOne(ref *v1.LegalTextRef, id string) bool {
	if ref == nil {
		return d.Names(nil, id)
	}
	return d.Names([]*v1.LegalTextRef{ref}, id)
}

// Refs is the reference to the current version of each of ids that has one, as
// a request that gives a consent to them names them.
func (d Day) Refs(ids ...string) []*v1.LegalTextRef {
	var refs []*v1.LegalTextRef
	for _, id := range ids {
		if version, ok := d.Current(id); ok {
			refs = append(refs, &v1.LegalTextRef{Id: id, Version: version})
		}
	}
	return refs
}

// KeptFor is how long an answer on the day may be kept: an hour, and no longer
// than until 00:00 UTC of the nearest day of a next version of the place app
// it gives (contracts README, section 15).
func (d Day) KeptFor() time.Duration {
	kept := keptFor
	for _, text := range d.next {
		if text.Place != string(legalv3.TextPlaceApp) {
			continue
		}
		if from, err := time.Parse(time.DateOnly, text.InForceSince); err == nil {
			kept = min(kept, from.Sub(d.at))
		}
	}
	return max(kept, 0)
}

// GetLegalTexts gives, on the day of the request in UTC, the current version of
// every text whose place is app, and the next version of every one that has
// one, each by ID in the order of code units: its body, its summary in the
// language asked — or else in English — and its translation into it, when the
// version has one, with no English in its place (contracts README, section 15).
// The answer is kept for KeptFor, in whole seconds. A tag that is no BCP 47 is
// the one refusal.
func (s *Service) GetLegalTexts(ctx context.Context, req *v1.GetLegalTextsRequest) (*v1.GetLegalTextsResponse, error) {
	tag := req.GetInterfaceLanguage()
	if tag != "" {
		if _, err := language.Parse(tag); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, "interface_language")
		}
	}
	day := s.texts.On(s.now())
	if info, ok := connect.CallInfoForServerContext(ctx); ok {
		info.ResponseHeader().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", day.KeptFor()/time.Second))
	}
	return &v1.GetLegalTextsResponse{Texts: day.List(tag), NextTexts: day.ListNext(tag)}, nil
}

// List is the current texts of the place app in the language tag, as
// GetLegalTexts gives them.
func (d Day) List(tag string) []*v1.LegalText { return listOf(d.current, tag) }

// ListNext is the next texts of the place app in the language tag, as
// GetLegalTexts gives them.
func (d Day) ListNext(tag string) []*v1.LegalText { return listOf(d.next, tag) }

// listOf is the texts of by whose place is app, by ID, in the language tag.
func listOf(by map[string]Text, tag string) []*v1.LegalText {
	var ids []string
	for id, text := range by {
		if text.Place == string(legalv3.TextPlaceApp) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	texts := make([]*v1.LegalText, 0, len(ids))
	for _, id := range ids {
		text := by[id]
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

// specTexts is the texts of the tag, read once, on the day of the first read:
// their privacy policy applies from a day long past, so legal.operator holds on
// every day after it.
var specTexts = sync.OnceValues(func() (*Texts, error) { return Load(time.Now()) })

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
