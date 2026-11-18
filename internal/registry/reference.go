// Package registry is a minimal read-only client for the Registry HTTP API v2:
// list a repository's tags and resolve a tag's manifest digest. That is all the
// update check needs (ADR-0054), and deliberately all this package offers — it
// never pulls, pushes, or resolves manifests beyond their digest header.
package registry

import (
	"fmt"
	"strings"
)

// dockerHubAPIHost is where the docker.io registry actually answers the v2 API.
const dockerHubAPIHost = "registry-1.docker.io"

// digestPrefix marks a value after "@" as a content digest, as opposed to the
// short image id running_images appends to floating tags (ADR-0053).
const digestPrefix = "sha256:"

// Reference is a parsed image reference, normalized for API access: Host is
// the registry endpoint to talk to, Repo the full repository path there
// ("library/…" for official Docker Hub images), Tag defaults to "latest".
// Digest is set only when the reference itself carried one ("@sha256:…") — a
// short-image-id suffix (12 hex chars, ADR-0053's floating-tag form) is not a
// digest and is dropped.
type Reference struct {
	Host   string
	Repo   string
	Tag    string
	Digest string
}

// String renders the reference in its familiar repo:tag form, without host
// normalization or digest — the form log lines and messages name an image by.
func (r Reference) String() string { return r.Repo + ":" + r.Tag }

// ParseReference parses an image reference the way docker does: the part
// before the first "/" is a registry host only when it looks like one
// (contains "." or ":", or is "localhost"); otherwise the host is Docker Hub
// and a bare repo gets the "library/" prefix.
func ParseReference(s string) (Reference, error) {
	if s == "" {
		return Reference{}, fmt.Errorf("empty image reference")
	}
	ref := Reference{}

	if name, after, ok := strings.Cut(s, "@"); ok {
		if strings.HasPrefix(after, digestPrefix) {
			ref.Digest = after
		}
		s = name
	}

	host := "docker.io"
	rest := s
	if first, remainder, ok := strings.Cut(s, "/"); ok &&
		(strings.ContainsAny(first, ".:") || first == "localhost") {
		host = first
		rest = remainder
	}

	// A tag colon can only appear after the last "/".
	ref.Tag = "latest"
	if i := strings.LastIndex(rest, ":"); i >= 0 && !strings.Contains(rest[i:], "/") {
		ref.Tag = rest[i+1:]
		rest = rest[:i]
	}
	if rest == "" {
		return Reference{}, fmt.Errorf("image reference %q has no repository", s)
	}

	switch host {
	case "docker.io", "index.docker.io":
		host = dockerHubAPIHost
		if !strings.Contains(rest, "/") {
			rest = "library/" + rest
		}
	}
	ref.Host = host
	ref.Repo = rest
	return ref, nil
}
