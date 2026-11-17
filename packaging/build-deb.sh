#!/bin/sh
# Baut das Debian-Paket betternas. Aufruf: packaging/build-deb.sh <arch> <version>
set -eu
ARCH="${1:?Architektur fehlt}"
VERSION="${2:?Version fehlt}"
SRC="$(cd "$(dirname "$0")/.." && pwd)"
ROOT="$(mktemp -d)"
trap 'rm -rf "$ROOT"' EXIT

for f in "$SRC/bin/$ARCH/betternas-api" "$SRC/bin/$ARCH/betternas-agent" "$SRC/frontend/dist/index.html" "$SRC/packaging/apt/betternas-archive-keyring.gpg"; do
	[ -e "$f" ] || { echo "FEHLER: $f fehlt (make build web; Schlüssel: make apt-key)"; exit 1; }
done

install -D -m 0755 "$SRC/bin/$ARCH/betternas-api" "$ROOT/usr/bin/betternas-api"
install -D -m 0755 "$SRC/bin/$ARCH/betternas-agent" "$ROOT/usr/sbin/betternas-agent"
mkdir -p "$ROOT/usr/share/betternas/web"
cp -a "$SRC/frontend/dist/." "$ROOT/usr/share/betternas/web/"
install -D -m 0644 "$SRC/packaging/apt/betternas-archive-keyring.gpg" "$ROOT/usr/share/keyrings/betternas-archive-keyring.gpg"
install -D -m 0644 "$SRC/packaging/samba/smb.conf" "$ROOT/usr/share/betternas/smb.conf"
install -D -m 0644 "$SRC/packaging/docker/daemon.json" "$ROOT/usr/share/betternas/docker-daemon.json"
install -D -m 0644 "$SRC/packaging/systemd/betternas-api.service" "$ROOT/usr/lib/systemd/system/betternas-api.service"
install -D -m 0644 "$SRC/packaging/systemd/betternas-agent.service" "$ROOT/usr/lib/systemd/system/betternas-agent.service"
install -D -m 0644 "$SRC/packaging/systemd/betternas-snapraid-sync@.service" "$ROOT/usr/lib/systemd/system/betternas-snapraid-sync@.service"
install -D -m 0644 "$SRC/packaging/systemd/betternas-snapraid-scrub@.service" "$ROOT/usr/lib/systemd/system/betternas-snapraid-scrub@.service"

mkdir -p "$ROOT/DEBIAN"
install -m 0755 "$SRC/packaging/scripts/postinst.sh" "$ROOT/DEBIAN/postinst"
install -m 0755 "$SRC/packaging/scripts/prerm.sh" "$ROOT/DEBIAN/prerm"
install -m 0755 "$SRC/packaging/scripts/postrm.sh" "$ROOT/DEBIAN/postrm"
SIZE="$(du -sk "$ROOT" | cut -f1)"
cat > "$ROOT/DEBIAN/control" <<CTRL
Package: betternas
Version: $VERSION
Architecture: $ARCH
Maintainer: BetterNAS <betternas@localhost>
Installed-Size: $SIZE
Depends: adduser, systemd, udev, util-linux, libpam-modules-bin, passwd, samba, mergerfs, snapraid, docker.io, docker-compose, libvirt-daemon-system, libvirt-clients, qemu-system-x86, qemu-utils, virtinst, ovmf, swtpm, swtpm-tools, smartmontools, gdisk, parted, e2fsprogs, xfsprogs
Section: admin
Priority: optional
Description: BetterNAS – NAS-, Docker- und KVM-Verwaltung
 Weboberfläche (betternas-api) und privilegierter Systemdienst (betternas-agent).
CTRL

mkdir -p "$SRC/dist"
rm -f "$SRC"/dist/betternas_*_"$ARCH".deb
dpkg-deb --root-owner-group -Zxz --build "$ROOT" "$SRC/dist/betternas_${VERSION}_${ARCH}.deb"
