#!/bin/sh
# Test für tools/release-iso.sh (mit Dummy-gh; lädt nichts hoch).
set -u
DIR="$(cd "$(dirname "$0")/../.." && pwd)"
SCRIPT="$DIR/tools/release-iso.sh"
T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT
fail=0
err() { echo "FEHLER: $*" >&2; fail=1; }

mkdir -p "$T/bin" "$T/dist"
# Dummy-gh: protokolliert Aufrufe; "release view" scheitert, wenn $T/norelease existiert.
cat > "$T/bin/gh" <<GH
#!/bin/sh
echo "\$*" >> "$T/gh.log"
if [ "\$1 \$2" = "release view" ] && [ -e "$T/norelease" ]; then exit 1; fi
exit 0
GH
chmod +x "$T/bin/gh"
run() { PATH="$T/bin:$PATH" BNAS_DIST="$T/dist" sh "$SCRIPT" "$@"; }

run >/dev/null 2>&1 && err "ohne Version hätte der Aufruf scheitern müssen"

if run 0.7.0 >/dev/null 2>"$T/err.txt"; then err "fehlendes ISO hätte scheitern müssen"; fi
grep -q "ISO" "$T/err.txt" || err "fehlendes ISO: Fehlermeldung nennt ISO nicht"

printf 'iso' > "$T/dist/betternas-0.7.0-amd64.iso"
: > "$T/gh.log"; touch "$T/norelease"
if run 0.7.0 >/dev/null 2>"$T/err.txt"; then err "fehlendes Release hätte scheitern müssen"; fi
grep -q "Release v0.7.0 existiert nicht" "$T/err.txt" || err "fehlendes Release: Meldung fehlt"
grep -q "release upload" "$T/gh.log" && err "ohne Release darf nichts hochgeladen werden"

rm "$T/norelease"; : > "$T/gh.log"
run 0.7.0 >/dev/null 2>&1 || err "Upload fehlgeschlagen"
grep -q "release upload v0.7.0 .*betternas-0.7.0-amd64.iso .*betternas-0.7.0-amd64.iso.sha256 --repo wattie2k1/BetterNAS --clobber" "$T/gh.log" || err "Upload-Aufruf falsch: $(cat "$T/gh.log")"
[ -s "$T/dist/betternas-0.7.0-amd64.iso.sha256" ] || err "sha256-Datei fehlt"
grep -q "betternas-0.7.0-amd64.iso" "$T/dist/betternas-0.7.0-amd64.iso.sha256" || err "sha256-Datei nennt das ISO nicht"

[ "$fail" = 0 ] && echo "release-iso: OK"
exit "$fail"
