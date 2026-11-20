#!/bin/sh
# Veröffentlicht ein Entwicklungs-Repository auf dem Entwicklungsrechner und trägt es als APT-Quelle ein.
# Aufruf: tools/dev-repo.sh <ssh-host> <version>      (nach make build web deb VERSION=<version>)
# Das Repo wird auf http://127.0.0.1:8088 des Zielrechners ausgeliefert (nur lokal, zum Testen von Updates).
set -eu
HOST="${1:?SSH-Host fehlt}"
VERSION="${2:?Version fehlt}"
SRC="$(cd "$(dirname "$0")/.." && pwd)"
DEB="$SRC/dist/betternas_${VERSION}_amd64.deb"
[ -f "$DEB" ] || { echo "FEHLER: $DEB fehlt (make build web deb VERSION=$VERSION)" >&2; exit 1; }
ssh "$HOST" 'command -v python3 >/dev/null' || { echo "FEHLER: python3 fehlt auf $HOST" >&2; exit 1; }

# Das Repo wird im Projekt (dist/apt) gepflegt, damit frühere Versionen erhalten bleiben.
"$SRC/tools/apt-repo.sh" "$SRC/dist/apt" "$DEB"

ssh "$HOST" 'sudo mkdir -p /srv/dockerdata/betternas-repo && sudo chown "$(id -u):$(id -g)" /srv/dockerdata/betternas-repo'
rsync -a --delete "$SRC/dist/apt/" "$HOST:/srv/dockerdata/betternas-repo/"
# Systeme mit einer älteren Version haben den Schlüsselbund noch nicht: aus dem Projekt nachtragen.
scp -q "$SRC/packaging/apt/betternas-archive-keyring.gpg" "$HOST:/tmp/betternas-archive-keyring.gpg"
ssh "$HOST" 'sudo install -m 0644 /tmp/betternas-archive-keyring.gpg /usr/share/keyrings/betternas-archive-keyring.gpg && rm -f /tmp/betternas-archive-keyring.gpg'
ssh "$HOST" 'sh -s' <<'REMOTE'
set -eu
if ! systemctl is-active --quiet bnas-dev-repo; then
	sudo systemd-run --unit=bnas-dev-repo --property=Restart=always \
		python3 -m http.server 8088 --bind 127.0.0.1 --directory /srv/dockerdata/betternas-repo >/dev/null
fi
sudo tee /etc/apt/sources.list.d/betternas.sources >/dev/null <<'EOF2'
Types: deb
URIs: http://127.0.0.1:8088
Suites: stable
Components: main
Signed-By: /usr/share/keyrings/betternas-archive-keyring.gpg
EOF2
sleep 1
sudo apt-get update 2>&1 | tail -4
REMOTE
echo "Fertig. Prüfen: ssh $HOST apt-cache policy betternas"
