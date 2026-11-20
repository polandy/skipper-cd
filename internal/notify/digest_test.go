package notify

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
)

var digestEnd = time.Date(2026, 8, 5, 16, 7, 0, 0, time.UTC)

func success(stack string) events.DeployEvent {
	return events.DeployEvent{Stack: stack, Status: events.StatusSuccess, Timestamp: digestEnd}
}

func signalTarget(digest string) config.NotificationTarget {
	return config.NotificationTarget{
		Format: config.NotifyFormatSignal, URL: "http://localhost:8020", Number: "+49111", Recipients: []string{"+49222"},
		On: []string{config.NotifyOnSuccess, config.NotifyOnFailed, config.NotifyOnHealExhausted}, Digest: digest,
	}
}

// runItems feeds items through the worker's dispatch in queue order, exactly
// as Run would, and returns the delivered signal messages.
func runItems(t *testing.T, n *Notifier, doer *fakeDoer, items ...queueItem) []string {
	t.Helper()
	for _, it := range items {
		n.handleItem(context.Background(), it)
	}
	msgs := make([]string, 0, len(doer.bodies))
	for _, b := range doer.bodies {
		var body struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal([]byte(b), &body); err != nil {
			t.Fatalf("decode signal body %q: %v", b, err)
		}
		msgs = append(msgs, body.Message)
	}
	return msgs
}

func ev(e events.DeployEvent) queueItem { return queueItem{ev: e} }

func runEnd() queueItem { return queueItem{runEnd: true, at: digestEnd} }

func TestNotifier_DigestBatchesARunsSuccesses(t *testing.T) {
	doer := &fakeDoer{}
	n := mustNew(t, []config.NotificationTarget{signalTarget("")}, doer)

	if got := runItems(t, n, doer, ev(success("web")), ev(success("db"))); len(got) != 0 {
		t.Fatalf("successes must wait for the end of the run, got %q", got)
	}
	got := runItems(t, n, doer, runEnd())
	if len(got) != 1 || !strings.HasPrefix(got[0], "2 Stacks deployt") || !strings.Contains(got[0], "db, web") {
		t.Fatalf("messages = %q, want one digest naming db, web", got)
	}
}

func TestNotifier_DigestOfOneSuccessIsTheSingleMessage(t *testing.T) {
	doer := &fakeDoer{}
	n := mustNew(t, []config.NotificationTarget{signalTarget(config.NotifyDigestPerRun)}, doer)

	got := runItems(t, n, doer, ev(success("web")), runEnd())
	if want := renderMessage("", success("web")); len(got) != 1 || got[0] != want {
		t.Fatalf("messages = %q, want the single-stack message %q", got, want)
	}
}

func TestNotifier_DigestOfNoSuccessSendsNothing(t *testing.T) {
	doer := &fakeDoer{}
	n := mustNew(t, []config.NotificationTarget{signalTarget("")}, doer)

	runItems(t, n, doer, runEnd())
	if doer.count() != 0 {
		t.Fatalf("a run without successes must send nothing, got %d", doer.count())
	}
}

func TestNotifier_DigestFailureIsSentAtOnceAndNotRepeated(t *testing.T) {
	doer := &fakeDoer{}
	n := mustNew(t, []config.NotificationTarget{signalTarget("")}, doer)
	failed := events.DeployEvent{Stack: "api", Status: events.StatusFailed, Error: "boom"}

	got := runItems(t, n, doer, ev(success("web")), ev(failed))
	if len(got) != 1 || !strings.HasPrefix(got[0], "api: ") {
		t.Fatalf("the failure must be sent as it happens, got %q", got)
	}
	got = runItems(t, n, doer, ev(success("db")), runEnd())
	if len(got) != 2 || !strings.HasPrefix(got[1], "2 Stacks deployt") || strings.Contains(got[1], "api") {
		t.Fatalf("messages = %q, want the failure then a digest of db, web without it", got)
	}
}

