package deploy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/polandy/skipper-cd/internal/config"
)

// resolveHealthCheck returns the stack's effective deploy gate. An explicit
// deploy_health_check wins: a mapping is returned unchanged, and the scalar
// deploy_health_check: false (ADR-0049) suppresses any gate — the one way to
// keep a compose healthcheck: for external monitoring without letting skipper
// --wait on it.
//
// With no explicit config, the gate is inferred:
//   - A stack with on_demand_containers gets NO gate, even if its compose file
//     declares a healthcheck: — skipper stops those containers right after up,
//     so `up --wait` would cold-start them only to be stopped again, and a slow
//     warm-up would time out into a spurious rollback (ADR-0049). An operator
//     who really wants a gate here must set deploy_health_check explicitly.
//   - Otherwise, when the compose file declares a healthcheck: for at least one
//     service, an automatic gate applies at the default timeout with no URL
//     probe: the service already opted into a Docker healthcheck, so skipper's
//     --wait + rollback gate (ADR-0022) applies without a redundant
//     deploy_health_check: {} per stack (ADR-0046).
//   - A stack with no compose healthcheck anywhere, or whose compose file
//     failed to parse, stays ungated.
func resolveHealthCheck(stack config.Stack, cf *composeFile) *config.HealthCheck {
	if explicit := stack.DeployHealthCheck; explicit != nil {
		if explicit.IsDisabled() {
			return nil
		}
		return explicit
	}
	if len(stack.OnDemandContainers) > 0 {
		return nil
	}
	if cf == nil || !cf.hasHealthcheck() {
		return nil
	}
	return &config.HealthCheck{TimeoutSeconds: config.DefaultHealthCheckTimeoutSeconds}
}

// HTTPDoer is the minimal HTTP client surface the deploy_health_check probe
// needs. Config.ProbeClient accepts it so tests inject a fake instead of a
// real *http.Client.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// defaultProbeInterval is the pause between two HTTP probe attempts.
const defaultProbeInterval = 2 * time.Second

// httpHealthProber GET-polls a URL until it answers 2xx, retrying every
// interval until a deadline expires. It implements the optional second stage
// of a stack's deploy_health_check gate (ADR-0022); the first stage is docker
// compose up --wait.
type httpHealthProber struct {
	doer     HTTPDoer
	interval time.Duration
}

// newHealthProber builds the prober from the Config probe seams, substituting
// the production defaults for zero values (New calls it exactly once).
func newHealthProber(client HTTPDoer, interval time.Duration) *httpHealthProber {
	if client == nil {
		client = &http.Client{}
	}
	if interval <= 0 {
		interval = defaultProbeInterval
	}
	return &httpHealthProber{doer: client, interval: interval}
}

// waitHealthy polls url until a 2xx response and returns nil, or returns the
// last probe error once timeout elapses. Each attempt shares the overall
// deadline, so a hanging request cannot outlive it.
func (p *httpHealthProber) waitHealthy(ctx context.Context, url string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		err := p.probeOnce(ctx, url)
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("health probe %s did not pass within %s: %w", url, timeout, err)
		case <-time.After(p.interval):
		}
	}
}

// probeOnce performs a single GET and reports a non-2xx status as an error.
func (p *httpHealthProber) probeOnce(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := p.doer.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status %s", resp.Status)
	}
	return nil
}
