// Package fsatomic writes files atomically via a temp file + rename, so a
// crash mid-write (e.g. a nixos-rebuild restarting the service) can never
// leave a truncated file behind, and fsyncs both the file and its directory so
// a power loss cannot either.
package fsatomic

import (
	"errors"
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

// tempFile is the part of *os.File the temp-file half of WriteFile uses.
type tempFile interface {
	Name() string
	Write(b []byte) (int, error)
	Chmod(mode os.FileMode) error
	Sync() error
	Close() error
}

// syncCloser is the part of *os.File the directory fsync uses.
type syncCloser interface {
	Sync() error
	Close() error
}

// fileSystem is the seam WriteFile writes through; tests swap single
// operations to reach failures (ENOSPC, EIO) a temp directory cannot produce.
type fileSystem struct {
	createTemp func(dir, pattern string) (tempFile, error)
	rename     func(oldpath, newpath string) error
	openDir    func(name string) (syncCloser, error)
}

// osFileSystem is the real filesystem every caller of WriteFile writes to.
var osFileSystem = fileSystem{
	createTemp: func(dir, pattern string) (tempFile, error) { return os.CreateTemp(dir, pattern) },
	rename:     os.Rename,
	openDir:    func(name string) (syncCloser, error) { return os.Open(name) },
}

// WriteFile writes data to path atomically and durably: it writes a temp file
// in the same directory, applies perm, fsyncs it, renames it over path and
// fsyncs the directory so the rename itself survives a power loss. On an error
// before the rename the target file is left untouched; an error from the
// directory fsync means the new content is in place but may not be durable.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	return osFileSystem.writeFile(path, data, perm)
}

func (fs fileSystem) writeFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := fs.createTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename

	if _, err := tmp.Write(data); err != nil {
		return errors.Join(err, tmp.Close())
	}
	if err := tmp.Chmod(perm); err != nil {
		return errors.Join(err, tmp.Close())
	}
	if err := tmp.Sync(); err != nil {
		return errors.Join(err, tmp.Close())
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := fs.rename(tmp.Name(), path); err != nil {
		return err
	}
	return fs.syncDir(dir)
}

// syncDir fsyncs a directory, persisting the entries just renamed into it.
func (fs fileSystem) syncDir(dir string) error {
	d, err := fs.openDir(dir)
	if err != nil {
		return err
	}
	if err := d.Sync(); err != nil {
		return errors.Join(err, d.Close())
	}
	return d.Close()
}
