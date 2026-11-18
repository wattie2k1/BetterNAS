#!/bin/sh
# Test für tools/apt-repo.sh (braucht apt-ftparchive, gpg, dpkg-deb, dpkg; sonst übersprungen).
set -u
DIR="$(cd "$(dirname "$0")/.." && pwd)"
REPO_SH="$DIR/apt-repo.sh"
for c in apt-ftparchive gpg dpkg-deb dpkg; do
	command -v "$c" >/dev/null 2>&1 || { echo "apt-repo: übersprungen ($c fehlt)"; exit 0; }
done
T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT
fail=0
tfail=0
err() { echo "FEHLER: $*" >&2; fail=1; tfail=1; }

# Zwei Wegwerf-Schlüssel: $T/<name>.sec.asc (privat) und $T/<name>.pub.gpg (öffentlich)
mkkey() {
	gh="$T/gh-$1"
	mkdir -m 700 "$gh"
	GNUPGHOME="$gh" gpg --batch --quiet --pinentry-mode loopback --passphrase "" \
		--quick-generate-key "Test $1 <t@t.invalid>" ed25519 sign never >/dev/null 2>&1
	GNUPGHOME="$gh" gpg --batch --quiet --pinentry-mode loopback --passphrase "" --export-secret-keys --armor > "$T/$1.sec.asc"
	GNUPGHOME="$gh" gpg --batch --quiet --export > "$T/$1.pub.gpg"
}
mkkey a
mkkey b
cat "$T/a.sec.asc" "$T/b.sec.asc" > "$T/ab.sec.asc"
: > "$T/empty.asc"

# gverify <schlüsselname> <gpg --verify-Argumente>: prüft mit einem frischen Schlüsselbund, der nur den öffentlichen Schlüssel enthält
gverify() {
	k="$1"; shift
	h="$(mktemp -d "$T/vh.XXXXXX")"
	GNUPGHOME="$h" gpg --batch --quiet --import "$T/$k.pub.gpg" >/dev/null 2>&1
	# Mehrfach signierte Dateien: gpg meldet einen Fehler, wenn eine der Signaturen unbekannt ist; apt genügt eine gültige.
	GNUPGHOME="$h" gpg --batch --status-fd 1 --verify "$@" 2>/dev/null | grep -q '^\[GNUPG:\] GOODSIG'
}

# Dummy-Paket: mkdeb <version> → $T/debs/betternas_<version>_amd64.deb
mkdeb() {
	d="$T/pkg-$1"
	mkdir -p "$d/DEBIAN" "$T/debs"
	printf 'Package: betternas\nVersion: %s\nArchitecture: amd64\nMaintainer: t <t@t.invalid>\nDescription: Test\n' "$1" > "$d/DEBIAN/control"
	dpkg-deb --root-owner-group -Zgzip --build "$d" "$T/debs/betternas_$1_amd64.deb" >/dev/null
}
for v in 0.1.0 0.2.0~rc1 0.2.0 0.3.0 0.4.0 0.5.0 0.6.0 0.7.0; do mkdeb "$v"; done
deb() { echo "$T/debs/betternas_$1_amd64.deb"; }
run() { # run <keyfile> <repo> args...
	k="$1"; shift
	BNAS_APT_KEY="$k" BNAS_KEEP="${BNAS_KEEP:-5}" sh "$REPO_SH" --native "$@"
}
packages() { cat "$1/dists/$2/main/binary-amd64/Packages" 2>/dev/null; }

