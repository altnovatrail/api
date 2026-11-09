package packages

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing/fstest"
	"time"

	"github.com/altnovatrail/module-kit/module"
	"github.com/altnovatrail/module-kit/moduleindex"
	"github.com/altnovatrail/module-kit/modulepackage"
	"github.com/altnovatrail/spec/types/formatv0"
)

// The reasons Lay and Read refuse, which their errors wrap.
var (
	// ErrIndex is an index whose bytes are not those of the digest modules.json
	// pins, or no index of the format.
	ErrIndex = errors.New("packages: not the index modules.json pins")
	// ErrFetch is a file of the release that could not be fetched: an answer that
	// is not 200, or more bytes than its limit.
	ErrFetch = errors.New("packages: the file could not be fetched from the release")
	// ErrArchive is an archive of a package that holds what a package does not:
	// a link or another kind of entry, a path out of the package or of no part of
	// it, or more bytes than its limit (architecture 3.10 spec).
	ErrArchive = errors.New("packages: the archive is not a package's")
	// ErrNotEmpty is a folder to lay the packages into that holds something.
	ErrNotEmpty = errors.New("packages: the folder is not empty")
)

// The limits of what is fetched and unpacked: an index, an archive as fetched,
// and the files it unpacks to, together. A package of the first slice is a few
// MB; the limits keep a wrong file from filling the disk or the memory of a
// build.
const (
	maxIndex    = 16 << 20
	maxArchive  = 256 << 20
	maxUnpacked = 512 << 20
)

// The layout of the folder (R07, R41): the pinned index; the three files of each
// package's chain under the digest of the package; and each file of a device
// part once, under its own digest, so that a medium the next version keeps takes
// no room.
const (
	IndexFile   = moduleindex.File
	packagesDir = "packages"
	filesDir    = "files"
)

// DefaultBase is where the releases of modules give their files, without a
// token: /<release>/<file> follows it (architecture 3.10 spec).
const DefaultBase = "https://github.com/altnovatrail/modules/releases/download"

// fetchTimeout bounds each file fetched: a release that stops answering fails
// the build rather than holding it.
const fetchTimeout = 2 * time.Minute

// Source is where Lay fetches the files of the releases.
type Source struct {
	// Client fetches; nil is a client of its own, with fetchTimeout.
	Client *http.Client
	// Base is DefaultBase, or a test's server.
	Base string
}

// Laid is what Lay laid: the packages and the files of their device parts,
// each counted once.
type Laid struct {
	Packages int
	Files    int
}

// Lay fetches the index of the release cfg pins, checks it by its digest, and
// lays each package of the index in api's window into dir, withdrawn ones too —
// a withdrawn version is kept to repeat a verdict (R63 spec; R07). Each package
// is fetched as its archive from the release its entry names, unpacked in
// memory, read by module-kit by the chain from its digest, and only the files
// the chain names are written. dir is made afresh beside where it is, and is
// there whole or not at all: it may not hold anything already.
func Lay(ctx context.Context, src Source, cfg Config, dir string) (Laid, error) {
	dir = filepath.Clean(dir)
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return Laid{}, fmt.Errorf("%w: %s", ErrNotEmpty, dir)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Laid{}, fmt.Errorf("packages: read %s: %w", dir, err)
	}
	client := src.Client
	if client == nil {
		client = &http.Client{Timeout: fetchTimeout}
	}
	data, err := fetch(ctx, client, src.Base, cfg.Release, IndexFile, maxIndex)
	if err != nil {
		return Laid{}, err
	}
	index, err := checkIndex(data, cfg.Index)
	if err != nil {
		return Laid{}, err
	}

	work, err := workFolder(dir)
	if err != nil {
		return Laid{}, err
	}
	done := false
	defer func() {
		if !done {
			_ = os.RemoveAll(work)
		}
	}()
	var laid Laid
	if err := os.WriteFile(filepath.Join(work, IndexFile), data, 0o444); err != nil {
		return Laid{}, fmt.Errorf("packages: write the index: %w", err)
	}
	for _, entry := range inWindow(index) {
		archive, err := fetch(ctx, client, src.Base, string(entry.Release), archiveName(entry.PackageDigest), maxArchive)
		if err != nil {
			return Laid{}, err
		}
		files, err := unpack(archive)
		if err != nil {
			return Laid{}, fmt.Errorf("%s: %w", archiveName(entry.PackageDigest), err)
		}
		pkg, err := modulepackage.Read(files, entry.PackageDigest)
		if err != nil {
			return Laid{}, fmt.Errorf("packages: %s: %w", entry.PackageDigest, err)
		}
		written, err := write(work, pkg)
		if err != nil {
			return Laid{}, err
		}
		laid.Packages++
		laid.Files += written
	}
	// The folder is renamed into place whole, so that a build cut short leaves no
	// half of it; an empty folder there already gives way.
	if err := os.Remove(dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Laid{}, fmt.Errorf("packages: %w", err)
	}
	if err := os.Rename(work, dir); err != nil {
		return Laid{}, fmt.Errorf("packages: put the folder in place: %w", err)
	}
	done = true
	return laid, nil
}

