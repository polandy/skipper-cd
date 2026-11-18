package notify

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/updatecheck"
)

func mustNewUpdateAlerter(t *testing.T, targets []config.NotificationTarget, doer Doer) *UpdateAlerter {
	t.Helper()
	a, err := NewUpdateAlerter(targets, doer, 0)
	if err != nil {
		t.Fatalf("NewUpdateAlerter: %v", err)
	}
	return a
}

func TestUpdateAlerter_SignalMessageForNewerTag(t *testing.T) {
	doer := &fakeDoer{}
	a := mustNewUpdateAlerter(t, []config.NotificationTarget{signalHealthTarget("host-a")}, doer)

	a.handle(context.Background(), updatecheck.Alert{
		Stack: "gitea", Service: "server", Running: "1.22.3", Latest: "1.22.6",
	})

	if doer.count() != 1 {
		t.Fatalf("expected one delivery, got %d", doer.count())
	}
	if got := doer.reqs[0].URL.String(); got != "http://signal.example:8020/v2/send" {
		t.Errorf("URL: got %q", got)
	}
	body := doer.bodies[0]
	for _, want := range []string{"[host-a]", "⬆️", "gitea/server", "1.22.6 available", "running 1.22.3"} {
		if !strings.Contains(body, want) {
			t.Errorf("signal body missing %q:\n%s", want, body)
		}
	}
}

func TestUpdateAlerter_SignalMessageForRebuiltTag(t *testing.T) {
	doer := &fakeDoer{}
	a := mustNewUpdateAlerter(t, []config.NotificationTarget{signalHealthTarget("")}, doer)

	a.handle(context.Background(), updatecheck.Alert{
		Stack: "proxy", Service: "traefik", Running: "v3.1", Rebuilt: true,
	})

	body := doer.bodies[0]
	for _, want := range []string{"⬆️", "proxy/traefik", "v3.1", "rebuilt upstream"} {
		if !strings.Contains(body, want) {
			t.Errorf("signal body missing %q:\n%s", want, body)
		}
	}
}

func TestUpdateAlerter_GenericPayloadCarriesUpdate(t *testing.T) {
	doer := &fakeDoer{}
	a := mustNewUpdateAlerter(t, []config.NotificationTarget{{
		Format:  config.NotifyFormatGeneric,
		URL:     "https://target.example/u",
		Headers: map[string]string{"Authorization": "Bearer x"},
	}}, doer)

	a.handle(context.Background(), updatecheck.Alert{
		Stack: "gitea", Service: "server", Running: "1.22.3", Latest: "1.22.6",
	})

	if got := doer.reqs[0].Header.Get("Authorization"); got != "Bearer x" {
		t.Errorf("header not applied, got %q", got)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(doer.bodies[0]), &payload); err != nil {
		t.Fatalf("payload not JSON: %v", err)
	}
	if payload["type"] != "update" || payload["stack"] != "gitea" || payload["service"] != "server" ||
		payload["running"] != "1.22.3" || payload["latest"] != "1.22.6" || payload["rebuilt"] != false {
		t.Errorf("unexpected payload: %v", payload)
	}
}

func TestUpdateAlerter_DisabledWithoutTargets(t *testing.T) {
	a := mustNewUpdateAlerter(t, nil, &fakeDoer{})
	if a.Enabled() {
		t.Error("no targets must report disabled")
	}
	// Fire on a disabled alerter is a no-op, not a panic.
	a.Fire(updatecheck.Alert{Stack: "x"})
}

func TestUpdateAlerter_RunDeliversFiredAlerts(t *testing.T) {
	doer := &fakeDoer{}
	a := mustNewUpdateAlerter(t, []config.NotificationTarget{signalHealthTarget("")}, doer)

	a.Fire(updatecheck.Alert{Stack: "gitea", Service: "server", Running: "1.22.3", Latest: "1.22.6"})

	// Cancelled ctx drains what is queued and returns — same contract as the
	// deploy Notifier.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a.Run(ctx)

	if doer.count() != 1 {
		t.Fatalf("expected the fired alert to be delivered on drain, got %d", doer.count())
	}
}
