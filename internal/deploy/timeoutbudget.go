// Timeouts below the healthcheck budget: an explicit wait shorter than what the
// compose healthcheck needs to decide ends a slow start as "still starting",
// which holds the change (ADR-0072). Explicit config still wins; skipper warns
// once per distinct finding and names the shortfall in that failure.

package deploy

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/polandy/skipper-cd/internal/compose"
	"github.com/polandy/skipper-cd/internal/config"
)

// budgetSeconds is the compose healthchecks' readiness budget in whole seconds
// (rounded up); 0 without any healthcheck or compose file.
func budgetSeconds(cf *composeFile) int {
	if cf == nil {
		return 0
	}
	return int((cf.ReadinessBudget() + time.Second - 1) / time.Second)
}

// shortfall renders one timeout key set below the budget, or "" when it is
// omitted or long enough.
func shortfall(key string, seconds, budget int, consequence string) string {
	if seconds <= 0 || seconds >= budget {
		return ""
	}
	return fmt.Sprintf("%s: %d is below the compose healthcheck budget of %ds (start_period + (retries+1)×interval + timeout of the slowest service), so %s — remove the key to wait the budget, or set it to at least %d",
		key, seconds, budget, consequence, budget)
}

// healthCheckShortfall reports an explicit deploy_health_check.timeout_seconds
// below the budget; "" when the gate is disabled or its timeout derived.
func healthCheckShortfall(stack config.Stack, cf *composeFile) string {
	hc := stack.DeployHealthCheck
	if hc == nil || hc.IsDisabled() {
		return ""
	}
	return shortfall("deploy_health_check.timeout_seconds", hc.TimeoutSeconds, budgetSeconds(cf),
		"a container still starting at the timeout fails the deploy and holds it")
}

// rolloutShortfall reports an explicit rollout.health_timeout_seconds below
// the budget.
func rolloutShortfall(stack config.Stack, cf *composeFile) string {
	if stack.Rollout == nil {
		return ""
	}
	return shortfall("rollout.health_timeout_seconds", stack.Rollout.HealthTimeoutSeconds, budgetSeconds(cf),
		"a canary still starting at the timeout fails the rollout and holds it")
}

// timeoutShortfalls joins every shortfall of the stack; "" when there is none.
func timeoutShortfalls(stack config.Stack, cf *composeFile) string {
	var found []string
	for _, s := range []string{healthCheckShortfall(stack, cf), rolloutShortfall(stack, cf)} {
		if s != "" {
			found = append(found, s)
		}
	}
	return strings.Join(found, "; ")
}

// warnShortTimeouts logs each stack's timeouts below its healthcheck budget
// once per distinct finding. A compose file that does not parse is skipped:
// discovery or the deploy reports it.
func (d *Deployer) warnShortTimeouts(stacks []config.Stack, stacksBaseDir string) {
	current := make(map[string]bool, len(stacks))
	for _, stack := range stacks {
		cf, err := parseComposeFile(filepath.Join(stacksBaseDir, stack.Name, compose.FileName))
		if err != nil {
			continue
		}
		finding := timeoutShortfalls(stack, cf)
		if finding == "" {
			if d.shortTimeouts.clear(stack.Name) {
				slog.Info("timeouts now cover the compose healthcheck budget", "stack", stack.Name)
			}
			continue
		}
		current[stack.Name] = true
		if d.shortTimeouts.record(stack.Name, finding) {
			slog.Warn("timeout below the compose healthcheck budget: "+finding, "stack", stack.Name)
		}
	}
	d.shortTimeouts.forgetOthers(current)
}
