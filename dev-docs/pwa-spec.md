# Install skipper-cd as an App (PWA)

The skipper-cd web UI is an **installable Progressive Web App (PWA)**. The user-facing summary is [`docs/pwa.md`](../docs/pwa.md). This
document describes **what** the PWA behaviour is for users and operators — not
how it is implemented. UI details live in
[`internal/ui/UI_SPEC.md`](https://github.com/polandy/skipper-cd/blob/main/internal/ui/UI_SPEC.md).

The PWA is an **enhancement layer**. The site continues to work exactly as before
in a normal browser tab for anyone who does not install it.

---

## 1. Purpose

The web UI was a normal website: it opened in a browser tab and reloaded its
whole shell (~215 KB, fonts inline) on every visit. Making it a PWA lets
operators:

- **Install skipper-cd** onto a phone home screen, a tablet, or a desktop app
  launcher, and open it in its **own standalone window** — own icon, own entry in
  the OS app switcher, no browser chrome.
- **Start fast**, because the app shell is served from a local cache instead of
  being re-downloaded every time.
- Get a dashboard that **feels like a native app** on mobile — full-screen,
  themed status bar, proper icon.

## 2. Scope

**In scope**

- Installability on Android/Chrome, desktop Chrome/Edge, and iOS Safari
  ("Add to Home Screen").
- A branded app identity: name, icons, colours, splash screen.
- Fast startup by caching the app shell (page, fonts, icons).
- Correct, automatic updates when a new skipper-cd version is deployed.

**Out of scope (non-goals)**

- **Offline live data.** skipper-cd is a real-time deploy dashboard; when the
  device is offline the app may open, but it shows the usual "reconnecting"
  state — it does **not** fabricate or replay stale deploy data from an offline
  store.
- **Push notifications.**
- **Any change to existing behaviour** of deploys, autosync, logs, the event
  stream, or the API. Nothing that works today changes.

## 3. User-facing behaviour

### 3.1 Installation

- On a supported browser the user gets the standard install affordance — an
  install icon in the address bar on desktop, "Add to Home Screen" on mobile.
- After installing, skipper-cd appears as its own app with the ship icon and
  launches in a **standalone window** at the dashboard root (`/`).
- Uninstalling is done the normal OS way; it removes the app and its cache.

### 3.2 App identity

- **Name:** "skipper-cd" (short name "skipper" where space is tight).
- **Icon:** the existing skipper-cd container-ship logo, rendered as proper app
  icons, including a **maskable** variant so Android can crop it into the system
  icon shape without clipping the ship.
- **Colours / splash:** a dark Catppuccin-Mocha themed launch screen and status
  bar, consistent with the app's default dark look.

### 3.3 Theme

- The app keeps its existing in-page **Mocha (dark) / Latte (light) toggle**.
- The OS-level window/status-bar colour follows the **operating system's**
  light/dark preference — it cannot follow the in-app toggle (a platform
  limitation), matching how the existing favicon already behaves.

### 3.4 Startup & freshness

- On launch the app loads its shell from cache for speed, but **prefers a fresh
  copy from the server** when reachable, so a just-deployed UI is picked up
  promptly rather than being pinned to a stale cached version.
- Live data (deploy events, logs, autosync/queue state) is **always fetched live
  from the server** and is never served from cache.

### 3.5 Updates

- Because skipper-cd deploys itself, the app must never get "stuck" on an old
  cached UI. When a **new version** is running on the server, the installed app
  detects it and offers to refresh, without the user clearing anything.
- **In a normal tab**, a plain reload already fetches the fresh UI (the shell is
  served network-first), so there is nothing extra to do.
- **In a long-lived standalone window** that never reloads on its own, the app
  shows a small **"A new version of skipper-cd is available."** banner with a
  **Reload** button. Tapping it swaps in the new version and reloads once; the
  banner can also be dismissed to keep working on the current version. The check
  runs on launch and again whenever the window regains focus, so a backgrounded
  app notices a deploy without a manual reload.

## 4. Operator / deployment notes

- **Secure context required.** Browsers only allow installing a PWA over
  **HTTPS** (or `localhost`). skipper-cd is normally reached through a reverse
  proxy with TLS, which already satisfies this. Over plain HTTP the UI still works
  as a normal site but will **not** offer installation. This is an operational
  prerequisite, not something the app enforces.
- **No new configuration.** The PWA behaviour is on whenever the web UI is
  enabled (`ui_enabled: true`). There are no new config keys, flags, or
  dependencies to manage.

## 5. Acceptance criteria

1. In Chrome DevTools → **Application → Manifest**, the app reports as
   **installable** with no errors, showing the skipper-cd name and icons.
2. In **Application → Service Workers**, a service worker is **active** with no
   errors.
3. A **Lighthouse** PWA check reports the app as installable.
4. The app can be **installed** and opens in a **standalone window** with the ship
   icon.
5. With the app installed and its cache warm, the **live event stream keeps
   working** — deploy events and logs still arrive in real time (live data is
   never intercepted or served from cache).
6. After a **version bump** on the server, a reload of the installed app picks up
   the new UI (no manual cache clearing needed).
7. After a **version bump**, a standalone window that stays open shows the
   **update banner**; tapping **Reload** brings it to the new version, and
   dismissing it leaves the current version running.
8. In a **plain browser tab**, everything behaves exactly as it does today.
