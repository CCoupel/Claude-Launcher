#!/usr/bin/env bash
# CI unique de Claude-Launcher (contrat C11 du template) : appelée par
# ng2-tests.yml, ci-branch.yml et release.yml, et en local par qa et deployer.
# Linux : vet, tests (-race), audit govulncheck, compilation croisée de l'interface native Windows,
# chart Helm (lint + rendu) et image du launcher du cluster (buildah, deploy/image/build.sh).
# Windows (Git Bash) : couches communes (core), système (platform) et interface
# native (ui/desktop) ; la coque tmux (ui/tmux) n'y tourne pas, elle est
# seulement compilée ; l'exe est construit puis lancé (« version »).
set -euo pipefail
cd "$(dirname "$0")/../ng2"

# #204 : sur une branche milestone/vX.Y.Z, Chart.yaml doit déjà porter la version du milestone (sinon la release
# échoue tard, au tag : « Version du chart = tag » de release.yml). Même contrôle et même message que release.yml ;
# hors branche milestone (main, tag, PR, HEAD détaché) : ignoré.
check_chart_version() {
  local ref=${GITHUB_REF_NAME:-}
  [ -n "$ref" ] || ref=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || true)
  if [[ ! "$ref" =~ ^milestone/v([0-9]+\.[0-9]+\.[0-9]+)$ ]]; then
    echo "chart : contrôle de version ignoré (« ${ref:-?} » n'est pas une branche milestone/vX.Y.Z)"
    return 0
  fi
  local v=${BASH_REMATCH[1]} chart=${CHART_YAML:-../deploy/helm/claude-launcher/Chart.yaml} cv av
  cv=$(sed -nE 's/^version: *"?([^"]+)"?/\1/p' "$chart")
  av=$(sed -nE 's/^appVersion: *"?([^"]+)"?/\1/p' "$chart")
  if [ "$cv" != "$v" ] || [ "$av" != "v$v" ]; then
    echo "ECHEC : Chart.yaml : version=$cv appVersion=$av, attendu $v et v$v (branche $ref)" >&2
    return 1
  fi
  echo "chart : version $v conforme à la branche $ref"
}
check_chart_version

# durée de chaque étape et total, affichés en fin d'exécution (#179 : budget « hors image ≤ 60 s »,
# relevé étape par étape). Horloge : $EPOCHREALTIME (bash ≥ 5, Git Bash compris), en microsecondes.
now_us() { local t=${EPOCHREALTIME/[.,]/}; echo "${t#0}"; }
ci_t0=$(now_us); st_t0=$ci_t0; durations=""
fmt_s() { local ds=$(( ($1 + 50000) / 100000 )); echo "$((ds / 10)),$((ds % 10)) s"; }
step_done() {   # step_done <nom> : clôt l'étape commencée au précédent step_done
  local t; t=$(now_us)
  durations="${durations:+$durations · }$1 $(fmt_s $((t - st_t0)))"; st_t0=$t
}
print_durations() { echo "durées : ${durations:+$durations · }total $(fmt_s $(($(now_us) - ci_t0)))"; }

# syntaxe du JS de l'interface native : une faute de frappe n'apparaît sinon qu'à l'exécution,
# dans la fenêtre (les tests Go ne parsent pas le JS) ; node requis, comme gcc pour -race
if ! command -v node >/dev/null 2>&1; then
  echo "ECHEC : node est requis pour contrôler la syntaxe du JS de l'interface (node --check) :" >&2
  echo "        l'installer (apt install nodejs / winget install OpenJS.NodeJS), ou le déclarer dans .claude-launcher/env.yaml" >&2
  exit 1
