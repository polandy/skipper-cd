// Package containerlogs serves a stack's `docker compose logs` to the UI —
// whole stack or a selected subset of its services: an initial backlog then a
// live follow, streamed as Server-Sent Events. It exists only when the UI is
// enabled. See dev-docs/adr/0037-container-logs-in-ui.md.
package containerlogs

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// dockerBin is the compose driver; skipper always shells out to the docker CLI.
const dockerBin = "docker"

// Tail bounds: the backlog size, clamped so a client cannot request an
// unbounded read.
const (
	defaultTail = 200
	minTail     = 1
	maxTail     = 1000
)

// Invocation is how to run docker compose against a stack: the working
// directory, the environment, and the leading `compose …` args that select the
// project. It mirrors the deploy path exactly (Invariant 1) so a logs read
// targets the same running project a deploy would.
type Invocation struct {
	Dir  string
	Env  []string
	Args []string
}

// Resolver supplies, for a currently-known stack, its docker compose Invocation
// and the stack's known service names (for validation). ok is false when the
// stack is not currently known — never deployed or not in the discovered set.
// It abstracts the Deployer and health poller so the handler stays testable.
type Resolver interface {
	Resolve(stack string) (inv Invocation, services []string, ok bool, err error)
}

// Handler serves GET /api/container-logs/{stack}. With no ?service= it streams
// the whole stack (services merged, each line labelled); one or more repeated
// ?service= params narrow the stream to that subset. It streams the backlog
// then the live follow as SSE. One log streams per request; the UI keeps at
// most one open, so a viewer holds at most one follow child — a disconnect
// cancels the request context, killing that child.
func Handler(streamer LogStreamer, resolver Resolver) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		stack := r.PathValue("stack")
		selected := r.URL.Query()["service"] // repeatable; empty = whole stack

		inv, services, known, err := resolver.Resolve(stack)
		if err != nil {
			http.Error(w, "cannot resolve stack", http.StatusInternalServerError)
			return
		}
		if !known {
			http.NotFound(w, r)
			return
		}
		for _, s := range selected {
			if !contains(services, s) {
				http.NotFound(w, r)
				return
			}
		}

		args := logsArgs(inv.Args, selected, tailParam(r), sinceParam(r))

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		// A failed frame write means the client went away; cancel so the streamer
		// stops reading (and its docker child is killed) instead of writing on.
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		// Headers are already sent, so an error here can't change the status;
		// log it unless it is the expected cancellation on client disconnect.
		if err := streamer.Stream(ctx, inv.Dir, inv.Env, dockerBin, args, func(line string) {
			if _, err := fmt.Fprintf(w, "data: %s\n\n", line); err != nil {
				cancel()
				return
			}
			flusher.Flush()
		}); err != nil && ctx.Err() == nil {
			slog.Warn("container logs stream ended with error", "stack", stack, "services", selected, "err", err)
		}
	})
}

// logsArgs builds the compose argv: the project-selecting prefix, then the logs
// subcommand. Exactly one selected service drops the compose service prefix
// (--no-log-prefix) since the scope is unambiguous; the whole-stack and
// multi-service views keep it so each line stays labelled. Selected services
// are appended as trailing args (none = whole stack). A valid since resumes
// after a reconnect and replaces the backlog tail.
func logsArgs(projectArgs []string, services []string, tail int, since string) []string {
	args := append([]string(nil), projectArgs...)
	args = append(args, "logs", "--no-color", "--timestamps", "--follow")
	if len(services) == 1 {
		args = append(args, "--no-log-prefix")
	}
	if since != "" {
		args = append(args, "--since", since)
	} else {
		args = append(args, "--tail", strconv.Itoa(tail))
	}
	return append(args, services...)
}

// tailParam reads ?tail, clamped to [minTail, maxTail]; absent or unparseable
// yields defaultTail.
func tailParam(r *http.Request) int {
	v := r.URL.Query().Get("tail")
	if v == "" {
		return defaultTail
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return defaultTail
	}
	if n < minTail {
		return minTail
	}
	if n > maxTail {
		return maxTail
	}
	return n
}

// sinceParam reads ?since, accepting only an RFC3339 timestamp; anything else
// is dropped so the request falls back to a tail backlog.
func sinceParam(r *http.Request) string {
	v := r.URL.Query().Get("since")
	if v == "" {
		return ""
	}
	if _, err := time.Parse(time.RFC3339, v); err != nil {
		return ""
	}
	return v
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
