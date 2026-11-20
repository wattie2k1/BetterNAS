#!/bin/sh
# Test für den Docker-Block (docker-gate) in packaging/scripts/postinst.sh:
# Docker bleibt auf frischen Installationen deaktiviert, auf bestehenden aktiv.
set -u
DIR="$(cd "$(dirname "$0")/../.." && pwd)"
POSTINST="$DIR/packaging/scripts/postinst.sh"
T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT
fail=0
err() { echo "FEHLER: $*" >&2; fail=1; }

sed -n '/# --- docker-gate:begin/,/# --- docker-gate:end/p' "$POSTINST" > "$T/gate.sh"
[ -s "$T/gate.sh" ] || { echo "FEHLER: docker-gate-Block fehlt in postinst.sh" >&2; exit 1; }

mkdir "$T/bin"
cat > "$T/bin/systemctl" <<'EOF2'
#!/bin/sh
echo "systemctl $*" >> "$CALLS"
EOF2
chmod +x "$T/bin/systemctl"

# run <name> <setup-befehle>: frisches Wurzelverzeichnis, setup ausführen, Block laufen lassen; Aufrufe in $T/calls.
run() {
	r="$T/root-$1"; rm -rf "$r"; mkdir -p "$r"
	( cd "$r" && eval "$2" )
	: > "$T/calls"
	BETTERNAS_ROOT="$r" CALLS="$T/calls" PATH="$T/bin:$PATH" sh "$T/gate.sh" || err "$1: Block fehlgeschlagen"
}

run fresh-chroot ':'
[ "$(cat "$T/calls")" = "systemctl disable docker.socket docker.service" ] || err "frisch (chroot): Docker nicht deaktiviert: $(cat "$T/calls")"

run fresh-systemd 'mkdir -p run/systemd/system'
[ "$(cat "$T/calls")" = "systemctl disable --now docker.socket docker.service" ] || err "frisch (systemd): $(cat "$T/calls")"

run paths 'mkdir -p etc/betternas && echo {} > etc/betternas/paths.json'
[ ! -s "$T/calls" ] || err "mit paths.json darf Docker nicht deaktiviert werden"

run daemon 'mkdir -p etc/docker && echo {} > etc/docker/daemon.json'
[ ! -s "$T/calls" ] || err "mit daemon.json darf Docker nicht deaktiviert werden"

run stacks 'mkdir -p srv/appdata/stacks/emby && echo x > srv/appdata/stacks/emby/compose.yaml'
[ ! -s "$T/calls" ] || err "mit Stacks darf Docker nicht deaktiviert werden"

run dataroot 'mkdir -p srv/appdata/docker'
[ ! -s "$T/calls" ] || err "mit data-root unter /srv/appdata darf Docker nicht deaktiviert werden"

[ "$fail" = 0 ] && echo "docker-gate OK"
exit "$fail"
