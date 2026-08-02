package prettylog

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

// newTestLogger returns a logger writing through Handler into buf. Color is
// forced via the enabled param — a *bytes.Buffer is never a terminal, so
// New's own auto-detection would always disable it.
func newTestLogger(buf *bytes.Buffer, enabled bool) *slog.Logger {
	h := New(buf)
	h.color = enabled
	return slog.New(h)
}

func TestHandler_GenericMessageRendersLevelGlyphAndAttrs(t *testing.T) {
	var buf bytes.Buffer
	newTestLogger(&buf, false).Info("web UI enabled")

	out := buf.String()
	if !strings.Contains(out, "· web UI enabled") {
		t.Errorf("expected level glyph + message, got %q", out)
	}
}

func TestHandler_GenericMessageWithAttrs(t *testing.T) {
	var buf bytes.Buffer
	newTestLogger(&buf, false).Info("stack health polling enabled", "interval_seconds", 30, "self_heal", true)

	out := buf.String()
	if !strings.Contains(out, "interval_seconds=30") || !strings.Contains(out, "self_heal=true") {
		t.Errorf("expected rendered attrs, got %q", out)
	}
}

func TestHandler_MessageWithStackAttrIndentsUnderAnchor(t *testing.T) {
	var buf bytes.Buffer
	newTestLogger(&buf, false).Info("rollout: draining old version", "stack", "nextcloud", "service", "web")

	out := buf.String()
	if !strings.Contains(out, "  ↳ ") {
		t.Errorf("expected indented step for a stack-scoped generic message, got %q", out)
	}
	// No anchor styles this message, so the raw stack attr must still show —
	// indentation alone does not say *which* stack.
	if !strings.Contains(out, "stack=nextcloud") {
		t.Errorf("expected the stack attr still rendered since no anchor names it, got %q", out)
	}
}

func TestHandler_WarnAndErrorGlyphs(t *testing.T) {
	var buf bytes.Buffer
	log := newTestLogger(&buf, false)
	log.Warn("webhook_secret is empty")
	log.Error("failed to build notifier", "err", "boom")

	out := buf.String()
	if !strings.Contains(out, "▲ webhook_secret") {
		t.Errorf("expected warn glyph, got %q", out)
	}
	if !strings.Contains(out, "✗ failed to build notifier") {
		t.Errorf("expected error glyph, got %q", out)
	}
}

func TestHandler_DebugSuppressedByDefault(t *testing.T) {
	var buf bytes.Buffer
	newTestLogger(&buf, false).Debug("reconcile tick skipped: deploy already in progress")

	if buf.Len() != 0 {
		t.Errorf("expected debug record to be suppressed, got %q", buf.String())
	}
}

func TestHandler_DeployingStackAnchor(t *testing.T) {
	var buf bytes.Buffer
	newTestLogger(&buf, false).Info("deploying stack",
		"stack", "nextcloud", "dir", "/repo/nextcloud", "project_dir", "/repo/nextcloud",
		"changed_files", []string{"docker-compose.yml", ".env"},
	)

	out := buf.String()
	if !strings.Contains(out, "▸ nextcloud  changed · 2 files") {
		t.Errorf("expected deploying-stack narrative, got %q", out)
	}
	if strings.Contains(out, "project_dir") {
		t.Errorf("expected noisy dir attrs suppressed in the anchor rendering, got %q", out)
	}
}

func TestHandler_RunningDeployHookAnchor(t *testing.T) {
	var buf bytes.Buffer
	newTestLogger(&buf, false).Info("running deploy hook", "stack", "nextcloud", "phase", "pre_deploy", "index", 0)

	out := buf.String()
	if !strings.Contains(out, "  ↳ pre_deploy [1]") {
		t.Errorf("expected 1-based hook step narrative, got %q", out)
	}
}

func TestHandler_DeployCompleteAnchor(t *testing.T) {
	var buf bytes.Buffer
	newTestLogger(&buf, false).Info("deploy complete", "stack", "nextcloud", "event_id", int64(42))

	out := buf.String()
	if !strings.Contains(out, "✓ nextcloud  deployed") {
		t.Errorf("expected deploy-complete narrative, got %q", out)
	}
}

