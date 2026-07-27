package git

import (
	"net/url"
	"strings"
)

// WebURL derives a forge's browse base URL from a Git clone URL — the prefix the
// UI appends "/commit/<sha>" to so a displayed commit SHA links to the commit
// page. Gitea and GitHub, the two forges skipper-cd speaks webhook to, share
// that path shape.
//
// The result is assembled from scheme, host and path only: userinfo, query and
// fragment are dropped, so a repo_url carrying a personal access token cannot
// leak into a page or a link the browser sends onward. SSH forms (scp-like,
// ssh://, git://) map to https, since a forge serves its web UI there — the SSH
// port is dropped with them, as it says nothing about the web port.
//
// Returns "" when no browse URL can be derived (local paths, file://,
// unparseable URLs, a URL with no repo path). Callers then render the SHA as
// plain text instead of guessing a link target.
func WebURL(cloneURL string) string {
	raw := strings.TrimSpace(cloneURL)
	if raw == "" {
		return ""
	}
	if host, path, ok := parseSCPLike(raw); ok {
		return buildWebURL("https", host, path)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	switch u.Scheme {
	case "http", "https":
		return buildWebURL(u.Scheme, u.Host, u.Path)
	case "ssh", "git":
		return buildWebURL("https", u.Hostname(), u.Path)
	default:
		return "" // file:// and local paths have no web UI
	}
}

// parseSCPLike splits git's scp-like syntax ("git@host:owner/repo.git") into
// host and path. It reports false for anything else, including a URL with a
// scheme and a local path that merely contains a colon.
func parseSCPLike(raw string) (host, path string, ok bool) {
	if strings.Contains(raw, "://") {
		return "", "", false
	}
	colon := strings.Index(raw, ":")
	if colon < 0 {
		return "", "", false
	}
	if slash := strings.Index(raw, "/"); slash >= 0 && slash < colon {
		return "", "", false // a path, not host:path
	}
	host, path = raw[:colon], raw[colon+1:]
	if at := strings.LastIndex(host, "@"); at >= 0 {
		host = host[at+1:] // drop the ssh user
	}
	if host == "" || path == "" {
		return "", "", false
	}
	return host, "/" + strings.TrimPrefix(path, "/"), true
}

// buildWebURL assembles the browse URL from the parts worth keeping, trimming
// the ".git" clone suffix and any trailing slash. path always arrives rooted —
// url.Parse yields a leading "/" and parseSCPLike prepends one. A path that
// trims away to nothing (say "/.git") yields "": there is nothing to browse.
func buildWebURL(scheme, host, path string) string {
	path = strings.TrimSuffix(strings.TrimRight(path, "/"), ".git")
	path = strings.TrimRight(path, "/")
	if host == "" || path == "" || path == "/" {
		return ""
	}
	return scheme + "://" + host + path
}