// workFolder makes the folder Lay lays into beside dir, under a name of its own
// that no other run takes, readable by all: the image copies it owned by root
// for serve, which is nonroot, to read (R41). MkdirTemp would make it for its
// owner alone.
func workFolder(dir string) (string, error) {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("packages: make the folder: %w", err)
	}
	work := filepath.Join(filepath.Dir(dir), "."+filepath.Base(dir)+"-"+hex.EncodeToString(random))
	if err := os.Mkdir(work, 0o755); err != nil {
		return "", fmt.Errorf("packages: make the folder: %w", err)
	}
	return work, nil
}

// checkIndex is the index of data, whose bytes have digest.
func checkIndex(data []byte, digest formatv0.Digest) (formatv0.ModuleIndex, error) {
	if digestOf(data) != digest {
		return formatv0.ModuleIndex{}, fmt.Errorf("%w: its digest is %s, and modules.json names %s", ErrIndex, digestOf(data), digest)
	}
	index, err := moduleindex.Read(data)
	if err != nil {
		return formatv0.ModuleIndex{}, fmt.Errorf("%w: %w", ErrIndex, err)
	}
	return index, nil
}

// inWindow is each entry of index in the window of the format versions api reads,
// withdrawn ones too, once by its package and in the order of the modules and
// their versions.
func inWindow(index formatv0.ModuleIndex) []formatv0.Entry {
	var entries []formatv0.Entry
	seen := map[formatv0.Digest]bool{}
	for _, id := range slices.Sorted(maps.Keys(index.Modules)) {
		versions := index.Modules[id]
		for _, v := range slices.Sorted(maps.Keys(versions)) {
			entry := versions[v]
			if !module.ReadsFormat(string(entry.ModuleFormatVersion)) || seen[entry.PackageDigest] {
				continue
			}
			seen[entry.PackageDigest] = true
			entries = append(entries, entry)
		}
	}
	return entries
}

// fetch is the file name of the release, no more than limit bytes.
func fetch(ctx context.Context, client *http.Client, base, release, name string, limit int64) ([]byte, error) {
	url := strings.TrimSuffix(base, "/") + "/" + release + "/" + name
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrFetch, name, err)
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %s %s: %w", ErrFetch, release, name, err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %s %s: %s", ErrFetch, release, name, res.Status)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %s %s: %w", ErrFetch, release, name, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: %s %s is more than %d bytes", ErrFetch, release, name, limit)
	}
	return data, nil
}

// archiveName is the name of the archive of the package of digest in its
// release: the digest with - for : (architecture 3.10 spec).
func archiveName(digest formatv0.Digest) string { return folderName(digest) + ".tar.gz" }

