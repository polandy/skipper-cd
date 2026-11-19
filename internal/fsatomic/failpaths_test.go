package fsatomic

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

var errInjected = errors.New("injected failure")

// failingTemp wraps a real temp file and fails the one operation named failOn.
type failingTemp struct {
	*os.File
	failOn string
	closes int
}

func (f *failingTemp) Write(b []byte) (int, error) {
	if f.failOn == "write" {
		return 0, errInjected
	}
	return f.File.Write(b)
}

func (f *failingTemp) Chmod(mode os.FileMode) error {
	if f.failOn == "chmod" {
		return errInjected
	}
	return f.File.Chmod(mode)
}

func (f *failingTemp) Sync() error {
	if f.failOn == "sync" {
		return errInjected
	}
	return f.File.Sync()
}

func (f *failingTemp) Close() error {
	f.closes++
	err := f.File.Close()
	if f.failOn == "close" {
		return errInjected
	}
	return err
}

// failingDir wraps a real directory handle and fails the one operation named failOn.
type failingDir struct {
	*os.File
	failOn string
	closes int
}

func (d *failingDir) Sync() error {
	if d.failOn == "dir-sync" {
		return errInjected
	}
	return d.File.Sync()
}

func (d *failingDir) Close() error {
	d.closes++
	err := d.File.Close()
	if d.failOn == "dir-close" {
		return errInjected
	}
	return err
}

// recordingFS is the real filesystem with one injected failure, keeping the
// handles it handed out so a test can check each was closed exactly once.
type recordingFS struct {
	failOn string
	tmp    *failingTemp
	dir    *failingDir
}

func (r *recordingFS) fileSystem() fileSystem {
	return fileSystem{
		createTemp: func(dir, pattern string) (tempFile, error) {
			f, err := os.CreateTemp(dir, pattern)
			if err != nil {
				return nil, err
			}
			r.tmp = &failingTemp{File: f, failOn: r.failOn}
			return r.tmp, nil
		},
		rename: func(oldpath, newpath string) error {
			if r.failOn == "rename" {
				return errInjected
			}
			return os.Rename(oldpath, newpath)
		},
		openDir: func(name string) (syncCloser, error) {
			d, err := os.Open(name)
			if err != nil {
				return nil, err
			}
			r.dir = &failingDir{File: d, failOn: r.failOn}
			return r.dir, nil
		},
	}
}

func TestWriteFile_EveryFailureIsReportedAndCleansUp(t *testing.T) {
	const original, replacement = "committed: true\n", "committed: false\n"
	tests := []struct {
		failOn string
		// renamed: the failure comes after the rename, so the new content is in place.
		renamed bool
	}{
		{failOn: "write"},
		{failOn: "chmod"},
		{failOn: "sync"},
		{failOn: "close"},
		{failOn: "rename"},
		{failOn: "dir-sync", renamed: true},
		{failOn: "dir-close", renamed: true},
	}
	for _, tt := range tests {
		t.Run(tt.failOn, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "state.yaml")
			if err := os.WriteFile(path, []byte(original), PrivateFileMode); err != nil {
				t.Fatal(err)
			}
			fs := &recordingFS{failOn: tt.failOn}

			err := fs.fileSystem().writeFile(path, []byte(replacement), PrivateFileMode)

			if !errors.Is(err, errInjected) {
				t.Fatalf("err = %v, want the injected %q failure", err, tt.failOn)
			}
			want := original
			if tt.renamed {
				want = replacement
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != want {
				t.Errorf("content = %q, want %q", got, want)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != "state.yaml" {
				t.Errorf("expected only state.yaml in dir (temp file removed), got %v", entries)
			}
			if fs.tmp.closes != 1 {
				t.Errorf("temp file closed %d times, want 1", fs.tmp.closes)
			}
			if tt.renamed && fs.dir.closes != 1 {
				t.Errorf("directory handle closed %d times, want 1", fs.dir.closes)
			}
		})
	}
}
