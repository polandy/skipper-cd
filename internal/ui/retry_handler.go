package ui

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// retryResponse is the body of an accepted POST /api/stacks/{stack}/retry.
type retryResponse struct {
	Stack string `json:"stack"`
}

// RetryHandler serves POST /api/stacks/{stack}/retry: one more attempt at a
// held stack's change (ADR-0062). request records the retry and reports whether
// the stack was held; on acceptance trigger starts a run. Answers 202 Accepted,
// or 409 Conflict when the stack has nothing held to retry. Guarded by
// RequireSameOrigin at the mux.
func RetryHandler(request func(stack string) bool, trigger func()) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stack := r.PathValue("stack")
		if !request(stack) {
			http.Error(w, "stack "+stack+" has no held change to retry", http.StatusConflict)
			return
		}
		slog.Info("retry of held change requested", "stack", stack)
		if trigger != nil {
			trigger()
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		// A failed body write cannot be reported to the client anymore; the
		// retry is recorded either way.
		_ = json.NewEncoder(w).Encode(retryResponse{Stack: stack})
	})
}
