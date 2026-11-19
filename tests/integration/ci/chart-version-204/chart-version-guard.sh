#!/usr/bin/env bash
# Contrôle « version du chart = branche milestone » de scripts/ci.sh (#204). Linux. Joué par QA.
#   1. Chart.yaml juste sur milestone/vX.Y.Z           -> le contrôle ne signale rien ;
#   2. Chart.yaml faux (version, puis appVersion)      -> ci.sh KO, message identique à release.yml ;
#   3. hors branche milestone (main, PR)               -> contrôle ignoré, message explicite, jamais KO à cause du chart ;
#   4. branche lue par `git rev-parse` si GITHUB_REF_NAME est absent.
# Banc : copie temporaire de scripts/ci.sh + Chart.yaml dans _work/tmp/, ng2/ vide : ci.sh échoue ensuite
# ailleurs (node --check sans fichier) ; seul compte ici le message du contrôle du chart. Jamais la CI complète.
# Usage : tests/integration/ci/chart-version-204/chart-version-guard.sh   -> « GARDE chart-version : OK », code 0
set -uo pipefail
cd "$(dirname "$0")/../../../.."
root=$PWD
mkdir -p _work/tmp
work=$(mktemp -d "$root/_work/tmp/chart-guard.XXXXXX")
trap 'rm -rf "$work"' EXIT
fails=0
ok() { echo "  OK    $*"; }
ko() { echo "  ECHEC $*"; fails=$((fails + 1)); }

bench() {   # bench <version> <appVersion> : banc avec un Chart.yaml donné
  rm -rf "$work/b"; mkdir -p "$work/b/scripts" "$work/b/ng2" "$work/b/deploy/helm/claude-launcher"
  cp "$root/scripts/ci.sh" "$work/b/scripts/ci.sh"
  printf 'apiVersion: v2\nname: claude-launcher\nversion: %s\nappVersion: "%s"\n' "$1" "$2" > "$work/b/deploy/helm/claude-launcher/Chart.yaml"
  git -C "$work/b" init -q 2>/dev/null; git -C "$work/b" checkout -q -b "${3:-main}" 2>/dev/null
}
run() {     # run <ref|-> : joue ci.sh du banc ; $out = sortie, $rc = code
  if [ "$1" = - ]; then out=$(cd "$work/b" && env -u GITHUB_REF_NAME bash scripts/ci.sh 2>&1); else out=$(cd "$work/b" && GITHUB_REF_NAME="$1" bash scripts/ci.sh 2>&1); fi
  rc=$?
}

echo "== 1. Chart.yaml juste sur milestone/v9.9.9 =="
bench 9.9.9 v9.9.9; run milestone/v9.9.9
if echo "$out" | grep -q 'Chart.yaml : version='; then ko "contrôle KO alors que le chart est juste"; else ok "aucun refus du chart"; fi

echo "== 2. Chart.yaml faux sur milestone/v9.9.9 =="
for pair in "9.9.8 v9.9.9" "9.9.9 v9.9.8" "6.4.0 v6.4.0"; do
  set -- $pair; bench "$1" "$2"; run milestone/v9.9.9
  [ "$rc" != 0 ] && ok "version=$1 appVersion=$2 : code $rc" || ko "version=$1 appVersion=$2 : code 0"
  echo "$out" | grep -qF "Chart.yaml : version=$1 appVersion=$2, attendu 9.9.9 et v9.9.9" \
    && ok "message identique à release.yml" || ko "message attendu absent : $(echo "$out" | grep -i chart | head -2)"
done

echo "== 3. Hors branche milestone : contrôle ignoré =="
for ref in main feature/x; do
  bench 1.0.0 v1.0.0; run "$ref"
  echo "$out" | grep -q 'Chart.yaml : version=' && ko "$ref : refus du chart à tort" || ok "$ref : pas de refus du chart"
  echo "$out" | grep -qiE 'ignor' && ok "$ref : message « ignoré »" || ko "$ref : message explicite absent"
done

echo "== 4. Branche lue par git rev-parse (GITHUB_REF_NAME absent) =="
bench 9.9.8 v9.9.8 milestone/v9.9.9; run -
echo "$out" | grep -qF 'Chart.yaml : version=9.9.8 appVersion=v9.9.8, attendu 9.9.9 et v9.9.9' && ok "branche git prise en compte" || ko "branche git ignorée"

[ "$fails" = 0 ] && echo "GARDE chart-version : OK" || { echo "GARDE chart-version : $fails échec(s)"; exit 1; }
