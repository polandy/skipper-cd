package ui

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/polandy/skipper-cd/internal/autosync"
)

func boolPtr(b bool) *bool { return &b }

func orderOf(names ...string) func() []string {
	return func() []string { return names }
}

func TestAutosyncHandler_GetReturnsSnapshot(t *testing.T) {
	ctrl := autosync.NewController(boolPtr(true), map[string]*bool{"gitea": boolPtr(false)})
	h := AutosyncHandler(ctrl, orderOf("gitea"), nil, nil)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/autosync", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var snap autosync.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !snap.Global || len(snap.Stacks) != 1 || snap.Stacks[0].Effective {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
}

// The UI orders the POST response against the SSE broadcast by the snapshot's
// `version`, so the field has to be on the wire under that name and be higher in
// the response to a change than in the state that preceded it.
func TestAutosyncHandler_SnapshotCarriesVersionOnTheWire(t *testing.T) {
	ctrl := autosync.NewController(boolPtr(true), nil)
	h := AutosyncHandler(ctrl, orderOf("gitea"), func() {}, func() {})

	versionOf := func(body []byte) float64 {
		var raw map[string]any
		if err := json.Unmarshal(body, &raw); err != nil {
			t.Fatalf("decode: %v", err)
		}
		v, ok := raw["version"].(float64)
		if !ok {
			t.Fatalf("no numeric \"version\" in %s", body)
		}
		return v
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/autosync", nil))
	before := versionOf(rec.Body.Bytes())

	after := versionOf(post(t, h, `{"scope":"global","enabled":false}`))
	if after <= before {
		t.Fatalf("version = %v after the change, want > %v", after, before)
	}
}

func TestAutosyncHandler_PostStackTogglesAndTriggersOnEnable(t *testing.T) {
	ctrl := autosync.NewController(boolPtr(true), nil)
	var changed, triggered int
	h := AutosyncHandler(ctrl, orderOf("gitea"),
		func() { changed++ },
		func() { triggered++ },
	)

	// Pause gitea: change published, no deploy triggered.
	post(t, h, `{"scope":"stack","stack":"gitea","enabled":false}`)
	if ctrl.Effective("gitea") {
		t.Error("gitea should be paused after enabled=false")
	}
	if changed != 1 || triggered != 0 {
		t.Fatalf("after disable: changed=%d triggered=%d, want 1/0", changed, triggered)
	}

	// Resume gitea: change published and a deploy triggered to drain.
	post(t, h, `{"scope":"stack","stack":"gitea","enabled":true}`)
	if !ctrl.Effective("gitea") {
		t.Error("gitea should sync after enabled=true")
	}
	if changed != 2 || triggered != 1 {
		t.Fatalf("after enable: changed=%d triggered=%d, want 2/1", changed, triggered)
	}
}

func TestAutosyncHandler_PostGlobal(t *testing.T) {
	ctrl := autosync.NewController(boolPtr(true), nil)
	h := AutosyncHandler(ctrl, orderOf("gitea"), func() {}, func() {})

	post(t, h, `{"scope":"global","enabled":false}`)
	if ctrl.GlobalEffective() {
		t.Error("global should be off after enabled=false")
	}
}

func TestAutosyncHandler_PostRejectsBadInput(t *testing.T) {
	ctrl := autosync.NewController(nil, nil)
	h := AutosyncHandler(ctrl, orderOf(), func() {}, func() {})

	for _, body := range []string{
		`{"scope":"bogus","enabled":true}`,
		`{"scope":"stack","enabled":true}`, // missing stack
		`not json`,
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/autosync", strings.NewReader(body)))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %q: status = %d, want 400", body, rec.Code)
		}
	}
}

func TestQueueHandler_ReturnsOrderedPending(t *testing.T) {
	q := autosync.NewQueue()
	q.Mark("gitea", []string{"docker-compose.yml"}, "stack")
	q.Mark("_nixos", []string{"flake.lock"}, "global")

	h := QueueHandler(q, orderOf("_nixos", "gitea"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/queue", nil))

	var view autosync.QueueView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.Count != 2 {
		t.Fatalf("count = %d, want 2", view.Count)
	}
	if view.Pending[0].Stack != "_nixos" || view.Pending[0].Position != 1 {
		t.Errorf("first item = %+v, want _nixos at position 1", view.Pending[0])
	}
	if view.Pending[1].Stack != "gitea" || view.Pending[1].Position != 2 {
		t.Errorf("second item = %+v, want gitea at position 2", view.Pending[1])
	}
}

// post issues a POST and returns the response body — the new snapshot.
func post(t *testing.T, h http.Handler, body string) []byte {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/autosync", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST %s: status = %d, want 200 (body: %s)", body, rec.Code, rec.Body.String())
	}
	return rec.Body.Bytes()
}

// captureLog runs fn with the default logger redirected, returning what it wrote.
func captureLog(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	fn()
	return buf.String()
}

// An autosync toggle decides whether a stack deploys at all, and it used to
// leave no trace: neither "who paused this" nor, when chasing the UC11 flake,
// "did the write even arrive" could be answered from the log.
func TestAutosyncHandler_PostLogsTheChange(t *testing.T) {
	tests := []struct {
		name          string
		body          string
		wantSubstrs   []string
		wantEffective string
		globalOff     bool
	}{
		{
			name:          "pausing one stack",
			body:          `{"scope":"stack","stack":"web","enabled":false}`,
			wantSubstrs:   []string{"autosync set", "scope=stack", "stack=web", "enabled=false"},
			wantEffective: "effective=false",
		},
		{
			name:          "the global switch",
			body:          `{"scope":"global","enabled":false}`,
			wantSubstrs:   []string{"autosync set", "scope=global", "enabled=false"},
			wantEffective: "effective=false",
		},
		{
			// A per-stack override wins over the global switch in both
			// directions (ADR-0019), so enabling one stack while global is off
			// really does resume it — the logged effective value is read back
			// from the controller rather than echoed from the request, which is
			// what makes it a record of what happened.
			name:          "enabling a stack while global is off",
			body:          `{"scope":"stack","stack":"web","enabled":true}`,
			wantSubstrs:   []string{"autosync set", "stack=web", "enabled=true"},
			wantEffective: "effective=true",
			globalOff:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := autosync.NewController(boolPtr(!tc.globalOff), nil)
			h := AutosyncHandler(ctrl, orderOf("web"), nil, nil)

			out := captureLog(t, func() {
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/autosync", strings.NewReader(tc.body)))
				if rec.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200", rec.Code)
				}
			})

			for _, want := range tc.wantSubstrs {
				if !strings.Contains(out, want) {
					t.Errorf("log missing %q; got %s", want, out)
				}
			}
			if !strings.Contains(out, tc.wantEffective) {
				t.Errorf("log missing %q; got %s", tc.wantEffective, out)
			}
		})
	}
}

// A rejected request must not claim a change happened.
func TestAutosyncHandler_PostDoesNotLogAnInvalidRequest(t *testing.T) {
	ctrl := autosync.NewController(boolPtr(true), nil)
	h := AutosyncHandler(ctrl, orderOf("web"), nil, nil)

	out := captureLog(t, func() {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/autosync",
			strings.NewReader(`{"scope":"nonsense","enabled":true}`)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	if strings.Contains(out, "autosync set") {
		t.Errorf("a rejected request must not be logged as a change; got %s", out)
	}
}
