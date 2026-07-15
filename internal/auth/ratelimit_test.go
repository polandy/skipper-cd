package auth

import (
	"testing"
	"time"
)

func TestAttemptLimiter_BlocksAtMax(t *testing.T) {
	l := newAttemptLimiter(3, time.Minute)
	// max failures are each allowed (not yet blocked)...
	for i := 0; i < 3; i++ {
		if l.blocked("k") {
			t.Fatalf("blocked too early before attempt %d", i+1)
		}
		l.fail("k")
	}
	// ...and the key is locked out only from the next request on.
	if !l.blocked("k") {
		t.Fatal("key should be blocked after max failures")
	}
	if l.blocked("other") {
		t.Fatal("a different key must not be blocked")
	}
}

func TestAttemptLimiter_WindowElapsesUnblocks(t *testing.T) {
	now := time.Unix(1000, 0)
	l := newAttemptLimiter(2, time.Minute)
	l.now = func() time.Time { return now }

	l.fail("k")
	l.fail("k")
	if !l.blocked("k") {
		t.Fatal("should be blocked after 2 failures")
	}
	now = now.Add(time.Minute) // window elapsed
	if l.blocked("k") {
		t.Fatal("elapsed window should unblock the key")
	}
	// And it takes the full count again to re-block.
	l.fail("k")
	if l.blocked("k") {
		t.Fatal("one failure in the fresh window must not block")
	}
}

func TestAttemptLimiter_ClearResets(t *testing.T) {
	l := newAttemptLimiter(2, time.Minute)
	l.fail("k")
	l.fail("k")
	if !l.blocked("k") {
		t.Fatal("blocked after 2 failures")
	}
	l.clear("k")
	if l.blocked("k") {
		t.Fatal("clear should unblock the key")
	}
}
