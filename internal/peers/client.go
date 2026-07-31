package peers

import (
	"context"
	"encoding/json"
	"errors"
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

// The audit paths a peer may serve: the versioned /v1 route is the stable
// cross-host contract (ADR-0039); the unversioned one is the pre-/v1 route an
// older peer still answers, kept as a 404-only fallback for mixed-version
// host pairs.
const (
	auditPathV1     = "/api/v1/audit"
	auditPathLegacy = "/api/audit"
)

// errNotFound reports a 404 from a peer — the one status that means "route
// not registered here" (an older skipper) rather than "request failed," so it
// alone drives the legacy-audit fallback.
var errNotFound = errors.New("not found")

// fannedStates is the curated subset of a peer's snapshot the merged read views
// need: the stack roster, live health, the health-watch status timeline, and app
// links — everything the primary renders in a peer's containers panel and roster
// row for health parity (ADR-0048). Everything else in a peer's snapshot
// (autosync, queue, …) is a per-host control concern the merged overview does
// not surface, so it is dropped to keep the payload lean.
var fannedStates = []string{events.StateStacks, events.StateHealth, events.StateHealthWatch, events.StateAppLinks}

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

	deploys, err := c.fetchAudit(ctx, baseURL)
	if err != nil {
		// Best-effort: keep the peer's live state, just omit its deploy rows.
		slog.Warn("peer audit fetch failed", "peer", baseURL, "err", err)
	} else {
		data.Deploys = deploys
	}
	return data, nil
}

// fetchAudit reads a peer's recent deploy records from the versioned audit
// route (ADR-0039), falling back to the legacy unversioned route only when the
// peer answers 404 — an older skipper without /api/v1/audit. Any other failure
// is a real error and is not retried on the legacy path.
func (c *httpClient) fetchAudit(ctx context.Context, baseURL string) ([]audit.Record, error) {
	deploys, err := getJSON[[]audit.Record](ctx, c.hc, fmt.Sprintf("%s%s?limit=%d", baseURL, auditPathV1, peerAuditLimit))
	if errors.Is(err, errNotFound) {
		deploys, err = getJSON[[]audit.Record](ctx, c.hc, fmt.Sprintf("%s%s?limit=%d", baseURL, auditPathLegacy, peerAuditLimit))
	}
	if err != nil {
		return nil, err
	}
	return *deploys, nil
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
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("GET %s: %w", url, errNotFound)
	}
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