func TestNotifier_DigestNeverHoldsHealExhausted(t *testing.T) {
	doer := &fakeDoer{}
	n := mustNew(t, []config.NotificationTarget{signalTarget("")}, doer)

	runItems(t, n, doer, ev(events.DeployEvent{Stack: "web", Status: events.StatusHealExhausted}))
	if doer.count() != 1 {
		t.Fatalf("heal_exhausted must be sent at once, got %d", doer.count())
	}
}

func TestNotifier_DigestOffSendsEverySuccess(t *testing.T) {
	doer := &fakeDoer{}
	n := mustNew(t, []config.NotificationTarget{signalTarget(config.NotifyDigestOff)}, doer)

	got := runItems(t, n, doer, ev(success("web")), ev(success("db")))
	if len(got) != 2 {
		t.Fatalf("digest: off must send each success at once, got %q", got)
	}
	if got = runItems(t, n, doer, runEnd()); len(got) != 2 {
		t.Fatalf("digest: off must send nothing at the end of the run, got %q", got)
	}
}

func TestNotifier_DigestTargetsAreIndependent(t *testing.T) {
	doer := &fakeDoer{}
	n := mustNew(t, []config.NotificationTarget{signalTarget(""), genericTarget(config.NotifyOnSuccess)}, doer)
	n.targets[1].digest = false

	runItems(t, n, doer, ev(success("web")), ev(success("db")))
	if doer.count() != 2 {
		t.Fatalf("the non-digest target gets each success at once, got %d requests", doer.count())
	}
	runItems(t, n, doer, runEnd())
	if doer.count() != 3 {
		t.Fatalf("the digest target gets one digest at the end, got %d requests", doer.count())
	}
}

func TestNotifier_DigestNextRunStartsEmpty(t *testing.T) {
	doer := &fakeDoer{}
	n := mustNew(t, []config.NotificationTarget{signalTarget("")}, doer)

	runItems(t, n, doer, ev(success("a")), ev(success("b")), runEnd())
	got := runItems(t, n, doer, ev(success("c")), runEnd())
	if len(got) != 2 || got[1] != renderMessage("", success("c")) {
		t.Fatalf("messages = %q, want the second run to report only c", got)
	}
}

