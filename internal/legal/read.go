package legal

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/altnovatrail/spec"
	"github.com/altnovatrail/spec/types/legalv3"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
)

// The steps of reading a part, by the names section 5 of legal/README.md gives
// them: the schema, then the rules in their order.
const (
	StepSchema           = "schema"
	RuleFiles            = "legal.files"
	RuleVersions         = "legal.versions"
	RuleSetTexts         = "legal.set-texts"
	RuleMarkdown         = "legal.markdown"
	RuleOperator         = "legal.operator"
	RuleInForceUnchanged = "legal.in-force-unchanged"
)

// whoWeAre is the heading, of the second level, of the section of the privacy
// policy that names the operator, which the pages of app that name it open
// (R115 spec).
const whoWeAre = "Who we are"

// Part is a part of the legal texts as it is read: its passport as a JSON value
// and its files under texts/, by their paths from the part.
type Part struct {
	Passport json.RawMessage
	Files    map[string][]byte
}

// Refusal is a part that a step of section 5 refuses, with the names it gives:
// the paths the schema refuses, or what a rule names, in the order of code
// units.
type Refusal struct {
	Step  string
	Names []string
}

func (r *Refusal) Error() string {
	return fmt.Sprintf("legal: the part breaks %s: %s", r.Step, strings.Join(r.Names, ", "))
}

// Read reads the part by section 5 on the day today, YYYY-MM-DD in UTC (section
// 2): the schema of the passport, then legal.files, legal.versions,
// legal.set-texts, legal.markdown and legal.operator, then, when previous is the
// part of the release before, legal.in-force-unchanged. It gives the passport,
// or the Refusal of the first step that refuses the part: a part is never read
// in part. A today that is no day is an error, and no Refusal.
func Read(part Part, previous *Part, today string) (legalv3.Legal, error) {
	if on, err := time.Parse(time.DateOnly, today); err != nil || on.Format(time.DateOnly) != today {
		return legalv3.Legal{}, fmt.Errorf("legal: a part is read on a day, YYYY-MM-DD, and %q is none", today)
	}
	schema, err := passportSchema()
	if err != nil {
		return legalv3.Legal{}, err
	}
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(part.Passport))
	if err != nil {
		return legalv3.Legal{}, &Refusal{Step: StepSchema}
	}
	if err := schema.Validate(value); err != nil {
		return legalv3.Legal{}, &Refusal{Step: StepSchema, Names: schemaNames(err)}
	}
	var passport legalv3.Legal
	if err := json.Unmarshal(part.Passport, &passport); err != nil {
		return legalv3.Legal{}, &Refusal{Step: StepSchema}
	}
	rules := []struct {
		name     string
		breaches func() []string
	}{
		{RuleFiles, func() []string { return fileBreaches(passport, part.Files) }},
		{RuleVersions, func() []string { return versionBreaches(passport) }},
		{RuleSetTexts, func() []string { return setTextBreaches(passport) }},
		{RuleMarkdown, func() []string { return markdownBreaches(passport, part.Files) }},
		{RuleOperator, func() []string { return operatorBreaches(passport, part.Files, today) }},
		{RuleInForceUnchanged, func() []string {
			if previous == nil {
				return nil
			}
			return changedBreaches(passport, part, *previous)
		}},
	}
	for _, rule := range rules {
		if names := rule.breaches(); len(names) > 0 {
			slices.Sort(names)
			return legalv3.Legal{}, &Refusal{Step: rule.name, Names: slices.Compact(names)}
		}
	}
	return passport, nil
}

