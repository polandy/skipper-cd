#!/usr/bin/env sh
# Fail if a workflow action or a Dockerfile base image is pinned by a movable tag only.
set -eu

cd "$(dirname "$0")/.."

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
