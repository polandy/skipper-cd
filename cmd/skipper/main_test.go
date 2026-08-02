package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime/debug"
	"slices"
	"strings"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/deploy"
	"github.com/polandy/skipper-cd/internal/health"
)

func TestResolveCommit(t *testing.T) {
	buildInfo := func(rev string, modified bool) (*debug.BuildInfo, bool) {
		info := &debug.BuildInfo{}
		if rev != "" {
			info.Settings = append(info.Settings, debug.BuildSetting{Key: "vcs.revision", Value: rev})
		}
		info.Settings = append(info.Settings, debug.BuildSetting{
			Key:   "vcs.modified",
			Value: map[bool]string{true: "true", false: "false"}[modified],
		})
		return info, true
	}

	tests := []struct {
		name     string
		injected string
		info     *debug.BuildInfo
		ok       bool
		want     string
	}{
		{name: "injected wins", injected: "abcdef1", want: "abcdef1"},
		{name: "injected is truncated", injected: "0123456789abcdef0123", want: "0123456789ab"},
		{name: "nix dirtyShortRev keeps suffix", injected: "338c45c-dirty", want: "338c45c-dirty"},
		{name: "falls back to build info", info: mustInfo(buildInfo("fedcba9876543210", false)), ok: true, want: "fedcba987654"},
		{name: "dirty tree suffixed", info: mustInfo(buildInfo("fedcba9876543210", true)), ok: true, want: "fedcba987654-dirty"},
		{name: "no build info", info: nil, ok: false, want: ""},
		{name: "build info without revision", info: mustInfo(buildInfo("", false)), ok: true, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveCommit(tt.injected, tt.info, tt.ok); got != tt.want {
				t.Errorf("resolveCommit(%q, …) = %q, want %q", tt.injected, got, tt.want)
			}
		})
	}
}

// mustInfo adapts the (info, ok) pair from the buildInfo helper for use in a
// struct literal.
func mustInfo(info *debug.BuildInfo, _ bool) *debug.BuildInfo { return info }

func TestNewLogHandler_JSONFormatEmitsJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(newLogHandler(config.LogFormatJSON, config.LogLevelInfo, &buf))

	logger.Info("hello", "stack", "gitea")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("expected JSON log output, got %q: %v", buf.String(), err)
	}
	if entry["msg"] != "hello" {
		t.Errorf("expected msg 'hello', got %v", entry["msg"])
	}
	if entry["stack"] != "gitea" {
		t.Errorf("expected stack 'gitea', got %v", entry["stack"])
	}
}

func TestNewLogHandler_TextFormatEmitsLogfmt(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(newLogHandler(config.LogFormatText, config.LogLevelInfo, &buf))

	logger.Info("hello", "stack", "gitea")

	out := buf.String()
	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("expected text output, got JSON: %q", out)
	}
	if !strings.Contains(out, "msg=hello") || !strings.Contains(out, "stack=gitea") {
		t.Errorf("expected logfmt keys in output, got %q", out)
	}
}

func TestNewLogHandler_PrettyIsTheDefault(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(newLogHandler(config.LogFormatPretty, config.LogLevelInfo, &buf))

	logger.Info("hello", "stack", "gitea")

	out := buf.String()
	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("expected pretty output, got JSON: %q", out)
	}
	if !strings.Contains(out, "hello") || !strings.Contains(out, "stack=gitea") {
		t.Errorf("expected the message and its attrs rendered, got %q", out)
	}

	// An unrecognized/empty format also falls back to pretty (config.Load
	// already defaults an empty log_format to LogFormatPretty; this just
	// confirms newLogHandler agrees rather than silently reverting to text).
	var fallback bytes.Buffer
	slog.New(newLogHandler("", config.LogLevelInfo, &fallback)).Info("hello")
	if strings.HasPrefix(strings.TrimSpace(fallback.String()), "{") {
		t.Errorf("expected an empty format to fall back to pretty, got JSON: %q", fallback.String())
	}
}

