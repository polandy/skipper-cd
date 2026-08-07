# TODO

- **UI branding: adopt the homelab "Flake" identity.** Apply the mark (lambda in a
  hexagon) to the web UI: favicon and header logo, plus the accent palette where it
  fits the existing design (Nix blue `#5277C3`/`#7EBAE4`; LED green `#3DDC84` dark /
  `#1FA864` light as accent; amber/red remain status-only). Source SVGs and usage
  rules live in the nixos repo under `branding/` (see `branding/README.md` there);
  use `favicon.svg` (heavier stroke) for anything rendered at 16-24 px. Per repo
  convention, update `internal/ui/UI_SPEC.md` first, then implement.
