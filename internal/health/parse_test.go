package health

import (
	"reflect"
	"testing"
)

func TestParseList(t *testing.T) {
	ids, images := parseList([]byte("abc\tnginx:1.27\n\ndef\tsha256:0123\r\nghi\n"))

	if want := []string{"abc", "def", "ghi"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("ids = %v, want %v", ids, want)
	}
	if images["abc"] != "nginx:1.27" || images["def"] != "sha256:0123" || images["ghi"] != "" {
		t.Errorf("images = %v", images)
	}
}

func TestParseInspect_ReadsComposeLabelsAndState(t *testing.T) {
	out := "abc\t/srv/app/\tweb\t/app-web-1\trunning\thealthy\t0\n"

	got, err := parseInspect([]byte(out), map[string]string{"abc": "nginx:1.27"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string][]psLine{"/srv/app": {{Service: "web", Name: "app-web-1", Image: "nginx:1.27", State: "running", Health: "healthy"}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

// compose ps reports Health only for a running container and ExitCode only for
// an exited or dead one; inspect carries both always (a stale health status on
// a stopped container, an old exit code on a restarted one).
func TestParseInspect_GatesHealthAndExitCodeLikeComposePS(t *testing.T) {
	tests := []struct {
		line       string
		wantHealth string
		wantExit   int
	}{
		{"x\t/d\ts\t/n\trunning\tunhealthy\t3", "unhealthy", 0},
		{"x\t/d\ts\t/n\texited\tunhealthy\t3", "", 3},
		{"x\t/d\ts\t/n\tdead\t\t137", "", 137},
		{"x\t/d\ts\t/n\trestarting\tstarting\t1", "", 0},
	}
	for _, tt := range tests {
		got, err := parseInspect([]byte(tt.line), nil)
		if err != nil {
			t.Fatalf("%q: unexpected error: %v", tt.line, err)
		}
		l := got["/d"][0]
		if l.Health != tt.wantHealth || l.ExitCode != tt.wantExit {
			t.Errorf("%q: health=%q exit=%d, want health=%q exit=%d", tt.line, l.Health, l.ExitCode, tt.wantHealth, tt.wantExit)
		}
	}
}

func TestParseInspect_SortsEachProjectByContainerName(t *testing.T) {
	out := "2\t/d\tb\t/p-b-1\trunning\t\t0\n1\t/d\ta\t/p-a-1\trunning\t\t0\n"

	got, err := parseInspect([]byte(out), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lines := got["/d"]; len(lines) != 2 || lines[0].Name != "p-a-1" || lines[1].Name != "p-b-1" {
		t.Errorf("got %+v, want p-a-1 before p-b-1", lines)
	}
}

func TestParseInspect_EmptyIsNoProjects(t *testing.T) {
	got, err := parseInspect([]byte("  \n"), nil)
	if err != nil || len(got) != 0 {
		t.Errorf("got %v, %v; want no projects and no error", got, err)
	}
}

func TestParseInspect_MalformedIsError(t *testing.T) {
	for _, in := range []string{"too\tfew\tcolumns", "x\t/d\ts\t/n\texited\t\tNaN"} {
		if _, err := parseInspect([]byte(in), nil); err == nil {
			t.Errorf("%q: expected an error", in)
		}
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
