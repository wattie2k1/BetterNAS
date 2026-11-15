#!/bin/sh
# Ende-zu-Ende-Test: unbeaufsichtigte Installation des ISOs in QEMU und Boot des installierten Systems.
# Aufruf: test/image/e2e.sh <iso> <bios|uefi|secureboot>
# Ohne KVM (z. B. macOS) läuft QEMU emuliert und braucht deutlich länger.
set -eu

ISO="${1:?ISO fehlt}"
MODE="${2:-uefi}"
SRC="$(cd "$(dirname "$0")/../.." && pwd)"
WORK="$SRC/build/e2e-$MODE"
INSTALL_TIMEOUT="${INSTALL_TIMEOUT:-5400}"
BOOT_TIMEOUT="${BOOT_TIMEOUT:-1800}"
SSH_PORT="${SSH_PORT:-2222}"
HTTPS_PORT="${HTTPS_PORT:-8443}"
PASSWORD="ittest-passwort"

rm -rf "$WORK"
mkdir -p "$WORK"

# --- Beschleunigung und Firmware ---------------------------------------------------------
ACCEL="-accel tcg,thread=multi"
CPU="-cpu max"
if [ -w /dev/kvm ]; then ACCEL="-accel kvm"; CPU="-cpu host"; fi

find_fw() {
	for f in "$@"; do [ -f "$f" ] && { echo "$f"; return 0; }; done
	return 1
}
MACHINE="-machine q35"
FW=""
case "$MODE" in
bios) ;;
uefi)
	CODE="$(find_fw /usr/share/OVMF/OVMF_CODE_4M.fd /opt/homebrew/share/qemu/edk2-x86_64-code.fd /usr/local/share/qemu/edk2-x86_64-code.fd)"
	VARS="$(find_fw /usr/share/OVMF/OVMF_VARS_4M.fd /opt/homebrew/share/qemu/edk2-i386-vars.fd /usr/local/share/qemu/edk2-i386-vars.fd)"
	cp "$VARS" "$WORK/vars.fd"
	FW="-drive if=pflash,format=raw,readonly=on,file=$CODE -drive if=pflash,format=raw,file=$WORK/vars.fd"
	;;
secureboot)
	CODE="$(find_fw /usr/share/OVMF/OVMF_CODE_4M.secboot.fd)" || { echo "OVMF mit Secure Boot nicht gefunden (Paket ovmf)"; exit 2; }
	VARS="$(find_fw /usr/share/OVMF/OVMF_VARS_4M.ms.fd)"
	cp "$VARS" "$WORK/vars.fd"
	MACHINE="-machine q35,smm=on -global driver=cfi.pflash01,property=secure,value=on"
	FW="-drive if=pflash,format=raw,unit=0,readonly=on,file=$CODE -drive if=pflash,format=raw,unit=1,file=$WORK/vars.fd"
	;;
*) echo "Unbekannter Modus: $MODE"; exit 2 ;;
esac

# --- Datenträger -------------------------------------------------------------------------
qemu-img create -q -f qcow2 "$WORK/disk.qcow2" 48G
ssh-keygen -q -t ed25519 -N "" -C "e2e" -f "$WORK/id_ed25519"
cat > "$WORK/answers.json" <<EOF
{
  "targetDevice": "/dev/vda",
  "confirmWipe": "/dev/vda",
  "hostname": "betternas-e2e",
  "adminUser": "ittest",
  "adminPassword": "$PASSWORD",
  "adminSSHKey": "$(cat "$WORK/id_ed25519.pub")"
}
EOF
make_cfg() {
	rm -f "$WORK/cfg.img"
	if command -v mkfs.vfat >/dev/null && command -v mcopy >/dev/null; then
		truncate -s 8M "$WORK/cfg.img"
		mkfs.vfat -n BNASCFG "$WORK/cfg.img" >/dev/null
		mcopy -i "$WORK/cfg.img" "$WORK/answers.json" ::answers.json
	else
		docker run --rm --platform linux/amd64 -v "$WORK":/w betternas-builder-amd64 sh -c \
			'truncate -s 8M /w/cfg.img && mkfs.vfat -n BNASCFG /w/cfg.img >/dev/null && mcopy -i /w/cfg.img /w/answers.json ::answers.json' 2>/dev/null
	fi
}
make_cfg

