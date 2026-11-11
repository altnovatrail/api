package legal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/altnovatrail/spec"
	"github.com/altnovatrail/spec/types/legalv2"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
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
	RuleInForceUnchanged = "legal.in-force-unchanged"
)

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

// Read reads the part by section 5: the schema of the passport, then legal.files,
// legal.versions, legal.set-texts and legal.markdown, then, when previous is
// the part of the release before, legal.in-force-unchanged. It gives the
// passport, or the Refusal of the first step that refuses the part: a part is
// never read in part.
func Read(part Part, previous *Part) (legalv2.Legal, error) {
	schema, err := passportSchema()
	if err != nil {
		return legalv2.Legal{}, err
	}
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(part.Passport))
	if err != nil {
		return legalv2.Legal{}, &Refusal{Step: StepSchema}
	}
	if err := schema.Validate(value); err != nil {
		return legalv2.Legal{}, &Refusal{Step: StepSchema, Names: schemaNames(err)}
	}
	var passport legalv2.Legal
	if err := json.Unmarshal(part.Passport, &passport); err != nil {
		return legalv2.Legal{}, &Refusal{Step: StepSchema}
	}
	rules := []struct {
		name     string
		breaches func() []string
	}{
		{RuleFiles, func() []string { return fileBreaches(passport, part.Files) }},
		{RuleVersions, func() []string { return versionBreaches(passport) }},
		{RuleSetTexts, func() []string { return setTextBreaches(passport) }},
		{RuleMarkdown, func() []string { return markdownBreaches(passport, part.Files) }},
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
			return legalv2.Legal{}, &Refusal{Step: rule.name, Names: slices.Compact(names)}
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
func filesOf(id string, v legalv2.TextVersion) []string {
	files := []string{fileOf(id, v.Version, "")}
	for _, tag := range v.Translations {
		files = append(files, fileOf(id, v.Version, string(tag)))
	}
	return files
}

// fileBreaches is legal.files: every version and every translation has its
// file, and every file is one of theirs.
func fileBreaches(passport legalv2.Legal, files map[string][]byte) []string {
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
func versionBreaches(passport legalv2.Legal) []string {
	var names []string
	broken := func(statuses []legalv2.Status) bool {
		draft, since := false, ""
		for i, s := range statuses {
			if s.Version != int64(i+1) {
				return true
			}
			if s.Status != legalv2.StatusStatusInForce {
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
		var statuses []legalv2.Status
		for _, v := range t.Versions {
			statuses = append(statuses, legalv2.Status{Version: v.Version, Status: v.Status, InForceSince: v.InForceSince})
		}
		if broken(statuses) {
			names = append(names, "texts."+string(id))
		}
	}
	for id, s := range passport.Sets {
		var statuses []legalv2.Status
		for _, v := range s.Versions {
			statuses = append(statuses, legalv2.Status{Version: v.Version, Status: v.Status, InForceSince: v.InForceSince})
		}
		if broken(statuses) {
			names = append(names, "sets."+string(id))
		}
	}
	return names
}

// setTextBreaches is legal.set-texts: a version of a set names versions that
// exist, of texts of the places of its notice and its form, and a version in
// force names versions in force.
func setTextBreaches(passport legalv2.Legal) []string {
	var names []string
	for id, s := range passport.Sets {
		for _, v := range s.Versions {
			if !setNames(passport, v.Notice, legalv2.TextPlaceSamplesPage, v.Status) ||
				!setNames(passport, v.Form, legalv2.TextPlaceForm, v.Status) {
				names = append(names, "sets."+string(id))
				break
			}
		}
	}
	return names
}

// setNames says whether a version of a set of status names by ref a version that
// exists, of a text of place, in force when the set's is.
func setNames(passport legalv2.Legal, ref legalv2.TextRef, place legalv2.TextPlace, status legalv2.StatusStatus) bool {
	t, ok := passport.Texts[ref.Text]
	if !ok || t.Place != place {
		return false
	}
	for _, v := range t.Versions {
		if v.Version == ref.Version {
			return status != legalv2.StatusStatusInForce || v.Status == legalv2.StatusStatusInForce
		}
	}
	return false
}

// markdownBreaches is legal.markdown: each version and each translation begins
// with a heading of the first level and holds no HTML, no image and no link.
// legal.files has held, so every one has its file.
func markdownBreaches(passport legalv2.Legal, files map[string][]byte) []string {
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
func changedBreaches(passport legalv2.Legal, part, previous Part) []string {
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
		t, there := passport.Texts[legalv2.ID(id)]
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
			var v legalv2.TextVersion
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
		_, there := passport.Sets[legalv2.SetID(id)]
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
	return entry, entry["version"], entry["status"] == string(legalv2.StatusStatusInForce)
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