test_rc_goes_only_to_testing() {
	r="$T/r1"
	run "$T/a.sec.asc" --suite testing "$r" "$(deb 0.2.0~rc1)" >/dev/null 2>&1 || err "rc: Lauf fehlgeschlagen"
	ls "$r"/pool/testing/main/b/betternas/*0.2.0~rc1* >/dev/null 2>&1 || err "rc: nicht in pool/testing"
	[ ! -e "$r/pool/stable" ] || err "rc: pool/stable darf nicht existieren"
	packages "$r" testing | grep -q '^Version: 0.2.0~rc1' || err "rc: nicht in testing/Packages"
	[ ! -e "$r/dists/stable" ] || err "rc: dists/stable darf nicht existieren"
}
test_stable_goes_to_both() {
	r="$T/r2"
	run "$T/a.sec.asc" --suite stable --suite testing "$r" "$(deb 0.2.0)" >/dev/null 2>&1 || err "stable: Lauf fehlgeschlagen"
	for s in stable testing; do
		packages "$r" "$s" | grep -q '^Version: 0.2.0$' || err "stable: nicht in $s/Packages"
		packages "$r" "$s" | grep -q "^Filename: pool/$s/main/b/betternas/" || err "stable: Filename in $s zeigt nicht auf pool/$s"
	done
}
test_default_is_both_suites() {
	r="$T/r2b"
	run "$T/a.sec.asc" "$r" "$(deb 0.2.0)" >/dev/null 2>&1 || err "default: Lauf fehlgeschlagen"
	[ -f "$r/dists/stable/InRelease" ] && [ -f "$r/dists/testing/InRelease" ] || err "default: beide Suiten erwartet"
}
test_first_release_from_empty_dir() {
	r="$T/does/not/exist/yet"
	run "$T/a.sec.asc" --suite stable "$r" "$(deb 0.1.0)" >/dev/null 2>&1 || err "leer: Lauf fehlgeschlagen"
	[ -f "$r/dists/stable/InRelease" ] || err "leer: InRelease fehlt"
}
test_prune_keeps_five() {
	r="$T/r3"
	for v in 0.1.0 0.2.0~rc1 0.2.0 0.3.0 0.4.0 0.5.0 0.6.0 0.7.0; do
		run "$T/a.sec.asc" --suite stable "$r" "$(deb "$v")" >/dev/null 2>&1 || err "prune: Lauf $v fehlgeschlagen"
	done
	n="$(ls "$r"/pool/stable/main/b/betternas/*.deb | wc -l | tr -d ' ')"
	[ "$n" = 5 ] || err "prune: $n statt 5 Pakete"
	for gone in 0.1.0 0.2.0~rc1 0.2.0; do
		ls "$r"/pool/stable/main/b/betternas/*"_${gone}_"* >/dev/null 2>&1 && err "prune: $gone hätte entfernt werden müssen"
	done
	for kept in 0.3.0 0.4.0 0.5.0 0.6.0 0.7.0; do
		packages "$r" stable | grep -q "^Version: $kept\$" || err "prune: $kept fehlt in Packages"
	done
}
test_prune_orders_tilde_below_release() {
	r="$T/r3b"
	for v in 0.2.0~rc1 0.2.0 0.3.0; do run "$T/a.sec.asc" --suite stable "$r" "$(deb "$v")" >/dev/null 2>&1; done
	BNAS_KEEP=2 run "$T/a.sec.asc" --suite stable "$r" "$(deb 0.4.0)" >/dev/null 2>&1
	packages "$r" stable | grep -q '^Version: 0.2.0$' && err "prune(2): 0.2.0 hätte entfernt werden müssen" 
	packages "$r" stable | grep -q '^Version: 0.2.0~rc1' && err "prune(2): ~rc1 hätte entfernt werden müssen"
	packages "$r" stable | grep -q '^Version: 0.4.0$' || err "prune(2): 0.4.0 fehlt"
	packages "$r" stable | grep -q '^Version: 0.3.0$' || err "prune(2): 0.3.0 fehlt"
}
test_signature_verifies() {
	r="$T/r4"
	run "$T/a.sec.asc" "$r" "$(deb 0.3.0)" >/dev/null 2>&1 || err "sig: Lauf fehlgeschlagen"
	for s in stable testing; do
		gverify a "$r/dists/$s/InRelease" >/dev/null 2>&1 || err "sig: InRelease($s) nicht verifizierbar"
		gverify a "$r/dists/$s/Release.gpg" "$r/dists/$s/Release" >/dev/null 2>&1 || err "sig: Release.gpg($s) nicht verifizierbar"
		grep -q "^Suite: $s\$" "$r/dists/$s/Release" || err "sig: Suite-Feld($s)"
		gverify b "$r/dists/$s/InRelease" >/dev/null 2>&1 && err "sig: fremder Schlüssel darf nicht verifizieren"
	done
}
test_double_signature() {
	r="$T/r5"
	run "$T/ab.sec.asc" "$r" "$(deb 0.3.0)" >/dev/null 2>&1 || err "doppelt: Lauf fehlgeschlagen"
	for k in a b; do
		gverify "$k" "$r/dists/stable/InRelease" >/dev/null 2>&1 || err "doppelt: InRelease nicht mit Schlüssel $k verifizierbar"
		gverify "$k" "$r/dists/stable/Release.gpg" "$r/dists/stable/Release" >/dev/null 2>&1 || err "doppelt: Release.gpg nicht mit Schlüssel $k verifizierbar"
	done
}
test_missing_key_aborts() {
	for k in "$T/empty.asc" "$T/nicht-vorhanden.asc"; do
		r="$T/r6-$(basename "$k")"
		if run "$k" "$r" "$(deb 0.3.0)" >/dev/null 2>"$T/err.txt"; then err "Schlüssel $k: Lauf hätte scheitern müssen"; fi
		grep -q "Signierschlüssel fehlt oder ist leer" "$T/err.txt" || err "Schlüssel $k: Fehlermeldung fehlt"
		[ ! -e "$r/dists/stable/InRelease" ] || err "Schlüssel $k: InRelease darf nicht entstehen"
	done
}
test_rejects_unknown_suite() {
	run "$T/a.sec.asc" --suite beta "$T/r7" "$(deb 0.3.0)" >/dev/null 2>&1 && err "Suite beta hätte abgelehnt werden müssen"
}

for t in test_rc_goes_only_to_testing test_stable_goes_to_both test_default_is_both_suites test_first_release_from_empty_dir \
	test_prune_keeps_five test_prune_orders_tilde_below_release test_signature_verifies test_double_signature \
	test_missing_key_aborts test_rejects_unknown_suite; do
	tfail=0
	"$t"
	[ "$tfail" = 0 ] && echo "ok  $t" || echo "FAIL $t"
done
exit "$fail"
