package peers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/polandy/skipper-cd/internal/audit"
	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/peers"
)

// fakeClient is an injected Client whose per-URL result the test controls.
type fakeClient struct {
	mu    sync.Mutex
	data  map[string]peers.Data
	fail  map[string]bool
	calls int
}

func (f *fakeClient) Fetch(_ context.Context, baseURL string) (peers.Data, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.fail[baseURL] {
		return peers.Data{}, errors.New("unreachable")
	}
	return f.data[baseURL], nil
}

func testPeers() []config.Peer {
	return []config.Peer{
		{Name: "host-b", URL: "http://host-b:8001"},
		{Name: "host-c", URL: "http://host-c:8001"},
	}
}

func dataWith(deploys ...audit.Record) peers.Data {
	return peers.Data{
		State:   map[string]json.RawMessage{"health": json.RawMessage(`{"stacks":{}}`)},
		Deploys: deploys,
	}
}

func peerView(t *testing.T, s peers.State, name string) peers.PeerView {
	t.Helper()
	for _, pv := range s.Peers {
		if pv.Name == name {
			return pv
		}
	}
	t.Fatalf("peer %q not in state %+v", name, s)
	return peers.PeerView{}
}

func TestState_TagsSelfAndPeers(t *testing.T) {
	fc := &fakeClient{data: map[string]peers.Data{
		"http://host-b:8001": dataWith(audit.Record{Stack: "gitea", Status: "success"}),
		"http://host-c:8001": dataWith(),
	}}
	reg := peers.New("host-a", testPeers(), fc, time.Second)
	reg.Poll(context.Background())

	s := reg.State()
	if s.Self != "host-a" {
		t.Errorf("Self = %q, want host-a", s.Self)
	}
	if len(s.Peers) != 2 {
		t.Fatalf("expected 2 peers, got %d", len(s.Peers))
	}
	b := peerView(t, s, "host-b")
	if !b.Reachable || b.Stale {
		t.Errorf("host-b reachable=%v stale=%v, want reachable/not-stale", b.Reachable, b.Stale)
	}
	if len(b.Deploys) != 1 || b.Deploys[0].Stack != "gitea" {
		t.Errorf("host-b deploys = %+v, want one gitea record", b.Deploys)
	}
	if b.LastSeen == nil {
		t.Errorf("host-b LastSeen is nil after a successful poll")
	}
}

func TestState_PeerOrderFollowsConfig(t *testing.T) {
	fc := &fakeClient{data: map[string]peers.Data{
		"http://host-b:8001": dataWith(),
		"http://host-c:8001": dataWith(),
	}}
	reg := peers.New("host-a", testPeers(), fc, time.Second)
	reg.Poll(context.Background())
	s := reg.State()
	if s.Peers[0].Name != "host-b" || s.Peers[1].Name != "host-c" {
		t.Errorf("peer order = [%s %s], want [host-b host-c]", s.Peers[0].Name, s.Peers[1].Name)
	}
}

func TestState_UnreachablePeerKeepsLastKnownAndMarksStale(t *testing.T) {
	fc := &fakeClient{data: map[string]peers.Data{
		"http://host-b:8001": dataWith(audit.Record{Stack: "gitea", Status: "success"}),
		"http://host-c:8001": dataWith(),
	}}
	reg := peers.New("host-a", testPeers(), fc, time.Second)
	reg.Poll(context.Background()) // first poll succeeds

	before := peerView(t, reg.State(), "host-b").LastSeen

	// host-b now unreachable; host-c still fine.
	fc.fail = map[string]bool{"http://host-b:8001": true}
	reg.Poll(context.Background())

	b := peerView(t, reg.State(), "host-b")
	if b.Reachable {
		t.Errorf("host-b Reachable = true after failure, want false")
	}
	if !b.Stale {
		t.Errorf("host-b Stale = false, want true (last-known kept)")
	}
	if len(b.Deploys) != 1 || b.Deploys[0].Stack != "gitea" {
		t.Errorf("host-b lost its last-known deploys: %+v", b.Deploys)
	}
	if b.LastSeen == nil || !b.LastSeen.Equal(*before) {
		t.Errorf("host-b LastSeen changed on a failed poll: was %v now %v", before, b.LastSeen)
	}
	if c := peerView(t, reg.State(), "host-c"); !c.Reachable || c.Stale {
		t.Errorf("host-c should stay live: reachable=%v stale=%v", c.Reachable, c.Stale)
	}
}

func TestState_NeverReachedPeerIsEmptyNotStale(t *testing.T) {
	fc := &fakeClient{fail: map[string]bool{
		"http://host-b:8001": true,
		"http://host-c:8001": true,
	}}
	reg := peers.New("host-a", testPeers(), fc, time.Second)
	reg.Poll(context.Background())

	b := peerView(t, reg.State(), "host-b")
	if b.Reachable {
		t.Errorf("never-reached peer Reachable = true")
	}
	if b.Stale {
		t.Errorf("never-reached peer Stale = true, want false (no last-known data to show)")
	}
	if b.LastSeen != nil || len(b.Deploys) != 0 || len(b.State) != 0 {
		t.Errorf("never-reached peer carries data: %+v", b)
	}
}

