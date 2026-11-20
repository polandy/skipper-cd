// app-detail.js — the row detail selection (docs/dev/ui-redesign-plan.md
// §3.6): which row of a table view has its detail open, and on which pane.
// Each view keeps one selection; opening another row or pane replaces it. The
// detail tracks the row and the pane's nodes by reference rather than by DOM
// position, so a panel wedged between them (the container-logs panel) never
// strands it, and a view that rebuilds its rows re-applies the selection by
// key (reapply). A pane renders inline, right after its row, or in its view's
// side column — beside the list on a wide screen, a full-screen sheet over it
// on a phone (ADR-0073) — and moves when the width crosses a breakpoint.
//
// A pane is registered once per DETAIL_TAB: openClass marks the row open (it
// drives the bound status bar), build(row, slot, opts) fills the pane through
// slot.set(nodes) — now or after a fetch, while slot.live() — and may hand
// slot.onRefresh(fn) the re-render a live update runs (refresh); the optional
// onClose(row) undoes what build put on the row. Each view registers its own.
//
// Loads after app-panels.js and before every view. Attached as App.detail.
App.detail = (function () {
  const panes = {};
  // view → { sel, row, opts, nodes }; nodes are the pane's elements.
  const open = {};
  // Keep in step with the .detail-side breakpoints in app.css.
  const wide = window.matchMedia('(min-width: 1200px)');
  const phone = window.matchMedia('(max-width: 700px)');
  wide.addEventListener('change', relayout);
  phone.addEventListener('change', relayout);
  // The view filters' row classes; an inline pane wears its row's.
  const ROW_HIDDEN = ['filtered-out', 'host-hidden'];

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

  // sideFor is view's side column, the wide-screen and phone home of its open pane.
  function sideFor(view) {
    return document.querySelector('.detail-side[data-detail-view="' + view + '"]');
  }

  // place puts entry's nodes where the current width wants them — moving
  // them only when they change place, so focus and scroll survive — and shows
  // the side column only while it holds them and their row is not filtered out.
  function place(view, entry) {
    const side = sideFor(view);
    const nodes = entry.nodes;
    const toSide = !!side && (wide.matches || phone.matches);
    const inSide = nodes.length > 0 && nodes[0].parentElement === side;
    const moveToSide = toSide && !inSide;
    if (moveToSide) side.append.apply(side, nodes);
    if (!toSide && (inSide || (nodes.length > 0 && !nodes[0].isConnected))) {
      entry.row.after.apply(entry.row, nodes);
      ROW_HIDDEN.forEach(function (c) {
        const hidden = entry.row.classList.contains(c);
        nodes.forEach(function (n) {
          n.classList.toggle(c, hidden);
        });
      });
    }
    const shown = toSide && nodes.length > 0 && getComputedStyle(entry.row).display !== 'none';
    if (side) side.parentElement.classList.toggle('side-open', shown);
    if (moveToSide && shown && phone.matches) focusSheet(nodes);
  }

  // focusSheet moves the focus off the covered row onto the panel, which outlives its re-renders.
  function focusSheet(nodes) {
    const inside = nodes.some(function (n) {
      return n.contains(document.activeElement);
    });
    if (inside) return;
    nodes[0].tabIndex = -1;
    nodes[0].focus({ preventScroll: true });
  }

  // relayout re-places every open detail — after a resize across the
  // breakpoint, or after a filter showed or hid rows.
  function relayout() {
    Object.keys(open).forEach(function (view) {
      const e = entryFor(view);
      if (e) place(view, e);
    });
  }

  function unplace(view, entry) {
    entry.nodes.forEach(function (n) {
      n.remove();
    });
    const side = sideFor(view);
    if (side) side.parentElement.classList.remove('side-open');
  }

  // openRow is the row view's detail is open on, or null.
  function openRow(view) {
    const e = entryFor(view);
    return e ? e.row : null;
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
    setExpanded(row, true);
    pane.build(row, slotFor(view, entry), entry.opts);
  }

  // slotFor is the handle a pane builder fills its entry through. set()
  // replaces the pane's nodes in place; once the entry is no
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
        place(view, entry);
      },
      onRefresh: function (fn) {
        entry.refresh = fn;
      },
    };
  }

  // refresh re-renders every open local detail on stack (every one when stack
  // is omitted) from the current snapshots; a peer's detail follows its own
  // snapshot through reapply. Focus inside a re-rendered panel lands on the
  // same control, else on the panel.
  function refresh(stack) {
    Object.keys(open).forEach(function (view) {
      const e = entryFor(view);
      if (!e || !e.refresh || e.row.classList.contains('peer-row')) return;
      if (stack && e.sel.stack !== stack) return;
      const was = focusKey(e.nodes);
      e.refresh();
      if (was !== null && open[view] === e) refocus(e.nodes, was);
    });
  }

  // focusKey names the focused control inside nodes by a selector that finds
  // it again after a re-render; null when the focus is elsewhere.
  function focusKey(nodes) {
    const a = document.activeElement;
    const inside = nodes.some(function (n) {
      return n.contains(a);
    });
    if (!inside) return null;
    if (a.dataset.tab) return '.dp-tab[data-tab="' + a.dataset.tab + '"]';
    if (a.dataset.action) return '[data-action="' + a.dataset.action + '"]';
    return '';
  }

  function refocus(nodes, key) {
    const kept = nodes.some(function (n) {
      return n.contains(document.activeElement);
    });
    if (kept) return;
    let target = null;
    if (key) {
      nodes.some(function (n) {
        target = n.querySelector(key);
        return !!target;
      });
    }
    if (!target && nodes.length) {
      target = nodes[0];
      target.tabIndex = -1;
    }
    if (target) target.focus({ preventScroll: true });
  }

  // toggle opens row's tab pane, or closes it when it is the one open.
  function toggle(view, row, tab, opts) {
    if (isOpen(view, row, tab)) close(view);
    else show(view, row, tab, opts);
  }

  // close closes view's open detail, if any. Focus inside the removed pane
  // goes back to the row's toggle rather than being lost to the page.
  function close(view) {
    const e = open[view];
    if (!e) return;
    delete open[view];
    const focusWasInPane = e.nodes.some(function (n) {
      return n.contains(document.activeElement);
    });
    unplace(view, e);
    const pane = panes[e.sel.tab];
    e.row.classList.remove(pane.openClass);
    setExpanded(e.row, false);
    if (pane.onClose) pane.onClose(e.row);
    const toggle = e.row.querySelector('.row-toggle');
    if (focusWasInPane && toggle) toggle.focus();
  }

  function setExpanded(row, expanded) {
    const toggle = row.querySelector('.row-toggle');
    if (toggle) toggle.setAttribute('aria-expanded', String(expanded));
  }

  // rowKeydown is a table view's keyboard path into its rows' detail: Enter or
  // Space on a row's toggle acts as a click on it, and Escape from inside the
  // open detail (or on its toggle) closes it. The container-logs panel keeps
  // its own Escape.
  function rowKeydown(view, e) {
    if (e.defaultPrevented) return;
    const toggle = e.target.closest('.row-toggle');
    if (toggle && (e.key === 'Enter' || e.key === ' ')) {
      e.preventDefault();
      toggle.click();
      return;
    }
    if (e.key !== 'Escape' || e.target.closest('.clog-panel')) return;
    const entry = entryFor(view);
    if (!entry) return;
    const inside =
      (toggle && entry.row.contains(toggle)) ||
      entry.nodes.some(function (n) {
        return n.contains(e.target);
      });
    if (!inside) return;
    e.preventDefault();
    close(view);
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
    unplace(view, e); // a side pane outlives the rebuilt list
    const row = Array.prototype.find.call(rows, function (r) {
      return detailMatchesRow(e.sel, r.dataset);
    });
    if (row) show(view, row, e.sel.tab, e.opts);
  }

  // tabKeydown moves a detail panel's tab selection by keyboard (tabKeyIndex):
  // it clicks the tab the key lands on — the view's own click path re-renders
  // the strip — then focuses that tab in the new strip.
  function tabKeydown(panel, e) {
    const tab = e.target.closest('.dp-tab');
    if (!tab) return;
    const strip = Array.prototype.slice.call(panel.querySelectorAll('.dp-tab'));
    const next = tabKeyIndex(e.key, strip.indexOf(tab), strip.length);
    if (next < 0) return;
    e.preventDefault();
    const id = strip[next].dataset.tab;
    strip[next].click();
    const landed = panel.querySelector('.dp-tab[data-tab="' + id + '"]');
    if (landed) landed.focus();
  }

  return {
    registerPane,
    isOpen,
    openRow,
    relayout,
    show,
    toggle,
    close,
    closeIf,
    rebind,
    reapply,
    refresh,
    tabKeydown,
    rowKeydown,
  };
})();
