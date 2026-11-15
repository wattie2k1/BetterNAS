#!/bin/sh
# postinst für das Paket betternas. Muss auch im chroot (Image-Bau) funktionieren.
set -e

if [ "$1" = "configure" ]; then
	getent group betternas >/dev/null || addgroup --quiet --system betternas
	getent passwd betternas >/dev/null || adduser --quiet --system --ingroup betternas \
		--home /var/lib/betternas --no-create-home --shell /usr/sbin/nologin betternas
	getent group betternas-admin >/dev/null || addgroup --quiet --system betternas-admin

	install -d -o betternas -g betternas -m 0750 /etc/betternas/tls /var/lib/betternas
	install -d -m 0755 /srv/disks /srv/pools /srv/appdata /srv/vms

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
