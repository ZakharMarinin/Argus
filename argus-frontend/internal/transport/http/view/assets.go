package view

import (
	"fmt"
	"hash/fnv"
	"io/fs"
)

// assetVersion is a short fingerprint of the static files: file names and sizes.
//
// Assets change only together with the binary, so they can be cached for a
// long time, but after a deploy the browser must see a new address. The
// fingerprint is computed once when templates are parsed and lives in
// Templates, not in a package variable: no global mutable state is needed here.
func assetVersion(src fs.FS) string {
	h := fnv.New32a()

	_ = fs.WalkDir(src, "static", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		// Writing to a hash can't fail.
		_, _ = fmt.Fprintf(h, "%s:%d;", path, info.Size())

		return nil
	})

	return fmt.Sprintf("%x", h.Sum32())
}
