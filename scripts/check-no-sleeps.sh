#!/usr/bin/env sh
# Fail if a fixed wait creeps back into the Playwright suite.
#
# Waiting a fixed number of milliseconds for an effect that only *probably*
# lands is the flake this project does not accept: it passes on a fast runner
# and fails on a loaded one, and it hides the real gap, which is that the UI
# does not publish the state the test needs. Playwright's assertions retry on
# their own, so the fix is a signal to assert on — see the announce gate's
# data-announce-ready for the pattern.
set -eu

cd "$(dirname "$0")/.."

hits=$(grep -rn 'waitForTimeout' e2e/ui --include='*.ts' || true)
if [ -n "$hits" ]; then
	echo 'ERROR: fixed waits found in the e2e suite:'
	echo "$hits" | sed 's/^/  /'
	echo
	echo 'Assert on a condition instead. If nothing observable exists to await,'
	echo 'publish it from the UI (e.g. a data- attribute reflecting the state)'
	echo 'and assert on that — a timeout only hides the missing signal.'
	exit 1
fi

echo 'OK: no fixed waits in the e2e suite.'
