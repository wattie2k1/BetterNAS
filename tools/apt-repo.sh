#!/bin/sh
# Erzeugt oder aktualisiert ein signiertes APT-Repository (Suite "stable", Komponente "main").
# Aufruf: tools/apt-repo.sh <repo-verzeichnis> <deb>...      (Pfade innerhalb des Projektverzeichnisses)
# Bestehende Pakete im Repo bleiben erhalten. Der private Schlüssel kommt aus $BNAS_APT_KEY
# (Standard ~/.betternas/apt-signing-key.asc) und wird nur über stdin an den Container gereicht.
set -eu
SRC="$(cd "$(dirname "$0")/.." && pwd)"

if [ "${1:-}" = "--inner" ]; then
	shift
	REPO="${1:?Repo-Verzeichnis fehlt}"
	shift
	export GNUPGHOME="$(mktemp -d)"
	trap 'rm -rf "$GNUPGHOME"' EXIT
	chmod 700 "$GNUPGHOME"
	gpg --batch --quiet --import
	KEYID="$(gpg --list-secret-keys --with-colons | awk -F: '/^fpr/ {print $10; exit}')"
	[ -n "$KEYID" ] || { echo "FEHLER: kein Signierschlüssel importiert" >&2; exit 1; }

	POOL="$REPO/pool/main/b/betternas"
	mkdir -p "$POOL"
	for deb in "$@"; do cp "$deb" "$POOL/"; done
	cd "$REPO"
	for arch in amd64 arm64; do
		d="dists/stable/main/binary-$arch"
		mkdir -p "$d"
		apt-ftparchive --arch "$arch" packages pool > "$d/Packages"
		gzip -kf9 "$d/Packages"
	done
	rm -f dists/stable/Release dists/stable/InRelease dists/stable/Release.gpg
	apt-ftparchive \
		-o APT::FTPArchive::Release::Origin=BetterNAS \
		-o APT::FTPArchive::Release::Label=BetterNAS \
		-o APT::FTPArchive::Release::Suite=stable \
		-o APT::FTPArchive::Release::Codename=stable \
		-o APT::FTPArchive::Release::Components=main \
		-o "APT::FTPArchive::Release::Architectures=amd64 arm64" \
		release dists/stable > /tmp/Release
	mv /tmp/Release dists/stable/Release
	gpg --batch --yes --pinentry-mode loopback --passphrase "" --default-key "$KEYID" --clearsign -o dists/stable/InRelease dists/stable/Release
	gpg --batch --yes --pinentry-mode loopback --passphrase "" --default-key "$KEYID" -abs -o dists/stable/Release.gpg dists/stable/Release
	exit 0
fi

REPO="${1:?Aufruf: tools/apt-repo.sh <repo-verzeichnis> <deb>...}"
shift
[ "$#" -gt 0 ] || { echo "FEHLER: keine .deb-Dateien angegeben" >&2; exit 1; }
KEY="${BNAS_APT_KEY:-$HOME/.betternas/apt-signing-key.asc}"
[ -r "$KEY" ] || { echo "FEHLER: Signierschlüssel $KEY fehlt (make apt-key)" >&2; exit 1; }
BUILDER="${BUILDER:-betternas-builder-amd64}"

# Pfade relativ zum Projekt, damit sie im Container unter /src gelten.
rel() { case "$1" in "$SRC"/*) echo "${1#"$SRC"/}" ;; /*) echo "FEHLER: $1 liegt außerhalb des Projekts" >&2; exit 1 ;; *) echo "$1" ;; esac; }
args=""
for p in "$REPO" "$@"; do args="$args $(rel "$p")"; done
# shellcheck disable=SC2086
docker run --rm -i --platform linux/amd64 -v "$SRC:/src" -w /src "$BUILDER" sh tools/apt-repo.sh --inner $args < "$KEY"
