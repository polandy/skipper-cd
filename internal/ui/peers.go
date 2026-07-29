package ui

import (
	"encoding/json"
	"io"
	"net/http"
)

// The peer surface for the multi-host fan-in (ADR-0048), read-only throughout:
// the configured host set, plus two proxies that resolve a peer name to its
// base URL and forward the request there.

// PeersHandler serves GET /api/peers — the effective multi-host set (ADR-0048)
// as JSON: the primary itself first (self:true), then each configured peer,
// with reachability and last-seen but without the bulky per-host read data
// (that rides the `peers` state event). `hosts` returns the live set; it is
// passed as an opaque payload so this package need not import internal/peers
// (which would cycle through config → ui).
func PeersHandler(hosts func() any) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// The status header is implicitly 200; a failed body write cannot be
		// reported to the client anymore.
		_ = json.NewEncoder(w).Encode(map[string]any{"hosts": hosts()})
	})
}

// PeerDiffsHandler serves GET /api/peers/{name}/events/{id}/diffs — it proxies a
// peer's diff for one deploy event to the browser, which cannot reach the peer
// cross-origin (ADR-0048). `resolve` maps a peer name + event id to the peer's
// diff URL (false when name is not a configured peer); it is passed as a func so
// this package need not import internal/peers. A peer that has evicted the event
// answers a non-2xx, forwarded as-is so the UI falls back to its "open the peer"
// link.
func PeerDiffsHandler(resolve func(name, id string) (string, bool), hc *http.Client) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		url, ok := resolve(r.PathValue("name"), r.PathValue("id"))
		if !ok {
			http.Error(w, "unknown peer", http.StatusNotFound)
			return
		}
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, url, nil)
		if err != nil {
			http.Error(w, "bad peer request", http.StatusInternalServerError)
			return
		}
		req.Header.Set("Accept", "application/json")
		resp, err := hc.Do(req)
		if err != nil {
			http.Error(w, "peer unreachable", http.StatusBadGateway)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	})
}

// PeerContainerLogsHandler serves
// GET /api/peers/{name}/container-logs/{stack} — the streaming sibling of
// PeerDiffsHandler (ADR-0048). It proxies a peer's live container-logs SSE
// stream to the browser, which cannot reach the peer cross-origin. Where a
// diff is a bounded JSON body, logs are an open-ended event-stream, so this
// forwards frames with a flush after each and stays open as long as the client
// does. `resolve` maps peer name + stack to the peer's logs URL (false when
// name is not a configured peer); the incoming query (?services=/tail/since) is
// passed through, so service selection needs no path segment. hc must have no
// timeout — an SSE follow is long-lived; a client disconnect cancels the
// request context, which tears down the upstream stream (and the peer's docker
// child) instead. The peer validates the stack/services, so a non-2xx (unknown
// stack/service, peer UI off) is forwarded as-is.
func PeerContainerLogsHandler(resolve func(name, stack string) (string, bool), hc *http.Client) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		target, ok := resolve(r.PathValue("name"), r.PathValue("stack"))
		if !ok {
			http.Error(w, "unknown peer", http.StatusNotFound)
			return
		}
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
		if err != nil {
			http.Error(w, "bad peer request", http.StatusInternalServerError)
			return
		}
		req.Header.Set("Accept", "text/event-stream")
		resp, err := hc.Do(req)
		if err != nil {
			http.Error(w, "peer unreachable", http.StatusBadGateway)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			http.Error(w, "peer rejected the logs request", resp.StatusCode)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		// Forward frames, flushing after each read so the browser gets events
		// live. The read unblocks with an error when the client disconnects and
		// r.Context() cancels the upstream request.
		buf := make([]byte, 4096)
		for {
			n, rerr := resp.Body.Read(buf)
			if n > 0 {
				if _, werr := w.Write(buf[:n]); werr != nil {
					return
				}
				flusher.Flush()
			}
			if rerr != nil {
				return
			}
		}
	})
}
