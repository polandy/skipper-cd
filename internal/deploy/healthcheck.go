package deploy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/polandy/skipper-cd/internal/config"
)

// resolveHealthCheck returns the stack's configured deploy_health_check when
// it set one. Otherwise, when the compose file declares a healthcheck: for at
// least one service, it returns an automatic gate at the default timeout with
// no URL probe: the operator already opted the service into a Docker
// healthcheck, so skipper's --wait + rollback gate (ADR-0022) applies without
// also requiring a redundant deploy_health_check: {} per stack (ADR-0046). A
// stack with no compose healthcheck anywhere, or whose compose file failed to
// parse, stays ungated exactly as an explicit absence would.
func resolveHealthCheck(explicit *config.HealthCheck, cf *composeFile) *config.HealthCheck {
	if explicit != nil {
		return explicit
	}
	if cf == nil || !cf.hasHealthcheck() {
		return nil
	}
	return &config.HealthCheck{TimeoutSeconds: config.DefaultHealthCheckTimeoutSeconds}
}

// httpDoer is the minimal HTTP client surface the health prober needs;
// tests inject a fake instead of a real *http.Client.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// defaultProbeInterval is the pause between two HTTP probe attempts.
const defaultProbeInterval = 2 * time.Second

// httpHealthProber GET-polls a URL until it answers 2xx, retrying every
// interval until a deadline expires. It implements the optional second stage
// of a stack's deploy_health_check gate (ADR-0022); the first stage is docker
// compose up --wait.
type httpHealthProber struct {
	doer     httpDoer
	interval time.Duration
}

// healthProber returns the deployer's prober, lazily constructing the real
// one on first use. Tests pre-set d.prober with a fake doer.
func (d *Deployer) healthProber() *httpHealthProber {
	if d.prober == nil {
		d.prober = &httpHealthProber{doer: &http.Client{}, interval: defaultProbeInterval}
	}
	return d.prober
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
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("unexpected status %s", resp.Status)
	}
	return nil
}
