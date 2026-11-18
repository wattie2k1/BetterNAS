#!/bin/sh
# Fügt ein Paket dem APT-Repo auf dem Branch gh-pages hinzu und veröffentlicht es als Orphan-Commit
# (die Historie wächst nicht). Bricht ab, ohne etwas zu pushen, wenn die Signatur nicht möglich ist.
# Aufruf: BNAS_APT_KEY=<schlüsseldatei> tools/publish-pages.sh <git-url> <keyring.gpg> <deb> <suite>...
# Anmeldung für den Push kommt von außen (z. B. GIT_CONFIG_*-Umgebung im Workflow).
set -eu
URL="${1:?Git-URL fehlt}"
KEYRING="${2:?Keyring fehlt}"
DEB="${3:?Paket fehlt}"
shift 3
[ "$#" -gt 0 ] || { echo "FEHLER: keine Suite angegeben" >&2; exit 1; }
HERE="$(cd "$(dirname "$0")" && pwd)"
FLAGS=""
for s in "$@"; do FLAGS="$FLAGS --suite $s"; done

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
if git ls-remote --exit-code --heads "$URL" gh-pages >/dev/null 2>&1; then
	git clone -q --depth 1 --branch gh-pages "$URL" "$WORK/pages"
else
	mkdir "$WORK/pages"
fi
# shellcheck disable=SC2086
"$HERE/apt-repo.sh" --native $FLAGS "$WORK/pages" "$DEB"
cp "$KEYRING" "$WORK/pages/betternas-archive-keyring.gpg"
touch "$WORK/pages/.nojekyll"

cd "$WORK/pages"
rm -rf .git
git init -q -b gh-pages
git add -A
git -c user.name="github-actions[bot]" -c user.email="41898282+github-actions[bot]@users.noreply.github.com" \
	commit -q -m "APT-Repo: $(basename "$DEB")"
git push -q --force "$URL" HEAD:gh-pages
echo "[publish] $(basename "$DEB") veröffentlicht (Suiten: $*)"
