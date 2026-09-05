// skipper-cd service worker — installability + app-shell caching.
//
// See docs/pwa.md and docs/adr/0018-pwa-installable-ui.md. Two rules matter:
//   1. Live traffic is NEVER cached. Requests to /api/* (incl. the SSE streams
//      /api/events and /api/logs), /metrics and /webhook are not intercepted at
//      all, so streaming takes the native network path untouched.
//   2. The shell is served network-first with a cache fallback, so a reachable
//      server always wins and a just-deployed UI is picked up promptly.
//
// __VERSION__ is replaced at serve time with the build version, so a new release
// changes this file's bytes and the browser installs a new worker. On an update
// that worker waits (see the install handler) until the page prompts the user to
// reload; once activated it drops the caches under the old name.

const CACHE = 'skipper-shell-__VERSION__';

// The app shell: the page plus the assets it needs to paint — including the
// self-hosted fonts (served same-origin from /fonts/), so the installed UI
// renders with its real typography offline.
const SHELL = [
  '/',
  '/app.css',
  '/app.js',
  '/app-state.js',
  '/app-panels.js',
  '/app-hosts.js',
  '/app-autosync.js',
  '/app-logs.js',
  '/app-clog.js',
  '/app-render.js',
  '/app-helpers.js',
  '/manifest.webmanifest',
  '/icons/icon-192.png',
  '/icons/icon-512.png',
  '/icons/icon-maskable-512.png',
  '/fonts/dm-sans-400.woff2',
  '/fonts/dm-sans-500.woff2',
  '/fonts/dm-sans-600.woff2',
  '/fonts/jetbrains-mono-400.woff2',
  '/fonts/jetbrains-mono-500.woff2',
  '/fonts/jetbrains-mono-600.woff2',
];

self.addEventListener('install', (event) => {
  event.waitUntil(caches.open(CACHE).then((cache) => cache.addAll(SHELL)));
  // Intentionally no skipWaiting() here: on an update (an existing worker still
  // controls the page) the new worker stays in "waiting" so the page can prompt
  // the user to reload. It activates only when the page posts SKIP_WAITING
  // below. On a first install there is no controller, so the browser activates
  // this worker immediately regardless.
});

// The page posts this when the user accepts the "new version available" prompt;
// activating the waiting worker fires controllerchange, which reloads the page
// onto the fresh shell. See docs/pwa.md.
self.addEventListener('message', (event) => {
  if (event.data && event.data.type === 'SKIP_WAITING') {
    self.skipWaiting();
  }
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((names) => Promise.all(names.filter((n) => n !== CACHE).map((n) => caches.delete(n))))
      .then(() => self.clients.claim()),
  );
});

// bypassed reports paths that must never be intercepted or cached: live data,
// metrics, and the webhook receiver.
function bypassed(pathname) {
  return pathname.startsWith('/api/') || pathname === '/metrics' || pathname === '/webhook';
}

self.addEventListener('fetch', (event) => {
  const req = event.request;
  const url = new URL(req.url);

  // Only the shell of this origin is our concern; everything else (live data,
  // cross-origin) takes the native path — no respondWith, no caching.
  if (req.method !== 'GET' || url.origin !== self.location.origin || bypassed(url.pathname)) {
    return;
  }

  // Network-first: prefer a fresh copy, fall back to cache when offline.
  event.respondWith(
    fetch(req)
      .then((resp) => {
        if (resp && resp.ok && resp.type === 'basic') {
          const copy = resp.clone();
          caches.open(CACHE).then((cache) => cache.put(req, copy));
        }
        return resp;
      })
      .catch(() =>
        caches.match(req).then((hit) => {
          if (hit) return hit;
          // For navigations with no cached match, fall back to the app shell.
          if (req.mode === 'navigate') return caches.match('/');
          return Response.error();
        }),
      ),
  );
});
