#!/usr/bin/env python3
"""Generate skipper-cd design-system cards — Catppuccin approach (Mocha default + Latte),
mirroring the JIT-Pack Design System card format: every card shows both palettes."""
import re, pathlib

ROOT = pathlib.Path(__file__).resolve().parents[2]
SRC = ROOT / "internal" / "ui" / "static" / "index.html"
OUT = pathlib.Path(__file__).resolve().parent / "dist"

# The header's inline container-ship SVG — uses currentColor and the semantic
# tokens, so it recolours per pane.
logo = re.search(r'<svg [^>]*aria-label="skipper-cd">.*?</svg>', open(SRC).read(), re.S).group(0)

MOCHA = dict(crust="#11111b", mantle="#181825", base="#1e1e2e", surface0="#313244",
    surface1="#45475a", surface2="#585b70", overlay0="#6c7086", overlay1="#7f849c",
    overlay2="#9399b2", subtext0="#a6adc8", subtext1="#bac2de", text="#cdd6f4",
    rosewater="#f5e0dc", flamingo="#f2cdcd", pink="#f5c2e7", mauve="#cba6f7",
    red="#f38ba8", maroon="#eba0ac", peach="#fab387", yellow="#f9e2af",
    green="#a6e3a1", teal="#94e2d5", sky="#89dceb", sapphire="#74c7ec",
    blue="#89b4fa", lavender="#b4befe", onaccent="#11111b", scheme="dark")
LATTE = dict(crust="#dce0e8", mantle="#e6e9ef", base="#eff1f5", surface0="#ccd0da",
    surface1="#bcc0cc", surface2="#acb0be", overlay0="#9ca0b0", overlay1="#8c8fa1",
    overlay2="#7c7f93", subtext0="#6c6f85", subtext1="#5c5f77", text="#4c4f69",
    rosewater="#dc8a78", flamingo="#dd7878", pink="#ea76cb", mauve="#8839ef",
    red="#d20f39", maroon="#e64553", peach="#fe640b", yellow="#df8e1d",
    green="#40a02b", teal="#179299", sky="#04a5e5", sapphire="#209fb5",
    blue="#1e66f5", lavender="#7287fd", onaccent="#eff1f5", scheme="light")

def palette_css(cls, P):
    vars_ = ";".join(f"--{k}:{v}" for k, v in P.items() if k not in ("onaccent", "scheme"))
    return f".{cls}{{{vars_};--on-accent:{P['onaccent']};color-scheme:{P['scheme']}}}"

BASE = f"""
*{{box-sizing:border-box;margin:0;padding:0}}
html{{-webkit-font-smoothing:antialiased}}
body{{background:#0d0d15;min-height:100vh;display:grid}}
body.side{{grid-template-columns:1fr 1fr}}
@media(max-width:920px){{body.side{{grid-template-columns:1fr}}}}
body.stack{{grid-template-columns:1fr}}
{palette_css('mocha', MOCHA)}
{palette_css('latte', LATTE)}
.pane{{
  --bg-deep:var(--mantle);--bg-base:var(--base);--bg-sunken:var(--crust);--bg-raised:var(--surface0);
  --border:color-mix(in srgb,var(--overlay2) 14%,transparent);
  --border-lit:color-mix(in srgb,var(--overlay2) 28%,transparent);
  --text-primary:var(--text);--text-secondary:var(--subtext0);--text-muted:var(--overlay1);
  --accent:var(--peach);--success:var(--teal);--danger:var(--red);
  --rollback:var(--maroon);--skip:var(--overlay1);--diff-add:var(--green);--hunk:var(--yellow);
  --accent-dim:color-mix(in srgb,var(--accent) 12%,transparent);
  --accent-glow:color-mix(in srgb,var(--accent) 30%,transparent);
  --success-glow:color-mix(in srgb,var(--success) 40%,transparent);
  --danger-glow:color-mix(in srgb,var(--danger) 40%,transparent);
  --font-ui:'DM Sans',-apple-system,'Segoe UI Variable Text','Segoe UI',Cantarell,'Noto Sans',sans-serif;
  --font-mono:'JetBrains Mono',ui-monospace,'Cascadia Code','SF Mono',Menlo,Consolas,monospace;
  --radius:8px;
  position:relative;overflow:hidden;min-width:0;
  background:var(--bg-deep);color:var(--text-primary);
  font:400 14px/1.5 var(--font-ui);padding:24px 28px 48px}}
.pane::before{{content:'';position:absolute;inset:0;pointer-events:none;
  background:linear-gradient(color-mix(in srgb,var(--overlay2) 7%,transparent) 1px,transparent 1px),
             linear-gradient(90deg,color-mix(in srgb,var(--overlay2) 7%,transparent) 1px,transparent 1px);
  background-size:48px 48px}}
.pane::after{{content:'';position:absolute;top:-200px;left:50%;transform:translateX(-50%);
  width:800px;height:400px;pointer-events:none;
  background:radial-gradient(ellipse,color-mix(in srgb,var(--accent) 8%,transparent) 0%,transparent 70%)}}
.pane>*{{position:relative;z-index:1}}
.pane-tag{{display:flex;justify-content:space-between;align-items:baseline;gap:12px;flex-wrap:wrap;
  margin-bottom:20px;padding-bottom:12px;border-bottom:1px solid var(--border)}}
.pane-tag b{{font-size:13px;font-weight:700;letter-spacing:.02em}}
.pane-tag span{{font:500 11px var(--font-mono);color:var(--text-muted)}}
.overline{{font-family:var(--font-mono);font-size:10px;font-weight:600;letter-spacing:1.2px;
  text-transform:uppercase;color:var(--text-muted)}}
.sec{{margin:28px 0 12px}}.sec:first-of-type{{margin-top:0}}
.mono{{font-family:var(--font-mono)}}
.muted{{color:var(--text-muted)}}
.dim2{{color:var(--text-secondary)}}
.card{{background:var(--bg-base);border:1px solid var(--border);border-radius:var(--radius)}}
.caption{{font-size:11.5px;color:var(--text-secondary);margin-top:8px}}
.row-flex{{display:flex;align-items:center;gap:14px;flex-wrap:wrap}}
@keyframes breathe{{0%,100%{{opacity:1;box-shadow:0 0 8px var(--accent-glow)}}50%{{opacity:.4;box-shadow:0 0 4px var(--accent-glow)}}}}
@keyframes expand{{from{{opacity:0;transform:translateY(-4px)}}to{{opacity:1;transform:translateY(0)}}}}
@keyframes rowEnter{{from{{opacity:0;transform:translateY(-12px) scale(.98)}}to{{opacity:1;transform:translateY(0) scale(1)}}}}
"""