func TestHandler_RollbackAnchors(t *testing.T) {
	var buf bytes.Buffer
	log := newTestLogger(&buf, false)
	log.Warn("deploy failed but rolled back", "stack", "arr-stack", "err", "health check failed: timeout")
	log.Error("deploy failed, rollback ran but stack is still unhealthy", "stack", "arr-stack", "err", "still down")

	out := buf.String()
	if !strings.Contains(out, "↺ arr-stack  rolled back  — health check failed: timeout") {
		t.Errorf("expected rollback narrative with error detail, got %q", out)
	}
	if !strings.Contains(out, "↺ arr-stack  rolled back · still unhealthy  — still down") {
		t.Errorf("expected unhealthy-rollback narrative, got %q", out)
	}
}

func TestHandler_SkippedAnchorAtInfoLevel(t *testing.T) {
	var buf bytes.Buffer
	newTestLogger(&buf, false).Info("skipping stack, no changes detected", "stack", "monitoring")

	out := buf.String()
	if !strings.Contains(out, "▪ monitoring  unchanged, skipped") {
		t.Errorf("expected skip narrative, got %q", out)
	}
}

func TestHandler_RunCompleteSummary(t *testing.T) {
	var buf bytes.Buffer
	newTestLogger(&buf, false).Info(MsgRunComplete,
		"deployed", 1, "rolled_back", 1, "rolled_back_unhealthy", 0,
		"queued", 0, "blocked", 0, "skipped", 2, "failed", 0,
	)

	out := buf.String()
	if !strings.Contains(out, "run complete  1 deployed · 1 rolled back · 2 skipped") {
		t.Errorf("expected run-complete summary omitting zero counts, got %q", out)
	}
}

func TestHandler_RunCompleteToneReflectsWorstOutcome(t *testing.T) {
	var buf bytes.Buffer
	newTestLogger(&buf, true).Info(MsgRunComplete, "deployed", 0, "rolled_back", 0, "rolled_back_unhealthy", 1,
		"queued", 0, "blocked", 0, "skipped", 0, "failed", 0)

	if !strings.Contains(buf.String(), ansiDanger+"✗"+ansiReset) {
		t.Errorf("expected danger-colored glyph when a rollback left a stack unhealthy, got %q", buf.String())
	}
}

func TestHandler_StackDiscoveredAnchor(t *testing.T) {
	var buf bytes.Buffer
	newTestLogger(&buf, false).Info(MsgStackDiscovered,
		"stack", "nextcloud", "pre_deploy_hooks", 1, "post_deploy_hooks", 1,
		"watch_dirs", []string{"./nextcloud"},
	)

	out := buf.String()
	if !strings.Contains(out, "◆ nextcloud") || !strings.Contains(out, "hooks pre_deploy·1 post_deploy·1") || !strings.Contains(out, "watch ./nextcloud") {
		t.Errorf("expected stack-discovered narrative with hooks and watch dirs, got %q", out)
	}
}

func TestHandler_StackDiscoveredAnchorWithoutHooksOrWatch(t *testing.T) {
	var buf bytes.Buffer
	newTestLogger(&buf, false).Info(MsgStackDiscovered, "stack", "arr-stack", "pre_deploy_hooks", 0, "post_deploy_hooks", 0, "watch_dirs", []string{})

	out := buf.String()
	if !strings.Contains(out, "hooks —") || !strings.Contains(out, "watch —") {
		t.Errorf("expected em-dash placeholders when a stack has no hooks/watch dirs, got %q", out)
	}
}

func TestHandler_StacksDisabledAnchor(t *testing.T) {
	var buf bytes.Buffer
	newTestLogger(&buf, false).Info(MsgStacksDisabled, "stacks", []string{"legacy-app", "archived"})

	out := buf.String()
	if !strings.Contains(out, "▪ parked · disabled: legacy-app, archived") {
		t.Errorf("expected the parked/disabled stack names narrated, got %q", out)
	}
}

func TestHandler_ColorWrapsGlyphAndResets(t *testing.T) {
	var buf bytes.Buffer
	newTestLogger(&buf, true).Error("deploy failed", "stack", "immich", "err", "boom")

	out := buf.String()
	if !strings.Contains(out, ansiDanger+"✗"+ansiReset) {
		t.Errorf("expected the glyph wrapped in the danger color code, got %q", out)
	}
}

func TestHandler_WithAttrsBindsAcrossRecords(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf, false).With("component", "reconcile")
	logger.Info("periodic reconcile enabled", "interval_seconds", 60)

	if !strings.Contains(buf.String(), "component=reconcile") {
		t.Errorf("expected bound attr in output, got %q", buf.String())
	}
}

func TestHandler_WithGroupPrefixesAttrKeys(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf, false).WithGroup("http")
	logger.Info("request", slog.String("method", "POST"))

	if !strings.Contains(buf.String(), "http.method=POST") {
		t.Errorf("expected group-prefixed attr key, got %q", buf.String())
	}
}

