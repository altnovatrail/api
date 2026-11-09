package packages

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing/fstest"

	"github.com/altnovatrail/module-kit/modulepackage"
	"github.com/altnovatrail/spec/types/formatv0"
)

// Set is the packages a folder holds, as serve reads them at its start: the
// pinned index and each package of it in api's window, by its digest. A package
// holds its three texts, read by module-kit by the chain of digests from the
// index; the files of its device part stay in the folder, each under its own
// digest, for the requests to serve from there (T32).
type Set struct {
	// Release is the release of modules that modules.json pins.
	Release string
	// Index is its index.
	Index formatv0.ModuleIndex
	// Packages is each package of the index in the window, withdrawn ones too:
	// its inventory, canonical view and listing.
	Packages map[formatv0.Digest]*modulepackage.Package
	// Dir is the folder.
	Dir string
}

// File is the path in the folder of the file of a device part of digest.
func (s *Set) File(digest formatv0.Digest) string {
	return filepath.Join(s.Dir, filesDir, folderName(digest))
}

// Read reads the folder dir that Lay laid for cfg: its index must have the digest
// cfg pins, so that a folder of another release is a failed start rather than
// another set of versions (architecture 3.3), and each package of that index in
// the window must be there: its texts whole, by the chain of digests from the
// index, and each file its listing names, of the size it names. The files of the
// device parts are not read again: Lay laid each under its digest after
// module-kit had read it, and an image does not change; so serve holds no media
// in memory and does not hash them at every start.
func Read(dir string, cfg Config) (*Set, error) {
	data, err := os.ReadFile(filepath.Join(dir, IndexFile))
	if err != nil {
		return nil, fmt.Errorf("packages: read the index: %w", err)
	}
	index, err := checkIndex(data, cfg.Index)
	if err != nil {
		return nil, err
	}
	set := &Set{Release: cfg.Release, Index: index, Packages: map[formatv0.Digest]*modulepackage.Package{}, Dir: dir}
	for _, entry := range inWindow(index) {
		texts, err := laidTexts(dir, entry.PackageDigest)
		if err != nil {
			return nil, err
		}
		pkg, err := modulepackage.ReadParts(texts, entry.PackageDigest)
		if err != nil {
			return nil, fmt.Errorf("packages: %s: %w", entry.PackageDigest, err)
		}
		for _, d := range deviceFiles(pkg.Listing) {
			info, err := os.Stat(set.File(d.Digest))
			if err != nil || !info.Mode().IsRegular() || info.Size() != d.Size {
				return nil, fmt.Errorf("packages: %s: %s is not the file of %s and %d bytes: %w",
					entry.PackageDigest, d.path, d.Digest, d.Size, modulepackage.ErrChain)
			}
		}
		set.Packages[entry.PackageDigest] = pkg
	}
	return set, nil
}

// laidTexts is the three texts of the package of digest as Lay laid them in dir,
// as the files of a package's folder; a text that is not there is left out, and
// module-kit refuses the package for it.
func laidTexts(dir string, digest formatv0.Digest) (fstest.MapFS, error) {
	own := filepath.Join(dir, packagesDir, folderName(digest))
	files := fstest.MapFS{}
	for _, name := range []string{modulepackage.InventoryFile, modulepackage.ServerFile, modulepackage.DeviceFile} {
		data, err := os.ReadFile(filepath.Join(own, name))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("packages: %s: %w", digest, err)
		}
		files[name] = &fstest.MapFile{Data: data, Mode: 0o444}
	}
	return files, nil
}
