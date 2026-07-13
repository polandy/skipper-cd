# Internal / Contributor Docs

Documentation for people **working on** skipper-cd, not for people **using** it.
These files deliberately live outside `docs/` so they are **not** published to the
[user manual](https://polandy.github.io/skipper-cd/) — read them here on GitHub.

- [`adr/`](adr/) — Architecture Decision Records: the design decisions and their
  rationale, one file per decision.
- [`local-ci.md`](local-ci.md) — run the whole CI pipeline locally via the nix
  dev shell + `make ci`.
- [`e2e-tests.md`](e2e-tests.md) — authoritative spec for the end-to-end tests.
- [`service-icons-spec.md`](service-icons-spec.md) — feature spec for service icons.

The user-facing reference (configuration, deploying, autosync, metrics, state) lives
in [`docs/`](../docs/) and is published as a MkDocs Material site.
