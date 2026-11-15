#!/bin/sh
# Entfernt Identitäten aus dem Image; jede Installation erzeugt eigene.
set -eu
R="$1"
: > "$R/etc/machine-id"
rm -f "$R/var/lib/dbus/machine-id" "$R"/etc/ssh/ssh_host_*
echo betternas > "$R/etc/hostname"
printf '127.0.0.1\tlocalhost\n127.0.1.1\tbetternas.local betternas\n::1\tlocalhost ip6-localhost ip6-loopback\n' > "$R/etc/hosts"
: > "$R/etc/fstab"
