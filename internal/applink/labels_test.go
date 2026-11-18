package applink

import (
	"reflect"
	"testing"
)

func TestParsePS_SkipsBlankAndUnlabeledLines(t *testing.T) {
	out := []byte("\n" + "c1\t/repo/stacks/web\n" + "\t/repo/stacks/no-id\n" + "c2\t\n" + "c3\t/repo/stacks/media\n")
	got := parsePS(out)
	want := []containerRef{
		{ID: "c1", WorkingDir: "/repo/stacks/web"},
		{ID: "c3", WorkingDir: "/repo/stacks/media"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestParseLabelLines_HandlesNullAndUnparsableAndOrdersByLine(t *testing.T) {
	out := []byte(`{"traefik.enable":"true"}` + "\n" + "null" + "\n" + "not-json" + "\n")
	got := parseLabelLines(out)
	want := []map[string]string{
		{"traefik.enable": "true"},
		nil,
		nil,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestExtractHosts_SingleHostRule(t *testing.T) {
	got := extractHosts("Host(`app.example.com`)")
	want := []string{"app.example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestExtractHosts_MultipleHostsOred(t *testing.T) {
	got := extractHosts("Host(`a.example.com`) || Host(`b.example.com`)")
	want := []string{"a.example.com", "b.example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestExtractHosts_VariadicHostsInOneCall(t *testing.T) {
	got := extractHosts("Host(`a.example.com`,`b.example.com`)")
	want := []string{"a.example.com", "b.example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestExtractHosts_CombinedWithOtherMatchers(t *testing.T) {
	got := extractHosts("Host(`app.example.com`) && PathPrefix(`/api`)")
	want := []string{"app.example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestExtractHosts_IgnoresHostRegexp(t *testing.T) {
	got := extractHosts("HostRegexp(`^.+\\.example\\.com$`)")
	if len(got) != 0 {
		t.Fatalf("expected no hosts from HostRegexp, got %v", got)
	}
}

func TestExtractHosts_NoHostCallYieldsNil(t *testing.T) {
	got := extractHosts("PathPrefix(`/api`)")
	if len(got) != 0 {
		t.Fatalf("expected no hosts, got %v", got)
	}
}

func TestHostsByWorkingDir_RequiresEnableLabel(t *testing.T) {
	refs := []containerRef{{ID: "c1", WorkingDir: "/repo/stacks/web"}}
	labelMaps := []map[string]string{
		{"traefik.http.routers.web.rule": "Host(`app.example.com`)"}, // no traefik.enable
	}
	got := hostsByWorkingDir(refs, labelMaps)
	if len(got) != 0 {
		t.Fatalf("expected no hosts without traefik.enable=true, got %v", got)
	}
}

func TestHostsByWorkingDir_GroupsDedupesAndSortsAcrossContainers(t *testing.T) {
	refs := []containerRef{
		{ID: "c1", WorkingDir: "/repo/stacks/auth"},
		{ID: "c2", WorkingDir: "/repo/stacks/auth"},
	}
	labelMaps := []map[string]string{
		{
			"traefik.enable":                  "true",
			"traefik.http.routers.auth.rule":  "Host(`auth.example.com`)",
			"traefik.http.routers.auth2.rule": "Host(`sso.example.com`)",
		},
		{
			"traefik.enable":                "true",
			"traefik.http.routers.dup.rule": "Host(`auth.example.com`)", // duplicate across containers
		},
	}
	got := hostsByWorkingDir(refs, labelMaps)
	want := map[string][]string{
		"/repo/stacks/auth": {"auth.example.com", "sso.example.com"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestHostsByWorkingDir_ToleratesFewerLabelMapsThanRefs(t *testing.T) {
	refs := []containerRef{
		{ID: "c1", WorkingDir: "/repo/stacks/web"},
		{ID: "c2", WorkingDir: "/repo/stacks/media"},
	}
	labelMaps := []map[string]string{
		{"traefik.enable": "true", "traefik.http.routers.web.rule": "Host(`web.example.com`)"},
		// c2's label map is missing (vanished between ps and inspect).
	}
	got := hostsByWorkingDir(refs, labelMaps)
	want := map[string][]string{"/repo/stacks/web": {"web.example.com"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
