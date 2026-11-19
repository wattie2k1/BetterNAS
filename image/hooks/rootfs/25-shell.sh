#!/bin/sh
# Aktiviert die Aliase ll und la in der Vorlage für neue Benutzer (und für root, falls dort schon eine .bashrc liegt).
set -eu
R="$1"
for f in "$R/etc/skel/.bashrc" "$R/root/.bashrc"; do
	[ -f "$f" ] || continue
	sed -i -e "s/^#alias ll='ls -l'/alias ll='ls -l'/" -e "s/^#alias la='ls -A'/alias la='ls -A'/" "$f"
	grep -q "^alias ll=" "$f" && grep -q "^alias la=" "$f" || { echo "25-shell: Aliase in $f nicht gefunden" >&2; exit 1; }
done