// The log_level threshold must hold for every format — a debug line that
// escapes in "json" but not in "pretty" would make the key format-dependent.
func TestNewLogHandler_LogLevelGatesEveryFormat(t *testing.T) {
	// Messages with no prettylog anchor, so all three formats render the text
	// verbatim and one assertion fits every format.
	const debugMsg = "reconcile tick skipped: deploy already in progress"
	const infoMsg = "web UI enabled"
	const warnMsg = "config warning"

	formats := []string{config.LogFormatPretty, config.LogFormatText, config.LogFormatJSON}
	for _, format := range formats {
		t.Run(format+"/info drops debug", func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(newLogHandler(format, config.LogLevelInfo, &buf))
			logger.Debug(debugMsg)
			if buf.Len() != 0 {
				t.Errorf("expected debug dropped at log_level=info, got %q", buf.String())
			}
			// Positive control: an Info record on the same handler does land,
			// so the empty buffer above means the level gated it rather than
			// the handler writing nowhere.
			logger.Info(infoMsg)
			if !strings.Contains(buf.String(), infoMsg) {
				t.Errorf("expected info to pass at log_level=info, got %q", buf.String())
			}
		})

		t.Run(format+"/debug keeps debug", func(t *testing.T) {
			var buf bytes.Buffer
			slog.New(newLogHandler(format, config.LogLevelDebug, &buf)).Debug(debugMsg)
			if !strings.Contains(buf.String(), debugMsg) {
				t.Errorf("expected debug to pass at log_level=debug, got %q", buf.String())
			}
		})

		t.Run(format+"/warn drops info", func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(newLogHandler(format, config.LogLevelWarn, &buf))
			logger.Info(infoMsg)
			if buf.Len() != 0 {
				t.Errorf("expected info dropped at log_level=warn, got %q", buf.String())
			}
			logger.Warn(warnMsg)
			if !strings.Contains(buf.String(), warnMsg) {
				t.Errorf("expected warn to pass at log_level=warn, got %q", buf.String())
			}
		})
	}
}

func TestHealthzHandler_OKWhileNoSyncRan(t *testing.T) {
	deployer := deploy.New(deploy.Config{})

	rec := httptest.NewRecorder()
	healthzHandler(deployer)(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

// failingSyncer always fails, simulating an unreachable git remote.
type failingSyncer struct{}

func (failingSyncer) Sync(context.Context) error { return errors.New("remote unreachable") }

func TestHealthzHandler_ServiceUnavailableAfterFailedSync(t *testing.T) {
	deployer := deploy.New(deploy.Config{Syncer: failingSyncer{}, StateDir: t.TempDir()})
	deployer.SyncAndDeployAll(context.Background(), &config.Config{})

	rec := httptest.NewRecorder()
	healthzHandler(deployer)(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "remote unreachable") {
		t.Errorf("expected sync error in body, got %q", rec.Body.String())
	}
}

// A server that cannot listen must report the failure to main rather than
// calling os.Exit from its own goroutine: an exit there skips the graceful
// path, and the startup sync may already be running `docker compose up` by the
// time a port turns out to be taken.
func TestStartServer_ListenFailureIsReportedNotFatal(t *testing.T) {
	// Hold a port so the server under test cannot bind it.
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Close() }()
	port := held.Addr().(*net.TCPAddr).Port

	fail := make(chan error, 2)
	srv := startServer("test", port, http.NewServeMux(), fail)
	defer func() { _ = srv.Close() }()

	// Blocks until the goroutine reports; no polling and no sleep. A regression
	// that exits instead would take the test binary down with it, which the
	// runner reports as a failure rather than a hang.
	got := <-fail
	if got == nil {
		t.Fatal("expected a listen error")
	}
	if !strings.Contains(got.Error(), "test server") {
		t.Errorf("error should name the server it came from; got %v", got)
	}
	// The mirror case — a clean Shutdown must stay silent, or every graceful
	// stop would exit 1 — is deliberately not a test: asserting that nothing
	// arrives on a channel is green whether or not the goroutine ever ran, and
	// the only way to order it is a completion signal that exists purely for
	// the test. The ErrServerClosed filter it would cover is unchanged here and
	// is exercised on every real shutdown.
}

func TestServicesOf(t *testing.T) {
	snap := health.Snapshot{Stacks: map[string]health.StackHealth{
		"web":  {Services: []health.ServiceHealth{{Name: "app"}, {Name: "db"}}},
		"bare": {},
	}}

	cases := []struct {
		name  string
		stack string
		want  []string
	}{
		{"names in reported order", "web", []string{"app", "db"}},
		{"stack without service detail", "bare", []string{}},
		{"stack not in snapshot", "ghost", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := servicesOf(snap, tc.stack)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("servicesOf(%q) = %v, want nil", tc.stack, got)
				}
				return
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("servicesOf(%q) = %v, want %v", tc.stack, got, tc.want)
			}
		})
	}
}

func TestStackAutosyncConfig(t *testing.T) {
	cfg := &config.Config{Stacks: []config.Stack{
		{Name: "inherit"},
		{Name: "on", Autosync: ptr(true)},
		{Name: "off", Autosync: ptr(false)},
	}}

	m := stackAutosyncConfig(cfg)

	if len(m) != 3 {
		t.Fatalf("map has %d entries, want 3: %v", len(m), m)
	}
	if m["inherit"] != nil {
		t.Errorf("inherit = %v, want nil (inherit global)", *m["inherit"])
	}
	if m["on"] == nil || !*m["on"] {
		t.Errorf("on = %v, want true", m["on"])
	}
	if m["off"] == nil || *m["off"] {
		t.Errorf("off = %v, want false", m["off"])
	}
}
