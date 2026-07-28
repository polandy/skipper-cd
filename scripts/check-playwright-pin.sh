#!/usr/bin/env sh
# Fail if the Playwright version drifts apart across the places that pin it.
#
# The CI container renders the visual baselines and the npm package drives the
# comparison; when the two disagree the symptom is a font-rasterisation pixel
# diff, not an error message, so nothing points at the version. This check turns
# that into a named failure.
#
# Not covered here: the browser binary the flake supplies comes from nixpkgs
# (pkgs.playwright-driver), whose version is not a literal in flake.nix. The
# comment next to it states the version it is matched to, and that comment IS
# compared below — so a flake bump that moves the driver must update the comment
# and will be caught here. Local runs compare behaviour rather than pixels
# (dev-docs/e2e-tests.md §5), so a drift there is far less damaging than in CI.
set -eu

cd "$(dirname "$0")/.."

fail=0
expected=''

# report <file> <version> — record one pin, and flag it when it disagrees with
# the first one seen.
report() {
	printf '  %-40s %s\n' "$1" "$2"
	if [ -z "$expected" ]; then
		expected="$2"
	elif [ "$2" != "$expected" ]; then
		fail=1
	fi
	if [ -z "$2" ]; then
		fail=1
	fi
}

echo 'Playwright version pins:'
report .github/workflows/ci.yml \
	"$(sed -n 's|.*mcr\.microsoft\.com/playwright:v\([0-9][0-9.]*\)-.*|\1|p' .github/workflows/ci.yml | head -1)"
report .github/workflows/docs.yml \
	"$(sed -n 's|.*mcr\.microsoft\.com/playwright:v\([0-9][0-9.]*\)-.*|\1|p' .github/workflows/docs.yml | head -1)"
report e2e/ui/package.json \
	"$(sed -n 's|.*"@playwright/test": *"\([0-9][0-9.]*\)".*|\1|p' e2e/ui/package.json | head -1)"
report e2e/ui/package-lock.json \
	"$(sed -n '/"node_modules\/@playwright\/test"/,/}/s|.*"version": *"\([0-9][0-9.]*\)".*|\1|p' e2e/ui/package-lock.json | head -1)"
report flake.nix \
	"$(sed -n 's|.*@playwright/test \([0-9][0-9.]*\).*|\1|p' flake.nix | head -1)"

if [ "$fail" -ne 0 ]; then
	echo
	echo 'ERROR: the Playwright pins above are not all the same version.'
	echo 'Set every one of them to the same value — an empty column means the'
	echo 'pin was not found where this check looks, so the pattern moved too.'
	exit 1
fi

echo "OK: all pinned to ${expected}."
