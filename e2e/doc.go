// Package e2e contains end-to-end tests that run the real skipper binary
// against a local git origin and a stub docker on PATH. They are guarded by
// the "e2e" build tag so the default `go test ./...` stays fast and needs no
// docker daemon; run them explicitly with:
//
//	go test -tags e2e ./e2e
//
// The full scope and test catalog live in docs/e2e-tests.md.
package e2e
