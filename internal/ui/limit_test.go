package ui

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLimitConcurrent_RejectsOverTheCapAndFreesTheSlotOnReturn(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	h := LimitConcurrent(1, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		entered <- struct{}{}
		<-release
	}))
	serve := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/events", nil))
		return rec
	}

	firstDone := make(chan struct{})
	go func() { serve(); close(firstDone) }()
	<-entered

	if rec := serve(); rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Errorf("second stream: got %d (Retry-After %q), want 429 with Retry-After", rec.Code, rec.Header().Get("Retry-After"))
	}

	close(release)
	<-firstDone
	if rec := serve(); rec.Code != http.StatusOK {
		t.Errorf("after the first stream ended: got %d, want 200", rec.Code)
	}
	if len(entered) != 1 {
		t.Errorf("handler entered %d more times, want 1 (only the stream after the slot freed)", len(entered))
	}
}
