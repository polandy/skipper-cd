package events

import (
	"sync"
	"testing"
	"time"
)

func TestBroadcaster_PublishReachesSubscriber(t *testing.T) {
	b := NewBroadcaster()
	ch, unsub := b.Subscribe()
	defer unsub()

	evt := DeployEvent{ID: 1, Stack: "gitea", Status: StatusSuccess}
	b.Publish(evt)

	select {
	case got := <-ch:
		if got.ID != 1 || got.Stack != "gitea" || got.Status != StatusSuccess {
			t.Errorf("unexpected event: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestBroadcaster_MultipleSubscribers(t *testing.T) {
	b := NewBroadcaster()
	ch1, unsub1 := b.Subscribe()
	defer unsub1()
	ch2, unsub2 := b.Subscribe()
	defer unsub2()

	evt := DeployEvent{ID: 42, Stack: "traefik", Status: StatusDeploying}
	b.Publish(evt)

	for i, ch := range []<-chan DeployEvent{ch1, ch2} {
		select {
		case got := <-ch:
			if got.ID != 42 {
				t.Errorf("subscriber %d: expected ID 42, got %d", i, got.ID)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d: timed out", i)
		}
	}
}

func TestBroadcaster_UnsubscribeStopsDelivery(t *testing.T) {
	b := NewBroadcaster()
	ch, unsub := b.Subscribe()
	unsub()

	b.Publish(DeployEvent{ID: 1, Stack: "test"})

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("received event after unsubscribe")
		}
	default:
		// expected: no event delivered
	}
}

// A subscriber that falls a full buffer behind is closed rather than silently
// missing values, so its reader can reconnect and resync (the SSE client
// resumes via Last-Event-ID). The buffered values still drain before the close.
func TestBroadcaster_ClosesLaggingSubscriber(t *testing.T) {
	b := NewBroadcaster()
	ch, unsub := b.Subscribe()
	defer unsub()

	for i := range subscriberBuffer + 1 {
		b.Publish(DeployEvent{ID: int64(i), Stack: "test"})
	}

	for i := range subscriberBuffer {
		evt, ok := <-ch
		if !ok {
			t.Fatalf("channel closed after %d values, want %d buffered first", i, subscriberBuffer)
		}
		if evt.ID != int64(i) {
			t.Fatalf("value %d has ID %d, want %d", i, evt.ID, i)
		}
	}
	assertClosed(t, ch)
	if b.HasSubscribers() {
		t.Error("a closed lagging subscriber must be removed")
	}
}

func TestBroadcaster_LaggingSubscriberDoesNotAffectOthers(t *testing.T) {
	b := NewBroadcaster()
	slow, unsubSlow := b.Subscribe()
	defer unsubSlow()
	fast, unsubFast := b.Subscribe()
	defer unsubFast()

	for i := range subscriberBuffer + 1 {
		b.Publish(DeployEvent{ID: int64(i)})
		<-fast
	}
	b.Publish(DeployEvent{ID: 99})

	if evt := <-fast; evt.ID != 99 {
		t.Errorf("the keeping-up subscriber got ID %d, want 99", evt.ID)
	}
	for range subscriberBuffer {
		<-slow
	}
	assertClosed(t, slow)
}

func TestBroadcaster_UnsubscribeAfterLagIsSafe(t *testing.T) {
	b := NewBroadcaster()
	_, unsub := b.Subscribe()
	for range subscriberBuffer + 1 {
		b.Publish(DeployEvent{})
	}
	unsub() // must not panic or close twice
	unsub()
}

func TestSSEPayload_StripsDiffsAndSetsHasDiffs(t *testing.T) {
	evt := DeployEvent{
		ID:     1,
		Stack:  "gitea",
		Status: StatusSuccess,
		Diffs:  map[string]string{"docker-compose.yml": "+new line"},
	}

	payload := evt.SSEPayload()
	if !payload.HasDiffs {
		t.Error("expected HasDiffs to be true")
	}
	if payload.Diffs != nil {
		t.Error("expected Diffs to be nil in SSE payload")
	}
	// Original event should be unchanged.
	if evt.Diffs == nil || len(evt.Diffs) != 1 {
		t.Error("original event Diffs should not be modified")
	}
}

func TestSSEPayload_StripsCommits(t *testing.T) {
	evt := DeployEvent{
		ID:      1,
		Stack:   "gitea",
		Status:  StatusSuccess,
		Diffs:   map[string]string{"docker-compose.yml": "+new line"},
		Commits: []CommitInfo{{SHA: "abc123", Subject: "feat: x", Author: "Jane Doe"}},
	}

	payload := evt.SSEPayload()
	if payload.Commits != nil {
		t.Error("expected Commits to be nil in SSE payload (fetched on demand)")
	}
	// Original event should be unchanged.
	if len(evt.Commits) != 1 {
		t.Error("original event Commits should not be modified")
	}
}

func TestSSEPayload_KeepsHealDrift(t *testing.T) {
	// A heal has no diff to fetch on demand, so its drift detail must survive the
	// SSE payload (unlike Diffs/Commits) — the healed row renders it directly.
	evt := DeployEvent{
		ID:        3,
		Stack:     "cyberchef",
		Status:    StatusHealed,
		HealDrift: []DriftedService{{Name: "cyberchef", Status: "unhealthy"}},
	}

	payload := evt.SSEPayload()
	if len(payload.HealDrift) != 1 || payload.HealDrift[0].Name != "cyberchef" {
		t.Fatalf("expected HealDrift preserved in SSE payload, got %+v", payload.HealDrift)
	}
}

func TestSSEPayload_NoDiffs(t *testing.T) {
	evt := DeployEvent{
		ID:     2,
		Stack:  "traefik",
		Status: StatusDeploying,
	}

	payload := evt.SSEPayload()
	if payload.HasDiffs {
		t.Error("expected HasDiffs to be false when no diffs")
	}
	if payload.Diffs != nil {
		t.Error("expected Diffs to be nil")
	}
}

func TestBroadcaster_ConcurrentPubSub(t *testing.T) {
	b := NewBroadcaster()

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch, unsub := b.Subscribe()
			defer unsub()
			b.Publish(DeployEvent{ID: 1, Stack: "test"})
			// drain
			for {
				select {
				case <-ch:
				default:
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestBroadcaster_HasSubscribers(t *testing.T) {
	b := NewStateBroadcaster()
	if b.HasSubscribers() {
		t.Fatal("expected no subscribers on a fresh broadcaster")
	}

	_, unsub := b.Subscribe()
	if !b.HasSubscribers() {
		t.Fatal("expected a subscriber after Subscribe")
	}

	unsub()
	if b.HasSubscribers() {
		t.Fatal("expected no subscribers after unsubscribe")
	}
}

func TestSSEPayload_KeepsFileChanges(t *testing.T) {
	// The Changes column renders the attribution straight from the payload —
	// there is no on-demand endpoint for it, so it must survive like HealDrift.
	evt := DeployEvent{
		ID:     4,
		Stack:  "mealie",
		Status: StatusSuccess,
		Diffs:  map[string]string{"mealie/docker-compose.yml": "+CHECK_CRON=0 12 9 * *"},
		FileChanges: []FileChange{
			{File: "mealie/docker-compose.yml", Kind: ChangeKindCompose, Services: []string{"mealie-restic"}},
		},
	}

	payload := evt.SSEPayload()

	if len(payload.FileChanges) != 1 || payload.FileChanges[0].Services[0] != "mealie-restic" {
		t.Fatalf("expected FileChanges preserved in SSE payload, got %+v", payload.FileChanges)
	}
}

// assertClosed fails unless ch is closed and drained; it never blocks.
func assertClosed[T any](t *testing.T, ch <-chan T) {
	t.Helper()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("channel delivered a value beyond its buffer, want closed")
		}
	default:
		t.Fatal("lagging subscriber's channel is still open, want closed")
	}
}
