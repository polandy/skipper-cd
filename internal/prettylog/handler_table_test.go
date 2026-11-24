package prettylog

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// errDiskFull is the error failingWriter returns.
var errDiskFull = errors.New("disk full")

// fixedTime keeps the rendered timestamp deterministic.
var fixedTime = time.Date(2026, 7, 20, 14, 32, 7, 0, time.UTC)

// failingWriter makes Handle's write fail, so the error path is exercised
// instead of the always-succeeding bytes.Buffer.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errDiskFull }

// TestHandler_RendersAnchoredMessages drives the exported handler through the
// deploy-lifecycle messages that had no rendering test, asserting the narrated
// body ends the line, so neither a raw "key=value" fallback nor an extra tail
// passes.
func TestHandler_RendersAnchoredMessages(t *testing.T) {
	tests := []struct {
		name  string
		msg   string
		attrs []any
		want  string
	}{
		{
			name:  "starting run names the stack count",
			msg:   "starting deploy run",
			attrs: []any{"stacks", 3},
			want:  "⇢ run starting  · 3 stacks",
		},
		{
			name: "webhook accepted narrates as deploying",
			msg:  "webhook accepted, starting deploy in background",
			want: "⇢ webhook received, deploying",
		},
		{
			name:  "sync names the branch",
			msg:   "pulling latest commits",
			attrs: []any{"branch", "main"},
			want:  "⇢ sync  branch=main",
		},
		{
			name: "clone without a branch is a bare sync",
			msg:  "cloning repository",
			want: "⇢ sync",
		},
		{
			name:  "deferred stack names the autosync pause",
			msg:   "deploy deferred: autosync paused",
			attrs: []any{"stack", "nextcloud"},
			want:  "▪ nextcloud  deferred · autosync paused",
		},
		{
			name:  "self-heal restoring",
			msg:   "self-heal: restoring stack to its deployed running state",
			attrs: []any{"stack", "nextcloud"},
			want:  "⟲ nextcloud  self-heal: restoring",
		},
		{
			name:  "self-heal restored",
			msg:   "self-heal: stack restored",
			attrs: []any{"stack", "nextcloud"},
			want:  "⟲ nextcloud  self-heal: restored",
		},
		{
			name:  "stacks resolved headers the roster",
			msg:   MsgStacksResolved,
			attrs: []any{"stacks", 3},
			want:  "▣ stacks  · 3 discovered",
		},
		{
			name:  "deploying stack with no changed files",
			msg:   "deploying stack",
			attrs: []any{"stack", "nextcloud", "changed_files", []string{}},
			want:  "▸ nextcloud  changed",
		},
		{
			name:  "deploying stack with one changed file names it",
			msg:   "deploying stack",
			attrs: []any{"stack", "nextcloud", "changed_files", []string{"docker-compose.yml"}},
			want:  "▸ nextcloud  changed · docker-compose.yml",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			newTestLogger(&buf, false).Info(tt.msg, tt.attrs...)
			if line := strings.TrimSuffix(buf.String(), "\n"); !strings.HasSuffix(line, tt.want) {
				t.Errorf("render of %q = %q, want it to end with %q", tt.msg, line, tt.want)
			}
		})
	}
}

// TestHandler_RunCompleteToneTable covers every worst-outcome branch of the
// run-complete summary glyph: failure > rollback > success > idle.
func TestHandler_RunCompleteToneTable(t *testing.T) {
	base := []any{"queued", 0, "blocked", 0, "held", 0, "removed", 0, "skipped", 0, "rolled_back_unhealthy", 0}
	tests := []struct {
		name  string
		extra []any
		want  string
	}{
		{"failure outranks everything", []any{"deployed", 1, "failed", 1, "rolled_back", 1}, ansiDanger + "✗" + ansiReset},
		{"a rollback tones the summary", []any{"deployed", 1, "failed", 0, "rolled_back", 1}, ansiRollback + "↺" + ansiReset},
		{"a clean success", []any{"deployed", 2, "failed", 0, "rolled_back", 0}, ansiSuccess + "✓" + ansiReset},
		{"nothing changed is idle", []any{"deployed", 0, "failed", 0, "rolled_back", 0}, ansiDim + "·" + ansiReset},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			attrs := append(append([]any{}, base...), tt.extra...)
			newTestLogger(&buf, true).Info(MsgRunComplete, attrs...)
			if !strings.Contains(buf.String(), tt.want) {
				t.Errorf("run complete = %q, want the glyph %q", buf.String(), tt.want)
			}
		})
	}
}

