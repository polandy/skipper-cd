# ADR-0070: Secrets are masked out of events and the UI log

Date: 2026-11-19

## Status

Accepted. Extends [ADR-0063](0063-failure-event-names-the-cause.md).

## Context

ADR-0063 put a failed command's stderr into the deploy error. That text reaches
the event stream, the history, the audit log and notifications, and the slog
line next to it, like every child process's output, is teed into the in-memory
log served at `/api/logs`. Neither surface needs authentication. A hook that
echoes `$DB_PASSWORD`, a compose error quoting an interpolated value, or git
printing a credentialed remote URL therefore published the secret. Env-file
diffs were already masked (`maskEnvDiff`), but by syntax only: nothing knew the
values themselves.

## Decision

`internal/redact.Secrets` is one process-wide set of secret values plus a rule
for URL credentials. It is applied where text leaves skipper:

- `Deployer.emit` masks the event's error and diffs, which covers history,
  audit, notifications and the UI events alike.
- `logbuf.Log` masks every entry's message and attr values, slog records and
  child-process lines alike, before it is stored or streamed.

The set is filled where env is resolved for a compose call, hook or log read
(`Deployer.resolveEnv`): the vars file, the stack's env files and the process
environment. A value is registered only when its key names a secret (`PASS`,
`SECRET`, `TOKEN`, `KEY`, `CRED`, `AUTH`, `PRIVATE`, `SALT`, `DSN`, any case)
and it is at least 4 characters long. Values are never removed, so a rotated
secret stays masked for the life of the process. URL userinfo in any text is
masked the way `git.RedactURL` masks `repo_url`: a password always, a lone
username (an access token) except on `ssh://`.

## Consequences

- The console (journal) keeps the full text. It is the operator's own surface,
  readable only by those who can read the env files anyway, and the one place
  left to debug a masked line.
- A secret under a key the pattern misses (`DB=postgres://…` aside, whose
  password the URL rule catches) is not masked.
- A secret is masked only after the first run read it, so output from before
  that (startup, the first sync) is covered by the URL rule alone.
- `/api/container-logs` streams the containers' own output and is not masked;
  restricting who can read it is an access question (authentication), not this
  one.
- The clone's `.git/config` still holds a credentialed `repo_url`. It lives in
  skipper's private state directory; the leak this ADR closes is the output path.

## Alternatives considered

- **Mask every env-file value**, as the diff does. Rejected: env files also
  carry ports, image tags and hostnames, and masking `1.2.3` or `postgres`
  wherever it appears would mangle exactly the error lines ADR-0063 added.
- **Redact in `withFailureCause` only.** Rejected: the same text reaches the log
  through the slog line and the child-output tee, and a hook's stdout never
  passes through the failure cause.
- **A git credential helper instead of a token in the URL.** Not needed for this
  leak: the URL rule masks the token wherever git prints it.
