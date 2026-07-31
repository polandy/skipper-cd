package registry

import "testing"

func TestParseReference(t *testing.T) {
	tests := []struct {
		in   string
		want Reference
	}{
		{
			in:   "nextcloud:29.0.4",
			want: Reference{Host: "registry-1.docker.io", Repo: "library/nextcloud", Tag: "29.0.4"},
		},
		{
			in:   "vaultwarden/server:1.32.0",
			want: Reference{Host: "registry-1.docker.io", Repo: "vaultwarden/server", Tag: "1.32.0"},
		},
		{
			in:   "ghcr.io/immich-app/immich-server:v1.135.3",
			want: Reference{Host: "ghcr.io", Repo: "immich-app/immich-server", Tag: "v1.135.3"},
		},
		{
			// docker.io spelled out normalizes like the bare form.
			in:   "docker.io/library/redis:6.2-alpine",
			want: Reference{Host: "registry-1.docker.io", Repo: "library/redis", Tag: "6.2-alpine"},
		},
		{
			in:   "index.docker.io/library/redis:6.2",
			want: Reference{Host: "registry-1.docker.io", Repo: "library/redis", Tag: "6.2"},
		},
		{
			// No tag defaults to latest.
			in:   "traefik",
			want: Reference{Host: "registry-1.docker.io", Repo: "library/traefik", Tag: "latest"},
		},
		{
			// A digest-pinned reference keeps the digest and the tag.
			in: "traefik:v3.1@sha256:6529d4b5f8ec2a0e6f7a2f5e2b1f9d3c4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b",
			want: Reference{
				Host: "registry-1.docker.io", Repo: "library/traefik", Tag: "v3.1",
				Digest: "sha256:6529d4b5f8ec2a0e6f7a2f5e2b1f9d3c4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b",
			},
		},
		{
			// The short-image-id suffix running_images appends (ADR-0053) is not
			// a digest and is dropped.
			in:   "nextcloud:34-ghostscript@40c2d6f1d8f0",
			want: Reference{Host: "registry-1.docker.io", Repo: "library/nextcloud", Tag: "34-ghostscript"},
		},
		{
			// A host:port registry: the colon before the first slash is a port,
			// not a tag separator.
			in:   "registry.example.com:5000/team/app:1.2.3",
			want: Reference{Host: "registry.example.com:5000", Repo: "team/app", Tag: "1.2.3"},
		},
		{
			in:   "localhost:5000/app",
			want: Reference{Host: "localhost:5000", Repo: "app", Tag: "latest"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseReference(tt.in)
			if err != nil {
				t.Fatalf("ParseReference(%q): %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("ParseReference(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseReference_Empty(t *testing.T) {
	if _, err := ParseReference(""); err == nil {
		t.Fatal("ParseReference(\"\") should fail")
	}
}
