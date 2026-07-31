//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// stubDockerScript is the fake `docker` placed first on PATH, shared verbatim
// with the Playwright harness (e2e/ui/fixtures/harness.ts) — one file so a
// change reaches both. Its UI-only branches are gated behind STUB_DOCKER_UI,
// which this harness does not set.
//
//go:embed fixtures/docker-stub.sh
var stubDockerScript string

const defaultCompose = "services:\n  app:\n    image: nginx:1.25\n"

// skipper is a running skipper binary under test, with its local git origin,
// stub docker, and derived paths.
type skipper struct {
	t          *testing.T
	base       string // temp root holding origin, state, config
	origin     string // local git origin repo (bare-less working tree)
	repoDir    string // where skipper clones the repo
	stateDir   string // where state.yaml is written (parent of repoDir)
	dockerLog  string // stub docker invocation log
	secret     string // webhook HMAC secret
	baseURL    string // http://127.0.0.1:<port> (webhook + UI)
	metricsURL string // http://127.0.0.1:<metrics_port>
	stacks     []string
	dependsOn  map[string][]string // per-stack depends_on edges (ADR-0032); optional
	stubEnv    map[string]string   // extra env for the stub docker (fail/hold scripting)
	extraCfg   string              // raw top-level YAML appended to skipper.yml

	proc   *exec.Cmd
	stderr *syncBuffer
}

// startSkipper builds a local origin containing one directory per stack (each
// with a default compose file), launches the skipper binary against it, waits
// until it is healthy, and waits for the startup deploy of every stack to
// settle. The instance is stopped via t.Cleanup.
func startSkipper(t *testing.T, stacks ...string) *skipper {
	return startSkipperEnv(t, nil, stacks...)
}

// startSkipperEnv is startSkipper with extra environment for the stub docker,
// used to script failing/held `up` invocations (see stubDockerScript).
func startSkipperEnv(t *testing.T, stubEnv map[string]string, stacks ...string) *skipper {
	return startSkipperOpts(t, stubEnv, "", stacks...)
}

// startSkipperOpts additionally appends extraCfg (raw top-level YAML, e.g. a
// health_watch block) to the generated skipper.yml.
func startSkipperOpts(t *testing.T, stubEnv map[string]string, extraCfg string, stacks ...string) *skipper {
	t.Helper()
	s := newSkipper(t, stubEnv, extraCfg, stacks)
	s.bootstrap(t)
	return s
}

// startSkipperOrdered starts skipper with per-stack depends_on edges (ADR-0032),
// so an e2e can exercise dependency ordering and the block-on-failure edge.
func startSkipperOrdered(t *testing.T, dependsOn map[string][]string, stubEnv map[string]string, stacks ...string) *skipper {
	t.Helper()
	s := newSkipper(t, stubEnv, "", stacks)
	s.dependsOn = dependsOn
	s.bootstrap(t)
	return s
}

// newSkipper builds the skipper fixture without launching it, so callers can set
// optional fields (e.g. dependsOn) before bootstrap.
func newSkipper(t *testing.T, stubEnv map[string]string, extraCfg string, stacks []string) *skipper {
	base := t.TempDir()
	return &skipper{
		t:         t,
		base:      base,
		origin:    filepath.Join(base, "origin"),
		repoDir:   filepath.Join(base, "state", "repo"),
		stateDir:  filepath.Join(base, "state"),
		dockerLog: filepath.Join(base, "docker.log"),
		secret:    "e2e-secret",
		stacks:    stacks,
		stubEnv:   stubEnv,
		extraCfg:  extraCfg,
		stderr:    &syncBuffer{},
	}
}

// bootstrap launches the built fixture and waits until every stack's startup
// deploy has settled.
func (s *skipper) bootstrap(t *testing.T) {
	t.Helper()
	requireGit(t)
	if err := os.MkdirAll(s.stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}

	s.initOrigin()
	stubDir := s.writeStubDocker()
	cfgPath := s.writeConfig(s.base)
	s.launch(cfgPath, stubDir)

	s.waitHealthy()
	for _, name := range s.stacks {
		s.waitFor(fmt.Sprintf("startup deploy of %q", name), func() bool {
			return s.stateHasStack(name)
		})
	}
}

// quoteAll returns each string double-quoted, for inlining into a YAML flow list.
func quoteAll(xs []string) []string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = fmt.Sprintf("%q", x)
	}
	return out
}

// initOrigin creates the origin repo with one committed directory per stack.
func (s *skipper) initOrigin() {
	s.t.Helper()
	git(s.t, "", "init", "-b", "main", s.origin)
	for _, name := range s.stacks {
		dir := filepath.Join(s.origin, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			s.t.Fatalf("mkdir origin stack %q: %v", name, err)
		}
		writeFile(s.t, filepath.Join(dir, "docker-compose.yml"), defaultCompose)
	}
	git(s.t, s.origin, "add", ".")
	git(s.t, s.origin, "commit", "-m", "initial")
}