// TestHandler_LevelGlyphTable drives a generic message at each level, calling
// Handle directly so the Debug record is rendered despite Enabled filtering it.
// It asserts the glyph in its position after the timestamp, since "·" also
// appears as a separator elsewhere.
func TestHandler_LevelGlyphTable(t *testing.T) {
	tests := []struct {
		name  string
		level slog.Level
		want  string
	}{
		{"error", slog.LevelError, ansiDanger + "✗" + ansiReset},
		{"warn", slog.LevelWarn, ansiWarn + "▲" + ansiReset},
		{"info is uncoloured", slog.LevelInfo, "·"},
		{"debug", slog.LevelDebug, ansiDim + "·" + ansiReset},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			h := New(&buf)
			h.color = true
			if err := h.Handle(context.Background(), recordAt(fixedTime, tt.level, "generic message")); err != nil {
				t.Fatalf("Handle: %v", err)
			}
			want := ansiDim + "14:32:07" + ansiReset + "  " + tt.want + " generic message\n"
			if buf.String() != want {
				t.Errorf("line for %s = %q, want %q", tt.level, buf.String(), want)
			}
		})
	}
}

// TestHandler_GroupPrefixingTable covers WithGroup's dot-prefixing, including
// nested groups and slog.Group attrs flattened recursively.
func TestHandler_GroupPrefixingTable(t *testing.T) {
	tests := []struct {
		name   string
		groups []string
		attr   slog.Attr
		want   string
	}{
		{"one group", []string{"http"}, slog.String("method", "POST"), "http.method=POST"},
		{"nested groups", []string{"http", "req"}, slog.String("method", "POST"), "http.req.method=POST"},
		{"group attr is flattened", nil, slog.Group("g", slog.String("x", "y")), "g.x=y"},
		{"nested group attr keeps the group prefix", []string{"out"}, slog.Group("g", slog.String("x", "y")), "out.g.x=y"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			var h slog.Handler = New(&buf)
			for _, g := range tt.groups {
				h = h.WithGroup(g)
			}
			rec := slog.NewRecord(fixedTime, slog.LevelInfo, "msg", 0)
			rec.AddAttrs(tt.attr)
			if err := h.Handle(context.Background(), rec); err != nil {
				t.Fatalf("Handle: %v", err)
			}
			if !strings.Contains(buf.String(), tt.want) {
				t.Errorf("got %q, want it to contain %q", buf.String(), tt.want)
			}
		})
	}
}

// TestHandler_WithAttrsAndGroupReturnSelfWhenEmpty pins the no-op shortcut:
// no attrs and an empty group name derive no new handler.
func TestHandler_WithAttrsAndGroupReturnSelfWhenEmpty(t *testing.T) {
	h := New(&bytes.Buffer{})
	if h.WithAttrs(nil) != h {
		t.Error("WithAttrs(nil) should return the same handler")
	}
	if h.WithGroup("") != h {
		t.Error(`WithGroup("") should return the same handler`)
	}
}

// TestHandler_HandleReturnsWriterError proves a failed write surfaces as the
// writer's own error rather than being swallowed.
func TestHandler_HandleReturnsWriterError(t *testing.T) {
	h := New(failingWriter{})
	err := h.Handle(context.Background(), recordAt(fixedTime, slog.LevelInfo, "boom"))
	if !errors.Is(err, errDiskFull) {
		t.Errorf("Handle = %v, want %v", err, errDiskFull)
	}
}

// TestHandler_DiffFileHeadersAreMetadataColored pins the +++/--- branch of the
// diff colouring: the file headers read as dim metadata, not as an addition.
func TestHandler_DiffFileHeadersAreMetadataColored(t *testing.T) {
	var buf bytes.Buffer
	newTestLogger(&buf, true).Info("file changed", "file", "x.yml",
		"diff", "--- a/x.yml\n+++ b/x.yml\n@@ -1 +1 @@\n-old\n+new\n")

	out := buf.String()
	for _, want := range []string{ansiDim + "--- a/x.yml", ansiDim + "+++ b/x.yml"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q colorized as metadata, got %q", want, out)
		}
	}
}
