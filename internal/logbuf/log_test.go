package logbuf

import (
	"sync"
	"testing"
	"time"
)

// appendN appends n entries with messages "msg-0" … "msg-(n-1)".
func appendN(l *Log, n int) {
	for i := range n {
		l.Append(time.Now(), "INFO", "msg-"+string(rune('0'+i%10)), nil)
	}
}

func TestLog_AppendAssignsIncreasingIDs(t *testing.T) {
	l := New(10)
	l.Append(time.Now(), "INFO", "first", nil)
	l.Append(time.Now(), "INFO", "second", nil)

	entries := l.Entries()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[1].ID <= entries[0].ID {
		t.Errorf("expected increasing IDs, got %d then %d", entries[0].ID, entries[1].ID)
	}
}

func TestLog_FirstIDIsSeededAboveZero(t *testing.T) {
	before := time.Now().UnixMilli()
	l := New(10)
	l.Append(time.Now(), "INFO", "first", nil)

	got := l.Entries()[0].ID
	if got < before {
		t.Errorf("expected first ID >= %d (UnixMilli seed), got %d", before, got)
	}
}

func TestLog_EvictsOldestBeyondCapacity(t *testing.T) {
	l := New(3)
	for _, msg := range []string{"a", "b", "c", "d", "e"} {
		l.Append(time.Now(), "INFO", msg, nil)
	}

	entries := l.Entries()
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	for i, want := range []string{"c", "d", "e"} {
		if entries[i].Msg != want {
			t.Errorf("entry %d: expected msg %q, got %q", i, want, entries[i].Msg)
		}
	}
}

func TestLog_EntriesReturnsCopyInOrder(t *testing.T) {
	l := New(10)
	l.Append(time.Now(), "INFO", "a", nil)
	l.Append(time.Now(), "WARN", "b", nil)

	entries := l.Entries()
	entries[0].Msg = "mutated"

	if got := l.Entries()[0].Msg; got != "a" {
		t.Errorf("internal state mutated through returned slice: got %q", got)
	}
}

func TestLog_EntriesAfterIDReturnsOnlyNewer(t *testing.T) {
	l := New(10)
	l.Append(time.Now(), "INFO", "a", nil)
	l.Append(time.Now(), "INFO", "b", nil)
	l.Append(time.Now(), "INFO", "c", nil)

	all := l.Entries()
	after := l.EntriesAfter(all[0].ID)
	if len(after) != 2 {
		t.Fatalf("expected 2 entries after ID %d, got %d", all[0].ID, len(after))
	}
	if after[0].Msg != "b" || after[1].Msg != "c" {
		t.Errorf("unexpected entries: %+v", after)
	}

	if got := l.EntriesAfter(all[2].ID); len(got) != 0 {
		t.Errorf("expected no entries after the newest ID, got %d", len(got))
	}
}

func TestLog_SubscribeReceivesLiveEntries(t *testing.T) {
	l := New(10)
	ch, unsub := l.Subscribe()
	defer unsub()

	l.Append(time.Now(), "ERROR", "boom", map[string]string{"stack": "gitea"})

	select {
	case got := <-ch:
		if got.Level != "ERROR" || got.Msg != "boom" || got.Attrs["stack"] != "gitea" {
			t.Errorf("unexpected entry: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for entry")
	}
}

func TestLog_UnsubscribeStopsDelivery(t *testing.T) {
	l := New(10)
	ch, unsub := l.Subscribe()
	unsub()

	l.Append(time.Now(), "INFO", "after unsub", nil)

	select {
	case <-ch:
		t.Error("received entry after unsubscribe")
	default:
		// expected: no entry delivered
	}
}

func TestLog_SlowSubscriberDropsInsteadOfBlocking(t *testing.T) {
	l := New(600)
	ch, unsub := l.Subscribe()
	defer unsub()

	// Overfill the subscriber buffer (size 256); Append must not block.
	appendN(l, 300)

	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			if count != 256 {
				t.Errorf("expected 256 buffered entries, got %d", count)
			}
			return
		}
	}
}

func TestLog_ChildLineRecordsCmdAndStreamAttrs(t *testing.T) {
	l := New(10)
	l.ChildLine("docker", "stderr", "Pulling image...", "")

	entries := l.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Level != "INFO" {
		t.Errorf("expected level INFO, got %q", e.Level)
	}
	if e.Msg != "Pulling image..." {
		t.Errorf("unexpected msg %q", e.Msg)
	}
	if e.Attrs["cmd"] != "docker" || e.Attrs["stream"] != "stderr" {
		t.Errorf("unexpected attrs: %+v", e.Attrs)
	}
	// Unattributed (docker/git) output carries no stack attr.
	if _, ok := e.Attrs["stack"]; ok {
		t.Errorf("docker output must not carry a stack attr: %+v", e.Attrs)
	}
}

// A deploy hook's output is attributed to its stack (ADR-0038), so the log view
// prefixes it [stack] and the stack filter matches it.
func TestLog_ChildLineAttributesHookOutputToStack(t *testing.T) {
	l := New(10)
	l.ChildLine("sh", "stdout", "Backup Successful", "nextcloud")

	e := l.Entries()[0]
	if e.Attrs["stack"] != "nextcloud" {
		t.Errorf("expected stack attr nextcloud, got %+v", e.Attrs)
	}
	if e.Time.IsZero() {
		t.Error("expected a non-zero timestamp")
	}
}

func TestLog_ConcurrentAppendAndSubscribeIsRaceFree(t *testing.T) {
	l := New(50)

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch, unsub := l.Subscribe()
			defer unsub()
			appendN(l, 20)
			for {
				select {
				case <-ch:
				default:
					return
				}
			}
		}()
	}
	wg.Wait()
}