# Component CSS — structure lifted from the real UI, colours re-expressed as Catppuccin tokens.
COMP = """
/* header */
.hdr{background:color-mix(in srgb,var(--bg-base) 82%,transparent);backdrop-filter:blur(20px) saturate(1.2);
  border:1px solid var(--border);border-radius:var(--radius);
  padding:0 28px;height:56px;display:flex;align-items:center;justify-content:space-between}
.brand{display:flex;align-items:center;gap:12px}
.brand-icon{width:32px;height:32px;color:var(--text-primary)}
.brand-icon svg{display:block;width:32px;height:32px}
.brand-name{font-family:var(--font-mono);font-size:15px;font-weight:600;color:var(--text-primary);letter-spacing:-.3px}
.brand-name span{color:var(--accent)}
.brand-tag{font-family:var(--font-mono);font-size:10px;font-weight:500;color:var(--text-muted);
  background:var(--bg-raised);border:1px solid var(--border);padding:2px 7px;border-radius:4px;
  letter-spacing:.5px;text-transform:uppercase;margin-left:4px}
.status-area{display:flex;align-items:center;gap:20px}
.indicator{display:flex;align-items:center;gap:8px;font-size:12px;font-weight:500;
  color:var(--text-secondary);font-family:var(--font-mono)}
.indicator-dot{width:7px;height:7px;border-radius:50%;background:var(--success);
  box-shadow:0 0 8px var(--success-glow);flex-shrink:0}
.indicator-dot.warn{background:var(--accent);box-shadow:0 0 8px var(--accent-glow);animation:breathe 2s ease-in-out infinite}
.indicator-dot.err{background:var(--danger);box-shadow:0 0 8px var(--danger-glow)}
.deploy-status{display:flex;align-items:center;gap:8px;font-size:12px;font-weight:500;
  font-family:var(--font-mono);color:var(--text-muted)}
.deploy-status.active{color:var(--accent)}
.deploy-status.active .deploy-dot{background:var(--accent);box-shadow:0 0 8px var(--accent-glow);animation:breathe 2s ease-in-out infinite}
.deploy-dot{width:7px;height:7px;border-radius:50%;background:var(--text-muted);flex-shrink:0}
/* toggle */
.filter-toggle{display:flex;align-items:center;gap:7px;font-size:12px;font-weight:500;
  font-family:var(--font-mono);color:var(--text-muted);cursor:pointer;user-select:none;
  background:none;border:none;padding:0}
.filter-toggle:hover{color:var(--text-secondary)}
.toggle-track{width:28px;height:16px;border-radius:8px;background:var(--bg-raised);
  border:1px solid var(--border-lit);position:relative;flex-shrink:0}
.toggle-thumb{position:absolute;top:2px;left:2px;width:10px;height:10px;border-radius:50%;
  background:var(--text-muted);transition:transform .2s,background .2s}
.filter-toggle.active{color:var(--accent)}
.filter-toggle.active .toggle-track{background:var(--accent-dim);border-color:color-mix(in srgb,var(--accent) 35%,transparent)}
.filter-toggle.active .toggle-thumb{transform:translateX(12px);background:var(--accent)}
/* badges */
.badge{display:inline-flex;align-items:center;gap:6px;padding:3px 10px;border-radius:5px;
  font-family:var(--font-mono);font-size:11px;font-weight:600;letter-spacing:.3px;
  white-space:nowrap;text-transform:uppercase}
.badge-success{background:color-mix(in srgb,var(--success) 12%,transparent);color:var(--success);
  border:1px solid color-mix(in srgb,var(--success) 20%,transparent)}
.badge-failed{background:color-mix(in srgb,var(--danger) 11%,transparent);color:var(--danger);
  border:1px solid color-mix(in srgb,var(--danger) 20%,transparent)}
.badge-skipped{background:color-mix(in srgb,var(--skip) 12%,transparent);color:var(--skip);
  border:1px solid color-mix(in srgb,var(--skip) 15%,transparent)}
.badge-deploying{background:var(--accent-dim);color:var(--accent);
  border:1px solid color-mix(in srgb,var(--accent) 20%,transparent)}
.badge-rolled_back{background:color-mix(in srgb,var(--rollback) 13%,transparent);color:var(--rollback);
  border:1px solid color-mix(in srgb,var(--rollback) 22%,transparent)}
.spinner{width:5px;height:5px;border-radius:50%;background:currentColor;animation:breathe 1.4s ease-in-out infinite}
/* files pill */
.files-pill{display:inline-flex;align-items:center;gap:4px;padding:3px 8px;border-radius:4px;
  background:color-mix(in srgb,var(--overlay2) 8%,transparent);border:1px solid var(--border);
  color:var(--text-secondary);font-family:var(--font-mono);font-size:11px;font-weight:500;
  cursor:pointer;transition:all .15s}
.files-pill:hover{background:color-mix(in srgb,var(--overlay2) 14%,transparent);
  border-color:var(--border-lit);color:var(--text-primary)}
.files-pill svg{width:12px;height:12px;opacity:.5}
.files-list{padding:10px 14px;background:var(--bg-sunken);border:1px solid var(--border);
  border-radius:6px;font-family:var(--font-mono);font-size:11px;color:var(--text-secondary);
  line-height:2;word-break:break-all;animation:expand .2s ease-out}
.file-path{padding:1px 6px;border-radius:3px;background:color-mix(in srgb,var(--overlay2) 10%,transparent);
  display:inline-block;margin:1px 0}
/* table */
.event-list{display:flex;flex-direction:column;gap:2px}
.event-list-header{display:grid;grid-template-columns:160px 1fr 110px 80px 100px;gap:12px;
  padding:0 20px 10px;font-family:var(--font-mono);font-size:10px;font-weight:600;
  color:var(--text-muted);text-transform:uppercase;letter-spacing:1px;
  border-bottom:1px solid var(--border);margin-bottom:4px}
.event-row{display:grid;grid-template-columns:160px 1fr 110px 80px 100px;gap:12px;
  align-items:center;padding:12px 20px;border-radius:var(--radius);position:relative}
.event-row:hover{background:color-mix(in srgb,var(--overlay2) 6%,transparent)}
.event-row.skipped{opacity:.35}
.event-row.deploying-row{background:var(--accent-dim);
  border:1px solid color-mix(in srgb,var(--accent) 18%,transparent);margin:2px 0}
.event-row.deploying-row::before{content:'';position:absolute;left:0;top:50%;transform:translateY(-50%);
  width:3px;height:60%;background:var(--accent);border-radius:0 2px 2px 0;box-shadow:0 0 12px var(--accent-glow)}
.event-row.failed-row{background:color-mix(in srgb,var(--danger) 9%,transparent)}
.event-row.failed-row::before{content:'';position:absolute;left:0;top:50%;transform:translateY(-50%);
  width:3px;height:60%;background:var(--danger);border-radius:0 2px 2px 0}
.event-row.rolled_back-row{background:color-mix(in srgb,var(--rollback) 12%,transparent)}
.event-row.rolled_back-row::before{content:'';position:absolute;left:0;top:50%;transform:translateY(-50%);
  width:3px;height:60%;background:var(--rollback);border-radius:0 2px 2px 0}
.cell-time{font-family:var(--font-mono);font-size:12px;color:var(--text-muted);white-space:nowrap}
.cell-stack{font-family:var(--font-mono);font-size:13px;font-weight:500;color:var(--text-primary);
  letter-spacing:-.3px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.cell-duration{font-family:var(--font-mono);font-size:12px;color:var(--text-muted)}
/* panels */
.error-detail{margin-top:4px;padding:10px 14px;background:color-mix(in srgb,var(--danger) 6%,transparent);
  border:1px solid color-mix(in srgb,var(--danger) 16%,transparent);border-radius:6px;
  font-family:var(--font-mono);font-size:11px;color:var(--danger);white-space:pre-wrap;
  word-break:break-all;line-height:1.6;animation:expand .2s ease-out}
.diff-panel{background:var(--bg-sunken);border:1px solid var(--border);border-radius:6px;
  font-family:var(--font-mono);font-size:11px;color:var(--text-secondary);overflow:hidden;animation:expand .2s ease-out}
.diff-file-section{border-bottom:1px solid var(--border)}
.diff-file-section:last-child{border-bottom:none}
.diff-file-header{display:flex;align-items:center;gap:6px;padding:8px 14px;
  background:color-mix(in srgb,var(--overlay2) 6%,transparent);cursor:pointer;user-select:none;
  font-weight:500;color:var(--text-primary);font-size:11px}
.diff-file-header:hover{background:color-mix(in srgb,var(--overlay2) 10%,transparent)}
.diff-file-header svg{width:10px;height:10px;opacity:.5;transition:transform .2s;flex-shrink:0}
.diff-file-header.expanded svg{transform:rotate(90deg)}
.diff-content{padding:6px 0;overflow-x:auto;white-space:pre;line-height:1.6}
.diff-line{padding:0 14px;display:block}
.diff-add{background:color-mix(in srgb,var(--diff-add) 10%,transparent);color:var(--diff-add)}
.diff-del{background:color-mix(in srgb,var(--danger) 10%,transparent);color:var(--danger)}
.diff-hunk{background:color-mix(in srgb,var(--hunk) 9%,transparent);color:var(--hunk);font-weight:500}
.diff-meta{color:var(--text-muted)}
/* view toggle */
.view-toggle{display:flex;gap:2px;padding:2px;background:var(--bg-raised);
  border:1px solid var(--border);border-radius:6px}
.view-toggle button{font-family:var(--font-mono);font-size:11px;font-weight:500;letter-spacing:.3px;
  color:var(--text-muted);background:none;border:none;border-radius:4px;padding:3px 10px;cursor:pointer}
.view-toggle button.active{color:var(--accent);background:var(--accent-dim)}
/* log view */
.log-pane{background:var(--bg-sunken);border:1px solid var(--border);border-radius:var(--radius);
  font-family:var(--font-mono);font-size:11.5px;line-height:1.7;padding:10px 0;overflow-y:auto}
.log-line{display:flex;gap:10px;align-items:baseline;padding:0 14px}
.log-line:hover{background:color-mix(in srgb,var(--overlay2) 6%,transparent)}
.log-time{color:var(--text-muted);flex-shrink:0}
.log-level{flex-shrink:0;width:44px;font-size:10px;font-weight:600;letter-spacing:.5px;color:var(--text-secondary)}
.log-level.ERROR{color:var(--danger)}
.log-level.WARN{color:var(--hunk)}
.log-level.DEBUG{color:var(--skip)}
.log-cmd{color:var(--skip);flex-shrink:0}
.log-msg{color:var(--text-primary);white-space:pre-wrap;word-break:break-all}
.log-attrs{color:var(--text-muted);word-break:break-all}
.log-empty{padding:60px 24px;text-align:center;color:var(--text-muted);font-size:13px;font-family:var(--font-mono)}
/* misc */
.sep{display:flex;align-items:center;gap:12px;padding:16px 20px 8px;font-family:var(--font-mono);
  font-size:10px;font-weight:600;color:var(--text-muted);text-transform:uppercase;letter-spacing:1.2px}
.sep::after{content:'';flex:1;height:1px;background:var(--border)}
.empty-state{display:flex;flex-direction:column;align-items:center;justify-content:center;
  padding:70px 24px;color:var(--text-muted)}
.empty-icon{width:64px;height:64px;margin-bottom:20px;opacity:.35}
.empty-state p{font-size:14px;font-family:var(--font-mono);letter-spacing:-.2px}
"""

