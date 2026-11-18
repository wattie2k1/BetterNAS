#!/bin/sh
# Erstellt ein neues BetterNAS-Release auf GitHub: ISO bauen und prüfen, Tag setzen und pushen,
# auf den Release-Workflow warten und das ISO an das Release anhängen.
#
# Aufruf:  tools/release.sh [Optionen]
#   --version X.Y.Z        Version (sonst Abfrage, Vorschlag = nächste Patch-Version)
#   --channel testing|stable   Kanal (sonst Abfrage)
#   --remote HOST          Rechner für den ISO-Bau und die QEMU-Tests (Standard: betternas-dev)
#   --modes "uefi bios secureboot"   Testmodi des ISO (Standard: alle drei)
#   --no-iso               kein ISO bauen und anhängen
#   --yes                  ohne Rückfrage fortfahren
#   --dry-run              nur anzeigen, was passieren würde (nichts bauen, taggen, pushen)
#
# Kanäle (siehe .github/workflows/release.yml):
#   testing  -> Tag vX.Y.Z-rcN: nur Kanal testing (Vorabversion)
#   stable   -> Tag vX.Y.Z: Kanäle stable UND testing (testing enthält immer auch die stabilen Versionen)
# Voraussetzungen: gh (angemeldet), sauberer und gepushter main, für das ISO ein SSH-Host wie in `make remote`.
set -eu

REPO="wattie2k1/BetterNAS"
PAGES="https://wattie2k1.github.io/BetterNAS"
SRC="$(cd "$(dirname "$0")/.." && pwd)"
REMOTE="betternas-dev"
MODES="uefi bios secureboot"
VERSION=""
CHANNEL=""
ISO=1
YES=0
DRY=0

die() { echo "FEHLER: $*" >&2; exit 1; }
say() { echo "[release] $*"; }

while [ $# -gt 0 ]; do
	case "$1" in
	--version) VERSION="${2:?--version braucht einen Wert}"; shift 2 ;;
	--channel) CHANNEL="${2:?--channel braucht einen Wert}"; shift 2 ;;
	--remote) REMOTE="${2:?--remote braucht einen Wert}"; shift 2 ;;
	--modes) MODES="${2-}"; shift 2 ;;
	--no-iso) ISO=0; shift ;;
	--yes) YES=1; shift ;;
	--dry-run) DRY=1; shift ;;
	-h | --help) sed -n '2,17p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
	*) die "Unbekannte Option '$1' (siehe --help)" ;;
	esac
done

cd "$SRC"

# Führt einen Befehl aus – im Trockenlauf nur anzeigen.
run() {
	if [ "$DRY" = 1 ]; then
		echo "[trocken] $*"
	else
		"$@"
	fi
}

ask() { # ask <Frage> <Standard> -> Antwort in $REPLY
	printf '%s [%s]: ' "$1" "$2" >&2
	read -r REPLY || REPLY=""
	[ -n "$REPLY" ] || REPLY="$2"
}

command -v git >/dev/null || die "git fehlt"
command -v gh >/dev/null || die "gh fehlt (https://cli.github.com)"

# --- Voraussetzungen -------------------------------------------------------------------------------
[ "$(git rev-parse --abbrev-ref HEAD)" = main ] || die "Du bist nicht auf main"
[ -z "$(git status --porcelain)" ] || die "Es gibt uncommittete Änderungen – erst committen"
git fetch -q --tags origin
[ "$(git rev-parse HEAD)" = "$(git rev-parse origin/main)" ] || die "main und origin/main weichen ab – erst pushen bzw. pullen"
gh auth status >/dev/null 2>&1 || die "gh ist nicht angemeldet (gh auth login)"

# --- Version ---------------------------------------------------------------------------------------
latest_stable="$(git tag -l 'v[0-9]*' | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -1 || true)"
if [ -z "$VERSION" ]; then
	suggest="0.1.0"
	if [ -n "$latest_stable" ]; then
		v="${latest_stable#v}"
		suggest="${v%.*}.$(( ${v##*.} + 1 ))"
	fi
	echo "Letzte stabile Version: ${latest_stable:-keine}" >&2
	ask "Neue Version (X.Y.Z)" "$suggest"
	VERSION="$REPLY"
fi
printf '%s\n' "$VERSION" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$' || die "Ungültige Version '$VERSION' (erlaubt: X.Y.Z)"
if git rev-parse -q --verify "refs/tags/v$VERSION" >/dev/null; then die "Tag v$VERSION existiert schon"; fi

# --- Kanal -----------------------------------------------------------------------------------------
if [ -z "$CHANNEL" ]; then
	cat >&2 <<EOF
Wohin soll v$VERSION veröffentlicht werden?
  1) nur testing   – Vorabversion (Tag v$VERSION-rcN), Geräte auf „stable“ sehen sie nicht
  2) stable + testing – Release (Tag v$VERSION), erscheint in beiden Kanälen
     (nur „stable“ ist nicht möglich: testing enthält immer auch die stabilen Versionen)
EOF
	ask "Auswahl" "1"
	case "$REPLY" in
	1 | testing) CHANNEL=testing ;;
	2 | stable | beides) CHANNEL=stable ;;
	*) die "Ungültige Auswahl '$REPLY'" ;;
	esac
fi
case "$CHANNEL" in testing | stable) ;; *) die "Ungültiger Kanal '$CHANNEL' (testing oder stable)" ;; esac

