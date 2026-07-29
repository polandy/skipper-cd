package config

import (
	"fmt"
	"net/url"
)

// The peers this instance federates read data in from (ADR-0048): their shape
// and validation. The fan-in itself lives in internal/peers.

// validateNotificationTarget checks a single notification target. Format and On
// have already been defaulted in Load.
// Peer is one other skipper instance this instance federates in (ADR-0048):
// the primary reads the peer's read data over HTTP and renders it, tagged by
// host, in one merged UI.
type Peer struct {
	// Name is the display label and identity key — it appears on the host
	// badge/filter and drives the peer's per-host colour. Must be unique.
	Name string `yaml:"name"`

	// URL is the peer's skipper base URL, reachable from this instance (its
	// LAN address, e.g. http://host-b:8001).
	URL string `yaml:"url"`
}

// validatePeers checks the peers list: each entry needs a unique name and a
// valid http(s) URL. Names must be unique because the name is the identity key
// the merged UI groups and colours by.
func validatePeers(peers []Peer) error {
	seen := make(map[string]bool, len(peers))
	for i, p := range peers {
		if p.Name == "" {
			return fmt.Errorf("peers[%d]: name is required (the host label shown in the UI)", i)
		}
		if seen[p.Name] {
			return fmt.Errorf("peers[%d]: duplicate peer name %q — peer names must be unique", i, p.Name)
		}
		seen[p.Name] = true
		if p.URL == "" {
			return fmt.Errorf("peers[%d] (%s): url is required (the peer's skipper base URL, e.g. http://host-b:8001)", i, p.Name)
		}
		if u, err := url.ParseRequestURI(p.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("peers[%d] (%s): url %q must be a valid http(s) URL", i, p.Name, p.URL)
		}
	}
	return nil
}
