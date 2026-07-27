# Internal / Contributor Docs

Documentation for people **working on** skipper-cd, not for people **using** it.
These files deliberately live outside `docs/` so they are **not** published to the
[user manual](https://polandy.github.io/skipper-cd/) — read them here on GitHub.

- [`adr/`](adr/) — Architecture Decision Records: the design decisions and their
  rationale, one file per decision.
- [`local-ci.md`](local-ci.md) — run the whole CI pipeline locally via the nix
  dev shell + `make ci`.
- [`e2e-tests.md`](e2e-tests.md) — authoritative spec for the end-to-end tests.
- [`ui-preview.md`](ui-preview.md) — one-command seeded instance for manually
  eyeballing the web UI.
- [`ui-design-concept.md`](ui-design-concept.md) — shared row/table/panel
  design language across the UI's views.
- [`http-api-spec.md`](http-api-spec.md) — the read-only HTTP JSON API
  reference.
- [`autosync-spec.md`](autosync-spec.md) — autosync and the leave-dirty queue.
- [`stack-roster-spec.md`](stack-roster-spec.md) — the Stacks tab inventory
  view.
- [`stack-discovery-spec.md`](stack-discovery-spec.md) — deriving the stack
  set from the deploy repo.
- [`multi-host-spec.md`](multi-host-spec.md) — multi-host federated UI
  read-data fan-in.
- [`deploy-hooks-spec.md`](deploy-hooks-spec.md) — pre-/post-deploy hooks.
- [`container-logs-spec.md`](container-logs-spec.md) — live container logs in
  the UI.
- [`zero-downtime-rollout-spec.md`](zero-downtime-rollout-spec.md) — opt-in
  canary rollout for Traefik-fronted stacks.
- [`traefik-app-links-spec.md`](traefik-app-links-spec.md) — app-link icons
  detected from Traefik labels.
- [`orphan-detection-spec.md`](orphan-detection-spec.md) — detecting compose
  projects skipper no longer manages.
- [`service-icons-spec.md`](service-icons-spec.md) — feature spec for service icons.
- [`sync-windows-spec.md`](sync-windows-spec.md) — discarded proposal, kept
  for reference only.
- [`sops-secrets-spec.md`](sops-secrets-spec.md) — undecided proposal for
  SOPS-encrypted env files.

`../tools/design-cards/` generates the preview cards for the companion
"skipper-cd Design System" project on claude.ai — see its own README.

The user-facing reference (configuration, deploying, autosync, metrics, state) lives
in [`docs/`](../docs/) and is published as a MkDocs Material site.
