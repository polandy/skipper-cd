package command

import (
	"strings"
	"sync"
	"testing"
)

// recordingSink collects ChildLine calls for assertions. Safe for
// concurrent use because stdout and stderr are written concurrently.
type recordingSink struct {
	mu    sync.Mutex
	lines []sinkLine
}

type sinkLine struct {
	cmd, stream, line string
}

func (s *recordingSink) ChildLine(cmd, stream, line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lines = append(s.lines, sinkLine{cmd, stream, line})
}

func (s *recordingSink) all() []sinkLine {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]sinkLine(nil), s.lines...)
}

func TestLineWriter_SplitsWritesIntoLines(t *testing.T) {
	sink := &recordingSink{}
	w := &lineWriter{sink: sink, cmd: "docker", stream: "stdout"}

	if _, err := w.Write([]byte("one\ntwo\n")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := sink.all()
	if len(got) != 2 {
		t.Fatalf("expected 2 lines, got %d: %+v", len(got), got)
	}
	if got[0] != (sinkLine{"docker", "stdout", "one"}) || got[1] != (sinkLine{"docker", "stdout", "two"}) {
		t.Errorf("unexpected lines: %+v", got)
	}
}

func TestLineWriter_JoinsLineSplitAcrossWrites(t *testing.T) {
	sink := &recordingSink{}
	w := &lineWriter{sink: sink, cmd: "git", stream: "stderr"}

	for _, chunk := range []string{"fetch", "ing or", "igin\n"} {
		if _, err := w.Write([]byte(chunk)); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	got := sink.all()
	if len(got) != 1 || got[0].line != "fetching origin" {
		t.Errorf("expected one joined line, got %+v", got)
	}
}

func TestLineWriter_FlushesPartialLineOnClose(t *testing.T) {
	sink := &recordingSink{}
	w := &lineWriter{sink: sink, cmd: "docker", stream: "stdout"}

	_, _ = w.Write([]byte("no trailing newline"))
	if len(sink.all()) != 0 {
		t.Fatal("partial line must not be emitted before Close")
	}

	if err := w.Close(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := sink.all()
	if len(got) != 1 || got[0].line != "no trailing newline" {
		t.Errorf("expected flushed partial line, got %+v", got)
	}
}

func TestLineWriter_CloseWithoutPartialLineEmitsNothing(t *testing.T) {
	sink := &recordingSink{}
	w := &lineWriter{sink: sink, cmd: "docker", stream: "stdout"}

	_, _ = w.Write([]byte("complete\n"))
	_ = w.Close()

	if got := sink.all(); len(got) != 1 {
		t.Errorf("expected no extra line on Close, got %+v", got)
	}
}

func TestLineWriter_StripsTrailingCarriageReturn(t *testing.T) {
	sink := &recordingSink{}
	w := &lineWriter{sink: sink, cmd: "docker", stream: "stdout"}

	_, _ = w.Write([]byte("progress 50%\r\ndone\n"))

	got := sink.all()
	if len(got) != 2 || got[0].line != "progress 50%" || got[1].line != "done" {
		t.Errorf("expected CR stripped, got %+v", got)
	}
}

func TestLineWriter_CapsOverlongLines(t *testing.T) {
	sink := &recordingSink{}
	w := &lineWriter{sink: sink, cmd: "docker", stream: "stdout"}

	long := strings.Repeat("x", maxLineLength+100)
	_, _ = w.Write([]byte(long))

	got := sink.all()
	if len(got) != 1 {
		t.Fatalf("expected the overlong chunk to be emitted as one capped line, got %d lines", len(got))
	}
	if len(got[0].line) != maxLineLength {
		t.Errorf("expected capped line of %d bytes, got %d", maxLineLength, len(got[0].line))
	}

	_ = w.Close()
	got = sink.all()
	if len(got) != 2 || got[1].line != strings.Repeat("x", 100) {
		t.Errorf("expected the remainder flushed on Close, got %d lines", len(got))
	}
}

func TestLineWriter_WriteNeverReturnsError(t *testing.T) {
	// A write error would make io.MultiWriter abort the child's writes and
	// fail deploys because of logging; Write must always accept everything.
	w := &lineWriter{sink: &recordingSink{}, cmd: "docker", stream: "stdout"}

	n, err := w.Write([]byte("line\n"))
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if n != 5 {
		t.Errorf("expected full write of 5 bytes, got %d", n)
	}
}
