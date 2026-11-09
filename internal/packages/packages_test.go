package packages_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/altnovatrail/module-kit/modulepackage"
	"github.com/altnovatrail/spec"
	"github.com/altnovatrail/spec/types/formatv0"

	"github.com/altnovatrail/api/internal/packages"
)

// built is the package of the vector name of spec's format, which module-kit
// builds from its module (format/vectors/packages/).
func built(t *testing.T, name string) *modulepackage.Package {
	t.Helper()
	module, err := fs.Sub(spec.Format, "vectors/packages/"+name+"/module")
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := modulepackage.Build(context.Background(), module)
	if err != nil {
		t.Fatalf("build %s: %v", name, err)
	}
	return pkg
}

// entry is a file of an archive: a regular file, or what a test puts there.
type entry struct {
	name     string
	data     []byte
	typeflag byte
}

// archiveOf is a package's archive as a release of modules gives it: its files,
// flat, and the folders of device/.
func archiveOf(t *testing.T, files map[string][]byte, extra ...entry) []byte {
	t.Helper()
	var entries []entry
	for _, name := range slices.Sorted(maps.Keys(files)) {
		entries = append(entries, entry{name: name, data: files[name], typeflag: tar.TypeReg})
	}
	return tarGz(t, append(entries, extra...))
}

