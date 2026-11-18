#!/bin/sh
# Führt alle Shell-Tests unter tools/tests aus.
set -u
DIR="$(cd "$(dirname "$0")" && pwd)"
rc=0
for t in "$DIR"/*.test.sh; do
	echo "== $(basename "$t")"
	sh "$t" || rc=1
done
exit "$rc"
