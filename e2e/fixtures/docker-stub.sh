#!/bin/sh
# Fake `docker`, first on PATH for both E2E harnesses: the Go one
# (e2e/harness_test.go) and the Playwright one (e2e/ui/fixtures/harness.ts).
# One file so a change reaches both — they used to hold separate copies whose
# comments claimed to be identical while they had long diverged.
#
# Every invocation is recorded as "<cwd>\t<args>" in $DOCKER_LOG; behaviour is
# scripted through env vars so one stub can drive every status either suite
# needs.
#
# STUB_DOCKER_UI gates the branches only the UI suite scripts (orphans,
# container logs, per-stack health). Unset — the Go harness — this file behaves
# exactly as its stub did before they were merged, which is what makes the merge
# safe rather than merely tidy.
dir=$(pwd)
printf '%s\t%s\n' "$dir" "$*" >> "$DOCKER_LOG"

if [ -n "$STUB_DOCKER_UI" ]; then
  # Orphan detection (ADR-0036): `volume ls` / `ps -a` answered from a shared
  # listing (setOrphans/setVolumes). Matched before the health `ps` case: `ps -a`
  # also contains " ps ", but health's `ps --format json --all` never does.
  case " $* " in
    *" volume "*)
      [ -n "$STUB_DOCKER_ORPHANS_DIR" ] && [ -f "$STUB_DOCKER_ORPHANS_DIR/volumes.txt" ] && cat "$STUB_DOCKER_ORPHANS_DIR/volumes.txt"
      exit 0
      ;;
    *" ps -a "*)
      [ -n "$STUB_DOCKER_ORPHANS_DIR" ] && [ -f "$STUB_DOCKER_ORPHANS_DIR/orphans.txt" ] && cat "$STUB_DOCKER_ORPHANS_DIR/orphans.txt"
      exit 0
      ;;
  esac

  # Container logs (ADR-0037): a fixed backlog then, with --follow, a periodic
  # tail. --no-log-prefix marks a single service (no prefix); otherwise merged, so
  # each line is prefixed with the service. Killed when the request context ends.
  case " $* " in
    *" logs "*)
      base=$(basename "$dir")
      pfx="$base-1  | "
      case " $* " in *" --no-log-prefix "*) pfx="" ;; esac
      printf '%s2026-01-01T00:00:00Z starting %s\n' "$pfx" "$base"
      printf '%s2026-01-01T00:00:01Z listening on :8080\n' "$pfx"
      printf '%s2026-01-01T00:00:02Z GET /health 200 ok\n' "$pfx"
      printf '%s2026-01-01T00:00:03Z ERROR upstream timeout\n' "$pfx"
      case " $* " in
        *" --follow "*)
          i=0
          while :; do i=$((i + 1)); printf '%s2026-01-01T00:01:%02dZ tick %s\n' "$pfx" "$i" "$i"; sleep 1; done
          ;;
      esac
      exit 0
      ;;
  esac

  # Update check (ADR-0054): `docker image inspect --format {{json .RepoDigests}}
  # <name>` — the local half of the digest comparison. Answered from an
  # rd-<sanitized-name>.json seeded by the harness; no file means a locally
  # built image (empty RepoDigests). Matched as one adjacent-word glob — two
  # separate " image " / " inspect " globs would share the middle space and
  # never match (the overlapping-space trap).
  case " $* " in
    *" image inspect "*)
      for last; do :; done
      f="$STUB_DOCKER_PS_DIR/rd-$(printf '%s' "$last" | tr '/:' '__').json"
      if [ -n "$STUB_DOCKER_PS_DIR" ] && [ -f "$f" ]; then cat "$f"; else echo '[]'; fi
      exit 0
      ;;
  esac

  # App-link detection (ADR-0041): the detector lists compose-labelled
  # containers (`ps --filter label=com.docker.compose.project`, one
  # "<id>\t<working_dir>" line each) and then batch-inspects their labels
  # (`inspect --format {{json .Config.Labels}} <id>…`). Both are answered from
  # files the harness seeds; no seed means no app links, which is every other
  # mask. Matched before the health `ps` case, which would otherwise swallow the
  # listing. File names mirror harness.ts's APPLINK_* constants.
  case " $* " in
    *" label=com.docker.compose.project "*)
      f="$STUB_DOCKER_PS_DIR/applinks-ps.txt"
      [ -n "$STUB_DOCKER_PS_DIR" ] && [ -f "$f" ] && cat "$f"
      exit 0
      ;;
    *" {{json .Config.Labels}} "*)
      for a; do
        case "$a" in
          inspect | --format | *{{json*) continue ;;
        esac
        f="$STUB_DOCKER_PS_DIR/applink-labels-$a.json"
        if [ -f "$f" ]; then cat "$f"; printf '\n'; else echo 'null'; fi
      done
      exit 0
      ;;
  esac

  # Health poll: emit the scripted `compose ps --format json` output for this
  # stack (keyed by the project dir's basename), else nothing (-> stopped).
  case " $* " in
    *" ps "*)
      base=$(basename "$dir")
      f="$STUB_DOCKER_PS_DIR/$base.json"
      [ -n "$STUB_DOCKER_PS_DIR" ] && [ -f "$f" ] && cat "$f"
      exit 0
      ;;
  esac
fi

if [ -n "$STUB_DOCKER_ECHO" ]; then
  case " $* " in
    *" up "*) echo "$STUB_DOCKER_ECHO" ;;
  esac
fi

# Health poll, single-file variant: the Go harness scripts one `compose ps`
# answer for every stack. Deliberately falls through rather than exiting, so a
# scripted `ps` still reaches the failure branches below.
if [ -n "$STUB_DOCKER_PS_FILE" ]; then
  case " $* " in
    *" ps "*) cat "$STUB_DOCKER_PS_FILE" ;;
  esac
fi

case " $* " in
  *" up "*)
    if [ -n "$STUB_DOCKER_HOLD_UP" ]; then
      while [ ! -f "$STUB_DOCKER_HOLD_UP" ]; do sleep 0.05; done
    fi
    ;;
esac

if [ -n "$STUB_DOCKER_FAIL_ON" ]; then
  case " $* " in
    *" $STUB_DOCKER_FAIL_ON "*) exit 1 ;;
  esac
fi

if [ -n "$STUB_DOCKER_FAIL_NTH_UP" ]; then
  case " $* " in
    *" up "*)
      c=$(cat "$DOCKER_LOG.upcount" 2>/dev/null || echo 0)
      c=$((c + 1))
      echo "$c" > "$DOCKER_LOG.upcount"
      case ",$STUB_DOCKER_FAIL_NTH_UP," in
        *",$c,"*) exit 1 ;;
      esac
      ;;
  esac
fi
exit 0
