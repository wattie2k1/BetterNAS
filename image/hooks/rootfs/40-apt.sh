#!/bin/sh
# Setzt die APT-Quellen des Zielsystems.
set -eu
R="$1"
rm -f "$R/etc/apt/sources.list"
cat > "$R/etc/apt/sources.list.d/debian.sources" <<'EOF'
Types: deb
URIs: http://deb.debian.org/debian
Suites: trixie trixie-updates
Components: main non-free-firmware
Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg

Types: deb
URIs: http://security.debian.org/debian-security
Suites: trixie-security
Components: main non-free-firmware
Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg
EOF
chroot "$R" apt-get clean
rm -rf "$R"/var/lib/apt/lists/*
