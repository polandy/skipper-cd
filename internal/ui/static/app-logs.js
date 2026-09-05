// app-logs.js — the Logs view (the skipper log stream, its bounded buffer and
// sliding window, quick filters, the "N new lines" pill, follow mode and the
// inline search/wrap/fullscreen controls), cut out of app.js by view
// (ADR-0035 amendment) and attached as App.logs.
//
// The log stream connects lazily on first activation and stays open;
// the connection indicator remains bound to /api/events.
//
// Loads before app.js, so what it needs from the chrome, the panels and the
// stream (App.chrome, App.panels, App.stream) is read at call time, never
// imported at load. app.js calls init() and initControls() at the two spots
// the wiring used to run, so listener registration order is unchanged.
App.logs = (function () {
  // Hooks into the Logs panel's own wiring, which is set up further down the
  // file. Declared here, ahead of their first use, so no earlier call can hit
  // them uninitialised; each is inert until then.
  let logSearchIsOpen = function () {
    return false;
  };
  let logSearchToggle = function () {};
  // Re-applies the in-log filter after a re-render, so fresh lines obey it.
  let logSearchApply = function () {};
  // Drops the Logs panel out of fullscreen. A view switch calls it because
  // fullscreen is a viewport-filling overlay: left on, it covers whichever view
  // the viewer moved to.
  let exitLogFullscreen = function () {};

  const logPane = document.getElementById('log-pane');
  const followBtn = document.getElementById('follow-logs');

  // The log view keeps a bounded client-side buffer (the backend replays at
  // most its own ring on connect) and renders a sliding window of it. The
  // order is chronological — newest line at the bottom, like `journalctl -f`
  // and like the container-log panel this view borrows its chrome from, so
  // "auto-scroll" points at the edge the tail is actually on. The window
  // starts at logPageSize lines and grows by logPageSize each time the user
  // scrolls to the older (top) edge.
  const logPageSize = 500;
  const maxLogBuffer = 2000;
  // Outcome lines kept past buffer eviction (mirrors internal/logbuf's pinned
  // set), so a busy run's child output can never push the line that says how a
  // deploy ended out of the pane before it was read.
  const maxPinnedLog = 50;
  const logEntries = []; // chronological, oldest → newest
  const pinnedLogEntries = []; // narrated outcome lines, exempt from eviction

  // The render window reads this merged view — evicted outcome lines back in
  // chronological position ahead of the ring — never logEntries directly.
  function logView() {
    return mergeLogView(pinnedLogEntries, logEntries);
  }
  let logVisible = logPageSize; // number of newest entries currently rendered
  let followLogs = localStorage.getItem('followLogs') !== 'false';
  let logSource = null;

  // ── Logs-view quick filters ──
  // Severity threshold + kind set + stack set, persisted per browser so a
  // narrowed view survives a reload (and a PWA relaunch). Kept in one object
  // so persisting is one write and the pure predicate takes one argument.
  const LOG_FILTERS_KEY = 'logQuickFilters';
  let logFilters = parseLogFilters(localStorage.getItem(LOG_FILTERS_KEY));
  // Lines that arrived while auto-scroll was off, so the "N new lines" pill
  // can offer to jump without moving the viewport under the reader.
  let pendingLogLines = 0;

  function renderLogLine(entry) {
    const line = document.createElement('div');
    line.className = 'log-line';
    line.dataset.testid = 'log-line';
    line.dataset.level = logLineLevel(entry);
    // The quick filters test these three facets; stashing them here keeps a
    // re-filter a DOM walk instead of a second pass over the buffer.
    const facets = logFacets(entry);
    line.dataset.sev = facets.level;
    line.dataset.kind = facets.kind;
    if (facets.stack) line.dataset.stack = facets.stack;
    if (!logMatchesFilters(facets, logFilters)) line.classList.add('log-out');
    line.innerHTML = logLineHTML(entry);
    // A changed file carries its diff on the record (the console prints it the
    // same way); render it under the line so the two surfaces read alike. The
    // capture layer has already clamped it, so the block is bounded.
    const attrs = entry.attrs || {};
    if (attrs.diff) line.insertAdjacentHTML('beforeend', logDiffBlockHTML(attrs.diff));
    return line;
  }

  // handleDiffToggle handles the two collapse/expand controls every diff panel
  // carries — the per-file header and the multi-commit "N commits" pill —
  // wherever the panel is rendered (deploy table or log view). Returns true when
  // it consumed the click so the caller stops. Shared by both click handlers.
  function handleDiffToggle(target) {
    const fileHeader = target.closest('.diff-file-header');
    if (fileHeader) {
      fileHeader.classList.toggle('expanded');
      return true;
    }
    const cpill = target.closest('.commits-pill');
    if (cpill) {
      const chead = cpill.closest('.diff-head');
      const clist = chead && chead.querySelector('.diff-commit-list');
      if (clist) clist.classList.toggle('expanded');
      return true;
    }
    return false;
  }

  // The newest line lives at the bottom; "follow" and fresh renders pin the
  // view to that edge.
  function scrollToNewest() {
    logPane.scrollTop = logPane.scrollHeight;
    clearPendingLines();
  }

  // ── Quick filters ──
  const logQuickBar = document.getElementById('log-quick-bar');
  const logQuickCount = document.getElementById('log-quick-count');
  const logStackChips = document.getElementById('log-stack-chips');
  const logStackSep = document.getElementById('log-stack-sep');
  const logNewPill = document.getElementById('log-newpill');

  function persistLogFilters() {
    try {
      localStorage.setItem(LOG_FILTERS_KEY, JSON.stringify(logFilters));
    } catch (_) {
      // Private mode / quota: the filter still applies for this session.
    }
  }

  // Hide or reveal every rendered line for the current filters, then refresh
  // the count. `.log-out` is the quick filters' own hide class, separate from
  // the in-log search's — the two narrow independently and a line must stay
  // hidden while either one excludes it.
  function applyLogQuickFilters() {
    logPane.querySelectorAll('.log-line').forEach(function (line) {
      const facets = {
        level: line.dataset.sev,
        kind: line.dataset.kind,
        stack: line.dataset.stack || '',
      };
      line.classList.toggle('log-out', !logMatchesFilters(facets, logFilters));
    });
    updateLogQuickCount();
  }

  function updateLogQuickCount() {
    const lines = logPane.querySelectorAll('.log-line');
    if (!logFiltersActive(logFilters)) {
      logQuickCount.textContent = '';
      return;
    }
    const shown = logPane.querySelectorAll('.log-line:not(.log-out)').length;
    logQuickCount.textContent = shown + ' of ' + lines.length;
  }

  function renderLogQuickChips() {
    logQuickBar.querySelectorAll('[data-sev]').forEach(function (b) {
      const on = b.dataset.sev === logFilters.sev;
      b.classList.toggle('active', on);
      b.setAttribute('aria-pressed', String(on));
    });
    logQuickBar.querySelectorAll('[data-kind]').forEach(function (b) {
      const on = logFilters.kinds.indexOf(b.dataset.kind) !== -1;
      b.classList.toggle('active', on);
      b.setAttribute('aria-pressed', String(on));
    });
    logStackChips.textContent = '';
    logFilters.stacks.forEach(function (name) {
      const chip = document.createElement('button');
      chip.type = 'button';
      chip.className = 'clog-chip active stack-chip';
      chip.dataset.testid = 'log-stack-chip';
      chip.dataset.stack = name;
      chip.title = 'Remove this stack filter';
      chip.append(name);
      const x = document.createElement('span');
      x.className = 'chip-x';
      x.setAttribute('aria-hidden', 'true');
      x.textContent = '×';
      chip.appendChild(x);
      logStackChips.appendChild(chip);
    });
    logStackSep.hidden = logFilters.stacks.length === 0;
  }

  // One entry point for every filter change: persist, repaint the chips,
  // re-filter what is rendered. Following the tail keeps following it — the
  // pane's position should not depend on which filter was last touched.
  function setLogFilters(next) {
    logFilters = next;
    persistLogFilters();
    renderLogQuickChips();
    applyLogQuickFilters();
    if (followLogs) scrollToNewest();
  }

  function toggleLogStackFilter(name) {
    const stacks = logFilters.stacks.slice();
    const i = stacks.indexOf(name);
    if (i === -1) stacks.push(name);
    else stacks.splice(i, 1);
    setLogFilters(Object.assign({}, logFilters, { stacks: stacks }));
  }

  // ── "N new lines" pill ──
  function countPendingLine() {
    pendingLogLines++;
    logNewPill.textContent =
      '↓ ' + pendingLogLines + (pendingLogLines === 1 ? ' new line' : ' new lines');
    logNewPill.hidden = false;
  }

  function clearPendingLines() {
    pendingLogLines = 0;
    logNewPill.hidden = true;
  }

  // setFollowLogs is the one place follow changes: it persists the choice and
  // repaints the control. keepScroll is for the scroll handler itself — it
  // already knows where the pane is, and jumping there again from inside a
  // scroll event would fight the user's own scrolling.
  function setFollowLogs(on, opts) {
    if (followLogs === on) return;
    followLogs = on;
    localStorage.setItem('followLogs', String(followLogs));
    if (opts && opts.keepScroll) {
      followBtn.classList.toggle('on', followLogs);
      followBtn.setAttribute('aria-pressed', String(followLogs));
      clearPendingLines(); // reaching the tail by scrolling settles the pill too
      return;
    }
    applyFollow();
  }

  // Remove a rendered line together with any diff/files panel opened below it,
  // so trimming the window never leaves an orphaned panel behind.
  function removeLogLine(line) {
    const next = line.nextElementSibling;
    if (next && (next.classList.contains('diff-panel') || next.classList.contains('files-list'))) {
      next.remove();
    }
    line.remove();
  }

  // Keep at most logVisible rendered lines, dropping from the older (top)
  // edge. The dropped entries stay in the buffer, so scrolling back can
  // reveal them again.
  function trimLogDom() {
    const lines = logPane.querySelectorAll('.log-line');
    const over = lines.length - logVisible;
    for (let i = 0; i < over; i++) {
      removeLogLine(lines[i]);
    }
  }

  // Rebuild the pane from the buffer for the current order and window size.
  // Used on view activation, sort toggle and other non-incremental changes.
  function renderLogWindow() {
    logPane.innerHTML = '';
    const view = logView();
    if (view.length === 0) {
      const empty = document.createElement('div');
      empty.className = 'log-empty';
      empty.id = 'log-empty';
      empty.textContent = 'Awaiting log output...';
      logPane.appendChild(empty);
      return;
    }
    const count = Math.min(logVisible, view.length);
    const slice = view.slice(view.length - count); // oldest → newest
    const frag = document.createDocumentFragment();
    slice.forEach(function (e) {
      frag.appendChild(renderLogLine(e));
    });
    logPane.appendChild(frag);
    applyLogQuickFilters();
    scrollToNewest();
    logSearchApply();
  }

  // Reveal another page of older entries at the older (top) edge, preserving
  // the reading position. No-op once the whole buffer is rendered.
  function loadMoreLogs() {
    const view = logView();
    const have = Math.min(logVisible, view.length);
    const target = Math.min(logVisible + logPageSize, view.length);
    if (target <= have) return;
    logVisible = target;
    const older = view.slice(view.length - target, view.length - have);
    const frag = document.createDocumentFragment();
    older.forEach(function (e) {
      frag.appendChild(renderLogLine(e));
    }); // oldest → newest
    const prevHeight = logPane.scrollHeight,
      prevTop = logPane.scrollTop;
    logPane.insertBefore(frag, logPane.firstChild);
    logPane.scrollTop = prevTop + (logPane.scrollHeight - prevHeight);
    applyLogQuickFilters();
  }

  // A single fresh (newest) line arrived over the stream.
  function appendNewestToDom(entry) {
    const empty = logPane.querySelector('.log-empty');
    if (empty) empty.remove();
    const node = renderLogLine(entry);
    logPane.appendChild(node);
    trimLogDom();
    logSearchApply(); // a fresh line obeys an active in-log filter
    if (followLogs) {
      scrollToNewest();
    } else if (!node.classList.contains('log-out')) {
      // Reading history: don't move the viewport, but say what landed. Only
      // lines that survive the quick filters count — offering to jump to
      // arrivals the pane would not show is worse than staying quiet.
      countPendingLine();
    }
    updateLogQuickCount();
  }

  function pushLog(entry) {
    logEntries.push(entry);
    const overflow = logEntries.length - maxLogBuffer;
    if (overflow > 0) logEntries.splice(0, overflow);
    // A reconnect can replay lines already held; the ID guard keeps the pinned
    // set strictly increasing so nothing is pinned twice.
    if (
      isLogOutcome(entry) &&
      (!pinnedLogEntries.length || pinnedLogEntries[pinnedLogEntries.length - 1].id < entry.id)
    ) {
      pinnedLogEntries.push(entry);
      const over = pinnedLogEntries.length - maxPinnedLog;
      if (over > 0) pinnedLogEntries.splice(0, over);
    }
    // While the logs view is hidden, or its live pill is paused, we only
    // buffer; the pane is rebuilt from the buffer on reactivation or unpause.
    if (App.chrome.activeView() === 'logs' && !logsPaused) appendNewestToDom(entry);
  }

  // Live/pause pill (clog-live), matching the container-log panel's: pausing
  // freezes the pane without dropping anything — pushLog keeps buffering, and
  // unpausing rebuilds the window to catch up in one go.
  const logsLive = document.getElementById('logs-live');
  const logsStat = document.getElementById('logs-stat');
  let logsPaused = false;

  function setLogsStat(text, cls) {
    logsStat.textContent = text;
    logsStat.className = 'clog-stat' + (cls ? ' ' + cls : '');
  }

  // SCROLL_EDGE_PX is how close to an edge counts as being at it — one line
  // height's worth of slack, so a pane sitting a pixel off the bottom after a
  // render still reads as "at the tail".
  const SCROLL_EDGE_PX = 40;

  // The log stream has no connection indicator (see the events indicator for
  // that), so a fatal error must recover silently. Like /api/events, EventSource
  // auto-reconnects after a transient drop but gives up for good on a fatal error
  // (non-2xx / bad content-type, readyState CLOSED); without our own retry the
  // pane would then stop receiving lines with nothing on screen to explain it.
  // Backoff is the same mechanism the events stream uses, with its own state.
  const logsReconnect = makeReconnector(
    function () {
      connectLogs();
    },
    setTimeout,
    clearTimeout,
  );

  function connectLogs() {
    if (logSource) return;
    logSource = new EventSource('/api/logs');
    logSource.addEventListener('log', function (e) {
      pushLog(JSON.parse(e.data));
    });
    logSource.onopen = function () {
      logsReconnect.reset(); // a good connection resets the backoff
      setLogsStat(logsPaused ? 'paused' : 'live · streaming', logsPaused ? 'paused' : '');
    };
    logSource.onerror = function () {
      // CONNECTING: the browser is retrying itself (Last-Event-ID replays the
      // gap on reconnect), so leave it. CLOSED: it gave up — clear the guard and
      // retry ourselves with a capped backoff.
      if (logSource.readyState === EventSource.CLOSED) {
        logSource = null;
        setLogsStat('reconnecting…', 'err');
        logsReconnect.schedule();
      }
    };
  }

  function applyFollow() {
    followBtn.classList.toggle('on', followLogs);
    followBtn.setAttribute('aria-pressed', String(followLogs));
    if (followLogs) scrollToNewest();
    else clearPendingLines(); // the pill only speaks for an armed-then-disarmed follow
  }

  // resume rebuilds the log stream after a tab suspension (the stream wake-up
  // in app.js calls it). Drop a stream the suspension killed even when the
  // Logs view is not active: connectLogs takes a non-null handle as "already
  // connected", so one left behind dead would make the view come up silent
  // when it is next opened. Reconnecting is what stays view-gated — that view
  // is what opens the stream, and applyView connects it on arrival.
  function resume() {
    const open = App.stream.streamIsOpen;
    if (logSource && !open(logSource)) {
      logSource.close();
      logSource = null;
    }
    if (App.chrome.activeView() === 'logs') logsReconnect.resume(open(logSource));
  }

  // init wires the pane, the quick-filter bar, the pill, the live toggle and
  // the follow button — everything the view ran at load.
  function init() {
    // Diff pill in the log view — same fetch/render/toggle behaviour as the
    // files pill in the deploy table, but anchored below the log line.
    logPane.addEventListener('click', function (e) {
      if (handleDiffToggle(e.target)) return;

      const pill = e.target.closest('.log-diff-pill');
      if (!pill) return;
      const line = pill.closest('.log-line');
      const existing = line.nextElementSibling;
      if (
        existing &&
        (existing.classList.contains('diff-panel') ||
          existing.classList.contains('files-list') ||
          existing.classList.contains('load-error'))
      ) {
        existing.remove();
        return;
      }
      function openLogDiff() {
        App.panels.fetchDiffs(pill.dataset.eventId, function (diffs, commits, err) {
          if (err) {
            // Fetch failed — surface it instead of the genuine "No diff recorded"
            // line, with a retry that re-runs just this fetch (T4.16).
            line.after(
              App.panels.createLoadError("Couldn't load the diff.", function (le) {
                le.remove();
                openLogDiff();
              }),
            );
          } else if (diffs && Object.keys(diffs).length > 0) {
            line.after(App.panels.renderDiffPanel(diffs, commits, null));
          } else {
            const note = document.createElement('div');
            note.className = 'files-list';
            note.textContent = 'No diff recorded for this deploy.';
            line.after(note);
          }
        });
      }
      openLogDiff();
    });

    logQuickBar.addEventListener('click', function (e) {
      const chip = e.target.closest('.clog-chip');
      if (!chip) return;
      if (chip.dataset.sev) {
        setLogFilters(Object.assign({}, logFilters, { sev: chip.dataset.sev }));
        return;
      }
      if (chip.dataset.kind) {
        const kinds = logFilters.kinds.slice();
        const i = kinds.indexOf(chip.dataset.kind);
        if (i === -1) kinds.push(chip.dataset.kind);
        else kinds.splice(i, 1);
        setLogFilters(Object.assign({}, logFilters, { kinds: kinds }));
        return;
      }
      if (chip.dataset.stack) toggleLogStackFilter(chip.dataset.stack);
    });

    // A stack prefix in a line is the fastest way to narrow to that stack.
    logPane.addEventListener('click', function (e) {
      const prefix = e.target.closest('.log-stack');
      if (!prefix) return;
      e.stopPropagation(); // never also toggle the line's diff panel
      toggleLogStackFilter(prefix.dataset.stack);
    });

    logPane.addEventListener('keydown', function (e) {
      if (e.key !== 'Enter' && e.key !== ' ') return;
      const prefix = e.target.closest('.log-stack');
      if (!prefix) return;
      e.preventDefault();
      toggleLogStackFilter(prefix.dataset.stack);
    });

    logNewPill.addEventListener('click', function () {
      setFollowLogs(true);
    });

    logsLive.addEventListener('click', function () {
      logsPaused = !logsPaused;
      logsLive.classList.toggle('paused', logsPaused);
      logsLive.querySelector('.clog-ltxt').textContent = logsPaused ? 'paused' : 'live';
      if (logsPaused) {
        setLogsStat('paused', 'paused');
        return;
      }
      setLogsStat('live · streaming');
      renderLogWindow(); // catch up everything buffered while paused
    });

    logsLive.addEventListener('keydown', function (e) {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        logsLive.click();
      }
    });

    logPane.addEventListener('scroll', function () {
      if (logPane.scrollTop <= SCROLL_EDGE_PX) loadMoreLogs();
      const atTail =
        logPane.scrollTop + logPane.clientHeight >= logPane.scrollHeight - SCROLL_EDGE_PX;
      // Scrolling away from the tail disengages follow: without this, every
      // arriving line yanks the viewport back and reading history is impossible.
      // Scrolling back to the tail re-arms it, so the control matches where the
      // pane actually is rather than needing to be toggled by hand.
      if (atTail !== followLogs) setFollowLogs(atTail, { keepScroll: true });
    });

    followBtn.addEventListener('click', function () {
      setFollowLogs(!followLogs);
    });
  }

  // ── skipper Logs view controls: the view is styled as one big clog-panel
  // (like the Stacks/Deploys container-log panel, just page-sized), so its
  // search/wrap/auto-scroll/fullscreen tools live inline in its own header —
  // not in the view-options popover, which no longer carries a logs group.
  // Search reveals the same filter bar the deploys/stacks views use (seeded by
  // type-to-search on desktop). Fullscreen fills below the header so it stays
  // reachable to toggle back off.
  function initControls() {
    const view = document.getElementById('log-view');
    const wrapBtn = document.getElementById('log-wrap');
    const fsBtn = document.getElementById('log-fs');
    const searchBtn = document.getElementById('log-search-open');
    const wrap = document.getElementById('log-filter-wrap');
    const input = document.getElementById('log-filter');
    const count = document.getElementById('log-filter-count');

    function highlightLine(line, q) {
      const ql = q.toLowerCase();
      const w = document.createTreeWalker(line, NodeFilter.SHOW_TEXT, null),
        ns = [];
      while (w.nextNode()) ns.push(w.currentNode);
      ns.forEach(function (n) {
        const text = n.nodeValue,
          low = text.toLowerCase();
        let idx = low.indexOf(ql);
        if (idx < 0) return;
        const frag = document.createDocumentFragment();
        let pos = 0;
        while (idx >= 0) {
          if (idx > pos) frag.appendChild(document.createTextNode(text.slice(pos, idx)));
          const mk = document.createElement('mark');
          mk.textContent = text.slice(idx, idx + q.length);
          frag.appendChild(mk);
          pos = idx + q.length;
          idx = low.indexOf(ql, pos);
        }
        if (pos < text.length) frag.appendChild(document.createTextNode(text.slice(pos)));
        n.parentNode.replaceChild(frag, n);
      });
    }
    function applyLogSearch() {
      const q = input.value.trim();
      wrap.classList.toggle('has-value', q.length > 0);
      let n = 0;
      logPane.querySelectorAll('.log-line').forEach(function (line) {
        if (line.dataset.origHtml == null) line.dataset.origHtml = line.innerHTML;
        line.innerHTML = line.dataset.origHtml;
        line.classList.remove('clog-out');
        if (!logLineVisible(line.textContent, q)) {
          line.classList.add('clog-out');
          return;
        }
        // A line the quick filters exclude is not on screen, so it must not be
        // counted as a hit — the two filters narrow independently and the count
        // has to describe what a viewer can actually see.
        if (q && !line.classList.contains('log-out')) {
          n++;
          highlightLine(line, q);
        }
      });
      count.textContent = q ? n + (n === 1 ? ' hit' : ' hits') : '';
    }
    function revealLogFilter(on) {
      wrap.classList.toggle('revealed', on);
      searchBtn.classList.toggle('on', on);
      App.chrome.syncStackSearchBtn(); // the header magnifier opens this same bar
    }
    function clearLogFilter(hide) {
      input.value = '';
      applyLogSearch();
      if (hide) {
        revealLogFilter(false);
        input.blur();
      }
    }
    // Re-apply after the log window re-renders (new lines) so they obey the filter.
    logSearchApply = function () {
      if (wrap.classList.contains('revealed') && input.value.trim()) applyLogSearch();
    };

    input.addEventListener('input', applyLogSearch);
    input.addEventListener('keydown', function (e) {
      if (e.key !== 'Escape') return;
      e.stopPropagation();
      if (input.value) clearLogFilter(false);
      else {
        revealLogFilter(false);
        input.blur();
      }
    });
    input.addEventListener('blur', function () {
      if (!input.value) revealLogFilter(false);
    });
    // Clicking again closes it (and clears the query), matching the
    // container-log panel's search tool. The header magnifier drives the same
    // toggle on this view, so both read and leave the one state.
    function toggleLogSearch() {
      if (wrap.classList.contains('revealed')) {
        clearLogFilter(true);
        return;
      }
      revealLogFilter(true);
      input.focus();
    }
    logSearchToggle = toggleLogSearch;
    logSearchIsOpen = function () {
      return wrap.classList.contains('revealed');
    };
    searchBtn.addEventListener('click', toggleLogSearch);

    wrapBtn.addEventListener('click', function () {
      wrapBtn.classList.toggle('on', logPane.classList.toggle('wrap'));
    });

    function setLogFullscreen(on) {
      view.classList.toggle('clog-fullscreen', on);
      fsBtn.classList.toggle('on', on);
    }
    exitLogFullscreen = function () {
      setLogFullscreen(false);
    };
    fsBtn.addEventListener('click', function () {
      setLogFullscreen(!view.classList.contains('clog-fullscreen'));
    });

    // Type-to-search: a printable key while viewing logs reveals the filter and
    // seeds it (mirrors the deploys/stacks type-to-search); Esc exits fullscreen
    // then the filter.
    document.addEventListener('keydown', function (e) {
      if (
        App.chrome.activeView() !== 'logs' ||
        e.defaultPrevented ||
        e.metaKey ||
        e.ctrlKey ||
        e.altKey
      )
        return;
      if (e.target === input) return;
      const tag = (e.target && e.target.tagName) || '';
      if (
        tag === 'INPUT' ||
        tag === 'TEXTAREA' ||
        tag === 'SELECT' ||
        (e.target && e.target.isContentEditable)
      )
        return;
      if (e.key === 'Escape') {
        if (view.classList.contains('clog-fullscreen')) setLogFullscreen(false);
        else if (wrap.classList.contains('revealed')) clearLogFilter(true);
        return;
      }
      if (e.key.length === 1 && e.key !== ' ') {
        revealLogFilter(true);
        input.focus();
        input.value += e.key;
        applyLogSearch();
        e.preventDefault();
      }
    });
  }

  return {
    renderLogLine,
    handleDiffToggle,
    renderLogWindow,
    connectLogs,
    resume,
    renderLogQuickChips,
    applyFollow,
    // Bound by initControls, so wrapped rather than captured at return.
    logSearchIsOpen: function () {
      return logSearchIsOpen();
    },
    logSearchToggle: function () {
      logSearchToggle();
    },
    exitLogFullscreen: function () {
      exitLogFullscreen();
    },
    init,
    initControls,
  };
})();
