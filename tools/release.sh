#!/bin/sh
# Veröffentlicht ein Release auf GitHub: signiertes APT-Repo über GitHub Pages (Branch gh-pages)
# und das ISO als Release-Asset. Der Schritt ist von außen sichtbar – deshalb erst nach e2e ausführen.
#
# Aufruf: tools/release.sh <owner/repo> <version>
# Voraussetzungen: `gh auth login`, vorhandenes Repo mit aktivierten Pages (Quelle: Branch gh-pages),
#   dist/betternas-<version>-amd64.iso und dist/betternas_<version>_amd64.deb (ISO mit APT_URL gebaut:
#   make remote VERSION=<version> APT_URL=https://<owner>.github.io/<repo> MODES="uefi bios secureboot"),
#   Signierschlüssel unter ~/.betternas/apt-signing-key.asc.
set -eu
REPO="${1:?GitHub-Repo fehlt (owner/repo)}"
VERSION="${2:?Version fehlt}"
SRC="$(cd "$(dirname "$0")/.." && pwd)"
ISO="$SRC/dist/betternas-$VERSION-amd64.iso"
DEB="$SRC/dist/betternas_${VERSION}_amd64.deb"
for f in "$ISO" "$DEB"; do [ -f "$f" ] || { echo "FEHLER: $f fehlt" >&2; exit 1; }; done
command -v gh >/dev/null || { echo "FEHLER: gh fehlt" >&2; exit 1; }

echo "[release] Aktualisiere APT-Repo (dist/apt) …"
"$SRC/tools/apt-repo.sh" "$SRC/dist/apt" "$DEB"
cp "$SRC/packaging/apt/betternas-archive-keyring.gpg" "$SRC/dist/apt/"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
URL="https://github.com/$REPO.git"
if git ls-remote --exit-code --heads "$URL" gh-pages >/dev/null 2>&1; then
	git clone -q --depth 1 --branch gh-pages "$URL" "$TMP/pages"
else
	mkdir "$TMP/pages" && git -C "$TMP/pages" init -q -b gh-pages && git -C "$TMP/pages" remote add origin "$URL"
fi
rsync -a --delete --exclude /.git "$SRC/dist/apt/" "$TMP/pages/"
touch "$TMP/pages/.nojekyll"
git -C "$TMP/pages" add -A
git -C "$TMP/pages" commit -q -m "APT-Repo: betternas $VERSION" || echo "[release] APT-Repo unverändert"
git -C "$TMP/pages" push -q origin gh-pages

echo "[release] Erzeuge Release v$VERSION …"
(cd "$SRC/dist" && sha256sum "$(basename "$ISO")" > "$(basename "$ISO").sha256")
gh release create "v$VERSION" --repo "$REPO" --title "BetterNAS $VERSION" --generate-notes \
	"$ISO" "$ISO.sha256"
echo "Fertig. APT-Quelle: https://${REPO%%/*}.github.io/${REPO##*/}"
