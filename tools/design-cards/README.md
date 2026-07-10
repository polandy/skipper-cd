# Design cards

Generates the preview cards for the **"skipper-cd Design System"** project on
[claude.ai/design](https://claude.ai) — the visual spec companion to
`internal/ui/UI_SPEC.md`.

```
python3 gen_ds.py     # writes 12 self-contained HTML cards to dist/
```

Each card is a standalone HTML file with a first-line
`<!-- @dsCard group="…" name="…" -->` marker (that marker is what the Design
System pane indexes) and shows the component in **Catppuccin Mocha** (dark
default) and **Latte** (light opt-in) side by side. Colours, component CSS and
the container-ship logo SVG are lifted from the real UI —
`internal/ui/static/index.html` is the single source of truth, the generator
re-expresses it as documentation.

After a UI change: regenerate, then upload the changed files from `dist/` to
the design project (Claude Code does this via its DesignSync tooling; the
project files mirror the `dist/` paths, e.g. `patterns/logs-view.html`).

`dist/` is generated output and gitignored.
