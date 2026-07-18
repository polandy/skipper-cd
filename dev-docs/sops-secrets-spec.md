# Feature Spec: SOPS-Encrypted Env Files

Status: proposal (not accepted)
Date: 2026-07-18

## Goal

Let the deploy repo be *complete*: secrets live in git, encrypted with
[sops](https://github.com/getsops/sops) + [age](https://age-encryption.org),
and skipper decrypts them at deploy time. This is the homelab equivalent of
ArgoCD's secret-management plugins.

Non-goals: sops-encrypted compose files or `vars_file`, other sops backends
(GPG, KMS — age only), secret rotation tooling, editing secrets through
skipper, docker `secrets:` integration.

## Applicability — when you do *not* need this

This feature only ever produces **environment variables in memory** for the
`compose up` process. It never writes a file to disk. That bounds where it
helps, and in a NixOS/sops-nix homelab it often helps very little.

Secrets reach containers three ways; skipper sits in only the first:

1. skipper-level `env_files` in the deploy config → injected into the compose
   process env. **This is the only path skipper-sops covers.**
2. `env_file:` **inside a compose file** → docker-compose reads the path off
   disk itself at `up` time. skipper is not in that loop, so decrypting in
   memory changes nothing.
3. Volume mounts (`/run/secrets/…:/…:ro`) of secret **files** — private keys,
   an `rclone.conf`, an authelia `configuration.yml`, a traefik dynamic-config
   dir. These must exist as real files; skipper-sops cannot produce them.

On top of that, anything rendered by a `sops.templates`-style mechanism
(a secret *interpolated into* surrounding structure, not a flat `KEY=value`
dotenv) is out of scope — skipper-sops decrypts one flat `.sops.env`, it does
not template.

So skipper-sops fits a deployment that treats secrets as **flat env-vars**
(the ArgoCD/12-factor default). A **file-mount-heavy** setup keeps a
host-level secret manager (sops-nix, or any tool that renders files) as the
right layer, because that layer's whole job is producing files on disk —
exactly what this feature cannot do. Concretely, in the reference homelab that
prompted this spec, a single stack (`duckdns`) was a clean candidate; every
other stack needed either a mounted secret file (the shared restic
`rclone.conf` mount alone tied ~16 stacks to sops-nix) or an interpolated
template. Reach for skipper-sops when your secrets are genuinely just env-vars
committed next to the compose file; otherwise it is additive, not a
replacement for the host secret manager.

## User model

```yaml
# skipper.yml
sops:
  age_key_file: /var/lib/skipper/age.key   # optional; default shown
stacks:
  - name: paperless
    env_files:
      - stacks/paperless/secrets.sops.env  # encrypted, committed
      - stacks/paperless/settings.env      # plaintext, as today
```

- An env file is treated as sops-encrypted when its content carries sops
  metadata (dotenv output contains `sops_...=` keys). No filename
  convention required — detection is by content, though `.sops.env` is the
  documented convention.
- The age private key lives **outside** the repo on the host
  (`age_key_file`); the repo only ever holds ciphertext. The NixOS module
  gets a matching `sopsAgeKeyFile` option.

## Decryption

- Via the `sops` binary through `command.Runner`
  (`sops --decrypt --input-type dotenv --output-type dotenv <file>`), like
  docker/git/nixos-rebuild — no Go library dependency, fakes stay trivial.
  `SOPS_AGE_KEY_FILE` is set in the command env from config.
- Output is captured **in memory** and fed into the existing env merge
  (`envfile.go` path). Plaintext never touches disk; precedence is
  unchanged: `env_files` > `vars_file` > `os.Environ()` (invariant 6).
- The sops binary becomes a runtime dependency only when an encrypted file
  is present; the NixOS module adds `sops` + `age` to the service PATH.

## Change detection (invariant 2 interaction)

The **ciphertext** is hashed, exactly as env files are today — no code
change in `hash.go`. Consequences, documented as behavior:

- Editing a secret re-encrypts the file → hash changes → redeploy. Correct.
- Rotating the age recipient re-encrypts every file → all affected stacks
  redeploy. Correct and desirable after rotation.
- sops' MAC covers the ciphertext, so tampering fails decryption, which
  fails the deploy (below) — never a silent partial deploy.

## Failure behavior

- Decryption failure (missing key, bad MAC, sops not installed) is a
  **deploy failure for that stack**: `failed` event, hashes not saved, next
  sync retries. Fail closed — never fall back to treating ciphertext as a
  plaintext env file.
- The error message names the file but never includes file content.
- Startup validation: if any configured env file is sops-encrypted and
  `age_key_file` does not exist, log a prominent warning (not fatal — the
  key may arrive later; the per-stack failure covers the rest).

## Package layout

- New `internal/sops` package: content sniffing (`IsEncrypted([]byte)`) and
  `Decrypt(runner, keyFile, path) ([]byte, error)`.
- `internal/deploy/envfile.go` calls it per env file before parsing;
  `internal/config` gains the `sops` section + validation.

## Testing

- `internal/sops`: table tests, fake Runner asserting exact sops argv +
  env; sniffing positives/negatives (plaintext env with a variable named
  `sops_x` — accept the false positive? see open questions).
- `internal/deploy`: encrypted env file in a stack → decrypted values win
  precedence; decrypt failure → `failed` event, no state save, compose
  never invoked.
- One real-sops integration test gated like `internal/git/integration_test.go`
  (skips when `sops`/`age` missing): encrypt with a throwaway age key,
  deploy, assert the value reached the compose env.

## Open questions

1. Content sniffing vs. explicit `.sops.env` suffix requirement? Sniffing
   is zero-config but a plaintext file containing a `sops_...` variable
   would be mis-detected (then fail decryption → deploy failure, loud not
   silent). Proposed: sniff, document the reserved prefix.
2. Should `vars_file` also support sops? Cheap to add via the same path —
   proposed: yes, same mechanism, one sentence of docs.
