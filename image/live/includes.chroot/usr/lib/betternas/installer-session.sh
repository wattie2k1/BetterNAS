#!/bin/sh
# Startet den Installer auf tty1 und danach erneut, falls er beendet wird.
while true; do
	/usr/sbin/betternas-installer
	echo
	echo "Der Installer wurde beendet. Enter startet ihn erneut (Alt+F2: Konsole)."
	read -r _
done
