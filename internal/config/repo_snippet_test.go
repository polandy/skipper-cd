package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestYamlSnippet_MarksTheFailingLine(t *testing.T) {
	src := []byte("one\ntwo\nthree\nfour\nfive\nsix\n")

	got := yamlSnippet(src, 3)

	want := strings.Join([]string{
		"    1 | one",
		"    2 | two",
		"  > 3 | three",
		"    4 | four",
		"    5 | five",
	}, "\n")
	if got != want {
		t.Errorf("snippet =\n%s\nwant\n%s", got, want)
	}
}

func TestYamlSnippet_ClipsAtTheEdges(t *testing.T) {
	src := []byte("one\ntwo\n")

	if got := yamlSnippet(src, 1); !strings.Contains(got, "> 1 | one") || strings.Contains(got, "0 |") {
		t.Errorf("snippet at line 1 =\n%s", got)
	}
	if got := yamlSnippet(src, 2); !strings.Contains(got, "> 2 | two") || strings.Contains(got, "3 |") {
		t.Errorf("snippet at last line =\n%s", got)
	}
}

func TestYamlSnippet_UnknownLineYieldsNothing(t *testing.T) {
	src := []byte("one\n")
	if got := yamlSnippet(src, 0); got != "" {
		t.Errorf("line 0 should yield no snippet, got %q", got)
	}
	if got := yamlSnippet(src, 99); got != "" {
		t.Errorf("out-of-range line should yield no snippet, got %q", got)
	}
}

func TestLoadRepoStacks_ParseErrorCarriesSnippet(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/web/docker-compose.yml": minimalCompose,
		// The stray tab on line 3 breaks the YAML there.
		"stacks/skipper.yaml": "stacks:\n  web:\n\ticon: nginx\n",
	})

	_, _, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"))
	if err == nil {
		t.Fatal("expected a parse error")
	}
	if !strings.Contains(err.Error(), "> 3 |") {
		t.Errorf("parse error should mark line 3:\n%s", err)
	}
}

func TestLoadRepoStacks_UnknownFieldErrorCarriesSnippet(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/web/docker-compose.yml": minimalCompose,
		"stacks/skipper.yaml":           "stacks:\n  web:\n    depends_onn: [db]\n",
	})

	_, _, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"))
	if err == nil {
		t.Fatal("expected an unknown-field error")
	}
	if !strings.Contains(err.Error(), "depends_onn") || !strings.Contains(err.Error(), "> 3 |") {
		t.Errorf("unknown-field error should mark the misspelled line:\n%s", err)
	}
}

func TestLoadRepoStacks_UnknownEntryErrorCarriesSnippet(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/web/docker-compose.yml": minimalCompose,
		"stacks/skipper.yaml":           "stacks:\n  web:\n    icon: nginx\n  ghost:\n    icon: casper\n",
	})

	_, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"))
	if err != nil || len(stackErrs) != 1 {
		t.Fatalf("err=%v stackErrs=%v", err, stackErrs)
	}
	msg := stackErrs[0].Err.Error()
	if !strings.Contains(msg, "> 4 |   ghost:") {
		t.Errorf("unknown-entry error should show the entry line:\n%s", msg)
	}
}

func TestLoadRepoStacks_HealthCheckErrorPointsAtTheField(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/bad/docker-compose.yml": minimalCompose,
		"stacks/skipper.yaml":           "stacks:\n  bad:\n    icon: x\n    health_check:\n      url: notaurl\n",
	})

	_, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"))
	if err != nil || len(stackErrs) != 1 {
		t.Fatalf("err=%v stackErrs=%v", err, stackErrs)
	}
	msg := stackErrs[0].Err.Error()
	if !strings.Contains(msg, "> 4 |     health_check:") {
		t.Errorf("health_check error should mark the field line:\n%s", msg)
	}
}

func TestLoadRepoStacks_DependencyErrorPointsAtTheField(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/app/docker-compose.yml": minimalCompose,
		"stacks/skipper.yaml":           "stacks:\n  app:\n    depends_on: [missing]\n",
	})

	_, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"))
	if err != nil || len(stackErrs) != 1 {
		t.Fatalf("err=%v stackErrs=%v", err, stackErrs)
	}
	msg := stackErrs[0].Err.Error()
	if !strings.Contains(msg, "> 3 |     depends_on: [missing]") {
		t.Errorf("depends_on error should mark the field line:\n%s", msg)
	}
}

func TestLoadRepoStacks_CycleErrorsPointAtTheEdges(t *testing.T) {
	repoDir := writeRepo(t, map[string]string{
		"stacks/a/docker-compose.yml": minimalCompose,
		"stacks/b/docker-compose.yml": minimalCompose,
		"stacks/skipper.yaml":         "stacks:\n  a:\n    depends_on: [b]\n  b:\n    depends_on: [a]\n",
	})

	_, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"))
	if err != nil || len(stackErrs) != 2 {
		t.Fatalf("err=%v stackErrs=%v", err, stackErrs)
	}
	for _, se := range stackErrs {
		if !strings.Contains(se.Err.Error(), "depends_on:") || !strings.Contains(se.Err.Error(), "> ") {
			t.Errorf("cycle error for %s should carry a marked snippet:\n%s", se.Stack, se.Err)
		}
	}
}

func TestLoadRepoStacks_ErrorWithoutFileLocationHasNoSnippet(t *testing.T) {
	// A reserved directory name with no skipper.yaml entry has no location to
	// point at — the error must stay clean rather than showing a bogus excerpt.
	repoDir := writeRepo(t, map[string]string{
		"stacks/_nixos/docker-compose.yml": minimalCompose,
	})

	_, stackErrs, err := LoadRepoStacks(filepath.Join(repoDir, "stacks"))
	if err != nil || len(stackErrs) != 1 {
		t.Fatalf("err=%v stackErrs=%v", err, stackErrs)
	}
	if strings.Contains(stackErrs[0].Err.Error(), "|") {
		t.Errorf("error without a file location must carry no snippet:\n%s", stackErrs[0].Err)
	}
}
