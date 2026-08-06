# TODO

- **A heavier small-size variant of the ship mark.** The header logo doubles as the
  favicon (`faviconSVG` in `internal/ui/theme.go`), but its 3.5 px hull and the
  half-opacity wave turn to mush at 16–24 px. Draw a simplified variant — no wave,
  thicker hull and boxes, geometry pushed outward — and use it for the favicon and
  the PWA icons while the primary mark keeps the header. (The homelab `branding/`
  set solves the same problem the same way with its separate `favicon.svg`.)

- **Per-instance logo override?** Open question, needs an ADR if pursued: a
  `ui_branding.logo`/`favicon` pointing at an SVG on disk would let a deployment
  carry its own mark. It breaks ADR-0035's everything-ships-in-the-binary rule, so
  it is only worth it if swapping the *mark* — not just the palette — is actually
  wanted. The `flake` palette already covers the colour half.