func TestNotifier_DigestShutdownSendsThePartialRun(t *testing.T) {
	doer := &fakeDoer{}
	n := mustNew(t, []config.NotificationTarget{signalTarget("")}, doer)
	n.now = func() time.Time { return digestEnd }
	n.Notify(success("web"))
	n.Notify(success("db"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	n.Run(ctx) // cancelled: returns after the drain

	got := runItems(t, n, doer)
	if len(got) != 1 || !strings.HasPrefix(got[0], "2 Stacks deployt") {
		t.Fatalf("messages = %q, want the in-flight run's digest", got)
	}
}

func TestNotifier_EndRunRidesTheQueue(t *testing.T) {
	n := mustNew(t, []config.NotificationTarget{signalTarget("")}, &fakeDoer{})
	n.now = func() time.Time { return digestEnd }

	n.Notify(success("web"))
	n.EndRun()
	if len(n.items) != 2 {
		t.Fatalf("EndRun must enqueue its marker behind the event, got %d items", len(n.items))
	}
	<-n.items
	if it := <-n.items; !it.runEnd || !it.at.Equal(digestEnd) {
		t.Errorf("second item = %+v, want the run-end marker at %v", it, digestEnd)
	}
}

func TestComposeDigest_Message(t *testing.T) {
	web := success("web")
	web.DurationMs = (2*time.Minute + 54*time.Second).Milliseconds()
	web.Timestamp = digestEnd.Add(-10 * time.Second)
	web.HealthGated = true
	db := success("db")
	db.ImageChanges = []events.ServiceImageChange{{Service: "server", Old: "ghcr.io/x/immich:v3.0.2", New: "ghcr.io/x/immich:v3.0.3"}}
	buf := digestBuffer{}
	buf.add(web)
	buf.add(db)
	d, _ := buf.take(digestEnd)

	m := composeDigest(d)

	// The span runs from web's start (10s + 2m54s before the end), not the sum.
	if want := "2 Stacks deployt (3m4s)"; m.Title != want {
		t.Errorf("title = %q, want %q", m.Title, want)
	}
	want := "db, web\n✓ 1 mit Health-Gate geprüft\n• db/server: v3.0.2 → v3.0.3"
	if m.Body != want {
		t.Errorf("body = %q, want %q", m.Body, want)
	}
}

func TestComposeDigest_OmitsGateLineWithoutGatedStack(t *testing.T) {
	buf := digestBuffer{}
	buf.add(success("a"))
	buf.add(success("b"))
	d, _ := buf.take(digestEnd)

	if m := composeDigest(d); strings.Contains(m.Body, wordHealthGated) {
		t.Errorf("body = %q, want no health-gate line", m.Body)
	}
}

func TestComposeDigest_CapsImageLines(t *testing.T) {
	buf := digestBuffer{}
	for _, s := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"} {
		e := success(s)
		e.ImageChanges = []events.ServiceImageChange{{Service: "app", Old: "nginx:1.25", New: "nginx:1.26"}}
		buf.add(e)
	}
	d, _ := buf.take(digestEnd)

	lines := strings.Split(composeDigest(d).Body, "\n")
	images := lines[1:]
	if len(images) != maxDigestImageLines+1 || images[len(images)-1] != "• +2 weitere" {
		t.Errorf("image lines = %q, want %d lines then \"• +2 weitere\"", images, maxDigestImageLines)
	}
}

func TestDigestBuffer_OverflowIsCountedNotDropped(t *testing.T) {
	buf := digestBuffer{}
	for range maxDigestEvents + 3 {
		buf.add(success("s"))
	}
	d, _ := buf.take(digestEnd)

	m := composeDigest(d)
	if !strings.HasPrefix(m.Title, "259 Stacks deployt") || !strings.HasSuffix(strings.Split(m.Body, "\n")[0], ", +3 weitere") {
		t.Errorf("title %q / body %q must count the overflow", m.Title, m.Body)
	}
}

func TestGenericFormatter_DigestCarriesEveryEvent(t *testing.T) {
	buf := digestBuffer{}
	web := success("web")
	web.Diffs = map[string]string{"docker-compose.yml": "secret diff"}
	buf.add(web)
	buf.add(success("db"))
	d, _ := buf.take(digestEnd)

	req, err := genericFormatter{url: "https://x.example/y"}.FormatDigest(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Kind   string               `json:"kind"`
		Stacks []string             `json:"stacks"`
		Events []events.DeployEvent `json:"events"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Kind != "run_digest" || strings.Join(body.Stacks, ",") != "db,web" || len(body.Events) != 2 {
		t.Fatalf("body = %+v, want a run_digest of db, web with both events", body)
	}
	if body.Events[1].Diffs != nil || !body.Events[1].HasDiffs {
		t.Errorf("digest events must be stripped like a single event: %+v", body.Events[1])
	}
}

func TestNtfyFormatter_DigestIsLowPriority(t *testing.T) {
	buf := digestBuffer{}
	buf.add(success("web"))
	buf.add(success("db"))
	d, _ := buf.take(digestEnd)

	req, err := ntfyFormatter{url: "https://ntfy.example/skipper", prefix: "nuc"}.FormatDigest(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get(ntfyHdrPriority); got != ntfyPrioLow {
		t.Errorf("priority = %q, want %q", got, ntfyPrioLow)
	}
	if got := req.Header.Get(ntfyHdrTitle); got != "[nuc] 2 Stacks deployt (0s)" {
		t.Errorf("title = %q", got)
	}
}
