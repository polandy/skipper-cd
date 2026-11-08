package ui

import "net/http"

// LimitConcurrent wraps a long-lived handler (an SSE stream) so at most max
// requests run at once; one over the cap gets 429 with a Retry-After, as the
// container-logs stream does.
func LimitConcurrent(maxRequests int, next http.Handler) http.Handler {
	slots := make(chan struct{}, maxRequests)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			w.Header().Set("Retry-After", "5")
			http.Error(w, "too many concurrent streams", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