func TestState_RecoveryClearsStale(t *testing.T) {
	fc := &fakeClient{data: map[string]peers.Data{
		"http://host-b:8001": dataWith(),
		"http://host-c:8001": dataWith(),
	}}
	reg := peers.New("host-a", testPeers(), fc, time.Second)
	reg.Poll(context.Background())
	fc.fail = map[string]bool{"http://host-b:8001": true}
	reg.Poll(context.Background())
	fc.fail = nil // host-b back
	reg.Poll(context.Background())

	if b := peerView(t, reg.State(), "host-b"); !b.Reachable || b.Stale {
		t.Errorf("recovered peer reachable=%v stale=%v, want reachable/not-stale", b.Reachable, b.Stale)
	}
}

func TestPoll_LogsReachabilityEdgesOncePerTransition(t *testing.T) {
	prev := slog.Default()
	var buf bytes.Buffer // slog's TextHandler guards its writer; Poll joins its goroutines before we read
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(prev)

	fc := &fakeClient{
		data: map[string]peers.Data{"http://host-b:8001": dataWith(), "http://host-c:8001": dataWith()},
		fail: map[string]bool{"http://host-c:8001": true}, // host-c down from the first poll
	}
	reg := peers.New("host-a", testPeers(), fc, time.Second)

	reg.Poll(context.Background()) // host-c → one "unreachable"; host-b reachable on first contact stays silent
	reg.Poll(context.Background()) // host-c still down → no repeat (edge-triggered)

	afterDown := buf.String()
	if got := strings.Count(afterDown, peers.MsgPeerUnreachable); got != 1 {
		t.Fatalf("want 1 unreachable log (host-c), got %d in %q", got, afterDown)
	}
	if strings.Contains(afterDown, "peer=host-b") {
		t.Errorf("a peer reachable from the first poll must log nothing, got %q", afterDown)
	}

	fc.fail = nil                                         // host-c recovers
	reg.Poll(context.Background())                        // → one "reachable again" for host-c
	fc.fail = map[string]bool{"http://host-b:8001": true} // host-b now drops
	reg.Poll(context.Background())                        // → one "unreachable" for host-b

	out := buf.String()
	if got := strings.Count(out, peers.MsgPeerReachable); got != 1 {
		t.Errorf("want 1 recovery log (host-c), got %d in %q", got, out)
	}
	if got := strings.Count(out, peers.MsgPeerUnreachable); got != 2 {
		t.Errorf("want 2 unreachable logs total (host-c start + host-b drop), got %d in %q", got, out)
	}
}

func TestHosts_SelfFirstThenPeers(t *testing.T) {
	fc := &fakeClient{data: map[string]peers.Data{
		"http://host-b:8001": dataWith(),
		"http://host-c:8001": dataWith(),
	}}
	reg := peers.New("host-a", testPeers(), fc, time.Second)
	reg.Poll(context.Background())

	hosts := reg.Hosts()
	if len(hosts) != 3 {
		t.Fatalf("expected 3 hosts (self + 2 peers), got %d", len(hosts))
	}
	if !hosts[0].Self || hosts[0].Name != "host-a" || !hosts[0].Reachable {
		t.Errorf("hosts[0] = %+v, want self host-a reachable", hosts[0])
	}
	if hosts[1].Name != "host-b" || hosts[1].Self {
		t.Errorf("hosts[1] = %+v, want peer host-b", hosts[1])
	}
}

// --- HTTP client (real transport, version-skew tolerance) ---

func TestHTTPClient_FetchCuratesAndToleratesUnknownFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/snapshot":
			// Includes a state the merge curates (health), one it drops
			// (autosync), and a future top-level key the primary must ignore.
			_, _ = w.Write([]byte(`{
				"health": {"stacks": {"gitea": {"status": "healthy"}}},
				"autosync": {"global": true},
				"future_state_v99": {"anything": 1}
			}`))
		case "/api/audit":
			// A record with an unknown field — must not break decoding.
			_, _ = w.Write([]byte(`[{"stack":"gitea","status":"success","future_field":"x"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := peers.NewHTTPClient(srv.Client())
	data, err := c.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if _, ok := data.State["health"]; !ok {
		t.Errorf("curated state missing health: %v", data.State)
	}
	if _, ok := data.State["autosync"]; ok {
		t.Errorf("autosync should be dropped from the merged view")
	}
	if _, ok := data.State["future_state_v99"]; ok {
		t.Errorf("unknown future state should not be curated in")
	}
	if len(data.Deploys) != 1 || data.Deploys[0].Stack != "gitea" {
		t.Errorf("deploys = %+v, want one gitea record", data.Deploys)
	}
}

func TestHTTPClient_SnapshotFailureIsUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := peers.NewHTTPClient(srv.Client())
	if _, err := c.Fetch(context.Background(), srv.URL); err == nil {
		t.Fatal("expected error when snapshot returns 500")
	}
}

func TestHTTPClient_AuditFailureIsBestEffort(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/snapshot" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"health": {"stacks": {}}}`))
			return
		}
		http.Error(w, "no audit", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := peers.NewHTTPClient(srv.Client())
	data, err := c.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("a failed audit must not fail the whole fetch: %v", err)
	}
	if len(data.Deploys) != 0 {
		t.Errorf("expected no deploys on audit failure, got %+v", data.Deploys)
	}
	if _, ok := data.State["health"]; !ok {
		t.Errorf("live state should still come through on audit failure")
	}
}
