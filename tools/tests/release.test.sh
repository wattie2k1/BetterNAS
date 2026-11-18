#!/bin/sh
# Test für tools/release.sh (Trockenlauf mit temporärem Repo und Dummy-gh; baut, taggt und pusht nichts).
set -u
DIR="$(cd "$(dirname "$0")/../.." && pwd)"
T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT
fail=0
err() { echo "FEHLER: $*" >&2; fail=1; }

git init -q --bare "$T/origin.git"
git clone -q "$T/origin.git" "$T/w" 2>/dev/null
cd "$T/w" || exit 1
git config user.email t@example.org; git config user.name t
git checkout -q -b main
mkdir tools
cp "$DIR/tools/release.sh" "$DIR/tools/deb-version.sh" tools/
echo x > a; git add -A; git commit -q -m "start"
git push -q origin main 2>/dev/null
git tag v0.7.0; git tag v0.7.1-rc1; git tag v0.7.1-rc2; git push -q origin --tags 2>/dev/null

mkdir "$T/bin"
printf '#!/bin/sh\nexit 0\n' > "$T/bin/gh"; chmod +x "$T/bin/gh"
rel() { PATH="$T/bin:$PATH" sh tools/release.sh "$@"; }

# Vorschlag: nächste Patch-Version (0.7.1, dort gibt es rc1+rc2), Kanal testing -> rc3
out="$(printf '\n1\n' | rel --dry-run --no-iso 2>&1)" || err "Trockenlauf (testing) scheiterte: $out"
echo "$out" | grep -q 'Letzte stabile Version: v0.7.0' || err "letzte stabile Version fehlt"
echo "$out" | grep -q 'Tag:        v0.7.1-rc3' || err "testing: erwartet v0.7.1-rc3: $out"
echo "$out" | grep -q 'nur testing' || err "Kanaltext testing fehlt"
echo "$out" | grep -q '\[trocken\] git push origin v0.7.1-rc3' || err "Push nicht angezeigt"

# neue Version: erster rc, Paketversion mit ~
out="$(rel --dry-run --no-iso --version 0.7.5 --channel testing 2>&1)" || err "rc1: $out"
echo "$out" | grep -q 'Tag:        v0.7.5-rc1' || err "erwartet v0.7.5-rc1: $out"
echo "$out" | grep -q 'Paketversion 0.7.5~rc1' || err "Paketversion mit ~ fehlt"

# stable + testing
out="$(printf '2\n' | rel --dry-run --no-iso --version 0.8.0 2>&1)" || err "stable: $out"
echo "$out" | grep -q 'Tag:        v0.8.0 ' || err "erwartet v0.8.0: $out"
echo "$out" | grep -q 'stable + testing' || err "Kanaltext stable fehlt"

# ISO im Trockenlauf mit passender Suite
out="$(rel --dry-run --version 0.8.0 --channel stable --modes uefi 2>&1)" || err "ISO-Trockenlauf: $out"
echo "$out" | grep -q 'make remote VERSION=0.8.0 REMOTE=betternas-dev MODES="uefi" APT_URL=https://wattie2k1.github.io/BetterNAS APT_SUITE=stable' || err "make-remote-Aufruf falsch: $out"
echo "$out" | grep -q 'release-iso.sh 0.8.0' || err "ISO-Upload nicht angezeigt"
out="$(rel --dry-run --version 0.8.0 --channel testing --modes uefi 2>&1)" || err "ISO testing"
echo "$out" | grep -q 'APT_SUITE=testing' || err "testing-ISO braucht APT_SUITE=testing"
echo "$out" | grep -q 'VERSION=0.8.0~rc1' || err "testing-ISO braucht Paketversion 0.8.0~rc1"

# Fehlerfälle
rel --dry-run --no-iso --version 0.7.0 --channel stable >/dev/null 2>&1 && err "vorhandener Tag hätte scheitern müssen"
rel --dry-run --no-iso --version 0.7 --channel stable >/dev/null 2>&1 && err "ungültige Version hätte scheitern müssen"
rel --dry-run --no-iso --version 0.9.0 --channel beta >/dev/null 2>&1 && err "ungültiger Kanal hätte scheitern müssen"
echo y > dirty
rel --dry-run --no-iso --version 0.9.0 --channel stable >/dev/null 2>&1 && err "unsauberer Baum hätte scheitern müssen"
rm dirty
git commit -q --allow-empty -m "lokal"
rel --dry-run --no-iso --version 0.9.0 --channel stable >/dev/null 2>&1 && err "ungepushter Commit hätte scheitern müssen"
git tag -l | grep -q 'v0.9.0\|v0.8.0\|v0.7.5\|v0.7.1-rc3' && err "Trockenlauf darf keine Tags anlegen"

[ "$fail" = 0 ] && echo "release: OK"
exit "$fail"
