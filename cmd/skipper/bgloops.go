package main

import (
	"context"
	"sync"

	"github.com/polandy/skipper-cd/internal/safego"
)

// bgLoops tracks the ctx-bound background loops so shutdown can join them:
// a loop's command is reaped only when the loop returns, so exiting earlier
// can leave a `docker` child running past skipper.
type bgLoops struct {
	wg sync.WaitGroup
}

// Go runs fn through safego.Go and counts it until it returns.
func (b *bgLoops) Go(name string, fn func()) {
	b.wg.Add(1)
	safego.Go(name, func() {
		defer b.wg.Done()
		fn()
	})
}

// Wait blocks until every loop has returned or ctx ends, and reports ctx's
// error in the latter case.
func (b *bgLoops) Wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() { b.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
