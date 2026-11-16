#!/bin/sh
# Baut Paket, Root-Dateisystem und ISO nativ auf einem Debian-/BetterNAS-Rechner (amd64)
# und führt optional die QEMU-Tests mit KVM aus. Go-Binaries und Weboberfläche werden lokal gebaut.
#
# Aufruf: tools/remote-build.sh <ssh-host> <version> [testmodi…]
#   z. B. tools/remote-build.sh betternas-dev 0.0.3-dev uefi bios secureboot
# Voraussetzung auf dem Zielrechner: sudo ohne Passwort und die Pakete aus image/Dockerfile.builder
# sowie qemu-system-x86, qemu-utils, ovmf, rsync.
set -eu

HOST="${1:?SSH-Host fehlt}"
VERSION="${2:?Version fehlt}"
shift 2
MODES="$*"
ARCH=amd64
SRC="$(cd "$(dirname "$0")/.." && pwd)"
REMOTE_DIR=/srv/appdata/betternas-build

cd "$SRC"
echo "[remote] Baue Go-Binaries und Weboberfläche lokal …"
make build VERSION="$VERSION" >/dev/null
[ -f frontend/dist/index.html ] || make web >/dev/null

echo "[remote] Übertrage Quellen nach $HOST:$REMOTE_DIR …"
ssh "$HOST" "sudo mkdir -p $REMOTE_DIR && sudo chown \$(id -u):\$(id -g) $REMOTE_DIR"
rsync -a --delete \
	--exclude /.git --exclude /.superpowers --exclude /frontend/node_modules \
	--exclude /build --exclude /dist --exclude /bin/arm64 \
	./ "$HOST:$REMOTE_DIR/"

# shellcheck disable=SC2087 # Variablen werden bewusst lokal eingesetzt
ssh "$HOST" sh -s <<EOF
set -eu
cd $REMOTE_DIR
mkdir -p dist build
rm -f dist/betternas_*_$ARCH.deb
echo "[remote] Paket …";       sudo packaging/build-deb.sh $ARCH $VERSION
echo "[remote] Root-Dateisystem …"; sudo APT_URL='${APT_URL:-}' image/build-rootfs.sh $ARCH > build/rootfs.log 2>&1 || { tail -30 build/rootfs.log; exit 1; }
sudo APT_URL='${APT_URL:-}' image/tests/check-rootfs.sh dist/rootfs-$ARCH.tar
echo "[remote] ISO …";         sudo image/build-iso.sh $ARCH $VERSION > build/iso.log 2>&1 || { tail -30 build/iso.log; exit 1; }
sudo chown -R \$(id -u):\$(id -g) dist build
for m in $MODES; do
	echo "[remote] Test \$m …"
	sudo test/image/e2e.sh dist/betternas-$VERSION-$ARCH.iso \$m > build/e2e-\$m.log 2>&1 || true
	grep -E "\[e2e\] (OK|FEHL|FEHLER|ALLE)" build/e2e-\$m.log || tail -20 build/e2e-\$m.log
done
EOF

echo "[remote] Hole Ergebnisse …"
mkdir -p dist
rsync -a "$HOST:$REMOTE_DIR/dist/betternas-$VERSION-$ARCH.iso" "$HOST:$REMOTE_DIR/dist/betternas_${VERSION}_$ARCH.deb" dist/
ls -lh "dist/betternas-$VERSION-$ARCH.iso"