FOLDER_SVG = '<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M2 4h5l2 2h5v7H2z"/></svg>'
CHEV_SVG = '<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2"><path d="M6 4l4 4-4 4"/></svg>'
RADAR_SVG = ('<svg class="empty-icon" viewBox="0 0 64 64" fill="none" stroke="currentColor" stroke-width="1.5" '
  'stroke-linecap="round" stroke-linejoin="round"><circle cx="32" cy="32" r="24"/><circle cx="32" cy="32" r="4"/>'
  '<line x1="32" y1="12" x2="32" y2="20"/><line x1="32" y1="44" x2="32" y2="52"/>'
  '<line x1="12" y1="32" x2="20" y2="32"/><line x1="44" y1="32" x2="52" y2="32"/></svg>')

def view_toggle(active="deploys"):
    return ('<div class="view-toggle">'
            f'<button class="{"active" if active == "deploys" else ""}">deploys</button>'
            f'<button class="{"active" if active == "logs" else ""}">logs</button></div>')

def theme_toggle(light=False):
    return (f'<button class="filter-toggle{" active" if light else ""}">'
            '<div class="toggle-track"><div class="toggle-thumb"></div></div><span>light</span></button>')

def header_html(deploying=False, conn=("", "connected"), compact=False, light=False):
    dep = ('<div class="deploy-status active"><div class="deploy-dot"></div><span>monitoring, grafana</span></div>'
           if deploying else
           '<div class="deploy-status"><div class="deploy-dot"></div><span>idle</span></div>')
    dotcls, conntext = conn
    toggles = ('<button class="filter-toggle active"><div class="toggle-track"><div class="toggle-thumb"></div></div><span>hide skipped</span></button>'
               '<button class="filter-toggle"><div class="toggle-track"><div class="toggle-thumb"></div></div><span>abs time</span></button>'
               + theme_toggle(light))
    tag = '' if compact else '<span class="brand-tag">live</span>'
    right = view_toggle() + dep + ('' if compact else toggles) + \
        f'<div class="indicator"><div class="indicator-dot {dotcls}"></div><span>{conntext}</span></div>'
    return (f'<div class="hdr"{" style=\'height:48px;padding:0 16px\'" if compact else ""}>'
            f'<div class="brand"><div class="brand-icon">{logo}</div>'
            f'<span class="brand-name">skipper<span>-cd</span></span>{tag}</div>'
            f'<div class="status-area"{" style=\'gap:14px\'" if compact else ""}>{right}</div></div>')

