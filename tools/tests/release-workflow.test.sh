#!/bin/sh
# Statische Prüfung von .github/workflows/release.yml (Sicherheitsvorgaben aus dem Design).
set -u
DIR="$(cd "$(dirname "$0")/../.." && pwd)"
W="$DIR/.github/workflows/release.yml"
fail=0
err() { echo "FEHLER: $*" >&2; fail=1; }
[ -f "$W" ] || { err "release.yml fehlt"; exit 1; }

# 1. Jede Action ist auf eine 40-stellige Commit-SHA gepinnt.
unpinned="$(grep -E '^\s*-?\s*uses:' "$W" | grep -vE 'uses: [^@ ]+@[0-9a-f]{40}( |$)' || true)"
[ -z "$unpinned" ] || err "Actions nicht auf SHA gepinnt: $unpinned"
grep -qE '^\s*-?\s*uses:' "$W" || err "keine Actions gefunden (Test wirkungslos)"

# 2. Nur Tags v* lösen aus, keine Pull-Requests.
grep -qE "^\s*tags:\s*\[\s*'?v\*'?\s*\]" "$W" || err "Trigger muss tags: ['v*'] sein"
grep -q 'pull_request' "$W" && err "pull_request darf nicht auslösen"

# 3. Workflow-Rechte standardmäßig leer; contents: write nur genau einmal (publish).
grep -qE '^permissions:\s*\{\}\s*$' "$W" || err "permissions: {} auf Workflow-Ebene fehlt"
[ "$(grep -c 'contents: write' "$W")" = 1 ] || err "contents: write muss genau einmal vorkommen"

# 4. Das Secret kommt nur einmal vor, und zwar im Job publish mit environment: release.
[ "$(grep -c 'secrets.APT_SIGNING_KEY' "$W")" = 1 ] || err "secrets.APT_SIGNING_KEY muss genau einmal vorkommen"
pub="$(awk '/^  publish:/{p=1} p' "$W")"
echo "$pub" | grep -q 'environment: release' || err "publish braucht environment: release"
echo "$pub" | grep -q 'secrets.APT_SIGNING_KEY' || err "das Secret gehört in den Job publish"
echo "$pub" | grep -qE 'group: release-publish' || err "publish braucht concurrency group release-publish"
echo "$pub" | grep -q 'cancel-in-progress: false' || err "publish darf Läufe nicht abbrechen (cancel-in-progress: false)"
echo "$pub" | grep -q 'APT_SIGNING_KEY. \]\|-n "\$APT_SIGNING_KEY"' || err "publish muss ein leeres Secret abfangen"

# 5. Reihenfolge: meta → test → build → publish, publish ohne Tests nicht erreichbar.
grep -qE '^  meta:' "$W" || err "Job meta fehlt"
awk '/^  test:/{t=1} /^  build:/{t=0} t' "$W" | grep -q 'needs: meta' || err "test braucht meta"
awk '/^  build:/{b=1} /^  publish:/{b=0} b' "$W" | grep -qE 'needs: \[meta, test\]' || err "build braucht meta und test"
echo "$pub" | grep -qE 'needs: \[meta, build\]' || err "publish braucht meta und build"

# 6. Tag/Version nie direkt in Shell-Zeilen einsetzen (Injection): kein ${{ github.ref_name }} in run-Blöcken.
grep -n 'github.ref_name' "$W" | grep -v 'GITHUB_REF_NAME' | grep -vE '^\s*[0-9]+:\s*[A-Z_]+:' >/dev/null && err "github.ref_name nur über env verwenden"

# 7. Ungültige Tags werden vor dem Test abgefangen (deb-version.sh im Job meta).
awk '/^  meta:/{m=1} /^  test:/{m=0} m' "$W" | grep -q 'tools/deb-version.sh' || err "meta muss tools/deb-version.sh aufrufen"

[ "$fail" = 0 ] && echo "release-workflow: OK"
exit "$fail"
