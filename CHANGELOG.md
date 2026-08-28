# Changelog

## [0.25.1](https://github.com/polandy/skipper-cd/compare/v0.25.0...v0.25.1) (2026-08-28)


### Bug Fixes

* **deploy:** retry a failed git sync before giving up the run ([#314](https://github.com/polandy/skipper-cd/issues/314)) ([27a6dd6](https://github.com/polandy/skipper-cd/commit/27a6dd601528b577abeacf26c345bf1b9ee8e330))

## [0.25.0](https://github.com/polandy/skipper-cd/compare/v0.24.0...v0.25.0) (2026-08-28)


### Features

* **ui:** filter the Stacks view to the stacks with an available update ([#310](https://github.com/polandy/skipper-cd/issues/310)) ([453d3d0](https://github.com/polandy/skipper-cd/commit/453d3d0e98729aa12603c5ad1d1c9240451f9b64))


### Bug Fixes

* **ui:** hide the incident badge at zero instead of leaving an empty pill ([#312](https://github.com/polandy/skipper-cd/issues/312)) ([e937b85](https://github.com/polandy/skipper-cd/commit/e937b85740992e089b2be2287b7af849a4c5ac99))

## [0.24.0](https://github.com/polandy/skipper-cd/compare/v0.23.0...v0.24.0) (2026-08-27)


### Features

* **deploy:** report a standing config error once, not every run ([6f12bd2](https://github.com/polandy/skipper-cd/commit/6f12bd2b518b652e06c4ab0a3c4b88ba3c0dc2e0))
* **events:** collapse a repeated outcome into the record it repeats ([725deec](https://github.com/polandy/skipper-cd/commit/725deecdfe4acd5eba18c167332ea669210fd944))


### Bug Fixes

* **deploy:** build from the tree skipper hashed, not the project directory ([d77eb96](https://github.com/polandy/skipper-cd/commit/d77eb96964a868e4a84c757d4472cedfb6570425))

## [0.23.0](https://github.com/polandy/skipper-cd/compare/v0.22.1...v0.23.0) (2026-08-15)


### Features

* **deploy:** record a removed stack in the deploy history ([#303](https://github.com/polandy/skipper-cd/issues/303)) ([5e6fb30](https://github.com/polandy/skipper-cd/commit/5e6fb30bc30b9024e0a376397faae36985c0f70f))

## [0.22.1](https://github.com/polandy/skipper-cd/compare/v0.22.0...v0.22.1) (2026-08-10)


### Bug Fixes

* **ui:** line up the deploy and stack rows on a phone ([#301](https://github.com/polandy/skipper-cd/issues/301)) ([3a28645](https://github.com/polandy/skipper-cd/commit/3a286450875c6367ec0b8e20268196c3b40b8842))

## [0.22.0](https://github.com/polandy/skipper-cd/compare/v0.21.0...v0.22.0) (2026-08-09)


### Features

* **deploy:** mark a success as the retry of a rollback ([67e73b0](https://github.com/polandy/skipper-cd/commit/67e73b0e401d9af119bd5077901493da0d57f895))
* **logs:** a run that changes nothing logs one line ([5fd6761](https://github.com/polandy/skipper-cd/commit/5fd6761f4914e118f5d9dace0826a2dc82835f03))
* **logs:** exempt deploy-outcome lines from log ring eviction ([91df771](https://github.com/polandy/skipper-cd/commit/91df7710683bd37c67a1c836fe98540594591192))
* **logs:** print each changed file's diff on the console ([985c170](https://github.com/polandy/skipper-cd/commit/985c170a368a185ad97e2166c3ff71be0fd010ab))
* **notify:** report the versions a deploy actually put into service ([#272](https://github.com/polandy/skipper-cd/issues/272)) ([8cd360b](https://github.com/polandy/skipper-cd/commit/8cd360b68554877abf7c7d10b223bffb6f02bd91))
* read-only registry update check — "update available" in the Stacks view (ADR-0054) ([b2c8591](https://github.com/polandy/skipper-cd/commit/b2c8591a38d31191290424f9706199e2a2d55ef2))
* **roster:** carry outcome history and incidents on the stacks snapshot ([c29cedb](https://github.com/polandy/skipper-cd/commit/c29cedbe1951685fcb383ea7e8c7d9a0bc870082))
* **ui:** add the flake palette as a sixth built-in ui_theme ([a26b729](https://github.com/polandy/skipper-cd/commit/a26b72986c5dd8b7ff1b189eb45aef386098883f))
* **ui:** chronological log view with quick filters and the console's narrative ([32c5380](https://github.com/polandy/skipper-cd/commit/32c538097e978f0b701957a490b3bb1aee19c61a))
* **ui:** deploys status filter chips and header incident badge ([14182d9](https://github.com/polandy/skipper-cd/commit/14182d9a66b920971e9c9215eb0dc09e48b91a39))
* **ui:** fold repetition out of the per-stack deploy history ([c087ca0](https://github.com/polandy/skipper-cd/commit/c087ca0da36308e8e4e67b870a96c7b6a3b45a54))
* **ui:** fold routine health-timeline cycles and add a per-service phase strip ([caf5a9a](https://github.com/polandy/skipper-cd/commit/caf5a9aed19b7afc276b02d98492e18fd9b0b117))
* **ui:** incident badge toggles its Deploys filter off ([850ee47](https://github.com/polandy/skipper-cd/commit/850ee479fbc20c4f553718530286c639cac4cabf))
* **ui:** outcome strip, last-incident line and retry note ([cfa9edc](https://github.com/polandy/skipper-cd/commit/cfa9edc8cb3a303a364bbf665dca1f3bd05566ab))
* **ui:** severity thresholds and outcome-aware filtering in the log view ([bebf19c](https://github.com/polandy/skipper-cd/commit/bebf19ccf2006e6965a462ffa99bdba9a302e561))
* **update-check:** default update notifications off — the UI is the surface ([#285](https://github.com/polandy/skipper-cd/issues/285)) ([04e54ff](https://github.com/polandy/skipper-cd/commit/04e54ff989d1388e6d82019b6dcdcec7e2be52d7))


### Bug Fixes

* **deploy:** read running images the way docker actually reports them ([#275](https://github.com/polandy/skipper-cd/issues/275)) ([e6c2b70](https://github.com/polandy/skipper-cd/commit/e6c2b70b6dfc750f37484ea43393e2cfb34b7409))
* **deploy:** report a service as removed only when it left the compose file ([#276](https://github.com/polandy/skipper-cd/issues/276)) ([cef5a60](https://github.com/polandy/skipper-cd/commit/cef5a6043e5f62a1b23169ceba206da68ea34266))
* **health:** give the poller vars_file so compose stops warning ([f539108](https://github.com/polandy/skipper-cd/commit/f539108bbd52f6cc4f98ee616cbc66a301f95f1f))
* log discarded persistence errors and close review hygiene items ([#280](https://github.com/polandy/skipper-cd/issues/280)) ([9ae87cf](https://github.com/polandy/skipper-cd/commit/9ae87cf269f53e3952df9f93b6a7828cab059203))
* **logs:** don't pin the no-op run summary ([258b839](https://github.com/polandy/skipper-cd/commit/258b83942d6fbe697091b2802773fa4adee9bc93))
* **notify:** deliver heal_exhausted notifications ([dd5e9f1](https://github.com/polandy/skipper-cd/commit/dd5e9f1a8f7ab0f1e70ff2d24b53d610f7cd519f))
* **notify:** shorten image digests to docker's short form in version lines ([#277](https://github.com/polandy/skipper-cd/issues/277)) ([b4fd399](https://github.com/polandy/skipper-cd/commit/b4fd39909eb9f3e23b0357029f5e9c81e31c96c2))
* **peers:** poll the versioned /api/v1/audit with a 404-only legacy fallback ([#283](https://github.com/polandy/skipper-cd/issues/283)) ([eff2d05](https://github.com/polandy/skipper-cd/commit/eff2d0582575c68c0546ea407a99ca3f5319ef80))
* **test:** hold the wildcard address so the listen-failure test fails on macOS ([5ef0c9b](https://github.com/polandy/skipper-cd/commit/5ef0c9b06f4ca35a1df7dedf7d2d07ff006b6556))
* **ui:** keep a roster row's glyphs, version and name on one line ([#286](https://github.com/polandy/skipper-cd/issues/286)) ([1a98331](https://github.com/polandy/skipper-cd/commit/1a983312646bdb7860ef58d8d75024d775eb5114))
* **ui:** make the Logs view fit — and use — every display ([9559396](https://github.com/polandy/skipper-cd/commit/9559396acad7d0a0d15098f2dcf8df742abbafe0))
* **ui:** publish the roster as soon as discovery knows the stack set ([#273](https://github.com/polandy/skipper-cd/issues/273)) ([402d121](https://github.com/polandy/skipper-cd/commit/402d121a54b4311a6ebe96c9f9e5256f8c78d82b))
* **ui:** recover a page whose deploy stream went away ([#288](https://github.com/polandy/skipper-cd/issues/288)) ([2b88de4](https://github.com/polandy/skipper-cd/commit/2b88de453a5cc91912760122532ca444cfb94b16))
* **ui:** subscribe to deploy events before replaying history on SSE connect ([#279](https://github.com/polandy/skipper-cd/issues/279)) ([1232d6d](https://github.com/polandy/skipper-cd/commit/1232d6ddaae7d893e45a03b583ff2ebee65eb140))

## [0.21.0](https://github.com/polandy/skipper-cd/compare/v0.20.0...v0.21.0) (2026-07-30)


### Features

* **ui:** record that a toggle was attempted, and what a stray click hit ([#266](https://github.com/polandy/skipper-cd/issues/266)) ([90b2a39](https://github.com/polandy/skipper-cd/commit/90b2a3998d9244de8414f901f3ebe22abbaaef23))


### Bug Fixes

* **e2e:** close the two layout races that ate the UC11 toggle click ([#267](https://github.com/polandy/skipper-cd/issues/267)) ([ab5e36f](https://github.com/polandy/skipper-cd/commit/ab5e36feb96c9ab6a21bd993d96094a81c2f0898))
* **e2e:** retry the harness launch when a reserved port is stolen ([#268](https://github.com/polandy/skipper-cd/issues/268)) ([4ed82dd](https://github.com/polandy/skipper-cd/commit/4ed82dd6ef3a433180e331fa469e2b9c11f2da9c))
* report a listen failure instead of exiting from its goroutine ([#254](https://github.com/polandy/skipper-cd/issues/254)) ([8d9c035](https://github.com/polandy/skipper-cd/commit/8d9c0354400670c748a7205a9781d159cab3f381))
* **ui:** announce a refused autosync write instead of swallowing it ([#264](https://github.com/polandy/skipper-cd/issues/264)) ([4102191](https://github.com/polandy/skipper-cd/commit/410219165da813718ebee67a5f67242d9132db4a))
* **ui:** keep the web-font swap from shifting the layout ([#269](https://github.com/polandy/skipper-cd/issues/269)) ([e20b3fd](https://github.com/polandy/skipper-cd/commit/e20b3fd02924537f177f35b24b0c535c47c0a93b))
* **ui:** keep UI diagnostics where a failed test can reach them ([#265](https://github.com/polandy/skipper-cd/issues/265)) ([557e54f](https://github.com/polandy/skipper-cd/commit/557e54f919c0b7a6b669b197253720e2787a0feb))
* **ui:** publish the announce gate's state instead of sleeping past it ([#256](https://github.com/polandy/skipper-cd/issues/256)) ([ee6ebc5](https://github.com/polandy/skipper-cd/commit/ee6ebc5f6b21208baf77a8d34533c222990764c4))
* **ui:** stop the autosync drawer from swallowing switch clicks ([#231](https://github.com/polandy/skipper-cd/issues/231)) ([8db9a57](https://github.com/polandy/skipper-cd/commit/8db9a57d411ad9c4961982c493975e19a43ba7f5))

## [0.20.0](https://github.com/polandy/skipper-cd/compare/v0.19.0...v0.20.0) (2026-07-27)


### ⚠ BREAKING CHANGES

* **config:** a host config carrying a key skipper does not know — including one retired by an earlier rename (working_dir, health_check, health_poll_interval_seconds) — now fails to load instead of starting with the key silently ignored. Run `skipper -config <path> -validate` against each host config before rolling this version out. The NixOS module takes a configFile path and generates no YAML, so it is unaffected.
* **config:** stacks_base_dir is now relative to repo_dir. Replace an absolute value like /var/lib/skipper/repo/stacks with the relative "stacks", or omit it entirely for the repo root.

### Features

* **api:** add GET /api/v1/snapshot and dogfood it in the UI ([fd857f0](https://github.com/polandy/skipper-cd/commit/fd857f00e4c7dd3ccf3c1b853546475a9a1f7500))
* **config:** add per-stack and global rollback opt-out ([#205](https://github.com/polandy/skipper-cd/issues/205)) ([92b589f](https://github.com/polandy/skipper-cd/commit/92b589f62c1e0ab585d985badd2f59357a8c816a))
* **config:** interpret stacks_base_dir relative to repo_dir ([#180](https://github.com/polandy/skipper-cd/issues/180)) ([75a6676](https://github.com/polandy/skipper-cd/commit/75a6676459d3e602330f45be7f094f3e132813b1))
* **config:** make webhook_secret optional, reconcile is the baseline ([#204](https://github.com/polandy/skipper-cd/issues/204)) ([d72a731](https://github.com/polandy/skipper-cd/commit/d72a73117b489a92688d20cb7eff730d2e5ac3ea))
* **config:** reject unknown keys and add -validate to check a config ([136a9e7](https://github.com/polandy/skipper-cd/commit/136a9e7af70c90978c702db6b7dde34127caf480))
* **deploy:** a bootstrap run converges the host without refreshing images ([2cf1030](https://github.com/polandy/skipper-cd/commit/2cf1030baf6d65b767e55358893a5ccedf42f937))
* **notify:** name changed services with old→new image in deploy notifications ([#206](https://github.com/polandy/skipper-cd/issues/206)) ([e103fc8](https://github.com/polandy/skipper-cd/commit/e103fc82f77a89aa520dca4768be8b9a77b0c7ef))
* **ui-preview:** seed the failure states, and let skipper check the config ([29d40d3](https://github.com/polandy/skipper-cd/commit/29d40d3b7c85bc313810785eab30127ae02083d7))
* **ui:** accessibility sweep — contrast, focus, live regions, keyboard (T2.5–T2.10) ([#192](https://github.com/polandy/skipper-cd/issues/192)) ([63d508a](https://github.com/polandy/skipper-cd/commit/63d508ace568c141b8407fb3a3bc3795f8cc14aa))
* **ui:** add a Version column naming which service a deploy updated ([744ebc8](https://github.com/polandy/skipper-cd/commit/744ebc8a39a40382cea531c2d5e892ce460aa623))
* **ui:** always-visible search trigger — discoverable stack filter (T3.11) ([#193](https://github.com/polandy/skipper-cd/issues/193)) ([7532671](https://github.com/polandy/skipper-cd/commit/7532671eb4faf0c6c77ac220b8e7396a50bcab53))
* **ui:** answer "why did nothing happen" per stack in the Stacks roster ([8a5bce0](https://github.com/polandy/skipper-cd/commit/8a5bce08fbc38e2dd037712626c0057e982a3fb7))
* **ui:** collapse deploy-row secondary actions into ⋯ overflow menu (T3.13) ([#197](https://github.com/polandy/skipper-cd/issues/197)) ([9276096](https://github.com/polandy/skipper-cd/commit/9276096c8e9705cc89c75912e5e5416620cbb575))
* **ui:** filter container logs to one or more services ([#207](https://github.com/polandy/skipper-cd/issues/207)) ([538ea3f](https://github.com/polandy/skipper-cd/commit/538ea3f4120aa11a158667175bf66adbedb19ce6))
* **ui:** first-run header tour teaches the glyph-only controls (T3.15) ([1c69417](https://github.com/polandy/skipper-cd/commit/1c69417280c650fc7560d42b0c174dd31c03009a))
* **ui:** fold roster-row secondary actions into ⋯ overflow menu (T3.13b) ([#198](https://github.com/polandy/skipper-cd/issues/198)) ([20ac739](https://github.com/polandy/skipper-cd/commit/20ac73958b8139b2de0cb594ff0fdd50f64d55ea))
* **ui:** link every commit SHA to its commit on the forge ([#226](https://github.com/polandy/skipper-cd/issues/226)) ([65e69ee](https://github.com/polandy/skipper-cd/commit/65e69eedee988835e45f6a19ed026cf97eebdd14))
* **ui:** loading skeleton + retryable fetch-errors (T4.16/T4.17) ([#201](https://github.com/polandy/skipper-cd/issues/201)) ([7012acc](https://github.com/polandy/skipper-cd/commit/7012accee72a3f0029be8bcc37f901192f7481aa))
* **ui:** multi-host federated UI — fan peers' read data into one merged view (ADR-0048) ([0c8207f](https://github.com/polandy/skipper-cd/commit/0c8207f8364c42ad02fc53edac3fe9c9827ef765))
* **ui:** peer container logs — proxy a peer's container-logs SSE (ADR-0048) ([#189](https://github.com/polandy/skipper-cd/issues/189)) ([ae86615](https://github.com/polandy/skipper-cd/commit/ae86615cc36407550e3041e3506735538910703a))
* **ui:** peer health parity — inline health, containers & app-links for peers (ADR-0048) ([19e31d4](https://github.com/polandy/skipper-cd/commit/19e31d472ebfb1ec1bc0ef1948c8ac407ea3c7f8))
* **ui:** persist the Hosts filter per browser (ADR-0048 amendment) ([ce67f75](https://github.com/polandy/skipper-cd/commit/ce67f757e8bf12ca1884686046bba96ca92b3d6e))
* **ui:** show each service's running version in the Stacks view ([#216](https://github.com/polandy/skipper-cd/issues/216)) ([2ce78b6](https://github.com/polandy/skipper-cd/commit/2ce78b6f55817ce463c19c292135ab93966e80fa))
* **ui:** status-badge icons + solid worst-state chips (T3.14) ([#199](https://github.com/polandy/skipper-cd/issues/199)) ([f2e2e96](https://github.com/polandy/skipper-cd/commit/f2e2e968ebff5715ac7f7fca96b9d10e35c771de))
* **ui:** surface roster logs/hooks inline instead of behind the ⋯ menu ([f0133f1](https://github.com/polandy/skipper-cd/commit/f0133f19a84ecf74ea45580cad0927a2b869a1b1))
* **ui:** surface unhealthy stacks via a header beacon + attention band ([#190](https://github.com/polandy/skipper-cd/issues/190)) ([3ed2564](https://github.com/polandy/skipper-cd/commit/3ed2564f6a045217be546c73b17074d4d9321d71))
* **ui:** view-toggle active bar + options caret (T3.12) ([#196](https://github.com/polandy/skipper-cd/issues/196)) ([373c22c](https://github.com/polandy/skipper-cd/commit/373c22c1a4187e9a2b5d4d1fbe1f0607f33e3463))


### Bug Fixes

* **config:** redact repo_url credentials in the -validate report ([#222](https://github.com/polandy/skipper-cd/issues/222)) ([8a4ccbe](https://github.com/polandy/skipper-cd/commit/8a4ccbe1857aeef49d1678420d4baf116d7d0cbf))
* **notify:** skip image-change diff when compose file fails to parse ([#208](https://github.com/polandy/skipper-cd/issues/208)) ([8c0aeee](https://github.com/polandy/skipper-cd/commit/8c0aeee8388c4ab45f35a89c96c0900f6c392878))
* reject cross-site writes and cap concurrent container-log streams ([3864a91](https://github.com/polandy/skipper-cd/commit/3864a91c4e6eb081b4a75605ef8c16abb64a3807))
* **ui-preview:** use the current names for two retired config keys ([53b635b](https://github.com/polandy/skipper-cd/commit/53b635b3a88f2341b2aedaab6e1026a4cd29df0a))
* **ui:** carry the state baseline on the stream so a connecting client misses nothing ([255e147](https://github.com/polandy/skipper-cd/commit/255e1474204165e55490dcb3c3afa77952c1e47f))
* **ui:** keep an open roster panel across a stacks republish ([#227](https://github.com/polandy/skipper-cd/issues/227)) ([f749aa2](https://github.com/polandy/skipper-cd/commit/f749aa2be837266bfdafc5406402421f19e8f410))
* **ui:** let the pending tag give way before the stack name does ([2a48d1c](https://github.com/polandy/skipper-cd/commit/2a48d1ccc94c8efd8498a64755679cf63f9ddd13))
* **ui:** make drawer open-focus deterministic (de-flake UAA4) ([c843b8b](https://github.com/polandy/skipper-cd/commit/c843b8b458092876322721eb24a3b8c7e557eb5a))
* **ui:** make the peer-detail panel's accent line continuous with its row ([60c06ed](https://github.com/polandy/skipper-cd/commit/60c06ed0e93e3096d112c7813e3aacb2185e1bf0))
* **ui:** make the Version column readable on tablet and phone ([#225](https://github.com/polandy/skipper-cd/issues/225)) ([177b55e](https://github.com/polandy/skipper-cd/commit/177b55e39f9c6eec77d9c171a0e1b1db3da64e34))
* **ui:** move the deploy-row health pill into the status column ([0bdf66b](https://github.com/polandy/skipper-cd/commit/0bdf66bdd571fb26a4d2846936bfd09738c06904))
* **ui:** order autosync snapshots by version so a switch never snaps back ([020093b](https://github.com/polandy/skipper-cd/commit/020093b5a193192a95a2a27694068035c5314899))
* **ui:** repair four Tier-1 UI defects ([#191](https://github.com/polandy/skipper-cd/issues/191)) ([01ff63a](https://github.com/polandy/skipper-cd/commit/01ff63ad58db80087acf4d4ff4b7c3f45705fc27))

## [0.19.0](https://github.com/polandy/skipper-cd/compare/v0.18.0...v0.19.0) (2026-07-21)


### ⚠ BREAKING CHANGES

* **config:** rename health_check to deploy_health_check, health_poll_interval_seconds to runtime_health_poll_interval_seconds
* **config:** working_dir must become project_directory in any existing skipper.yaml before upgrading — the YAML decoder is not strict, so a stale key is silently ignored rather than erroring.

### Features

* **config:** absolute-path checks, dead self_heal override warning, hook-timeout warning ([0a38a37](https://github.com/polandy/skipper-cd/commit/0a38a378114428b24a83366689ca7435e1ee4a5f))
* **config:** default stack_discovery to true (ADR-0043) ([0439e66](https://github.com/polandy/skipper-cd/commit/0439e66b8725e3d3a9c23f6daa87cd57a7caef28))
* **config:** fold per-stack overrides into the host config (ADR-0043) ([c7ca5ee](https://github.com/polandy/skipper-cd/commit/c7ca5eed4f3e61af8ed5fcd2bd87871efd320d71))
* **config:** require webhook_secret ([7ee4134](https://github.com/polandy/skipper-cd/commit/7ee41349f7971f850b1f4ddc582d8f761da31102))
* **config:** shift compose validation to discovery + extract internal/compose ([eb992e6](https://github.com/polandy/skipper-cd/commit/eb992e6d33fb6c5333b4660e9aa0022a20b5fa1e))
* **config:** validate rollout service names against compose at discovery ([5a57ec6](https://github.com/polandy/skipper-cd/commit/5a57ec6e03dd125821170037e2820921de60ee86))
* **config:** validate vars_file and working_dir, add startup warnings ([246b706](https://github.com/polandy/skipper-cd/commit/246b706239dceabc96413abaf30131219008cb83))
* **deploy:** auto-gate deploys on the compose file's own healthcheck ([9bc6f6e](https://github.com/polandy/skipper-cd/commit/9bc6f6ee2a155b55e6562d694f1ff7876a52400f))
* **deploy:** explicit + on-demand opt-out for the auto health gate (ADR-0049) ([9516247](https://github.com/polandy/skipper-cd/commit/951624727044bf7b80c44df48fc86746a357c3bf))
* **deploy:** warn when on_demand_containers doesn't match a container_name ([2407363](https://github.com/polandy/skipper-cd/commit/2407363270534de5cfa64bbba1ea311bd215721f))
* **deploy:** zero-downtime rollout for Traefik-fronted services ([#146](https://github.com/polandy/skipper-cd/issues/146)) ([7e41d4d](https://github.com/polandy/skipper-cd/commit/7e41d4df060228d32916ade997bede27e0d39947))
* **logging:** add pretty console output, make it the default log_format ([6df79a2](https://github.com/polandy/skipper-cd/commit/6df79a296a286c6ff761d5d44971c7ebee7479a2))
* **ui:** generalize tap-tip bubble to every glyph-only control ([0024ab7](https://github.com/polandy/skipper-cd/commit/0024ab7243ee98c13b3e4d8f8632bf9b9003ffce))
* **ui:** restyle the Logs view as a page-sized clog-panel ([7d01fe4](https://github.com/polandy/skipper-cd/commit/7d01fe43d930a248fc2ba35ac0a7e7ddefc95f73))
* **ui:** serve the app shell gzip-compressed when the client supports it ([d9a2aba](https://github.com/polandy/skipper-cd/commit/d9a2aba68dcaac3c8bdc52066a37a4a66395083b))


### Bug Fixes

* **selfheal:** never heal an idle on-demand container ([#172](https://github.com/polandy/skipper-cd/issues/172)) ([07a7115](https://github.com/polandy/skipper-cd/commit/07a71157c526eac133f153335da5b5acf5726456))


### Code Refactoring

* **config:** rename health_check to deploy_health_check, health_poll_interval_seconds to runtime_health_poll_interval_seconds ([cb8fd13](https://github.com/polandy/skipper-cd/commit/cb8fd13cc0b1a0f89d5d9502b469948b2411f545))
* **config:** rename working_dir to project_directory, add project_directory_base ([f09689e](https://github.com/polandy/skipper-cd/commit/f09689ee6b480734e5eac0ded897959d24e0e5f9))

## [0.18.0](https://github.com/polandy/skipper-cd/compare/v0.17.0...v0.18.0) (2026-07-19)


### Features

* **deploy:** pre-/post-deploy hooks for backup-before-update ([#139](https://github.com/polandy/skipper-cd/issues/139)) ([c3e65d7](https://github.com/polandy/skipper-cd/commit/c3e65d7f8e07ae176fd679cf30b7f4a27aa30f5b))
* **hooks:** surface deploy hooks in the web UI ([#145](https://github.com/polandy/skipper-cd/issues/145)) ([c860ff2](https://github.com/polandy/skipper-cd/commit/c860ff28c04e0f0e204b6fd210e8336c2695755f))
* **ui:** app-link icon for Traefik-routed stacks (ADR-0041) ([6f07c01](https://github.com/polandy/skipper-cd/commit/6f07c01060f86adf03668725641f09fe9be966cc))

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
