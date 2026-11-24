package deploy

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
)

const shortTimeoutWarning = "timeout below the compose healthcheck budget"

func TestTimeoutShortfalls(t *testing.T) {
	path := filepath.Join(t.TempDir(), "docker-compose.yml")
	writeFile(t, path, slowHealthcheckCompose)
	slow, err := parseComposeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	plainPath := filepath.Join(t.TempDir(), "docker-compose.yml")
	writeFile(t, plainPath, composeWithImage("nginx:1.25"))
	plain, err := parseComposeFile(plainPath)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		stack config.Stack
		cf    *composeFile
		want  []string
	}{
		{
			name:  "explicit gate timeout below the budget",
			stack: config.Stack{DeployHealthCheck: &config.HealthCheck{TimeoutSeconds: 30}},
			cf:    slow,
			want:  []string{"deploy_health_check.timeout_seconds: 30 is below the compose healthcheck budget of 270s", "set it to at least 270"},
		},
		{
			name:  "explicit rollout timeout below the budget",
			stack: config.Stack{Rollout: &config.Rollout{HealthTimeoutSeconds: 45}},
			cf:    slow,
			want:  []string{"rollout.health_timeout_seconds: 45 is below the compose healthcheck budget of 270s"},
		},
		{
			name:  "timeouts at the budget",
			stack: config.Stack{DeployHealthCheck: &config.HealthCheck{TimeoutSeconds: 270}, Rollout: &config.Rollout{HealthTimeoutSeconds: 300}},
			cf:    slow,
		},
		{
			name:  "omitted timeouts are derived",
			stack: config.Stack{DeployHealthCheck: &config.HealthCheck{}, Rollout: &config.Rollout{}},
			cf:    slow,
		},
		{
			name:  "disabled gate",
			stack: config.Stack{DeployHealthCheck: &config.HealthCheck{Enabled: new(false), TimeoutSeconds: 30}},
			cf:    slow,
		},
		{
			name:  "no compose healthcheck",
			stack: config.Stack{DeployHealthCheck: &config.HealthCheck{TimeoutSeconds: 30}},
			cf:    plain,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := timeoutShortfalls(tc.stack, tc.cf)
			if len(tc.want) == 0 && got != "" {
				t.Errorf("timeoutShortfalls = %q, want none", got)
			}
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("timeoutShortfalls = %q, want it to contain %q", got, want)
				}
			}
		})
	}
}

func TestDeployAllStacks_WarnsTimeoutBelowBudgetOncePerDistinctMessage(t *testing.T) {
	overrides := []config.Stack{
		{Name: "slow", DeployHealthCheck: &config.HealthCheck{TimeoutSeconds: 30}},
		{Name: "derived", DeployHealthCheck: &config.HealthCheck{}},
	}
	repoDir, cfg := discoveryRepo(t, []string{"slow", "derived"}, overrides)
	for _, name := range []string{"slow", "derived"} {
		writeFile(t, filepath.Join(repoDir, "stacks", name, "docker-compose.yml"), slowHealthcheckCompose)
	}
	d, _, _ := discoveryDeployer(t, repoDir)

	log := captureLogAt(t, slog.LevelInfo, func(*testing.T) {
		d.DeployAllStacks(context.Background(), cfg)
		d.DeployAllStacks(context.Background(), cfg)
	})

	if got := countLines(log, shortTimeoutWarning); got != 1 {
		t.Fatalf("short-timeout warnings over two identical runs = %d, want 1:\n%s", got, log)
	}
	for _, want := range []string{"stack=slow", "deploy_health_check.timeout_seconds: 30", "270s"} {
		if !strings.Contains(log, want) {
			t.Errorf("warning does not name %q:\n%s", want, log)
		}
	}
	// The slow stack's warning is the positive signal that the check ran.
	for _, line := range strings.Split(log, "\n") {
		if strings.Contains(line, shortTimeoutWarning) && strings.Contains(line, "stack=derived") {
			t.Errorf("an omitted timeout must not warn: %s", line)
		}
	}
}
