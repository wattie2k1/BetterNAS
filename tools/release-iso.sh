#!/bin/sh
# Hängt das ISO an ein bestehendes GitHub-Release an (das Release selbst legt der Workflow nach dem Tag-Push an).
# Aufruf: tools/release-iso.sh <version>
# Voraussetzungen: `gh auth login`; dist/betternas-<version>-amd64.iso (gebaut mit
#   make remote VERSION=<version> APT_URL=https://wattie2k1.github.io/BetterNAS MODES="uefi bios secureboot").
set -eu
REPO="wattie2k1/BetterNAS"
VERSION="${1:?Version fehlt (z. B. 0.7.0 oder 0.7.0-rc1)}"
SRC="$(cd "$(dirname "$0")/.." && pwd)"
DIST="${BNAS_DIST:-$SRC/dist}"
ISO="$DIST/betternas-$VERSION-amd64.iso"
[ -f "$ISO" ] || { echo "FEHLER: ISO $ISO fehlt (make remote …)" >&2; exit 1; }
command -v gh >/dev/null || { echo "FEHLER: gh fehlt" >&2; exit 1; }
gh release view "v$VERSION" --repo "$REPO" >/dev/null 2>&1 || {
	echo "FEHLER: Release v$VERSION existiert nicht – erst den Tag pushen und den Workflow abwarten" >&2
	exit 1
}
if command -v sha256sum >/dev/null 2>&1; then
	(cd "$DIST" && sha256sum "$(basename "$ISO")" > "$(basename "$ISO").sha256")
else
	(cd "$DIST" && shasum -a 256 "$(basename "$ISO")" > "$(basename "$ISO").sha256")
fi
gh release upload "v$VERSION" "$ISO" "$ISO.sha256" --repo "$REPO" --clobber
echo "Fertig: ISO an Release v$VERSION angehängt."
