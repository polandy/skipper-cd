package command

import "bytes"

// LineSink receives one line of child-process output. Implementations must
// be safe for concurrent use: a command's stdout and stderr are written
// concurrently. stack is the deploy stack the command runs for when the caller
// set it via WithStack (deploy hooks do, ADR-0038); it is empty for docker/git
// output, which the runner cannot attribute to a stack.
type LineSink interface {
	ChildLine(cmd, stream, line, stack string)
}

// maxLineLength caps how many bytes accumulate without a newline before
// the buffered chunk is emitted as a line anyway, bounding memory against
// binary or progress-bar output.
const maxLineLength = 8 * 1024

// lineWriter buffers written bytes and emits complete lines to a LineSink.
// Write never returns an error: io.MultiWriter aborts the whole write on
// the first error, and a sink problem must not fail the child command.
// It is not safe for concurrent use; each stream gets its own writer.
type lineWriter struct {
	sink   LineSink
	cmd    string
	stream string
	stack  string // deploy stack for attribution (empty for unattributed output)
	buf    bytes.Buffer
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf.Write(p)
	for {
		line, rest, found := bytes.Cut(w.buf.Bytes(), []byte("\n"))
		if !found {
			break
		}
		w.emit(line)
		remainder := append([]byte(nil), rest...)
		w.buf.Reset()
		w.buf.Write(remainder)
	}
	for w.buf.Len() >= maxLineLength {
		w.emit(w.buf.Next(maxLineLength))
	}
	return len(p), nil
}

// Close flushes a trailing unterminated line. Run calls it after the child
// exits.
func (w *lineWriter) Close() error {
	if w.buf.Len() > 0 {
		w.emit(w.buf.Bytes())
		w.buf.Reset()
	}
	return nil
}

func (w *lineWriter) emit(line []byte) {
	line = bytes.TrimSuffix(line, []byte("\r"))
	w.sink.ChildLine(w.cmd, w.stream, string(line), w.stack)
}
