// app-clog.js — the container-logs panel (ADR-0037), the first view file cut
// out of app.js (ADR-0035 amendment). Attaches its API as App.clog; reads the
// per-host resolvers from App.resolve and the log-line renderer from App.logs.
//
// A live `docker compose logs` panel opened from a logs icon, per stack
// (merged services) and per container. One log open at a time; the panel
// streams from /api/container-logs via EventSource and trails the row/line it
// was opened from. Controls: live/pause, auto-scroll, wrap, in-log search,
// fullscreen.
App.clog = (function () {
  const { healthMapFor } = App.resolve;
  const CLOG_ICONS = {
    search:
      '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="7"/><path d="M21 21l-4-4"/></svg>',
    wrap: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 6h16"/><path d="M4 12h13a3 3 0 0 1 0 6h-4"/><path d="M16 15l-3 3 3 3"/><path d="M4 18h6"/></svg>',
    scroll:
      '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 4v11"/><path d="M8 11l4 4 4-4"/><path d="M5 20h14"/></svg>',
    fs: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M8 3H5a2 2 0 0 0-2 2v3"/><path d="M16 3h3a2 2 0 0 1 2 2v3"/><path d="M8 21H5a2 2 0 0 1-2-2v-3"/><path d="M16 21h3a2 2 0 0 0 2-2v-3"/></svg>',
    filter:
      '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 4h18l-7 8v6l-4 2v-8z"/></svg>',
  };
  let panel = null,
    es = null,
    btn = null,
    body = null,
    key = null;
  let selected = [],
    curStack = '',
    curHost = ''; // selected = [] → whole stack; else the chosen service subset
  // 'container' streams docker compose logs; 'skipper' streams /api/logs
  // filtered to the stack — the hook output (ADR-0038), in the same panel.
  let mode = 'container';
  let follow = true,
    paused = false,
    query = '',
    tail = 200;
  let fsHolder = null; // marks the panel's row spot while it is fullscreen in <body>

  function toBottom() {
    if (body) body.scrollTop = body.scrollHeight;
  }
  function setStat(text, cls) {
    const s = panel && panel.querySelector('.clog-stat');
    if (s) {
      s.textContent = text;
      s.className = 'clog-stat' + (cls ? ' ' + cls : '');
    }
  }

  // A stream error, applied to both ends of the panel. The footer says what
  // happened; the live/pause pill has to follow, or a closed stream keeps a
  // green "live" next to a footer saying it is closed — and its toggle would
  // put "live · streaming" back on a stream that is gone.
  function applyStreamError(readyState) {
    if (!panel) return;
    const s = clogStreamStatus(readyState);
    setStat(s.text, s.cls);
    const live = panel.querySelector('.clog-live');
    if (!live) return;
    live.classList.toggle('dead', s.closed);
    live.querySelector('.clog-ltxt').textContent = s.closed ? 'closed' : paused ? 'paused' : 'live';
  }

  // The stack's services, from the (peer-aware) health snapshot at open time.
  function servicesFor() {
    const h = healthMapFor(curHost)[curStack];
    return (h && h.services) || [];
  }
  // A stack with fewer than two services has nothing to filter, so the
  // per-service control is suppressed (only in container mode, never hooks).
  function hasServiceFilter() {
    return mode === 'container' && servicesFor().length >= 2;
  }

  // The scope label in the header: whole stack, one service, a short list, or
  // an "N services" count once the list would get long.
  function scopeText() {
    if (!selected.length) return curStack + ' · all services';
    if (selected.length === 1) return curStack + ' / ' + selected[0];
    if (selected.length <= 3) return curStack + ' / ' + selected.join(' + ');
    return curStack + ' / ' + selected.length + ' services';
  }

  // Empty string when there is nothing to filter, so the head shows no filter
  // tool either.
  function svcRowHTML() {
    if (!hasServiceFilter()) return '';
    return clogSvcsHTML(servicesFor(), selected);
  }

  // Reflect the current selection onto the chips + scope label after a toggle.
  function syncChips() {
    if (!panel) return;
    panel.querySelectorAll('.clog-svcs .clog-chip').forEach(function (x) {
      const svc = x.dataset.svc;
      x.classList.toggle(
        'active',
        svc === '' ? selected.length === 0 : selected.indexOf(svc) !== -1,
      );
    });
    const sc = panel.querySelector('.clog-scope');
    if (sc) sc.textContent = '· ' + scopeText();
  }

  // decorate splits a compose log line into service prefix (merged view),
  // leading RFC3339 timestamp (--timestamps) and message, colouring each and
  // tinting error/warn lines. Everything is escaped — no HTML from the child.
  function decorate(data) {
    let svc = '',
      rest = data;
    if (selected.length !== 1) {
      // merged + multi keep the compose prefix; a single service drops it
      const m = rest.match(/^([^|]{1,60}?)\s+\|\s?(.*)$/);
      if (m) {
        svc = m[1];
        rest = m[2];
      }
    }
    let ts = '';
    const t = rest.match(/^(\S+)\s([\s\S]*)$/);
    if (t && /^\d{4}-\d\d-\d\dT[\d:.]+/.test(t[1])) {
      ts = t[1];
      rest = t[2];
    }
    let cls = '';
    if (/error|fatal|panic|\bfail/i.test(rest)) cls = 'clog-err';
    else if (/warn/i.test(rest)) cls = 'clog-warn';
    let html = '';
    if (svc) html += '<span class="clog-svc">' + escapeHtml(svc) + ' |</span> ';
    if (ts) html += '<span class="clog-ts">' + escapeHtml(ts) + '</span> ';
    html += cls ? '<span class="' + cls + '">' + escapeHtml(rest) + '</span>' : escapeHtml(rest);
    return html;
  }

  function highlight(root, q) {
    const ql = q.toLowerCase();
    const w = document.createTreeWalker(root, NodeFilter.SHOW_TEXT, null),
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
  function filterLine(ln) {
    if (ln.dataset.orig == null) ln.dataset.orig = ln.innerHTML;
    ln.innerHTML = ln.dataset.orig;
    ln.classList.remove('clog-out', 'clog-hit');
    if (!logLineVisible(ln.textContent, query)) {
      ln.classList.add('clog-out');
      return;
    }
    if (query) {
      ln.classList.add('clog-hit');
      highlight(ln, query);
    }
  }
  function applySearch(q) {
    query = q;
    let n = 0;
    body.querySelectorAll('.clog-ln').forEach(function (ln) {
      filterLine(ln);
      if (q && !ln.classList.contains('clog-out')) n++;
    });
    const hits = panel.querySelector('.clog-hits');
    if (hits) hits.textContent = q ? n + (n === 1 ? ' hit' : ' hits') : '';
    if (follow) toBottom();
  }

  function appendLine(data) {
    if (paused) return;
    const ln = document.createElement('span');
    ln.className = 'clog-ln';
    ln.innerHTML = decorate(data);
    if (query) filterLine(ln);
    body.appendChild(ln);
    while (body.children.length > 3000) body.removeChild(body.firstChild);
    if (query) {
      const hits = panel.querySelector('.clog-hits');
      if (hits) hits.textContent = body.querySelectorAll('.clog-ln.clog-hit').length + ' hits';
    }
    if (follow) toBottom();
  }

  // Render one /api/logs entry (skipper mode) via the shared renderLogLine,
  // tagged .clog-ln so the panel's search/wrap/scroll apply.
  function appendSkipperLine(entry) {
    if (paused) return;
    const ln = App.logs.renderLogLine(entry);
    ln.classList.add('clog-ln');
    if (query) filterLine(ln);
    body.appendChild(ln);
    while (body.children.length > 3000) body.removeChild(body.firstChild);
    if (query) {
      const hits = panel.querySelector('.clog-hits');
      if (hits) hits.textContent = body.querySelectorAll('.clog-ln.clog-hit').length + ' hits';
    }
    if (follow) toBottom();
  }

  function connect() {
    if (es) {
      es.close();
      es = null;
    }
    setStat('live · streaming', paused ? 'paused' : '');
    if (mode === 'skipper') {
      // Hook log: skipper's own stream, filtered to the stack's attributed lines.
      es = new EventSource('/api/logs');
      es.addEventListener('log', function (ev) {
        if (!panel || !panel.isConnected) {
          close();
          return;
        }
        let entry;
        try {
          entry = JSON.parse(ev.data);
        } catch (_) {
          return;
        }
        if (((entry.attrs && entry.attrs.stack) || '') !== curStack) return;
        appendSkipperLine(entry);
      });
      es.onerror = function (ev) {
        applyStreamError(ev.target.readyState);
      };
      es.onopen = function () {
        if (panel) setStat(paused ? 'paused' : 'live · streaming', paused ? 'paused' : '');
      };
      return;
    }
    // A peer's logs stream through the primary's proxy (the browser can't reach
    // the peer cross-origin, ADR-0048); a local stack hits the endpoint directly.
    let url = curHost
      ? '/api/peers/' +
        encodeURIComponent(curHost) +
        '/container-logs/' +
        encodeURIComponent(curStack)
      : '/api/container-logs/' + encodeURIComponent(curStack);
    url += '?tail=' + tail;
    if (selected.length) url += '&services=' + selected.map(encodeURIComponent).join(',');
    es = new EventSource(url);
    es.onmessage = function (ev) {
      if (!panel || !panel.isConnected) {
        close();
        return;
      } // dropped by a re-render
      appendLine(ev.data);
    };
    es.onerror = function (ev) {
      applyStreamError(ev.target.readyState);
    };
    es.onopen = function () {
      if (panel) setStat(paused ? 'paused' : 'live · streaming', paused ? 'paused' : '');
    };
  }

  function buildPanel(scope) {
    const el = document.createElement('div');
    el.className = 'clog-panel';
    el.dataset.testid = 'clog-panel';
    el.innerHTML =
      '<div class="clog-head" data-taptip>' +
      '<span class="clog-title">' +
      CLOG_ICON +
      ' logs <span class="clog-scope">· ' +
      escapeHtml(scope) +
      '</span></span>' +
      '<span class="clog-live" data-testid="clog-live" role="button" tabindex="0" title="Live — click to pause"><span class="clog-dot"></span><span class="clog-ltxt">live</span></span>' +
      '<span class="clog-grow"></span>' +
      '<button class="clog-tool" data-clog="search" type="button" title="Search in log">' +
      CLOG_ICONS.search +
      '</button>' +
      (hasServiceFilter()
        ? '<button class="clog-tool" data-clog="svcfilter" type="button" title="Filter by service">' +
          CLOG_ICONS.filter +
          '</button>'
        : '') +
      '<button class="clog-tool" data-clog="wrap" type="button" title="Wrap long lines">' +
      CLOG_ICONS.wrap +
      '</button>' +
      '<button class="clog-tool on" data-clog="scroll" type="button" title="Auto-scroll — follow the tail">' +
      CLOG_ICONS.scroll +
      '</button>' +
      '<span class="clog-tail" data-testid="clog-tail">' +
      '<button data-tail="50" type="button">50</button>' +
      '<button data-tail="200" class="active" type="button">200</button>' +
      '<button data-tail="1000" type="button">1000</button>' +
      '</span>' +
      '<button class="clog-tool" data-clog="fs" type="button" title="Fullscreen">' +
      CLOG_ICONS.fs +
      '</button>' +
      '</div>' +
      svcRowHTML() +
      '<div class="clog-search clog-hide" data-testid="clog-search"><span class="clog-sic">' +
      CLOG_ICONS.search +
      '</span>' +
      '<input type="text" placeholder="Search in log…" autocomplete="off" spellcheck="false" aria-label="Search in log"><span class="clog-hits"></span></div>' +
      '<div class="clog-body" data-testid="clog-body"></div>' +
      '<div class="clog-foot"><span class="clog-stat">live · streaming</span></div>';
    return el;
  }

  function wire() {
    const live = panel.querySelector('.clog-live');
    live.addEventListener('click', function () {
      if (this.classList.contains('dead')) return; // nothing left to pause
      paused = !paused;
      this.classList.toggle('paused', paused);
      this.querySelector('.clog-ltxt').textContent = paused ? 'paused' : 'live';
      setStat(paused ? 'paused' : 'live · streaming', paused ? 'paused' : '');
    });
    live.addEventListener('keydown', function (e) {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        live.click();
      }
    });
    panel.querySelectorAll('.clog-tool[data-clog]').forEach(function (b) {
      b.addEventListener('click', function () {
        const k = b.dataset.clog;
        if (k === 'wrap') {
          b.classList.toggle('on', body.classList.toggle('wrap'));
        } else if (k === 'scroll') {
          follow = b.classList.toggle('on');
          if (follow) toBottom();
        } else if (k === 'fs') {
          fullscreen(!panel.classList.contains('clog-fullscreen'));
        } else if (k === 'svcfilter') {
          const row = panel.querySelector('.clog-svcs');
          if (row) b.classList.toggle('on', row.classList.toggle('clog-hide') === false);
        } else if (k === 'search') {
          const box = panel.querySelector('.clog-search');
          const show = box.classList.toggle('clog-hide') === false;
          b.classList.toggle('on', show);
          const inp = box.querySelector('input');
          if (show) inp.focus();
          else {
            inp.value = '';
            applySearch('');
          }
        }
      });
    });
    panel.querySelector('.clog-search input').addEventListener('input', function () {
      applySearch(this.value.trim());
    });
    panel.querySelectorAll('.clog-tail button').forEach(function (b) {
      b.addEventListener('click', function () {
        panel.querySelectorAll('.clog-tail button').forEach(function (x) {
          x.classList.remove('active');
        });
        b.classList.add('active');
        tail = parseInt(b.dataset.tail, 10) || 200;
        body.innerHTML = '';
        connect(); // re-pull the backlog at the new size
      });
    });
    // Service chips toggle membership in the selected set; "all" clears it.
    // Each change re-pulls the backlog at the new scope (like the tail buttons).
    panel.querySelectorAll('.clog-svcs .clog-chip').forEach(function (c) {
      c.addEventListener('click', function () {
        const svc = c.dataset.svc;
        if (svc === '') {
          selected = [];
        } else {
          const i = selected.indexOf(svc);
          if (i === -1) selected.push(svc);
          else selected.splice(i, 1);
        }
        syncChips();
        body.innerHTML = '';
        connect();
      });
    });
  }

  // The panel lives inside <main>, which has its own stacking context
  // (z-index:1), so a high z-index alone can't lift a fullscreen overlay above
  // the sticky header. Reparent it to <body> for the duration, leaving a
  // comment where it belongs so exit can restore it.
  function fullscreen(on) {
    const b = panel.querySelector('.clog-tool[data-clog="fs"]');
    if (on) {
      if (!fsHolder) {
        fsHolder = document.createComment('clog-fs');
        panel.before(fsHolder);
        document.body.appendChild(panel);
      }
      panel.classList.add('clog-fullscreen');
      if (b) b.classList.add('on');
      toBottom();
      return;
    }
    panel.classList.remove('clog-fullscreen');
    if (b) b.classList.remove('on');
    if (fsHolder && fsHolder.parentNode) {
      fsHolder.replaceWith(panel); // back to its row
      fsHolder = null;
    } else if (fsHolder) {
      fsHolder = null;
      close(); // the row was re-rendered away while fullscreen → tear down
    }
  }

  function open(button, stack, service, host) {
    const newKey = (host || '') + '\n' + stack + '\n' + (service || '');
    if (key === newKey) {
      close();
      return;
    } // same icon → toggle closed
    close();
    btn = button;
    key = newKey;
    curStack = stack;
    selected = service ? [service] : [];
    curHost = host || '';
    mode = 'container';
    follow = true;
    paused = false;
    query = '';
    btn.classList.add('on');
    panel = buildPanel(scopeText());
    const anchor =
      button.closest('.hp-svc') ||
      button.closest('.event-row') ||
      button.closest('.roster-row') ||
      button;
    anchor.after(panel);
    body = panel.querySelector('.clog-body');
    wire();
    connect();
  }

  // Open this stack's hook log inline in skipper mode (ADR-0038); toggle closed
  // on a second click.
  function openHookLog(button, stack) {
    const newKey = `${stack}\n#hook`;
    if (key === newKey) {
      close();
      return;
    }
    close();
    btn = button;
    key = newKey;
    curStack = stack;
    selected = [];
    curHost = '';
    mode = 'skipper';
    follow = true;
    paused = false;
    query = '';
    if (btn) btn.classList.add('on');
    panel = buildPanel(`${stack} · deploy hook`);
    const anchor = button.closest('.event-row') || button.closest('.roster-row') || button;
    anchor.after(panel);
    body = panel.querySelector('.clog-body');
    wire();
    // /api/logs replays its own backlog and has no tail param — hide the selector.
    const tailSel = panel.querySelector('.clog-tail');
    if (tailSel) tailSel.style.display = 'none';
    connect();
  }

  function close() {
    if (es) {
      es.close();
      es = null;
    }
    if (fsHolder) {
      if (fsHolder.parentNode) fsHolder.remove();
      fsHolder = null;
    }
    if (panel) {
      panel.remove();
      panel = null;
    }
    if (btn) {
      btn.classList.remove('on');
      btn = null;
    }
    body = null;
    key = null;
    query = '';
    mode = 'container';
    curHost = '';
    selected = [];
  }

  // Type-to-search: while a log is open, a printable key routes into the
  // in-log search, overriding the deploys/stacks type-to-search. Capture phase
  // + stopImmediatePropagation so it wins over those document keydown listeners.
  document.addEventListener(
    'keydown',
    function (e) {
      if (!panel) return; // no log open → leave default search
      if (e.defaultPrevented || e.metaKey || e.ctrlKey || e.altKey) return;
      const input = panel.querySelector('.clog-search input');
      if (e.target === input) return; // already in the log search → native typing
      const tag = (e.target && e.target.tagName) || '';
      if (
        tag === 'INPUT' ||
        tag === 'TEXTAREA' ||
        tag === 'SELECT' ||
        (e.target && e.target.isContentEditable)
      )
        return;
      if (e.key === 'Escape' || e.key.length !== 1 || e.key === ' ') return;
      const box = panel.querySelector('.clog-search');
      const searchBtn = panel.querySelector('.clog-tool[data-clog="search"]');
      if (box.classList.contains('clog-hide')) {
        box.classList.remove('clog-hide');
        if (searchBtn) searchBtn.classList.add('on');
      }
      input.focus();
      input.value += e.key; // focus mid-keydown doesn't reliably route the char
      applySearch(input.value.trim());
      e.preventDefault();
      e.stopImmediatePropagation();
    },
    true,
  );

  return {
    toggle: function (button) {
      open(
        button,
        button.dataset.clogStack,
        button.dataset.clogService || '',
        button.dataset.clogHost || '',
      );
    },
    openHookLog: function (button, stack) {
      openHookLog(button, stack);
    },
    close: close,
    escape: function () {
      if (panel && panel.classList.contains('clog-fullscreen')) {
        fullscreen(false);
        return true;
      }
      if (panel) {
        close();
        return true;
      }
      return false;
    },
  };
})();