func TestHandler_TimestampFormat(t *testing.T) {
	var buf bytes.Buffer
	h := New(&buf)
	h.color = false
	ts := time.Date(2026, 7, 20, 14, 32, 7, 0, time.UTC)
	line := render(recordAt(ts, slog.LevelInfo, "web UI enabled"), nil, false)

	if !strings.HasPrefix(line, "14:32:07  ") {
		t.Errorf("expected HH:MM:SS timestamp prefix, got %q", line)
	}
}

func recordAt(t time.Time, level slog.Level, msg string) slog.Record {
	return slog.NewRecord(t, level, msg, 0)
}

func TestHandler_MultiHostFanInStartupLine(t *testing.T) {
	var buf bytes.Buffer
	newTestLogger(&buf, false).Info("multi-host fan-in enabled", "peers", 2, "poll_interval_seconds", 3)

	out := buf.String()
	// Anchored: the narrated body, not a raw "peers=2 poll_interval_seconds=3".
	if !strings.Contains(out, "⇢ multi-host fan-in") || !strings.Contains(out, "2 peers · poll 3s") {
		t.Errorf("expected narrated fan-in startup line, got %q", out)
	}
	if strings.Contains(out, "peers=2") {
		t.Errorf("expected attrs consumed by the anchor body, got raw attrs: %q", out)
	}
}

func TestHandler_PeerReachabilityEdges(t *testing.T) {
	var buf bytes.Buffer
	log := newTestLogger(&buf, false)
	log.Warn("peer unreachable", "peer", "host-c", "err", "dial tcp: refused")
	log.Info("peer reachable again", "peer", "host-c")

	out := buf.String()
	if !strings.Contains(out, "▲ peer host-c  unreachable") {
		t.Errorf("expected narrated unreachable edge, got %q", out)
	}
	if !strings.Contains(out, "— dial tcp: refused") {
		t.Errorf("expected the failure reason appended, got %q", out)
	}
	if !strings.Contains(out, "✓ peer host-c  reachable again") {
		t.Errorf("expected narrated recovery edge, got %q", out)
	}
}

func TestColorEnabled_RespectsNoColorEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if colorEnabled(os.Stdout) {
		t.Error("expected NO_COLOR to disable color even for a terminal-like writer")
	}
}

func TestColorEnabled_FalseForNonFileWriter(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	var buf bytes.Buffer
	if colorEnabled(&buf) {
		t.Error("expected a non-*os.File writer to disable color")
	}
}

func TestHandler_EnabledMatchesDefaultInfoThreshold(t *testing.T) {
	h := New(&bytes.Buffer{})
	if h.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("expected Debug disabled by default")
	}
	if !h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("expected Info enabled by default")
	}
}

func TestHandler_FileChangedRendersTheDiffBlock(t *testing.T) {
	var buf bytes.Buffer
	newTestLogger(&buf, false).Info("file changed", "file", "flake.nix",
		"diff", "@@ -13,7 +13,7 @@\n-    old line\n+    new line\n     context\n")

	out := buf.String()
	if !strings.Contains(out, "↳ flake.nix") {
		t.Errorf("expected the file name to still lead the block, got %q", out)
	}
	for _, want := range []string{"@@ -13,7 +13,7 @@", "-    old line", "+    new line", "     context"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected diff line %q in the output, got %q", want, out)
		}
	}
	// The block is indented past the timestamp column so it reads as detail
	// under its file rather than as further log lines.
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n")[1:] {
		if !strings.HasPrefix(line, "      ") {
			t.Errorf("expected every diff line indented, got %q", line)
		}
	}
}

func TestHandler_FileChangedWithoutDiffIsUnchanged(t *testing.T) {
	var buf bytes.Buffer
	newTestLogger(&buf, false).Info("file changed", "file", "flake.lock")

	if got := strings.Count(buf.String(), "\n"); got != 1 {
		t.Errorf("expected a single line when no diff is attached, got %d lines: %q", got, buf.String())
	}
}

func TestHandler_DiffBlockColorsAddsRemovesAndHunks(t *testing.T) {
	var buf bytes.Buffer
	newTestLogger(&buf, true).Info("file changed", "file", "compose.yml",
		"diff", "@@ -1 +1 @@\n-old\n+new\n")

	out := buf.String()
	for _, want := range []string{ansiSuccess + "+new", ansiDanger + "-old", ansiWarn + "@@ -1 +1 @@"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q colorized, got %q", want, out)
		}
	}
}
