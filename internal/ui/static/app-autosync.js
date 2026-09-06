// app-autosync.js — the autosync control and drawer (dev-docs/autosync-spec.md,
// ADR-0016), cut out of the app script by view (ADR-0035 amendment) and attached as
// App.autosync.
//
// State comes from the 'autosync' and 'queue' SSE events (initial snapshots
// are sent on connect). The header control shows global state + pending count
// and opens the drawer; the drawer holds the global switch, the ordered queue,
// and a filterable per-stack switch list. Toggles POST to /api/autosync; the
// server pushes back an 'autosync' event so other tabs stay in sync.
//
// Loads before the stream, so the chrome and deploy surfaces it calls (App.chrome,
// App.deploys) are read at call time, never imported at load; the bootstrap calls
// init() at the spot this block used to occupy.
App.autosync = (function () {
  const S = App.state;

  const asBtn = document.getElementById('autosync-btn');
  const asCountEl = document.getElementById('autosync-count');
  const asDrawer = document.getElementById('autosync-drawer');
  const asSub = document.getElementById('autosync-sub');
  const asGlobalSw = document.getElementById('autosync-global-sw');
  const asGlobalNote = document.getElementById('autosync-global-note');
  const asQueueList = document.getElementById('autosync-queue-list');
  const asStackList = document.getElementById('autosync-stack-list');
  const asQCount = document.getElementById('autosync-qcount');
  const asFilter = document.getElementById('autosync-filter');
  const asFilterWrap = document.getElementById('autosync-filter-wrap');
  const asFilterClear = document.getElementById('autosync-filter-clear');

  function autosyncStacks() {
    return (S.autosyncSnap && S.autosyncSnap.stacks) || [];
  }

  function stackByName(name) {
    return autosyncStacks().filter(function (s) {
      return s.name === name;
    })[0];
  }

  function anyPaused() {
    if (!S.autosyncSnap) return false;
    if (!S.autosyncSnap.global) return true;
    return autosyncStacks().some(function (s) {
      return !s.effective;
    });
  }

  // autosyncPost writes a toggle and applies the snapshot it answers with.
  //
  // A failure here is invisible in the interface: the switch simply does not
  // move, which is indistinguishable from a click that never landed. It is
  // therefore announced on the console — the same treatment a dropped stale
  // snapshot gets — so an operator (and a failing test) can tell "the write was
  // refused" from "the control is dead". The switch is deliberately left
  // showing server state rather than the attempted one: the write did not
  // happen, so showing it as though it had would be the worse lie.
  function autosyncPost(scope, stack, enabled) {
    const what = scope === 'stack' ? scope + ' ' + stack : scope;
    // Recorded before the request, not after: it marks that a click actually
    // reached this handler. An empty note buffer on a failed toggle then means
    // the click never got here at all, which no later message could tell apart
    // from a request that vanished (T8).
    App.chrome.uiNote('debug', 'autosync: toggling', what, '->', enabled);
    fetch('/api/autosync', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ scope: scope, stack: stack, enabled: enabled }),
    })
      .then(function (r) {
        if (!r.ok) {
          App.chrome.uiNote('warn', 'autosync: toggle refused for', what, '— HTTP', r.status);
          return null;
        }
        return r.json();
      })
      .then(function (snap) {
        if (snap) applyAutosyncSnapshot(snap);
      })
      .catch(function (err) {
        App.chrome.uiNote('warn', 'autosync: toggle for', what, 'did not reach the server —', err);
      });
  }

  // applyAutosyncSnapshot installs a snapshot unless it is older than the one
  // already applied — the POST response and the SSE broadcast of the same change
  // can overtake each other, which would make a switch snap back
  // (dev-docs/autosync-spec.md, "Snapshot ordering").
  function applyAutosyncSnapshot(snap) {
    if (!snapshotIsFresh(S.autosyncVersion, snap)) {
      // A drop is invisible by design, so leave a trace of it.
      App.chrome.uiNote(
        'debug',
        'autosync: dropped a stale snapshot',
        snap.version,
        '<',
        S.autosyncVersion,
      );
      return;
    }
    S.autosyncSnap = snap;
    if (typeof snap.version === 'number') S.autosyncVersion = snap.version;
    renderAutosync();
  }

  // patchStackRow updates an existing row's cells without touching the row or
  // switch nodes themselves — see renderRowList for why that matters.
  function patchStackRow(row, s, pos) {
    const item = S.queueByStack[s.name];
    const qpos = row.querySelector('.qpos');
    qpos.className = pos !== null ? 'qpos' : 'qpos blank';
    qpos.textContent = autosyncPosText(pos, item);
    row.querySelector('.stack-name').innerHTML =
      escapeHtml(s.name) + autosyncReasonChipHTML(s, item);
    row.querySelector('.stack-detail').innerHTML = autosyncDetailHTML(s, item, Date.now());
    const sw = row.querySelector('.sw');
    sw.classList.toggle('on', s.effective);
    sw.setAttribute('aria-checked', String(s.effective));
    sw.title = autosyncSwitchTitle(s);
  }

  // renderRowList paints a stack list, patching in place whenever the rows and
  // their order are unchanged.
  //
  // Rebuilding wholesale would be simpler, but a snapshot can land in the middle
  // of a click: if the switch node is replaced between mousedown and mouseup the
  // browser fires the `click` on their common ancestor instead, the delegated
  // handler finds no `.sw` under it, and the toggle is silently lost. Every SSE
  // tick used to swap the whole list, so an autosync/queue event arriving at the
  // wrong moment ate the click (and any keyboard focus with it).
  function renderRowList(el, rows) {
    const key = rows
      .map(function (r) {
        return r.stack.name;
      })
      .join('\n');
    if (el.dataset.rowKey === key && el.children.length === rows.length) {
      rows.forEach(function (r, i) {
        patchStackRow(el.children[i], r.stack, r.pos);
      });
      return;
    }
    el.dataset.rowKey = key;
    const now = Date.now();
    el.innerHTML = rows
      .map(function (r) {
        return autosyncRowHTML(r.stack, r.pos, S.queueByStack[r.stack.name], now);
      })
      .join('');
  }

  function renderAutosyncBtn() {
    const count = S.queueSnap.count || 0;
    asBtn.classList.toggle('has-pending', count > 0);
    asBtn.classList.toggle('paused', anyPaused());
    if (S.autosyncSnap) asBtn.dataset.global = String(S.autosyncSnap.global);
    asCountEl.textContent = count;
    asBtn.title =
      count > 0
        ? `${count} stack${count === 1 ? '' : 's'} queued — autosync paused`
        : 'Autosync — all in sync';
  }

  function renderSub() {
    const paused = autosyncStacks().filter(function (s) {
      return !s.effective;
    }).length;
    const q = S.queueSnap.count || 0;
    if (q === 0 && paused === 0) asSub.textContent = 'All stacks in sync.';
    else if (q === 0)
      asSub.textContent = `${paused} stack${paused === 1 ? '' : 's'} paused · nothing waiting.`;
    else
      asSub.innerHTML = `<b>${q}</b> update${q === 1 ? '' : 's'} waiting · ${paused} stack${paused === 1 ? '' : 's'} paused.`;
  }

  // renderGlobal paints the global switch — and, the first time a snapshot
  // arrives, arms it. Before that the drawer has no server state: the switch
  // would show the markup's optimistic "on" and a click could not compute the
  // opposite value, so it posted nothing and the control read as broken. While
  // `data-ready` is "false" it is inert (aria-disabled, and pointer-events off
  // in CSS, so a click waits for the state instead of falling into the void).
  function renderGlobal() {
    if (!S.autosyncSnap) return;
    asDrawer.dataset.ready = 'true';
    asGlobalSw.removeAttribute('aria-disabled');
    asGlobalSw.classList.toggle('on', S.autosyncSnap.global);
    asGlobalSw.setAttribute('aria-checked', String(S.autosyncSnap.global));
    asGlobalNote.textContent = S.autosyncSnap.global
      ? 'on · deploys apply automatically'
      : 'off · stacks pause unless individually enabled';
  }

  function renderQueueList() {
    const pending = S.queueSnap.pending || [];
    asQCount.textContent = pending.length ? `(${pending.length})` : '';
    if (pending.length === 0) {
      asQueueList.dataset.rowKey = '';
      asQueueList.innerHTML =
        '<div class="qempty">Nothing queued — resumed deploys apply immediately.</div>';
      return;
    }
    renderRowList(
      asQueueList,
      pending.map(function (it) {
        const s = stackByName(it.stack) || {
          name: it.stack,
          effective: false,
          overridden: false,
          config: null,
        };
        return { stack: s, pos: it.position };
      }),
    );
  }

  function renderStackList() {
    const query = (asFilter.value || '').trim().toLowerCase();
    asFilterWrap.classList.toggle('has-value', query.length > 0);
    const stacks = autosyncStacks();
    const list = stacks.filter(function (s) {
      return s.name.toLowerCase().indexOf(query) !== -1;
    });
    if (list.length === 0) {
      asStackList.dataset.rowKey = '';
      asStackList.innerHTML =
        `<div class="qempty">` +
        (stacks.length ? `No stack matches “${escapeHtml(query)}”.` : 'No stacks.') +
        `</div>`;
      return;
    }
    renderRowList(
      asStackList,
      list.map(function (s) {
        return { stack: s, pos: null };
      }),
    );
  }

  function renderAutosync() {
    renderAutosyncBtn();
    renderGlobal();
    renderQueueList();
    renderStackList();
    renderSub();
  }

  function setDrawer(open) {
    if (open) App.chrome.closeOtherSurfaces(asDrawer);
    asDrawer.classList.toggle('open', open);
    asBtn.classList.toggle('open', open);
    asBtn.setAttribute('aria-expanded', String(open));
    // While the max-height transition runs, geometry inside the drawer is in
    // flux: content taller than the current clip shows a transient scrollbar,
    // which re-wraps text and moves the right-aligned switches. data-settled
    // marks the end of that window so a test (or any caller) can wait for the
    // drawer to stop moving instead of clicking into the transition (T8).
    asDrawer.dataset.settled = 'false';
    // Always open at the top: the drawer is taller than it is tall enough to
    // show, so a leftover scroll offset from the previous visit would hide the
    // global switch — the row the whole surface is about.
    if (open) asDrawer.scrollTop = 0;
    App.chrome.manageSurfaceFocus(asDrawer, asBtn, open);
  }

  // missGeometry compacts where a stray click landed relative to the switch it
  // should have hit: the pointer position, the switch's current box, and the
  // font-load state — a swap of a `font-display: swap` face reflows the drawer,
  // so `loading` at click time marks the layout as mid-shift. All synchronous
  // reads inside a handler that already ran, so recording them cannot perturb
  // the race being measured (T8).
  function missGeometry(e, sw) {
    let s = '[';
    if (typeof e.clientX === 'number') s += 'at ' + e.clientX + ',' + e.clientY + ' ';
    if (sw) {
      const r = sw.getBoundingClientRect();
      s += 'sw ' + Math.round(r.left) + ',' + Math.round(r.top);
      s += ' ' + Math.round(r.width) + 'x' + Math.round(r.height) + ' ';
    }
    return s + 'fonts ' + (document.fonts ? document.fonts.status : 'n/a') + ']';
  }

  // Per-stack switches are event-delegated so re-rendering the lists is cheap.
  function toggleFromEvent(e) {
    const target = e.target;
    const sw = target.closest ? target.closest('.sw[data-stack]') : null;
    if (!sw) {
      // Only interesting when the click landed *inside a stack row* yet missed
      // that row's switch — a click aimed at the control that did not reach it —
      // or anywhere in the drawer while fonts are still loading, when a swap can
      // shift the layout under a click already in flight. The rest of the drawer
      // (filter, background, queue lines) is legitimately non-switch surface,
      // and noting those would evict the diagnostics this buffer exists for.
      // See T8.
      const row = target.closest ? target.closest('.stack-row') : null;
      if (row && row.querySelector('.sw[data-stack]')) {
        App.chrome.uiNote(
          'debug',
          'autosync: click inside row',
          row.getAttribute('data-stack'),
          'missed its switch — hit',
          String(target.className || target.nodeName),
          missGeometry(e, row.querySelector('.sw[data-stack]')),
        );
      } else if (document.fonts && document.fonts.status === 'loading') {
        App.chrome.uiNote(
          'debug',
          'autosync: stray click while fonts were loading — hit',
          String(target.className || target.nodeName),
          missGeometry(e, asStackList.querySelector('.sw[data-stack]')),
        );
      }
      return false;
    }
    autosyncPost('stack', sw.getAttribute('data-stack'), !sw.classList.contains('on'));
    return true;
  }

  // init wires the drawer's listeners, registers it as a surface and paints
  // the initial state — everything the block ran at load.
  function init() {
    asDrawer.addEventListener('transitionend', function (e) {
      if (e.target === asDrawer && e.propertyName === 'max-height') {
        asDrawer.dataset.settled = String(asDrawer.classList.contains('open'));
      }
    });
    App.chrome.trapFocus(asDrawer);

    asBtn.addEventListener('click', function () {
      setDrawer(!asDrawer.classList.contains('open'));
    });
    asBtn.addEventListener('keydown', function (e) {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        asBtn.click();
      }
    });

    asGlobalSw.addEventListener('click', function () {
      if (S.autosyncSnap) autosyncPost('global', '', !S.autosyncSnap.global);
    });
    asGlobalSw.addEventListener('keydown', function (e) {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        asGlobalSw.click();
      }
    });

    asDrawer.addEventListener('click', function (e) {
      toggleFromEvent(e);
    });
    asDrawer.addEventListener('keydown', function (e) {
      if (e.key === 'Enter' || e.key === ' ') {
        if (toggleFromEvent(e)) e.preventDefault();
      }
    });

    asFilter.addEventListener('input', renderStackList);
    asFilter.addEventListener('keydown', function (e) {
      if (e.key === 'Escape' && asFilter.value) {
        e.stopPropagation();
        asFilter.value = '';
        renderStackList();
      }
    });
    asFilterClear.addEventListener('click', function () {
      asFilter.value = '';
      renderStackList();
      asFilter.focus();
    });

    App.chrome.registerSurface({
      surface: asDrawer,
      isOpen: function () {
        return asDrawer.classList.contains('open');
      },
      close: function () {
        setDrawer(false);
      },
      within: [asDrawer, asBtn],
    });

    renderAutosync(); // initial paint before the first SSE snapshot arrives
  }

  return { applyAutosyncSnapshot, renderAutosync, setDrawer, init };
})();
