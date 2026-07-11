package autosync

import (
	"testing"
	"time"
)

func TestQueueMarkCountClear(t *testing.T) {
	q := NewQueue()
	if q.Count() != 0 {
		t.Fatalf("new queue count = %d, want 0", q.Count())
	}

	q.Mark("a", []string{"f1"}, "global")
	q.Mark("b", []string{"f2"}, "stack")
	if q.Count() != 2 {
		t.Fatalf("count = %d, want 2", q.Count())
	}

	q.Clear("a")
	if q.Count() != 1 {
		t.Fatalf("count after clear = %d, want 1", q.Count())
	}
	q.Clear("missing") // must not panic
	if q.Count() != 1 {
		t.Fatalf("count after clearing absent = %d, want 1", q.Count())
	}
}

func TestQueueMarkPreservesSinceUpdatesFiles(t *testing.T) {
	q := NewQueue()
	q.now = func() time.Time { return time.Unix(1000, 0) }
	q.Mark("a", []string{"f1"}, "global")

	q.now = func() time.Time { return time.Unix(2000, 0) } // later re-mark
	q.Mark("a", []string{"f1", "f2"}, "stack")

	items := q.Snapshot([]string{"a"})
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if !items[0].Since.Equal(time.Unix(1000, 0)) {
		t.Fatalf("Since = %v, want the first queued time (1000)", items[0].Since)
	}
	if len(items[0].ChangedFiles) != 2 || items[0].Reason != "stack" {
		t.Fatalf("re-mark should update files/reason, got %+v", items[0])
	}
}

func TestQueueSnapshotDeployOrder(t *testing.T) {
	q := NewQueue()
	// Marked out of order; snapshot must follow the given deploy order.
	q.Mark("gitea", []string{"x"}, "stack")
	q.Mark("_nixos", []string{"y"}, "global")
	q.Mark("traefik", []string{"z"}, "global")

	order := []string{"_nixos", "traefik", "gitea", "authelia"}
	got := []string{}
	for _, it := range q.Snapshot(order) {
		got = append(got, it.Stack)
	}
	want := []string{"_nixos", "traefik", "gitea"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestQueueSnapshotIncludesUnorderedStacks(t *testing.T) {
	q := NewQueue()
	q.Mark("zeta", nil, "global")
	q.Mark("alpha", nil, "global")

	// Neither is in the deploy order; they must still appear, sorted by name.
	got := []string{}
	for _, it := range q.Snapshot([]string{"other"}) {
		got = append(got, it.Stack)
	}
	if len(got) != 2 || got[0] != "alpha" || got[1] != "zeta" {
		t.Fatalf("got %v, want [alpha zeta]", got)
	}
}
