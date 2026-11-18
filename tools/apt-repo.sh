#!/bin/sh
# Erzeugt oder aktualisiert ein signiertes APT-Repository (Komponente "main", Suiten stable und testing).
# Aufruf: tools/apt-repo.sh [--native] [--suite <stable|testing>]... <repo-verzeichnis> <deb>...
#   Ohne --suite werden beide Suiten gepflegt. Pro Suite gibt es einen eigenen Pool (pool/<suite>/main/b/betternas/),
#   darin bleiben die BNAS_KEEP (Standard 5) höchsten Versionen; Pakete anderer Suiten bleiben unberührt.
#   Ohne --native läuft das Skript im Builder-Container (Schlüssel über stdin, Pfade innerhalb des Projekts);
#   mit --native direkt auf dem Host (apt-ftparchive, gpg, dpkg-deb nötig).
# Der private Schlüssel kommt aus $BNAS_APT_KEY (Standard ~/.betternas/apt-signing-key.asc). Enthält die Datei mehrere
# Schlüssel, signieren alle (Überlappung bei der Schlüssel-Rotation).
set -eu
SRC="$(cd "$(dirname "$0")/.." && pwd)"
KEY="${BNAS_APT_KEY:-$HOME/.betternas/apt-signing-key.asc}"
KEEP="${BNAS_KEEP:-5}"

MODE=docker
SUITES=""
while [ "$#" -gt 0 ]; do
	case "$1" in
	--native) MODE=native; shift ;;
	--inner) MODE=inner; shift ;;
	--suite)
		case "${2:-}" in
		stable | testing) SUITES="$SUITES $2" ;;
		*) echo "FEHLER: Unbekannte Suite '${2:-}' (erlaubt: stable, testing)" >&2; exit 1 ;;
		esac
		shift 2
		;;
	*) break ;;
	esac
done
[ -n "$SUITES" ] || SUITES="stable testing"
REPO="${1:?Aufruf: tools/apt-repo.sh [--native] [--suite <suite>]... <repo-verzeichnis> <deb>...}"
shift
[ "$#" -gt 0 ] || { echo "FEHLER: keine .deb-Dateien angegeben" >&2; exit 1; }

if [ "$MODE" = docker ]; then
	[ -r "$KEY" ] || { echo "FEHLER: Signierschlüssel $KEY fehlt oder ist leer (make apt-key)" >&2; exit 1; }
	BUILDER="${BUILDER:-betternas-builder-amd64}"
	# Pfade relativ zum Projekt, damit sie im Container unter /src gelten.
	rel() { case "$1" in "$SRC"/*) echo "${1#"$SRC"/}" ;; /*) echo "FEHLER: $1 liegt außerhalb des Projekts" >&2; exit 1 ;; *) echo "$1" ;; esac; }
	args=""
	for s in $SUITES; do args="$args --suite $s"; done
	for p in "$REPO" "$@"; do args="$args $(rel "$p")"; done
	# shellcheck disable=SC2086
	docker run --rm -i --platform linux/amd64 -e BNAS_KEEP="$KEEP" -v "$SRC:/src" -w /src "$BUILDER" sh tools/apt-repo.sh --inner $args < "$KEY"
	exit 0
fi

# --- ab hier: nativer Kern (Host oder Container) ---
if [ "$MODE" = inner ]; then
	KEYDATA="$(cat)"
else
	KEYDATA="$(cat "$KEY" 2>/dev/null || true)"
fi
[ -n "$KEYDATA" ] || { echo "FEHLER: Signierschlüssel fehlt oder ist leer" >&2; exit 1; }

GNUPGHOME="$(mktemp -d)"
export GNUPGHOME
WORK="$(mktemp -d)"
trap 'rm -rf "$GNUPGHOME" "$WORK"' EXIT
chmod 700 "$GNUPGHOME"
printf '%s\n' "$KEYDATA" | gpg --batch --quiet --import 2>/dev/null
# Fingerabdrücke der geheimen Hauptschlüssel (die fpr-Zeile direkt nach "sec")
FPRS="$(gpg --list-secret-keys --with-colons | awk -F: '$1=="sec"{s=1;next} $1=="fpr"&&s{print $10;s=0}')"
[ -n "$FPRS" ] || { echo "FEHLER: Signierschlüssel fehlt oder ist leer (kein geheimer Schlüssel importierbar)" >&2; exit 1; }
USERS=""
for f in $FPRS; do USERS="$USERS --local-user $f"; done

# Pakete mit absolutem Pfad merken, bevor wir ins Repo wechseln.
DEBS=""
for d in "$@"; do
	[ -f "$d" ] || { echo "FEHLER: $d existiert nicht" >&2; exit 1; }
	DEBS="$DEBS $(cd "$(dirname "$d")" && pwd)/$(basename "$d")"
done

mkdir -p "$REPO"
cd "$REPO"
for suite in $SUITES; do
	POOL="pool/$suite/main/b/betternas"
	mkdir -p "$POOL"
	for deb in $DEBS; do cp "$deb" "$POOL/"; done

	# Nur die KEEP höchsten Versionen behalten (sort -V ordnet "~" wie Debian unter die Release-Version).
	for f in "$POOL"/*.deb; do
		printf '%s\t%s\n' "$(dpkg-deb -f "$f" Version)" "$f"
	done | sort -t "$(printf '\t')" -k1,1 -rV | awk -F '\t' -v keep="$KEEP" 'NR>keep{print $2}' | while IFS= read -r old; do
		rm -f -- "$old"
	done

	for arch in amd64 arm64; do
		d="dists/$suite/main/binary-$arch"
		mkdir -p "$d"
		apt-ftparchive --arch "$arch" packages "pool/$suite" > "$d/Packages"
		gzip -kf9 "$d/Packages"
	done
	rm -f "dists/$suite/Release" "dists/$suite/InRelease" "dists/$suite/Release.gpg"
	apt-ftparchive \
		-o APT::FTPArchive::Release::Origin=BetterNAS \
		-o APT::FTPArchive::Release::Label=BetterNAS \
		-o "APT::FTPArchive::Release::Suite=$suite" \
		-o "APT::FTPArchive::Release::Codename=$suite" \
		-o APT::FTPArchive::Release::Components=main \
		-o "APT::FTPArchive::Release::Architectures=amd64 arm64" \
		release "dists/$suite" > "$WORK/Release"
	mv "$WORK/Release" "dists/$suite/Release"
	# shellcheck disable=SC2086
	gpg --batch --yes --pinentry-mode loopback --passphrase "" $USERS --clearsign -o "dists/$suite/InRelease" "dists/$suite/Release"
	# shellcheck disable=SC2086
	gpg --batch --yes --pinentry-mode loopback --passphrase "" $USERS -abs -o "dists/$suite/Release.gpg" "dists/$suite/Release"
done
