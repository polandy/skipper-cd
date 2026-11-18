package safego_test

import (
	"sync"
	"testing"

	"github.com/polandy/skipper-cd/internal/safego"
)

func TestGo_RunsFunctionToCompletion(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	called := false

	safego.Go("test", func() {
		defer wg.Done()
		called = true
	})

	wg.Wait()
	if !called {
		t.Error("function was not called")
	}
}

func TestGo_RecoversPanicWithoutCrashingProcess(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)

	safego.Go("test-panic", func() {
		defer wg.Done()
		panic("boom")
	})

	wg.Wait() // if the panic were not recovered, the test binary would crash here
}

func TestGo_PanicDoesNotStopOtherGoroutines(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(2)
	ranAfterPanic := false

	safego.Go("panics", func() {
		defer wg.Done()
		panic("boom")
	})
	safego.Go("keeps running", func() {
		defer wg.Done()
		ranAfterPanic = true
	})

	wg.Wait()
	if !ranAfterPanic {
		t.Error("a panic in one safego.Go call must not prevent another from running")
	}
}
