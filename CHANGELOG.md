# Changelog

## [0.8.1](https://github.com/polandy/skipper-cd/compare/v0.8.0...v0.8.1) (2026-07-12)


### Bug Fixes

* **nixos:** keep --same-dir so the rebuild finds the flake ([999be1f](https://github.com/polandy/skipper-cd/commit/999be1fb5c9088087f571a7d8b6c18958deeb554))
* **nixos:** run self-update rebuild fire-and-forget to end the shutdown wedge ([0bb4e10](https://github.com/polandy/skipper-cd/commit/0bb4e10aa96b90f9a37b9e6acd82ab4d0f8ac66b))

## [0.8.0](https://github.com/polandy/skipper-cd/compare/v0.7.0...v0.8.0) (2026-07-12)


### Features

* **ui:** always hide skipped deploys, remove skip-filter toggle ([2d91ad2](https://github.com/polandy/skipper-cd/commit/2d91ad26b6a9b9d737d27af6042dd6aa2eb33f5e))
* **ui:** show deployed version in header ([9f00c99](https://github.com/polandy/skipper-cd/commit/9f00c99a2a04229c903b3fdd6d93c2aad072b38c))

## [0.7.0](https://github.com/polandy/skipper-cd/compare/v0.6.0...v0.7.0) (2026-07-11)


### Features

* **nixos:** self-heal skipper-cd after a failed self-update ([0a5514d](https://github.com/polandy/skipper-cd/commit/0a5514d3b57a29c839d5bcc822503f9c29c55af9))


### Bug Fixes

* **command:** bound cmd.Wait with WaitDelay to unwedge self-update shutdown ([fe07e8b](https://github.com/polandy/skipper-cd/commit/fe07e8b32056cb484ea9e49f38f99ace471003b8))

## [0.6.0](https://github.com/polandy/skipper-cd/compare/v0.5.0...v0.6.0) (2026-07-11)


### Features

* **ui:** give _nixos a recognizable icon and content diffs ([2d8d3eb](https://github.com/polandy/skipper-cd/commit/2d8d3eba1b3613c7aac5b555d3d6d96bd8306402))
* **ui:** give _nixos a recognizable icon and content diffs ([94ef58d](https://github.com/polandy/skipper-cd/commit/94ef58d79c3ac846141ca66fc20568dd8caf3158))
* **ui:** self-host fonts and add data-testid test hooks ([62519d0](https://github.com/polandy/skipper-cd/commit/62519d0651adc2efe78a1f6ce0bc5f65a3a41288))

## [0.5.0](https://github.com/polandy/skipper-cd/compare/v0.4.0...v0.5.0) (2026-07-11)


### Features

* **autosync:** add autosync/queue HTTP API and live SSE ([fd0e2ec](https://github.com/polandy/skipper-cd/commit/fd0e2ec75d6a21ab7cf88a9dc7097ee6ef3aec9c))
* **autosync:** add config toggles and resolution controller + queue ([5514f2d](https://github.com/polandy/skipper-cd/commit/5514f2d38f0a8490fd0396337a45dee52521165c))
* **autosync:** defer and queue deploys while autosync is paused ([552a4c4](https://github.com/polandy/skipper-cd/commit/552a4c42ed30bddb82545c472ddb4575c2e99137))
* **autosync:** pause deploys globally or per stack with a queued-changes queue ([cb3db12](https://github.com/polandy/skipper-cd/commit/cb3db1261b415937c5e7aec9a36ccad6a453c113))
* **brand:** update README logo to the container-ship design ([34ab06f](https://github.com/polandy/skipper-cd/commit/34ab06f03c5cc1c1df630889d6ac8250acf4e8ab))
* **icons:** add stack icon resolution and caching package ([68fbb4f](https://github.com/polandy/skipper-cd/commit/68fbb4fe0a7f3ef0a6651fc7e8ed7c73ef24b6ac))
* **icons:** fall back to PNG then WebP when no SVG exists ([b655479](https://github.com/polandy/skipper-cd/commit/b65547990ae7d9be4065507a4260f966d4141f44))
* per-stack service icons and ArgoCD-style autosync ([ffc3366](https://github.com/polandy/skipper-cd/commit/ffc3366ca5288d0c68a3c1fdef2cc3b0fd53556b))
* **ui:** autosync controls, pending count, and queue drawer ([09bda61](https://github.com/polandy/skipper-cd/commit/09bda618015eb538198bd5fc7b403434d892063e))
* **ui:** newest-first, sortable, paginated log view ([#14](https://github.com/polandy/skipper-cd/issues/14)) ([072f356](https://github.com/polandy/skipper-cd/commit/072f35675c78f86b716a405a1a8da3f109d495c2))
* **ui:** show per-stack service icons in the deploy table ([dffaafc](https://github.com/polandy/skipper-cd/commit/dffaafc9ebb176f4159688b574ecb4c1426c514a))


### Bug Fixes

* **autosync:** drop a stack from the queue once it is no longer paused ([3a4bd67](https://github.com/polandy/skipper-cd/commit/3a4bd67c500d533e1b4633954ffb58f896173371))
* **brand:** rename logo asset to refresh GitHub's image cache ([1720247](https://github.com/polandy/skipper-cd/commit/17202476c40e9857a646de8e4b340674448609eb))
* **deploy:** revert nix hashes when a rebuild fails but skipper survives ([#15](https://github.com/polandy/skipper-cd/issues/15)) ([6c5d223](https://github.com/polandy/skipper-cd/commit/6c5d2234286f43b7a8530895416c1ac21fc63d63))

## [0.4.0](https://github.com/polandy/skipper-cd/compare/v0.3.1...v0.4.0) (2026-07-10)


### Features

* **ui:** diff pill on deploy-complete lines in the log view ([0e052e3](https://github.com/polandy/skipper-cd/commit/0e052e358bc2b26fc650c6456f27992b487107bf))
* **ui:** make deploy lifecycle scannable in the log view ([fa0c7b8](https://github.com/polandy/skipper-cd/commit/fa0c7b83ebdd6f886f0d09f863803b6e4aa57007))
* **ui:** replace PNG logo with theme-aware helm SVG ([e0e0eb0](https://github.com/polandy/skipper-cd/commit/e0e0eb05d570b522aacb3df5141f0446034f136a))
* **ui:** swap helm logo for container-ship variant ([859e7e7](https://github.com/polandy/skipper-cd/commit/859e7e7addd95a9531ee5c095b1cbdba4ea63fac))
* **ui:** switch to Catppuccin theme with Mocha default and Latte toggle ([e509065](https://github.com/polandy/skipper-cd/commit/e5090655dd1b3f0a56363d7b1409d06c7b1951f5))


### Bug Fixes

* **git:** pin origin to the configured repo_url on sync ([45ca228](https://github.com/polandy/skipper-cd/commit/45ca228cedf491da44195090502c899f23e28d6a))
* **git:** pin origin to the configured repo_url on sync ([fb0919e](https://github.com/polandy/skipper-cd/commit/fb0919e83d249dc84a28400ecd7868b693694223))
* **git:** redact credentials in the clone log ([ed4d9fc](https://github.com/polandy/skipper-cd/commit/ed4d9fcb281cc900c2d7919add0118ee67bb9bfd))
* **git:** redact credentials in the clone log ([6d5c5e7](https://github.com/polandy/skipper-cd/commit/6d5c5e7f4da226e88451d9b2de7e2ea3155f35e5))
* **nixos:** run rebuild in a transient unit to survive self-restart ([cfc99c3](https://github.com/polandy/skipper-cd/commit/cfc99c330745430302b346d5a797f2b3aad3338e))

## [0.3.1](https://github.com/polandy/skipper-cd/compare/v0.3.0...v0.3.1) (2026-07-10)


### Bug Fixes

* **module:** ignore missing /run/secrets in service sandbox ([c1b9bed](https://github.com/polandy/skipper-cd/commit/c1b9bed1107af10018c7c8f539166d7cad65435b))

## [0.3.0](https://github.com/polandy/skipper-cd/compare/v0.2.0...v0.3.0) (2026-07-09)


### Features

* automatic rollback on failed docker compose up ([9dce6e1](https://github.com/polandy/skipper-cd/commit/9dce6e18101620fac87e75aac859af9eb635de06))
* **config:** add log_format option for JSON logs ([d846e9c](https://github.com/polandy/skipper-cd/commit/d846e9cc1109f851938b1c619e674e190d86af82))
* **docker:** add container HEALTHCHECK against /healthz ([9a6f4b9](https://github.com/polandy/skipper-cd/commit/9a6f4b91a1d803aad448fe99e853731b0bf713b3))
* graceful shutdown and read-header timeouts for HTTP servers ([060f8ff](https://github.com/polandy/skipper-cd/commit/060f8ff278dc47f6a67a842c6db5bbda8d87978f))
* **metrics:** count deploy runs waiting on the deploy lock ([2c565e7](https://github.com/polandy/skipper-cd/commit/2c565e7eb01d35cf38f3a2ba726495ec06e24d23))
* report repository sync health via /healthz ([b2f6b79](https://github.com/polandy/skipper-cd/commit/b2f6b7978ca6fabe400cd9f58ae6806c7168d33e))
* **webhook:** only deploy pushes to the configured branch ([5516565](https://github.com/polandy/skipper-cd/commit/55165655697bc34ce05f6cdac6fff8ef002e2c95))


### Bug Fixes

* **config:** reject empty, duplicate and reserved stack names ([d207e19](https://github.com/polandy/skipper-cd/commit/d207e1965f48fcdf8879c4a8cf0e1f404485a267))
* enforce command_timeout_seconds per command instead of per run ([08724b1](https://github.com/polandy/skipper-cd/commit/08724b189cc9b674335c66928c01b1e29bcb98eb))
* exclude locally-built images from docker compose pull ([937e7a5](https://github.com/polandy/skipper-cd/commit/937e7a51cca80fc302b3d435b64c2a6570a5d221))
* gitignore actual binary name skipper instead of skipper-cd ([f715469](https://github.com/polandy/skipper-cd/commit/f7154696c0371e21d74f7e089d7f651e758f320e))
* **git:** re-clone when repo dir exists but is not a git clone ([78b4dde](https://github.com/polandy/skipper-cd/commit/78b4ddef5c5a13058d7dac152b57cb40b955397b))
* **module:** set TimeoutStopSec and pull in network-online.target ([d692285](https://github.com/polandy/skipper-cd/commit/d69228504ee59fc80a294864635c5685174a7d43))
* **ui:** close SSE stream when keepalive write fails ([420a144](https://github.com/polandy/skipper-cd/commit/420a144d420d436c4cf5b9abc820763f86a60a57))
* **ui:** update rolled-back rows in place and fix files pill attribute ([67126f4](https://github.com/polandy/skipper-cd/commit/67126f4a433050b4f361dc035866c074c214749f))
* use original compose dir as project-directory during rollback ([c0810e8](https://github.com/polandy/skipper-cd/commit/c0810e8912779285061385ec196cad5a8a9257ab))
* **webhook:** limit request body size to 1 MiB ([db129e3](https://github.com/polandy/skipper-cd/commit/db129e3a6c7fb031c8811f9cba0dcb42b450151a))
* write state and history files atomically ([2ca481f](https://github.com/polandy/skipper-cd/commit/2ca481fd6e78e0b8b43a1d14f278cda6e81c7976))
