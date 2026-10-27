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

	drv, err := SystemDerivation(context.Background(), out, "/repo", ".#host-a", noHostname(t))
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
	if _, err := SystemDerivation(context.Background(), out, "/repo", "/srv/repo#nuc", noHostname(t)); err != nil {
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
		"empty flake ref":  {flake: "#host-a"},
		"nothing at all":   {flake: ""},
		"embedded spaces":  {flake: ".#host-a", out: "/nix/store/a b.drv"},
		"warning prefixed": {flake: ".#host-a", out: "warning: /nix/store/a.drv"},
	} {
		t.Run(name, func(t *testing.T) {
			out := &fakeOutputter{out: tc.out, err: tc.err}
			drv, err := SystemDerivation(context.Background(), out, "/repo", tc.flake, noHostname(t))
			if err == nil {
				t.Fatalf("expected an error, got drv %q", drv)
			}
			if drv != "" {
				t.Errorf("a failure must return no derivation, got %q", drv)
			}
		})
	}
}

// noHostname fails the test if the evaluation asks for the host name: the
// flake names its configuration.
func noHostname(t *testing.T) func() (string, error) {
	return func() (string, error) {
		t.Error("the host name must only be read when the flake names no configuration")
		return "", errors.New("unexpected")
	}
}

func TestSystemDerivation_FallsBackToHostName(t *testing.T) {
	for _, flake := range []string{".", ".#"} {
		out := &fakeOutputter{out: "/nix/store/aaaa.drv"}
		hostname := func() (string, error) { return "nuc", nil }

		if _, err := SystemDerivation(context.Background(), out, "/repo", flake, hostname); err != nil {
			t.Fatalf("flake %q: %v", flake, err)
		}
		if len(out.calls) != 1 || out.calls[0].args[4] != `.#nixosConfigurations."nuc"` {
			t.Errorf("flake %q: calls = %+v, want the installable .#nixosConfigurations.\"nuc\"", flake, out.calls)
		}
	}
}

func TestSystemDerivation_HostNameFailure(t *testing.T) {
	for name, hostname := range map[string]func() (string, error){
		"read error": func() (string, error) { return "", errors.New("no uts namespace") },
		"empty":      func() (string, error) { return "", nil },
	} {
		t.Run(name, func(t *testing.T) {
			out := &fakeOutputter{out: "/nix/store/aaaa.drv"}
			drv, err := SystemDerivation(context.Background(), out, "/repo", ".", hostname)
			if err == nil {
				t.Fatalf("expected an error, got drv %q", drv)
			}
			if len(out.calls) != 0 {
				t.Errorf("nothing to evaluate without a configuration name, got calls %+v", out.calls)
			}
		})
	}
}

func TestParseFlake(t *testing.T) {
	for flake, want := range map[string][2]string{
		".#host-a":          {".", "host-a"},
		"/srv/repo#nuc":     {"/srv/repo", "nuc"},
		"github:o/r#my.box": {"github:o/r", "my.box"},
		".":                 {".", ""},
		".#":                {".", ""},
		"path:/a#b#host":    {"path:/a#b", "host"}, // split at the last "#", as nixos-rebuild does
	} {
		ref, name, ok := ParseFlake(flake)
		if !ok || ref != want[0] || name != want[1] {
			t.Errorf("ParseFlake(%q) = %q, %q, %v; want %q, %q, true", flake, ref, name, ok, want[0], want[1])
		}
	}
	for _, flake := range []string{"", "#", "#host"} {
		if _, _, ok := ParseFlake(flake); ok {
			t.Errorf("ParseFlake(%q) must not be ok", flake)
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
