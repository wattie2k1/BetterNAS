#!/bin/sh
# Leitet aus einem Release-Tag Paketversion und Kanal ab.
# Aufruf: tools/deb-version.sh <tag>      Ausgabe: "<debversion> <stable|testing>"
#   v0.6.0      -> 0.6.0 stable
#   v0.6.0-rc1  -> 0.6.0~rc1 testing   ("~" sortiert in Debian UNTER 0.6.0, "-rc1" läge darüber)
set -eu
TAG="${1:-}"
bad() {
	echo "FEHLER: Ungültiger Release-Tag '$TAG' (erlaubt: vX.Y.Z oder vX.Y.Z-rcN)" >&2
	exit 1
}
case "$TAG" in
*"
"*) bad ;;
esac
printf '%s\n' "$TAG" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-rc[0-9]+)?$' || bad
VERSION="${TAG#v}"
case "$VERSION" in
*-rc*) echo "$(echo "$VERSION" | sed 's/-rc/~rc/') testing" ;;
*) echo "$VERSION stable" ;;
esac
