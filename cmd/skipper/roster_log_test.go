package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
)

func withCapturedSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestLogStackRoster_LogsHeaderAndPerStackDetail(t *testing.T) {
	buf := withCapturedSlog(t)

	logStackRoster([]config.Stack{
		{
			Name:      "nextcloud",
			WatchDirs: []string{"./nextcloud"},
			Hooks:     config.Hooks{PreDeploy: []string{"echo hi"}, PostDeploy: []string{"echo bye"}},
		},
		{Name: "arr-stack"},
	})

	out := buf.String()
	if !strings.Contains(out, `msg="stacks resolved" stacks=2`) {
		t.Errorf("expected a header line with the stack count, got %q", out)
	}
	if !strings.Contains(out, `msg="stack discovered" stack=nextcloud pre_deploy_hooks=1 post_deploy_hooks=1`) {
		t.Errorf("expected nextcloud's hook counts, got %q", out)
	}
	if !strings.Contains(out, `msg="stack discovered" stack=arr-stack pre_deploy_hooks=0 post_deploy_hooks=0`) {
		t.Errorf("expected arr-stack with zero hooks, got %q", out)
	}
}

func TestLogStackRoster_EmptySetStillLogsHeader(t *testing.T) {
	buf := withCapturedSlog(t)

	logStackRoster(nil)

	if !strings.Contains(buf.String(), `msg="stacks resolved" stacks=0`) {
		t.Errorf("expected a header line even for an empty stack set, got %q", buf.String())
	}
}