// setStackImage rewrites a stack's compose file to use the given nginx tag and
// commits it to the origin, simulating a pushed change.
func (s *skipper) setStackImage(stack, tag string) {
	s.t.Helper()
	content := fmt.Sprintf("services:\n  app:\n    image: nginx:%s\n", tag)
	writeFile(s.t, filepath.Join(s.origin, stack, "docker-compose.yml"), content)
	git(s.t, s.origin, "commit", "-am", "bump "+stack+" to "+tag)
}

// writeStubDocker writes the stub docker script into its own dir and returns
// that dir (to be prepended to PATH).
func (s *skipper) writeStubDocker() string {
	s.t.Helper()
	dir := filepath.Join(s.t.TempDir(), "stub-bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		s.t.Fatalf("mkdir stub bin: %v", err)
	}
	path := filepath.Join(dir, "docker")
	if err := os.WriteFile(path, []byte(stubDockerScript), 0o755); err != nil {
		s.t.Fatalf("write stub docker: %v", err)
	}
	return dir
}

// writeConfig writes the skipper config and returns its path.
func (s *skipper) writeConfig(base string) string {
	s.t.Helper()
	ports := freePorts(s.t, 2)
	port, metricsPort := ports[0], ports[1]
	s.baseURL = fmt.Sprintf("http://127.0.0.1:%d", port)
	s.metricsURL = fmt.Sprintf("http://127.0.0.1:%d", metricsPort)

	var b strings.Builder
	fmt.Fprintf(&b, "repo_url: %q\n", s.origin)
	fmt.Fprintf(&b, "repo_dir: %q\n", s.repoDir)
	// stacks_base_dir omitted: it is relative to repo_dir and defaults to the
	// repo root, which is exactly repoDir here (stacks live at the clone root).
	fmt.Fprintf(&b, "branch: main\n")
	fmt.Fprintf(&b, "webhook_secret: %q\n", s.secret)
	fmt.Fprintf(&b, "port: %d\n", port)
	fmt.Fprintf(&b, "metrics_port: %d\n", metricsPort)
	fmt.Fprintf(&b, "ui_enabled: true\n")
	fmt.Fprintf(&b, "command_timeout_seconds: 30\n")
	// Update check (ADR-0054) off: it is on by default, and a test host must
	// not resolve its fake image references against real registries.
	fmt.Fprintf(&b, "update_check:\n  interval_seconds: 0\n")
	fmt.Fprintf(&b, "icons:\n  cache_dir: %q\n", filepath.Join(base, "icons"))
	fmt.Fprintf(&b, "stacks:\n")
	for _, name := range s.stacks {
		fmt.Fprintf(&b, "  - name: %q\n", name)
		if deps := s.dependsOn[name]; len(deps) > 0 {
			fmt.Fprintf(&b, "    depends_on: [%s]\n", strings.Join(quoteAll(deps), ", "))
		}
	}
	if s.extraCfg != "" {
		b.WriteString(s.extraCfg)
	}

	cfgPath := filepath.Join(base, "skipper.yml")
	writeFile(s.t, cfgPath, b.String())
	return cfgPath
}

// launch starts the skipper binary with the stub dir prepended to PATH and
// DOCKER_LOG pointing at the stub's log, and registers cleanup.
func (s *skipper) launch(cfgPath, stubDir string) {
	s.t.Helper()
	cmd := exec.Command(skipperBin, "-config", cfgPath)
	env := map[string]string{
		"PATH":       stubDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"DOCKER_LOG": s.dockerLog,
	}
	for k, v := range s.stubEnv {
		env[k] = v
	}
	cmd.Env = withEnv(os.Environ(), env)
	cmd.Stdout = s.stderr
	cmd.Stderr = s.stderr
	if err := cmd.Start(); err != nil {
		s.t.Fatalf("start skipper: %v", err)
	}
	s.proc = cmd
	s.t.Cleanup(s.stop)
}

// stop terminates the skipper process gracefully, dumping its output on failure.
func (s *skipper) stop() {
	if s.proc == nil || s.proc.Process == nil {
		return
	}
	_ = s.proc.Process.Signal(os.Interrupt)
	done := make(chan struct{})
	go func() { _, _ = s.proc.Process.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = s.proc.Process.Kill()
	}
	if s.t.Failed() {
		s.t.Logf("skipper output:\n%s", s.stderr.String())
	}
}

// --- assertions / polling -------------------------------------------------

// waitHealthy blocks until GET /healthz returns 200.
func (s *skipper) waitHealthy() {
	s.t.Helper()
	s.waitFor("healthz 200", func() bool {
		resp, err := http.Get(s.baseURL + "/healthz")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})
}

