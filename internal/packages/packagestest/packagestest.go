// Package packagestest builds, for the tests, what package packages lays and
// reads: the packages of the vectors of spec's format, their archives, an index
// of them and a release of modules that serves them, and a folder laid from
// them, read as serve reads it (R41). The tests run without the network.
//
// The vectors of packages hold no answer slot bound choice, so the package
// holds modules of its own too, synthetic, under modules/: choices, whose tasks
// a tap checks, with one that AI would read and one with a rubric.
package packagestest

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"maps"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/altnovatrail/module-kit/modulepackage"
	"github.com/altnovatrail/spec"
	"github.com/altnovatrail/spec/types/formatv0"

	"github.com/altnovatrail/api/internal/packages"
)

// own is the modules of this package's own, which Own builds.
//
//go:embed modules
var own embed.FS

// versionLine is the line of a manifest that names the module's version.
var versionLine = regexp.MustCompile(`(?m)^version: .*$`)

// Built is the package of the vector name of spec's format, which module-kit
// builds from its module (format/vectors/packages/); with version, the same
// module at that version, for a module with two.
func Built(t testing.TB, name string, version ...string) *modulepackage.Package {
	t.Helper()
	module, err := fs.Sub(spec.Format, "vectors/packages/"+name+"/module")
	if err != nil {
		t.Fatal(err)
	}
	return build(t, name, module, version)
}

// Own is the package of this package's own module name, under modules/; with
// version, the same module at that version.
func Own(t testing.TB, name string, version ...string) *modulepackage.Package {
	t.Helper()
	module, err := fs.Sub(own, "modules/"+name)
	if err != nil {
		t.Fatal(err)
	}
	return build(t, name, module, version)
}

// OwnEdited is the package of this package's own module name at version, with
// each of its files as edit gives it back: a later version that changes the
// module, as one with retired skills.
func OwnEdited(t testing.TB, name, version string, edit func(path string, data []byte) []byte) *modulepackage.Package {
	t.Helper()
	module, err := fs.Sub(own, "modules/"+name)
	if err != nil {
		t.Fatal(err)
	}
	return build(t, name, edited(t, module, edit), []string{version})
}

// The lines of a manifest that name the module's ID and its audience's age
// categories.
var (
	idLine   = regexp.MustCompile(`(?m)^id: .*$`)
	agesLine = regexp.MustCompile(`(?m)^  age_categories: .*$`)
)

// As is an edit for OwnEdited: the manifest names the module id, for the age
// categories ages, a list as YAML writes it, "[adult]" — another module of the
// same graph.
func As(id, ages string) func(path string, data []byte) []byte {
	return func(path string, data []byte) []byte {
		if path == "manifest.yaml" {
			data = idLine.ReplaceAllLiteral(data, []byte("id: "+id))
			data = agesLine.ReplaceAllLiteral(data, []byte("  age_categories: "+ages))
		}
		return data
	}
}

// build is the package module-kit builds of module, at version when given.
func build(t testing.TB, name string, module fs.FS, version []string) *modulepackage.Package {
	t.Helper()
	if len(version) > 0 {
		module = edited(t, module, func(path string, data []byte) []byte {
			if path == "manifest.yaml" {
				data = versionLine.ReplaceAll(data, []byte(`version: "`+version[0]+`"`))
			}
			return data
		})
	}
	pkg, err := modulepackage.Build(context.Background(), module)
	if err != nil {
		t.Fatalf("build %s: %v", name, err)
	}
	return pkg
}

