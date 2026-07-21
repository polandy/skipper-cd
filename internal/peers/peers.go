// Package peers fans in read data from other skipper instances (ADR-0048).
//
// The primary skipper polls each configured peer's read API, tags every
// record with the originating host, and exposes one merged model its own UI
// renders. skipper stays decentralized: every host runs its own full skipper;
// the primary is only a read-side convenience layer. Nothing here writes
// deploy state — the primary only ever reads from peers, so no deploy,
// rollback or state invariant is touched by this path.
package peers

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/polandy/skipper-cd/internal/audit"
	"github.com/polandy/skipper-cd/internal/config"
)

// Client is the consumer-side seam over one peer's read API (its `/api/v1`
// surface, ADR-0039). The HTTP implementation is NewHTTPClient; tests inject a
// fake peer. A non-nil error means the peer was unreachable this cycle.
type Client interface {
	Fetch(ctx context.Context, baseURL string) (Data, error)
}

// Data is one peer's fanned-in read data. State holds the curated subset of the
// peer's snapshot the merged views need (keyed by state-event name, e.g.
// "stacks", "health", "app_links"), passed through as raw JSON so a peer field
// the primary does not know is ignored, never a parse break (version-skew
// tolerance, ADR-0048). Deploys is the peer's recent audit history, newest
// first, for the merged deploy feed.
type Data struct {
	State   map[string]json.RawMessage
	Deploys []audit.Record
}

// State is the `peers` UI state payload (ADR-0048): the primary's own name plus
// one entry per configured peer, each carrying its reachability and last-known
// read data. The UI derives the host set (self + peers), per-host colours and
// the merged feed from it.
type State struct {
	// Self is the primary's own host label (config host_name), the identity key
	// its own rows are tagged and coloured by in the merged UI.
	Self string `json:"self"`
	// Peers is one view per configured peer, in config order.
	Peers []PeerView `json:"peers"`
}

// PeerView is one peer's entry in the merged state: its identity, reachability
// and last-known data. When a peer has never been reached, LastSeen is nil and
// State/Deploys are empty; when a poll fails after an earlier success, the
// last-known data is kept and Stale is set so the UI dims it rather than
// blanking the host.
type PeerView struct {
	Name      string                     `json:"name"`
	URL       string                     `json:"url"`
	Reachable bool                       `json:"reachable"`
	LastSeen  *time.Time                 `json:"last_seen,omitempty"`
	Stale     bool                       `json:"stale"`
	State     map[string]json.RawMessage `json:"state,omitempty"`
	Deploys   []audit.Record             `json:"deploys,omitempty"`
}

// HostMeta is one host's lean identity + reachability, without the bulky read
// data — the shape GET /api/peers returns for the effective host set.
type HostMeta struct {
	Name      string     `json:"name"`
	URL       string     `json:"url,omitempty"`
	Self      bool       `json:"self,omitempty"`
	Reachable bool       `json:"reachable"`
	LastSeen  *time.Time `json:"last_seen,omitempty"`
	Stale     bool       `json:"stale"`
}

// Registry fans in a fixed set of peers: it polls them, caches each one's
// last-known data + reachability, and builds the merged UI state. It is safe
// for concurrent use — Poll writes the cache while State/Hosts read it.
type Registry struct {
	self    string
	peers   []config.Peer
	client  Client
	timeout time.Duration

	mu    sync.RWMutex
	cache map[string]*cacheEntry
}

// cacheEntry is one peer's last poll outcome. hasData stays true once any poll
// has succeeded, so a later failure keeps the last-known data (marked stale)
// instead of blanking the host.
type cacheEntry struct {
	data      Data
	lastSeen  time.Time
	reachable bool
	hasData   bool
}

// New builds a Registry for the given peers. self is the primary's own host
// label; timeout bounds each per-peer fetch. Callers construct a Registry only
// when peers are configured.
func New(self string, peers []config.Peer, client Client, timeout time.Duration) *Registry {
	cache := make(map[string]*cacheEntry, len(peers))
	for _, p := range peers {
		cache[p.Name] = &cacheEntry{}
	}
	return &Registry{self: self, peers: peers, client: client, timeout: timeout, cache: cache}
}

// Self returns the primary's own host label.
func (r *Registry) Self() string { return r.self }

// Poll fetches every peer concurrently and updates the cache. A peer that
// errors or times out keeps its last-known data (marked stale via State); a
// peer that succeeds refreshes its data and lastSeen. Poll blocks until all
// peers have been tried (each bounded by the configured timeout).
func (r *Registry) Poll(ctx context.Context) {
	var wg sync.WaitGroup
	for _, p := range r.peers {
		wg.Add(1)
		go func(p config.Peer) {
			defer wg.Done()
			fctx, cancel := context.WithTimeout(ctx, r.timeout)
			defer cancel()
			data, err := r.client.Fetch(fctx, p.URL)

			r.mu.Lock()
			defer r.mu.Unlock()
			e := r.cache[p.Name]
			if err != nil {
				// Unreachable: keep the last-known data and lastSeen; State()
				// marks it stale. A never-yet-reached peer stays empty.
				e.reachable = false
				return
			}
			e.data = data
			e.reachable = true
			e.hasData = true
			e.lastSeen = time.Now()
		}(p)
	}
	wg.Wait()
}

// State builds the `peers` UI state payload from the current cache.
func (r *Registry) State() State {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s := State{Self: r.self, Peers: make([]PeerView, 0, len(r.peers))}
	for _, p := range r.peers {
		e := r.cache[p.Name]
		pv := PeerView{Name: p.Name, URL: p.URL, Reachable: e.reachable}
		if e.hasData {
			ls := e.lastSeen
			pv.LastSeen = &ls
			pv.Stale = !e.reachable // have data, but the last poll failed
			pv.State = e.data.State
			pv.Deploys = e.data.Deploys
		}
		s.Peers = append(s.Peers, pv)
	}
	return s
}

// Hosts returns the effective host set — the primary (self) first, then each
// peer — with reachability but without the bulky read data. It backs GET
// /api/peers.
func (r *Registry) Hosts() []HostMeta {
	r.mu.RLock()
	defer r.mu.RUnlock()
	hosts := make([]HostMeta, 0, len(r.peers)+1)
	hosts = append(hosts, HostMeta{Name: r.self, Self: true, Reachable: true})
	for _, p := range r.peers {
		e := r.cache[p.Name]
		h := HostMeta{Name: p.Name, URL: p.URL, Reachable: e.reachable}
		if e.hasData {
			ls := e.lastSeen
			h.LastSeen = &ls
			h.Stale = !e.reachable
		}
		hosts = append(hosts, h)
	}
	return hosts
}
