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
	"strings"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/deploy"
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
	logger := slog.New(newLogHandler(config.LogFormatJSON, &buf))

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
	logger := slog.New(newLogHandler(config.LogFormatText, &buf))

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
	logger := slog.New(newLogHandler(config.LogFormatPretty, &buf))

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
	slog.New(newLogHandler("", &fallback)).Info("hello")
	if strings.HasPrefix(strings.TrimSpace(fallback.String()), "{") {
		t.Errorf("expected an empty format to fall back to pretty, got JSON: %q", fallback.String())
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
}

func TestStartServer_CleanShutdownReportsNothing(t *testing.T) {
	// http.ErrServerClosed is the normal outcome of Shutdown and must not be
	// mistaken for a failure — otherwise every graceful stop would exit 1.
	fail := make(chan error, 2)
	srv := startServer("test", 0, http.NewServeMux(), fail)

	if err := srv.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	select {
	case err := <-fail:
		t.Errorf("clean shutdown reported a failure: %v", err)
	default:
	}
}
