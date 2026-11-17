#!/bin/sh
# Aktiviert Dienste, legt Gruppen an und sperrt root.
set -eu
R="$1"
chroot "$R" systemctl enable systemd-networkd systemd-resolved systemd-timesyncd ssh avahi-daemon
chroot "$R" groupadd -f -r betternas-admin
chroot "$R" passwd -l root
ln -sf /run/systemd/resolve/stub-resolv.conf "$R/etc/resolv.conf"
# Standard-NAT-Netz von libvirt beim Start aktivieren (die Paket-Postinst schafft das im Chroot nicht).
mkdir -p "$R/etc/libvirt/qemu/networks/autostart"
ln -sf ../default.xml "$R/etc/libvirt/qemu/networks/autostart/default.xml"
