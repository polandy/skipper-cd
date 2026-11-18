package autosync

import "testing"

func ptr(b bool) *bool { return &b }

func TestControllerEffective_ResolutionMatrix(t *testing.T) {
	tests := []struct {
		name     string
		global   *bool
		stackCfg *bool
		want     bool
	}{
		{"global on, stack unset", ptr(true), nil, true},
		{"global off, stack unset", ptr(false), nil, false},
		{"global off, stack on (override wins)", ptr(false), ptr(true), true},
		{"global on, stack off (override wins)", ptr(true), ptr(false), false},
		{"global unset defaults on", nil, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewController(tt.global, map[string]*bool{"web": tt.stackCfg})
			if got := c.Effective("web"); got != tt.want {
				t.Fatalf("Effective = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestControllerUIOverride_WinsOverConfigAndClears(t *testing.T) {
	c := NewController(ptr(true), map[string]*bool{"web": ptr(true)})

	c.SetStack("web", ptr(false))
	if c.Effective("web") {
		t.Fatal("stack override false should pause the stack")
	}

	c.SetStack("web", nil) // clear override
	if !c.Effective("web") {
		t.Fatal("cleared override should fall back to config (on)")
	}
}

func TestControllerGlobalOverride(t *testing.T) {
	c := NewController(ptr(true), nil)
	c.SetGlobal(ptr(false))
	if c.GlobalEffective() {
		t.Fatal("global override false should make global off")
	}
	if c.Effective("anything") {
		t.Fatal("unset stack should follow global override (off)")
	}
	c.SetGlobal(nil)
	if !c.GlobalEffective() {
		t.Fatal("cleared global override should fall back to config (on)")
	}
}

func TestControllerReason(t *testing.T) {
	c := NewController(ptr(false), map[string]*bool{"a": ptr(false)})
	if got := c.Reason("a"); got != "stack" {
		t.Fatalf("Reason(a) = %q, want stack (paused by its own setting)", got)
	}
	if got := c.Reason("b"); got != "global" {
		t.Fatalf("Reason(b) = %q, want global", got)
	}

	on := NewController(ptr(true), nil)
	if got := on.Reason("x"); got != "" {
		t.Fatalf("Reason(x) = %q, want empty when autosync is on", got)
	}
}

// overriddenOf reports whether the controller currently holds a UI override for
// the stack (via the public snapshot, so the test does not reach into internals).
func overriddenOf(c *Controller, name string) bool {
	for _, s := range c.Snapshot([]string{name}).Stacks {
		if s.Name == name {
			return s.Overridden
		}
	}
	return false
}

// A per-stack override equal to what the stack would inherit anyway must not be
// stored: toggling a stack back to its baseline value is the "return to inherit"
// gesture, not a sticky pin. See ADR-0019.
func TestControllerSetStack_CollapsesRedundantOverride(t *testing.T) {
	c := NewController(ptr(true), nil) // global on, web inherits

	c.SetStack("web", ptr(false)) // forced-off (differs from baseline on)
	if c.Effective("web") {
		t.Fatal("forced-off override should pause the stack")
	}
	if !overriddenOf(c, "web") {
		t.Fatal("an override differing from baseline must be held")
	}

	c.SetStack("web", ptr(true)) // == baseline (on) → collapse to inherit
	if !c.Effective("web") {
		t.Fatal("re-enabling should resume the stack")
	}
	if overriddenOf(c, "web") {
		t.Fatal("a redundant override (== baseline) must collapse to inherit, not pin")
	}
}

// Toggling global collapses every per-stack override that now equals its
// baseline, so the global switch is a true master and a UI pause does not survive
// a global off→on cycle. This is the chosen semantic of ADR-0019.
func TestControllerSetGlobal_CollapsesRedundantStackOverride(t *testing.T) {
	c := NewController(ptr(true), nil)

	c.SetStack("web", ptr(false)) // forced-off while global on (a real exception)

	c.SetGlobal(ptr(false)) // baseline(web) now off; override off == baseline → collapse
	if overriddenOf(c, "web") {
		t.Fatal("override equal to the new global baseline must collapse")
	}
	if c.Effective("web") {
		t.Fatal("collapsed stack now inherits global off → paused")
	}

	c.SetGlobal(ptr(true)) // web inherits → resumes with global ("pause gone")
	if !c.Effective("web") {
		t.Fatal("global on must resume the collapsed stack")
	}
}

// A per-stack override that still differs from the new global baseline is a
// genuine exception and must survive the global toggle.
func TestControllerSetGlobal_KeepsGenuineException(t *testing.T) {
	c := NewController(ptr(true), nil)

	c.SetStack("web", ptr(false)) // forced-off
	c.SetGlobal(ptr(true))        // baseline stays on; off != on → kept

	if c.Effective("web") {
		t.Fatal("a genuine forced-off exception must survive a global toggle")
	}
	if !overriddenOf(c, "web") {
		t.Fatal("the exception must still be an override")
	}
}

// Config-pinned stacks pin the baseline to the config value, so global is
// irrelevant to them and toggling a stack to its config value collapses back to
// config (not to a UI override).
func TestControllerSetStack_CollapsesToConfigBaseline(t *testing.T) {
	c := NewController(ptr(true), map[string]*bool{"web": ptr(false)}) // config off

	c.SetStack("web", ptr(true)) // forced-on (differs from config off)
	if !c.Effective("web") {
		t.Fatal("forced-on override should resume the config-off stack")
	}

	c.SetGlobal(ptr(false)) // baseline(web) = config off; on != off → kept, global irrelevant
	if !c.Effective("web") {
		t.Fatal("config-pinned forced-on must be unaffected by the global toggle")
	}

	c.SetStack("web", ptr(false)) // == config baseline (off) → collapse to config
	if c.Effective("web") {
		t.Fatal("toggling to the config value collapses to config off")
	}
	if overriddenOf(c, "web") {
		t.Fatal("no UI override should remain; the stack is back on config")
	}
}

func TestControllerSnapshot(t *testing.T) {
	c := NewController(ptr(true), map[string]*bool{"a": ptr(false)})
	c.SetStack("b", ptr(false)) // UI override

	snap := c.Snapshot([]string{"a", "b", "c"})
	if !snap.Global || snap.GlobalOverridden {
		t.Fatalf("global = %v overridden = %v, want true/false", snap.Global, snap.GlobalOverridden)
	}
	if len(snap.Stacks) != 3 {
		t.Fatalf("got %d stacks, want 3", len(snap.Stacks))
	}

	byName := map[string]StackState{}
	for _, s := range snap.Stacks {
		byName[s.Name] = s
	}
	if s := byName["a"]; s.Effective || s.Overridden || s.Config == nil || *s.Config {
		t.Fatalf("a = %+v, want effective=false overridden=false config=false", s)
	}
	if s := byName["b"]; s.Effective || !s.Overridden || s.Config != nil {
		t.Fatalf("b = %+v, want effective=false overridden=true config=nil", s)
	}
	if s := byName["c"]; !s.Effective || s.Overridden || s.Config != nil {
		t.Fatalf("c = %+v, want effective=true overridden=false config=nil", s)
	}
}

// The snapshot version orders the two channels that publish autosync state (the
// POST response and the SSE broadcast), so it must advance on every mutation and
// never go backwards.
func TestControllerSnapshot_VersionAdvancesOnEveryMutation(t *testing.T) {
	c := NewController(ptr(true), nil)
	order := []string{"web"}

	if v := c.Snapshot(order).Version; v != 0 {
		t.Fatalf("fresh controller version = %d, want 0", v)
	}

	prev := uint64(0)
	mutations := []func(){
		func() { c.SetGlobal(ptr(false)) },
		func() { c.SetStack("web", ptr(true)) },
		func() { c.SetStack("web", nil) },  // clearing is a mutation too
		func() { c.SetGlobal(ptr(false)) }, // a no-op write still advances
		func() { c.SetGlobal(nil) },
	}
	for i, mutate := range mutations {
		mutate()
		v := c.Snapshot(order).Version
		if v <= prev {
			t.Fatalf("mutation %d: version = %d, want > %d", i, v, prev)
		}
		prev = v
	}
}

// Reading the state must not advance the version — otherwise every SSE republish
// would look newer than the toggle the UI just applied.
func TestControllerSnapshot_VersionStableWithoutMutation(t *testing.T) {
	c := NewController(ptr(true), nil)
	c.SetGlobal(ptr(false))

	first := c.Snapshot(nil).Version
	if v := c.Snapshot(nil).Version; v != first {
		t.Fatalf("version = %d after a second read, want it unchanged at %d", v, first)
	}
}