def row(time, stack, badge, dur, files, cls=""):
    b = {"deploying": '<span class="badge badge-deploying"><span class="spinner"></span>deploying</span>',
         "success": '<span class="badge badge-success">success</span>',
         "failed": '<span class="badge badge-failed">failed</span>',
         "rolled_back": '<span class="badge badge-rolled_back">rolled back</span>',
         "skipped": '<span class="badge badge-skipped">skipped</span>'}[badge]
    f = (f'<span><button class="files-pill">{FOLDER_SVG}{files}</button></span>'
         if files else '<span class="cell-duration">&mdash;</span>')
    return (f'<div class="event-row {cls}"><span class="cell-time">{time}</span>'
            f'<span class="cell-stack">{stack}</span><span>{b}</span>'
            f'<span class="cell-duration">{dur}</span>{f}</div>')

DIFF_PANEL = f'''<div class="diff-panel">
 <div class="diff-file-section">
  <div class="diff-file-header expanded">{CHEV_SVG}stacks/grafana/docker-compose.yml</div>
  <div class="diff-content"><span class="diff-line diff-meta">diff --git a/stacks/grafana/docker-compose.yml b/stacks/grafana/docker-compose.yml</span><span class="diff-line diff-meta">--- a/stacks/grafana/docker-compose.yml</span><span class="diff-line diff-meta">+++ b/stacks/grafana/docker-compose.yml</span><span class="diff-line diff-hunk">@@ -4,7 +4,7 @@ services:</span><span class="diff-line">   grafana:</span><span class="diff-line diff-del">-    image: grafana/grafana:11.1.0</span><span class="diff-line diff-add">+    image: grafana/grafana:11.2.2</span><span class="diff-line">     restart: unless-stopped</span><span class="diff-line">     ports:</span><span class="diff-line">       - "3000:3000"</span></div>
 </div>
 <div class="diff-file-section">
  <div class="diff-file-header">{CHEV_SVG}stacks/grafana/provisioning/datasources.yml</div>
 </div>
</div>'''

ERROR_PANEL = ('<div class="error-detail">docker compose up -d failed (exit 1):\n'
 'Error response from daemon: driver failed programming external connectivity on endpoint traefik '
 '(0.0.0.0:443): port is already allocated</div>')

def page(title, layout, bodies, extra_css=""):
    """bodies: (mocha_body, latte_body) — usually the same string twice."""
    mocha_body, latte_body = bodies
    return f"""<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{title} &mdash; skipper-cd DS</title>
<style>{BASE}{COMP}{extra_css}</style></head><body class="{layout}">
<section class="pane mocha"><header class="pane-tag"><b>Catppuccin Mocha</b><span>dark &middot; default</span></header>
{mocha_body}
</section>
<section class="pane latte"><header class="pane-tag"><b>Catppuccin Latte</b><span>light &middot; opt-in</span></header>
{latte_body}
</section>
</body></html>
"""

cards = {}

