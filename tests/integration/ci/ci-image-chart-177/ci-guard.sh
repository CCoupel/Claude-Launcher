#!/usr/bin/env bash
# Garde de scripts/ci.sh (#177) — joué par QA (jamais depuis ci.sh : il le relance). Linux, dans le conteneur de dev.
#   1. outil manquant (helm, buildah) : ECHEC EXPLICITE, code != 0, jamais un saut silencieux ;
#   2. passage complet : ligne « durées » avec les étapes « chart » et « image », « CI OK » ;
#   3. image : étape à chaud <= IMAGE_BUDGET_S (20 s, décision D1), hors image <= CI_BUDGET_S (60 s) ;
#   4. une seule image « ci » gardée : celle du passage précédent est supprimée (stockage borné).
# Usage : tests/integration/ci/ci-image-chart-177/ci-guard.sh   (variables : IMAGE_BUDGET_S, CI_BUDGET_S)
set -uo pipefail
cd "$(dirname "$0")/../../../.."
IMAGE_BUDGET_S=${IMAGE_BUDGET_S:-20}
CI_BUDGET_S=${CI_BUDGET_S:-60}
work=$(mktemp -d "${TMPDIR:-/tmp}/ci-guard.XXXXXX")
trap 'rm -rf "$work"' EXIT
fails=0
ok()   { echo "  OK    $*"; }
ko()   { echo "  ECHEC $*"; fails=$((fails + 1)); }
# secondes (décimale « 14,8 s ») d'une étape dans la ligne « durées : a 0,2 s · b 1,0 s … »
step_s() { grep -E '^durées' "$1" | tr '·' '\n' | grep -E "(^|[ :])$2 [0-9]" | sed -E 's/.* ([0-9]+)[,.]([0-9]) s.*/\1.\2/' | head -1; }
le()   { awk -v a="$1" -v b="$2" 'BEGIN{exit !(a<=b)}'; }

