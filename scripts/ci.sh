#!/usr/bin/env bash
# CI unique de Claude-Launcher (contrat C11 du template) : appelée par
# ng2-tests.yml, ci-branch.yml et release.yml, et en local par qa et deployer.
# Linux : vet, tests (-race), audit govulncheck, compilation croisée de l'interface native Windows.
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
    # launcher réel (/tmp/tmux-<uid>) ne sont regardés que par le filet ci-dessous (préfixes de test).
    tdir=$(mktemp -d /tmp/clci.XXXXXX)
    trap 'rm -rf "$out" "$tdir"' EXIT
    # verrou : chemin valable aussi en worktree ; un verrou déjà là au départ n'est pas le nôtre
    lock=$(git rev-parse --path-format=absolute --git-path index.lock)
    lock_before=0; [ -e "$lock" ] && lock_before=1
    # filet : un tmux démarré après la suppression de TMUX_TMPDIR retombe sur /tmp/tmux-<uid> ;
    # sockets de TEST seulement (préfixes des tests du dépôt), jamais cl, cl-hidden ni le launcher réel
    defdir="/tmp/tmux-$(id -u)"
    # préfixes produits UNIQUEMENT par les tests (grep des *_test.go) : le socket du launcher réel
    # (deskSocket() : « claude-desk-ng2 », ou CL_DESK_SOCKET de l'utilisateur) n'y correspond pas
    testsocks() { find "$defdir" -maxdepth 1 -type s \( -name 'cl-test-*' -o -name 'cl-e2e-*' -o -name 'cl-ansible-secagent-*' \
      -o -name 'cl-desk-test-*' -o -name 'cl-desk-paste-*' -o -name 'cl-desk-seed-*' -o -name 'cl-desk-size-*' \
      -o -name 'cl-desk-vis-*' -o -name 'cl-desk-r158-*' \) 2>/dev/null | sort || true; }
    defsocks_before=$(testsocks)
    # -race À LA PLACE de go test (une seule passe : le détecteur de courses coûte peu ici, ~+5 %
    # sur la suite, et les courses de production sont de vrais défauts — DEF-1 #158) ; il exige cgo, donc gcc
    if ! command -v "${CC:-gcc}" >/dev/null 2>&1 && ! command -v cc >/dev/null 2>&1; then
      echo "ECHEC : go test -race exige un compilateur C (gcc) : l'installer (apt install gcc / dnf install gcc)," >&2
      echo "        ou le déclarer dans .claude-launcher/env.yaml (outil manquant)" >&2
      exit 1
    fi
    CGO_ENABLED=1 TMUX_TMPDIR="$tdir" TMPDIR="$tdir" go test -race ./...
    socks=$(find "$tdir" -type s 2>/dev/null || true)
    if [ -n "$socks" ]; then
      echo "ECHEC : sockets tmux laissés par les tests (#154) :" >&2
      echo "$socks" >&2
      while read -r sk; do tmux -S "$sk" kill-server 2>/dev/null || true; done <<<"$socks"
      exit 1
    fi
    new_defsocks=$(comm -13 <(printf '%s\n' "$defsocks_before") <(testsocks) | sed '/^$/d')
    if [ -n "$new_defsocks" ]; then
      echo "ECHEC : sockets tmux de test laissés dans $defdir (#154) :" >&2
      echo "$new_defsocks" >&2
      while read -r sk; do tmux -S "$sk" kill-server 2>/dev/null || true; done <<<"$new_defsocks"
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
