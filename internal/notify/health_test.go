package notify

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/health"
	"github.com/polandy/skipper-cd/internal/healthwatch"
	"github.com/polandy/skipper-cd/internal/metrics"
)

func mustNewHealthAlerter(t *testing.T, targets []config.NotificationTarget, doer Doer) *HealthAlerter {
	t.Helper()
	a, err := NewHealthAlerter(targets, doer, 0)
	if err != nil {
		t.Fatalf("NewHealthAlerter: %v", err)
	}
	return a
}

func signalHealthTarget(prefix string) config.NotificationTarget {
	return config.NotificationTarget{
		Format:     config.NotifyFormatSignal,
		URL:        "http://signal.example:8020",
		Number:     "+4100000000",
		Recipients: []string{"+4111111111"},
		Prefix:     prefix,
	}
}

func unhealthyAlert() healthwatch.Alert {
	return healthwatch.Alert{
		Stack:            "vaultwarden",
		Service:          "vaultwarden",
		From:             health.Healthy,
		To:               health.Unhealthy,
		Since:            time.Date(2026, 7, 16, 15, 47, 5, 0, time.UTC),
		PrevDuration:     2*time.Hour + 13*time.Minute,
		Commit:           "a1b2c3d4e5f6a7b8",
		DeployCorrelated: true,
	}
}

func TestHealthAlerter_SignalMessageForNewFailure(t *testing.T) {
	doer := &fakeDoer{}
	a := mustNewHealthAlerter(t, []config.NotificationTarget{signalHealthTarget("host-a")}, doer)

	a.handle(context.Background(), unhealthyAlert())

	if doer.count() != 1 {
		t.Fatalf("expected one delivery, got %d", doer.count())
	}
	if got := doer.reqs[0].URL.String(); got != "http://signal.example:8020/v2/send" {
		t.Errorf("URL: got %q", got)
	}
	body := doer.bodies[0]
	for _, want := range []string{
		"[host-a]", "🚨", "vaultwarden/vaultwarden", "healthy → unhealthy",
		"was healthy 2h13m", "after deploy of a1b2c3d",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("signal body missing %q:\n%s", want, body)
		}
	}
}

func TestHealthAlerter_SignalMessageForRecovery(t *testing.T) {
	doer := &fakeDoer{}
	a := mustNewHealthAlerter(t, []config.NotificationTarget{signalHealthTarget("")}, doer)

	a.handle(context.Background(), healthwatch.Alert{
		Stack: "vaultwarden", Service: "vaultwarden",
		From: health.Unhealthy, To: health.Healthy,
		PrevDuration: 4*time.Minute + 12*time.Second,
	})

	body := doer.bodies[0]
	for _, want := range []string{"✅", "recovered", "vaultwarden/vaultwarden", "after 4m12s"} {
		if !strings.Contains(body, want) {
			t.Errorf("signal body missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "after deploy of") {
		t.Errorf("uncorrelated alert must not name a commit:\n%s", body)
	}
}

func TestHealthAlerter_GenericPayloadCarriesAlert(t *testing.T) {
	doer := &fakeDoer{}
	a := mustNewHealthAlerter(t, []config.NotificationTarget{{
		Format:  config.NotifyFormatGeneric,
		URL:     "https://target.example/h",
		Headers: map[string]string{"Authorization": "Bearer x"},
	}}, doer)

	a.handle(context.Background(), unhealthyAlert())

	if got := doer.reqs[0].Header.Get("Authorization"); got != "Bearer x" {
		t.Errorf("header not applied, got %q", got)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(doer.bodies[0]), &payload); err != nil {
		t.Fatalf("payload not JSON: %v", err)
	}
	if payload["type"] != "health" || payload["stack"] != "vaultwarden" ||
		payload["from"] != "healthy" || payload["to"] != "unhealthy" ||
		payload["deploy_correlated"] != true || payload["commit"] != "a1b2c3d4e5f6a7b8" {
		t.Errorf("unexpected payload: %v", payload)
	}
}

func TestHealthAlerter_DisabledWithoutTargets(t *testing.T) {
	a := mustNewHealthAlerter(t, nil, &fakeDoer{})
	if a.Enabled() {
		t.Error("no targets must report disabled")
	}
	a.Fire(unhealthyAlert()) // must not panic or block
}

func TestHealthAlerter_CountsOutcomes(t *testing.T) {
	okBefore := counterValue(t, metrics.HealthAlertsSent.WithLabelValues("signal", "ok"))
	errBefore := counterValue(t, metrics.HealthAlertsSent.WithLabelValues("signal", "error"))

	a := mustNewHealthAlerter(t, []config.NotificationTarget{signalHealthTarget("")}, &fakeDoer{})
	a.handle(context.Background(), unhealthyAlert())

	failing := mustNewHealthAlerter(t, []config.NotificationTarget{signalHealthTarget("")}, &fakeDoer{status: 500})
	failing.handle(context.Background(), unhealthyAlert())

	if got := counterValue(t, metrics.HealthAlertsSent.WithLabelValues("signal", "ok")) - okBefore; got != 1 {
		t.Errorf("ok outcome delta: got %v, want 1", got)
	}
	if got := counterValue(t, metrics.HealthAlertsSent.WithLabelValues("signal", "error")) - errBefore; got != 1 {
		t.Errorf("error outcome delta: got %v, want 1", got)
	}
}

func TestHealthAlerter_RunDeliversFiredAlerts(t *testing.T) {
	doer := &fakeDoer{}
	a := mustNewHealthAlerter(t, []config.NotificationTarget{signalHealthTarget("")}, doer)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { a.Run(ctx); close(done) }()

	a.Fire(unhealthyAlert())
	deadline := time.After(2 * time.Second)
	for doer.count() == 0 {
		select {
		case <-deadline:
			t.Fatal("alert was not delivered")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	cancel()
	<-done
}

func TestHealthAlerter_RunThreadsRunCtxIntoLiveDelivery(t *testing.T) {
	doer := &fakeDoer{}
	a := mustNewHealthAlerter(t, []config.NotificationTarget{signalHealthTarget("")}, doer)

	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), ctxProbeKey{}, "run-ctx"))
	done := make(chan struct{})
	go func() { a.Run(ctx); close(done) }()

	a.Fire(unhealthyAlert())
	deadline := time.After(2 * time.Second)
	for doer.count() == 0 {
		select {
		case <-deadline:
			t.Fatal("alert was not delivered")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	cancel()
	<-done

	got := doer.reqs[0].Context().Value(ctxProbeKey{})
	if got != "run-ctx" {
		t.Errorf("live delivery request context = %v, want it to derive from Run's ctx (\"run-ctx\")", got)
	}
}