# PATH « sans <outil> » : un lien par exécutable de l'ancien PATH, sauf les outils exclus
path_without() {
  local dir="$work/path-no-$1" d f
  mkdir -p "$dir"
  IFS=: read -ra dirs <<<"$PATH"
  for d in "${dirs[@]}"; do
    [ -d "$d" ] || continue
    for f in "$d"/*; do
      [ -x "$f" ] && [ ! -d "$f" ] || continue
      [ "$(basename "$f")" = "$1" ] && continue
      [ -e "$dir/$(basename "$f")" ] || ln -s "$f" "$dir/$(basename "$f")" 2>/dev/null
    done
  done
  echo "$dir"
}

echo "== 1. outil manquant : échec explicite =="
for tool in helm buildah; do
  p=$(path_without "$tool")
  PATH="$p" bash scripts/ci.sh >"$work/no-$tool.log" 2>&1
  code=$?
  if [ "$code" = 0 ]; then ko "sans $tool : ci.sh a réussi (saut silencieux)"; 
  elif grep -q "ECHEC : $tool est requis" "$work/no-$tool.log"; then ok "sans $tool : échec explicite (code $code, message nommant l'outil)"
  else ko "sans $tool : échec (code $code) mais sans message « $tool est requis » :"; tail -5 "$work/no-$tool.log"; fi
  grep -q "CI OK" "$work/no-$tool.log" && ko "sans $tool : « CI OK » affiché"
done

echo "== 1b. CL_CI_IMAGE (barrière de release : image non construite) =="
CL_CI_IMAGE=x bash scripts/ci.sh >"$work/badflag.log" 2>&1; code=$?
if [ "$code" != 0 ] && grep -q "CL_CI_IMAGE doit valoir 0 ou 1" "$work/badflag.log"; then ok "CL_CI_IMAGE=x : échec explicite (code $code)"; else ko "CL_CI_IMAGE=x : code $code sans le message attendu"; fi
p=$(path_without buildah)
CL_CI_IMAGE=0 PATH="$p" bash scripts/ci.sh >"$work/noimage.log" 2>&1; code=$?
if [ "$code" = 0 ] && grep -q '^CI OK' "$work/noimage.log"; then ok "CL_CI_IMAGE=0 sans buildah : CI OK (buildah n'est plus exigé)"; else ko "CL_CI_IMAGE=0 sans buildah : code $code"; tail -8 "$work/noimage.log"; fi
grep -q "image : NON construite" "$work/noimage.log" && ok "CL_CI_IMAGE=0 : ligne « image : NON construite » affichée" || ko "CL_CI_IMAGE=0 : ligne « NON construite » absente"
grep -E '^durées' "$work/noimage.log" | grep -q "image sautée" && ok "CL_CI_IMAGE=0 : étape « image sautée » dans les durées" || ko "CL_CI_IMAGE=0 : « image sautée » absent des durées"
CL_CI_IMAGE=0 PATH="$(path_without helm)" bash scripts/ci.sh >"$work/nohelm0.log" 2>&1; code=$?
if [ "$code" != 0 ] && grep -q "ECHEC : helm est requis" "$work/nohelm0.log"; then ok "CL_CI_IMAGE=0 sans helm : échec explicite (helm reste exigé)"; else ko "CL_CI_IMAGE=0 sans helm : code $code sans message"; fi

echo "== 2. deux passages complets (froid puis chaud) =="
for i in 1 2; do
  scripts/ci.sh >"$work/pass$i.log" 2>&1; code=$?
  if [ "$code" != 0 ]; then ko "passage $i : code $code"; tail -15 "$work/pass$i.log"; continue; fi
  grep -q '^CI OK' "$work/pass$i.log" && ok "passage $i : CI OK" || ko "passage $i : « CI OK » absent"
  line=$(grep -E '^durées' "$work/pass$i.log")
  echo "      $line"
  for step in chart image; do
    [ -n "$(step_s "$work/pass$i.log" "$step")" ] && ok "passage $i : étape « $step » mesurée" || ko "passage $i : étape « $step » absente de la ligne durées"
  done
done

echo "== 3. budgets (passage à chaud = le 2e ; un 3e si le budget image est dépassé : la durée dépend de la charge de la machine) =="
img=$(step_s "$work/pass2.log" image)
if [ -n "$img" ] && ! le "$img" "$IMAGE_BUDGET_S"; then
  echo "      image à chaud ${img} s > ${IMAGE_BUDGET_S} s : 3e passage pour écarter une charge ponctuelle"
  scripts/ci.sh >"$work/pass3.log" 2>&1 && img3=$(step_s "$work/pass3.log" image) || img3=""
  if [ -n "$img3" ] && le "$img3" "$img"; then img=$img3; cp "$work/pass3.log" "$work/pass2.log"; fi
fi
total=$(grep -E '^durées' "$work/pass2.log" | sed -E 's/.*total ([0-9]+)[,.]([0-9]) s.*/\1.\2/')
if [ -n "$img" ] && [ -n "$total" ]; then
  hors=$(awk -v t="$total" -v i="$img" 'BEGIN{printf "%.1f", t-i}')
  le "$img" "$IMAGE_BUDGET_S" && ok "image à chaud ${img} s <= ${IMAGE_BUDGET_S} s" || ko "image à chaud ${img} s > ${IMAGE_BUDGET_S} s"
  le "$hors" "$CI_BUDGET_S" && ok "hors image ${hors} s <= ${CI_BUDGET_S} s" || ko "hors image ${hors} s > ${CI_BUDGET_S} s"
else
  ko "durées illisibles (image='$img' total='$total')"
fi

echo "== 4. une seule image « ci » gardée =="
if [ "$(id -u)" = 0 ]; then bah=(buildah); else bah=(sudo buildah); fi
n=$("${bah[@]}" --storage-driver vfs images --noheading 2>/dev/null | awk '$1 == "localhost/claude-launcher" && $2 == "ci"' | wc -l)
[ "$n" = 1 ] && ok "une image localhost/claude-launcher:ci (passage précédent supprimé)" || ko "$n image(s) :ci (attendu 1)"
d=$("${bah[@]}" --storage-driver vfs images --noheading --filter dangling=true 2>/dev/null | wc -l)
[ "$d" -le 1 ] && ok "au plus une image orpheline ($d)" || ko "$d images orphelines : le passage précédent n'est pas nettoyé"
[ ! -d build/ci ] && ok "dossier build/ci supprimé" || ko "build/ci laissé"

echo
[ "$fails" = 0 ] && { echo "GARDE ci.sh : OK"; exit 0; } || { echo "GARDE ci.sh : $fails ECHEC(S)"; exit 1; }
