#!/bin/sh
# Erzeugt die unterstützten Locales und setzt Deutsch als Standard.
set -eu
R="$1"
printf 'de_DE.UTF-8 UTF-8\nen_US.UTF-8 UTF-8\n' > "$R/etc/locale.gen"
chroot "$R" locale-gen
echo 'LANG=de_DE.UTF-8' > "$R/etc/default/locale"
ln -sf /usr/share/zoneinfo/Europe/Berlin "$R/etc/localtime"
echo Europe/Berlin > "$R/etc/timezone"
