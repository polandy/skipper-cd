package containerlogs

import (
	"bufio"
	"context"
	"io"
	"os/exec"
	"strings"
	"sync"
)

// maxLogLine caps a single scanned line, guarding against binary or
// progress-bar output that never emits a newline.
const maxLogLine = 256 * 1024

// LogStreamer runs name+args in dir with env and delivers each output line to
// onLine until the process exits or ctx is cancelled. It is the seam that lets
// tests inject canned output instead of executing docker; ExecStreamer is the
// real implementation.
type LogStreamer interface {
	Stream(ctx context.Context, dir string, env []string, name string, args []string, onLine func(line string)) error
}

// ExecStreamer is the real LogStreamer: it runs the command via os/exec and
// scans stdout and stderr line by line. Cancelling ctx kills the child
// (exec.CommandContext), so a disconnected client leaves no orphaned
// `docker compose logs --follow` process behind.
type ExecStreamer struct{}

// Stream implements LogStreamer.
func (ExecStreamer) Stream(ctx context.Context, dir string, env []string, name string, args []string, onLine func(string)) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = env

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	// stdout and stderr are scanned concurrently; a mutex serializes onLine so
	// the caller (which writes to one ResponseWriter) never sees interleaving.
	var mu sync.Mutex
	emit := func(s string) {
		mu.Lock()
		onLine(s)
		mu.Unlock()
	}
	var wg sync.WaitGroup
	scan := func(r io.Reader) {
		defer wg.Done()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), maxLogLine)
		for sc.Scan() {
			emit(strings.TrimRight(sc.Text(), "\r"))
		}
	}
	wg.Add(2)
	go scan(stdout)
	go scan(stderr)
	wg.Wait()

	return cmd.Wait()
}