// edited is a copy of module with each of its files as edit gives it back.
func edited(t testing.TB, module fs.FS, edit func(path string, data []byte) []byte) fs.FS {
	t.Helper()
	files := fstest.MapFS{}
	err := fs.WalkDir(module, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(module, path)
		if err != nil {
			return err
		}
		files[path] = &fstest.MapFile{Data: edit(path, data), Mode: 0o644}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// TarEntry is an entry of an archive: a regular file, a folder, or a link,
// which a test of a refusal puts there.
type TarEntry struct {
	Name     string
	Data     []byte
	Typeflag byte
}

// Archive is a package's archive as a release of modules gives it: its files,
// flat, and whatever extra a test adds.
func Archive(t testing.TB, files map[string][]byte, extra ...TarEntry) []byte {
	t.Helper()
	var entries []TarEntry
	for _, name := range slices.Sorted(maps.Keys(files)) {
		entries = append(entries, TarEntry{Name: name, Data: files[name], Typeflag: tar.TypeReg})
	}
	return TarGz(t, append(entries, extra...))
}

// TarGz is the entries as a gzipped tar; a link points out of the package.
func TarGz(t testing.TB, entries []TarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		header := &tar.Header{Name: e.Name, Mode: 0o644, Size: int64(len(e.Data)), Typeflag: e.Typeflag}
		if e.Typeflag == tar.TypeSymlink {
			header.Linkname, header.Size = "/etc/passwd", 0
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if e.Typeflag == tar.TypeReg {
			if _, err := tw.Write(e.Data); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// DigestOf is the digest of data, as the format writes one.
func DigestOf(data []byte) formatv0.Digest {
	sum := sha256.Sum256(data)
	return formatv0.Digest("sha256:" + hex.EncodeToString(sum[:]))
}

// ArchiveName is the name of the archive of the package of d in its release.
func ArchiveName(d formatv0.Digest) string { return packages.Name(d) + ".tar.gz" }

// Release is the files of the releases of modules a test serves, by release and
// name, and what the server was asked for.
type Release struct {
	mu    sync.Mutex
	files map[string][]byte
	asked []string
}

// Put gives the file name in the release tag.
func (r *Release) Put(tag, name string, data []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.files == nil {
		r.files = map[string][]byte{}
	}
	r.files[tag+"/"+name] = data
}

// Remove takes the file name out of the release tag.
func (r *Release) Remove(tag, name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.files, tag+"/"+name)
}

// Asked is each path the server was asked for, in order.
func (r *Release) Asked() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.asked)
}

// Serve serves the releases until the test ends, as the source Lay fetches from.
func (r *Release) Serve(t testing.TB) packages.Source {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		defer r.mu.Unlock()
		path := strings.TrimPrefix(req.URL.Path, "/")
		r.asked = append(r.asked, path)
		data, ok := r.files[path]
		if !ok {
			http.NotFound(w, req)
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(server.Close)
	return packages.Source{Client: server.Client(), Base: server.URL}
}

// Entry is an entry of a test's index: of a package built, or of a package that
// is not there — one of a format api does not read — by its fields.
type Entry struct {
	Pkg       *modulepackage.Package
	ID        string
	Version   string
	Format    string
	Digest    formatv0.Digest
	Release   string
	Withdrawn string
}

// Index is a module index of the entries, as module-kit reads one.
func Index(t testing.TB, entries ...Entry) []byte {
	t.Helper()
	modules := map[string]map[string]map[string]any{}
	for _, e := range entries {
		id, version, format, digest := e.ID, e.Version, e.Format, e.Digest
		if e.Pkg != nil {
			id, version, digest = string(e.Pkg.Inventory.ID), string(e.Pkg.Inventory.Version), e.Pkg.Digest
			format = string(e.Pkg.Inventory.ModuleFormatVersion)
		}
		value := map[string]any{"module_format_version": format, "package_digest": digest, "release": e.Release}
		if e.Pkg != nil {
			if teaches := teachesOf(e.Pkg.View); len(teaches) > 0 {
				value["teaches"] = teaches
			}
		}
		if e.Withdrawn != "" {
			value["withdrawn"] = e.Withdrawn
		}
		if modules[id] == nil {
			modules[id] = map[string]map[string]any{}
		}
		modules[id][version] = value
	}
	data, err := json.Marshal(map[string]any{"module_format_version": "0.4", "modules": modules})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// teachesOf is the anchor skills the skills of view's graph teach, each once, in
// the order of their bytes: the teaches of its entry, as a release of modules
// writes it (format/README.md, section 26).
func teachesOf(view formatv0.CanonicalView) []string {
	if view.SkillGraph == nil {
		return nil
	}
	var teaches []string
	for _, skill := range view.SkillGraph.Skills {
		for _, anchor := range skill.Teaches {
			teaches = append(teaches, string(anchor))
		}
	}
	slices.Sort(teaches)
	return slices.Compact(teaches)
}

// Set is the folder of the entries, laid by Lay from a release that serves
// their packages, and read back as serve reads it at its start.
func Set(t testing.TB, entries ...Entry) *packages.Set {
	t.Helper()
	rel := &Release{}
	index := Index(t, entries...)
	cfg := packages.Config{Release: "v9.0.0", Index: DigestOf(index)}
	rel.Put(cfg.Release, packages.IndexFile, index)
	for _, e := range entries {
		if e.Pkg != nil {
			rel.Put(e.Release, ArchiveName(e.Pkg.Digest), Archive(t, e.Pkg.Files))
		}
	}
	dir := filepath.Join(t.TempDir(), "packages")
	if _, err := packages.Lay(t.Context(), rel.Serve(t), cfg, dir); err != nil {
		t.Fatal(err)
	}
	set, err := packages.Read(dir, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return set
}
