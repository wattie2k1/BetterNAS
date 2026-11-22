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
SMB_PORT="${SMB_PORT:-4445}"
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
# Zweite Platte für die Speicher-Szenarien; wird erst beim Boot des installierten Systems angeschlossen.
qemu-img create -q -f qcow2 "$WORK/data.qcow2" 8G
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
BOOTN=0
boot_vm() {
	BOOTN=$((BOOTN + 1))
	# shellcheck disable=SC2086
	# bootindex: Mit zweiter Platte würde SeaBIOS sonst evtl. von der leeren Datenplatte starten.
	$QEMU -serial "file:$WORK/boot$BOOTN.log" \
		-drive file="$WORK/disk.qcow2",if=none,id=sys0,format=qcow2 -device virtio-blk-pci,drive=sys0,bootindex=0 \
		-drive file="$WORK/data.qcow2",if=none,id=data0,format=qcow2 -device virtio-blk-pci,drive=data0,serial=bnas-e2e-data \
		-nic user,model=virtio-net-pci,hostfwd=tcp:127.0.0.1:$SSH_PORT-:22,hostfwd=tcp:127.0.0.1:$HTTPS_PORT-:443,hostfwd=tcp:127.0.0.1:$SMB_PORT-:445 &
	QPID=$!
	if ! wait_for "$WORK/boot$BOOTN.log" "BetterNAS ist bereit" "$BOOT_TIMEOUT" "$QPID"; then
		echo "[e2e] FEHLER: System nicht bereit"; tail -20 "$WORK/boot$BOOTN.log"; exit 1
	fi
	echo "[e2e] System gestartet: $(grep -a 'BetterNAS ist bereit' "$WORK/boot$BOOTN.log" | tail -1 | tr -d '\r')"
	for _ in $(seq 1 30); do $SSH true 2>/dev/null && break; sleep 10; done
}
SSH="ssh -i $WORK/id_ed25519 -p $SSH_PORT -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=10 -o LogLevel=ERROR ittest@127.0.0.1"
trap 'kill $QPID 2>/dev/null || true' EXIT
boot_vm

# --- 3. Prüfungen per SSH ----------------------------------------------------------------
fail=0
check() { if $SSH "$2" >/dev/null 2>&1; then echo "[e2e] OK   $1"; else echo "[e2e] FEHL $1"; fail=1; fi; }
check "machine-id erzeugt"          'test -s /etc/machine-id'
check "Hostname gesetzt"            'test "$(hostname)" = betternas-e2e'
check "/srv/appdata eingehängt"     'findmnt /srv/appdata'
check "Admin in betternas-admin"    'id -nG | grep -qw betternas-admin'
check "Samba-Benutzer angelegt"     "echo $PASSWORD | sudo -S pdbedit -L | grep -q '^ittest:'"
check "mergerfs installiert"        'command -v mergerfs'
check "snapraid installiert"        'command -v snapraid'
check "NFS-Server installiert"      'test -x /usr/sbin/exportfs'
check "Docker installiert"          'command -v docker && command -v virsh && command -v virt-clone'
check "systemd ohne Fehler"         'test "$(systemctl is-system-running)" = running'
check "BetterNAS-Dienste aktiv"     'systemctl is-active betternas-agent betternas-api'
code="$(curl -sk -o /dev/null -w '%{http_code}' "https://127.0.0.1:$HTTPS_PORT/" || true)"
if [ "$code" = 200 ]; then echo "[e2e] OK   Weboberfläche per HTTPS"; else echo "[e2e] FEHL Weboberfläche per HTTPS ($code)"; fail=1; fi
code="$(curl -sk -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' \
	-d "{\"username\":\"ittest\",\"password\":\"$PASSWORD\"}" "https://127.0.0.1:$HTTPS_PORT/api/v1/auth/login" || true)"
