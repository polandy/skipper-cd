package config

import (
	"slices"
	"testing"
)

// TestCyclicStacks covers the peel both modes now share: the host config
// rejects the whole file on a cycle, stack discovery fails only the stacks
// named, and both must agree on which stacks those are.
func TestCyclicStacks(t *testing.T) {
	tests := []struct {
		name   string
		stacks []Stack
		want   []string
	}{
		{
			name:   "a resolvable chain has no cycle",
			stacks: []Stack{{Name: "db"}, {Name: "api", DependsOn: []string{"db"}}, {Name: "web", DependsOn: []string{"api"}}},
		},
		{
			name:   "two stacks depending on each other are both stuck",
			stacks: []Stack{{Name: "a", DependsOn: []string{"b"}}, {Name: "b", DependsOn: []string{"a"}}},
			want:   []string{"a", "b"},
		},
		{
			name:   "a stack depending on itself is stuck",
			stacks: []Stack{{Name: "a", DependsOn: []string{"a"}}},
			want:   []string{"a"},
		},
		{
			// Downstream of a cycle cannot be ordered either, so it is reported
			// with it rather than deployed against an unresolved dependency.
			name: "a dependent of a cycle is stuck with it",
			stacks: []Stack{
				{Name: "a", DependsOn: []string{"b"}},
				{Name: "b", DependsOn: []string{"a"}},
				{Name: "web", DependsOn: []string{"a"}},
			},
			want: []string{"a", "b", "web"},
		},
		{
			// Discovery drops a broken stack before this runs; its dependents
			// must not all read as cyclic just because the name is gone.
			name:   "a dependency outside the set does not make a stack cyclic",
			stacks: []Stack{{Name: "web", DependsOn: []string{"gone"}}},
		},
		{
			name:   "the report keeps config order",
			stacks: []Stack{{Name: "z", DependsOn: []string{"y"}}, {Name: "y", DependsOn: []string{"z"}}},
			want:   []string{"z", "y"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cyclicStacks(tt.stacks)
			if !slices.Equal(got, tt.want) {
				t.Errorf("cyclicStacks = %v, want %v", got, tt.want)
			}
		})
	}
}
