#!/usr/bin/env bash
# Claude-Launcher v4 — redirection de l'ancienne commande vers Claude-Launcher-NG.
#
# Depuis la v4, tout le launcher est dans le binaire Go `claude-launcher-ng`
# (navigateur, mise en page des teams, vignettes Teamleaders). L'ancien menu fzf
# (--legacy) n'existe plus. Ce script ne garde que les anciens points d'entrée :
#   ~/claude-launcher.sh [--no-update]     → claude-launcher-ng shell
#   ~/claude-launcher.sh --configure       → claude-launcher-ng configure
#   ~/claude-launcher.sh --relayout S WIN  → claude-launcher-ng relayout WIN
#     (liaison tmux « bind R » d'un ~/.tmux.conf existant)
# Il est tenu à jour par la mise à jour automatique du NG.

SCRIPT_VERSION="v4.0.0"

ng="${CLAUDE_LAUNCHER_NG:-$(command -v claude-launcher-ng 2>/dev/null)}"
[[ -z "$ng" && -x "$HOME/.local/bin/claude-launcher-ng" ]] && ng="$HOME/.local/bin/claude-launcher-ng"
if [[ -z "$ng" || ! -x "$ng" ]]; then
  echo "claude-launcher-ng introuvable : voir https://github.com/CCoupel/Claude-Launcher#installation" >&2
  exit 1
fi

case "${1:-}" in
  ""|--no-update) exec "$ng" shell "$@" ;;
  --configure)    exec "$ng" configure ;;
  --relayout)     exec "$ng" relayout "$3" ;;
  # vignettes et watchers lancés par une v3 encore en cours d'exécution
  --mirror)       exec "$ng" mirror "$2" "$3" ;;
  --layout-watch) exec "$ng" layout-watch "$3" "$4" "$5" ;;
  *)
    echo "claude-launcher.sh $1 : supprimé en v4 (l'ancien menu n'existe plus)." >&2
    echo "Utiliser : claude-launcher-ng [shell|configure|relayout WIN_ID|version]" >&2
    exit 1 ;;
esac
