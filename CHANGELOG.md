# Changelog

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
