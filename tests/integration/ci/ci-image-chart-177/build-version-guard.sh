#!/usr/bin/env bash
# Garde de deploy/image/build.sh (#177) : le binaire candidat repris par SOURCE_BIN doit annoncer EXACTEMENT la version
# demandée — « v6.3.0.90 » ou « v6.3.0.9x » n'est PAS « v6.3.0.9 » (une simple correspondance de préfixe publierait
# une image sous le mauvais tag). Rapide (< 5 s) : binaire FACTICE (script) et BUILDAH remplacé par un bouchon qui
# note son appel — aucune image n'est construite. Joué par QA.
set -uo pipefail
cd "$(dirname "$0")/../../../.."
work=$(mktemp -d "${TMPDIR:-/tmp}/build-guard.XXXXXX")
out="build/guard-$$"
trap 'rm -rf "$work" "$out"' EXIT
fails=0
ok() { echo "  OK    $*"; }
ko() { echo "  ECHEC $*"; fails=$((fails + 1)); }

cat >"$work/buildah-stub" <<STUB
#!/bin/sh
echo "BUILDAH-APPELE" >> "$work/buildah.calls"
exit 3
STUB
chmod +x "$work/buildah-stub"

fake() { # fake <sortie de « version »> : binaire factice
  printf '#!/bin/sh\necho "%s"\n' "$1" >"$work/claude-launcher-ng2"; chmod +x "$work/claude-launcher-ng2"
}
run() { # run <version demandée> : lance build.sh avec le binaire factice et le bouchon
  rm -f "$work/buildah.calls"
  SOURCE_BIN="$work/claude-launcher-ng2" OUT_DIR="$out" BUILDAH="$work/buildah-stub" deploy/image/build.sh "$1" >"$work/run.log" 2>&1
}

echo "== version annoncée différente de la version demandée : refus, aucune construction =="
want=v6.3.0.9
for got in "Claude-Launcher-NG2 v6.3.0.90" "Claude-Launcher-NG2 v6.3.0.9x" "Claude-Launcher-NG2 v6.3.0.9-dev" "Claude-Launcher-NG2 v6.3.0.9.1" \
           "Claude-Launcher-NG2 v6.3.0" "Claude-Launcher-NG2 v6.3.0.8" "Claude-Launcher-NG2 v6.3.0.99" \
           "Claude-Launcher-NG2 v6.3.0.9 extra"; do
  fake "$got"
  run "$want"; code=$?
  if [ "$code" != 0 ] && [ ! -e "$work/buildah.calls" ] && grep -q "ECHEC" "$work/run.log"; then
    ok "« $got » refusé pour $want (code $code, aucune construction)"
  elif [ -e "$work/buildah.calls" ]; then ko "« $got » ACCEPTÉ pour $want : la construction a démarré (correspondance de préfixe)"
  else ko "« $got » : code $code sans message ECHEC :"; tail -3 "$work/run.log"; fi
done

echo "== version exacte : acceptée (la construction démarre : le bouchon est appelé) =="
for got in "Claude-Launcher-NG2 v6.3.0.9"; do
  fake "$got"
  run "$want"
  if [ -e "$work/buildah.calls" ]; then ok "« $got » accepté pour $want"
  else ko "« $got » refusé à tort pour $want :"; tail -3 "$work/run.log"; fi
done
fake "Claude-Launcher-NG2 v6.3.0.9"
run v6.3.0.90
[ -e "$work/buildah.calls" ] && ko "v6.3.0.9 accepté pour v6.3.0.90" || ok "v6.3.0.9 refusé pour v6.3.0.90 (sens inverse)"

echo
[ "$fails" = 0 ] && { echo "GARDE build.sh : OK"; exit 0; } || { echo "GARDE build.sh : $fails ECHEC(S)"; exit 1; }
