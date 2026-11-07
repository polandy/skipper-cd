// Pre-paint: the configured theme's dark variant is the stylesheet default
// (baked server-side into data-theme/data-server-theme), so only a saved
// per-browser theme override and/or an explicit light-mode preference must be
// applied before first paint to avoid a flash. Loaded as a blocking <script>
// in <head> — its own file rather than inline so the CSP can forbid inline
// script (ADR-0067).
//
// data-server-theme is left untouched — it is the "what is this environment
// actually configured for" reference the mismatch notice compares against.
// The override is honoured only when the theme switcher is enabled
// (data-theme-switcher="on"); with it off the deployed theme is fixed and any
// stale override in localStorage stays dormant.
'use strict';

(function () {
  const el = document.documentElement;
  if (el.getAttribute('data-theme-switcher') === 'on') {
    const override = localStorage.getItem('themeOverride');
    if (override) el.setAttribute('data-theme', override);
  }
  if (localStorage.getItem('colorScheme') === 'light') el.classList.add('light');
  // First-run header tour (T3.15): the glyph-only header teaches its controls
  // once via captions + a banner, then tucks them away for good. Applying the
  // "seen" class here — before paint — keeps returning browsers from flashing
  // the tour and leaves their header layout byte-identical to the no-tour case.
  if (localStorage.getItem('headerTourSeen') === '1') el.classList.add('header-tour-seen');
})();
