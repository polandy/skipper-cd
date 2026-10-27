package nixos

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeOutputter records the evaluation call and returns canned stdout.
type fakeOutputter struct {
	calls []runCall
	out   string
	err   error
}

func (f *fakeOutputter) Output(_ context.Context, dir string, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, runCall{dir: dir, name: name, args: args})
	return []byte(f.out), f.err
}

func TestSystemDerivation_EvaluatesWithRevisionNeutralized(t *testing.T) {
	out := &fakeOutputter{out: "/nix/store/aaaa-nixos-system-host-a.drv\n"}

	drv, err := SystemDerivation(context.Background(), out, "/repo", ".#host-a")
	if err != nil {
		t.Fatal(err)
	}

	if drv != "/nix/store/aaaa-nixos-system-host-a.drv" {
		t.Errorf("drv = %q, want the trimmed store path", drv)
	}
	want := runCall{dir: "/repo", name: "nix", args: []string{
		"--extra-experimental-features", "nix-command flakes",
		"eval", "--raw", `.#nixosConfigurations."host-a"`,
		"--apply", `c: (c.extendModules { modules = [ { system.configurationRevision = c.pkgs.lib.mkForce null; } ]; }).config.system.build.toplevel.drvPath`,
	}}
	if len(out.calls) != 1 || out.calls[0].dir != want.dir || out.calls[0].name != want.name || !slices.Equal(out.calls[0].args, want.args) {
		t.Errorf("calls = %+v, want exactly %+v", out.calls, want)
	}
}

func TestSystemDerivation_KeepsTheFlakeRef(t *testing.T) {
	out := &fakeOutputter{out: "/nix/store/aaaa.drv"}
	if _, err := SystemDerivation(context.Background(), out, "/repo", "/srv/repo#nuc"); err != nil {
		t.Fatal(err)
	}
	if got := out.calls[0].args[4]; got != `/srv/repo#nixosConfigurations."nuc"` {
		t.Errorf("installable = %q", got)
	}
}

func TestSystemDerivation_Failures(t *testing.T) {
	for name, tc := range map[string]struct {
		flake string
		out   string
		err   error
	}{
		"eval error":       {flake: ".#host-a", err: errors.New("exit status 1")},
		"empty output":     {flake: ".#host-a", out: ""},
		"not a drv":        {flake: ".#host-a", out: "/nix/store/aaaa-nixos-system"},
		"relative path":    {flake: ".#host-a", out: "aaaa.drv"},
		"several lines":    {flake: ".#host-a", out: "/nix/store/a.drv\n/nix/store/b.drv"},
		"flake without #":  {flake: "."},
		"empty attribute":  {flake: ".#"},
		"empty flake ref":  {flake: "#host-a"},
		"nothing at all":   {flake: ""},
		"embedded spaces":  {flake: ".#host-a", out: "/nix/store/a b.drv"},
		"warning prefixed": {flake: ".#host-a", out: "warning: /nix/store/a.drv"},
	} {
		t.Run(name, func(t *testing.T) {
			out := &fakeOutputter{out: tc.out, err: tc.err}
			drv, err := SystemDerivation(context.Background(), out, "/repo", tc.flake)
			if err == nil {
				t.Fatalf("expected an error, got drv %q", drv)
			}
			if drv != "" {
				t.Errorf("a failure must return no derivation, got %q", drv)
			}
		})
	}
}

func TestSplitFlakeAttr(t *testing.T) {
	for flake, want := range map[string][2]string{
		".#host-a":          {".", "host-a"},
		"/srv/repo#nuc":     {"/srv/repo", "nuc"},
		"github:o/r#my.box": {"github:o/r", "my.box"},
	} {
		ref, name, ok := SplitFlakeAttr(flake)
		if !ok || ref != want[0] || name != want[1] {
			t.Errorf("SplitFlakeAttr(%q) = %q, %q, %v; want %q, %q, true", flake, ref, name, ok, want[0], want[1])
		}
	}
	for _, flake := range []string{"", ".", ".#", "#host"} {
		if _, _, ok := SplitFlakeAttr(flake); ok {
			t.Errorf("SplitFlakeAttr(%q) must not be ok", flake)
		}
	}
}

// TestSystemDrvApplyExpr_MatchesFlakeCheck keeps the flake check
// (nix/system-drv-check.nix), which proves the expression's revision
// neutralization against a real nixosSystem, testing the expression skipper runs.
func TestSystemDrvApplyExpr_MatchesFlakeCheck(t *testing.T) {
	check, err := os.ReadFile(filepath.Join("..", "..", "nix", "system-drv-check.nix"))
	if err != nil {
		t.Fatalf("read flake check: %v", err)
	}
	if !strings.Contains(string(check), "systemDrv = "+SystemDrvApplyExpr+";") {
		t.Errorf("nix/system-drv-check.nix must define systemDrv as SystemDrvApplyExpr verbatim:\n%s", SystemDrvApplyExpr)
	}
}
