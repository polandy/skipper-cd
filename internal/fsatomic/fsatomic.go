// Package fsatomic writes files atomically via a temp file + rename, so a
// crash mid-write (e.g. a nixos-rebuild restarting the service) can never
// leave a truncated file behind.
package fsatomic

import (
	"os"
	"path/filepath"
)

// WriteFile writes data to path atomically: it writes a temp file in the same
// directory, applies perm, and renames it over path. On any error the target
// file is left untouched.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