# ── Foundations: Colors ─────────────────────────────────────────
colors_css = """
.depth{border-radius:12px;padding:14px;border:1px solid var(--border)}
.depth div{border-radius:10px;padding:14px;margin-top:10px;border:1px solid var(--border-lit)}
.depth span{font:500 11px var(--font-mono);color:var(--text-secondary);display:block}
.swgrid{display:grid;grid-template-columns:repeat(auto-fill,minmax(118px,1fr));gap:10px}
.swatch{background:var(--bg-base);border:1px solid var(--border);border-radius:10px;padding:10px;display:flex;flex-direction:column;gap:2px}
.swatch i{display:block;height:30px;border-radius:6px;margin-bottom:6px}
.swatch b{font-size:12px;font-family:var(--font-mono)}
.swatch span{font-size:10.5px;color:var(--text-secondary);font-family:var(--font-mono)}
.semrow{display:grid;grid-template-columns:24px 105px 130px 1fr;gap:10px;align-items:center;padding:10px 14px}
.semrow+.semrow{border-top:1px solid var(--border)}
.semrow i{width:20px;height:20px;border-radius:6px}
.semrow b{font-size:11.5px}.semrow span{font-size:10.5px}
.semrow em{font-style:normal;font-size:12px;color:var(--text-secondary)}
.txtdemo p+p{margin-top:6px}
"""
def colors_body(P):
    sw = "".join(f'<div class="swatch"><i style="background:{P[n]}"></i><b>{n}</b><span>{P[n]}</span></div>'
        for n in ["peach","teal","red","maroon","green","yellow","blue","lavender","sapphire","sky","mauve","pink","flamingo","rosewater"])
    sem = [("peach","--accent","Brand wordmark &middot; active deploy &middot; toggles &middot; connecting &middot; glow"),
           ("teal","--success","Success &middot; connected"),
           ("red","--danger","Failed &middot; error panels &middot; reconnecting &middot; diff deletions"),
           ("maroon","--rollback","Rolled back (failed, old containers restored)"),
           ("overlay1","--skip","Skipped &middot; rows at 35% opacity"),
           ("green","--diff-add","Diff additions"),
           ("yellow","--hunk","Diff hunk headers")]
    semrows = "".join(f'<div class="semrow"><i style="background:{P[c]}"></i><b class="mono">{t}</b>'
                      f'<span class="mono muted">{c} {P[c]}</span><em>{d}</em></div>' for c, t, d in sem)
    return f"""
<h2 class="overline sec">Background depth</h2>
<div class="depth" style="background:{P['crust']}"><span>crust &middot; sunken (diff/files panels) {P['crust']}</span>
 <div style="background:{P['mantle']}"><span>mantle &middot; page background {P['mantle']}</span>
  <div style="background:{P['base']}"><span>base &middot; header glass, cards {P['base']}</span>
   <div style="background:{P['surface0']}"><span>surface0 &middot; tags, toggle tracks {P['surface0']}</span></div>
  </div></div></div>
<h2 class="overline sec">Text hierarchy</h2>
<div class="card txtdemo" style="padding:16px 18px">
 <p style="color:{P['text']};font-weight:600;font-size:15px">text &mdash; stack names, headings {P['text']}</p>
 <p style="color:{P['subtext0']};font-size:13px">subtext0 &mdash; indicators, panels {P['subtext0']}</p>
 <p style="color:{P['overlay1']};font-size:13px">overlay1 &mdash; timestamps, durations, column headers {P['overlay1']}</p>
</div>
<h2 class="overline sec">Accents</h2>
<div class="swgrid">{sw}</div>
<h2 class="overline sec">Semantic mapping &mdash; one fixed token table</h2>
<div class="card" style="padding:6px 0">{semrows}</div>
"""
cards["foundations/colors.html"] = ("Foundations", "Colors & semantic tokens", page(
    "Colors &amp; semantic tokens", "side", (colors_body(MOCHA), colors_body(LATTE)), colors_css))

# ── Foundations: Typography ─────────────────────────────────────
typo_css = """
.spec{padding:14px 16px}
.spec .line{display:flex;align-items:baseline;gap:14px;padding:9px 0;flex-wrap:wrap}
.spec .line+.line{border-top:1px solid var(--border)}
.spec .tag{font:500 10.5px var(--font-mono);color:var(--text-muted);min-width:165px}
"""
typo_body = """
<h2 class="overline sec">DM Sans &mdash; UI copy</h2>
<div class="card spec">
 <div class="line"><span class="tag">400 &middot; body</span><span style="font-size:14px">Redeploys only stacks whose tracked files changed.</span></div>
 <div class="line"><span class="tag">500 &middot; emphasis</span><span style="font-size:14px;font-weight:500">Change detection always comes from the repo clone.</span></div>
 <div class="line"><span class="tag">600 &middot; headings</span><span style="font-size:16px;font-weight:600">Deployment history</span></div>
</div>
<h2 class="overline sec">JetBrains Mono &mdash; everything machine-flavoured</h2>
<div class="card spec">
 <div class="line"><span class="tag">15px 600 &middot; wordmark</span><span class="mono" style="font-size:15px;font-weight:600;letter-spacing:-.3px">skipper<span style="color:var(--accent)">-cd</span></span></div>
 <div class="line"><span class="tag">13px 500 &middot; stack names</span><span class="mono" style="font-size:13px;font-weight:500;letter-spacing:-.3px">monitoring &middot; grafana &middot; traefik</span></div>
 <div class="line"><span class="tag">12px 400 &middot; time / duration</span><span class="mono muted" style="font-size:12px">2m ago &middot; 14s &middot; 1m 32s</span></div>
 <div class="line"><span class="tag">11px 600 &middot; badges</span><span class="mono" style="font-size:11px;font-weight:600;letter-spacing:.3px;text-transform:uppercase;color:var(--success)">SUCCESS</span></div>
 <div class="line"><span class="tag">11px 400 &middot; diffs / errors</span><span class="mono" style="font-size:11px;color:var(--diff-add)">+    image: grafana/grafana:11.2.2</span></div>
 <div class="line"><span class="tag">10px 600 &middot; column headers</span><span class="mono muted" style="font-size:10px;font-weight:600;letter-spacing:1px;text-transform:uppercase">TIME&emsp;STACK&emsp;STATUS&emsp;DURATION&emsp;FILES</span></div>
</div>
<p class="caption">Rule of thumb: if a value comes from the machine (stack names, timestamps, statuses, file paths, diffs), it is JetBrains Mono. DM Sans is reserved for human-written copy. Type is palette-independent &mdash; only the ink changes.</p>
"""
cards["foundations/typography.html"] = ("Foundations", "Typography", page(
    "Typography", "side", (typo_body, typo_body), typo_css))

