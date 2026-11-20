#!/bin/sh
# Test für image/hooks/rootfs/35-kernel.sh (Kernel aus trixie-backports im Image).
set -u
DIR="$(cd "$(dirname "$0")/../.." && pwd)"
HOOK="$DIR/image/hooks/rootfs/35-kernel.sh"
T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT
fail=0
err() { echo "FEHLER: $*" >&2; fail=1; }

# chroot wird durch einen Dummy ersetzt, der die Aufrufe aufzeichnet (dpkg --print-architecture liefert amd64).
mkdir "$T/bin"
cat > "$T/bin/chroot" <<'EOF2'
#!/bin/sh
shift
echo "$*" >> "$CALLS"
[ "$1" = dpkg ] && echo amd64
exit 0
EOF2
chmod +x "$T/bin/chroot"

r="$T/root"
mkdir -p "$r/etc/apt/sources.list.d"
echo "deb http://alt" > "$r/etc/apt/sources.list"
CALLS="$T/calls" PATH="$T/bin:$PATH" sh "$HOOK" "$r" >/dev/null 2>&1 || err "Hook fehlgeschlagen"
SRCDIR="$DIR/packaging/apt"
cmp -s "$SRCDIR/betternas-backports.sources" "$r/etc/apt/sources.list.d/betternas-backports.sources" || err "Backports-Quelle fehlt oder weicht von packaging/apt ab"
cmp -s "$SRCDIR/betternas-kernel.pref" "$r/etc/apt/preferences.d/betternas-kernel.pref" || err "Pin fehlt oder weicht von packaging/apt ab"
grep -qxF "Suites: trixie-backports" "$SRCDIR/betternas-backports.sources" || err "Quelle nennt nicht trixie-backports"
grep -qxF "Pin: release n=trixie-backports" "$SRCDIR/betternas-kernel.pref" || err "Pin nennt nicht trixie-backports"
[ ! -e "$r/etc/apt/sources.list" ] || err "alte sources.list nicht entfernt"
grep -qxF "env DEBIAN_FRONTEND=noninteractive apt-get update" "$T/calls" || err "apt-get update fehlt"
grep -q -- "-t trixie-backports install linux-image-amd64" "$T/calls" || err "Kernel wird nicht aus den Backports installiert"
grep -q -- "--no-install-recommends" "$T/calls" || err "--no-install-recommends fehlt"

[ "$fail" = 0 ] && echo "kernel-hook OK"
exit "$fail"
