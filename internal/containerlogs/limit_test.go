package containerlogs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// blockingStreamer reports when a stream has actually started and holds it open
// until released, so a test can pin a known number of concurrent streams
// without waiting on the clock.
type blockingStreamer struct {
	started chan struct{}
	release chan struct{}
}

func (b *blockingStreamer) Stream(context.Context, string, []string, string, []string, func(string)) error {
	b.started <- struct{}{}
	<-b.release
	return nil
}

func TestHandler_RefusesBeyondMaxConcurrentStreams(t *testing.T) {
	bs := &blockingStreamer{
		started: make(chan struct{}, maxConcurrentStreams),
		release: make(chan struct{}),
	}
	mux := http.NewServeMux()
	mux.Handle("GET /api/container-logs/{stack}", Handler(bs, okResolver("api")))

	get := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/container-logs/web", nil))
		return rec
	}

	var wg sync.WaitGroup
	for range maxConcurrentStreams {
		wg.Add(1)
		go func() {
			defer wg.Done()
			get()
		}()
	}
	// Every slot is held only once each request has entered Stream — waiting on
	// the signal rather than on a duration keeps this deterministic.
	for range maxConcurrentStreams {
		<-bs.started
	}

	if rec := get(); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d once every slot is held", rec.Code, http.StatusTooManyRequests)
	}

	close(bs.release)
	wg.Wait()

	// A finished stream returns its slot, so the endpoint recovers on its own.
	if rec := get(); rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 after the held streams finished", rec.Code)
	}
}

// A request that never reaches the streaming stage must not consume a slot.
func TestHandler_UnknownStackDoesNotConsumeASlot(t *testing.T) {
	bs := &blockingStreamer{
		started: make(chan struct{}, maxConcurrentStreams),
		release: make(chan struct{}),
	}
	close(bs.release) // streams return immediately
	mux := http.NewServeMux()
	mux.Handle("GET /api/container-logs/{stack}", Handler(bs, fakeResolver{known: false}))

	for range maxConcurrentStreams + 1 {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/container-logs/web", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 for an unknown stack", rec.Code)
		}
	}
}