# ── Foundations: Surfaces & effects ─────────────────────────────
surf_css = """
.fx{display:grid;grid-template-columns:1fr;gap:14px}
.fxcard{background:var(--bg-base);border:1px solid var(--border);border-radius:var(--radius);padding:16px 18px;position:relative;overflow:hidden}
.fxcard h3{font-size:13px;font-weight:600;margin-bottom:4px}
.fxcard p{font-size:11.5px;color:var(--text-secondary)}
.griddemo{height:100px;border-radius:6px;margin-top:12px;border:1px solid var(--border);position:relative;background:var(--bg-deep)}
.griddemo::before{content:'';position:absolute;inset:0;background:linear-gradient(color-mix(in srgb,var(--overlay2) 12%,transparent) 1px,transparent 1px),linear-gradient(90deg,color-mix(in srgb,var(--overlay2) 12%,transparent) 1px,transparent 1px);background-size:48px 48px}
.glowdemo{height:100px;border-radius:6px;margin-top:12px;border:1px solid var(--border);background:radial-gradient(ellipse at 50% -30%,color-mix(in srgb,var(--accent) 20%,transparent) 0%,transparent 70%),var(--bg-deep)}
.glassdemo{height:100px;border-radius:6px;margin-top:12px;border:1px solid var(--border);position:relative;overflow:hidden;background:var(--bg-deep)}
.glassdemo .behind{position:absolute;inset:0;display:flex;flex-wrap:wrap;gap:8px;padding:10px}
.glassdemo .behind i{width:56px;height:22px;border-radius:5px;background:var(--accent-dim);border:1px solid color-mix(in srgb,var(--accent) 25%,transparent)}
.glassdemo .glass{position:absolute;left:0;right:0;top:24px;height:52px;background:color-mix(in srgb,var(--bg-base) 82%,transparent);backdrop-filter:blur(20px) saturate(1.2);border-top:1px solid var(--border);border-bottom:1px solid var(--border);display:flex;align-items:center;padding:0 14px;font:600 11px var(--font-mono);color:var(--text-secondary)}
.animrow{display:flex;gap:20px;align-items:center;margin-top:12px;flex-wrap:wrap}
.pulse{width:10px;height:10px;border-radius:50%;background:var(--accent);animation:breathe 2s ease-in-out infinite}
.slidein{padding:8px 14px;border-radius:6px;background:var(--accent-dim);border:1px solid color-mix(in srgb,var(--accent) 18%,transparent);font:500 11px var(--font-mono);animation:rowEnter 2.4s cubic-bezier(.16,1,.3,1) infinite}
"""
surf_body = """
<div class="fx">
 <div class="fxcard"><h3>Grid overlay</h3><p>48px blueprint grid in a faint overlay tint, fixed behind everything &mdash; the "chart paper" feel survives both palettes.</p><div class="griddemo"></div></div>
 <div class="fxcard"><h3>Peach glow</h3><p>Radial ellipse pinned top-centre (800&times;400, ~8% peach). The single warm light source on the page &mdash; was amber, now <span class="mono">--accent</span>.</p><div class="glowdemo"></div></div>
 <div class="fxcard"><h3>Frosted-glass header</h3><p>Sticky 56px bar: <span class="mono">color-mix(base 82%)</span> + blur(20px) saturate(1.2), hairline border-bottom.</p><div class="glassdemo"><div class="behind"><i></i><i></i><i></i><i></i><i></i><i></i><i></i><i></i><i></i><i></i></div><div class="glass">skipper-cd &middot; content scrolls beneath</div></div></div>
 <div class="fxcard"><h3>Borders, radius &amp; motion</h3><p>Hairlines only: <span class="mono">color-mix(overlay2 14%)</span>, lit to 28% on hover; radius 8px for rows/cards, 4&ndash;6px for pills and panels. No drop shadows &mdash; depth comes from the crust&rarr;surface0 background steps and glows. <b>breathe</b> 2s pulses dots, <b>rowEnter</b> 0.4s slides new rows in, <b>expand</b> 0.2s unfolds panels.</p>
  <div class="animrow"><span class="pulse"></span><span class="slidein">new event row</span></div></div>
</div>
"""
cards["foundations/surfaces-effects.html"] = ("Foundations", "Surfaces, grid & glow", page(
    "Surfaces &amp; effects", "side", (surf_body, surf_body), surf_css))

# ── Navigation: Header ──────────────────────────────────────────
def hdr_body(light):
    return f"""
<h2 class="overline sec">Idle &middot; connected</h2>
{header_html(deploying=False, conn=("", "connected"), light=light)}
<h2 class="overline sec">Deploying &middot; active stacks shown, peach breathing dots</h2>
{header_html(deploying=True, conn=("", "connected"), light=light)}
<h2 class="overline sec">Connecting (peach pulse) &rarr; reconnecting (red)</h2>
{header_html(deploying=False, conn=("warn", "connecting"), light=light)}
<div style="height:10px"></div>
{header_html(deploying=False, conn=("err", "reconnecting"), light=light)}
<p class="caption">Sticky, 56px, frosted glass. Left: ship logo (32px) + mono wordmark with peach <span class="mono">-cd</span> + LIVE tag. Right: view toggle (deploys | logs) &middot; deploy indicator &middot; skip filter &middot; time-mode toggle &middot; theme toggle (Mocha default, Latte opt-in &mdash; shown active in the Latte pane) &middot; SSE connection state. Skip/time toggles are deploys-only, a follow toggle replaces them in the logs view; all persist in localStorage. At &le;700px the bar drops to 48px, the LIVE tag disappears and the toggles lose their labels.</p>
"""
cards["navigation/header.html"] = ("Navigation", "App header", page(
    "Header", "stack", (hdr_body(False), hdr_body(True))))

# ── Controls: Badges & pills ────────────────────────────────────
badge_body = f"""
<h2 class="overline sec">Status badges &mdash; one per deploy outcome</h2>
<div class="card" style="padding:18px"><div class="row-flex">
 <span class="badge badge-deploying"><span class="spinner"></span>deploying</span>
 <span class="badge badge-success">success</span>
 <span class="badge badge-failed">failed</span>
 <span class="badge badge-rolled_back">rolled back</span>
 <span class="badge badge-skipped">skipped</span>
</div>
<p class="caption">Mono 11px uppercase, ~12% tint fill + ~20% border in the status colour: peach / teal / red / maroon / overlay. <b>deploying</b> carries a breathing dot; <b>rolled back</b> = failed but old containers restored.</p></div>
<h2 class="overline sec">Files pill &mdash; opens the files/diff panel</h2>
<div class="card" style="padding:18px"><div class="row-flex">
 <button class="files-pill">{FOLDER_SVG}1 file</button>
 <button class="files-pill">{FOLDER_SVG}3 files</button>
 <span class="cell-duration">&mdash; (no changed files)</span>
</div></div>
<h2 class="overline sec">Tags &amp; file paths</h2>
<div class="card" style="padding:18px"><div class="row-flex">
 <span class="brand-tag">live</span>
 <span class="file-path mono" style="font-size:11px">stacks/grafana/docker-compose.yml</span>
 <span class="file-path mono" style="font-size:11px">hosts/anchor/configuration.nix</span>
</div></div>
"""
cards["controls/badges-pills.html"] = ("Controls", "Status badges & pills", page(
    "Badges &amp; pills", "side", (badge_body, badge_body)))

