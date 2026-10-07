package command

import (
	"errors"
	"sync"
)

// stderrTailLines bounds how many of a command's last stderr lines Run keeps
// for its error. BuildKit prints a ~20-line summary after the failing step's
// own output, so the tail must reach past it to the step's first error line.
const stderrTailLines = 64

// ExitError is what Run returns for a command that ran and failed. Its message
// is the underlying error's ("exit status 1"); Tail holds the last lines the
// command wrote to stderr, oldest first, for a caller that wants to name the
// cause rather than just the exit status.
type ExitError struct {
	Err  error
	Tail []string
}

// Error returns the underlying error's message unchanged.
func (e *ExitError) Error() string { return e.Err.Error() }

// Unwrap exposes the underlying error (an *exec.ExitError, a context error).
func (e *ExitError) Unwrap() error { return e.Err }

// StderrTail returns the stderr tail an error from Run carries, oldest line
// first, or nil when it carries none.
func StderrTail(err error) []string {
	if ee, ok := errors.AsType[*ExitError](err); ok {
		return ee.Tail
	}
	return nil
}

// tailSink is a LineSink that keeps only the last limit lines it receives.
type tailSink struct {
	mu    sync.Mutex
	limit int
	lines []string
}

func newTailSink(limit int) *tailSink {
	return &tailSink{limit: limit}
}

// ChildLine records one line, dropping the oldest once limit are held.
func (s *tailSink) ChildLine(_, _, line, _ string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.lines) == s.limit {
		copy(s.lines, s.lines[1:])
		s.lines = s.lines[:s.limit-1]
	}
	s.lines = append(s.lines, line)
}

// snapshot returns a copy of the held lines, oldest first.
func (s *tailSink) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.lines...)
}