fi
for f in ui/desktop/frontend/*.js; do
  node --check "$f" || { echo "ECHEC : erreur de syntaxe dans ng2/$f (node --check)" >&2; exit 1; }
done
step_done "frontend"

out=$(mktemp -d)
# nettoyage des dossiers temporaires : un stockage de conteneurs rootless laissé par un test (podman :
# couches overlay aux droits restreints) résiste à « rm -rf » ; chmod puis, à défaut, « podman unshare ».
# Un échec ne fait pas échouer une CI verte (le dossier est sous /tmp) : il est signalé.
cleanup_tmp() {
  local d
  for d in "$@"; do
    [ -n "$d" ] && [ -e "$d" ] || continue
    chmod -R u+rwX "$d" 2>/dev/null || true
    rm -rf "$d" 2>/dev/null && continue
    if command -v podman >/dev/null 2>&1; then podman unshare rm -rf "$d" 2>/dev/null || true; fi
    rm -rf "$d" 2>/dev/null || echo "⚠ nettoyage incomplet de $d (stockage de conteneurs d'un test ?) : à supprimer à la main" >&2
  done
  return 0
}
tdir=""
# en échec aussi, les durées des étapes passées (l'étape en échec n'y figure pas)
on_exit() { local rc=$?; cleanup_tmp "$out" "$tdir"; [ "$rc" = 0 ] || print_durations >&2; }
trap on_exit EXIT

case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*)
    go vet ./core/... ./platform/... ./ui/desktop/...
    step_done "vet"
    go build ./...
    step_done "build"
    go test ./core/... ./platform/... ./ui/desktop/...
    step_done "tests"
    CGO_ENABLED=0 go build -tags desktop,production -trimpath -ldflags "-s -w -H windowsgui" \
      -o "$out/claude-launcher-ng2.exe" .
    ls -l "$out/claude-launcher-ng2.exe"
    # en ligne de commande, la sortie revient à l'invite (console du parent)
    "$out/claude-launcher-ng2.exe" version | tee "$out/version.txt"
    grep -q 'Claude-Launcher-NG2' "$out/version.txt"
    step_done "exe Windows"
    ;;
  *)
    go vet ./...
    step_done "vet"
    # #186 : la version de tmux du poste/runner est tracée (un test de mise en page instable en CI en dépend peut-être)
    if command -v tmux >/dev/null 2>&1; then tmux -V; else echo "tmux : absent (tests tmux ignorés)"; fi
    # #154 : les tests ne laissent ni verrou git ni socket tmux de test.
    # Dossier dédié sous /tmp en dur (sun_path ≤ 108 ; un TMPDIR dans le projet est trop long, exception de confinement autorisée) pour
    # TMUX_TMPDIR et TMPDIR : tout tmux des tests y place son socket ; les sockets du
    # launcher réel (/tmp/tmux-<uid>) ne sont regardés que par le filet ci-dessous (préfixes de test).
    tdir=$(mktemp -d /tmp/clci.XXXXXX)
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
    # chart et image (#177) : helm et buildah requis sous Linux — jamais de saut silencieux (les tests Go du
    # chart s'ignoreraient sans helm) ; sudo requis hors root pour la couche d'outils de l'image
    # CL_CI_IMAGE=0 (barrière de release.yml seulement) : image non construite ici, la release la construit et la publie
    ci_image=${CL_CI_IMAGE:-1}
    [[ "$ci_image" =~ ^[01]$ ]] || { echo "ECHEC : CL_CI_IMAGE doit valoir 0 ou 1 (reçu « $ci_image »)" >&2; exit 1; }
    tools=(helm); [ "$ci_image" = 1 ] && tools+=(buildah)
    for tool in "${tools[@]}"; do
      command -v "$tool" >/dev/null 2>&1 || { echo "ECHEC : $tool est requis (chart / image du launcher du cluster) :" \
        "le déclarer dans .claude-launcher/env.yaml (outil manquant ; en CI : azure/setup-helm, buildah du runner)" >&2; exit 1; }
    done
    if [ "$ci_image" = 1 ] && [ "$(id -u)" != 0 ] && ! sudo -n true 2>/dev/null; then
      echo "ECHEC : la construction de l'image exige root ou sudo sans mot de passe (couche d'outils de deploy/image/Dockerfile)" >&2
      exit 1
    fi
    # #179 : ui/desktop (≈ 55 s seule) en tranches simultanées, en même temps que les autres paquets.
    # Sous-paquets écartés (tests en boîte blanche, utilitaires partagés), t.Parallel() aussi (t.Setenv
    # presque partout) : sélection par -test.run sur un binaire de test compilé une fois, une tranche =
    # un processus, avec son propre dossier tmux/temporaire sous $tdir (garde des sockets inchangée).
    # Répartition : tous les noms de « go test -list » (Test, Example, Fuzz ; Benchmark non lancés par
    # go test), triés, distribués à tour de rôle ; garde : chaque nom dans exactement une tranche.
    slices=${CI_DESKTOP_SLICES:-4}
    # une erreur d'expansion arithmétique ne fait pas échouer « set -e » (le bloc case est abandonné et la
    # suite s'exécute, « CI OK » compris) : valeur vérifiée avant tout calcul
    [[ "$slices" =~ ^[1-9][0-9]*$ ]] || { echo "ECHEC : CI_DESKTOP_SLICES doit être un entier ≥ 1 (reçu « $slices »)" >&2; exit 1; }
    dpkg=./ui/desktop
    dbin="$tdir/desktop.test"
    CGO_ENABLED=1 go test -race -c -o "$dbin" "$dpkg"
    # liste = celle de « go test -list '.*' » (même binaire, compilé une fois)
    mapfile -t dtests < <(cd "$dpkg" && "$dbin" -test.list '.*' | grep -E '^(Test|Example|Fuzz)' | sort)
    [ "${#dtests[@]}" -gt 0 ] || { echo "ECHEC : aucun test listé dans $dpkg (go test -list)" >&2; exit 1; }
    declare -a sel=()
    for i in "${!dtests[@]}"; do
      k=$((i % slices)); sel[k]="${sel[k]:+${sel[k]}|}${dtests[i]}"
    done
    # garde : union des tranches = liste complète, sans doublon ni oubli
    covered=$(for k in "${!sel[@]}"; do tr '|' '\n' <<<"${sel[k]}"; done | sort)
    if [ "$covered" != "$(printf '%s\n' "${dtests[@]}")" ]; then
      echo "ECHEC : répartition de $dpkg incomplète ou en double (tests hors tranche) :" >&2
      diff <(printf '%s\n' "${dtests[@]}") <(printf '%s\n' "$covered") >&2 || true
      exit 1
    fi
    others=$(go list ./... | grep -vx "claude-launcher-ng2/ui/desktop")
    step_done "compilation tests"
    pids=(); logs=(); names=()
    mkdir -p "$tdir/others"
    CGO_ENABLED=1 TMUX_TMPDIR="$tdir/others" TMPDIR="$tdir/others" go test -race $others >"$tdir/others.log" 2>&1 &
    pids+=($!); logs+=("$tdir/others.log"); names+=("autres paquets")
    for k in "${!sel[@]}"; do
      sd="$tdir/d$k"; mkdir -p "$sd"
      # le binaire tourne dans le dossier du paquet, comme sous go test (fichiers relatifs des tests)
      ( cd "$dpkg" && TMUX_TMPDIR="$sd" TMPDIR="$sd" "$dbin" -test.run "^(${sel[k]})\$" ) >"$tdir/d$k.log" 2>&1 &
      pids+=($!); logs+=("$tdir/d$k.log"); names+=("ui/desktop tranche $((k + 1))/$slices")
    done
    trc=0
    for j in "${!pids[@]}"; do
      if wait "${pids[j]}"; then
        echo "ok   ${names[j]} ($(tail -n 1 "${logs[j]}" | tr -s '\t' ' '))"
      else
        trc=1; echo "FAIL ${names[j]} :" >&2; cat "${logs[j]}" >&2
      fi
    done
    [ "$trc" = 0 ] || { echo "ECHEC : go test -race" >&2; exit 1; }
    step_done "tests ($slices tranches ui/desktop)"
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
    step_done "garde sockets/verrou"
    # audit des dépendances et de la bibliothèque standard (commands.audit)
    go run golang.org/x/vuln/cmd/govulncheck@latest ./...
    step_done "vulnérabilités"
    CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -tags desktop,production -trimpath \
      -ldflags "-s -w -H windowsgui" -o "$out/claude-launcher-ng2.exe" .
    step_done "exe Windows"
    # chart Helm (#170, #173) : lint et rendu des trois jeux de valeurs (minimal, QUALIF, PROD). Les refus
    # (CIDR, TLS, valeurs réservées…) sont contrôlés par les tests Go (ng2/chart_helm_test.go, via helm template :
    # helm lint v4 ne fait pas échouer un « fail » de template). L'émetteur réel n'est pas dans le dépôt : un nom
    # de test est donné au rendu de QUALIF et de PROD.
    chart=../deploy/helm/claude-launcher
    issuer=(--set ingress.tls.clusterIssuer=ci-test)
    # v7.0.0 : l'authentification est activée par défaut (#172) — le rendu exige le Secret de la GitHub App et au moins un
    # administrateur (valeurs de test, jamais de secret) ; le rendu minimal sans Ingress la désactive (sans adresse https, refusée)
    auth=(--set auth.existingSecret=ci-test --set 'auth.admins={ci-admin}')
    minimal=(--set rootBase=/ci --set volume.existingClaim=ci --set ingress.enabled=false --set web.access=token --set auth.enabled=false)
    helm lint --quiet "$chart" "${minimal[@]}" >/dev/null
    helm lint --quiet "$chart" -f ../deploy/helm/values-qualif.yaml "${issuer[@]}" "${auth[@]}" >/dev/null
    helm lint --quiet "$chart" -f ../deploy/helm/values-prod.yaml "${issuer[@]}" "${auth[@]}" >/dev/null
    helm template ci "$chart" "${minimal[@]}" >/dev/null
    helm template ci "$chart" -n claude-qualif -f ../deploy/helm/values-qualif.yaml "${issuer[@]}" "${auth[@]}" >/dev/null
    helm template ci "$chart" -n claude-ng2 -f ../deploy/helm/values-prod.yaml "${issuer[@]}" "${auth[@]}" >/dev/null
    # authentification activée par défaut (aucun auth.enabled posé), avec compte local et GitHub
    helm template ci "$chart" -n claude-ng2 -f ../deploy/helm/values-prod.yaml "${issuer[@]}" "${auth[@]}" --set auth.localAdmin.enabled=true >/dev/null
    # QUALIF avant les applications GitHub : GitHub coupé, compte local seul
    helm template ci "$chart" -n claude-qualif -f ../deploy/helm/values-qualif.yaml "${issuer[@]}" --set auth.github.enabled=false >/dev/null
    # refus attendus : authentification activée sans Secret ni administrateur ; ingress externe sans authentification
    if helm template ci "$chart" -n claude-ng2 -f ../deploy/helm/values-prod.yaml "${issuer[@]}" >/dev/null 2>&1; then
      echo "ECHEC : le chart rend avec auth.enabled sans auth.existingSecret ni auth.admins (refus attendu)" >&2; exit 1
    fi
    if helm template ci "$chart" "${minimal[@]}" --set externalIngress.enabled=true --set externalIngress.className=traefik \
      --set externalIngress.host=ext.ci.test --set externalIngress.tls.clusterIssuer=ci-public >/dev/null 2>&1; then
      echo "ECHEC : le chart rend un ingress externe avec auth.enabled=false (refus attendu)" >&2; exit 1
    fi
    step_done "chart"
    # image du launcher du cluster (#167, décision C3) : même Dockerfile que la release ; buildah sans démon,
    # stockage vfs ; la couche d'outils (RUN apt) exige root → sudo hors root (deploy/image/build.sh).
    # Cache : l'image « ci » est GARDÉE (la supprimer emporterait ses couches intermédiaires, donc le cache) ;
    # seule l'image « ci » du passage précédent est supprimée après la construction (disque borné : une image).
    if [ "$ci_image" = 0 ]; then
      echo "image : NON construite (CL_CI_IMAGE=0 — barrière de release : l'image est construite et publiée par release-ng2-cluster)"
      step_done "image sautée"
    else
    if [ "$(id -u)" = 0 ]; then bah=(buildah); else bah=(sudo buildah); fi
    prev=$("${bah[@]}" --storage-driver vfs images -q localhost/claude-launcher:ci 2>/dev/null || true)
    # appel par bash : ne dépend pas du bit exécutable (perdu si le dépôt est extrait sans les modes de fichier)
    [ -r ../deploy/image/build.sh ] || { echo "ECHEC : deploy/image/build.sh absent ou illisible" >&2; exit 1; }
    (cd .. && OUT_DIR=build/ci bash deploy/image/build.sh ci) >"$out/image.log" 2>&1 || {
      cat "$out/image.log" >&2; echo "ECHEC : construction de l'image (deploy/image/build.sh)" >&2; exit 1; }
    tail -n 1 "$out/image.log"
    cur=$("${bah[@]}" --storage-driver vfs images -q localhost/claude-launcher:ci 2>/dev/null || true)
    if [ -n "$prev" ] && [ "$prev" != "$cur" ]; then "${bah[@]}" --storage-driver vfs rmi "$prev" >/dev/null 2>&1 || true; fi
    rm -rf ../build/ci
    step_done "image"
    fi
    ;;
esac
print_durations
echo "CI OK"
