(function () {
  // Shared state comes from app-state.js (loaded before this file); S is the
  // store every view reads and applyState / applyPeers / handleEvent below
  // write. The views that have moved out are imported from App.<view>.
  const S = App.state;
  const clog = App.clog;
  const autosync = App.autosync;
  const logs = App.logs;
  const roster = App.roster;
  const {
    renderDisabledStacks,
    renderRoster,
    updateAppLinks,
    updateRosterHealth,
    refreshRosterRow,
    clearRosterFilter,
    presetRosterUpdateFilter,
    countRosterUpdates,
  } = roster;
  const deploys = App.deploys;
  const {
    isDeploying,
    settleInitialState,
    initialStateIsSettled,
    updateStackAffordances,
    updateDeployingIndicator,
    setRunDrawer,
    applyQueueSnapshot,
    applyOrphansSnapshot,
    handleEvent,
    refilterDeploys,
    clearDeployFilter,
    presetDeployStatusFilter,
  } = deploys;
  const hosts = App.hosts;
  const { schedulePeerReflow } = hosts;
  const panels = App.panels;
  const { createLoadError, hookPhaseNode, populateIcon } = panels;
  // What app.js still owes the views already cut out: the chrome, deploy and
  // logs functions they call, under the namespace each will own once it moves.
  // Function declarations hoist, so the stubs can sit ahead of the definitions.
  App.chrome = {
    uiNote,
    registerSurface,
    manageSurfaceFocus,
    trapFocus,
    setViewOptions,
    syncStackSearchBtn,
    renderUpdateBadge,
    jumpToStack,
    flashRow,
    announceOutcome,
    activeView: function () {
      return activeView;
    },
  };
  App.stream = { streamIsOpen, applyHookRun, clearOfflineNotice };
  const loadingState = document.getElementById('loading-state');
  const connDot = document.getElementById('conn-dot');
  const connText = document.getElementById('conn-text');
  const stackSearchBtn = document.getElementById('stack-search-btn');
  const a11yAnnounce = document.getElementById('a11y-announce');
  // ── A11y announcements + UI diagnostics (T2.8) ──

  // announce voices a message through the off-screen polite live region (T2.8).
  // The region is cleared first, then set on the next frame, so two identical
  // consecutive outcomes (e.g. two stacks both "deploy failed") still re-fire.
  function announce(msg) {
    if (!a11yAnnounce || !msg) return;
    a11yAnnounce.textContent = '';
    requestAnimationFrame(function () {
      a11yAnnounce.textContent = msg;
    });
  }
  // The `/api/events` stream replays the deploy-event backlog through the same
  // handler as live events, so announcements must be suppressed until that burst
  // has settled — otherwise every (re)connect would read the whole history aloud
  // (T2.8). Each connect arms a one-shot gate: the replay burst (rendered within
  // a frame or two) lands while it is closed, then it opens and stays open, so
  // every later live deploy announces. Not reset by incoming events — a live
  // deploy arriving soon after connect must not keep pushing the gate shut.
  //
  // The gate's state is mirrored onto the live region as data-announce-ready,
  // so "the replay burst is over" is observable instead of having to be waited
  // out: a test asserts on the attribute rather than sleeping past the timer.
  // uiNote records a diagnostic on the console and keeps a bounded copy on the
  // page. The console alone is not enough: nothing collects it, so a failure in
  // CI (or a user's report) arrives without the reason the UI already knew. The
  // e2e harness reads this buffer when a test fails; keeping it a plain array
  // means no listener has to be attached while the test runs, which matters —
  // subscribing to the console is itself enough to change the timing of the
  // race UC11 is chasing.
  const UI_NOTES_MAX = 50;
  window.__uiNotes = [];
  // level picks the console method, so a by-design drop stays `debug` while a
  // failure is a `warn` — both are recorded either way, because which one
  // explains a bug is not knowable in advance.
  function uiNote(level) {
    const args = Array.prototype.slice.call(arguments, 1);
    console[level].apply(console, args);
    window.__uiNotes.push(
      new Date().toISOString() +
        ' ' +
        args
          .map(function (a) {
            return a instanceof Error ? a.message : String(a);
          })
          .join(' '),
    );
    // Bounded: a page left open for hours must not grow this without limit.
    if (window.__uiNotes.length > UI_NOTES_MAX) window.__uiNotes.shift();
  }
  // The web fonts arriving is the page's one late layout reflow (their swap
  // re-wraps text); timestamping it lets a stray-click note be ordered against
  // the shift that caused it (T8).
  if (document.fonts && document.fonts.ready) {
    document.fonts.ready.then(function () {
      uiNote('debug', 'fonts: settled');
    });
  }

  // ANNOUNCE_SETTLE_MS is how long after a connect the gate stays shut: long
  // enough for the replay burst to render, short enough that a deploy landing
  // right after a reconnect is still voiced.
  const ANNOUNCE_SETTLE_MS = 700;
  let announceReady = false;
  let announceSettleTimer = null;
  // setAnnounceReady moves the gate and its published state together, so the
  // attribute can never disagree with the flag the announce path reads.
  function setAnnounceReady(ready) {
    announceReady = ready;
    if (a11yAnnounce) a11yAnnounce.dataset.announceReady = ready ? '1' : '0';
  }
  // announceOutcome voices a terminal deploy outcome once the gate is open: the
  // post-connect replay burst lands while it is closed, so a reconnect never
  // announces the backlog (T2.8).
  function announceOutcome(message) {
    if (announceReady) announce(message);
  }
  function armAnnounceGate() {
    setAnnounceReady(false);
    clearTimeout(announceSettleTimer);
    announceSettleTimer = setTimeout(function () {
      setAnnounceReady(true);
    }, ANNOUNCE_SETTLE_MS);
  }
  // Publish the closed state at boot, before the first connect arms the gate,
  // so the attribute is never absent while the flag says shut.
  setAnnounceReady(false);

  // ── Row panels, ⋯ menu, app-link popover, icons ── live in app-panels.js
  // (App.panels); their document-level listeners are wired here, at the spot
  // they used to register.
  panels.init();

  // Paint the running-hook phase + pulse the badge on the stack's row in both
  // views; clear it when S.hookRunSnap has no stack.
  function applyHookRun() {
    document.querySelectorAll('.hook-phase').forEach(function (n) {
      n.remove();
    });
    document
      .querySelectorAll('.hooks-badge[data-hook-active], .more-btn[data-hook-active]')
      .forEach(function (b) {
        b.removeAttribute('data-hook-active');
      });
    const hr = S.hookRunSnap;
    if (!hr || !hr.stack) return;

    // Deploys view: the stack's newest row while deploying. The hooks badge now
    // lives inside the collapsed ⋯ menu, so the running-hook pulse rides the
    // visible ⋯ button instead (the badge itself is still marked for when it opens).
    const drow = Array.from(
      document.getElementById('tbody').querySelectorAll('.event-row[data-stack]'),
    ).find(function (r) {
      return r.dataset.stack === hr.stack;
    });
    if (drow && drow.dataset.status === 'deploying') {
      const cell = drow.querySelector('.status-cell');
      if (cell) cell.appendChild(hookPhaseNode(hr));
      // Marked independently: a running hook is reported by the hookrun stream,
      // while the badge only exists once the roster snapshot has carried the
      // stack's hooks. Gating the ⋯ pulse on the badge would drop it whenever
      // the roster lags the hook.
      const b = drow.querySelector('.hooks-badge');
      if (b) b.dataset.hookActive = '1';
      const mb = drow.querySelector('.more-btn');
      if (mb) mb.dataset.hookActive = '1';
    }

    // Stacks view: the roster row while it shows the live deploying state.
    const rlist = document.getElementById('roster-list');
    if (rlist && isDeploying(hr.stack)) {
      const rrow = Array.from(
        rlist.querySelectorAll('.roster-row[data-stack]:not(.peer-row)'),
      ).find(function (r) {
        return r.dataset.stack === hr.stack;
      });
      if (rrow) {
        const rcell = rrow.querySelector('.roster-status');
        if (rcell) rcell.appendChild(hookPhaseNode(hr));
        // The hooks badge sits inline on the roster row, so the running-hook
        // pulse rides the badge itself.
        const rb = rrow.querySelector('.hooks-badge');
        if (rb) rb.dataset.hookActive = '1';
      }
    }
  }

  // registerSurface wires the shared dismiss behaviour for a pop-out surface (a
  // drawer or popover): Escape closes it, and a click outside it closes it. Each
  // surface used to copy-paste its own pair of global document handlers; this
  // centralizes them. `isOpen` reports whether the surface is showing, `close`
  // dismisses it, and `within` lists the elements (the surface plus its trigger)
  // whose clicks are internal and must not dismiss it.
  function registerSurface(opts) {
    document.addEventListener('keydown', function (e) {
      if (e.key === 'Escape') opts.close();
    });
    document.addEventListener('click', function (e) {
      if (!opts.isOpen()) return;
      // composedPath() is captured when the event is dispatched, so it still
      // names the surface even if the click's handler synchronously re-rendered
      // and detached e.target (e.g. toggling a host in the Hosts drawer rebuilds
      // its list) — a plain contains(e.target) would then miss and wrongly close.
      const path = typeof e.composedPath === 'function' ? e.composedPath() : [];
      if (
        opts.within.some(function (el) {
          return el.contains(e.target) || path.indexOf(el) !== -1;
        })
      )
        return;
      opts.close();
    });
  }

  // --- Pop-out focus management (T2.7) -----------------------------------
  // Keyboard users must be pulled into a drawer/popover when it opens and
  // returned to the opener when it closes; the role="dialog" drawers must also
  // trap Tab so it can't wander behind the open panel.
  const FOCUSABLE_SEL =
    'a[href],button:not([disabled]),input:not([disabled]),' +
    'select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])';
  // Upper bound on frames the open-focus keeps re-trying while a surface settles
  // (see manageSurfaceFocus). Comfortably longer than the 0.22s open transition
  // (~14 frames at 60fps) so focus always lands within the animation.
  const FOCUS_SETTLE_FRAMES = 30;
  function focusablesIn(el) {
    return Array.prototype.filter.call(el.querySelectorAll(FOCUSABLE_SEL), function (n) {
      // Skip hidden controls (a collapsed vo-group, a display:none clear button).
      return n.offsetWidth > 0 || n.offsetHeight > 0 || n.getClientRects().length > 0;
    });
  }
  // focusRestsInside reports whether focus already sits on a real control within
  // `surface` — the bare surface fallback (its tabindex=-1 container) doesn't
  // count. Lets the open-focus bow out once the observer has moved focus inside.
  function focusRestsInside(surface) {
    const ae = document.activeElement;
    return !!ae && ae !== surface && surface.contains(ae);
  }
  // manageSurfaceFocus moves focus into `surface` on open and restores it to
  // `opener` on close. Restore only fires when focus is still inside the surface
  // (Escape / keyboard close) — an outside click already moved focus elsewhere,
  // so yanking it back would be wrong.
  //
  // On open the first attempt is synchronous, so focus has landed by the time
  // the caller's "surface is open" await resolves — no observer has to wait out
  // the open animation. Should the surface not be focusable yet (contents
  // rendered at open time, a live list still rebuilding, or visibility:hidden
  // still settling), it retries each frame until focus rests on a real control,
  // bounded by FOCUS_SETTLE_FRAMES. Retrying stops the instant focus is inside —
  // whether we placed it or the user moved there first — so it can neither be
  // lost to the open animation nor yank focus back from where the user went next.
  function manageSurfaceFocus(surface, opener, open) {
    if (open) {
      surface._opener = opener;
      let tries = FOCUS_SETTLE_FRAMES;
      (function settle() {
        if (!surface.classList.contains('open')) return; // toggled off already
        if (focusRestsInside(surface)) return; // observer moved focus in — don't override
        const target = focusablesIn(surface)[0] || surface;
        // preventScroll: the surface is mid-open, so its scroll box is still a
        // sliver — a scroll-into-view here leaves the drawer parked past its own
        // first rows, with the control the viewer came for cut off at the top.
        target.focus({ preventScroll: true });
        if (document.activeElement === target || --tries <= 0) return;
        requestAnimationFrame(settle);
      })();
    } else {
      const ae = document.activeElement;
      const returnFocus = surface._opener && (!ae || ae === document.body || surface.contains(ae));
      const opener = surface._opener;
      surface._opener = null;
      if (returnFocus && opener && typeof opener.focus === 'function')
        opener.focus({ preventScroll: true });
    }
  }
  // trapFocus keeps Tab/Shift+Tab within an open dialog (wraps at the ends).
  function trapFocus(dialog) {
    dialog.addEventListener('keydown', function (e) {
      if (e.key !== 'Tab' || !dialog.classList.contains('open')) return;
      const f = focusablesIn(dialog);
      if (!f.length) {
        e.preventDefault();
        dialog.focus();
        return;
      }
      const first = f[0],
        last = f[f.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    });
  }
  // ── Deploys view ── lives in app-deploys.js (App.deploys); wired here, once
  // the surface registry above exists — the spot its run drawer registered.
  deploys.init();

  setInterval(function () {
    if (!S.absoluteTime) {
      document
        .getElementById('tbody')
        .querySelectorAll('.cell-time')
        .forEach(function (cell) {
          const abs = cell.dataset.ts;
          if (abs) cell.textContent = formatTime(abs);
        });
    }
    // The incident badge's 24h window ages out on the same tick — the stacks
    // snapshot only republishes after runs, and the count must not wait for one.
    renderIncidentBadge();
  }, 30000);

  // EventSource auto-reconnects after a transient drop (readyState CONNECTING),
  // but a *fatal* stream error — a non-2xx response or a bad content-type — puts
  // it in CLOSED for good and it never retries. Without our own retry the
  // indicator would then sit on `reconnecting` forever, so we retry ourselves
  // with a capped backoff that onopen resets once a connection is good.
  const eventsReconnect = makeReconnector(
    function () {
      connect();
    },
    setTimeout,
    clearTimeout,
  );

  // applyPeers ingests a fresh 'peers' snapshot: store it, then let the hosts
  // view apply it end to end.
  function applyPeers(d) {
    S.peersSnap = d || null;
    S.selfHost = (S.peersSnap && S.peersSnap.self) || '';
    hosts.applyPeers();
  }

  // applyState routes one state snapshot (by its state-event name) to the same
  // handling whether it is the baseline the stream sends on (re)connect or a
  // later live state event. State payloads are snapshots (full replacements), so
  // applying one is idempotent. 'deploy' is not here — deploy events are the
  // append-style history, streamed over SSE, not part of the state snapshot.
  function applyState(name, d) {
    switch (name) {
      case 'autosync':
        autosync.applyAutosyncSnapshot(d);
        break;
      case 'queue':
        S.queueSnap = d;
        S.queueByStack = {};
        (S.queueSnap.pending || []).forEach(function (it) {
          S.queueByStack[it.stack] = it;
        });
        applyQueueSnapshot();
        autosync.renderAutosync();
        break;
      case 'upcoming':
        S.upcomingSnap = (d && d.upcoming) || [];
        updateDeployingIndicator();
        break;
      case 'hookrun':
        S.hookRunSnap = d || {};
        applyHookRun();
        break;
      case 'health':
        S.healthSnap = (d && d.stacks) || {};
        updateStackAffordances();
        updateRosterHealth();
        renderHealthAttention(); // beacon + band read the same snapshot
        break;
      case 'healthwatch':
        S.healthwatchSnap = (d && d.stacks) || {};
        break;
      case 'stacks':
        S.disabledSnap = (d && d.disabled) || [];
        S.rosterSnap = (d && d.roster) || [];
        S.repoWebURL = (d && d.repo_web_url) || '';
        S.updatesSnap = (d && d.updates) || null;
        S.incidentsSnap = (d && d.incidents_24h) || [];
        renderDisabledStacks();
        renderRoster();
        updateStackAffordances(); // roster carries the hooks — (de)show the badge
        renderIncidentBadge();
        break;
      case 'app_links':
        S.appLinksSnap = (d && d.stacks) || {};
        updateAppLinks();
        break;
      case 'orphans':
        S.orphansSnap = (d && d.orphans) || [];
        applyOrphansSnapshot();
        break;
      case 'peers':
        applyPeers(d);
        break;
    }
  }

  // ── SSE connection (/api/events) ──

  // The live stream handle, or null before the first connect. Kept out here so
  // the wake-up path (resumeStreams) can tell a surviving stream from a dead one.
  let eventsSource = null;

  const STREAM_OFFLINE_MSG = "Can't reach skipper — the deploy stream is offline.";

  // Once attempts keep failing, the skeleton stops promising a connection and
  // says so. Its spinner and shimmer rows mean "rows are on their way", which is
  // exactly wrong when nothing can reach the server — a page left on a suspended
  // device or off the network would otherwise sit on "Connecting…" forever.
  // Reuses the load-error component rather than inventing a second failure look;
  // its Retry reconnects on the spot instead of making the user reload.
  let offlineNotice = null;

  function showOfflineNotice() {
    if (initialStateIsSettled()) return; // the table is up — the indicator carries it
    if (offlineNotice) offlineNotice.remove(); // replace one spent on "Retrying…"
    offlineNotice = createLoadError(STREAM_OFFLINE_MSG, function () {
      resumeStreams();
    });
    // Beside the skeleton, never inside it: the skeleton is aria-hidden
    // decoration, and this is a real message carrying a real control. A
    // focusable button in an aria-hidden subtree is reachable by keyboard but
    // absent from the accessibility tree.
    loadingState.style.display = 'none';
    loadingState.parentElement.insertBefore(offlineNotice, loadingState.nextSibling);
  }

  function clearOfflineNotice() {
    if (!offlineNotice) return;
    offlineNotice.remove();
    offlineNotice = null;
    // Put the skeleton back if the picture is still unknown — a connection came
    // up and the rows (or the synced marker) are on their way.
    if (!initialStateIsSettled()) loadingState.style.display = '';
  }

  // The stream carries its own baseline: it subscribes, then sends the current
  // state, then the deltas. Reading the baseline over a second channel would
  // reopen the gap a change can vanish into (ADR-0039 amendment). Re-run on
  // every reconnect.
  function connect() {
    connDot.className = 'indicator-dot warn';
    connText.textContent = 'connecting';
    connDot.parentElement.dataset.state = 'connecting';
    connDot.parentElement.title = 'connecting';
    armAnnounceGate(); // re-hydrate silently; the replay burst must not announce (T2.8)
    S.autosyncVersion = null; // the version restarts with the server; the baseline re-seeds it
    openStream();
  }

  function openStream() {
    // Drop any previous stream before opening the next. Usually it is already
    // CLOSED (close() is then a no-op), but a stream a suspension left behind
    // can still be holding a connection — reopening over it would leak one.
    if (eventsSource) eventsSource.close();
    const es = new EventSource('/api/events');
    eventsSource = es; // the wake-up path reads its readyState; handlers keep `es`

    es.onopen = function () {
      eventsReconnect.reset(); // a good connection resets the backoff and "offline"
      clearOfflineNotice();
      connDot.className = 'indicator-dot';
      connText.textContent = 'connected';
      connDot.parentElement.dataset.state = 'connected';
      connDot.parentElement.title = 'connected';
    };

    es.addEventListener('deploy', function (e) {
      const evt = JSON.parse(e.data);
      handleEvent(evt, false);
      refilterDeploys();
      updateStackAffordances(); // a new/mutated row may change which row is newest
      schedulePeerReflow(); // re-slot peer rows below/among the just-inserted local row
      if (activeView === 'stacks') refreshRosterRow(evt.stack); // reflect in-flight/settled state live
    });

    // End-of-replay marker (T4.17): the deploy history has finished replaying.
    // If it carried rows, handleEvent already retired the skeleton; if it was
    // empty, this is what reveals the genuine-empty state (settleInitialState is
    // one-shot, so reconnect replays are no-ops).
    es.addEventListener('synced', function () {
      settleInitialState();
    });

    // Live state changes reuse the same apply path as the snapshot fetch.
    [
      'autosync',
      'queue',
      'upcoming',
      'hookrun',
      'health',
      'healthwatch',
      'stacks',
      'app_links',
      'orphans',
      'peers',
    ].forEach(function (name) {
      es.addEventListener(name, function (e) {
        applyState(name, JSON.parse(e.data));
      });
    });

    es.onerror = function () {
      eventsReconnect.failed();
      if (eventsReconnect.isOffline()) showOfflineNotice();
      connDot.className = 'indicator-dot err';
      connText.textContent = 'reconnecting';
      connDot.parentElement.dataset.state = 'reconnecting';
      connDot.parentElement.title = 'reconnecting';
      // CLOSED means the browser gave up (fatal error) and will not retry on its
      // own — schedule our own reconnect. CONNECTING means it is already retrying,
      // so leave it be and let onopen recover the indicator.
      if (es.readyState === EventSource.CLOSED) eventsReconnect.schedule();
    };
  }

  // ── Stacks view ── lives in app-roster.js (App.roster); wired here, at the
  // spot the block occupied, so its type-to-search stays behind the deploys one.
  roster.init();

  // What the trigger below searches depends on the view, so its label does too.
  const SEARCH_LABEL_STACKS = 'Search stacks';
  const SEARCH_LABEL_LOG = 'Search in log';

  // T3.11 — always-visible search trigger. The stack filter used to be
  // desktop-invisible (type-to-search only, an easter egg); this header magnifier
  // opens the same bar for the active view — on Logs too, where it opens the
  // in-log search: one magnifier, same place, whichever view is up. Hidden only
  // on mobile (the popover entry covers it there) via CSS. syncStackSearchBtn
  // reflects the bar's open state on the trigger.
  function syncStackSearchBtn() {
    const onLogs = activeView === 'logs';
    const open = onLogs
      ? logs.logSearchIsOpen()
      : activeView === 'stacks'
        ? roster.filterRevealed()
        : deploys.filterRevealed();
    stackSearchBtn.classList.toggle('active', open);
    stackSearchBtn.setAttribute('aria-expanded', String(open));
    const label = onLogs ? SEARCH_LABEL_LOG : SEARCH_LABEL_STACKS;
    stackSearchBtn.title = label;
    stackSearchBtn.setAttribute('aria-label', label);
  }
  stackSearchBtn.addEventListener('click', function () {
    if (activeView === 'deploys') {
      if (deploys.filterRevealed()) clearDeployFilter(true);
      else deploys.openFilter();
    } else if (activeView === 'stacks') {
      if (roster.filterRevealed()) clearRosterFilter(true);
      else {
        roster.openFilter();
      }
    } else if (activeView === 'logs') {
      logs.logSearchToggle();
    }
  });

  // ── Theme toggle ──

  // Colour-scheme toggle (dark is the default; light is opt-in per browser).
  // Which palette (Catppuccin/Nord/Solarized/Gruvbox/Rosé Pine) is configured
  // server-side via ui_theme and baked into <html data-theme> — this toggle
  // only ever flips dark/light within that palette.
  const themeToggle = document.getElementById('theme-toggle');
  let lightTheme = localStorage.getItem('colorScheme') === 'light';

  function applyTheme() {
    document.documentElement.classList.toggle('light', lightTheme);
    themeToggle.classList.toggle('active', lightTheme);
    themeToggle.title = lightTheme ? 'Switch to dark theme' : 'Switch to light theme';
  }

  themeToggle.addEventListener('click', function () {
    lightTheme = !lightTheme;
    localStorage.setItem('colorScheme', lightTheme ? 'light' : 'dark');
    applyTheme();
  });

  applyTheme();

  // Theme picker — this browser only, and opt-in. It is wired up only when the
  // deployment enables the switcher (data-theme-switcher="on", baked in from
  // ui_theme_switcher); otherwise the picker is hidden, no override is applied,
  // and the block below never runs. Choosing a theme here never changes the
  // environment's configured ui_theme (data-server-theme, baked in at serve
  // time); it only persists a local override applied instantly (no reload:
  // every theme's CSS is always present, switching data-theme is enough).
  // Picking the server's own theme clears the override, so the page goes back
  // to following whatever the environment is configured for.
  if (document.documentElement.getAttribute('data-theme-switcher') === 'on') {
    const THEME_LABELS = {
      catppuccin: 'Catppuccin',
      nord: 'Nord',
      solarized: 'Solarized',
      gruvbox: 'Gruvbox',
      'rose-pine': 'Rosé Pine',
      flake: 'Flake',
    };
    const serverTheme = document.documentElement.getAttribute('data-server-theme');
    const themeSelect = document.getElementById('theme-select');
    const themeNotice = document.getElementById('theme-notice');
    const themeNoticeText = document.getElementById('theme-notice-text');
    let themeNoticeTimer = null;

    themeSelect.value = document.documentElement.getAttribute('data-theme');

    function updateThemeNotice() {
      clearTimeout(themeNoticeTimer);
      const override = localStorage.getItem('themeOverride');
      if (!override || override === serverTheme) {
        themeNotice.hidden = true;
        return;
      }
      themeNoticeText.textContent =
        'Showing ' +
        (THEME_LABELS[override] || override) +
        ' in this browser — this environment is configured for ' +
        (THEME_LABELS[serverTheme] || serverTheme) +
        '.';
      themeNotice.hidden = false;
      themeNoticeTimer = setTimeout(function () {
        themeNotice.hidden = true;
      }, 6000);
    }

    themeSelect.addEventListener('change', function () {
      const chosen = themeSelect.value;
      document.documentElement.setAttribute('data-theme', chosen);
      if (chosen === serverTheme) {
        localStorage.removeItem('themeOverride');
      } else {
        localStorage.setItem('themeOverride', chosen);
      }
      updateThemeNotice();
    });

    document.getElementById('theme-notice-close').addEventListener('click', function () {
      clearTimeout(themeNoticeTimer);
      themeNotice.hidden = true;
    });

    updateThemeNotice(); // show on load if a saved override already differs from the server theme
  }

  // ── First-run header tour (T3.15) ──
  // The header is glyph-only by design; on a fresh browser only, captions under
  // each control plus the banner teach the mapping. "Got it" (or Esc) marks it
  // seen — the class hides the captions/banner and persists so it never returns.
  // Purely localStorage-gated (no timers), so the shown/dismissed states are
  // deterministic. Returning browsers get .header-tour-seen pre-paint (see head).
  (function () {
    const root = document.documentElement;
    const dismiss = document.getElementById('header-tour-dismiss');
    if (!dismiss || root.classList.contains('header-tour-seen')) return;
    function endTour() {
      if (root.classList.contains('header-tour-seen')) return;
      root.classList.add('header-tour-seen');
      try {
        localStorage.setItem('headerTourSeen', '1');
      } catch (e) {
        // Private-mode storage can throw; the class already hid the tour for
        // this session, so log and carry on rather than leaving it half-applied.
        uiNote('warn', 'could not persist headerTourSeen', e);
      }
      // Keyboard users lose the vanished button; land them back in the header.
      const nav = document.querySelector('.view-toggle button.active');
      if (nav) nav.focus();
    }
    dismiss.addEventListener('click', endTour);
    document.addEventListener('keydown', function (e) {
      if (e.key === 'Escape') endTour();
    });
  })();

  // ── Autosync controls ── live in app-autosync.js (App.autosync); wired here,
  // at the spot the block occupied, once the surfaces it registers with exist.
  autosync.init();

  // ── Hosts drawer ── lives in app-hosts.js (App.hosts); wired here, at the
  // spot the block occupied.
  hosts.init();

  // ── Logs view ── lives in app-logs.js (App.logs); wired here, at the spot
  // the block occupied, once the view state below exists.
  const viewButtons = document.querySelectorAll('#view-toggle button');
  const savedView = localStorage.getItem('activeView');
  let activeView = savedView === 'logs' || savedView === 'stacks' ? savedView : 'deploys';
  logs.init();

  // ── Stream wake-up ──

  // An OS that freezes a backgrounded tab — an installed PWA, a locked phone —
  // tears the streams down and drops or throttles the retry timer that would
  // have rebuilt them, so a page can come back to the foreground stuck on
  // `reconnecting` with nothing left to wake it. The wake-up drives the
  // reconnect itself instead of trusting that timer. `online` is the same
  // recovery for the case where the network, not the tab, was what went away.

  function streamIsOpen(src) {
    return src !== null && src.readyState === EventSource.OPEN;
  }

  function resumeStreams() {
    eventsReconnect.resume(streamIsOpen(eventsSource));
    logs.resume();
  }

  document.addEventListener('visibilitychange', function () {
    if (document.visibilityState === 'visible') resumeStreams();
  });
  window.addEventListener('online', resumeStreams);

  // ── View switching ──

  function applyView() {
    clog.close(); // a container-log panel belongs to its row; drop it on a view switch
    if (activeView !== 'logs') logs.exitLogFullscreen(); // it would overlay the new view
    document.body.classList.toggle('view-logs', activeView === 'logs');
    document.body.classList.toggle('view-stacks', activeView === 'stacks');
    viewButtons.forEach(function (btn) {
      btn.classList.toggle('active', btn.dataset.view === activeView);
    });
    if (activeView === 'logs') {
      logs.connectLogs();
      logs.renderLogWindow();
    }
    if (activeView === 'stacks') renderRoster();
    renderUpdateBadge(); // its .active state is view-dependent (Stacks only)
    syncStackSearchBtn(); // the trigger is view-aware + logs-hidden (T3.11)
  }

  // View-specific options (time mode / sort / follow) live in a popover behind
  // the view buttons so the header never gains or loses controls on a view
  // switch. Clicking the already-active view button toggles that popover;
  // clicking the other button switches views (and closes the popover).
  const viewSwitch = document.getElementById('view-switch');
  const viewOptions = document.getElementById('view-options');

  function setViewOptions(open) {
    if (open) {
      autosync.setDrawer(false);
      setRunDrawer(false);
    } // surfaces are mutually exclusive
    viewOptions.classList.toggle('open', open);
    let activeBtn = null;
    viewButtons.forEach(function (btn) {
      // Keep aria-expanded honest on every button — only the active one can be
      // open, the rest are always false — so the caret cue (CSS keys off it)
      // never reads "open" on a stale non-active button.
      const isActive = btn.dataset.view === activeView;
      btn.setAttribute('aria-expanded', String(isActive && open));
      if (isActive) activeBtn = btn;
    });
    // role=menu popover: focus the first option on open, return to the active
    // view button on keyboard close (no hard trap — Escape closes it). (T2.7)
    manageSurfaceFocus(viewOptions, activeBtn, open);
  }

  viewButtons.forEach(function (btn) {
    btn.addEventListener('click', function () {
      if (btn.dataset.view === activeView) {
        // Active button toggles its options popover.
        if (document.querySelector(`.vo-group[data-view="${activeView}"]`)) {
          setViewOptions(!viewOptions.classList.contains('open'));
        }
        return;
      }
      activeView = btn.dataset.view;
      localStorage.setItem('activeView', activeView);
      setViewOptions(false);
      applyView();
    });
  });

  // jumpToStack drives the cross-view jump-btn (deploys <-> stacks): switch to
  // targetView, then scroll to and briefly flash that stack's row there. In
  // the deploys view a stack can have many rows (it's an event log), so the
  // newest one — first in DOM order — is the landing target; the stacks view
  // has exactly one row per stack (it's inventory). Silently does nothing
  // past the view switch when the stack has no row in the target view (e.g.
  // "never deployed"). Clears the target view's own search filter first: a
  // leftover query from before the jump could otherwise leave the landing row
  // `.filtered-out` (display: none) — switched to, but invisible and with no
  // indication why.
  function jumpToStack(targetView, stack) {
    if (targetView !== activeView) {
      activeView = targetView;
      localStorage.setItem('activeView', activeView);
      setViewOptions(false);
      applyView();
    }
    if (targetView === 'stacks') clearRosterFilter(true);
    else clearDeployFilter(true);
    const escaped = window.CSS && CSS.escape ? CSS.escape(stack) : stack;
    const target =
      targetView === 'stacks'
        ? document
            .getElementById('roster-list')
            .querySelector(`.roster-row[data-stack="${escaped}"]`)
        : document.getElementById('tbody').querySelector(`.event-row[data-stack="${escaped}"]`);
    if (!target) return;
    flashRow(target);
  }

  // flashRow scrolls a row into view and flashes the accent jump highlight —
  // shared by the cross-view jump and the retry note's rollback landing.
  function flashRow(target) {
    target.scrollIntoView({ behavior: 'smooth', block: 'center' });
    target.classList.remove('jump-target');
    void target.offsetWidth; // restart the animation if it's already landed once
    target.classList.add('jump-target');
    setTimeout(function () {
      target.classList.remove('jump-target');
    }, 1800);
  }

  // ─── Live-health attention surface (ADR-0027 extension) ───
  // Two always-consistent affordances that lift a currently-unhealthy stack out
  // of the chronological deploy log — where its row-bound health pill can sit far
  // down, or vanish once the row ages out of the bounded log: a header BEACON
  // (present in every view) and, in the Deploys view, an attention BAND pinned
  // above the log. Both render from attentionStacks(S.healthSnap) and jump to the
  // stack's newest row on activate (degrading to a plain view switch when the
  // stack has no row, exactly like jumpBtn).
  // ─── Incident badge (UI_SPEC.md "Incident badge") ───
  // The beacon answers "what is unhealthy NOW"; this badge answers "what went
  // wrong RECENTLY" — the axis a recovered rollback disappears on. Counts
  // S.incidentsSnap (declared with the other 'stacks' snapshot state above),
  // re-filtered against the window on the client clock (the 30s tick), and
  // lands on the Deploys view with the bad-outcome status chips pre-selected.
  const BAD_OUTCOME_STATUSES = ['failed', 'rolled_back', 'rolled_back_unhealthy', 'heal_exhausted'];
  const incidentBadge = document.getElementById('incident-badge');
  const incidentBadgeCount = document.getElementById('incident-badge-count');

  function renderIncidentBadge() {
    const n = recentIncidentCount(S.incidentsSnap, Date.now());
    incidentBadge.hidden = n === 0;
    if (n === 0) return;
    incidentBadgeCount.textContent = String(n);
    const label = incidentBadgeLabel(n);
    incidentBadge.title = label;
    incidentBadge.setAttribute('aria-label', label);
  }

  incidentBadge.addEventListener('click', function () {
    // Toggle: a second click on the badge, with its own preset still the whole
    // filter, takes the filter back off rather than re-applying it.
    if (activeView === 'deploys' && deploys.incidentPresetIsActive(BAD_OUTCOME_STATUSES)) {
      clearDeployFilter(true);
      return;
    }
    if (activeView !== 'deploys') {
      activeView = 'deploys';
      localStorage.setItem('activeView', activeView);
      setViewOptions(false);
      applyView();
    }
    presetDeployStatusFilter(BAD_OUTCOME_STATUSES);
  });

  // ─── Update badge (UI_SPEC.md "Updates filter") ───
  // The registry check (ADR-0054) marks individual version chips; this badge is
  // the fleet-level answer — how many stacks are waiting on one — and the only
  // always-visible way into the Stacks view's updates-only filter, which would
  // otherwise sit behind a filter bar nobody opens. It counts the rendered
  // roster rows rather than the snapshot, so the number is always the number of
  // rows the filter hands back. A filter, never a trigger: applying an update
  // stays a git commit.
  const updateBadge = document.getElementById('update-badge');
  const updateBadgeCount = document.getElementById('update-badge-count');

  function renderUpdateBadge() {
    const n = countRosterUpdates();
    updateBadge.hidden = n === 0;
    updateBadge.classList.toggle(
      'active',
      activeView === 'stacks' && roster.updateFilterPresetActive(),
    );
    if (n === 0) return;
    updateBadgeCount.textContent = String(n);
    const label = updateBadgeLabel(n);
    updateBadge.title = label;
    updateBadge.setAttribute('aria-label', label);
  }

  updateBadge.addEventListener('click', function () {
    // Toggle: a second click, with its own preset still the whole filter, takes
    // the narrowing back off rather than re-applying it.
    if (activeView === 'stacks' && roster.updateFilterPresetActive()) {
      clearRosterFilter(true);
      return;
    }
    if (activeView !== 'stacks') {
      activeView = 'stacks';
      localStorage.setItem('activeView', activeView);
      setViewOptions(false);
      applyView();
    }
    presetRosterUpdateFilter();
  });

  const healthBeaconWrap = document.getElementById('health-beacon-wrap');
  const healthBeacon = document.getElementById('health-beacon');
  const healthBeaconIcon = healthBeacon.querySelector('.hb-icon');
  const healthBeaconCount = document.getElementById('health-beacon-count');
  const beaconPop = document.getElementById('beacon-pop');
  const attentionBand = document.getElementById('attention-band');
  healthBeaconIcon.innerHTML = WARN_ICON;

  function setBeaconPop(open) {
    beaconPop.classList.toggle('open', open);
    healthBeacon.setAttribute('aria-expanded', String(open));
  }

  function renderHealthBeacon(att) {
    const n = att.length;
    healthBeacon.hidden = n === 0;
    // Collapse the wrapper too, so a hidden beacon leaves no status-area gap.
    healthBeaconWrap.hidden = n === 0;
    if (n === 0) {
      setBeaconPop(false);
      beaconPop.innerHTML = '';
      return;
    }
    const label = attentionLabel(n);
    healthBeaconCount.textContent = String(n);
    healthBeacon.title = label;
    healthBeacon.setAttribute('aria-label', label);
    beaconPop.innerHTML = beaconPopHTML(att);
  }

  function renderAttentionBand(att) {
    const n = att.length;
    attentionBand.hidden = n === 0;
    if (n === 0) {
      attentionBand.innerHTML = '';
      return;
    }
    attentionBand.innerHTML = attentionBandHTML(att);
    attentionBand.querySelectorAll('.attention-row').forEach(function (row) {
      populateIcon(row.querySelector('.stack-icon'), row.dataset.stack);
    });
  }

  // Single entry point, called from applyState's 'health' case — the snapshot
  // bootstrap and the live SSE stream both route through it.
  function renderHealthAttention() {
    const att = attentionStacks(S.healthSnap);
    renderHealthBeacon(att);
    renderAttentionBand(att);
  }

  healthBeacon.addEventListener('click', function (e) {
    e.stopPropagation();
    setBeaconPop(!beaconPop.classList.contains('open'));
  });
  beaconPop.addEventListener('click', function (e) {
    const item = e.target.closest('.beacon-item');
    if (!item) return;
    setBeaconPop(false);
    // Land in the current list view when it has stack rows (Stacks -> roster
    // row), else fall back to Deploys — so a jump from Stacks doesn't yank the
    // user out of the view they're triaging in.
    jumpToStack(activeView === 'stacks' ? 'stacks' : 'deploys', item.dataset.stack);
  });
  attentionBand.addEventListener('click', function (e) {
    const row = e.target.closest('.attention-row');
    if (!row) return;
    jumpToStack('deploys', row.dataset.stack);
  });

  registerSurface({
    isOpen: function () {
      return beaconPop.classList.contains('open');
    },
    close: function () {
      setBeaconPop(false);
    },
    within: [healthBeacon, beaconPop],
  });

  registerSurface({
    isOpen: function () {
      return viewOptions.classList.contains('open');
    },
    close: function () {
      setViewOptions(false);
    },
    within: [viewSwitch], // the switch wraps both buttons and the options popover
  });

  logs.renderLogQuickChips(); // paint the persisted filter before the first lines land
  logs.applyFollow();
  applyView();

  // Paint the deployed skipper-cd build identity in the header once on load.
  // Feature-branch builds show the branch (which release semver alone cannot
  // distinguish); release/Nix/dev builds show the version. The short commit is
  // appended whenever the build path knows it.
  fetch('/api/version')
    .then(function (r) {
      return r.json();
    })
    .then(function (d) {
      if (!d || !d.version) return;
      const el = document.getElementById('brand-version');
      if (!el) return;
      const base = d.version === 'dev' ? 'dev' : 'v' + d.version;
      const head = d.branch && d.branch !== 'main' ? d.branch : base;
      const text = d.commit ? head + ' · ' + d.commit : head;
      el.textContent = text;
      // The label may be clipped (ellipsis) on a long branch name, and it is
      // hidden entirely below 1000px — so the identity also rides the logo,
      // where a hover tooltip or a tap-tip reaches it at every width.
      el.title = text;
      const icon = document.querySelector('.brand-icon');
      if (icon) icon.title = text;
    })
    .catch(function () {});

  // ── Tap-tips: touch tooltips (data-taptip opt-in) ──

  // Tap-reveal tooltips for touch (titles never show on touch): a tap flashes
  // the title in a bubble. Any control under a `data-taptip` ancestor (or
  // carrying the attribute itself) opts in; mouse/pen keep the native tooltip.
  (function setupTapTips() {
    const tip = document.createElement('div');
    tip.className = 'tap-tip';
    tip.setAttribute('aria-hidden', 'true');
    document.body.appendChild(tip);
    let hideTimer = null;
    function hide() {
      tip.classList.remove('show');
    }
    function flash(el) {
      const text = el.getAttribute('title');
      if (!text) return;
      tip.textContent = text;
      // Render off-screen first to measure, then centre under the control and
      // clamp to the viewport so it never overflows the edges.
      tip.style.left = '-9999px';
      tip.style.top = '0px';
      tip.classList.add('show');
      const r = el.getBoundingClientRect();
      const margin = 6;
      let left = r.left + r.width / 2 - tip.offsetWidth / 2;
      left = Math.max(margin, Math.min(left, window.innerWidth - tip.offsetWidth - margin));
      tip.style.left = left + 'px';
      tip.style.top = r.bottom + 6 + 'px';
      clearTimeout(hideTimer);
      hideTimer = setTimeout(hide, 1600);
    }
    document.addEventListener(
      'pointerdown',
      function (e) {
        if (e.pointerType && e.pointerType !== 'touch') return; // mouse/pen: native tooltip
        const el = e.target.closest && e.target.closest('[title]');
        if (!el) return;
        if (el.closest('.view-options')) return; // popover rows already show visible labels
        if (!el.closest('[data-taptip]')) return; // deliberate opt-in, not "any [title]"
        flash(el);
      },
      { passive: true },
    );
    window.addEventListener('scroll', hide, { passive: true });
  })();

  connect();

  // ── PWA: service worker + update prompt (ADR-0023) ──

  // Register the service worker for installability + app-shell caching, and
  // prompt to reload when a newer version has been deployed. Failure-tolerant:
  // an insecure context (plain HTTP) simply skips it, the page keeps working as
  // a normal site. See docs/pwa.md.
  if ('serviceWorker' in navigator) {
    window.addEventListener('load', function () {
      registerServiceWorker();
    });
  }

  // The update banner: shown once a new service worker has installed and is
  // waiting. Tapping Reload asks that worker to activate; the resulting
  // controllerchange reloads the page onto the fresh shell.
  // Deduped by worker identity: the same waiting worker never re-prompts (its
  // statechange and the on-load reg.waiting check can both reach here), but a
  // later deploy's new worker can prompt again even after a dismissal.
  let promptedWorker = null;
  // Set when the user accepts the prompt. Together with a worker already
  // controlling the page at load, it tells a real version handover (reload onto
  // it) apart from the first-install clients.claim() (must not bounce a fresh
  // visit) — see the controllerchange listener.
  let updateAccepted = false;
  function showUpdateBanner(worker) {
    if (promptedWorker === worker) return;
    promptedWorker = worker;
    const banner = document.getElementById('update-banner');
    const reload = document.getElementById('update-banner-reload');
    const close = document.getElementById('update-banner-close');
    reload.onclick = function () {
      updateAccepted = true;
      reload.disabled = true;
      worker.postMessage({ type: 'SKIP_WAITING' });
    };
    close.onclick = function () {
      banner.hidden = true;
    };
    banner.hidden = false;
  }

  function registerServiceWorker() {
    // Reload once when a waiting worker takes over: either the user accepted the
    // prompt (here or, via hadController, in another tab). A controllerchange
    // with neither — no controller at load and no acceptance — is just the
    // first-install clients.claim(), which must not bounce a fresh visit.
    const hadController = !!navigator.serviceWorker.controller;
    let reloading = false;
    navigator.serviceWorker.addEventListener('controllerchange', function () {
      if (reloading || (!updateAccepted && !hadController)) return;
      reloading = true;
      window.location.reload();
    });

    navigator.serviceWorker
      .register('/sw.js')
      .then(function (reg) {
        // A worker that finished installing while the page was closed is already
        // waiting on load — updatefound won't fire for it, so check explicitly.
        if (reg.waiting && navigator.serviceWorker.controller) {
          showUpdateBanner(reg.waiting);
        }
        reg.addEventListener('updatefound', function () {
          const sw = reg.installing;
          if (!sw) return;
          sw.addEventListener('statechange', function () {
            // installed + an existing controller ⇒ an update is ready; without a
            // controller this is the first install, which needs no prompt.
            if (sw.state === 'installed' && navigator.serviceWorker.controller) {
              showUpdateBanner(sw);
            }
          });
        });
        // Check for a new worker now and whenever the tab regains focus, so a
        // long-lived standalone PWA notices a deploy without a manual reload.
        reg.update().catch(function () {});
        document.addEventListener('visibilitychange', function () {
          if (document.visibilityState === 'visible') reg.update().catch(function () {});
        });
      })
      .catch(function () {});
  }

  // ── Logs view controls ── live in app-logs.js; wired here, at the spot the
  // block occupied, so its document-level key handler keeps its place in line.
  logs.initControls();

  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') clog.escape();
  });
})();
