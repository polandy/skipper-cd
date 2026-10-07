# ADR-0063: A failed command's event names the cause from its stderr

## Status

Accepted

## Context

A failed `build`, `pull`, `up` or deploy hook reached the event, the
notification and the audit log as its exit status only:
`docker compose build: exit status 1`. The cause was in the child's stderr,
which skipper tees into the journal and the log view but never kept.

In production a Renovate digest bump of the Nextcloud base image met an apt pin
that Debian no longer shipped. The build failed every five minutes for five
hours. Every event and the Grafana alert said `exit status 1`. The line that
explained it, `E: Unable to correct problems, you have held broken packages.`,
stood only in the journal, between thousands of layer-download progress lines.

Two constraints shape what the event can carry:

- The text must stay short. It is a notification body and a table cell, not a
  log.
- The same failure must yield the same text on every run. ADR-0056 collapses a
  repeat only when the error text is identical, and BuildKit prefixes each line
  of a step's output with its elapsed time (`#6 4.640 E: …`), which differs on
  every run.

## Decision

`command.ShellRunner.Run` keeps the last 64 lines a command wrote to stderr. A
failed command returns a `*command.ExitError` that carries them; its message is
unchanged, and `Unwrap` still reaches the `*exec.ExitError`.

The deploy layer condenses that tail into one line and appends it to the error
of every compose call and every hook (`withFailureCause`):

- ANSI escape sequences, carriage-return progress redraws, BuildKit's step
  prefix (`#6 `) and elapsed stamp (`4.640 `), and dash-only frame lines are
  removed.
- For a failed BuildKit step (`#N ERROR: …`) the cause is the step's **first**
  error-looking line plus BuildKit's verdict, with the quoted command cut to 60
  characters. The first line, not the last: apt and similar tools state the
  problem first and follow it with context lines that look the same.
- Otherwise it is the last error-looking line (`E:`, `ERROR`, `Error response
  from daemon`, `fatal:`, …), else the last line.
- Each part is capped at 160 characters.

The result reads, for the incident:

```
docker compose build: exit status 1: E: Unable to correct problems, you have held broken packages. — process "/bin/sh -c apt-get update && apt-get install -y ghostscript…" did not complete successfully: exit code: 100
```

## Consequences

- The UI row, the Signal notification and the audit record say why a deploy
  failed without a trip to the journal.
- Repeats still collapse: the parts that change between runs are removed before
  the text is built.
- The rule matches text, not structure. A tool whose error line looks like none
  of the patterns falls back to its last stderr line, which is still more than
  the exit status said. A BuildKit format change degrades the same way rather
  than failing.
- Only stderr is kept. A hook that reports its failure on stdout gets the exit
  status alone, as before.
- Error texts get longer. An alert or a log query that matched the exact old
  text (`…: exit status 1`) needs a prefix match.

## Alternatives considered

- **Keep the whole tail on the event.** The event, its persisted history and
  every notification would carry up to 64 lines of mostly progress output, and
  the timestamps in them would defeat ADR-0056.
- **Read the cause back from the journal.** It ties skipper to journald and
  still needs the same condensing.
