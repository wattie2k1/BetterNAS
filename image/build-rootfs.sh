#!/bin/sh
# Baut das Root-Dateisystem des BetterNAS-Zielsystems.
# Aufruf (im Builder-Container, als root): image/build-rootfs.sh <arch>
set -eu

ARCH="${1:?Architektur fehlt (amd64|arm64)}"
SRC="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$SRC/dist"
TAR="$OUT/rootfs-$ARCH.tar"
SQUASH="$OUT/rootfs-$ARCH.squashfs"
mkdir -p "$OUT"

PACKAGES="$(sed -e 's/#.*//' -e "s/@ARCH@/$ARCH/g" "$SRC/image/packages/rootfs.list" | tr -s ' \n' ',' | sed 's/^,//; s/,$//')"
# Eigene Pakete: mmdebstrap prüft den Pfad außerhalb und installiert ihn innerhalb des chroot –
# daher liegen sie unter demselben Pfad im Container und (per setup-hook) im chroot.
PKGDIR=/tmp/betternas-pkgs
rm -rf "$PKGDIR"
mkdir -p "$PKGDIR"
EXTRA=""
for deb in "$OUT"/betternas_*_"$ARCH".deb; do
	[ -e "$deb" ] || continue
	cp "$deb" "$PKGDIR/"
	EXTRA="$EXTRA,$PKGDIR/$(basename "$deb")"
done

mmdebstrap \
	--mode=root \
	--variant=minbase \
	--architectures="$ARCH" \
	--components="main non-free-firmware" \
	--include="$PACKAGES$EXTRA" \
	--setup-hook='mkdir -p "$1'"$PKGDIR"'" && cp -a '"$PKGDIR"'/. "$1'"$PKGDIR"'"/' \
	--customize-hook='cp -a --no-preserve=ownership '"$SRC"'/image/overlay/rootfs/. "$1"/' \
	--customize-hook='rm -rf "$1'"$PKGDIR"'"' \
	--customize-hook='for h in '"$SRC"'/image/hooks/rootfs/*.sh; do sh "$h" "$1" || exit 1; done' \
	trixie "$TAR" http://deb.debian.org/debian

rm -f "$SQUASH"
WORK="$(mktemp -d)"
tar -C "$WORK" -xpf "$TAR"
mksquashfs "$WORK" "$SQUASH" -comp zstd -Xcompression-level 19 -noappend -quiet
rm -rf "$WORK"
ls -lh "$TAR" "$SQUASH"
