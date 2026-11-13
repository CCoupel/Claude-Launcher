#!/usr/bin/env bash
# CI unique de Claude-Launcher (contrat C11 du template) : appelée par
# ng2-tests.yml, ci-branch.yml et release.yml, et en local par qa et deployer.
# Linux : vet, tests, compilation croisée de l'interface native Windows.
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
    go test ./...
    CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -tags desktop,production -trimpath \
      -ldflags "-s -w -H windowsgui" -o "$out/claude-launcher-ng2.exe" .
    ;;
esac
echo "CI OK"