// sendWebhook posts a correctly signed push payload for ref and returns the
// HTTP status code.
func (s *skipper) sendWebhook(ref string) int {
	s.t.Helper()
	body := []byte(fmt.Sprintf(`{"ref":%q}`, ref))
	req, err := http.NewRequest(http.MethodPost, s.baseURL+"/webhook", bytes.NewReader(body))
	if err != nil {
		s.t.Fatalf("build webhook request: %v", err)
	}
	req.Header.Set("X-Gitea-Signature", sign(body, s.secret))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatalf("send webhook: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// sendWebhookRaw posts a push payload for ref with an explicit signature header
// (used to exercise the invalid-signature path) and returns the status code.
func (s *skipper) sendWebhookRaw(ref, signature string) int {
	s.t.Helper()
	body := []byte(fmt.Sprintf(`{"ref":%q}`, ref))
	req, err := http.NewRequest(http.MethodPost, s.baseURL+"/webhook", bytes.NewReader(body))
	if err != nil {
		s.t.Fatalf("build webhook request: %v", err)
	}
	req.Header.Set("X-Gitea-Signature", signature)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatalf("send webhook: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// healthStatus returns the current /healthz status code.
func (s *skipper) healthStatus() int {
	s.t.Helper()
	resp, err := http.Get(s.baseURL + "/healthz")
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// metricsBody returns the raw /metrics scrape from the metrics server.
func (s *skipper) metricsBody() string {
	s.t.Helper()
	resp, err := http.Get(s.metricsURL + "/metrics")
	if err != nil {
		s.t.Fatalf("scrape metrics: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return string(data)
}

// postAutosync sets a global or per-stack autosync override and returns the
// status code. Pass stack == "" for the global scope.
func (s *skipper) postAutosync(stack string, enabled bool) int {
	s.t.Helper()
	scope := "global"
	if stack != "" {
		scope = "stack"
	}
	body, _ := json.Marshal(map[string]any{"scope": scope, "stack": stack, "enabled": enabled})
	resp, err := http.Post(s.baseURL+"/api/autosync", "application/json", bytes.NewReader(body))
	if err != nil {
		s.t.Fatalf("post autosync: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// queueBody returns the raw GET /api/queue JSON.
func (s *skipper) queueBody() string {
	s.t.Helper()
	resp, err := http.Get(s.baseURL + "/api/queue")
	if err != nil {
		s.t.Fatalf("get queue: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return string(data)
}

// breakOrigin removes the origin repo so the next sync fails, driving the
// service unhealthy.
func (s *skipper) breakOrigin() {
	s.t.Helper()
	if err := os.RemoveAll(s.origin); err != nil {
		s.t.Fatalf("remove origin: %v", err)
	}
}

// dockerUps returns how many `compose … up` invocations the stub recorded for
// the given stack (attributed by the invocation's working directory).
func (s *skipper) dockerUps(stack string) int {
	n := 0
	for _, line := range s.dockerLogLines() {
		cwd, args, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		if filepath.Base(cwd) == stack && strings.Contains(" "+args+" ", " up ") {
			n++
		}
	}
	return n
}

func (s *skipper) dockerLogLines() []string {
	data, err := os.ReadFile(s.dockerLog)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

// stateHasStack reports whether state.yaml records the given stack.
func (s *skipper) stateHasStack(stack string) bool {
	data, err := os.ReadFile(filepath.Join(s.stateDir, "state.yaml"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), stack+":")
}

// waitFor polls cond until it returns true, failing the test after a timeout.
func (s *skipper) waitFor(what string, cond func() bool) {
	s.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	s.t.Fatalf("timed out waiting for %s", what)
}

// --- small helpers --------------------------------------------------------

// metricValue parses the value of a single Prometheus series line (e.g.
// `skipper_webhooks_received_total` or `skipper_deploys_triggered_total{stack="web"}`)
// from a scrape body. ok is false when the series is absent.
func metricValue(body, series string) (value float64, ok bool) {
	for _, line := range strings.Split(body, "\n") {
		rest, found := strings.CutPrefix(line, series+" ")
		if !found {
			continue
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(rest), 64)
		return v, err == nil
	}
	return 0, false
}

func sign(body []byte, secret string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}

// withEnv returns base with the given keys set, replacing any existing entries.
func withEnv(base []string, kv map[string]string) []string {
	out := make([]string, 0, len(base)+len(kv))
	for _, e := range base {
		k, _, _ := strings.Cut(e, "=")
		if _, override := kv[k]; override {
			continue
		}
		out = append(out, e)
	}
	for k, v := range kv {
		out = append(out, k+"="+v)
	}
	return out
}

// freePorts reserves n distinct free TCP ports. All listeners are held open
// while the ports are collected so the OS cannot hand out the same port twice,
// then closed for the process under test to bind.
func freePorts(t *testing.T, n int) []int {
	t.Helper()
	listeners := make([]net.Listener, 0, n)
	ports := make([]int, 0, n)
	for range n {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("pick free port: %v", err)
		}
		listeners = append(listeners, l)
		ports = append(ports, l.Addr().(*net.TCPAddr).Port)
	}
	for _, l := range listeners {
		_ = l.Close()
	}
	return ports
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not available: %v", err)
	}
}

// git runs a git command with a fixed test identity, failing on error. When
// dir is empty the command runs without -C (for `git init <path>`).
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := []string{"-c", "user.name=e2e", "-c", "user.email=e2e@example.com"}
	if dir != "" {
		full = append([]string{"-C", dir}, full...)
	}
	full = append(full, args...)
	if out, err := exec.CommandContext(context.Background(), "git", full...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// syncBuffer is a goroutine-safe buffer for capturing child process output
// (exec writes stdout and stderr concurrently).
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
