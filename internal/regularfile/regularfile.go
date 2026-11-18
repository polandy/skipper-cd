// Package regularfile opens files a Git push controls. A pushed path can name
// a FIFO (open blocks forever), a device like /dev/zero (reads never end) or a
// symlink to anything the daemon can read, so every read of a repo path goes
// through here: only a regular file is opened, and reads are capped.
package regularfile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
)

// ErrNotRegular means the path is not a regular file (or, under RejectLinks,
// is a symlink).
var ErrNotRegular = errors.New("not a regular file")

// ErrTooLarge means the file exceeds the caller's read limit.
var ErrTooLarge = errors.New("file too large")

// Links says whether a symlink as the path's final component is followed.
type Links bool

const (
	// FollowLinks opens a symlink's target, which must itself be regular.
	FollowLinks Links = true
	// RejectLinks refuses a symlink outright, wherever it points.
	RejectLinks Links = false
)

// Open opens path read-only if it is a regular file. O_NONBLOCK keeps open(2)
// from waiting on a FIFO's writer; the type is then checked on the opened
// descriptor, so the path cannot be swapped between check and read.
func Open(path string, links Links) (*os.File, error) {
	flags := os.O_RDONLY | syscall.O_NONBLOCK
	if links == RejectLinks {
		flags |= syscall.O_NOFOLLOW
	}
	f, err := os.OpenFile(path, flags, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, fmt.Errorf("%s: %w (symlink); commit the file itself", path, ErrNotRegular)
		}
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("%s: %w (mode %s)", path, ErrNotRegular, info.Mode().Type())
	}
	return f, nil
}

// ReadFile reads path whole if it is a regular file of at most limit bytes.
func ReadFile(path string, limit int64, links Links) ([]byte, error) {
	f, err := Open(path, links)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, TooLarge(path, limit)
	}
	return data, nil
}

// TooLarge returns the ErrTooLarge error for path, naming the limit — for a
// caller that streams the file itself rather than reading it whole.
func TooLarge(path string, limit int64) error {
	return fmt.Errorf("%s: %w: exceeds the %d-byte limit", path, ErrTooLarge, limit)
}
