#!/bin/sh
# prerm für das Paket betternas.
set -e
if [ "$1" = "remove" ] && [ -d /run/systemd/system ]; then
	systemctl stop betternas-api.service betternas-agent.service || true
fi
exit 0
