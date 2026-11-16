#!/bin/sh
# postinst für das Paket betternas. Muss auch im chroot (Image-Bau) funktionieren.
set -e

if [ "$1" = "configure" ]; then
	getent group betternas >/dev/null || addgroup --quiet --system betternas
	getent passwd betternas >/dev/null || adduser --quiet --system --ingroup betternas \
		--home /var/lib/betternas --no-create-home --shell /usr/sbin/nologin betternas
	getent group betternas-admin >/dev/null || addgroup --quiet --system betternas-admin

	install -d -o betternas -g betternas -m 0750 /etc/betternas/tls /var/lib/betternas
	install -d -m 0755 /srv/disks /srv/parity /srv/pools /srv/appdata /srv/vms
	install -d -m 0755 /etc/betternas/snapraid /var/lib/betternas/snapraid

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
