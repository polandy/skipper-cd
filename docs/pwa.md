# Install as an App (PWA)

The web UI is an **installable Progressive Web App**: add it to a phone home screen or a desktop app launcher and it opens in its own standalone window with the skipper icon, starting fast from a local cache. Nothing to configure — it is on whenever `ui_enabled: true`.

## Installing

- Use the browser's standard affordance: the install icon in the address bar (desktop Chrome/Edge) or **Add to Home Screen** (Android, iOS Safari).
- **HTTPS required** (or `localhost`): browsers only offer installation over a secure context. Behind a TLS reverse proxy this is already satisfied; over plain HTTP the UI still works as a normal site, just without the install offer.

## Updates

- **Normal tab:** a reload always fetches the fresh UI — nothing to do.
- **Installed app:** after a new skipper-cd version is deployed, a small **"A new version of skipper-cd is available"** banner appears with a **Reload** button — no cache clearing needed. The check runs on launch and whenever the window regains focus.

## Good to know

- Live data (deploys, logs, health) is always fetched from the server, never from cache. Offline, the app opens but shows the usual "reconnecting" state — it does not replay stale deploy data.
- The in-app dark/light toggle works as usual; the OS-level window and status-bar colour follow the operating system's light/dark preference (a platform limitation).
