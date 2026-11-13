package packages_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"testing"

	"github.com/altnovatrail/module-kit/modulepackage"

	"github.com/altnovatrail/api/internal/packages"
	"github.com/altnovatrail/api/internal/packages/packagestest"
)

// Whatever the path of a request for a file names as its package, FromName
// does not panic, and what it reads is a digest whose Name is that name (R42).
func FuzzFromName(f *testing.F) {
	f.Add(packages.Name(packagestest.Built(f, "smallest").Digest))
	for _, seed := range []string{
		"sha256-" + strings.Repeat("a", 64),
		"sha256-" + strings.Repeat("A", 64),
		"sha256-" + strings.Repeat("a", 63),
		"sha256:" + strings.Repeat("a", 64),
		"sha512-" + strings.Repeat("a", 128),
		"sha256-",
		"",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, name string) {
		digest, ok := packages.FromName(name)
		if !ok {
			return
		}
		if !packages.IsDigest(string(digest)) || packages.Name(digest) != name {
			t.Fatalf("FromName(%q) = %q, whose Name is %q", name, digest, packages.Name(digest))
		}
		if again, okAgain := packages.FromName(packages.Name(digest)); !okAgain || again != digest {
			t.Fatalf("FromName(Name(%q)) = %q, %v", digest, again, okAgain)
		}
	})
}

// releaseForm is a tag vX.Y.Z with no leading zeros, as R41 pins one.
var releaseForm = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// Whatever modules.json holds — a pull request may change it, and the check of
// CI reads it —, ParseConfig does not panic, and what it reads is a release
// and a digest of their form, which read back from themselves (R41).
func FuzzParseConfig(f *testing.F) {
	digest := "sha256:" + strings.Repeat("a", 64)
	for _, seed := range []string{
		`{"release": "v0.1.4", "index": "` + digest + `"}`,
		`{"index": "` + digest + `", "release": "v10.0.0"}` + "\n",
		`{"release": "v0.01.4", "index": "` + digest + `"}`,
		`{"release": "v0.1.4", "index": "` + digest + `", "modules": []}`,
		`{"release": "v0.1.4", "index": "` + digest + `"} {}`,
		`{"release": "v0.1.4", "release": "v0.1.5"}`,
		`{"Release": "v0.1.4", "index": "` + digest + `"}`,
		`{"release": "v0.1.4", "index": "` + digest + `"}`,
		`[]`,
		``,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		cfg, err := packages.ParseConfig(data)
		if err != nil {
			if !errors.Is(err, packages.ErrConfig) {
				t.Fatalf("ParseConfig(%q) = %v, want ErrConfig", data, err)
			}
			return
		}
		if !releaseForm.MatchString(cfg.Release) || !packages.IsDigest(string(cfg.Index)) {
			t.Fatalf("ParseConfig(%q) = %+v, not of the form", data, cfg)
		}
		written, err := json.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if again, err := packages.ParseConfig(written); err != nil || again != cfg {
			t.Fatalf("ParseConfig(%q) = %+v; written again, %s, = %+v, %v", data, cfg, written, again, err)
		}
	})
}

// Whatever an archive of a release holds, Unpack does not panic, and what it
// takes is regular files of the parts of a package alone, by clean paths in
// its folder, of no more bytes than the tar holds; an archive with a link is
// refused (R41). The fuzzer varies the tar, which the test gzips: the gzip is
// the standard library's, and the rules are api's. testdata/fuzz/FuzzUnpack
// holds a sparse file of PAX, which the reader would unfold to 500 MB.
func FuzzUnpack(f *testing.F) {
	smallest := packagestest.Built(f, "smallest")
	media := packagestest.Built(f, "with-media-and-notice")
	dirs := []packagestest.TarEntry{{Name: "device/", Typeflag: tar.TypeDir}, {Name: "device/media/", Typeflag: tar.TypeDir}}
	for _, archive := range [][]byte{
		packagestest.Archive(f, smallest.Files),
		packagestest.Archive(f, media.Files, dirs...),
		packagestest.Archive(f, smallest.Files, packagestest.TarEntry{Name: "device/media/x", Typeflag: tar.TypeSymlink}),
		packagestest.Archive(f, smallest.Files, packagestest.TarEntry{Name: "../x", Data: []byte("x"), Typeflag: tar.TypeReg}),
		packagestest.Archive(f, smallest.Files, packagestest.TarEntry{Name: "device/../server.json", Data: []byte("x"), Typeflag: tar.TypeReg}),
		packagestest.Archive(f, smallest.Files, packagestest.TarEntry{Name: "/inventory.json", Data: []byte("x"), Typeflag: tar.TypeReg}),
		packagestest.Archive(f, smallest.Files, packagestest.TarEntry{Name: "README", Data: []byte("x"), Typeflag: tar.TypeReg}),
		packagestest.Archive(f, smallest.Files, packagestest.TarEntry{Name: "eval/", Typeflag: tar.TypeDir}),
		packagestest.Archive(f, smallest.Files, packagestest.TarEntry{Name: "device/LICENSE", Data: []byte("x"), Typeflag: tar.TypeReg}),
		packagestest.TarGz(f, []packagestest.TarEntry{{Name: "./device.json", Data: []byte("{}"), Typeflag: tar.TypeReg}}),
	} {
		f.Add(tarOf(f, archive))
	}
	f.Fuzz(func(t *testing.T, tarred []byte) {
		var archive bytes.Buffer
		gz := gzip.NewWriter(&archive)
		_, _ = gz.Write(tarred)
		_ = gz.Close()
		files, err := packages.Unpack(archive.Bytes())
		if err != nil {
			if !errors.Is(err, packages.ErrArchive) {
				t.Fatalf("Unpack = %v, want ErrArchive", err)
			}
			return
		}
		if link := linkIn(tarred); link != "" {
			t.Fatalf("Unpack took an archive with the link %q", link)
		}
		unpacked := 0
		for name, file := range files {
			if !fs.ValidPath(name) || path.Clean(name) != name || !ofAPackage(name) || !file.Mode.IsRegular() {
				t.Fatalf("Unpack took %q, %v", name, file.Mode)
			}
			unpacked += len(file.Data)
		}
		if unpacked > len(tarred) {
			t.Fatalf("Unpack took %d bytes of a tar of %d", unpacked, len(tarred))
		}
	})
}

// tarOf is the tar of a gzipped archive.
func tarOf(f *testing.F, archive []byte) []byte {
	f.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		f.Fatal(err)
	}
	tarred, err := io.ReadAll(gz)
	if err != nil {
		f.Fatal(err)
	}
	return tarred
}

// linkIn is the name of the first link of a tar, as the standard library reads
// it, or "".
func linkIn(tarred []byte) string {
	reader := tar.NewReader(bytes.NewReader(tarred))
	for {
		header, err := reader.Next()
		if err != nil {
			return ""
		}
		if header.Typeflag == tar.TypeSymlink || header.Typeflag == tar.TypeLink {
			return header.Name
		}
	}
}

// ofAPackage says whether name is a file of a package: one of its three texts,
// or a file of its device part.
func ofAPackage(name string) bool {
	switch name {
	case modulepackage.InventoryFile, modulepackage.ServerFile, modulepackage.DeviceFile:
		return true
	}
	return strings.HasPrefix(name, modulepackage.DeviceFolder) && name != modulepackage.DeviceFolder
}
