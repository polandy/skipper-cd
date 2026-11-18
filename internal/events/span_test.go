package events

import (
	"testing"
	"time"
)

func TestSpanAbsorbInto(t *testing.T) {
	base := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	at := func(minutes int) time.Time { return base.Add(time.Duration(minutes) * time.Minute) }

	tests := []struct {
		name string
		prev Span // the standing run
		next Span // the single new occurrence
		want Span
	}{
		{
			name: "first repeat starts the run at two",
			prev: Span{Latest: at(0)},
			next: Span{Latest: at(5)},
			want: Span{Count: 2, FirstSeen: at(0), Latest: at(5)},
		},
		{
			name: "further repeat grows the count and keeps the start",
			prev: Span{Count: 3, FirstSeen: at(0), Latest: at(10)},
			next: Span{Latest: at(15)},
			want: Span{Count: 4, FirstSeen: at(0), Latest: at(15)},
		},
		{
			name: "a count of one is a single occurrence, not a run",
			prev: Span{Count: 1, Latest: at(0)},
			next: Span{Latest: at(5)},
			want: Span{Count: 2, FirstSeen: at(0), Latest: at(5)},
		},
		{
			// A reloaded log can hold occurrences out of order, so the run must
			// keep the oldest start and the newest recurrence either way.
			name: "an occurrence older than the run moves the start, not the latest",
			prev: Span{Count: 2, FirstSeen: at(10), Latest: at(20)},
			next: Span{Latest: at(5)},
			want: Span{Count: 3, FirstSeen: at(5), Latest: at(20)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.next.AbsorbInto(tt.prev)
			if got != tt.want {
				t.Errorf("AbsorbInto = %+v, want %+v", got, tt.want)
			}
		})
	}
}
