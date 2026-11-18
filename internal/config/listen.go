package config

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// Who can reach the main (webhook/UI) server: the address it binds to and the
// Host names its UI answers (see ADR-0067). The enforcement lives in
// internal/ui (RequireAllowedHost) and cmd/skipper (the bind).

// localhostName is the one non-IP listen_address accepted: it is what an
// operator writes for "loopback only", and it never resolves off the host.
const localhostName = "localhost"

// ListenAddr returns the main server's listen address: listen_address joined
// with port, ":<port>" (every interface) when listen_address is empty.
func (c *Config) ListenAddr() string {
	return net.JoinHostPort(c.ListenAddress, strconv.Itoa(c.Port))
}

// validateListen checks listen_address and allowed_hosts.
func validateListen(listenAddress string, allowedHosts []string) error {
	if listenAddress != "" && !isBindableAddress(listenAddress) {
		if host, _, err := net.SplitHostPort(listenAddress); err == nil && isBindableAddress(host) {
			return fmt.Errorf("listen_address %q includes a port; give the address only (e.g. \"127.0.0.1\") — the port is set by port", listenAddress)
		}
		return fmt.Errorf("listen_address %q must be an IP address (e.g. \"127.0.0.1\" or \"::1\") or \"localhost\"; leave it empty to listen on every interface", listenAddress)
	}
	for i, h := range allowedHosts {
		if err := validateAllowedHost(h); err != nil {
			return fmt.Errorf("allowed_hosts[%d] %q: %w", i, h, err)
		}
	}
	return nil
}

func isBindableAddress(addr string) bool {
	return addr == localhostName || net.ParseIP(addr) != nil
}

// validateAllowedHost checks one allowed_hosts entry: a host name or IP,
// optionally with a :port, exactly as it appears in a browser's address bar.
func validateAllowedHost(h string) error {
	switch {
	case strings.TrimSpace(h) == "":
		return fmt.Errorf("is empty; give a host name such as \"skipper.example.com\"")
	case strings.ContainsAny(h, "/?#@"):
		return fmt.Errorf("give a host name (optionally with :port), not a URL — e.g. \"skipper.example.com\"")
	case strings.Contains(h, "*"):
		return fmt.Errorf("wildcards are not supported; list every name the UI is reached by")
	}
	if _, port, err := net.SplitHostPort(h); err == nil {
		if n, err := strconv.Atoi(port); err != nil || n < minPort || n > maxPort {
			return fmt.Errorf("port %q must be a number between %d and %d", port, minPort, maxPort)
		}
	}
	return nil
}
