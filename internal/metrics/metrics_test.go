package metrics_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	_ "github.com/polandy/skipper-cd/internal/metrics"
)

// metricsDoc is the user-facing metric reference; its table is the contract
// dashboards and alerts are built against.
const metricsDoc = "../../docs/metrics.md"

// declaredNames returns the metric names metrics.go registers, read from the
// Name: field of each collector's Opts literal. Parsed rather than gathered
// because a *Vec with no observed label values exports nothing until it is
// used, so gathering would silently miss most of them — exactly the ones most
// likely to be new.
func declaredNames(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "metrics.go", nil, 0)
	if err != nil {
		t.Fatalf("parse metrics.go: %v", err)
	}
	var names []string
	ast.Inspect(f, func(n ast.Node) bool {
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		if key, ok := kv.Key.(*ast.Ident); !ok || key.Name != "Name" {
			return true
		}
		lit, ok := kv.Value.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		name, err := strconv.Unquote(lit.Value)
		if err != nil {
			t.Fatalf("unquote %s: %v", lit.Value, err)
		}
		names = append(names, name)
		return true
	})
	if len(names) == 0 {
		t.Fatal("found no metric names in metrics.go — the Opts shape must have changed")
	}
	sort.Strings(names)
	return names
}

// documentedNames returns the metric names listed in the reference table.
func documentedNames(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(metricsDoc)
	if err != nil {
		t.Fatalf("read %s: %v", metricsDoc, err)
	}
	// Table rows open with the metric name in backticks; prose elsewhere on the
	// page mentions names inside queries, so anchor on the row start.
	row := regexp.MustCompile("(?m)^\\| `(skipper_[a-z0-9_]+)`")
	var names []string
	for _, m := range row.FindAllStringSubmatch(string(data), -1) {
		names = append(names, m[1])
	}
	if len(names) == 0 {
		t.Fatalf("found no metric rows in %s — the table format must have changed", metricsDoc)
	}
	sort.Strings(names)
	return names
}

func TestEveryMetricIsDocumented(t *testing.T) {
	declared := declaredNames(t)
	documented := documentedNames(t)

	inDoc := make(map[string]bool, len(documented))
	for _, n := range documented {
		inDoc[n] = true
	}
	for _, n := range declared {
		if !inDoc[n] {
			t.Errorf("metric %q is registered but missing from %s — a dashboard cannot be built against an undocumented metric", n, metricsDoc)
		}
	}
}

func TestNoDocumentedMetricHasBeenRemoved(t *testing.T) {
	declared := declaredNames(t)
	documented := documentedNames(t)

	registered := make(map[string]bool, len(declared))
	for _, n := range declared {
		registered[n] = true
	}
	for _, n := range documented {
		if !registered[n] {
			t.Errorf("%s documents %q but nothing registers it — a renamed metric silently breaks every dashboard querying the old name", metricsDoc, n)
		}
	}
}

func TestMetricNamesFollowTheSkipperPrefix(t *testing.T) {
	// Everything this binary exports shares one prefix, so a Prometheus scrape
	// covering several jobs can be filtered to skipper by name alone.
	for _, n := range declaredNames(t) {
		if !strings.HasPrefix(n, "skipper_") {
			t.Errorf("metric %q does not carry the skipper_ prefix", n)
		}
	}
}

func TestDefaultRegistryGathers(t *testing.T) {
	// Importing the package registers every collector on the default registry
	// via promauto, which panics on a duplicate name. Gathering proves the
	// registration went through and that no two collectors collide.
	got, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather from the default registry: %v", err)
	}

	seen := make(map[string]bool, len(got))
	for _, mf := range got {
		if seen[mf.GetName()] {
			t.Errorf("metric family %q gathered twice", mf.GetName())
		}
		seen[mf.GetName()] = true
	}
}
