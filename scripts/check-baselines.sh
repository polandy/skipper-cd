#!/usr/bin/env sh
# Fail if the visual-snapshot baselines are not real PNGs.
#
# The Playwright container ships no git-lfs, so CI checks the baselines out in a
# separate job and hands them over as an artifact that overwrites the pointer
# files this checkout wrote (ADR-0052). Nothing else notices when that hand-off
# breaks: a pointer file is valid text, so a pixel compare against one fails with
# an image-decode error that reads like a broken test, and an empty directory
# makes the compares pass against nothing at all.
#
# Locally this is only worth running on the paths that need real pixels —
# regenerating a baseline, or a RUN_SNAPSHOTS=1 run. A plain local `make e2e-ui`
# compares behaviour rather than pixels, so pointers are harmless there.
set -eu

cd "$(dirname "$0")/.."

dir=e2e/ui/__screenshots__

if [ ! -d "$dir" ]; then
	echo "ERROR: $dir does not exist — the baselines were never checked out."
	exit 1
fi

count=$(find "$dir" -name '*.png' | wc -l)
if [ "$count" -eq 0 ]; then
	echo "ERROR: no baseline PNGs under $dir."
	echo 'In CI this means the ui-baselines artifact was empty or unpacked'
	echo 'elsewhere; pixel compares would silently have nothing to compare.'
	exit 1
fi

# A pointer file is a few lines of text opening with the LFS spec URL; a real
# PNG starts with the binary signature. Grep the first line rather than trusting
# the file size, so a large pointer or a tiny PNG is still classified correctly.
pointers=$(find "$dir" -name '*.png' -exec grep -l '^version https://git-lfs\.github\.com/spec/' {} + 2>/dev/null || true)
if [ -n "$pointers" ]; then
	echo 'ERROR: these baselines are still git-lfs pointer files, not PNGs:'
	echo "$pointers" | sed 's/^/  /'
	echo
	echo 'In CI: the ui-baselines artifact did not overwrite them (check the'
	echo 'baselines job and the download-artifact path).'
	echo 'Locally: git-lfs is not installed or its filter is not configured —'
	echo 'install it, then `git lfs pull`.'
	exit 1
fi

echo "OK: $count baseline PNGs, none of them pointers."
