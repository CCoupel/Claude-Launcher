#!/usr/bin/env bash
# Test de reproduction #242 (regression) : bandeau d'attente affiché, le clic sur la ligne N d'un terminal doit
# donner la ligne N. Aucun vrai projet ni claude : l'interface réelle tourne sur des réponses factices.
# Usage : tests/integration/ui-web/clic-bandeau-242/run.sh   (code 0 = vert, 1 = rouge, 2 = harnais en erreur)
set -u
here="$(cd "$(dirname "$0")" && pwd)"
bin="${CHROME_HEADLESS:-chrome-headless-shell}"
command -v "$bin" >/dev/null || { echo "SKIP/ERREUR : $bin introuvable"; exit 2; }
prof="$(mktemp -d /tmp/cl-242-XXXX)"; trap 'rm -rf "$prof"' EXIT
dom="$(timeout 90 "$bin" --no-sandbox --disable-gpu --user-data-dir="$prof" --window-size=1200,800 \
  --allow-file-access-from-files --virtual-time-budget=8000 --dump-dom "file://$here/harness.html" 2>/dev/null)"
line="$(printf '%s' "$dom" | grep -o '<pre id="result">[^<]*' | sed 's/<pre id="result">//; s/&quot;/"/g')"
echo "$line"
case "$line" in VERT*) exit 0;; ROUGE*) exit 1;; *) exit 2;; esac