// passportSchema is the schema of the passport, schemas/legal.schema.json of
// spec.Legal, compiled once.
var passportSchema = sync.OnceValues(func() (*jsonschema.Schema, error) {
	const name = "schemas/legal.schema.json"
	data, err := readFile(spec.Legal, name)
	if err != nil {
		return nil, err
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("legal: read the schema: %w", err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(name, doc); err != nil {
		return nil, fmt.Errorf("legal: read the schema: %w", err)
	}
	schema, err := compiler.Compile(name)
	if err != nil {
		return nil, fmt.Errorf("legal: compile the schema: %w", err)
	}
	return schema, nil
})

// schemaNames is the places the schema refuses, by the names of the fields
// from the root joined by ".", each once, in the order of code units.
func schemaNames(err error) []string {
	verr, ok := err.(*jsonschema.ValidationError)
	if !ok {
		return nil
	}
	set := map[string]bool{}
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) == 0 {
			set[strings.Join(e.InstanceLocation, ".")] = true
		}
		for _, cause := range e.Causes {
			walk(cause)
		}
	}
	walk(verr)
	return slices.Sorted(maps.Keys(set))
}

// fileOf is the file of a version of a text, by its path from the part, and
// with a tag, the file of its translation into that language.
func fileOf(id string, version int64, tag string) string {
	if tag == "" {
		return fmt.Sprintf("texts/%s/%d.md", id, version)
	}
	return fmt.Sprintf("texts/%s/%d.%s.md", id, version, tag)
}

// filesOf is the files of a version of a text: its own, then its translations',
// in the order the passport names them.
func filesOf(id string, v legalv3.TextVersion) []string {
	files := []string{fileOf(id, v.Version, "")}
	for _, tag := range v.Translations {
		files = append(files, fileOf(id, v.Version, string(tag)))
	}
	return files
}

// fileBreaches is legal.files: every version and every translation has its
// file, and every file is one of theirs.
func fileBreaches(passport legalv3.Legal, files map[string][]byte) []string {
	wanted := map[string]bool{}
	for id, t := range passport.Texts {
		for _, v := range t.Versions {
			for _, file := range filesOf(string(id), v) {
				wanted[file] = true
			}
		}
	}
	var names []string
	for file := range wanted {
		if _, ok := files[file]; !ok {
			names = append(names, file)
		}
	}
	for file := range files {
		if !wanted[file] {
			names = append(names, file)
		}
	}
	return names
}

// versionBreaches is legal.versions: 1, 2, 3… in order, every draft after every
// version in force, and the dates of the versions in force never going back.
func versionBreaches(passport legalv3.Legal) []string {
	var names []string
	broken := func(statuses []legalv3.Status) bool {
		draft, since := false, ""
		for i, s := range statuses {
			if s.Version != int64(i+1) {
				return true
			}
			if s.Status != legalv3.StatusStatusInForce {
				draft = true
				continue
			}
			date := ""
			if s.InForceSince != nil {
				date = *s.InForceSince
			}
			// YYYY-MM-DD compares as text.
			if draft || date < since {
				return true
			}
			since = date
		}
		return false
	}
	for id, t := range passport.Texts {
		var statuses []legalv3.Status
		for _, v := range t.Versions {
			statuses = append(statuses, legalv3.Status{Version: v.Version, Status: v.Status, InForceSince: v.InForceSince})
		}
		if broken(statuses) {
			names = append(names, "texts."+string(id))
		}
	}
	for id, s := range passport.Sets {
		var statuses []legalv3.Status
		for _, v := range s.Versions {
			statuses = append(statuses, legalv3.Status{Version: v.Version, Status: v.Status, InForceSince: v.InForceSince})
		}
		if broken(statuses) {
			names = append(names, "sets."+string(id))
		}
	}
	return names
}

// setTextBreaches is legal.set-texts: a version of a set names versions that
// exist, of texts of the places of its notice and its form, and a version in
// force names versions in force that apply by its own day.
func setTextBreaches(passport legalv3.Legal) []string {
	var names []string
	for id, s := range passport.Sets {
		for _, v := range s.Versions {
			if !setNames(passport, v.Notice, legalv3.TextPlaceSamplesPage, v) ||
				!setNames(passport, v.Form, legalv3.TextPlaceForm, v) {
				names = append(names, "sets."+string(id))
				break
			}
		}
	}
	return names
}

