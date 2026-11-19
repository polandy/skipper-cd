package main

import (
	"context"
	"errors"
	"testing"
)

func TestBgLoops_WaitReturnsOnceEveryLoopReturned(t *testing.T) {
	var b bgLoops
	release := make(chan struct{})
	returned := make(chan struct{})
	b.Go("blocked", func() { <-release; close(returned) })

	close(release)
	if err := b.Wait(context.Background()); err != nil {
		t.Fatalf("Wait = %v, want nil", err)
	}
	select {
	case <-returned:
	default:
		t.Fatal("Wait returned before the loop did")
	}
}

func TestBgLoops_WaitGivesUpWhenContextEnds(t *testing.T) {
	var b bgLoops
	release := make(chan struct{})
	defer close(release)
	b.Go("stuck", func() { <-release })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := b.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait = %v, want context.Canceled", err)
	}
}

func TestBgLoops_WaitCountsAPanickingLoopAsReturned(t *testing.T) {
	var b bgLoops
	b.Go("panics", func() { panic("boom") })

	if err := b.Wait(context.Background()); err != nil {
		t.Fatalf("Wait = %v, want nil", err)
	}
}
