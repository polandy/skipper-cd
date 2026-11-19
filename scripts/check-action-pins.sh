#!/usr/bin/env sh
# Fail if a workflow action or a Dockerfile base image is pinned by tag only.
#
# A tag can be moved by whoever controls the upstream repo; the workflows run
# with a release PAT and registry push rights, so a moved tag would inherit
# both. A commit SHA (with the release as a trailing comment) and an image
# digest cannot move. Dependabot updates both forms in place.
set -eu

cd "$(dirname "$0")/.."

# Local actions (`uses: ./...`) are part of this repo and need no pin.
actions=$(grep -rnE '^[[:space:]-]*uses:' .github/workflows |
	grep -vE 'uses:[[:space:]]*\./' |
	grep -vE 'uses:[[:space:]]*[^@[:space:]]+@[0-9a-f]{40}[[:space:]]+#[[:space:]]*v[0-9]' || true)
images=$(grep -nE '^FROM[[:space:]]' Dockerfile | grep -v '@sha256:[0-9a-f]\{64\}' || true)

if [ -n "$actions$images" ]; then
	echo 'ERROR: not pinned to an immutable reference:'
	[ -z "$actions" ] || echo "$actions" | sed 's/^/  /'
	[ -z "$images" ] || echo "$images" | sed 's/^/  Dockerfile:/'
	echo
	echo 'Pin each action as "owner/repo@<40-char commit SHA> # vX.Y.Z" and each'
	echo 'base image as "image:tag@sha256:<digest>"; Dependabot keeps both current.'
	exit 1
fi

echo 'OK: every action and base image is pinned.'