func tarGz(t *testing.T, entries []entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		header := &tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.data)), Typeflag: e.typeflag}
		if e.typeflag == tar.TypeSymlink {
			header.Linkname, header.Size = "/etc/passwd", 0
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if e.typeflag == tar.TypeReg {
			if _, err := tw.Write(e.data); err != nil {
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

func digestOf(data []byte) formatv0.Digest {
	sum := sha256.Sum256(data)
	return formatv0.Digest("sha256:" + hex.EncodeToString(sum[:]))
}

func archiveName(d formatv0.Digest) string {
	return strings.Replace(string(d), ":", "-", 1) + ".tar.gz"
}

// release is the files of the releases of modules a test serves, by release and
// name, and what the server was asked for.
type release struct {
	mu    sync.Mutex
	files map[string][]byte
	asked []string
}

func (r *release) put(tag, name string, data []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.files == nil {
		r.files = map[string][]byte{}
	}
	r.files[tag+"/"+name] = data
}

func (r *release) serve(t *testing.T) packages.Source {
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

// indexEntry is an entry of a test's index.
type indexEntry struct {
	pkg       *modulepackage.Package
	id        string
	version   string
	format    string
	digest    formatv0.Digest
	release   string
	withdrawn string
}

// indexOf is a module index of the entries, as module-kit reads one.
func indexOf(t *testing.T, entries ...indexEntry) []byte {
	t.Helper()
	modules := map[string]map[string]map[string]any{}
	for _, e := range entries {
		id, version, format, digest := e.id, e.version, e.format, e.digest
		if e.pkg != nil {
			id, version, digest = string(e.pkg.Inventory.ID), string(e.pkg.Inventory.Version), e.pkg.Digest
			format = string(e.pkg.Inventory.ModuleFormatVersion)
		}
		value := map[string]any{"module_format_version": format, "package_digest": digest, "release": e.release}
		if e.withdrawn != "" {
			value["withdrawn"] = e.withdrawn
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

// world is a release of two packages of format 0.4 and an entry of 0.3, which
// api does not read, served by a test's server.
type world struct {
	smallest, media *modulepackage.Package
	index           []byte
	cfg             packages.Config
	rel             *release
	src             packages.Source
}

const outOfWindow = "sha256:0000000000000000000000000000000000000000000000000000000000000003"

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{smallest: built(t, "smallest"), media: built(t, "with-media-and-notice"), rel: &release{}}
	w.index = indexOf(t,
		indexEntry{pkg: w.smallest, release: "v0.2.0"},
		indexEntry{pkg: w.media, release: "v0.3.0", withdrawn: "answer_leak"},
		indexEntry{id: "example/older", version: "0.1.0", format: "0.3", digest: outOfWindow, release: "v0.1.0"},
	)
	w.cfg = packages.Config{Release: "v0.3.0", Index: digestOf(w.index)}
	w.rel.put("v0.3.0", "module-index.json", w.index)
	w.rel.put("v0.2.0", archiveName(w.smallest.Digest), archiveOf(t, w.smallest.Files))
	w.rel.put("v0.3.0", archiveName(w.media.Digest), archiveOf(t, w.media.Files,
		entry{name: "device/", typeflag: tar.TypeDir}, entry{name: "device/media/", typeflag: tar.TypeDir}))
	w.src = w.rel.serve(t)
	return w
}

// Lay lays each package of the window, withdrawn too, from the release its entry
// names, and each file of a device part once; Read reads them back by the chain.
func TestLayLaysTheWindowAndReadReadsItBack(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	dir := filepath.Join(t.TempDir(), "packages")
	laid, err := packages.Lay(t.Context(), w.src, w.cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	unique := map[formatv0.Digest]bool{}
	for _, p := range []*modulepackage.Package{w.smallest, w.media} {
		for name, data := range p.Files {
			if strings.HasPrefix(name, modulepackage.DeviceFolder) {
				unique[digestOf(data)] = true
			}
		}
	}
	if laid.Packages != 2 || laid.Files != len(unique) {
		t.Errorf("laid %+v, want 2 packages and %d files", laid, len(unique))
	}
	for _, asked := range w.rel.asked {
		if strings.Contains(asked, strings.TrimPrefix(outOfWindow, "sha256:")) {
			t.Errorf("the package out of the window was fetched: %s", asked)
		}
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o755 {
		t.Errorf("the folder is %v, %v; want it readable by all, as nonroot reads the image's", info.Mode(), err)
	}
	license := filepath.Join(dir, "files", strings.Replace(string(w.smallest.Listing.License.Digest), ":", "-", 1))
	if w.smallest.Listing.License.Digest != w.media.Listing.License.Digest {
		t.Fatal("the vectors no longer share a LICENSE: the test of a file laid once needs another pair")
	}
	if _, err := os.Stat(license); err != nil {
		t.Errorf("the shared LICENSE is not laid by its digest: %v", err)
	}

	set, err := packages.Read(dir, w.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if set.Release != "v0.3.0" || len(set.Packages) != 2 || set.Packages[w.media.Digest] == nil || set.Packages[w.smallest.Digest] == nil {
		t.Errorf("Read = %s and %d packages, want the release and both", set.Release, len(set.Packages))
	}
	got := set.Packages[w.media.Digest]
	for _, name := range []string{"inventory.json", "server.json", "device.json"} {
		if !bytes.Equal(got.Files[name], w.media.Files[name]) {
			t.Errorf("%s read back is not the one built", name)
		}
	}
	if len(got.Files) != 3 {
		t.Errorf("serve holds %d files of the package, want its three texts alone", len(got.Files))
	}
	notice := set.File(w.media.Listing.Notice.Digest)
	if data, err := os.ReadFile(notice); err != nil || !bytes.Equal(data, w.media.Files["device/NOTICE"]) {
		t.Errorf("the NOTICE under its digest = %q, %v", data, err)
	}
}

// An index of no package in the window lays the index alone, and serve reads it.
func TestAnIndexOfNoPackageInTheWindowLaysTheIndexAlone(t *testing.T) {
	t.Parallel()

	rel := &release{}
	index := indexOf(t, indexEntry{id: "example/older", version: "0.1.0", format: "0.3", digest: outOfWindow, release: "v0.1.4"})
	rel.put("v0.1.4", "module-index.json", index)
	cfg := packages.Config{Release: "v0.1.4", Index: digestOf(index)}
	dir := filepath.Join(t.TempDir(), "packages")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	laid, err := packages.Lay(t.Context(), rel.serve(t), cfg, dir)
	if err != nil || laid != (packages.Laid{}) {
		t.Fatalf("Lay = %+v, %v; want nothing laid but the index", laid, err)
	}
	set, err := packages.Read(dir, cfg)
	if err != nil || len(set.Packages) != 0 {
		t.Errorf("Read = %v, %v; want no package", set, err)
	}
}

// Whatever does not match leaves no folder behind, and a folder that holds
// something is not laid over.
func TestLayRefusesWhatDoesNotMatch(t *testing.T) {
	t.Parallel()

	tampered := func(w *world) map[string][]byte {
		files := maps.Clone(w.smallest.Files)
		files["server.json"] = append(bytes.Clone(files["server.json"]), ' ')
		return files
	}
	for _, tc := range []struct {
		name   string
		change func(t *testing.T, w *world)
		want   error
	}{
		{"another index", func(t *testing.T, w *world) { w.cfg.Index = digestOf([]byte("another")) }, packages.ErrIndex},
		{"no index", func(t *testing.T, w *world) { w.cfg.Release = "v9.9.9" }, packages.ErrFetch},
		{"no archive", func(t *testing.T, w *world) {
			w.rel.put("v0.2.0", archiveName(w.smallest.Digest), nil)
			delete(w.rel.files, "v0.2.0/"+archiveName(w.smallest.Digest))
		}, packages.ErrFetch},
		{"a changed part", func(t *testing.T, w *world) {
			w.rel.put("v0.2.0", archiveName(w.smallest.Digest), archiveOf(t, tampered(w)))
		}, modulepackage.ErrChain},
		{"a link", func(t *testing.T, w *world) {
			w.rel.put("v0.2.0", archiveName(w.smallest.Digest), archiveOf(t, w.smallest.Files, entry{name: "device/media/x", typeflag: tar.TypeSymlink}))
		}, packages.ErrArchive},
		{"a path out", func(t *testing.T, w *world) {
			w.rel.put("v0.2.0", archiveName(w.smallest.Digest), archiveOf(t, w.smallest.Files, entry{name: "../x", data: []byte("x"), typeflag: tar.TypeReg}))
		}, packages.ErrArchive},
		{"a file of no part", func(t *testing.T, w *world) {
			w.rel.put("v0.2.0", archiveName(w.smallest.Digest), archiveOf(t, w.smallest.Files, entry{name: "README", data: []byte("x"), typeflag: tar.TypeReg}))
		}, packages.ErrArchive},
		{"a folder of no part", func(t *testing.T, w *world) {
			w.rel.put("v0.2.0", archiveName(w.smallest.Digest), archiveOf(t, w.smallest.Files, entry{name: "eval/", typeflag: tar.TypeDir}))
		}, packages.ErrArchive},
		{"a file twice", func(t *testing.T, w *world) {
			w.rel.put("v0.2.0", archiveName(w.smallest.Digest), archiveOf(t, w.smallest.Files, entry{name: "device/LICENSE", data: []byte("x"), typeflag: tar.TypeReg}))
		}, packages.ErrArchive},
		{"no gzip", func(t *testing.T, w *world) {
			w.rel.put("v0.2.0", archiveName(w.smallest.Digest), []byte("not an archive"))
		}, packages.ErrArchive},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			tc.change(t, w)
			parent := t.TempDir()
			dir := filepath.Join(parent, "packages")
			if _, err := packages.Lay(t.Context(), w.src, w.cfg, dir); !errors.Is(err, tc.want) {
				t.Fatalf("Lay = %v, want %v", err, tc.want)
			}
			if left, _ := os.ReadDir(parent); len(left) != 0 {
				t.Errorf("the refusal left %v behind", left)
			}
		})
	}

	w := newWorld(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := packages.Lay(t.Context(), w.src, w.cfg, dir); !errors.Is(err, packages.ErrNotEmpty) {
		t.Errorf("Lay into a folder with a file = %v, want ErrNotEmpty", err)
	}
}

// A folder that is not the one Lay laid for the configuration fails Read.
func TestReadRefusesAFolderThatIsNotLaid(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	dir := filepath.Join(t.TempDir(), "packages")
	if _, err := packages.Lay(t.Context(), w.src, w.cfg, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := packages.Read(dir, packages.Config{Release: "v0.3.0", Index: digestOf([]byte("another"))}); !errors.Is(err, packages.ErrIndex) {
		t.Errorf("Read of another index = %v, want ErrIndex", err)
	}
	if _, err := packages.Read(t.TempDir(), w.cfg); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Read of an empty folder = %v, want no index", err)
	}

	medium := filepath.Join(dir, "files", strings.Replace(string(w.media.Listing.Notice.Digest), ":", "-", 1))
	if err := os.Chmod(medium, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(medium, []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := packages.Read(dir, w.cfg); !errors.Is(err, modulepackage.ErrChain) {
		t.Errorf("Read of a changed file = %v, want ErrChain", err)
	}
	if err := os.Remove(medium); err != nil {
		t.Fatal(err)
	}
	if _, err := packages.Read(dir, w.cfg); !errors.Is(err, modulepackage.ErrChain) {
		t.Errorf("Read of a missing file = %v, want ErrChain", err)
	}
	listing := filepath.Join(dir, "packages", strings.Replace(string(w.smallest.Digest), ":", "-", 1), "device.json")
	if err := os.Chmod(listing, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(listing, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := packages.Read(dir, w.cfg); !errors.Is(err, modulepackage.ErrChain) {
		t.Errorf("Read of a broken listing = %v, want ErrChain", err)
	}
}

func TestTheConfigurationIsAReleaseAndADigest(t *testing.T) {
	t.Parallel()

	digest := "sha256:" + strings.Repeat("a", 64)
	got, err := packages.ParseConfig([]byte(`{"release": "v0.1.4", "index": "` + digest + `"}`))
	if err != nil || got != (packages.Config{Release: "v0.1.4", Index: formatv0.Digest(digest)}) {
		t.Errorf("ParseConfig = %+v, %v", got, err)
	}
	for _, bad := range []string{
		``, `[]`, `{"release": "v0.1.4"}`, `{"index": "` + digest + `"}`,
		`{"release": "0.1.4", "index": "` + digest + `"}`, `{"release": "v0.01.4", "index": "` + digest + `"}`,
		`{"release": "v0.1.4", "index": "sha256:` + strings.Repeat("A", 64) + `"}`,
		`{"release": "v0.1.4", "index": "` + digest + `", "modules": []}`,
		`{"release": "v0.1.4", "index": "` + digest + `"} {}`,
		`{"release": "v0.1.4", "index": "` + digest + `"}}`,
		`{"release": "v0.1.4", "index": "sha256:` + strings.Repeat("b", 64) + `", "index": "` + digest + `"}`,
		`{"release": "v0.1.4", "release": "v0.1.4"}`,
		`{"release": 4, "index": "` + digest + `"}`,
		`{"release": "v0.1.4", "index": "` + digest + `"`,
		`"v0.1.4"`,
	} {
		if _, err := packages.ParseConfig([]byte(bad)); !errors.Is(err, packages.ErrConfig) {
			t.Errorf("ParseConfig(%s) = %v, want ErrConfig", bad, err)
		}
	}
}

// The check of CI: the index by its digest, a release no older than main's, and
// no current version lost but by a withdrawal; it names each module's current
// version.
func TestTheCheckOfCI(t *testing.T) {
	t.Parallel()

	smallest := built(t, "smallest")
	current := indexEntry{pkg: smallest, release: "v0.2.0"}
	main := indexOf(t, current)
	mainCfg := packages.Config{Release: "v0.2.0", Index: digestOf(main)}

	got, err := packages.Check(mainCfg, main, nil, nil)
	if err != nil || len(got) != 1 || !got[0].OK || got[0].Version != "0.1.0" || got[0].ID != "example/smallest" {
		t.Errorf("Check with no main = %+v, %v", got, err)
	}
	if _, err := packages.Check(packages.Config{Release: "v0.2.0", Index: digestOf([]byte("x"))}, main, nil, nil); !errors.Is(err, packages.ErrIndex) {
		t.Errorf("Check of another index = %v, want ErrIndex", err)
	}
	if _, err := packages.Check(packages.Config{Release: "v0.1.9", Index: mainCfg.Index}, main, &mainCfg, main); !errors.Is(err, packages.ErrOlderRelease) {
		t.Errorf("Check of an older release = %v, want ErrOlderRelease", err)
	}
	if _, err := packages.Check(mainCfg, main, &packages.Config{Release: "v0.2.0", Index: digestOf([]byte("x"))}, main); !errors.Is(err, packages.ErrIndex) {
		t.Errorf("Check against another main index = %v, want ErrIndex", err)
	}

	// The next release moves the module out of the window: lost.
	moved := indexOf(t, indexEntry{id: "example/smallest", version: "0.1.0", format: "0.3", digest: outOfWindow, release: "v0.2.0"})
	if _, err := packages.Check(packages.Config{Release: "v0.3.0", Index: digestOf(moved)}, moved, &mainCfg, main); !errors.Is(err, packages.ErrLostVersion) {
		t.Errorf("Check of a module out of the window = %v, want ErrLostVersion", err)
	}
	// Withdrawn, it may have none.
	withdrawn := indexOf(t, indexEntry{pkg: smallest, release: "v0.2.0", withdrawn: "wrong_key"})
	got, err = packages.Check(packages.Config{Release: "v0.3.0", Index: digestOf(withdrawn)}, withdrawn, &mainCfg, main)
	if err != nil || len(got) != 1 || got[0].OK {
		t.Errorf("Check of a withdrawal = %+v, %v; want no current version, and no refusal", got, err)
	}
}

// A file of the digest that is no index, a folder that is a file, and a folder
// with no parent are refused before anything is fetched or laid.
func TestLayRefusesWhatItCannotLayInto(t *testing.T) {
	t.Parallel()

	rel := &release{}
	notIndex := []byte(`{"modules": []}`)
	rel.put("v0.1.4", "module-index.json", notIndex)
	src := rel.serve(t)
	if _, err := packages.Lay(t.Context(), src, packages.Config{Release: "v0.1.4", Index: digestOf(notIndex)}, filepath.Join(t.TempDir(), "p")); !errors.Is(err, packages.ErrIndex) {
		t.Errorf("Lay of no index = %v, want ErrIndex", err)
	}
	w := newWorld(t)
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := packages.Lay(t.Context(), w.src, w.cfg, file); err == nil {
		t.Error("Lay into a file: no error")
	}
	if _, err := packages.Lay(t.Context(), w.src, w.cfg, filepath.Join(t.TempDir(), "no", "parent")); err == nil {
		t.Error("Lay into a folder with no parent: no error")
	}
	if _, err := packages.Check(packages.Config{Release: "v0.1.4", Index: digestOf(notIndex)}, notIndex, nil, nil); !errors.Is(err, packages.ErrIndex) {
		t.Errorf("Check of no index = %v, want ErrIndex", err)
	}
}
