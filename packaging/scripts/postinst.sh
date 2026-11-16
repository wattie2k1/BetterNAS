#!/bin/sh
# postinst für das Paket betternas. Muss auch im chroot (Image-Bau) funktionieren.
set -e

if [ "$1" = "configure" ]; then
	getent group betternas >/dev/null || addgroup --quiet --system betternas
	getent passwd betternas >/dev/null || adduser --quiet --system --ingroup betternas \
		--home /var/lib/betternas --no-create-home --shell /usr/sbin/nologin betternas
	getent group betternas-admin >/dev/null || addgroup --quiet --system betternas-admin

	install -d -o betternas -g betternas -m 0750 /etc/betternas/tls /var/lib/betternas
	install -d -m 0755 /srv/disks /srv/parity /srv/pools /srv/appdata /srv/vms /srv/vms/disks /srv/vms/iso
	install -d -m 0755 /etc/betternas/snapraid /var/lib/betternas/snapraid
	install -d -m 0700 /srv/appdata/stacks
	install -d -m 0710 /srv/appdata/docker

	# Docker: data-root auf /srv/appdata (nicht mergerfs). Eine vorhandene eigene daemon.json bleibt unangetastet.
	install -d -m 0755 /etc/docker
	if [ ! -e /etc/docker/daemon.json ]; then
		install -m 0644 /usr/share/betternas/docker-daemon.json /etc/docker/daemon.json
		if [ -d /run/systemd/system ]; then
			systemctl try-restart docker.service || true
		fi
	fi

	# libvirt: Standard-NAT-Netz `default` aktivieren (VMs ohne eigene Bridge). Im chroot ohne systemd übersprungen.
	if [ -d /run/systemd/system ] && command -v virsh >/dev/null 2>&1; then
		systemctl enable --now libvirtd.socket >/dev/null 2>&1 || true
		# libvirt kommt direkt nach der Installation oft erst nach einigen Sekunden hoch.
		for _ in 1 2 3 4 5 6 7 8 9 10; do
			virsh -c qemu:///system net-info default >/dev/null 2>&1 && break
			sleep 1
		done
		virsh -c qemu:///system net-autostart default >/dev/null 2>&1 || true
		virsh -c qemu:///system net-start default >/dev/null 2>&1 || true
	fi

	# Samba: Freigaben liegen in /etc/samba/betternas.d. Die smb.conf wird nur ersetzt, wenn sie noch der
	# Debian-Auslieferung entspricht oder fehlt; eigene Konfiguration bleibt unangetastet.
	install -d -m 0755 /etc/samba/betternas.d
	if [ ! -e /etc/samba/betternas.d/shares.conf ]; then
		printf '%s\n' "# Verwaltet von BetterNAS – nicht manuell bearbeiten" > /etc/samba/betternas.d/shares.conf
	fi
	if [ ! -e /etc/samba/smb.conf ] || { [ -e /usr/share/samba/smb.conf ] && cmp -s /etc/samba/smb.conf /usr/share/samba/smb.conf; }; then
		[ -e /etc/samba/smb.conf ] && cp -p /etc/samba/smb.conf /etc/samba/smb.conf.betternas-orig
		install -m 0644 /usr/share/betternas/smb.conf /etc/samba/smb.conf
		if [ -d /run/systemd/system ]; then
			systemctl try-reload-or-restart smbd.service || true
		fi
	fi

	deb-systemd-helper enable betternas-agent.service >/dev/null || true
	deb-systemd-helper enable betternas-api.service >/dev/null || true
	if [ -d /run/systemd/system ]; then
		systemctl daemon-reload
		systemctl restart betternas-agent.service betternas-api.service || true
	fi

	if [ -z "$(getent group betternas-admin | cut -d: -f4)" ]; then
		echo "BetterNAS: Noch kein Administrator. Anlegen mit: sudo adduser <benutzer> betternas-admin"
	fi
fi
exit 0
