package packages_test

import (
	"archive/tar"
	"bytes"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/altnovatrail/module-kit/modulepackage"
	"github.com/altnovatrail/spec/types/formatv0"

	"github.com/altnovatrail/api/internal/packages"
	"github.com/altnovatrail/api/internal/packages/packagestest"
)

// world is a release of two packages of format 0.4 and an entry of 0.3, which
// api does not read, served by a test's server.
type world struct {
	smallest, media *modulepackage.Package
	index           []byte
	cfg             packages.Config
	rel             *packagestest.Release
	src             packages.Source
}

const outOfWindow = "sha256:0000000000000000000000000000000000000000000000000000000000000003"

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{smallest: packagestest.Built(t, "smallest"), media: packagestest.Built(t, "with-media-and-notice"), rel: &packagestest.Release{}}
	w.index = packagestest.Index(t,
		packagestest.Entry{Pkg: w.smallest, Release: "v0.2.0"},
		packagestest.Entry{Pkg: w.media, Release: "v0.3.0", Withdrawn: "answer_leak"},
		packagestest.Entry{ID: "example/older", Version: "0.1.0", Format: "0.3", Digest: outOfWindow, Release: "v0.1.0"},
	)
	w.cfg = packages.Config{Release: "v0.3.0", Index: packagestest.DigestOf(w.index)}
	w.rel.Put("v0.3.0", "module-index.json", w.index)
	w.rel.Put("v0.2.0", packagestest.ArchiveName(w.smallest.Digest), packagestest.Archive(t, w.smallest.Files))
	w.rel.Put("v0.3.0", packagestest.ArchiveName(w.media.Digest), packagestest.Archive(t, w.media.Files,
		packagestest.TarEntry{Name: "device/", Typeflag: tar.TypeDir}, packagestest.TarEntry{Name: "device/media/", Typeflag: tar.TypeDir}))
	w.src = w.rel.Serve(t)
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
				unique[packagestest.DigestOf(data)] = true
			}
		}
	}
	if laid.Packages != 2 || laid.Files != len(unique) {
		t.Errorf("laid %+v, want 2 packages and %d files", laid, len(unique))
	}
	for _, asked := range w.rel.Asked() {
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

	rel := &packagestest.Release{}
	index := packagestest.Index(t, packagestest.Entry{ID: "example/older", Version: "0.1.0", Format: "0.3", Digest: outOfWindow, Release: "v0.1.4"})
	rel.Put("v0.1.4", "module-index.json", index)
	cfg := packages.Config{Release: "v0.1.4", Index: packagestest.DigestOf(index)}
	dir := filepath.Join(t.TempDir(), "packages")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	laid, err := packages.Lay(t.Context(), rel.Serve(t), cfg, dir)
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
		{"another index", func(t *testing.T, w *world) { w.cfg.Index = packagestest.DigestOf([]byte("another")) }, packages.ErrIndex},
		{"no index", func(t *testing.T, w *world) { w.cfg.Release = "v9.9.9" }, packages.ErrFetch},
		{"no archive", func(t *testing.T, w *world) {
			w.rel.Remove("v0.2.0", packagestest.ArchiveName(w.smallest.Digest))
		}, packages.ErrFetch},
		{"a changed part", func(t *testing.T, w *world) {
			w.rel.Put("v0.2.0", packagestest.ArchiveName(w.smallest.Digest), packagestest.Archive(t, tampered(w)))
		}, modulepackage.ErrChain},
		{"a link", func(t *testing.T, w *world) {
			w.rel.Put("v0.2.0", packagestest.ArchiveName(w.smallest.Digest), packagestest.Archive(t, w.smallest.Files, packagestest.TarEntry{Name: "device/media/x", Typeflag: tar.TypeSymlink}))
		}, packages.ErrArchive},
		{"a path out", func(t *testing.T, w *world) {
			w.rel.Put("v0.2.0", packagestest.ArchiveName(w.smallest.Digest), packagestest.Archive(t, w.smallest.Files, packagestest.TarEntry{Name: "../x", Data: []byte("x"), Typeflag: tar.TypeReg}))
		}, packages.ErrArchive},
		{"a file of no part", func(t *testing.T, w *world) {
			w.rel.Put("v0.2.0", packagestest.ArchiveName(w.smallest.Digest), packagestest.Archive(t, w.smallest.Files, packagestest.TarEntry{Name: "README", Data: []byte("x"), Typeflag: tar.TypeReg}))
		}, packages.ErrArchive},
		{"a folder of no part", func(t *testing.T, w *world) {
			w.rel.Put("v0.2.0", packagestest.ArchiveName(w.smallest.Digest), packagestest.Archive(t, w.smallest.Files, packagestest.TarEntry{Name: "eval/", Typeflag: tar.TypeDir}))
		}, packages.ErrArchive},
		{"a file twice", func(t *testing.T, w *world) {
			w.rel.Put("v0.2.0", packagestest.ArchiveName(w.smallest.Digest), packagestest.Archive(t, w.smallest.Files, packagestest.TarEntry{Name: "device/LICENSE", Data: []byte("x"), Typeflag: tar.TypeReg}))
		}, packages.ErrArchive},
		{"no gzip", func(t *testing.T, w *world) {
			w.rel.Put("v0.2.0", packagestest.ArchiveName(w.smallest.Digest), []byte("not an archive"))
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
	if _, err := packages.Read(dir, packages.Config{Release: "v0.3.0", Index: packagestest.DigestOf([]byte("another"))}); !errors.Is(err, packages.ErrIndex) {
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

	smallest := packagestest.Built(t, "smallest")
	current := packagestest.Entry{Pkg: smallest, Release: "v0.2.0"}
	main := packagestest.Index(t, current)
	mainCfg := packages.Config{Release: "v0.2.0", Index: packagestest.DigestOf(main)}

	got, err := packages.Check(mainCfg, main, nil, nil)
	if err != nil || len(got) != 1 || !got[0].OK || got[0].Version != "0.1.0" || got[0].ID != "example/smallest" {
		t.Errorf("Check with no main = %+v, %v", got, err)
	}
	if _, err := packages.Check(packages.Config{Release: "v0.2.0", Index: packagestest.DigestOf([]byte("x"))}, main, nil, nil); !errors.Is(err, packages.ErrIndex) {
		t.Errorf("Check of another index = %v, want ErrIndex", err)
	}
	if _, err := packages.Check(packages.Config{Release: "v0.1.9", Index: mainCfg.Index}, main, &mainCfg, main); !errors.Is(err, packages.ErrOlderRelease) {
		t.Errorf("Check of an older release = %v, want ErrOlderRelease", err)
	}
	if _, err := packages.Check(mainCfg, main, &packages.Config{Release: "v0.2.0", Index: packagestest.DigestOf([]byte("x"))}, main); !errors.Is(err, packages.ErrIndex) {
		t.Errorf("Check against another main index = %v, want ErrIndex", err)
	}

	// The next release moves the module out of the window: lost.
	moved := packagestest.Index(t, packagestest.Entry{ID: "example/smallest", Version: "0.1.0", Format: "0.3", Digest: outOfWindow, Release: "v0.2.0"})
	if _, err := packages.Check(packages.Config{Release: "v0.3.0", Index: packagestest.DigestOf(moved)}, moved, &mainCfg, main); !errors.Is(err, packages.ErrLostVersion) {
		t.Errorf("Check of a module out of the window = %v, want ErrLostVersion", err)
	}
	// Withdrawn, it may have none.
	withdrawn := packagestest.Index(t, packagestest.Entry{Pkg: smallest, Release: "v0.2.0", Withdrawn: "wrong_key"})
	got, err = packages.Check(packages.Config{Release: "v0.3.0", Index: packagestest.DigestOf(withdrawn)}, withdrawn, &mainCfg, main)
	if err != nil || len(got) != 1 || got[0].OK {
		t.Errorf("Check of a withdrawal = %+v, %v; want no current version, and no refusal", got, err)
	}
}

// A file of the digest that is no index, a folder that is a file, and a folder
// with no parent are refused before anything is fetched or laid.
func TestLayRefusesWhatItCannotLayInto(t *testing.T) {
	t.Parallel()

	rel := &packagestest.Release{}
	notIndex := []byte(`{"modules": []}`)
	rel.Put("v0.1.4", "module-index.json", notIndex)
	src := rel.Serve(t)
	if _, err := packages.Lay(t.Context(), src, packages.Config{Release: "v0.1.4", Index: packagestest.DigestOf(notIndex)}, filepath.Join(t.TempDir(), "p")); !errors.Is(err, packages.ErrIndex) {
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
	if _, err := packages.Check(packages.Config{Release: "v0.1.4", Index: packagestest.DigestOf(notIndex)}, notIndex, nil, nil); !errors.Is(err, packages.ErrIndex) {
		t.Errorf("Check of no index = %v, want ErrIndex", err)
	}
}

// A set says which digests the image gives, which version of a module is
// current, and where each file of a device part lies.
func TestTheSetGivesTheStatusTheCurrentVersionAndTheFiles(t *testing.T) {
	t.Parallel()

	older, newer := packagestest.Built(t, "smallest"), packagestest.Built(t, "smallest", "0.2.0")
	media := packagestest.Built(t, "with-media-and-notice")
	set := packagestest.Set(t,
		packagestest.Entry{Pkg: older, Release: "v0.1.0"},
		packagestest.Entry{Pkg: newer, Release: "v0.2.0", Withdrawn: "wrong_key"},
		packagestest.Entry{Pkg: media, Release: "v0.2.0"},
		packagestest.Entry{ID: "example/older", Version: "0.1.0", Format: "0.3", Digest: outOfWindow, Release: "v0.1.0"},
		// A digest two entries name is given when either gives it.
		packagestest.Entry{ID: "example/twice", Version: "0.1.0", Format: "0.4", Digest: older.Digest, Release: "v0.1.0", Withdrawn: "wrong_key"},
	)
	for digest, want := range map[formatv0.Digest]packages.Status{
		older.Digest: packages.Given,
		newer.Digest: packages.NotGiven,
		media.Digest: packages.Given,
		outOfWindow:  packages.NotGiven,
		packagestest.DigestOf([]byte("no package")): packages.Unknown,
	} {
		if got := set.Status(digest); got != want {
			t.Errorf("Status(%s) = %v, want %v", digest, got, want)
		}
	}

	if pkg, ok := set.Current("example/smallest"); !ok || pkg.Digest != older.Digest {
		t.Errorf("Current of a module whose newest version is withdrawn = %v, %v; want the older", pkg, ok)
	}
	if pkg, ok := set.Current("example/older"); ok {
		t.Errorf("Current of a module out of the window = %v, want none", pkg.Digest)
	}
	if pkg, ok := set.Current("example/none"); ok {
		t.Errorf("Current of a module the index does not name = %v, want none", pkg.Digest)
	}
	want := []formatv0.ModuleID{"example/older", "example/smallest", "example/twice", "example/with-media-and-notice"}
	if got := set.Modules(); !slices.Equal(got, want) {
		t.Errorf("Modules = %v, want %v", got, want)
	}
	if !set.Names("example/older") || set.Names("example/none") {
		t.Error("Names does not tell the modules of the index")
	}

	for path, data := range map[string][]byte{
		"LICENSE":                media.Files["device/LICENSE"],
		"NOTICE":                 media.Files["device/NOTICE"],
		"media/pictures/dot.png": media.Files["device/media/pictures/dot.png"],
	} {
		name, digest, ok := set.DeviceFile(media.Digest, path)
		got, err := os.ReadFile(name)
		if !ok || err != nil || digest != packagestest.DigestOf(data) || !bytes.Equal(got, data) {
			t.Errorf("DeviceFile(%s) = %s, %s, %v: %v", path, name, digest, ok, err)
		}
	}
	for _, missing := range []struct {
		digest formatv0.Digest
		path   string
	}{{media.Digest, "media/none.png"}, {media.Digest, "device/LICENSE"}, {older.Digest, "NOTICE"}, {outOfWindow, "LICENSE"}} {
		if _, _, ok := set.DeviceFile(missing.digest, missing.path); ok {
			t.Errorf("DeviceFile(%s, %s) is there", missing.digest, missing.path)
		}
	}
}
