#!/bin/bash
# Point d'entrée de l'image du launcher du cluster (#167, contracts/launcher-env.md).
# Refuse de démarrer sans CL_ROOT_BASE (la racine partagée doit être explicite : jamais de
# configuration créée dans le HOME éphémère du conteneur), puis remplace le shell par le launcher
# en mode serveur : SIGTERM du kubelet lui parvient directement (arrêt propre).
# Arguments en plus : ajoutés à la commande (ex. pour un diagnostic : entrypoint.sh --addr 0.0.0.0:9000).
set -euo pipefail

case "${1:-}" in
  # sous-commandes utiles en diagnostic (kubectl exec / run) : passées telles quelles
  version|backends|help|--help|-h) exec claude-launcher-ng2 "$@" ;;
esac

if [ -z "${CL_ROOT_BASE:-}" ]; then
  echo "entrypoint : CL_ROOT_BASE non défini — refus de démarrer (racine partagée des projets, ex. /share/MFS/ROOT/CLAUDE_PROJETS ; posée par le chart : rootBase)" >&2
  exit 64
fi
if [ ! -d "$CL_ROOT_BASE" ]; then
  echo "entrypoint : CL_ROOT_BASE=$CL_ROOT_BASE n'est pas un dossier (volume monté ?) — refus de démarrer" >&2
  exit 66
fi
mkdir -p "${HOME:-/home/launcher}" 2>/dev/null || true

# CL_WEB_ADDR : écoute du launcher (posée à 127.0.0.1:8081 par le chart quand le proxy d'authentification est actif, v7.0.0) ;
# défaut : 0.0.0.0:8080, comportement de v6.9.0
args=(desktop --web --addr "${CL_WEB_ADDR:-0.0.0.0:8080}" --no-open)
[ -n "${CL_WEB_URL:-}" ] && args+=(--url "$CL_WEB_URL")
exec claude-launcher-ng2 "${args[@]}" "$@"
