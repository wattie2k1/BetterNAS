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
EXTRA=""
for deb in "$OUT"/betternas_*_"$ARCH".deb; do
	[ -e "$deb" ] && EXTRA="$EXTRA,$deb"
done

mmdebstrap \
	--mode=root \
	--variant=minbase \
	--architectures="$ARCH" \
	--components="main non-free-firmware" \
	--include="$PACKAGES$EXTRA" \
	--customize-hook='cp -a '"$SRC"'/image/overlay/rootfs/. "$1"/' \
	--customize-hook='for h in '"$SRC"'/image/hooks/rootfs/*.sh; do sh "$h" "$1" || exit 1; done' \
	trixie "$TAR" http://deb.debian.org/debian

rm -f "$SQUASH"
WORK="$(mktemp -d)"
tar -C "$WORK" -xpf "$TAR"
mksquashfs "$WORK" "$SQUASH" -comp zstd -Xcompression-level 19 -noappend -quiet
rm -rf "$WORK"
ls -lh "$TAR" "$SQUASH"
