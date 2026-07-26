package health

import "testing"

func TestParsePS_NDJSON(t *testing.T) {
	out := []byte(`{"Service":"app","State":"running","Health":"healthy"}
{"Service":"db","State":"running","Health":""}`)
	lines, err := parsePS(out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if lines[0].Service != "app" || lines[1].Service != "db" {
		t.Errorf("unexpected services: %+v", lines)
	}
}

func TestParsePS_JSONArray(t *testing.T) {
	out := []byte(`[{"Service":"app","State":"running","Health":"healthy"},{"Service":"db","State":"exited","ExitCode":0}]`)
	lines, err := parsePS(out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
}

func TestParsePS_EmptyIsNoLines(t *testing.T) {
	for _, in := range [][]byte{nil, []byte(""), []byte("   \n ")} {
		lines, err := parsePS(in)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", in, err)
		}
		if len(lines) != 0 {
			t.Errorf("expected 0 lines for %q, got %d", in, len(lines))
		}
	}
}

func TestParsePS_MalformedIsError(t *testing.T) {
	if _, err := parsePS([]byte(`{not json`)); err == nil {
		t.Fatal("expected an error for malformed output")
	}
}

func TestParsePS_CarriesImage(t *testing.T) {
	out := []byte(`{"Service":"app","Image":"nextcloud:30.0.2","State":"running","Health":"healthy"}`)
	lines, err := parsePS(out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	if lines[0].Image != "nextcloud:30.0.2" {
		t.Errorf("image = %q, want nextcloud:30.0.2", lines[0].Image)
	}
}

func TestParsePS_MissingImageIsEmpty(t *testing.T) {
	// An older compose (or a `ps` output without the field) must degrade to an
	// empty image, never a parse error — the UI then simply shows no version.
	lines, err := parsePS([]byte(`{"Service":"app","State":"running"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 1 || lines[0].Image != "" {
		t.Errorf("expected one line with an empty image, got %+v", lines)
	}
}

func TestRollup(t *testing.T) {
	tests := []struct {
		name  string
		lines []psLine
		want  Status
	}{
		{
			name:  "healthy when all running and healthy",
			lines: []psLine{{Service: "a", State: "running", Health: "healthy"}, {Service: "b", State: "running", Health: "healthy"}},
			want:  Healthy,
		},
		{
			name:  "healthy when running without a healthcheck",
			lines: []psLine{{Service: "a", State: "running", Health: ""}},
			want:  Healthy,
		},
		{
			name:  "unhealthy when any service unhealthy",
			lines: []psLine{{Service: "a", State: "running", Health: "healthy"}, {Service: "b", State: "running", Health: "unhealthy"}},
			want:  Unhealthy,
		},
		{
			name:  "unhealthy when a service is restarting",
			lines: []psLine{{Service: "a", State: "restarting", Health: ""}},
			want:  Unhealthy,
		},
		{
			name:  "unhealthy when a service exited non-zero",
			lines: []psLine{{Service: "a", State: "exited", ExitCode: 1}},
			want:  Unhealthy,
		},
		{
			// An on-demand container (Sablier-style) is stopped by skipper after
			// the deploy — often via SIGKILL (137). That is its intended idle
			// state, never a failure.
			name:  "stopped when an on-demand container exited non-zero",
			lines: []psLine{{Service: "app", Name: "monica-app", State: "exited", ExitCode: 137, onDemand: true}},
			want:  Stopped,
		},
		{
			// Only the exited state is forgiven: a crash-looping on-demand
			// container is still a real failure.
			name:  "unhealthy when an on-demand container is restarting",
			lines: []psLine{{Service: "app", Name: "monica-app", State: "restarting", onDemand: true}},
			want:  Unhealthy,
		},
		{
			name:  "starting when any service still starting and none unhealthy",
			lines: []psLine{{Service: "a", State: "running", Health: "healthy"}, {Service: "b", State: "running", Health: "starting"}},
			want:  Starting,
		},
		{
			name:  "unhealthy dominates starting",
			lines: []psLine{{Service: "a", State: "running", Health: "starting"}, {Service: "b", State: "running", Health: "unhealthy"}},
			want:  Unhealthy,
		},
		{
			name:  "stopped when there are no containers",
			lines: nil,
			want:  Stopped,
		},
		{
			name:  "stopped when all exited cleanly",
			lines: []psLine{{Service: "a", State: "exited", ExitCode: 0}},
			want:  Stopped,
		},
		{
			name:  "health field wins over state",
			lines: []psLine{{Service: "a", State: "running", Health: "unhealthy"}},
			want:  Unhealthy,
		},
		{
			name:  "running service alongside a cleanly-exited one-shot is healthy",
			lines: []psLine{{Service: "app", State: "running", Health: "healthy"}, {Service: "migrate", State: "exited", ExitCode: 0}},
			want:  Healthy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rollup(tt.lines); got != tt.want {
				t.Errorf("rollup = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestServicesOf(t *testing.T) {
	svcs := servicesOf([]psLine{{Service: "app", Image: "nextcloud:30.0.2", State: "running", Health: "healthy"}})
	if len(svcs) != 1 {
		t.Fatalf("expected 1 service, got %d", len(svcs))
	}
	if svcs[0] != (ServiceHealth{Name: "app", Image: "nextcloud:30.0.2", State: "running", Health: "healthy", Status: Healthy}) {
		t.Errorf("unexpected service: %+v", svcs[0])
	}
	if servicesOf(nil) != nil {
		t.Error("expected nil services for no lines")
	}
}
