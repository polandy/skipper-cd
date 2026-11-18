# ADR-0059: Per-service change attribution

- Status: Accepted
- Date: 2026-09-06

## Context

A deploy row names which service moved only when an **image** moved (ADR-0053). Everything else — the case that fills the log — arrives as a file count.

The trigger was a fleet-wide change: a backup sidecar in every stack's compose file gained two environment variables. Every stack redeployed, and every row said `1 file`. Nothing in the UI could say that only the sidecar was touched and the application containers were restarted for a change that never reached them. Opening the diff answers it, one row at a time; a log of twenty such rows does not.

skipper hashes six kinds of input per stack (Invariant 2). They do not have the same blast radius:

| Input | Reaches |
| --- | --- |
| `docker-compose.yml` | the services whose block changed |
| a `build:` service's Dockerfile | the services building from it |
| a stack `env_files` entry | every service (compose passes it as `--env-file`) |
| the global `vars_file` | every service |
| a `watch_dirs` file | unknown — an opaque directory skipper hashes |
| the stack's deploy-shaping config | every service |

Only the first two can name a container. The rest genuinely reach all of them.

## Decision

Every terminal deploy event carries `file_changes`: one entry per changed file with its **kind** and the **services it reached**. The UI renders it as a chip per kind in the row's Changes column and as a per-file note in the files/diff panel.

Compose attribution is resolved by **comparing the previously deployed revision of the file against the current one, service block by service block** (`git show <last_deployed_commit>:<path>`, both revisions parsed into `map[string]yaml.Node`, each block compared by re-encoding). Anything outside the service blocks — the top-level keys, an `x-` anchor a service only aliases — is compared as one unit; when it differs, the change is reported as reaching every service rather than being pinned to blocks that only look unchanged because they alias what moved.

The result is three distinct states, not two:

- **services named** — those containers;
- **`wide`** — reaches every service, or could not be resolved (no previous commit, an unreadable or unparseable revision, a change outside the service blocks);
- **neither** — the comparison ran and no service definition changed at all (a comment or a reordering). No container is blamed for it.

Attribution is **display only**. It is not hashed, not a pull input, and never a deploy trigger — nothing about what gets deployed changes.

## Alternatives considered

**Map diff hunk lines to service blocks.** The obvious route: skipper already has the unified diff, and a YAML node tree gives each service's line range. Rejected — the mapping is brittle in exactly the cases that matter. Hunk headers describe one side of the diff, so an added block shifts every line number after it; the stored diff is truncated at 10 KB per file, silently dropping later hunks; and a line inside a hunk's context is not a change at all. The revision comparison answers the semantic question directly and cannot be off by a line.

**Attribute env files per service from compose `env_file:`.** Tempting, and it was in the mockup: a compose service listing `.env.restic` clearly reads it. Rejected because it would be wrong. skipper passes a stack's `env_files` to compose as `--env-file`, which applies to the **whole project** — the variables reach every service regardless of who lists the file. Naming two of five services would be a confident lie; `stack-wide` is the true answer. (Per-service compose `env_file:` entries are not skipper-hashed inputs at all, so they never appear in a change set on their own.)

**A `services` column of its own.** Rejected on the [design language](../ui-design-concept.md)'s budget rule: the Changes column already answers "what moved" for images with the same chip, and a second column would compete with it for the same glance while costing width the Stack name needs.

## Consequences

- The common case is answered without a click: `compose mealie-restic` on the row.
- Two suppressions keep the column quiet: a service already named by an image chip is dropped from the `compose` kind, and a stack-wide `compose` entry renders nothing — both would only repeat what the row already shows. A bump row therefore looks exactly as it did before this change.
- One extra `git show` per deploy of a stack whose compose file changed — the same call the rollback path already makes, on the same file.
- A stack's **first** deploy has no previous revision to compare against, so its compose change is `wide` (and, by the suppression above, renders no chip).
- Older events and peer rows carry no `file_changes` and simply show no change chips; the column and the panels degrade to what they showed before.
