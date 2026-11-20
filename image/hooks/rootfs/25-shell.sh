#!/bin/sh
# Aktiviert die Aliase ll und la in der Vorlage für neue Benutzer und für root (falls dort schon eine .bashrc liegt).
# Die Vorlage in /etc/skel muss die auskommentierten Zeilen enthalten (sonst Fehler). Die .bashrc von root
# stammt aus base-files und sieht je nach Version anders aus; fehlen dort die Aliase, werden sie angehängt.
set -eu
R="$1"
for f in "$R/etc/skel/.bashrc" "$R/root/.bashrc"; do
	[ -f "$f" ] || continue
	sed -i -e "s/^#alias ll='ls -l'/alias ll='ls -l'/" -e "s/^#alias la='ls -A'/alias la='ls -A'/" "$f"
	if grep -q "^alias ll=" "$f" && grep -q "^alias la=" "$f"; then
		continue
	fi
	case "$f" in
	*/etc/skel/.bashrc)
		echo "25-shell: Aliase in $f nicht gefunden" >&2
		exit 1
		;;
	esac
	grep -q "^alias ll=" "$f" || printf '%s\n' "alias ll='ls -l'" >> "$f"
	grep -q "^alias la=" "$f" || printf '%s\n' "alias la='ls -A'" >> "$f"
done
