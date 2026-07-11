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
