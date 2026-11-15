#!/bin/sh
# postrm für das Paket betternas. Daten unter /var/lib/betternas bleiben bei "remove" erhalten.
set -e
if [ "$1" = "purge" ]; then
	deb-systemd-helper purge betternas-agent.service betternas-api.service >/dev/null || true
	rm -rf /var/lib/betternas /etc/betternas/tls
fi
if [ -d /run/systemd/system ]; then
	systemctl daemon-reload || true
fi
exit 0