# ── Controls: Toggles & indicators ──────────────────────────────
tgl_body = """
<h2 class="overline sec">Filter toggles &mdash; header controls, localStorage-persisted</h2>
<div class="card" style="padding:18px"><div class="row-flex" style="gap:28px">
 <button class="filter-toggle active"><div class="toggle-track"><div class="toggle-thumb"></div></div><span>hide skipped</span></button>
 <button class="filter-toggle"><div class="toggle-track"><div class="toggle-thumb"></div></div><span>abs time</span></button>
</div>
<p class="caption">Active = peach thumb + peach label. "hide skipped" defaults on; "abs time" switches the Time column between relative and <span class="mono">toLocaleString()</span> &mdash; the tooltip always shows the other format.</p></div>
<h2 class="overline sec">Connection indicator &mdash; SSE lifecycle</h2>
<div class="card" style="padding:18px"><div class="row-flex" style="gap:28px">
 <div class="indicator"><div class="indicator-dot warn"></div><span>connecting</span></div>
 <div class="indicator"><div class="indicator-dot"></div><span>connected</span></div>
 <div class="indicator"><div class="indicator-dot err"></div><span>reconnecting</span></div>
</div></div>
<h2 class="overline sec">Deploy indicator &mdash; idle vs. active</h2>
<div class="card" style="padding:18px"><div class="row-flex" style="gap:28px">
 <div class="deploy-status"><div class="deploy-dot"></div><span>idle</span></div>
 <div class="deploy-status active"><div class="deploy-dot"></div><span>monitoring, grafana</span></div>
</div>
<p class="caption">Shows the active stack name(s) while deploying; the dot breathes peach. All indicators: 7px dot + mono 12px label, glow in the dot colour.</p></div>
"""
cards["controls/toggles-indicators.html"] = ("Controls", "Toggles & indicators", page(
    "Toggles &amp; indicators", "side", (tgl_body, tgl_body)))

# ── Patterns: Deploy table ──────────────────────────────────────
table_body = f"""
<div class="event-list-header"><span>Time</span><span>Stack</span><span>Status</span><span>Duration</span><span>Files</span></div>
<div class="event-list">
{row("just now","monitoring","deploying","&mdash;","3 files","deploying-row")}
{row("2m ago","grafana","success","14s","1 file")}
{row("18m ago","traefik","failed","31s","2 files","failed-row")}
{ERROR_PANEL}
{row("1h ago","paperless","rolled_back","1m 32s","1 file","rolled_back-row")}
{row("1h ago","immich","skipped","&mdash;","","skipped")}
{row("2d ago","monitoring","success","41s","2 files")}
</div>
<p class="caption">5-column grid <span class="mono">160px 1fr 110px 80px 100px</span>, newest first with a slide-in. Row states paint a ~10% tint background plus a 3px left accent bar (peach glows while deploying). Skipped rows sit at 35% opacity and are hidden by the default filter. Failed rows expand their error panel directly beneath.</p>
"""
cards["patterns/deploy-table.html"] = ("Patterns", "Deploy table", page(
    "Deploy table", "stack", (table_body, table_body)))

# ── Patterns: Expandable panels ─────────────────────────────────
panels_body = f"""
<h2 class="overline sec">Files panel &mdash; plain list when the event has no stored diffs</h2>
<div class="files-list"><span class="file-path">stacks/monitoring/docker-compose.yml</span><br><span class="file-path">stacks/monitoring/prometheus/prometheus.yml</span><br><span class="file-path">stacks/monitoring/.env</span></div>
<h2 class="overline sec">Diff panel &mdash; when <span class="mono" style="text-transform:none">has_diffs</span>, fetched from <span class="mono" style="text-transform:none">GET /api/events/{{id}}/diffs</span></h2>
{DIFF_PANEL}
<p class="caption">One collapsible section per file (single-file diffs default open). Additions green, deletions red, hunk headers yellow, metadata muted &mdash; the classic Catppuccin diff mapping. Cached client-side after first fetch; truncated at 10&thinsp;KB/file, 50&thinsp;KB/event.</p>
<h2 class="overline sec">Error detail &mdash; failed events with an error field</h2>
{ERROR_PANEL.replace('margin-top:4px;','')}
<p class="caption">All three insert as full-width siblings directly below their row, unfold with the 0.2s expand animation, and toggle closed on a second click. Panels sit on <span class="mono">crust</span> &mdash; the sunken layer.</p>
"""
cards["patterns/expandable-panels.html"] = ("Patterns", "Files, diff & error panels", page(
    "Expandable panels", "side", (panels_body, panels_body)))

# ── Patterns: Logs view ─────────────────────────────────────────
def logline(time, level, msg, attrs="", cmd=""):
    if cmd:
        mid = f'<span class="log-cmd">[{cmd}]</span>'
    else:
        mid = f'<span class="log-level {level}">{level}</span>'
    a = f'<span class="log-attrs">{attrs}</span>' if attrs else ''
    return (f'<div class="log-line"><span class="log-time">{time}</span>{mid}'
            f'<span class="log-msg">{msg}</span>{a}</div>')

logs_body = f"""
<div class="row-flex" style="justify-content:space-between;margin-bottom:12px">
 {view_toggle("logs")}
 <button class="filter-toggle active"><div class="toggle-track"><div class="toggle-thumb"></div></div><span>follow</span></button>
</div>
<div class="log-pane">
{logline("14:02:11","INFO","received push webhook","ref=refs/heads/main commits=2")}
{logline("14:02:11","DEBUG","hashing stack inputs","stack=monitoring files=14")}
{logline("14:02:12","INFO","deploying stack","stack=monitoring")}
{logline("14:02:12","","Container prometheus  Recreated",cmd="docker")}
{logline("14:02:13","","Container grafana  Started",cmd="docker")}
{logline("14:02:14","INFO","stack deployed","stack=monitoring duration=14s")}
{logline("14:02:14","WARN","skipping pull: no image refs changed","stack=immich")}
{logline("14:02:15","ERROR","docker compose up -d failed: exit 1","stack=traefik")}
{logline("14:02:15","","Bind for 0.0.0.0:443 failed: port is already allocated",cmd="docker")}
{logline("14:02:16","INFO","rolled back to last deployed commit","stack=traefik commit=3f2c9a1")}
</div>
<h2 class="overline sec">Empty state</h2>
<div class="log-pane"><div class="log-empty">Awaiting log output...</div></div>
<p class="caption">Full-width mono pane on <span class="mono">crust</span> (sunken), newest line at the <b>bottom</b> &mdash; terminal semantics, unlike the deploy table's prepend. Each line: muted <span class="mono">toLocaleTimeString()</span> timestamp, level badge (ERROR red &middot; WARN yellow &middot; DEBUG muted &middot; INFO secondary), message, dim <span class="mono">key=value</span> attrs. Child-process output renders a muted <span class="mono">[docker]</span>-style prefix instead of a level badge. DOM capped at 1000 lines; the follow toggle auto-scrolls on append.</p>
"""
cards["patterns/logs-view.html"] = ("Patterns", "Logs view", page(
    "Logs view", "side", (logs_body, logs_body)))