if [ "$code" = 200 ]; then echo "[e2e] OK   Anmeldung mit Installer-Admin"; else echo "[e2e] FEHL Anmeldung mit Installer-Admin ($code)"; fail=1; fi

# --- 4. Daten-Szenario über die API: Platte, Pool, Benutzer, Freigabe, SMB ---------------
API="https://127.0.0.1:$HTTPS_PORT"
JAR="$WORK/cookies.txt"
USERPW="e2e-user-passwort-1"
curl -sk -c "$JAR" -H 'Content-Type: application/json' -d "{\"username\":\"ittest\",\"password\":\"$PASSWORD\"}" "$API/api/v1/auth/login" > "$WORK/login.json"
CSRF="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["csrfToken"])' "$WORK/login.json")"
api() { # Methode Pfad [Body] → Antworttext; Statuscode landet in $WORK/status
	curl -sk -b "$JAR" -X "$1" -H "X-CSRF-Token: $CSRF" -H 'Content-Type: application/json' \
		-w '%{http_code}' -o "$WORK/body.json" ${3:+-d "$3"} "$API$2" > "$WORK/status"
	cat "$WORK/body.json"
}
jget() { python3 -c 'import json,sys; d=json.load(sys.stdin); print(eval(sys.argv[1]))' "$1"; }
wait_job() { # Antwort mit jobId
	id="$(printf '%s' "$1" | jget 'd["jobId"]')"
	for _ in $(seq 1 120); do
		st="$(curl -sk -b "$JAR" "$API/api/v1/jobs/$id" | jget 'd["status"]')"
		[ "$st" = succeeded ] && return 0
		[ "$st" = failed ] && { curl -sk -b "$JAR" "$API/api/v1/jobs/$id" | jget 'd["log"]'; return 1; }
		sleep 2
	done
	return 1
}
step() { # Name Befehl…
	name="$1"; shift
	if "$@"; then echo "[e2e] OK   $name"; else echo "[e2e] FEHL $name"; fail=1; fi
}

DISK="$(api GET /api/v1/disks | jget '[x["id"] for x in d if x["serial"]=="bnas-e2e-data"][0]')"
step "Datenplatte erkannt" test -n "$DISK"
PREVIEW="$(api POST "/api/v1/disks/$DISK/format/preview")"
TOKEN="$(printf '%s' "$PREVIEW" | jget 'd["confirmToken"]')"
JOB="$(api POST "/api/v1/disks/$DISK/format" "{\"fsType\":\"ext4\",\"confirmSerial\":\"bnas-e2e-data\",\"confirmToken\":\"$TOKEN\"}")"
step "Datenplatte formatieren" wait_job "$JOB"
JOB="$(api POST /api/v1/pools "{\"name\":\"e2e\",\"branches\":[\"$DISK\"],\"minFree\":\"1G\"}")"
step "Pool anlegen" wait_job "$JOB"
api POST /api/v1/users "{\"name\":\"e2euser\",\"fullName\":\"E2E\",\"password\":\"$USERPW\",\"groups\":[],\"admin\":false}" >/dev/null
step "Benutzer anlegen" test "$(cat "$WORK/status")" = 201
JOB="$(api POST /api/v1/shares '{"name":"E2E","pool":"e2e","subdir":"E2E","comment":"Test","browsable":true,"guest":false,"recycle":true,"fruit":true,"acl":[{"type":"user","name":"e2euser","access":"rw"}]}')"
step "Freigabe anlegen" wait_job "$JOB"
api POST /api/v1/setup/complete >/dev/null
step "Einrichtung abschließen" test "$(cat "$WORK/status")" = 204
step "Einrichtung gespeichert" test "$(api GET /api/v1/setup | jget 'd["completed"]')" = True

