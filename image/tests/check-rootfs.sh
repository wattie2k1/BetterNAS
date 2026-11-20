#!/bin/sh
# Prüft ein gebautes rootfs.tar auf die Anforderungen des Zielsystems.
set -u
TAR="${1:?rootfs.tar fehlt}"
[ -f "$TAR" ] || { echo "FEHLER: $TAR existiert nicht"; exit 1; }
W="$(mktemp -d)"
trap 'rm -rf "$W"' EXIT
tar -C "$W" -xpf "$TAR" || exit 1
fail=0
err() { echo "FEHLER: $*"; fail=1; }

[ -s "$W/etc/machine-id" ] && err "etc/machine-id ist nicht leer"
[ -e "$W/etc/machine-id" ] || err "etc/machine-id fehlt"
ls "$W"/etc/ssh/ssh_host_* >/dev/null 2>&1 && err "SSH-Host-Keys im Image"
for f in usr/bin/mergerfs usr/bin/snapraid usr/sbin/smbd usr/lib/shim/shimx64.efi.signed usr/lib/grub/i386-pc/modinfo.sh \
         usr/sbin/grub-install usr/sbin/sgdisk usr/sbin/mkfs.btrfs etc/issue.d/betternas.issue; do
	[ -e "$W/$f" ] || err "$f fehlt"
done
for f in usr/bin/virsh usr/bin/qemu-img usr/bin/qemu-system-x86_64; do
	[ -e "$W/$f" ] || err "$f fehlt"
done
[ -e "$W/usr/bin/docker" ] || err "docker-cli fehlt"
[ -L "$W/etc/libvirt/qemu/networks/autostart/default.xml" ] || err "libvirt-Netz default startet nicht automatisch"
ls "$W"/usr/share/OVMF/OVMF_CODE*.fd >/dev/null 2>&1 || err "OVMF (UEFI-Firmware für VMs) fehlt"
[ -L "$W/etc/systemd/system/multi-user.target.wants/systemd-networkd.service" ] || err "systemd-networkd nicht aktiviert"
grep -q '^root:[!*]' "$W/etc/shadow" || err "root ist nicht gesperrt"
grep -q '^betternas-admin:' "$W/etc/group" || err "Gruppe betternas-admin fehlt"
ls "$W"/boot/vmlinuz-* >/dev/null 2>&1 || err "Kernel fehlt"
ls "$W"/boot/initrd.img-* >/dev/null 2>&1 || err "initramfs fehlt"
# Der Kernel stammt aus trixie-backports; die Quelle bleibt für Kernel-Updates im System (Kopfzeile = von BetterNAS verwaltet).
BPO="$W/etc/apt/sources.list.d/betternas-backports.sources"
grep -qxF 'Suites: trixie-backports' "$BPO" 2>/dev/null || err "Backports-Quelle fehlt"
[ -s "$W/etc/apt/preferences.d/betternas-kernel.pref" ] || err "Kernel-Pin (preferences.d) fehlt"
awk '/^Package: linux-image-(amd64|arm64)$/{f=1} f&&/^Version:/{print; exit}' "$W/var/lib/dpkg/status" | grep -q '~bpo' || err "Kernel stammt nicht aus den Backports"
grep -q 'de_DE.UTF-8' "$W/etc/locale.gen" || err "Locale de_DE fehlt"
# Besitzer müssen erhalten bleiben (Overlay daher mit --no-preserve=ownership kopieren) (z. B. /var/mail → Gruppe mail, /etc/shadow → Gruppe shadow)
[ "$(stat -c %g "$W/var/mail")" = "$(awk -F: '$1=="mail"{print $3}' "$W/etc/group")" ] || err "Besitzer im Image verloren (/var/mail)"
[ "$(stat -c %g "$W/etc/shadow")" = "$(awk -F: '$1=="shadow"{print $3}' "$W/etc/group")" ] || err "Besitzer im Image verloren (/etc/shadow)"

if ls "$W"/var/lib/dpkg/info/betternas.list >/dev/null 2>&1; then
	[ -x "$W/usr/bin/betternas-api" ] || err "betternas-api fehlt"
	[ -L "$W/etc/systemd/system/multi-user.target.wants/betternas-api.service" ] || err "betternas-api nicht aktiviert"
	grep -q '^betternas:' "$W/etc/passwd" || err "Systembenutzer betternas fehlt"
	head -n1 "$W/etc/samba/smb.conf" | grep -q 'Verwaltet von BetterNAS' || err "smb.conf wurde nicht von BetterNAS übernommen"
	[ -f "$W/etc/samba/betternas.d/shares.conf" ] || err "/etc/samba/betternas.d/shares.conf fehlt"
	[ -s "$W/usr/share/keyrings/betternas-archive-keyring.gpg" ] || err "APT-Schlüsselbund von BetterNAS fehlt"
	if [ -n "${APT_URL:-}" ]; then
		grep -qxF "URIs: $APT_URL" "$W/etc/apt/sources.list.d/betternas.sources" 2>/dev/null || err "BetterNAS-APT-Quelle fehlt oder zeigt nicht auf $APT_URL"
		head -n1 "$W/etc/apt/sources.list.d/betternas.sources" | grep -q 'Verwaltet von BetterNAS' || err "BetterNAS-APT-Quelle ohne Kopfzeile"
		grep -qxF "Suites: ${APT_SUITE:-stable}" "$W/etc/apt/sources.list.d/betternas.sources" || err "BetterNAS-APT-Quelle hat nicht die Suite ${APT_SUITE:-stable}"
	else
		[ ! -e "$W/etc/apt/sources.list.d/betternas.sources" ] || err "BetterNAS-APT-Quelle vorhanden, obwohl APT_URL leer ist"
	fi
	[ "$(stat -c %u "$W/etc/betternas/tls")" = "$(awk -F: '$1=="betternas"{print $3}' "$W/etc/passwd")" ] || err "/etc/betternas/tls gehört nicht betternas"
else
	echo "HINWEIS: Paket betternas nicht im Image (dist/betternas_*.deb fehlte beim Bau)"
fi

[ "$fail" = 0 ] && echo "rootfs OK"
exit "$fail"
