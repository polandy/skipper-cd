// Package fsatomic writes files atomically via a temp file + rename, so a
// crash mid-write (e.g. a nixos-rebuild restarting the service) can never
// leave a truncated file behind, and fsyncs both the file and its directory so
// a power loss cannot either.
package fsatomic

import (
	"os"
	"path/filepath"
)

// PrivateFileMode is the permission mode for persisted skipper state files
// (deploy state, event history, audit log, health-watch state): readable and
// writable by the owner and group only, not world-readable on a multi-user host.
const PrivateFileMode os.FileMode = 0o640

// PrivateDirMode is the matching directory mode: without it, PrivateFileMode
// on the files it holds would still let other users list the directory.
const PrivateDirMode os.FileMode = 0o750

// WriteFile writes data to path atomically and durably: it writes a temp file
// in the same directory, applies perm, fsyncs it, renames it over path and
// fsyncs the directory so the rename itself survives a power loss. On an error
// before the rename the target file is left untouched; an error from the
// directory fsync means the new content is in place but may not be durable.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
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
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	return syncDir(dir)
}

// syncDir fsyncs a directory, persisting the entries just renamed into it.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := d.Sync(); err != nil {
		d.Close()
		return err
	}
	return d.Close()
}