// setNames says whether the version set of a set names by ref a version that
// exists, of a text of place, in force when the set's is and applying by its
// day.
func setNames(passport legalv3.Legal, ref legalv3.TextRef, place legalv3.TextPlace, set legalv3.SetVersion) bool {
	t, ok := passport.Texts[ref.Text]
	if !ok || t.Place != place {
		return false
	}
	for _, v := range t.Versions {
		if v.Version != ref.Version {
			continue
		}
		if set.Status != legalv3.StatusStatusInForce {
			return true
		}
		// YYYY-MM-DD compares as text.
		return v.Status == legalv3.StatusStatusInForce && dayOf(v.InForceSince) <= dayOf(set.InForceSince)
	}
	return false
}

// dayOf is the day of a version in force, which the schema holds it always has.
func dayOf(since *string) string {
	if since == nil {
		return ""
	}
	return *since
}

// operatorBreaches is legal.operator: the part has the privacy policy, at the
// place app, with a version in force whose day has come by today, and every
// version of it, a draft too, has the section "Who we are" (R115 spec). The
// policy is named by its ID when it is missing, elsewhere or not yet in force,
// and a version without the section by its file; a translation needs none.
func operatorBreaches(passport legalv3.Legal, files map[string][]byte, today string) []string {
	var names []string
	policy, ok := passport.Texts[PrivacyPolicy]
	if !ok || policy.Place != legalv3.TextPlaceApp {
		names = append(names, "texts."+PrivacyPolicy)
	} else if current, _ := pick(policy.Versions, today); current == 0 {
		names = append(names, "texts."+PrivacyPolicy)
	}
	for _, v := range policy.Versions {
		if file := fileOf(PrivacyPolicy, v.Version, ""); !WhoWeAre(files[file]) {
			names = append(names, file)
		}
	}
	return names
}

// WhoWeAre says whether source has the section "Who we are": a heading of the
// second level, not inside another block, whose text is that alone, as
// CommonMark reads it — no other case, level or emphasis. goldmark's GFM cuts a
// run of text where an autolink of www. could begin, and micromark keeps it
// whole, so the text is that of every node of the heading, each a text. goldmark
// keeps the source of a text, where micromark resolves its character references
// and backslash escapes; goldmark's writer of HTML resolves them as it does, and
// leaves "Who we are" as it is while escaping whatever else.
func WhoWeAre(source []byte) bool {
	doc := markdown.Parser().Parse(text.NewReader(source))
	for n := doc.FirstChild(); n != nil; n = n.NextSibling() {
		heading, ok := n.(*ast.Heading)
		if !ok || heading.Level != 2 {
			continue
		}
		var words []byte
		plain := true
		for c := heading.FirstChild(); c != nil && plain; c = c.NextSibling() {
			t, ok := c.(*ast.Text)
			plain = ok
			if ok {
				words = append(words, t.Value(source)...)
			}
		}
		var read bytes.Buffer
		w := bufio.NewWriter(&read)
		html.DefaultWriter.Write(w, words)
		if plain && w.Flush() == nil && read.String() == whoWeAre {
			return true
		}
	}
	return false
}

// markdownBreaches is legal.markdown: each version and each translation begins
// with a heading of the first level and holds no HTML, no image and no link.
// legal.files has held, so every one has its file.
func markdownBreaches(passport legalv3.Legal, files map[string][]byte) []string {
	var names []string
	for id, t := range passport.Texts {
		for _, v := range t.Versions {
			for _, file := range filesOf(string(id), v) {
				if !Markdown(files[file]) {
					names = append(names, file)
				}
			}
		}
	}
	return names
}

// markdown is CommonMark with GitHub's flavour, whose literal autolinks are
// links, as micromark's extension of GFM reads them.
var markdown = goldmark.New(goldmark.WithExtensions(extension.GFM))

