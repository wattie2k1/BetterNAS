#!/bin/sh
# Erzeugt den Signierschlüssel für das BetterNAS-APT-Repository (einmalig).
#   - privater Schlüssel: $BNAS_APT_KEY (Standard ~/.betternas/apt-signing-key.asc, Rechte 600) – NICHT ins Repo, selbst sichern!
#   - öffentlicher Schlüssel: packaging/apt/betternas-archive-keyring.{asc,gpg} (kommt ins .deb)
# Aufruf: tools/apt-key.sh   (nutzt den Builder-Container, siehe make builder)
set -eu
SRC="$(cd "$(dirname "$0")/.." && pwd)"
KEY="${BNAS_APT_KEY:-$HOME/.betternas/apt-signing-key.asc}"
BUILDER="${BUILDER:-betternas-builder-amd64}"

if [ -e "$KEY" ]; then
	echo "FEHLER: $KEY existiert bereits. Ein vorhandener Schlüssel wird nicht überschrieben." >&2
	exit 1
fi
mkdir -p "$(dirname "$KEY")"
chmod 700 "$(dirname "$KEY")"
umask 077

# Der Container schreibt den öffentlichen Schlüssel nach /out und den privaten auf stdout.
docker run --rm --platform linux/amd64 -v "$SRC/packaging/apt:/out" "$BUILDER" sh -eu -c '
	export GNUPGHOME="$(mktemp -d)"
	chmod 700 "$GNUPGHOME"
	gpg --batch --quiet --pinentry-mode loopback --passphrase "" \
		--quick-generate-key "BetterNAS Archive Signing Key <archive@betternas.invalid>" ed25519 sign never >&2
	gpg --export --armor > /out/betternas-archive-keyring.asc
	gpg --export > /out/betternas-archive-keyring.gpg
	gpg --batch --pinentry-mode loopback --passphrase "" --export-secret-keys --armor
	rm -rf "$GNUPGHOME"
' > "$KEY"
chmod 600 "$KEY"
echo "Privater Schlüssel: $KEY  (bitte sichern; für Releases als Secret APT_SIGNING_KEY im Environment release hinterlegen, siehe README)"
echo "Öffentlicher Schlüssel: packaging/apt/betternas-archive-keyring.asc"
