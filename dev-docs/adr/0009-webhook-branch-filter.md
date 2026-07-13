# ADR-0009: Webhook filters pushes by branch ref

Status: accepted
Date: 2026-07-09

## Context

Any push with a valid HMAC signature triggered a full sync-and-deploy run,
including pushes to feature branches. Those runs were harmless (the sync
resets to the configured branch, hashes then show no change) but wasted
work and produced confusing logs.

## Decision

The webhook handler parses the push payload's `ref` field and only triggers
a deploy when it equals `refs/heads/<configured branch>`. Other refs are
acknowledged with HTTP 200 ("ignoring push to …"). Payloads that are not
JSON or carry no `ref` (manual `curl`, other event types) trigger a deploy —
when in doubt, deploying is the safe direction because unchanged stacks are
skipped anyway.

## Consequences

- Feature-branch pushes no longer cause sync runs or log noise.
- Manual triggering via `curl -X POST /webhook` keeps working.
- Only the `ref` field is interpreted; everything else in the payload stays
  ignored on purpose (no coupling to Gitea/GitHub payload schemas).
