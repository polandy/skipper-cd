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

func TestBroadcaster_NonBlockingOnSlowSubscriber(t *testing.T) {
	b := NewBroadcaster()
	ch, unsub := b.Subscribe()
	defer unsub()

	// Fill the buffer (size 16).
	for i := range 20 {
		b.Publish(DeployEvent{ID: int64(i), Stack: "test"})
	}

	// Should have received 16 (buffer size), rest dropped.
	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			goto done
		}
	}
done:
	if count != 16 {
		t.Errorf("expected 16 buffered events, got %d", count)
	}
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
