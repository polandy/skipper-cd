package logbuf

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// newTestLogger returns a logger routing through the tee Handler into log,
// with the wrapped handler writing logfmt to out.
func newTestLogger(log *Log, out *bytes.Buffer, opts *slog.HandlerOptions) *slog.Logger {
	return slog.New(NewHandler(slog.NewTextHandler(out, opts), log))
}

func TestHandler_ForwardsRecordsToWrappedHandler(t *testing.T) {
	log := New(10)
	var out bytes.Buffer
	logger := newTestLogger(log, &out, nil)

	logger.Info("hello", "stack", "gitea")

	if !strings.Contains(out.String(), "msg=hello") || !strings.Contains(out.String(), "stack=gitea") {
		t.Errorf("wrapped handler output missing record: %q", out.String())
	}
}

func TestHandler_AppendsLevelTimeAndMessageToLog(t *testing.T) {
	log := New(10)
	var out bytes.Buffer
	logger := newTestLogger(log, &out, nil)

	logger.Warn("disk almost full")

	entries := log.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Level != "WARN" {
		t.Errorf("expected level WARN, got %q", e.Level)
	}
	if e.Msg != "disk almost full" {
		t.Errorf("unexpected msg %q", e.Msg)
	}
	if e.Time.IsZero() {
		t.Error("expected a non-zero timestamp")
	}
}

func TestHandler_StringifiesRecordAttrs(t *testing.T) {
	log := New(10)
	var out bytes.Buffer
	logger := newTestLogger(log, &out, nil)

	logger.Error("deploy failed", "stack", "gitea", "attempt", 3, "err", errors.New("boom"))

	e := log.Entries()[0]
	want := map[string]string{"stack": "gitea", "attempt": "3", "err": "boom"}
	for k, v := range want {
		if e.Attrs[k] != v {
			t.Errorf("attr %q: expected %q, got %q", k, v, e.Attrs[k])
		}
	}
}

func TestHandler_FlattensGroupValuesWithDottedKeys(t *testing.T) {
	log := New(10)
	var out bytes.Buffer
	logger := newTestLogger(log, &out, nil)

	logger.Info("request", slog.Group("http", slog.String("method", "POST")))

	e := log.Entries()[0]
	if e.Attrs["http.method"] != "POST" {
		t.Errorf("expected http.method=POST, got attrs %+v", e.Attrs)
	}
}

func TestHandler_WithAttrsPrebindsAttrs(t *testing.T) {
	log := New(10)
	var out bytes.Buffer
	logger := newTestLogger(log, &out, nil).With("component", "webhook")

	logger.Info("received push")

	e := log.Entries()[0]
	if e.Attrs["component"] != "webhook" {
		t.Errorf("expected pre-bound attr, got %+v", e.Attrs)
	}
}

func TestHandler_WithGroupPrefixesAttrKeys(t *testing.T) {
	log := New(10)
	var out bytes.Buffer
	logger := newTestLogger(log, &out, nil).WithGroup("deploy")

	logger.Info("starting", "stack", "gitea")

	e := log.Entries()[0]
	if e.Attrs["deploy.stack"] != "gitea" {
		t.Errorf("expected deploy.stack=gitea, got %+v", e.Attrs)
	}
	// The wrapped handler keeps its native grouping.
	if !strings.Contains(out.String(), "deploy.stack=gitea") {
		t.Errorf("wrapped handler output missing group: %q", out.String())
	}
}

func TestHandler_EnabledDelegatesToWrappedHandler(t *testing.T) {
	log := New(10)
	var out bytes.Buffer
	logger := newTestLogger(log, &out, &slog.HandlerOptions{Level: slog.LevelWarn})

	logger.Info("filtered out")
	logger.Warn("kept")

	entries := log.Entries()
	if len(entries) != 1 || entries[0].Msg != "kept" {
		t.Errorf("expected only the WARN entry to be captured, got %+v", entries)
	}
}

func TestHandler_WithAttrsDoesNotMutateParent(t *testing.T) {
	log := New(10)
	var out bytes.Buffer
	base := newTestLogger(log, &out, nil)

	child := base.With("component", "webhook")
	child.Info("from child")
	base.Info("from base")

	entries := log.Entries()
	if entries[0].Attrs["component"] != "webhook" {
		t.Errorf("child entry missing pre-bound attr: %+v", entries[0].Attrs)
	}
	if _, ok := entries[1].Attrs["component"]; ok {
		t.Errorf("base entry inherited the child's attr: %+v", entries[1].Attrs)
	}
}

// The ring is bounded (DefaultCapacity entries) and every entry is streamed to
// every connected browser over /api/logs. A record carrying a payload — a file
// diff is up to 10 KB — would evict real history and push kilobytes per line
// down the stream, so the capture layer keeps messages and clamps anything
// oversized. A diff is clamped by lines, the unit its reader thinks in.
func TestHandler_ClampsAMultiLineAttrByLines(t *testing.T) {
	var buf bytes.Buffer
	log := New(8)
	logger := slog.New(NewHandler(slog.NewTextHandler(&buf, nil), log))

	diff := strings.Repeat("+added line\n", 100)
	logger.Info("file changed", "file", "flake.nix", "diff", diff)

	entries := log.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 captured entry, got %d", len(entries))
	}
	got := entries[0].Attrs["diff"]
	if n := strings.Count(got, "+added line"); n != maxAttrValueLines {
		t.Errorf("expected %d diff lines kept, got %d", maxAttrValueLines, n)
	}
	// The count names what a reader would go looking for, not a byte figure.
	if want := "(60 lines omitted)"; !strings.Contains(got, want) {
		t.Errorf("expected %q, got tail %q", want, got[max(0, len(got)-40):])
	}
	// Short attrs on the same record are untouched — only the payload is cut.
	if entries[0].Attrs["file"] != "flake.nix" {
		t.Errorf("expected the file attr intact, got %q", entries[0].Attrs["file"])
	}
	// The wrapped handler still receives the record in full: the console is the
	// surface the diff exists for.
	if strings.Count(buf.String(), "+added line") != 100 {
		t.Error("expected the wrapped handler to receive the untruncated record")
	}
}

func TestHandler_KeepsAShortMultiLineAttrVerbatim(t *testing.T) {
	log := New(8)
	logger := slog.New(NewHandler(slog.NewTextHandler(&bytes.Buffer{}, nil), log))

	diff := "@@ -1 +1 @@\n-old\n+new\n"
	logger.Info("file changed", "file", "compose.yml", "diff", diff)

	if got := log.Entries()[0].Attrs["diff"]; got != diff {
		t.Errorf("expected a small diff kept verbatim, got %q", got)
	}
}

// A value with few but very long lines must not slip past the line budget.
func TestHandler_ClampsARunawaySingleLineAttr(t *testing.T) {
	log := New(8)
	logger := slog.New(NewHandler(slog.NewTextHandler(&bytes.Buffer{}, nil), log))

	logger.Info("watch dirs", "dirs", strings.Repeat("x", maxAttrValueLen*2))

	got := log.Entries()[0].Attrs["dirs"]
	if len(got) > maxAttrValueLen+32 {
		t.Errorf("expected the value clamped, got %d bytes", len(got))
	}
	if !strings.Contains(got, "truncated") {
		t.Error("expected the clamp to announce itself")
	}
}
