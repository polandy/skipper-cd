package auth

import (
	"sync"
	"time"
)

// Rate-limit defaults for failed token attempts, keyed by client IP. After
// maxTokenAttempts wrong tokens within tokenAttemptWindow that IP is locked out
// (429) until the window elapses — throttling online brute force while staying
// well clear of a human fat-fingering the login a few times. Only the token
// path is limited; a trusted proxy is never throttled.
const (
	maxTokenAttempts   = 10
	tokenAttemptWindow = time.Minute
)

// attemptLimiter is a per-key fixed-window failure counter. The first failure in
// a fresh window starts the clock; once the count reaches max the key is blocked
// for the remainder of the window. A successful auth clears the key. It is safe
// for concurrent use.
type attemptLimiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	now    func() time.Time // injectable for tests
	seen   map[string]*attemptWindow
}

type attemptWindow struct {
	count int
	reset time.Time // when the current window elapses
}

func newAttemptLimiter(max int, window time.Duration) *attemptLimiter {
	return &attemptLimiter{
		max:    max,
		window: window,
		now:    time.Now,
		seen:   make(map[string]*attemptWindow),
	}
}

// blocked reports whether key is currently locked out. An elapsed window is
// dropped so the key starts fresh.
func (l *attemptLimiter) blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	w := l.seen[key]
	if w == nil {
		return false
	}
	if !l.now().Before(w.reset) {
		delete(l.seen, key)
		return false
	}
	return w.count >= l.max
}

// fail records one failed attempt for key, starting a new window if the previous
// one elapsed.
func (l *attemptLimiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	w := l.seen[key]
	if w == nil || !now.Before(w.reset) {
		l.sweep(now) // opportunistically drop stale windows before adding one
		w = &attemptWindow{reset: now.Add(l.window)}
		l.seen[key] = w
	}
	w.count++
}

// clear forgets key's failures; called on a successful auth.
func (l *attemptLimiter) clear(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.seen, key)
}

// sweep drops elapsed windows to bound the map under many distinct IPs. The
// caller holds the lock. Cheap: it only scans once the map is non-trivial.
func (l *attemptLimiter) sweep(now time.Time) {
	if len(l.seen) < 1024 {
		return
	}
	for k, w := range l.seen {
		if !now.Before(w.reset) {
			delete(l.seen, k)
		}
	}
}
