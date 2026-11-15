#!/bin/sh
# Baut das bootfähige BetterNAS-ISO mit Installer.
# Aufruf (im Builder-Container, als root): image/build-iso.sh <arch> <version>
set -eu

ARCH="${1:?Architektur fehlt}"
VERSION="${2:?Version fehlt}"
SRC="$(cd "$(dirname "$0")/.." && pwd)"
SQUASH="$SRC/dist/rootfs-$ARCH.squashfs"
INSTALLER="$SRC/bin/$ARCH/betternas-installer"
OUT="$SRC/dist/betternas-$VERSION-$ARCH.iso"
BUILD=/build/live

[ -f "$SQUASH" ] || { echo "FEHLER: $SQUASH fehlt (make rootfs)"; exit 1; }
[ -x "$INSTALLER" ] || { echo "FEHLER: $INSTALLER fehlt"; exit 1; }

# live-build braucht ein Dateisystem mit Gerätedateien – nicht im gemounteten Projektordner bauen.
rm -rf "$BUILD"
mkdir -p "$BUILD"
cd "$BUILD"

APPEND="boot=live components quiet locales=de_DE.UTF-8 keyboard-layouts=de console=tty0 console=ttyS0,115200"
lb config \
	--mode debian \
	--distribution trixie \
	--architectures "$ARCH" \
	--archive-areas "main non-free-firmware" \
	--binary-images iso-hybrid \
	--bootloaders "grub-efi,syslinux" \
	--uefi-secure-boot enable \
	--debian-installer none \
	--apt-recommends false \
	--firmware-binary false \
	--firmware-chroot false \
	--memtest none \
	--checksums sha256 \
	--iso-application "BetterNAS" \
	--iso-publisher "BetterNAS" \
	--iso-volume "BETTERNAS" \
	--image-name "betternas" \
	--bootappend-live "$APPEND"

sed -e 's/#.*//' -e '/^\s*$/d' "$SRC/image/packages/live.list" > config/package-lists/betternas.list.chroot

cp -a "$SRC/image/live/includes.chroot/." config/includes.chroot/
install -D -m 0755 "$INSTALLER" config/includes.chroot/usr/sbin/betternas-installer
mkdir -p config/includes.chroot/etc/systemd/system/multi-user.target.wants
ln -sf /etc/systemd/system/betternas-installer.service \
	config/includes.chroot/etc/systemd/system/multi-user.target.wants/betternas-installer.service

mkdir -p config/includes.binary/betternas
cp "$SQUASH" config/includes.binary/betternas/rootfs.squashfs
echo "$VERSION" > config/includes.binary/betternas/VERSION

mkdir -p config/bootloaders
cp -a /usr/share/live/build/bootloaders/grub-pc /usr/share/live/build/bootloaders/isolinux \
	/usr/share/live/build/bootloaders/syslinux_common config/bootloaders/
cp "$SRC/image/live/bootloaders/grub.cfg" config/bootloaders/grub-pc/grub.cfg
cp "$SRC/image/live/bootloaders/live.cfg.in" "$SRC/image/live/bootloaders/menu.cfg" config/bootloaders/syslinux_common/
cp "$SRC/image/live/bootloaders/isolinux.cfg" config/bootloaders/isolinux/isolinux.cfg
# Serielle Konsole in GRUB (Headless-Geräte, automatisierte Tests)
cat >> config/bootloaders/grub-pc/config.cfg <<'EOF'

serial --unit=0 --speed=115200
terminal_input --append serial
terminal_output --append serial
EOF

lb build
mv "$BUILD"/betternas-*.iso "$OUT"
ls -lh "$OUT"
