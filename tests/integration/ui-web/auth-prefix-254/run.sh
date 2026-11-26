#!/usr/bin/env bash
# E2E #254 (addendum 4) : l'interface réelle derrière un reverse proxy À SOUS-CHEMIN (/claude-qualif, jamais retiré) qui pose X-Forwarded-User,
# pilotée par un navigateur sans tête (chrome-headless-shell, protocole CDP). Aucun vrai projet, aucun GitHub : HOME,
# cache et ports factices. Contrat : contracts/auth.md. Usage : tests/integration/ui-web/auth-prefix-254/run.sh
# Code 0 = vert, 1 = rouge, 2 = harnais en erreur (outil manquant, binaire non compilé).
set -u
here="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$here/../../../.." && pwd)"
chrome="${CHROME_HEADLESS:-chrome-headless-shell}"
for tool in "$chrome" node go; do command -v "$tool" >/dev/null || { echo "ERREUR : $tool introuvable"; exit 2; }; done
work="$(mktemp -d /tmp/cl-e2e-254-XXXX)"; pids=""
trap 'for p in $pids; do kill "$p" 2>/dev/null; done; sleep 0.3; rm -rf "$work"' EXIT
free_port() { node -e 'const s=require("net").createServer().listen(0,"127.0.0.1",()=>{console.log(s.address().port);s.close()})'; }
bin="$work/cl"
(cd "$root/ng2" && go build -o "$bin" .) || { echo "ERREUR : compilation du launcher"; exit 2; }
mkdir -p "$work/home" "$work/cache"
lport="$(free_port)"; pport="$(free_port)"; cport="$(free_port)"
ext=launcher.example.org; int=launcher.internal
HOME="$work/home" XDG_STATE_HOME="$work/state" CL_CACHE_DIR="$work/cache" CL_CONFIG_DIR="$work/cfg" \
CL_WEB_ACCESS=proxy CL_WEB_URL="https://$ext/claude-qualif" CL_WEB_ALT_URLS="https://$int" CL_AUTH_ADMINS=ccoupel CL_AUTH_USERS=alice \
CL_AUTH_LOCAL_ADMIN_HOSTS="$int" CL_HEALTH_ADDR="" \
  "$bin" desktop --web --no-open --addr "127.0.0.1:$lport" >"$work/launcher.log" 2>&1 &
pids="$pids $!"
for _ in $(seq 100); do node -e 'fetch("http://127.0.0.1:'"$lport"'/signed-out",{headers:{host:"'"$ext"'"}}).then(r=>process.exit(r.status==200?0:1)).catch(()=>process.exit(1))' && break; sleep 0.2; done || { echo "ERREUR : launcher injoignable"; cat "$work/launcher.log"; exit 2; }
LPORT="$lport" PPORT="$pport" CPORT="$cport" EXT="$ext" INT="$int" CHROME="$chrome" WORK="$work" node "$here/e2e.mjs"
