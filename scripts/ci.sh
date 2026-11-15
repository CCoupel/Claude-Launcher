#!/usr/bin/env bash
# CI unique de Claude-Launcher (contrat C11 du template) : appelée par
# ng2-tests.yml, ci-branch.yml et release.yml, et en local par qa et deployer.
# Linux : vet, tests, audit govulncheck, compilation croisée de l'interface native Windows.
# Windows (Git Bash) : couches communes (core), système (platform) et interface
# native (ui/desktop) ; la coque tmux (ui/tmux) n'y tourne pas, elle est
# seulement compilée ; l'exe est construit puis lancé (« version »).
set -euo pipefail
cd "$(dirname "$0")/../ng2"

out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT

case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*)
    go vet ./core/... ./platform/... ./ui/desktop/...
    go build ./...
    go test ./core/... ./platform/... ./ui/desktop/...
    CGO_ENABLED=0 go build -tags desktop,production -trimpath -ldflags "-s -w -H windowsgui" \
      -o "$out/claude-launcher-ng2.exe" .
    ls -l "$out/claude-launcher-ng2.exe"
    # en ligne de commande, la sortie revient à l'invite (console du parent)
    "$out/claude-launcher-ng2.exe" version | tee "$out/version.txt"
    grep -q 'Claude-Launcher-NG2' "$out/version.txt"
    ;;
  *)
    go vet ./...
    # #154 : les tests ne laissent ni verrou git ni socket tmux de test.
    # Dossier dédié sous /tmp en dur (sun_path ≤ 108 ; un TMPDIR dans le projet est trop long, exception de confinement autorisée) pour
    # TMUX_TMPDIR et TMPDIR : tout tmux des tests y place son socket ; les sockets du
    # launcher réel (/tmp/tmux-<uid>) ne sont jamais regardés.
    tdir=$(mktemp -d /tmp/clci.XXXXXX)
    trap 'rm -rf "$out" "$tdir"' EXIT
    # verrou : chemin valable aussi en worktree ; un verrou déjà là au départ n'est pas le nôtre
    lock=$(git rev-parse --path-format=absolute --git-path index.lock)
    lock_before=0; [ -e "$lock" ] && lock_before=1
    TMUX_TMPDIR="$tdir" TMPDIR="$tdir" go test ./...
    socks=$(find "$tdir" -type s 2>/dev/null || true)
    if [ -n "$socks" ]; then
      echo "ECHEC : sockets tmux laissés par les tests (#154) :" >&2
      echo "$socks" >&2
      while read -r sk; do tmux -S "$sk" kill-server 2>/dev/null || true; done <<<"$socks"
      exit 1
    fi
    # un git concurrent (launcher, autre terminal) tient le verrou un instant : on lui laisse 3 s ;
    # échec seulement si le verrou, absent au départ, persiste sans aucun processus git vivant
    if [ "$lock_before" = 0 ]; then
      for _ in 1 2 3 4 5 6; do [ -e "$lock" ] || break; sleep 0.5; done
      if [ -e "$lock" ] && ! pgrep -x git >/dev/null 2>&1; then
        echo "ECHEC : $lock laissé par les tests (#154)" >&2
        exit 1
      fi
    fi
    # audit des dépendances et de la bibliothèque standard (commands.audit)
    go run golang.org/x/vuln/cmd/govulncheck@latest ./...
    CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -tags desktop,production -trimpath \
      -ldflags "-s -w -H windowsgui" -o "$out/claude-launcher-ng2.exe" .
    ;;
esac
echo "CI OK"
