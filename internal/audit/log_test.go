package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/polandy/skipper-cd/internal/events"
)

func at(minutes int) time.Time {
	return time.Date(2026, 7, 18, 0, minutes, 0, 0, time.UTC)
}

func terminalEvent(stack string, status events.Status, ts time.Time) events.DeployEvent {
	return events.DeployEvent{Stack: stack, Status: status, Timestamp: ts}
}

func TestLog_RecordsTerminalOutcomeWithMappedFields(t *testing.T) {
	l := NewLog("")
	l.Record(events.DeployEvent{
		Stack:        "immich",
		Status:       events.StatusSuccess,
		Timestamp:    at(1),
		DurationMs:   4200,
		Error:        "",
		ChangedFiles: []string{"docker-compose.yml", "app.env"},
		Commits: []events.CommitInfo{
			{SHA: "headsha"}, // newest first — deployed HEAD
			{SHA: "oldersha"},
		},
	})

	got := l.Stack("immich", 0)
	if len(got) != 1 {
		t.Fatalf("want 1 record, got %d", len(got))
	}
	r := got[0]
	if r.Stack != "immich" || r.Status != events.StatusSuccess {
		t.Errorf("stack/status wrong: %+v", r)
	}
	if r.DurationMs != 4200 {
		t.Errorf("DurationMs = %d, want 4200", r.DurationMs)
	}
	if r.ChangedFiles != 2 {
		t.Errorf("ChangedFiles = %d, want 2 (count, not list)", r.ChangedFiles)
	}
	if r.CommitSHA != "headsha" {
		t.Errorf("CommitSHA = %q, want the newest (HEAD) commit %q", r.CommitSHA, "headsha")
	}
}

func TestLog_IgnoresNonTerminalStatuses(t *testing.T) {
	l := NewLog("")
	for _, s := range []events.Status{
		events.StatusDeploying,
		events.StatusSkipped,
		events.StatusQueued,
		events.StatusBlocked,
	} {
		l.Record(terminalEvent("web", s, at(1)))
	}
	if got := l.Stack("web", 0); len(got) != 0 {
		t.Fatalf("non-terminal statuses must not be recorded, got %d: %+v", len(got), got)
	}
}

func TestLog_RecordsAllTerminalStatuses(t *testing.T) {
	l := NewLog("")
	terminals := []events.Status{
		events.StatusSuccess,
		events.StatusFailed,
		events.StatusRolledBack,
		events.StatusRolledBackUnhealthy,
		events.StatusHealed,
		events.StatusHealExhausted,
		events.StatusRemoved,
	}
	for i, s := range terminals {
		l.Record(terminalEvent("web", s, at(i+1)))
	}
	if got := l.Stack("web", 0); len(got) != len(terminals) {
		t.Fatalf("want %d terminal records, got %d", len(terminals), len(got))
	}
}

func TestLog_StackReturnsNewestFirstWithLimit(t *testing.T) {
	l := NewLog("")
	l.Record(terminalEvent("web", events.StatusSuccess, at(1)))
	l.Record(terminalEvent("web", events.StatusFailed, at(2)))
	l.Record(terminalEvent("web", events.StatusSuccess, at(3)))

	got := l.Stack("web", 2)
	if len(got) != 2 {
		t.Fatalf("limit 2: got %d", len(got))
	}
	if !got[0].Timestamp.Equal(at(3)) || !got[1].Timestamp.Equal(at(2)) {
		t.Errorf("want newest-first [at(3), at(2)], got [%v, %v]", got[0].Timestamp, got[1].Timestamp)
	}
}