# Phase 4–6: Docker, VMs, Backup, 2FA-Status, Benachrichtigungen (ohne Netzwerkzugriff auf Registries)
# Docker bleibt gesperrt, bis die Speicherorte eingerichtet sind (docker_not_configured): zuerst Ordner anlegen und setzen.
check "Docker-Ordner anlegen" "echo $PASSWORD | sudo -S sh -c 'mkdir -p /srv/appdata/apps /srv/appdata/dockerdata && chmod 755 /srv/appdata/apps /srv/appdata/dockerdata'"
api PUT /api/v1/docker/paths '{"appdata":"/srv/appdata/apps","dockerdata":"/srv/appdata/dockerdata"}' >/dev/null
step "Docker-Speicherorte"   test "$(cat "$WORK/status")" = 200
for _ in $(seq 1 30); do [ "$(api GET /api/v1/docker/status | jget 'd["active"]')" = True ] && break; sleep 2; done
step "Docker läuft"          test "$(api GET /api/v1/docker/status | jget 'd["installed"] and d["active"]')" = True
api POST /api/v1/stacks '{"name":"e2estack","compose":"services:\n  web:\n    image: nginx:stable\n","env":"A=1\n"}' >/dev/null
step "Stack anlegen"         test "$(cat "$WORK/status")" = 201
step "Stack gültig"          test "$(api POST /api/v1/stacks/e2estack/validate | jget 'd["ok"]')" = True
step "Stack löschen"         sh -c "curl -sk -b '$JAR' -X DELETE -H 'X-CSRF-Token: $CSRF' -o /dev/null -w '%{http_code}' '$API/api/v1/stacks/e2estack' | grep -q 204"
step "libvirt installiert"   test "$(api GET /api/v1/vms/status | jget 'd["installed"]')" = True
step "VM-Netz default"       test "$(api GET /api/v1/vms/networks | jget '"default" in d')" = True
api POST /api/v1/backups >/dev/null
step "Konfig-Backup anlegen" test "$(cat "$WORK/status")" = 201
step "Backup in Liste"       test "$(api GET /api/v1/backups | jget 'len(d)')" -ge 1
step "2FA-Status"            test "$(api GET /api/v1/auth/totp | jget 'd["enabled"]')" = False
step "Benachrichtigungen"    test "$(api GET /api/v1/notifications/config >/dev/null; cat "$WORK/status")" = 200
step "Bridge-Status"         test "$(api GET /api/v1/network/bridge | jget 'len(d["candidates"])>=1 and not d["active"]')" = True

if command -v smbclient >/dev/null; then
	step "SMB: Datei schreiben" sh -c "smbclient //127.0.0.1/E2E -p $SMB_PORT -U 'e2euser%$USERPW' -c 'put /etc/hostname test.txt; ls' 2>&1 | grep -q test.txt"
else
	echo "[e2e] ÜBERSPRUNGEN SMB-Prüfungen (smbclient fehlt auf diesem Rechner)"
fi

# --- 5. Neustart: Pool und Freigabe müssen von selbst wieder da sein ---------------------
echo "[e2e] Neustart des installierten Systems …"
$SSH "echo $PASSWORD | sudo -S systemctl poweroff" >/dev/null 2>&1 || true
for _ in $(seq 1 60); do kill -0 "$QPID" 2>/dev/null || break; sleep 2; done
kill "$QPID" 2>/dev/null || true
wait "$QPID" 2>/dev/null || true
boot_vm
check "Pool nach Neustart eingehängt"  'findmnt -t fuse.mergerfs /srv/pools/e2e'
check "Samba-Freigabe nach Neustart"   'grep -q "\[E2E\]" /etc/samba/betternas.d/e2e.conf'
if command -v smbclient >/dev/null; then
	step "SMB: Datei nach Neustart lesbar" sh -c "smbclient //127.0.0.1/E2E -p $SMB_PORT -U 'e2euser%$USERPW' -c 'get test.txt -' 2>&1 | grep -q betternas"
fi

[ "$fail" = 0 ] && echo "[e2e] ALLE PRÜFUNGEN BESTANDEN ($MODE)"
exit "$fail"
