#!/bin/sh
# Test für image/hooks/rootfs/40-apt.sh (BetterNAS-Paketquelle im Image).
set -u
DIR="$(cd "$(dirname "$0")/../.." && pwd)"
HOOK="$DIR/image/hooks/rootfs/40-apt.sh"
T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT
fail=0
err() { echo "FEHLER: $*" >&2; fail=1; }

# "chroot" und "rm" im Hook arbeiten auf dem Verzeichnis; chroot wird durch ein Dummy ersetzt.
mkdir "$T/bin"
printf '#!/bin/sh\nexit 0\n' > "$T/bin/chroot"
chmod +x "$T/bin/chroot"

newroot() { r="$T/$1"; mkdir -p "$r/etc/apt/sources.list.d" "$r/var/lib/apt/lists"; echo "$r"; }
S() { echo "$1/etc/apt/sources.list.d/betternas.sources"; }

r="$(newroot leer)"
PATH="$T/bin:$PATH" APT_URL='' sh "$HOOK" "$r" >/dev/null 2>&1 || err "ohne APT_URL: Hook fehlgeschlagen"
[ ! -e "$(S "$r")" ] || err "ohne APT_URL darf keine Quelle entstehen"

r="$(newroot standard)"
PATH="$T/bin:$PATH" APT_URL=https://example.test/repo sh "$HOOK" "$r" >/dev/null 2>&1 || err "Standard: Hook fehlgeschlagen"
[ "$(head -n1 "$(S "$r")")" = "# Verwaltet von BetterNAS – nicht manuell bearbeiten" ] || err "Standard: Kopfzeile fehlt"
grep -qxF "URIs: https://example.test/repo" "$(S "$r")" || err "Standard: URIs fehlt"
grep -qxF "Suites: stable" "$(S "$r")" || err "Standard: Suites: stable erwartet"
grep -qxF "Signed-By: /usr/share/keyrings/betternas-archive-keyring.gpg" "$(S "$r")" || err "Standard: Signed-By fehlt"

r="$(newroot testing)"
PATH="$T/bin:$PATH" APT_URL=https://example.test/repo APT_SUITE=testing sh "$HOOK" "$r" >/dev/null 2>&1 || err "testing: Hook fehlgeschlagen"
grep -qxF "Suites: testing" "$(S "$r")" || err "testing: Suites: testing erwartet"

r="$(newroot ungueltig)"
if PATH="$T/bin:$PATH" APT_URL=https://example.test/repo APT_SUITE=beta sh "$HOOK" "$r" >/dev/null 2>"$T/err.txt"; then err "ungültige Suite wurde akzeptiert"; fi
grep -q "APT_SUITE" "$T/err.txt" || err "ungültige Suite: Fehlermeldung nennt APT_SUITE nicht"
[ ! -e "$(S "$r")" ] || err "ungültige Suite: keine Datei erwartet"

[ "$fail" = 0 ] && echo "apt-hook: OK"
exit "$fail"
