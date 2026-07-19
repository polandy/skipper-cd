# Changelog

## [0.17.0](https://github.com/polandy/skipper-cd/compare/v0.16.0...v0.17.0) (2026-07-19)


### Features

* live container logs in the UI (ADR-0037) ([eebdc45](https://github.com/polandy/skipper-cd/commit/eebdc458e1f205c5759698f14babc8f4790aadd2))
* **orphans:** detect orphaned & unmanaged compose projects (ADR-0036) ([e9661bd](https://github.com/polandy/skipper-cd/commit/e9661bd18503854d9a2fe906b8d0db45bb7a0f46))
* **ui:** add Stacks roster view ([b748f27](https://github.com/polandy/skipper-cd/commit/b748f2778901e664ec8acd15eb86707412d3b80e))
* **ui:** jump between Deploys and Stacks views per stack ([c98a5b7](https://github.com/polandy/skipper-cd/commit/c98a5b7eeb846e77c256f5fa277836edee9bad15))


### Bug Fixes

* **config:** validate ports and command timeout at load ([#136](https://github.com/polandy/skipper-cd/issues/136)) ([72eac3d](https://github.com/polandy/skipper-cd/commit/72eac3dbd60414075e8c0534b86f94f77b138a0c))
* **docker:** run the container as non-root, harden the compose example ([56b843c](https://github.com/polandy/skipper-cd/commit/56b843ce585947281149c68ecd10a6c12efd1d1b))
* recover panics in background goroutines, thread ctx into live notify delivery ([728c4b6](https://github.com/polandy/skipper-cd/commit/728c4b6119ffbff322cbd953c82e9e6e35426851))
* security/deps audit round 2 (path traversal, error-wrap, file perms, Go 1.26.5) ([d836ae0](https://github.com/polandy/skipper-cd/commit/d836ae0443fd29255697aebb60f4845f6239e2f9))

## [0.16.0](https://github.com/polandy/skipper-cd/compare/v0.15.0...v0.16.0) (2026-07-18)


### Features

* **dev:** add ui-preview for eyeballing the web UI ([#120](https://github.com/polandy/skipper-cd/issues/120)) ([d77a8ad](https://github.com/polandy/skipper-cd/commit/d77a8ad4066db69672ae8ed51ced33f3cbdce596))


### Bug Fixes

* **ui:** reconnect the /api/logs stream after a fatal error ([#117](https://github.com/polandy/skipper-cd/issues/117)) ([943fdd1](https://github.com/polandy/skipper-cd/commit/943fdd19c1ccc9ff8e7a68803820115302031dd5))
* **ui:** recover connection indicator from a fatal SSE stream error ([#116](https://github.com/polandy/skipper-cd/issues/116)) ([8440e42](https://github.com/polandy/skipper-cd/commit/8440e42ea57b8a9dcee412ccfe686ef2166438f0))
* **ui:** scope IconsHandler to static/icons and fail-fast on missing sw.js ([#114](https://github.com/polandy/skipper-cd/issues/114)) ([06fe8a3](https://github.com/polandy/skipper-cd/commit/06fe8a34a5e18ec1cbaf08ce5eff0b0545ac7b1a))

## [0.15.0](https://github.com/polandy/skipper-cd/compare/v0.14.0...v0.15.0) (2026-07-18)


### Features

* **config:** anchor discovery skipper.yaml at stacks_base_dir ([#113](https://github.com/polandy/skipper-cd/issues/113)) ([b5c9dff](https://github.com/polandy/skipper-cd/commit/b5c9dff30b0634b9ea7674ddc739a30a905b51bc))
* **config:** show the offending skipper.yaml excerpt in discovery errors ([2c677ee](https://github.com/polandy/skipper-cd/commit/2c677ee9f971d2841f41bb90e81bb853a49afe6c))
* stack discovery from the deploy repo (ADR-0034) ([#109](https://github.com/polandy/skipper-cd/issues/109)) ([858d57e](https://github.com/polandy/skipper-cd/commit/858d57e986a9e5c167d1c8dfe63c0d9a6da5e803))
* **ui:** stack-discovery UI surface with skipper.yaml error excerpts ([#111](https://github.com/polandy/skipper-cd/issues/111)) ([2c677ee](https://github.com/polandy/skipper-cd/commit/2c677ee9f971d2841f41bb90e81bb853a49afe6c))


### Bug Fixes

* **ui:** panel lifecycle, pending-tag escaping + refresh, keyboard health pill ([#112](https://github.com/polandy/skipper-cd/issues/112)) ([617d87c](https://github.com/polandy/skipper-cd/commit/617d87c32ff4c076f675c64a499c9a02bf2846c8))

## [0.14.0](https://github.com/polandy/skipper-cd/compare/v0.13.0...v0.14.0) (2026-07-18)


### Features

* **audit:** durable per-stack deploy history (ADR-0033) ([#102](https://github.com/polandy/skipper-cd/issues/102)) ([406d299](https://github.com/polandy/skipper-cd/commit/406d299216a249a52ed96aa6f4a302a826589ada))
* **config:** enable the web UI by default ([5ff2ba3](https://github.com/polandy/skipper-cd/commit/5ff2ba318a131c8c73bf0ee701880a54ad1f7f28))
* **ui:** self-heal row detail badge and drifted-services panel ([#105](https://github.com/polandy/skipper-cd/issues/105)) ([9edc848](https://github.com/polandy/skipper-cd/commit/9edc8481990ccf27b412766c4c0f2e962c344e28))

## [0.13.0](https://github.com/polandy/skipper-cd/compare/v0.12.0...v0.13.0) (2026-07-17)


### Features

* **deploy:** stack deploy ordering via depends_on ([#101](https://github.com/polandy/skipper-cd/issues/101)) ([8f8e6fa](https://github.com/polandy/skipper-cd/commit/8f8e6fa4b3a6b7edf36b35a6e0ff094996f61c24))
* **healthwatch:** alert on own-stack health transitions (ADR-0031) ([bbd94c2](https://github.com/polandy/skipper-cd/commit/bbd94c2ae7c4cb106445ab4a5117be9068b7ee30))
* **healthwatch:** per-service alert cooldown with catch-up ([#100](https://github.com/polandy/skipper-cd/issues/100)) ([1cd70eb](https://github.com/polandy/skipper-cd/commit/1cd70eb376b470260b8c5f8b79eb925bef3f7d84))
* **selfheal:** add opt-in runtime-drift self-heal (ADR-0029) ([#94](https://github.com/polandy/skipper-cd/issues/94)) ([06e8731](https://github.com/polandy/skipper-cd/commit/06e8731e7fd1a0278acf000ee0e720bd0e4f2e31))
* **ui:** health-watch status history in the per-service panel (ADR-0031) ([e141474](https://github.com/polandy/skipper-cd/commit/e141474d77eb5c6efad891b20493324a955ba7e7))


### Bug Fixes

* **ui:** bind deploy error box to its row + carry diffs on rolled-back events ([#95](https://github.com/polandy/skipper-cd/issues/95)) ([368535e](https://github.com/polandy/skipper-cd/commit/368535e6eaf492f02b34c6e994badb6c6b31c520))
* **ui:** enforce one open panel per deploy row ([#98](https://github.com/polandy/skipper-cd/issues/98)) ([4597fe3](https://github.com/polandy/skipper-cd/commit/4597fe3619687d3a56cdd76ab2bf3f590c3dd83a))

## [0.12.0](https://github.com/polandy/skipper-cd/compare/v0.11.0...v0.12.0) (2026-07-16)


### Features

* **deploy:** health-check-gated rollback ([#71](https://github.com/polandy/skipper-cd/issues/71)) ([1eb5bcf](https://github.com/polandy/skipper-cd/commit/1eb5bcf68d67e3eec621d256dac46e5fe47ac2ff))
* **notify:** optional per-target message prefix for signal ([#78](https://github.com/polandy/skipper-cd/issues/78)) ([a480e5d](https://github.com/polandy/skipper-cd/commit/a480e5d8657ebb9853fd3787eb5131f09fc28075))
* **ui:** glyph-only header with a view-options popover ([6d5ee2f](https://github.com/polandy/skipper-cd/commit/6d5ee2fee9023e508ff1ec6ab3ed6501ba43e76a))
* **ui:** live stack health — poller + ADR-0027 ([#83](https://github.com/polandy/skipper-cd/issues/83)) ([65dc66d](https://github.com/polandy/skipper-cd/commit/65dc66d033673ee6cb13c2ea9b66e54bc4a12f65))
* **ui:** prompt to reload when a new PWA version is waiting ([#74](https://github.com/polandy/skipper-cd/issues/74)) ([489d291](https://github.com/polandy/skipper-cd/commit/489d291196cea5a40af6115aa337e08dfc5b9d54))
* **ui:** render live stack-health pill in the deploy view ([#84](https://github.com/polandy/skipper-cd/issues/84)) ([3bd28af](https://github.com/polandy/skipper-cd/commit/3bd28af837915cca8ee6891c2bcf960bc2633d34))
* **ui:** show commit metadata in the diff panel ([c706a84](https://github.com/polandy/skipper-cd/commit/c706a84ffc8935d631c2456c1a315aeeb2647c5a))
* **ui:** show repo-relative file paths in deploy events ([c41d32d](https://github.com/polandy/skipper-cd/commit/c41d32d6b464d73dca56e8e7a0a25771ddd8e8e7))
* **ui:** type-to-search filter for the deploys view ([#77](https://github.com/polandy/skipper-cd/issues/77)) ([2a74f45](https://github.com/polandy/skipper-cd/commit/2a74f45701af2f545f70349a7fa7b4971e53f1a9))
* **ui:** upcoming-deploys look-ahead in the header ([#76](https://github.com/polandy/skipper-cd/issues/76)) ([eaf7c77](https://github.com/polandy/skipper-cd/commit/eaf7c7761965e58cb947156cb04e203d7679534b))


### Bug Fixes

* **deploy:** carry diffs on a reconciled _nixos success ([28d1c54](https://github.com/polandy/skipper-cd/commit/28d1c54d6dfee74088bdf55fd704988e148b0f27))
* **deploy:** keep the diff base while a change is queued ([d8763a8](https://github.com/polandy/skipper-cd/commit/d8763a80c76f9352c1487240253f27655b28c00f))
* **deploy:** reconcile a self-restart-interrupted nixos-rebuild into success ([#79](https://github.com/polandy/skipper-cd/issues/79)) ([d1d6c29](https://github.com/polandy/skipper-cd/commit/d1d6c296e343ecfd69e306dcb57adde64c4b483d))

## [0.11.0](https://github.com/polandy/skipper-cd/compare/v0.10.0...v0.11.0) (2026-07-14)


### Features

* **ui:** configurable UI themes with an opt-in per-browser switcher ([59d01ce](https://github.com/polandy/skipper-cd/commit/59d01ce5c953dfad51c295f65b321fb98f373041))

## [0.10.0](https://github.com/polandy/skipper-cd/compare/v0.9.0...v0.10.0) (2026-07-14)


### Features

* **notify:** outbound deploy notifications ([3e3bbc9](https://github.com/polandy/skipper-cd/commit/3e3bbc91f76dfae4f7473ed70e2d34cb3972e72c))

## [0.9.0](https://github.com/polandy/skipper-cd/compare/v0.8.1...v0.9.0) (2026-07-13)


### Features

* **ui:** installable PWA with app-shell caching ([90ba588](https://github.com/polandy/skipper-cd/commit/90ba588f2103c4b03228cd77a8499002d1a88e37))
* **ui:** show branch and commit in the header build identity ([1c7a384](https://github.com/polandy/skipper-cd/commit/1c7a384ca569240c74ed3b5ee96b9597091e08c1))
* **ui:** version tooltip and portrait header display ([310c9c3](https://github.com/polandy/skipper-cd/commit/310c9c3e447ee20d0224979103b57e26ed48958a))


### Bug Fixes

* **autosync:** collapse UI overrides to inherit at the baseline ([ca7184c](https://github.com/polandy/skipper-cd/commit/ca7184c8e0be7cfd3a36f3846d8f6735943a6e0e))
* **ui:** compact mobile header without horizontal overflow ([bd5e783](https://github.com/polandy/skipper-cd/commit/bd5e7836db2ead0199cb6881b0aa9ceda0da993b))
* **ui:** optically center the header ship logo ([7f2e494](https://github.com/polandy/skipper-cd/commit/7f2e49486d2a0c15c9f85762bc3e6c14b940b353))
* **ui:** replace queued row on resume and carry its diff ([3b670c9](https://github.com/polandy/skipper-cd/commit/3b670c9c5f2c01c8c397b3e8e1edc82fdfd60fd2))

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