func TestLog_PerStackCapEvictsOldestOfSameStackOnly(t *testing.T) {
	l := newLog("", 3)
	// busy stack overflows its cap...
	for i := 1; i <= 5; i++ {
		l.Record(terminalEvent("busy", events.StatusSuccess, at(i)))
	}
	// ...a quiet stack recorded long ago must survive untouched.
	l.Record(terminalEvent("quiet", events.StatusSuccess, at(0)))

	busy := l.Stack("busy", 0)
	if len(busy) != 3 {
		t.Fatalf("busy capped at 3, got %d", len(busy))
	}
	if !busy[len(busy)-1].Timestamp.Equal(at(3)) {
		t.Errorf("oldest kept should be at(3) (at(1),at(2) evicted), got %v", busy[len(busy)-1].Timestamp)
	}
	if got := l.Stack("quiet", 0); len(got) != 1 {
		t.Fatalf("quiet stack must not be evicted by busy stack, got %d", len(got))
	}
}

func TestLog_PersistsAndReloads(t *testing.T) {
	dir := t.TempDir()
	l := NewLog(dir)
	l.Record(terminalEvent("web", events.StatusSuccess, at(1)))
	l.Record(terminalEvent("db", events.StatusFailed, at(2)))

	reloaded := NewLog(dir)
	if got := reloaded.Stack("web", 0); len(got) != 1 || got[0].Status != events.StatusSuccess {
		t.Errorf("web not reloaded: %+v", got)
	}
	if got := reloaded.Stack("db", 0); len(got) != 1 || got[0].Status != events.StatusFailed {
		t.Errorf("db not reloaded: %+v", got)
	}
}

func TestLog_LoadSkipsTornTrailingLine(t *testing.T) {
	dir := t.TempDir()
	l := NewLog(dir)
	l.Record(terminalEvent("web", events.StatusSuccess, at(1)))

	// Simulate a crash mid-append: a truncated JSON line at the end.
	f, err := os.OpenFile(filepath.Join(dir, auditFileName), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"stack":"web","status":"succ`)
	f.Close()

	reloaded := NewLog(dir)
	if got := reloaded.Stack("web", 0); len(got) != 1 {
		t.Fatalf("torn line must be skipped, good record kept: got %d", len(got))
	}
}

func TestLog_RecentAcrossStacksNewestFirst(t *testing.T) {
	l := NewLog("")
	l.Record(terminalEvent("a", events.StatusSuccess, at(1)))
	l.Record(terminalEvent("b", events.StatusSuccess, at(3)))
	l.Record(terminalEvent("a", events.StatusSuccess, at(2)))

	got := l.Recent(0)
	if len(got) != 3 {
		t.Fatalf("want 3, got %d", len(got))
	}
	if !got[0].Timestamp.Equal(at(3)) || !got[1].Timestamp.Equal(at(2)) || !got[2].Timestamp.Equal(at(1)) {
		t.Errorf("want global newest-first at(3),at(2),at(1); got %v,%v,%v",
			got[0].Timestamp, got[1].Timestamp, got[2].Timestamp)
	}
}

func TestLog_EmptyStateDirIsInMemoryOnly(t *testing.T) {
	l := NewLog("")
	l.Record(terminalEvent("web", events.StatusSuccess, at(1)))
	if got := l.Stack("web", 0); len(got) != 1 {
		t.Fatalf("in-memory record lost: %d", len(got))
	}
	// Nothing to assert on disk — just that Record/query work with no file.
}

func TestLog_CompactsSoDiskStaysBounded(t *testing.T) {
	dir := t.TempDir()
	l := newLog(dir, 2)
	for i := 1; i <= 40; i++ {
		l.Record(terminalEvent("web", events.StatusSuccess, at(i)))
	}
	data, err := os.ReadFile(filepath.Join(dir, auditFileName))
	if err != nil {
		t.Fatal(err)
	}
	lines := 0
	for _, ln := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(ln) != "" {
			lines++
		}
	}
	// cap is 2, so the file must stay bounded well below the 40 appends.
	if lines > 2*2+compactionSlack {
		t.Fatalf("file grew unbounded: %d lines for cap 2", lines)
	}
	// and the live view is still correct after compaction churn.
	if got := l.Stack("web", 0); len(got) != 2 || !got[0].Timestamp.Equal(at(40)) {
		t.Fatalf("post-compaction live state wrong: %+v", got)
	}
}
