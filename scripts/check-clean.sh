#!/usr/bin/env bash
# Refuse content that should not be in a public repository.
#
# The patterns here are deliberately generic — absolute home directories, real
# email addresses, key material — because this script is itself published. A
# list of the specific names a particular author must not leak belongs in a
# private file; point REPOHOP_EXTRA_PATTERNS at one to have it checked too.
set -uo pipefail

cd "$(dirname "$0")/.."
status=0

report() {
	status=1
	printf '\n%s\n' "$1"
	shift
	printf '%s\n' "$@"
}

search() {
	grep -rniE --exclude-dir=.git --exclude=go.sum --exclude-dir=dist \
		--exclude="$(basename "$0")" "$1" . 2>/dev/null
}

if hits=$(search '/home/[a-z][a-z0-9._-]*/|/Users/[a-z][a-z0-9._-]*/|C:\\+Users\\+[a-z]') && [ -n "$hits" ]; then
	report "Absolute home directories (paths must come from the config or a temporary directory):" "$hits"
fi

# Addresses at example.invalid and noreply are the ones tests and tooling use.
if hits=$(search '[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}' | grep -viE 'example\.invalid|example\.com|noreply|@v[0-9]|users\.noreply') && [ -n "$hits" ]; then
	report "Email addresses (use example.invalid in fixtures):" "$hits"
fi

if hits=$(search 'BEGIN [A-Z ]*PRIVATE KEY|AKIA[0-9A-Z]{16}|ghp_[A-Za-z0-9]{20,}') && [ -n "$hits" ]; then
	report "Possible key material:" "$hits"
fi

if [ -n "${REPOHOP_EXTRA_PATTERNS:-}" ] && [ -f "$REPOHOP_EXTRA_PATTERNS" ]; then
	extra=$(grep -vE '^[[:space:]]*(#|$)' "$REPOHOP_EXTRA_PATTERNS")
	if [ -n "$extra" ]; then
		if hits=$(grep -rniE --exclude-dir=.git --exclude=go.sum --exclude-dir=dist \
			-f <(printf '%s\n' "$extra") . 2>/dev/null) && [ -n "$hits" ]; then
			report "Matches from $REPOHOP_EXTRA_PATTERNS:" "$hits"
		fi
	fi
fi

if [ "$status" -eq 0 ]; then
	echo "public-clean: nothing found"
fi
exit "$status"
