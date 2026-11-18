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
# Eigene Pakete: Quelle nur, wenn eine Adresse angegeben wurde (APT_URL). Der Schlüssel liegt im Paket betternas.
# APT_SUITE wählt den Update-Kanal (stable = Standard, testing = Vorabversionen); die GUI kann ihn später umstellen.
rm -f "$R/etc/apt/sources.list.d/betternas.sources"
if [ -n "${APT_URL:-}" ]; then
	SUITE="${APT_SUITE:-stable}"
	case "$SUITE" in
	stable | testing) ;;
	*) echo "FEHLER: APT_SUITE muss stable oder testing sein (ist: $SUITE)" >&2; exit 1 ;;
	esac
	cat > "$R/etc/apt/sources.list.d/betternas.sources" <<EOF
# Verwaltet von BetterNAS – nicht manuell bearbeiten
Types: deb
URIs: $APT_URL
Suites: $SUITE
Components: main
Signed-By: /usr/share/keyrings/betternas-archive-keyring.gpg
EOF
fi
chroot "$R" apt-get clean
rm -rf "$R"/var/lib/apt/lists/*
