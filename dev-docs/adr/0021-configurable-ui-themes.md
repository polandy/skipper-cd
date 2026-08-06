# ADR-0021: Configurable UI themes, with a per-browser override

Status: accepted
Date: 2026-07-13

## Context

The web UI shipped with a single fixed palette: Catppuccin, Mocha (dark) and
Latte (light), with a header toggle for dark/light persisted per browser.

Operators running more than one skipper-cd instance (e.g. one per host) had no
way to tell them apart at a glance beyond the version label in the header —
easy to miss, and identical across tabs when several instances are open side
by side.

## Decision

### A configured, per-deployment palette

`ui_theme` (`skipper.yml`) selects one of five built-in palettes — `catppuccin`
(default), `nord`, `solarized`, `gruvbox`, `rose-pine` — each with its own
dark/light variant. `config.Load` defaults and validates it against
`uitheme.ValidThemes`.

The chosen theme is baked into `<html data-theme="…">` once, at handler
construction time (`ui.IndexHandler`, mirroring how `ServiceWorkerHandler`
bakes `__VERSION__` into `sw.js`) — not fetched or applied by JS. This means it
can never flash: it is present in the very first bytes of HTML.

### One semantic layer, five palettes

Every theme defines the same raw token set (`--crust/--mantle/--base/--surface0`
depth ramp, `--overlay1/--overlay2/--subtext0/--text` text tiers, six
`--raw-*` accent colours) under `:root[data-theme="<name>"]` /
`:root[data-theme="<name>"].light`. A single semantic bridge (`--accent`,
`--success`, `--bg-deep`, …) maps these to component-facing tokens, unchanged
since before this ADR. No component rule ever names a theme or a raw colour —
adding a sixth theme is purely a new CSS block plus a `themeIdentity` entry in
`internal/ui/theme.go`, nothing else in the page changes.

### Dark/light stays a separate, per-browser axis

The existing toggle is generalized (`localStorage` key renamed `theme` →
`colorScheme`, values `mocha`/`latte` → `dark`/`light`; CSS class `.latte` →
`.light`) but its job is unchanged: flip dark/light *within* whichever theme is
active. It never selects a theme.

### PWA identity follows the theme too

The favicon (an inlined SVG, generated in Go from a small per-theme
`themeIdentity` — it has no access to the page's CSS custom properties) and
the PWA meta/manifest colours (`theme-color` metas, `manifest.webmanifest`
`theme_color`/`background_color`) are now theme-aware, computed the same way
as the page CSS. This was in scope because it directly serves the goal:
distinguishing instances by browser tab and installed-app icon, not just
inside the page.

### A per-browser override, on top of the config — opt-in

Beyond picking a theme in `skipper.yml`, an **opt-in** header **theme picker**
(`<select>`, enabled by `ui_theme_switcher: true`) lets a single browser view a
different palette than the one configured — instantly, no reload, since every
theme's CSS is always present and switching is just changing the `data-theme`
attribute.

The switcher defaults to **off**. The whole point of a per-instance palette is
to be a reliable at-a-glance marker (which host am I looking at?), and a picker
that any visitor can flip — persisted per browser — would quietly undermine
that. So the default keeps the deployed theme fixed, and the picker exists
mainly to *try palettes out* before committing one to the config. The flag is
baked into `<html data-theme-switcher="on|off">` at serve time; with it off the
CSS hides the `<select>` and the pre-paint/picker JS ignores any saved
`themeOverride`, so a stale override simply lies dormant until it is re-enabled
(never silently overriding a locked-down deployment). This never touches
the server: `<html>` carries a second, immutable `data-server-theme` attribute
(the actual configured value, set once and never mutated by JS) purely as the
comparison reference.

The override is `localStorage`-only (`themeOverride`). Choosing the
configured theme again clears it, so the page resumes following whatever
`ui_theme` says — including transparently picking up a later config change.
Whenever an override is active, a small dismissible notice explains the
discrepancy ("Showing X in this browser — this environment is configured for
Y") and auto-hides after a few seconds, so a stale local preference is never a
silent trap when someone forgets they set it. Full behaviour:
[`internal/ui/UI_SPEC.md`](https://github.com/polandy/skipper-cd/blob/main/internal/ui/UI_SPEC.md#theme-override).

## Consequences

- Multiple instances can look visibly different at a glance (browser tab,
  installed PWA icon, and the page itself) without any change to behaviour —
  purely cosmetic, no new API surface beyond the existing static HTML/manifest.
- Renaming `latte`/`mocha` to `light`/`dark` is a breaking change for the E2E
  visual-snapshot baselines' *names* (not their pixels — Catppuccin's own
  colours are untouched); `ud-chrome.spec.ts` and the corresponding
  `__screenshots__` files were renamed together with this change.
- A theme is only ever removed by deleting its CSS block, its
  `themeIdentity` entry, and its name from `uitheme.ValidThemes` — the three stay
  in lockstep by construction (`config.validateConfig` rejects anything not in
  `uitheme.ValidThemes`, and `ui.IndexHandler`/`ManifestHandler` fall back to
  Catppuccin for a name absent from `themeIdentities`, which cannot happen for
  a config that passed validation).
- The per-browser override intentionally has no server-side counterpart (no
  endpoint, no persistence beyond `localStorage`) — it is a personal viewing
  preference, not a deployment setting, by design.

## Amendment (2026-08-06): a sixth palette, and the lockstep is now tested

`flake` joins the five palettes named above, added through exactly the
mechanism this ADR predicted — one CSS block per variant plus a
`themeIdentity` entry — so the decision itself is unchanged. Two notes the
addition surfaced:

The consequence above claims the three places a theme lives stay "in lockstep
by construction". That held for two of them: `config.validateConfig` rejects a
name outside `uitheme.ValidThemes`, and `themeIdentities` has a Catppuccin
fallback. The **stylesheet** had no such guard — a name in `ValidThemes` whose
`:root[data-theme="…"]` block is missing passes validation and serves a page
styled by the bare-`:root` fallback, silently wearing Catppuccin's colours
under another name's label. `internal/ui/theme_test.go` now asserts all three
sides against `ValidThemes`, including the per-host `--host-*` slots and the
picker's `<option>` list, so the gap is closed by test rather than by claim.

A palette may also need its accent to be a *different colour* per variant
rather than the same one retuned, when the source palette's accent cannot
clear contrast on both grounds — `flake` is the first to do so. Nothing in the
token design prevents this; it is worth naming because the other five made the
opposite choice and it could otherwise read as an inconsistency.
