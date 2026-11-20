#!/bin/sh
# Installiert den Linux-Kernel aus trixie-backports (neuerer Kernel statt des Debian-LTS-Kernels).
# Quelle und Pin stammen aus packaging/apt (dieselben Dateien liefert das Paket betternas aus); sie bleiben im
# Zielsystem, damit neue Kernel-Versionen später wie normale Updates (apt dist-upgrade) kommen.
set -eu
R="$1"
APT="$(cd "$(dirname "$0")/../../../packaging/apt" && pwd)"
install -D -m 0644 "$APT/betternas-backports.sources" "$R/etc/apt/sources.list.d/betternas-backports.sources"
install -D -m 0644 "$APT/betternas-kernel.pref" "$R/etc/apt/preferences.d/betternas-kernel.pref"
# Die Debian-Quellen schreibt erst 40-apt.sh; hier genügt eine vorläufige, damit apt die Pakete findet.
if [ ! -e "$R/etc/apt/sources.list.d/debian.sources" ]; then
	cat > "$R/etc/apt/sources.list.d/debian.sources" <<'EOF2'
Types: deb
URIs: http://deb.debian.org/debian
Suites: trixie trixie-updates
Components: main non-free-firmware
Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg
EOF2
fi
rm -f "$R/etc/apt/sources.list"
ARCH="$(chroot "$R" dpkg --print-architecture)"
chroot "$R" env DEBIAN_FRONTEND=noninteractive apt-get update
chroot "$R" env DEBIAN_FRONTEND=noninteractive apt-get -y --no-install-recommends \
	-t trixie-backports install "linux-image-$ARCH"
chroot "$R" apt-get clean
