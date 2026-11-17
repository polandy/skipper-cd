// app-detail.js — the row detail selection (docs/dev/ui-redesign-plan.md
// §3.6): which row of a table view has its detail open, and on which pane.
// Each view keeps one selection; opening another row or pane replaces it. The
// detail tracks the row and the pane's nodes by reference rather than by DOM
// position, so a panel wedged between them (the container-logs panel) never
// strands it, and a view that rebuilds its rows re-applies the selection by
// key (reapply). Panes are rendered inline, right after their row.
//
// A pane is registered once per DETAIL_TAB: openClass marks the row open (it
// drives the bound status bar), build(row, slot, opts) fills the pane through
// slot.set(nodes) — now or after a fetch, while slot.live() — and the optional
// onClose(row) undoes what build put on the row. The shared panes (health,
// history, hooks) are registered here; a view registers its own.
//
// Loads after app-panels.js and before every view. Attached as App.detail.
App.detail = (function () {
  const { createHealthPanel, createAuditPanel, createHooksPanel } = App.panels;

  const panes = {};
  // view → { sel, row, opts, nodes }; nodes are the pane's elements after row.
  const open = {};

  function registerPane(tab, pane) {
    panes[tab] = pane;
  }

  // entryFor returns a view's open entry while its row is still on screen. A
  // view that drops rows without closing them (peer rows rebuilt from a new
  // snapshot) leaves a detached entry behind, which counts as closed.
  function entryFor(view) {
    const e = open[view];
    return e && e.row.isConnected ? e : null;
  }

  // isOpen reports whether row has its detail open in view — on tab, or on
  // any pane when tab is omitted.
  function isOpen(view, row, tab) {
    const e = entryFor(view);
    return !!e && e.row === row && (!tab || e.sel.tab === tab);
  }

  // show opens row's tab pane in view, replacing whatever the view had open.
  function show(view, row, tab, opts) {
    close(view);
    const entry = {
      sel: detailSelection(view, row.dataset, tab),
      row: row,
      opts: opts || {},
      nodes: [],
    };
    open[view] = entry;
    const pane = panes[tab];
    row.classList.add(pane.openClass);
    pane.build(row, slotFor(view, entry), entry.opts);
  }

  // slotFor is the handle a pane builder fills its entry through. set()
  // replaces the pane's nodes in place after the row; once the entry is no
  // longer the open one (closed, or replaced while a fetch ran) both live()
  // and set() report that, so a late fetch never resurrects a closed pane.
  function slotFor(view, entry) {
    const live = function () {
      return open[view] === entry;
    };
    return {
      live: live,
      set: function (nodes) {
        if (!live()) return;
        entry.nodes.forEach(function (n) {
          n.remove();
        });
        entry.nodes = [].concat(nodes);
        entry.row.after.apply(entry.row, entry.nodes);
      },
    };
  }

  // toggle opens row's tab pane, or closes it when it is the one open.
  function toggle(view, row, tab, opts) {
    if (isOpen(view, row, tab)) close(view);
    else show(view, row, tab, opts);
  }

  // close closes view's open detail, if any.
  function close(view) {
    const e = open[view];
    if (!e) return;
    delete open[view];
    e.nodes.forEach(function (n) {
      n.remove();
    });
    const pane = panes[e.sel.tab];
    e.row.classList.remove(pane.openClass);
    if (pane.onClose) pane.onClose(e.row);
  }

  // closeIf closes view's detail when it is open on row (and on tab, when
  // given) — for a row about to leave, or an affordance that went away.
  function closeIf(view, row, tab) {
    if (isOpen(view, row, tab)) close(view);
  }

  // rebind moves view's detail, when it is open on from, onto to and rebuilds
  // it there — a row a repeat supersedes, or one that settled in place.
  function rebind(view, from, to) {
    const e = entryFor(view);
    if (!e || e.row !== from) return;
    close(view);
    show(view, to, e.sel.tab, e.opts);
  }

  // reapply re-opens view's selection on its row among rows, after the view
  // rebuilt them; a selection whose row is gone is dropped.
  function reapply(view, rows) {
    const e = open[view];
    if (!e) return;
    delete open[view];
    const row = Array.prototype.find.call(rows, function (r) {
      return detailMatchesRow(e.sel, r.dataset);
    });
    if (row) show(view, row, e.sel.tab, e.opts);
  }

  registerPane(DETAIL_TAB.health, {
    openClass: 'health-open',
    build: function (row, slot, opts) {
      row.dataset.health = opts.health || 'unknown'; // tints the row (variant A)
      slot.set(createHealthPanel(row.dataset.stack));
    },
    onClose: function (row) {
      row.removeAttribute('data-health');
    },
  });
  registerPane(DETAIL_TAB.history, {
    openClass: 'audit-open',
    build: function (row, slot) {
      slot.set(createAuditPanel(row.dataset.stack));
    },
  });
  registerPane(DETAIL_TAB.hooks, {
    openClass: 'hooks-open',
    build: function (row, slot) {
      slot.set(createHooksPanel(row.dataset.stack));
    },
  });

  return { registerPane, isOpen, show, toggle, close, closeIf, rebind, reapply };
})();
