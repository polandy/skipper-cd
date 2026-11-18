# ADR-0047: Rename `health_check` to `deploy_health_check`, `health_poll_interval_seconds` to `runtime_health_poll_interval_seconds`

Status: accepted
Date: 2026-07-21

## Context

Four config keys share a "health" prefix but name unrelated mechanisms:
per-stack `health_check` is a one-shot deploy-time gate (`up --wait` + optional
HTTP probe, triggers rollback on failure, ADR-0022); global
`health_poll_interval_seconds` is the cadence of the continuous runtime
poller that feeds the UI's stack-health view (ADR-0027); `health_watch` is the
alerting watchdog built on that poller's feed (ADR-0031); `self_heal` is the
corrective-redeploy consumer of the same feed (ADR-0029). Only the first two
are actually confusable: `health_check` sounds like it could be the same
"health" the poller/watchdog/self-heal observe continuously, when it is in
fact a deploy-time-only gate that runs once per deploy and has no relationship
to the poll loop at all. `health_watch` and `self_heal` already name their own
distinct role and don't share this ambiguity, so they are unchanged.

## Decision

**Rename `health_check` to `deploy_health_check`** (`Stack.HealthCheck` →
`Stack.DeployHealthCheck`, YAML key `health_check` → `deploy_health_check`;
the `HealthCheck` struct *type* itself keeps its name — only the field that
holds it is renamed). The new name states plainly that this is a deploy-time
gate, contrasting with the `runtime_` family below.

**Rename `health_poll_interval_seconds` to
`runtime_health_poll_interval_seconds`** (`Config.HealthPollIntervalSeconds` →
`Config.RuntimeHealthPollIntervalSeconds`). The new name states plainly that
this cadence drives continuous runtime observation (UI health view, self-heal,
health_watch), not any single deploy.

Clean break, no alias — consistent with ADR-0043's and ADR-0045's precedent
(the only users are two single-admin homelab hosts, migrated atomically
alongside a host config edit + rebuild).

`deploy_health_check` remains part of a stack's `ConfigHash`
(`stackDeployInputs`, ADR-0043): the YAML key change means the canonical hash
input changes for every stack regardless of value — see Consequences.
`runtime_health_poll_interval_seconds` is a global field, not part of any
stack's `ConfigHash`, so its rename triggers no redeploy.

## Consequences

- `deploy_health_check` states what it gates (a deploy) and no longer risks
  being read as related to the continuous runtime poller;
  `runtime_health_poll_interval_seconds` states what it drives (continuous
  runtime observation) and no longer risks being read as deploy-time.
  `health_watch` and `self_heal` are untouched — they already name their own
  role unambiguously.
- **One-time redeploy of every stack on upgrade**, for the same reason as
  ADR-0045: `stackDeployInputs`'s `yaml:"health_check"` tag becoming
  `yaml:"deploy_health_check"` changes the canonically-marshaled hash input
  even for a stack whose value didn't change, so `Stack.ConfigHash` changes
  for every discovered stack on the first sync after upgrading. A one-time
  `docker compose up -d` per stack is the entire effect (idempotent, no
  downtime for an unchanged deploy) — accepted, not worth a migration shim for
  two single-admin hosts.
- **Migration required on both homelab hosts**: every `health_check:` /
  `health_poll_interval_seconds:` in host-a's and host-b's NixOS config must
  become `deploy_health_check:` / `runtime_health_poll_interval_seconds:`
  before or alongside the upgrade. A leftover `health_check:` silently stops
  gating the stack (falls back to the automatic compose-`healthcheck:` gate
  of ADR-0046, or ungated if the compose file has none) rather than erroring;
  a leftover `health_poll_interval_seconds:` silently falls back to the
  default 30s cadence rather than erroring — YAML has no way to flag an
  unknown field as fatal the way `LoadRepoStacks`' strict decoding does for
  the in-repo override file.
- ADR-0022, ADR-0027, ADR-0028, ADR-0029, ADR-0031, ADR-0032, ADR-0034,
  ADR-0038, ADR-0040, ADR-0043 and ADR-0046 keep their original `health_check`
  / `health_poll_interval_seconds` wording — ADRs are point-in-time decision
  records, not living docs; CLAUDE.md and `docs/` carry the current names.