// unpack is the files of an archive of a package, by their paths in its folder.
// The archive holds the files of the package and its folders alone: a link or
// another kind of entry, a path that leaves the folder or names no part of a
// package, a path twice, or more bytes than maxUnpacked is refused. The files
// are a MapFS of testing/fstest, a file system in memory of the standard
// library, which imports no testing.
func unpack(archive []byte) (fstest.MapFS, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrArchive, err)
	}
	files := fstest.MapFS{}
	reader := tar.NewReader(gz)
	var total int64
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrArchive, err)
		}
		name := strings.TrimSuffix(strings.TrimPrefix(header.Name, "./"), "/")
		switch {
		case name == "" || name == ".":
			continue
		case !fs.ValidPath(name) || path.Clean(name) != name:
			return nil, fmt.Errorf("%w: the path %q", ErrArchive, header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if name != strings.TrimSuffix(modulepackage.DeviceFolder, "/") && !strings.HasPrefix(name, modulepackage.DeviceFolder) {
				return nil, fmt.Errorf("%w: the folder %q", ErrArchive, name)
			}
			continue
		case tar.TypeReg:
		default:
			return nil, fmt.Errorf("%w: %q is no file", ErrArchive, name)
		}
		if !partOfPackage(name) {
			return nil, fmt.Errorf("%w: %q is of no part of a package", ErrArchive, name)
		}
		if _, twice := files[name]; twice {
			return nil, fmt.Errorf("%w: %q twice", ErrArchive, name)
		}
		total += header.Size
		if header.Size < 0 || total > maxUnpacked {
			return nil, fmt.Errorf("%w: more than %d bytes unpacked", ErrArchive, maxUnpacked)
		}
		data, err := io.ReadAll(io.LimitReader(reader, header.Size+1))
		if err != nil || int64(len(data)) != header.Size {
			return nil, fmt.Errorf("%w: %q is cut short", ErrArchive, name)
		}
		files[name] = &fstest.MapFile{Data: data, Mode: 0o444}
	}
	return files, nil
}

// partOfPackage says whether name is a file a package as files has: its three
// texts and the files of its device part (format/README.md, section 25).
func partOfPackage(name string) bool {
	switch name {
	case modulepackage.InventoryFile, modulepackage.ServerFile, modulepackage.DeviceFile:
		return true
	}
	return strings.HasPrefix(name, modulepackage.DeviceFolder)
}

// write lays pkg into the folder dir: its texts under its digest, and each file
// of its device part under that file's digest, unless an earlier package laid
// it. It says how many files of a device part it wrote.
func write(dir string, pkg *modulepackage.Package) (int, error) {
	own := filepath.Join(dir, packagesDir, folderName(pkg.Digest))
	if err := os.MkdirAll(own, 0o755); err != nil {
		return 0, fmt.Errorf("packages: %w", err)
	}
	for _, name := range []string{modulepackage.InventoryFile, modulepackage.ServerFile, modulepackage.DeviceFile} {
		if err := os.WriteFile(filepath.Join(own, name), pkg.Files[name], 0o444); err != nil {
			return 0, fmt.Errorf("packages: %w", err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, filesDir), 0o755); err != nil {
		return 0, fmt.Errorf("packages: %w", err)
	}
	written := 0
	for _, d := range deviceFiles(pkg.Listing) {
		target := filepath.Join(dir, filesDir, folderName(d.Digest))
		if _, err := os.Stat(target); err == nil {
			continue
		}
		data := pkg.Files[modulepackage.DeviceFolder+d.path]
		if err := os.WriteFile(target, data, 0o444); err != nil {
			return 0, fmt.Errorf("packages: %w", err)
		}
		written++
	}
	return written, nil
}

// deviceFile is a file of a device part: its path in device/ and its descriptor.
type deviceFile struct {
	path string
	formatv0.Descriptor
}

// deviceFiles is every file a listing names, by its path in device/, in order.
func deviceFiles(listing formatv0.DeviceListing) []deviceFile {
	files := []deviceFile{{"LICENSE", listing.License}}
	if listing.Notice != nil {
		files = append(files, deviceFile{"NOTICE", *listing.Notice})
	}
	for _, media := range slices.Sorted(maps.Keys(listing.Media)) {
		files = append(files, deviceFile{"media/" + string(media), listing.Media[media]})
	}
	return files
}

// folderName is a digest as a name in the folder: sha256-<hex>.
func folderName(digest formatv0.Digest) string {
	return strings.Replace(string(digest), ":", "-", 1)
}

// digestOf is the digest of data, as the format writes one.
func digestOf(data []byte) formatv0.Digest {
	sum := sha256.Sum256(data)
	return formatv0.Digest("sha256:" + hex.EncodeToString(sum[:]))
}
