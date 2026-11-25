#!/usr/bin/env bash
# Projet FACTICE avec une team réglable depuis la carte TEAM (#250) — outil de test, aucun code de production.
#
#   seed-factice-team.sh <dossier-projet-factice> [<CLAUDE_CONFIG_DIR>]
#   seed-factice-team.sh --clean <dossier-projet-factice> [<CLAUDE_CONFIG_DIR>]
#
# Refuse tout dossier dont le nom ne contient pas « factice » et qui ne porte pas de marqueur .factice : jamais un
# vrai projet. Idempotent (réécrit les mêmes fichiers). --clean retire ce que le script a créé (liste dans
# <projet>/.factice-seed) et la team factice du CLAUDE_CONFIG_DIR.
#
# Variable facultative PANE_IDS="%12,%13,%14" : ids des panes tmux VIVANTS du projet ouvert, affectés dans l'ordre
# aux teammates « qui tournent » (dev-backend, qa, planner) pour que la carte les montre démarrés (⟳ sur dev-backend
# et qa : modèle réel ≠ modèle résolu). Sans elle : ids fictifs (%9001…), teammates non démarrés, pas de ⟳.
set -euo pipefail

clean=0
if [ "${1:-}" = "--clean" ]; then clean=1; shift; fi
proj="${1:-}"; cfg="${2:-}"
[ -n "$proj" ] || { sed -n '2,15p' "$0"; exit 2; }
proj="$(cd "$proj" 2>/dev/null && pwd)" || { echo "dossier introuvable : $1" >&2; exit 2; }
case "$(basename "$proj")" in *factice*) ;; *) [ -f "$proj/.factice" ] || { echo "REFUS : « $proj » n'est pas un projet factice (nom sans « factice », pas de .factice)" >&2; exit 2; } ;; esac
[ "$proj" != "/" ] && [ "$proj" != "$HOME" ] || { echo "REFUS : dossier invalide" >&2; exit 2; }
team="factice-250-$(basename "$proj")"
manifest="$proj/.factice-seed"
agents="$proj/.claude/agents"

if [ $clean -eq 1 ]; then
  if [ -f "$manifest" ]; then
    while IFS= read -r f; do [ -n "$f" ] && rm -f -- "$proj/$f" && echo "retiré : $f"; done < "$manifest"
    rm -f -- "$manifest"
  fi
  [ -n "$cfg" ] && [ -d "$cfg/teams/$team" ] && rm -rf -- "${cfg:?}/teams/$team" && echo "retiré : $cfg/teams/$team"
  echo "nettoyé (le dossier projet et .factice sont conservés)"; exit 0
fi

mkdir -p "$agents"
: > "$manifest.new"
put() { # put <chemin relatif> : contenu sur stdin
  mkdir -p "$(dirname "$proj/$1")"; cat > "$proj/$1"; echo "$1" >> "$manifest.new"; echo "écrit  : $1"
}
tpl() { # tpl <nom> <model> <effort>
  printf -- '---\nname: %s\ndescription: gabarit factice de %s\nmodel: %s\neffort: %s\n---\n\n# Template factice %s\n' "$1" "$1" "$2" "$3" "$1" \
    | put ".claude/agents/$1.template.md"
}

touch "$proj/.factice"
put .claude/project-config.json <<JSON
{"team_name":"$team","agents":{"registry":[
 {"name":"teamleader","tag":"v5.16.0"},{"name":"implementation-planner","tag":"v5.16.0"},
 {"name":"dev-backend","tag":"v5.16.0"},{"name":"dev-frontend","tag":"v5.16.0"},
 {"name":"qa","tag":"v5.16.0"},{"name":"doc-updater","tag":"v5.16.0"},{"name":"deploy","tag":"v5.16.0"},
 {"name":"cdp","tag":"v5.16.0"},{"name":"generic","tag":"v5.16.0"}],
 "generic":[{"name":"redacteur"}]}}
JSON
tpl teamleader opus medium
tpl implementation-planner opus high
tpl dev-backend sonnet medium
tpl dev-frontend sonnet medium
tpl qa sonnet medium
tpl doc-updater haiku low
tpl deploy sonnet low
tpl generic sonnet medium
# cdp : pas un teammate, ni model: ni effort: → « non réglable »
printf '# CDP factice\nRègles d orchestration (pas un teammate).\n' | put .claude/agents/cdp.template.md
# compagnons existants : sans frontmatter (bloc ajouté en tête au premier réglage) ; avec model: opus (source « compagnon »)
printf '# dev-backend — adaptations projet\n\n- Go, tests ciblés.\n' | put .claude/agents/dev-backend.md
printf -- '---\nmodel: opus\n---\n# qa — compagnon\nNotes QA.\n' | put .claude/agents/qa.md
printf '# redacteur — spécification\nInstance générique factice.\n' | put .claude/agents/generic.redacteur.md
printf '{\n  "model": "opus",\n  "effortLevel": "medium",\n  "permissions": { "allow": [] }\n}\n' | put .claude/settings.json

if [ -n "$cfg" ]; then
  IFS=',' read -r p1 p2 p3 <<< "${PANE_IDS:-%9001,%9002,%9003}"
  tdir="$cfg/teams/$team"; mkdir -p "$tdir"
  cat > "$tdir/config.json" <<JSON
{"name":"$team","members":[
 {"name":"team-lead","agentType":"team-lead","tmuxPaneId":"leader","cwd":"$proj"},
 {"name":"dev-backend","agentType":"dev-backend","tmuxPaneId":"${p1:-%9001}","model":"claude-opus-4-6","cwd":"$proj"},
 {"name":"qa","agentType":"qa","tmuxPaneId":"${p2:-%9002}","model":"claude-sonnet-4-6","cwd":"$proj"},
 {"name":"planner","agentType":"implementation-planner","tmuxPaneId":"${p3:-%9003}","model":"claude-opus-4-6","cwd":"$proj"}]}
JSON
  echo "écrit  : $tdir/config.json (dev-backend et qa tournent avec un modèle ≠ résolu → ⟳ si les panes existent)"
fi
sort -u "$manifest.new" > "$manifest"; rm -f "$manifest.new"
echo "terminé. Défaire : $0 --clean $proj${cfg:+ $cfg}"
