#!/bin/sh
# Test für tools/deb-version.sh
set -u
DIR="$(cd "$(dirname "$0")/.." && pwd)"
S="$DIR/deb-version.sh"
fail=0
ok() { [ "$1" = "$2" ] || { echo "FEHLER: $3: erwartet '$2', bekommen '$1'" >&2; fail=1; }; }

ok "$(sh "$S" v0.6.0 2>&1)" "0.6.0 stable" "stabiler Tag"
ok "$(sh "$S" v0.6.0-rc1 2>&1)" "0.6.0~rc1 testing" "Release-Candidate"
ok "$(sh "$S" v10.20.30-rc12 2>&1)" "10.20.30~rc12 testing" "mehrstellige Zahlen"

for bad in v1.2 vfoo v1.2.3-beta1 0.6.0 v1.2.3-rc v1.2.3-rc1-x "v1.2.3
x" ""; do
	if sh "$S" "$bad" >/dev/null 2>&1; then
		echo "FEHLER: ungültiger Tag '$bad' wurde akzeptiert" >&2
		fail=1
	fi
done
if ! sh "$S" v1.2 2>&1 | grep -q "Ungültiger"; then
	echo "FEHLER: Fehlermeldung nicht deutsch/aussagekräftig" >&2
	fail=1
fi

if command -v dpkg >/dev/null 2>&1; then
	dpkg --compare-versions 0.6.0~rc1 lt 0.6.0 || { echo "FEHLER: 0.6.0~rc1 muss unter 0.6.0 liegen" >&2; fail=1; }
fi

[ "$fail" = 0 ] && echo "deb-version: OK"
exit "$fail"