// Markdown says whether source keeps legal.markdown: it begins with a heading
// of the first level, and holds no HTML, no image and no link — an autolink, a
// literal one of GFM and a definition of a link among them.
func Markdown(source []byte) bool {
	ctx := parser.NewContext()
	doc := markdown.Parser().Parse(text.NewReader(source), parser.WithContext(ctx))
	if len(ctx.References()) > 0 {
		return false
	}
	first, ok := doc.FirstChild().(*ast.Heading)
	if !ok || first.Level != 1 {
		return false
	}
	clean := true
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		switch n.(type) {
		case *ast.HTMLBlock, *ast.RawHTML, *ast.Image, *ast.Link, *ast.AutoLink:
			clean = false
			return ast.WalkStop, nil
		}
		return ast.WalkContinue, nil
	})
	return clean
}

// changedBreaches is legal.in-force-unchanged: every version in force in the
// release before is here, its entry the same — and, for a text, its place — and
// its file and the file of each of its translations the same, byte for byte. A
// text or a set whose entry changed or is gone is named by its ID; a file that
// changed, by its path. The part before passed no schema here, so it is read
// as a value of any shape.
func changedBreaches(passport legalv3.Legal, part, previous Part) []string {
	var before struct {
		Texts map[string]struct {
			Place    any               `json:"place"`
			Versions []json.RawMessage `json:"versions"`
		} `json:"texts"`
		Sets map[string]struct {
			Versions []json.RawMessage `json:"versions"`
		} `json:"sets"`
	}
	_ = json.Unmarshal(previous.Passport, &before)
	var now struct {
		Texts map[string]struct {
			Versions []json.RawMessage `json:"versions"`
		} `json:"texts"`
		Sets map[string]struct {
			Versions []json.RawMessage `json:"versions"`
		} `json:"sets"`
	}
	_ = json.Unmarshal(part.Passport, &now)
	var names []string
	for id, then := range before.Texts {
		t, there := passport.Texts[legalv3.ID(id)]
		for _, raw := range then.Versions {
			entry, number, inForce := entryOf(raw)
			if !inForce {
				continue
			}
			kept, found := keptEntry(now.Texts[id].Versions, number)
			if !there || !found || !reflect.DeepEqual(kept, entry) || string(t.Place) != fmt.Sprint(then.Place) {
				names = append(names, "texts."+id)
			}
			if !there || !found {
				continue
			}
			var v legalv3.TextVersion
			if err := json.Unmarshal(raw, &v); err != nil {
				continue
			}
			for _, file := range filesOf(id, v) {
				was, wasThere := previous.Files[file]
				is, isThere := part.Files[file]
				if wasThere != isThere || !bytes.Equal(was, is) {
					names = append(names, file)
				}
			}
		}
	}
	for id, then := range before.Sets {
		_, there := passport.Sets[legalv3.SetID(id)]
		for _, raw := range then.Versions {
			entry, number, inForce := entryOf(raw)
			if !inForce {
				continue
			}
			kept, found := keptEntry(now.Sets[id].Versions, number)
			if !there || !found || !reflect.DeepEqual(kept, entry) {
				names = append(names, "sets."+id)
			}
		}
	}
	return names
}

// entryOf is a version's entry as a value, its number, and whether it is in
// force.
func entryOf(raw json.RawMessage) (any, any, bool) {
	var entry map[string]any
	if err := json.Unmarshal(raw, &entry); err != nil {
		return nil, nil, false
	}
	return entry, entry["version"], entry["status"] == string(legalv3.StatusStatusInForce)
}

// keptEntry is the entry of versions numbered number, as a value.
func keptEntry(versions []json.RawMessage, number any) (any, bool) {
	for _, raw := range versions {
		var entry map[string]any
		if err := json.Unmarshal(raw, &entry); err == nil && reflect.DeepEqual(entry["version"], number) {
			return entry, true
		}
	}
	return nil, false
}
