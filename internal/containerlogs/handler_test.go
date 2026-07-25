package containerlogs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeStreamer records the invocation it was handed and replays canned lines.
type fakeStreamer struct {
	dir   string
	env   []string
	name  string
	args  []string
	lines []string
}

func (f *fakeStreamer) Stream(ctx context.Context, dir string, env []string, name string, args []string, onLine func(string)) error {
	f.dir, f.env, f.name, f.args = dir, env, name, args
	for _, l := range f.lines {
		onLine(l)
	}
	return nil
}

type fakeResolver struct {
	inv      Invocation
	services []string
	known    bool
	err      error
}

func (f fakeResolver) Resolve(string) (Invocation, []string, bool, error) {
	return f.inv, f.services, f.known, f.err
}

// serve routes a request through a mux so the {stack} path value resolves;
// service selection rides the ?services= query.
func serve(streamer LogStreamer, resolver Resolver, target string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.Handle("GET /api/container-logs/{stack}", Handler(streamer, resolver))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

var projectArgs = []string{"compose", "-f", "/repo/web/docker-compose.yml", "--project-directory", "/srv/web"}

func okResolver(services ...string) fakeResolver {
	return fakeResolver{
		inv:      Invocation{Dir: "/srv/web", Env: []string{"A=1"}, Args: projectArgs},
		services: services,
		known:    true,
	}
}

func TestHandler_SingleServiceArgv_DropsPrefix(t *testing.T) {
	fs := &fakeStreamer{}
	rec := serve(fs, okResolver("api", "db"), "/api/container-logs/web?services=api&tail=200")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if fs.name != "docker" {
		t.Errorf("binary = %q, want docker", fs.name)
	}
	if fs.dir != "/srv/web" {
		t.Errorf("dir = %q, want /srv/web", fs.dir)
	}
	want := []string{
		"compose", "-f", "/repo/web/docker-compose.yml", "--project-directory", "/srv/web",
		"logs", "--no-color", "--timestamps", "--follow", "--no-log-prefix", "--tail", "200", "api",
	}
	if got := strings.Join(fs.args, " "); got != strings.Join(want, " ") {
		t.Errorf("args =\n  %v\nwant\n  %v", fs.args, want)
	}
}

func TestHandler_MultiServiceArgv_KeepsPrefix(t *testing.T) {
	fs := &fakeStreamer{}
	// comma-separated ?services= subset → service labels retained (no --no-log-prefix)
	serve(fs, okResolver("api", "db", "web"), "/api/container-logs/web?services=api,db&tail=200")

	want := []string{
		"compose", "-f", "/repo/web/docker-compose.yml", "--project-directory", "/srv/web",
		"logs", "--no-color", "--timestamps", "--follow", "--tail", "200", "api", "db",
	}
	if got := strings.Join(fs.args, " "); got != strings.Join(want, " ") {
		t.Errorf("args =\n  %v\nwant\n  %v", fs.args, want)
	}
}

func TestHandler_MergedStackArgv_KeepsServicePrefix(t *testing.T) {
	fs := &fakeStreamer{}
	// no ?services= → whole stack, service labels retained (no --no-log-prefix, no trailing service)
	serve(fs, okResolver("api", "db"), "/api/container-logs/web?tail=200")

	want := []string{
		"compose", "-f", "/repo/web/docker-compose.yml", "--project-directory", "/srv/web",
		"logs", "--no-color", "--timestamps", "--follow", "--tail", "200",
	}
	if got := strings.Join(fs.args, " "); got != strings.Join(want, " ") {
		t.Errorf("args =\n  %v\nwant\n  %v", fs.args, want)
	}
}

func TestHandler_TailClamp(t *testing.T) {
	cases := map[string]string{
		"/api/container-logs/web?tail=50":    "50",
		"/api/container-logs/web?tail=99999": "1000", // above cap
		"/api/container-logs/web?tail=0":     "1",    // below min
		"/api/container-logs/web?tail=-5":    "1",
		"/api/container-logs/web?tail=abc":   "200", // unparseable → default
		"/api/container-logs/web":            "200", // absent → default
	}
	for target, wantTail := range cases {
		fs := &fakeStreamer{}
		serve(fs, okResolver(), target)
		if !argHasPair(fs.args, "--tail", wantTail) {
			t.Errorf("%s: args %v, want --tail %s", target, fs.args, wantTail)
		}
	}
}

func TestHandler_SinceReplacesTail(t *testing.T) {
	fs := &fakeStreamer{}
	serve(fs, okResolver(), "/api/container-logs/web?since=2026-07-19T14:00:00Z&tail=200")

	if argHas(fs.args, "--tail") {
		t.Errorf("since request should not pass --tail: %v", fs.args)
	}
	if !argHasPair(fs.args, "--since", "2026-07-19T14:00:00Z") {
		t.Errorf("args %v, want --since 2026-07-19T14:00:00Z", fs.args)
	}
}

func TestHandler_InvalidSinceFallsBackToTail(t *testing.T) {
	fs := &fakeStreamer{}
	serve(fs, okResolver(), "/api/container-logs/web?since=not-a-time")

	if argHas(fs.args, "--since") {
		t.Errorf("invalid since should be dropped: %v", fs.args)
	}
	if !argHasPair(fs.args, "--tail", "200") {
		t.Errorf("args %v, want --tail 200 fallback", fs.args)
	}
}

func TestHandler_UnknownStack404(t *testing.T) {
	fs := &fakeStreamer{}
	rec := serve(fs, fakeResolver{known: false}, "/api/container-logs/nope")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if fs.args != nil {
		t.Errorf("streamer must not run for unknown stack: %v", fs.args)
	}
}

func TestHandler_UnknownService404(t *testing.T) {
	fs := &fakeStreamer{}
	rec := serve(fs, okResolver("api", "db"), "/api/container-logs/web?services=ghost")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if fs.args != nil {
		t.Errorf("streamer must not run for unknown service: %v", fs.args)
	}
}

func TestHandler_UnknownServiceInSubset404(t *testing.T) {
	fs := &fakeStreamer{}
	// one valid, one unknown in the comma list → the whole request is rejected, nothing streams
	rec := serve(fs, okResolver("api", "db"), "/api/container-logs/web?services=api,ghost")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if fs.args != nil {
		t.Errorf("streamer must not run when any selected service is unknown: %v", fs.args)
	}
}

// A bare ?services= or stray empty tokens (e.g. a trailing comma) are not
// service names: empties are dropped, so the request degrades to the surviving
// subset (or the whole stack) instead of validating "" to a false 404.
func TestHandler_EmptyServiceTokensDropped(t *testing.T) {
	// "?services=" alone → whole stack (no trailing service arg).
	fs := &fakeStreamer{}
	rec := serve(fs, okResolver("api", "db"), "/api/container-logs/web?services=")
	if rec.Code != http.StatusOK {
		t.Fatalf("empty services= status = %d, want 200", rec.Code)
	}
	if argHas(fs.args, "api") || argHas(fs.args, "db") {
		t.Errorf("empty services= must stream the whole stack, got trailing service arg: %v", fs.args)
	}

	// "api,," → the empties drop out, leaving the single valid service.
	fs = &fakeStreamer{}
	rec = serve(fs, okResolver("api", "db"), "/api/container-logs/web?services=api,,")
	if rec.Code != http.StatusOK {
		t.Fatalf("trailing-comma services status = %d, want 200", rec.Code)
	}
	want := []string{
		"compose", "-f", "/repo/web/docker-compose.yml", "--project-directory", "/srv/web",
		"logs", "--no-color", "--timestamps", "--follow", "--no-log-prefix", "--tail", "200", "api",
	}
	if got := strings.Join(fs.args, " "); got != strings.Join(want, " ") {
		t.Errorf("args =\n  %v\nwant\n  %v", fs.args, want)
	}
}

func TestHandler_ResolverError500(t *testing.T) {
	fs := &fakeStreamer{}
	rec := serve(fs, fakeResolver{err: context.DeadlineExceeded}, "/api/container-logs/web")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestHandler_StreamsLinesAsSSE(t *testing.T) {
	fs := &fakeStreamer{lines: []string{"2026-07-19T14:00:00Z hello", "world"}}
	rec := serve(fs, okResolver("api"), "/api/container-logs/web?services=api")

	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("content-type = %q, want text/event-stream", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "data: 2026-07-19T14:00:00Z hello\n\n") {
		t.Errorf("body missing first line frame:\n%s", body)
	}
	if !strings.Contains(body, "data: world\n\n") {
		t.Errorf("body missing second line frame:\n%s", body)
	}
}

func argHas(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func argHasPair(args []string, flag, val string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == val {
			return true
		}
	}
	return false
}
