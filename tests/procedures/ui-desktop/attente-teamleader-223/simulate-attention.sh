#!/bin/sh
# simulate-attention.sh — simule l'attente d'un teamleader (#223) quand claude est un faux claude (QUALIF) :
# écrit / efface l'état que poseraient les hooks (contracts/attention.md § 4), sans jq, sh POSIX.
#
# Usage : simulate-attention.sh <chemin du projet> question|permission|teammate|fin [--pod <pod> [<namespace>]]
#   question   : attente « question » du teamleader  (charge utile PreToolUse AskUserQuestion)
#   permission : attente « permission » du teamleader (charge utile Notification)
#   teammate   : attente écrite par un AUTRE pane (un teammate) : ne doit RIEN signaler
#   fin        : retire tout état d'attente du projet (équivaut à la réponse de l'utilisateur)
#
# Pane leader : même règle que le launcher (core.LeaderOf) = le plus petit %id des panes de la session tmux du projet.
# Session du projet : variable TMUX_SESSION si posée ; sinon la session du serveur tmux dont un pane a pour dossier
# courant le projet ; sinon la session « p-… » dont le nom finit par le nom du projet.
# Serveur tmux : variable TMUX_SOCKET (défaut : claude-desk-ng2, serveur de la fenêtre native sur l'hôte ; CL_DESK_SOCKET
# le remplace dans les tests) ; en pod : serveur tmux par défaut du conteneur, session « cl » (TMUX_SESSION le remplace).
# --pod : le script se rejoue DANS le pod par `kubectl exec -i <pod> [-n <namespace>] -- sh -s` ; <chemin du projet> est alors
# le chemin vu DANS le pod (ex. /share/MFS/ROOT/CLAUDE_PROJETS/PROJETS/CAT/projet).
set -u

usage() { sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//' >&2; exit 2; }

# ── rejeu dans le pod ──────────────────────────────────────────────────────────────────
if [ "${1:-}" = "--in-pod" ]; then
  shift; IN_POD=1
else
  IN_POD=
  [ $# -ge 2 ] || usage
  if [ "${3:-}" = "--pod" ]; then
    [ -n "${4:-}" ] || usage
    ns=; [ -n "${5:-}" ] && ns="-n $5"
    # shellcheck disable=SC2086
    exec kubectl exec -i $ns "$4" -- sh -s -- --in-pod "$1" "$2" < "$0"
  fi
fi

proj=${1:-}; action=${2:-}
[ -n "$proj" ] && [ -n "$action" ] || usage
case "$action" in question|permission|teammate|fin) ;; *) echo "action inconnue : $action" >&2; usage;; esac
[ -d "$proj" ] || { echo "projet introuvable : $proj" >&2; exit 1; }
proj=$(cd "$proj" && pwd)

# ── tmux : hôte (socket nommé) ou conteneur (serveur par défaut) ───────────────────────────
tm() { if [ -n "$IN_POD" ]; then tmux "$@"; else tmux -L "${TMUX_SOCKET:-${CL_DESK_SOCKET:-claude-desk-ng2}}" "$@"; fi; }

if [ -n "${TMUX_SESSION:-}" ]; then
  sess=$TMUX_SESSION
elif [ -n "$IN_POD" ]; then
  sess=cl
else
  sess=$(tm list-panes -a -F '#{session_name} #{pane_current_path}' 2>/dev/null | while read -r s p; do
    [ "$p" = "$proj" ] && { echo "$s"; break; }
  done)
  if [ -z "$sess" ]; then
    base=$(basename "$proj")
    sess=$(tm list-sessions -F '#{session_name}' 2>/dev/null | while read -r s; do
      case "$s" in p-*"$base") echo "$s"; break;; esac
    done)
  fi
fi
[ -n "$sess" ] || { echo "aucune session tmux pour $proj (serveur ${TMUX_SOCKET:-${CL_DESK_SOCKET:-claude-desk-ng2}}) : le projet est-il ouvert ?" >&2; exit 1; }

# panes de la session, numéros croissants : le premier est le leader (règle du launcher), le suivant un teammate
nums=$(tm list-panes -s -t "=$sess" -F '#{pane_id}' 2>/dev/null | sed -n 's/^%\([0-9][0-9]*\)$/\1/p' | sort -n)
leader=$(printf '%s\n' "$nums" | sed -n 1p)
mate=$(printf '%s\n' "$nums" | sed -n 2p)
[ -n "$leader" ] || { echo "aucun pane dans la session $sess" >&2; exit 1; }

dir="$proj/.claude-launcher/attention"

put() { # put <n° de pane> <charge utile>
  mkdir -p "$dir" && printf '%s' "$2" > "$dir/.$1.tmp" && mv -f "$dir/.$1.tmp" "$dir/$1.json" || { echo "écriture impossible : $dir/$1.json" >&2; exit 1; }
}

Q='{"hook_event_name":"PreToolUse","tool_name":"AskUserQuestion","tool_input":{"questions":[{"question":"Valider le plan ? (attente simulée)","header":"Plan","options":[{"label":"Oui"},{"label":"Non"}]}]}}'
P='{"hook_event_name":"Notification","notification_type":"permission_prompt","message":"Claude a besoin de votre permission pour utiliser Bash (attente simulée)"}'

echo "projet $proj — session tmux $sess — leader %$leader${mate:+, teammate %$mate}"
case "$action" in
  question)   put "$leader" "$Q"; echo "écrit $dir/$leader.json : question du teamleader" ;;
  permission) put "$leader" "$P"; echo "écrit $dir/$leader.json : permission du teamleader" ;;
  teammate)
    [ -n "$mate" ] || { echo "pas de teammate dans la session $sess (un seul pane) : ouvrir un teammate d'abord" >&2; exit 1; }
    put "$mate" "$Q"; echo "écrit $dir/$mate.json : question d'un TEAMMATE (rien ne doit être signalé)" ;;
  fin)
    n=0
    for f in "$dir"/*.json; do [ -e "$f" ] && rm -f "$f" && { echo "retiré $f"; n=$((n + 1)); }; done
    echo "fin d'attente : $n fichier(s) retiré(s)" ;;
esac
exit 0
