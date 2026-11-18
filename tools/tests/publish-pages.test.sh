#!/bin/sh
# Test für tools/publish-pages.sh gegen ein lokales Bare-Repo (braucht apt-ftparchive, gpg, dpkg-deb, git).
set -u
DIR="$(cd "$(dirname "$0")/../.." && pwd)"
PUB="$DIR/tools/publish-pages.sh"
for c in apt-ftparchive gpg dpkg-deb git; do
	command -v "$c" >/dev/null 2>&1 || { echo "publish-pages: übersprungen ($c fehlt)"; exit 0; }
done
T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT
fail=0
err() { echo "FEHLER: $*" >&2; fail=1; }

gh="$T/gh"; mkdir -m 700 "$gh"
GNUPGHOME="$gh" gpg --batch --quiet --pinentry-mode loopback --passphrase "" --quick-generate-key "Test <t@t.invalid>" ed25519 sign never >/dev/null 2>&1
GNUPGHOME="$gh" gpg --batch --quiet --pinentry-mode loopback --passphrase "" --export-secret-keys --armor > "$T/key.asc"
: > "$T/empty.asc"
printf 'keyring' > "$T/keyring.gpg"
mkdeb() {
	d="$T/pkg-$1"; mkdir -p "$d/DEBIAN" "$T/debs"
	printf 'Package: betternas\nVersion: %s\nArchitecture: amd64\nMaintainer: t <t@t.invalid>\nDescription: Test\n' "$1" > "$d/DEBIAN/control"
	dpkg-deb --root-owner-group -Zgzip --build "$d" "$T/debs/betternas_$1_amd64.deb" >/dev/null
}
mkdeb 0.1.0; mkdeb 0.2.0
git init -q --bare "$T/origin.git"
pub() { BNAS_APT_KEY="$1" sh "$PUB" "file://$T/origin.git" "$T/keyring.gpg" "$2" "${3:-stable}" ${4:-}; }
show() { git --git-dir="$T/origin.git" show "gh-pages:$1" 2>/dev/null; }
count() { git --git-dir="$T/origin.git" rev-list --count gh-pages 2>/dev/null; }

# fehlender/leerer Schlüssel: nichts wird veröffentlicht
pub "$T/empty.asc" "$T/debs/betternas_0.1.0_amd64.deb" >/dev/null 2>&1 && err "leerer Schlüssel hätte scheitern müssen"
git --git-dir="$T/origin.git" rev-parse --verify -q gh-pages >/dev/null && err "leerer Schlüssel: gh-pages darf nicht entstehen"

# erstes Release: gh-pages existiert noch nicht
pub "$T/key.asc" "$T/debs/betternas_0.1.0_amd64.deb" stable testing >/dev/null 2>&1 || err "erstes Release fehlgeschlagen"
show dists/stable/InRelease | grep -q "BEGIN PGP SIGNED MESSAGE" || err "InRelease(stable) fehlt"
show dists/testing/InRelease | grep -q "BEGIN PGP SIGNED MESSAGE" || err "InRelease(testing) fehlt"
[ "$(show betternas-archive-keyring.gpg)" = keyring ] || err "Keyring fehlt auf gh-pages"
git --git-dir="$T/origin.git" ls-tree --name-only gh-pages | grep -qx ".nojekyll" || err ".nojekyll fehlt"
[ "$(count)" = 1 ] || err "erste Veröffentlichung: ein Commit erwartet, $(count)"

# zweites Release: vorherige Pakete bleiben, Historie bleibt bei einem Commit (Orphan)
pub "$T/key.asc" "$T/debs/betternas_0.2.0_amd64.deb" testing >/dev/null 2>&1 || err "zweites Release fehlgeschlagen"
show dists/testing/main/binary-amd64/Packages | grep -q '^Version: 0.1.0' || err "0.1.0 muss in testing erhalten bleiben"
show dists/testing/main/binary-amd64/Packages | grep -q '^Version: 0.2.0' || err "0.2.0 fehlt in testing"
show dists/stable/main/binary-amd64/Packages | grep -q '^Version: 0.2.0' && err "0.2.0 (rc) darf nicht in stable landen"
show dists/stable/main/binary-amd64/Packages | grep -q '^Version: 0.1.0' || err "0.1.0 muss in stable erhalten bleiben"
[ "$(count)" = 1 ] || err "Historie wächst: $(count) Commits"

[ "$fail" = 0 ] && echo "publish-pages: OK"
exit "$fail"
