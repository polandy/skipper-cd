package health

import (
	"strings"
	"testing"
	"text/template"
)

// inspectedContainer mirrors the field names of the docker CLI's typed
// container-inspect struct, which the CLI executes an inspect template against
// first; a template that does not fit it falls back to the raw JSON map, where
// a container without a healthcheck prints nothing.
type inspectedContainer struct {
	ID     string
	Name   string
	Config *struct{ Labels map[string]string }
	State  *struct {
		Status   string
		Health   *struct{ Status string }
		ExitCode int
	}
}

func TestInspectColumns_ExecuteAgainstTheTypedInspectStruct(t *testing.T) {
	tmpl, err := template.New("inspect").Option("missingkey=error").Parse(inspectArgs[len(inspectArgs)-1])
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c := inspectedContainer{ID: "abc", Name: "/app-web-1"}
	c.Config = &struct{ Labels map[string]string }{Labels: map[string]string{workingDirLabel: "/srv/app", serviceLabel: "web"}}
	c.State = &struct {
		Status   string
		Health   *struct{ Status string }
		ExitCode int
	}{Status: "running"}

	var b strings.Builder
	if err := tmpl.Execute(&b, c); err != nil {
		t.Fatalf("template does not fit the typed inspect struct: %v", err)
	}
	if want := "abc\t/srv/app\tweb\t/app-web-1\trunning\t\t0"; b.String() != want {
		t.Errorf("got %q, want %q", b.String(), want)
	}
}