if [ "$CHANNEL" = testing ]; then
	last_rc="$(git tag -l "v$VERSION-rc*" | sed "s/^v$VERSION-rc//" | grep -E '^[0-9]+$' | sort -n | tail -1 || true)"
	TAG="v$VERSION-rc$(( ${last_rc:-0} + 1 ))"
	SUITE=testing
else
	TAG="v$VERSION"
	SUITE=stable
fi
TAGVER="${TAG#v}"
DEBVER="$(tools/deb-version.sh "$TAG" | cut -d' ' -f1)"

# --- Zusammenfassung -------------------------------------------------------------------------------
if [ "$ISO" = 1 ]; then iso_txt="ja (Host $REMOTE, Tests: ${MODES:-keine})"; else iso_txt="nein"; fi
cat >&2 <<EOF

  Tag:        $TAG  (Paketversion $DEBVER)
  Kanäle:     $([ "$CHANNEL" = stable ] && echo "stable + testing" || echo "nur testing (Vorabversion)")
  Commit:     $(git log -1 --format='%h %s')
  ISO:        $iso_txt
  Trockenlauf: $([ "$DRY" = 1 ] && echo ja || echo nein)

EOF
if [ "$YES" != 1 ] && [ "$DRY" != 1 ]; then
	ask "Fortfahren? (ja/nein)" "nein"
	case "$REPLY" in ja | j | y | yes) ;; *) die "Abgebrochen" ;; esac
fi

# --- ISO bauen (zuerst: scheitert hier etwas, ist noch nichts veröffentlicht) ------------------------
ISO_FILE="$SRC/dist/betternas-$TAGVER-amd64.iso"
if [ "$ISO" = 1 ]; then
	say "Baue ISO $DEBVER auf $REMOTE (dauert einige Minuten) …"
	LOG="$(mktemp)"
	trap 'rm -f "$LOG"' EXIT
	if [ "$DRY" = 1 ]; then
		echo "[trocken] make remote VERSION=$DEBVER REMOTE=$REMOTE MODES=\"$MODES\" APT_URL=$PAGES APT_SUITE=$SUITE"
	else
		make remote VERSION="$DEBVER" REMOTE="$REMOTE" MODES="$MODES" APT_URL="$PAGES" APT_SUITE="$SUITE" 2>&1 | tee "$LOG" || true
		[ -f "dist/betternas-$DEBVER-amd64.iso" ] || die "ISO wurde nicht gebaut – es wurde nichts veröffentlicht"
		if grep -q '\[e2e\] FEHL' "$LOG"; then die "QEMU-Tests melden Fehler (siehe oben) – es wurde nichts veröffentlicht"; fi
		want=0
		for _ in $MODES; do want=$((want + 1)); done
		got="$(grep -c 'ALLE PRÜFUNGEN BESTANDEN' "$LOG" || true)"
		[ "$got" -ge "$want" ] || die "Nur $got von $want QEMU-Testmodi bestanden – es wurde nichts veröffentlicht"
		# Das Release-Asset heißt nach dem Tag (X.Y.Z-rcN), die Paketversion enthält „~“.
		[ "$DEBVER" = "$TAGVER" ] || mv -f "dist/betternas-$DEBVER-amd64.iso" "$ISO_FILE"
		say "ISO fertig: $ISO_FILE"
	fi
fi

# --- Tag setzen und pushen -------------------------------------------------------------------------
say "Setze Tag $TAG und pushe ihn …"
run git tag "$TAG"
run git push origin "$TAG"

# --- Auf den Workflow warten -----------------------------------------------------------------------
if [ "$DRY" = 1 ]; then
	echo "[trocken] warte auf den Workflow „Release“ für $TAG (Freigabe im Environment „release“)"
	[ "$ISO" = 1 ] && echo "[trocken] tools/release-iso.sh $TAGVER"
	say "Trockenlauf beendet – nichts wurde gebaut, getaggt oder gepusht."
	exit 0
fi
say "Warte auf den Workflow „Release“ …"
run_id=""
i=0
while [ -z "$run_id" ] && [ "$i" -lt 30 ]; do
	run_id="$(gh run list --repo "$REPO" --workflow release.yml --branch "$TAG" --limit 1 --json databaseId --jq '.[0].databaseId // empty' 2>/dev/null || true)"
	[ -n "$run_id" ] || { sleep 2; i=$((i + 1)); }
done
[ -n "$run_id" ] || die "Workflow-Lauf nicht gefunden – prüfe https://github.com/$REPO/actions; danach: tools/release-iso.sh $TAGVER"
say "Lauf: https://github.com/$REPO/actions/runs/$run_id"
say "Der Schritt „publish“ wartet auf deine Freigabe im Environment „release“ (Seite oben → Review deployments)."
gh run watch "$run_id" --repo "$REPO" --exit-status || die "Workflow fehlgeschlagen – Release unvollständig (https://github.com/$REPO/actions/runs/$run_id)"

# --- ISO anhängen ----------------------------------------------------------------------------------
if [ "$ISO" = 1 ]; then
	say "Hänge ISO an das Release an …"
	tools/release-iso.sh "$TAGVER" || die "ISO-Upload fehlgeschlagen – später erneut: tools/release-iso.sh $TAGVER"
fi
say "Fertig: https://github.com/$REPO/releases/tag/$TAG"