QEMU="qemu-system-x86_64 $ACCEL $CPU $MACHINE -m 3072 -smp 4 -display none -no-reboot $FW"

wait_for() { # Datei Muster Timeout PID
	end=$(( $(date +%s) + $3 ))
	while [ "$(date +%s)" -lt "$end" ]; do
		grep -q "$2" "$1" 2>/dev/null && return 0
		kill -0 "$4" 2>/dev/null || { grep -q "$2" "$1" 2>/dev/null && return 0; return 1; }
		sleep 5
	done
	return 1
}

# --- 1. Installation ---------------------------------------------------------------------
echo "[e2e] Installation ($MODE) startet …"
# shellcheck disable=SC2086
$QEMU -serial "file:$WORK/install.log" \
	-drive file="$WORK/disk.qcow2",if=virtio \
	-drive file="$WORK/cfg.img",if=virtio,format=raw \
	-drive file="$ISO",media=cdrom,readonly=on \
	-boot d -nic none &
QPID=$!
if ! wait_for "$WORK/install.log" "Installation abgeschlossen" "$INSTALL_TIMEOUT" "$QPID"; then
	kill "$QPID" 2>/dev/null || true
	echo "[e2e] FEHLER: Installation nicht abgeschlossen"; grep "\[BetterNAS\]" "$WORK/install.log" | tail -5; exit 1
fi
wait "$QPID" || true
echo "[e2e] Installation abgeschlossen"

# --- 2. Boot des installierten Systems ---------------------------------------------------
# shellcheck disable=SC2086
$QEMU -serial "file:$WORK/boot.log" \
	-drive file="$WORK/disk.qcow2",if=virtio \
	-nic user,model=virtio-net-pci,hostfwd=tcp:127.0.0.1:$SSH_PORT-:22,hostfwd=tcp:127.0.0.1:$HTTPS_PORT-:443 &
QPID=$!
trap 'kill $QPID 2>/dev/null || true' EXIT
if ! wait_for "$WORK/boot.log" "BetterNAS ist bereit" "$BOOT_TIMEOUT" "$QPID"; then
	echo "[e2e] FEHLER: System nicht bereit"; tail -20 "$WORK/boot.log"; exit 1
fi
echo "[e2e] System gestartet: $(grep -a 'BetterNAS ist bereit' "$WORK/boot.log" | tail -1 | tr -d '\r')"

# --- 3. Prüfungen per SSH ----------------------------------------------------------------
SSH="ssh -i $WORK/id_ed25519 -p $SSH_PORT -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=10 -o LogLevel=ERROR ittest@127.0.0.1"
for _ in $(seq 1 30); do $SSH true 2>/dev/null && break; sleep 10; done
fail=0
check() { if $SSH "$2" >/dev/null 2>&1; then echo "[e2e] OK   $1"; else echo "[e2e] FEHL $1"; fail=1; fi; }
check "machine-id erzeugt"          'test -s /etc/machine-id'
check "Hostname gesetzt"            'test "$(hostname)" = betternas-e2e'
check "/srv/appdata eingehängt"     'findmnt /srv/appdata'
check "Admin in betternas-admin"    'id -nG | grep -qw betternas-admin'
check "Samba-Benutzer angelegt"     "echo $PASSWORD | sudo -S pdbedit -L | grep -q '^ittest:'"
check "mergerfs installiert"        'command -v mergerfs'
check "systemd ohne Fehler"         'test "$(systemctl is-system-running)" = running'
[ "$fail" = 0 ] && echo "[e2e] ALLE PRÜFUNGEN BESTANDEN ($MODE)"
exit "$fail"
