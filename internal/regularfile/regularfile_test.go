package regularfile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func TestReadFile_ReadsRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	writeFile(t, path, "hello")

	data, err := ReadFile(path, 5, FollowLinks)
	if err != nil || string(data) != "hello" {
		t.Fatalf("ReadFile = (%q, %v), want (hello, nil)", data, err)
	}
}

func TestReadFile_RejectsFileOverLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	writeFile(t, path, "hello!")

	_, err := ReadFile(path, 5, FollowLinks)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	if !strings.Contains(err.Error(), "5-byte") {
		t.Errorf("err = %q, want it to name the limit", err)
	}
}

func TestReadFile_RejectsCharDeviceBehindSymlink(t *testing.T) {
	link := filepath.Join(t.TempDir(), "zero")
	symlink(t, "/dev/zero", link)

	_, err := ReadFile(link, 1<<20, FollowLinks)
	if !errors.Is(err, ErrNotRegular) {
		t.Fatalf("err = %v, want ErrNotRegular", err)
	}
}

// A FIFO must be refused without blocking in open(2) waiting for a writer:
// a hang here is the bug.
func TestOpen_RejectsFIFOWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fifo")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}

	_, err := Open(path, FollowLinks)
	if !errors.Is(err, ErrNotRegular) {
		t.Fatalf("err = %v, want ErrNotRegular", err)
	}
}

func TestOpen_RejectsDirectory(t *testing.T) {
	_, err := Open(t.TempDir(), FollowLinks)
	if !errors.Is(err, ErrNotRegular) {
		t.Fatalf("err = %v, want ErrNotRegular", err)
	}
}

func TestOpen_FollowLinksReadsRegularTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	writeFile(t, target, "x")
	link := filepath.Join(dir, "link")
	symlink(t, target, link)

	f, err := Open(link, FollowLinks)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_ = f.Close()
}

func TestOpen_RejectLinksRefusesSymlinkToRegularFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	writeFile(t, target, "x")
	link := filepath.Join(dir, "link")
	symlink(t, target, link)

	_, err := Open(link, RejectLinks)
	if !errors.Is(err, ErrNotRegular) {
		t.Fatalf("err = %v, want ErrNotRegular", err)
	}
}

func TestOpen_MissingFileIsNotExist(t *testing.T) {
	_, err := Open(filepath.Join(t.TempDir(), "missing"), RejectLinks)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want os.ErrNotExist", err)
	}
}