# ── Patterns: Empty state & separators ──────────────────────────
empty_body = f"""
<h2 class="overline sec">Empty state &mdash; before any event arrives</h2>
<div class="card"><div class="empty-state">{RADAR_SVG}<p>Awaiting deployment events...</p></div></div>
<h2 class="overline sec">Separator &mdash; groups replayed history from live events</h2>
<div class="card" style="padding-bottom:14px">
<div class="sep">earlier</div>
{row("3d ago","traefik","success","28s","1 file")}
</div>
<p class="caption">The radar icon picks up the nautical theme at 35% opacity &mdash; quiet, not sad. Separators are mono 10px uppercase with a hairline rule filling the remaining width.</p>
"""
cards["patterns/empty-states.html"] = ("Patterns", "Empty state & separators", page(
    "Empty state &amp; separators", "side", (empty_body, empty_body)))

# ── Screens: Desktop dashboard ──────────────────────────────────
def desk_body(light):
    return f"""
{header_html(deploying=True, conn=("", "connected"), light=light)}
<div style="height:28px"></div>
<div class="event-list-header"><span>Time</span><span>Stack</span><span>Status</span><span>Duration</span><span>Files</span></div>
<div class="event-list">
{row("just now","monitoring","deploying","&mdash;","3 files","deploying-row")}
{row("just now","grafana","deploying","&mdash;","1 file","deploying-row")}
{row("5m ago","traefik","success","22s","1 file")}
{DIFF_PANEL}
{row("38m ago","paperless","failed","44s","2 files","failed-row")}
{ERROR_PANEL}
{row("1h ago","immich","rolled_back","1m 05s","1 file","rolled_back-row")}
<div class="sep">earlier</div>
{row("1d ago","monitoring","success","36s","4 files")}
{row("2d ago","grafana","success","18s","1 file")}
</div>
"""
cards["screens/dashboard-desktop.html"] = ("Screens", "Dashboard — desktop", page(
    "Dashboard", "stack", (desk_body(False), desk_body(True))))

# ── Screens: Mobile ─────────────────────────────────────────────
mob_css = """
.frame{width:390px;max-width:100%;margin:0 auto;background:var(--bg-deep);border:1px solid var(--border-lit);
  border-radius:24px;overflow:hidden;position:relative}
.frame .hdr{border-radius:0;border-left:none;border-right:none;border-top:none}
.m-list{padding:12px;display:flex;flex-direction:column;gap:2px}
.m-row{display:grid;grid-template-columns:1fr auto;grid-template-rows:auto auto;gap:4px 12px;
  padding:10px 14px;border-radius:var(--radius);position:relative}
.m-row .cell-stack{grid-column:1;grid-row:1}
.m-row .m-badge{grid-column:2;grid-row:1;justify-self:end}
.m-row .cell-time{grid-column:1;grid-row:2;font-size:11px}
.m-row .cell-duration{grid-column:2;grid-row:2;text-align:right}
.m-row.deploying-row{background:var(--accent-dim);border:1px solid color-mix(in srgb,var(--accent) 18%,transparent)}
.m-row.deploying-row::before{content:'';position:absolute;left:0;top:50%;transform:translateY(-50%);
  width:3px;height:60%;background:var(--accent);border-radius:0 2px 2px 0;box-shadow:0 0 12px var(--accent-glow)}
.m-row.failed-row{background:color-mix(in srgb,var(--danger) 9%,transparent)}
.m-row.haspill{cursor:pointer}
"""
def mrow(time, stack, badge, dur, cls=""):
    b = {"deploying": '<span class="badge badge-deploying"><span class="spinner"></span>deploying</span>',
         "success": '<span class="badge badge-success">success</span>',
         "failed": '<span class="badge badge-failed">failed</span>',
         "rolled_back": '<span class="badge badge-rolled_back">rolled back</span>'}[badge]
    return (f'<div class="m-row {cls}"><span class="cell-stack">{stack}</span>'
            f'<span class="m-badge">{b}</span><span class="cell-time">{time}</span>'
            f'<span class="cell-duration">{dur}</span></div>')
mob_body = f"""
<div class="frame">
{header_html(deploying=True, conn=("", "connected"), compact=True)}
<div class="m-list">
{mrow("just now","monitoring","deploying","&mdash;","deploying-row haspill")}
{mrow("2m ago","grafana","success","14s","haspill")}
{mrow("18m ago","traefik","failed","31s","failed-row haspill")}
{mrow("1h ago","paperless","rolled_back","1m 32s","haspill")}
{mrow("2d ago","monitoring","success","41s","haspill")}
</div>
</div>
<p class="caption" style="text-align:center">&le;700px: column header gone, rows collapse to a 2&times;2 grid (stack + badge / time + duration). The Files column is hidden &mdash; tapping anywhere on a row with changed files toggles its files/diff panel instead. Header shrinks to 48px.</p>
"""
cards["screens/dashboard-mobile.html"] = ("Screens", "Dashboard — mobile ≤700px", page(
    "Dashboard &mdash; mobile", "side", (mob_body, mob_body), mob_css))

# ── write out ───────────────────────────────────────────────────
for path, (group, name, html) in cards.items():
    f = OUT / path
    f.parent.mkdir(parents=True, exist_ok=True)
    marker = f'<!-- @dsCard group="{group}" name="{name}" -->\n'
    f.write_text(marker + html)
    print(f"{path}  [{group} / {name}]  {len(html)//1024}K")
print(f"\n{len(cards)} cards -> {OUT}")
