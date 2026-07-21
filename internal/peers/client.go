package peers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/polandy/skipper-cd/internal/audit"
	"github.com/polandy/skipper-cd/internal/events"
)

// peerAuditLimit bounds how many of a peer's recent deploy records the fan-in
// pulls for the merged feed — enough for an at-a-glance cross-host overview
// without transferring a peer's whole history every poll.
const peerAuditLimit = 100

// fannedStates is the curated subset of a peer's snapshot the merged read views
// need: the stack roster, live health, and app links. Everything else in a
// peer's snapshot (autosync, queue, …) is a per-host control concern the merged
// overview does not surface, so it is dropped to keep the payload lean.
var fannedStates = []string{events.StateStacks, events.StateHealth, events.StateAppLinks}

// httpClient fetches a peer's read data over HTTP. It is the production Client;
// tests inject a fake peer instead.
type httpClient struct {
	hc *http.Client
}

// NewHTTPClient returns a Client that reads peers over HTTP using hc (whose
// Timeout, together with the Registry's per-fetch context deadline, bounds a
// slow peer).
func NewHTTPClient(hc *http.Client) Client { return &httpClient{hc: hc} }

// Fetch reads one peer's merged read data. The snapshot is the reachability
// signal — if it fails the peer is unreachable and the whole fetch errors. The
// audit history is best-effort: a failure there yields no peer deploy rows but
// does not mark the peer unreachable, since its live state still came through.
func (c *httpClient) Fetch(ctx context.Context, baseURL string) (Data, error) {
	baseURL = normalizeBaseURL(baseURL)
	snap, err := getJSON[map[string]json.RawMessage](ctx, c.hc, baseURL+"/api/v1/snapshot")
	if err != nil {
		return Data{}, err
	}
	data := Data{State: make(map[string]json.RawMessage, len(fannedStates))}
	for _, name := range fannedStates {
		if v, ok := (*snap)[name]; ok {
			data.State[name] = v
		}
	}

	deploys, err := getJSON[[]audit.Record](ctx, c.hc, fmt.Sprintf("%s/api/audit?limit=%d", baseURL, peerAuditLimit))
	if err != nil {
		// Best-effort: keep the peer's live state, just omit its deploy rows.
		slog.Warn("peer audit fetch failed", "peer", baseURL, "err", err)
	} else {
		data.Deploys = *deploys
	}
	return data, nil
}

// getJSON fetches url and decodes the JSON body into T. Unknown fields are
// ignored (encoding/json's default), which is exactly the version-skew
// tolerance the fan-in relies on: a peer running a newer skipper may add fields
// the primary does not know, and they are simply dropped.
func getJSON[T any](ctx context.Context, hc *http.Client, url string) (*T, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: status %s", url, resp.Status)
	}
	var v T
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return nil, fmt.Errorf("GET %s: decode: %w", url, err)
	}
	return &v, nil
}

// normalizeBaseURL trims a trailing slash so path joins produce a single
// separator regardless of how the peer URL was written in config.
func normalizeBaseURL(u string) string { return strings.TrimRight(u, "/") }
