# Adaptations projet — deploy PROD (Claude-Launcher)

Racine : `ROOT_BASE=${CL_ROOT_BASE:-/share/MFS/ROOT/CLAUDE_PROJETS}` (`CL_ROOT_BASE` de `prod.env` si posée, #166).

La PROD est le binaire du hub, sur MooseFS : `$ROOT_BASE/claude-launcher-ng2` (Linux)
et `claude-launcher-ng2.exe` (Windows), dans le même dossier.

1. Attendre la CI `release.yml` verte sur le tag, puis télécharger les binaires de la release GitHub
   (`gh release download vX.Y.Z`) et les vérifier contre `SHA256SUMS`.
2. Sauvegarder le binaire Linux en place : `claude-launcher-ng2.bak-v<version précédente>` (un seul `.bak` gardé).
3. Remplacer `claude-launcher-ng2` (Linux amd64, chmod +x) et `claude-launcher-ng2.exe`.
4. Les processus `mirror` / `layout-watch` gardent l'ancien binaire : `pkill -f 'claude-launcher-ng2 mirror'` après
   la mise à jour, puis demander à l'utilisateur de relancer le hub (`Esc` puis `l`).
5. Clôturer le milestone `vX.Y.Z` avec le tag ; proposer une maquette du site `gh-pages` si le site est en retard.

## Launcher du cluster (v6.3.0) — PROD dans `claude-ng2`

Réglages actuels inchangés : racine `/share/MFS/ROOT/CLAUDE_PROJETS`, `backends.yaml` réel (backend `k8s` avec `context: k8s`,
`namespace: claude-ng2`) — en cluster le contexte est ignoré et le ServiceAccount du pod est utilisé : le launcher du cluster
pilote **les mêmes pods de projets** que le hub (prise de main comme entre deux postes).

> **En cluster, le contexte de TOUS les backends `k8s` est ignoré** (ServiceAccount du pod, namespace déclaré) : un backend
> `k8s` d'un **autre cluster** déclaré dans le `backends.yaml` de cette racine agirait sur le cluster du pod (dans le namespace
> déclaré, si le Role le permet). Ne déclarer dans ce fichier que des backends `k8s` du cluster où tourne le launcher.

**Prérequis de la première release (une seule fois)**

- **P3 — paquets ghcr publics, par l'utilisateur** : à la première release, la CI crée les paquets
  `ghcr.io/ccoupel/claude-launcher` et `ghcr.io/ccoupel/charts/claude-launcher`, **privés** par défaut. L'utilisateur les rend
  publics une fois : GitHub → profil → *Packages* → chaque paquet → *Package settings* → *Change visibility* → *Public*.
  Le paquet de **QUALIF** `ghcr.io/ccoupel/claude-launcher-qualif` (publié depuis le poste, `deploy.qualif.md`) aussi, à sa première
  publication. Séparation : la CI de release n'écrit que `claude-launcher` (PROD) ; la QUALIF n'écrit que `claude-launcher-qualif`.
  Sans cela le cluster ne peut pas tirer l'image (aucun secret de tirage prévu) ni `helm` lire le chart.
  Contrôle (sans jeton) : `curl -s "https://ghcr.io/token?scope=repository:ccoupel/claude-launcher:pull"` rend un `token`
  (et non `DENIED`) ; idem avec `ccoupel/charts/claude-launcher`.
- Compte de déploiement : `PRIVATE/PROD/kubeconfig`, administrateur du cluster (**risque accepté** le 2026-11-17, voir
  `deploy.qualif.md`) ; émetteur `claude-launcher-ca` (`PRIVATE/PROD/values-local.yaml`) ; nom mDNS `launcher.local` ; fiche administrateur
  (`_work/reports/infra-admin-noeuds-20261117-090000.md`).

**Certificats** — tous émis et renouvelés par cert-manager

Aucun certificat n'est généré ni signé à la main : `openssl` ne sert qu'à **lire** ou **vérifier** (`x509 -noout`, `verify`).
- **CA `claude-launcher-ca`** : `Certificate` cert-manager (`isCA`, `nameConstraints` critiques : `launcher.local`, `launcher-qualif.local`)
  amorcé par l'Issuer `selfSigned`, exposé par le **ClusterIssuer `claude-launcher-ca`** (`deploy/cert-manager/claude-launcher-ca.yaml`).
  Renouvelé par cert-manager 30 jours avant l'échéance (2036), avec **la même clé** (`rotationPolicy: Never`) : les postes qui font
  confiance à la CA n'ont rien à refaire. Seul le certificat **public** est exporté (`PRIVATE/GLOBAL/claude-launcher-ca.crt`), jamais la clé.
- **Certificat de l'Ingress** : `Certificate` créé par le chart (`templates/certificate.yaml`, `issuerRef: ClusterIssuer claude-launcher-ca`
  via `PRIVATE/PROD/values-local.yaml`) ; émis et renouvelé automatiquement par cert-manager (durée par défaut 90 jours, renouvelé aux 2/3).
  Contrôle : `kubectl -n claude-ng2 get certificate` → `READY True`.
- **Certificats d'essai** éventuels : toujours des `Certificate` cert-manager (supprimés après l'essai), jamais `openssl req`/`x509 -signkey`.

1. Image et chart de release : construits et publiés par la CI sur le tag (travail `release-ng2-cluster` de `release.yml`) :
   image `ghcr.io/ccoupel/claude-launcher:vX.Y.Z` (amd64 + arm64, empreinte jointe à la release :
   `claude-launcher-image-vX.Y.Z.digest`), chart `oci://ghcr.io/ccoupel/charts/claude-launcher` version `X.Y.Z` et archive
   `claude-launcher-X.Y.Z.tgz` jointe. Prérequis à la 1re release : paquets ghcr **publics** (P3, ci-dessus). Contrôle :
   `gh release view vX.Y.Z` liste le `.tgz` et le `.digest`.
2. Installation **par empreinte**, après **contrôle du rendu** (comme la QUALIF). L'empreinte vient de la pièce jointe
   `claude-launcher-image-vX.Y.Z.digest` de la release (écrite par la CI, `release-ng2-cluster`) ; le chart est celui **publié** au tag
   (`oci://ghcr.io/ccoupel/charts/claude-launcher`, version `X.Y.Z`) ; émetteur `claude-launcher-ca` par `PRIVATE/PROD/values-local.yaml`
   (le dépôt porte `a-renseigner`, refusé par le chart). Arrêt si l'image rendue n'est pas exactement
   `ghcr.io/ccoupel/claude-launcher:vX.Y.Z@<empreinte>` (jamais `-qualif`) ou si le namespace n'est pas `claude-ng2` :
   ```bash
   (
     set -euo pipefail
     . .claude/agents/environments/prod.env        # PROD_NAMESPACE, PROD_KUBECONFIG, PROD_VALUES_LOCAL
     V=vX.Y.Z; T=$(mktemp -d)
     trap 'rm -rf "$T"' EXIT
     [ "$PROD_NAMESPACE" = claude-ng2 ] || { echo "ARRÊT : namespace de PROD « $PROD_NAMESPACE » ≠ claude-ng2" >&2; exit 1; }
     gh release download "$V" -p "claude-launcher-image-$V.digest" -D "$T"
     ref=$(cat "$T/claude-launcher-image-$V.digest")
     case "$ref" in "ghcr.io/ccoupel/claude-launcher:$V@sha256:"*) ;; *) echo "ARRÊT : empreinte de release inattendue « $ref »" >&2; exit 1 ;; esac
     D=${ref#*@}
     [[ "$D" =~ ^sha256:[0-9a-f]{64}$ ]] || { echo "ARRÊT : empreinte illisible « $D »" >&2; exit 1; }
     ARGS=(claude-launcher oci://ghcr.io/ccoupel/charts/claude-launcher --version "${V#v}" -n "$PROD_NAMESPACE"
           -f deploy/helm/values-prod.yaml -f "$PROD_VALUES_LOCAL" --set "image.tag=$V" --set "image.digest=$D")
     img=$(helm template "${ARGS[@]}" | sed -nE 's/^ +image: "?([^"]+)"?$/\1/p' | sort -u)
     [ "$img" = "ghcr.io/ccoupel/claude-launcher:$V@$D" ] || { echo "ARRÊT : image rendue « $img »" >&2; exit 1; }
     case "$img" in *-qualif*) echo "ARRÊT : image de QUALIF en PROD" >&2; exit 1 ;; esac
     KUBECONFIG=$PROD_KUBECONFIG helm upgrade --install "${ARGS[@]}"
     KUBECONFIG=$PROD_KUBECONFIG kubectl -n "$PROD_NAMESPACE" rollout status statefulset/claude-launcher --timeout=5m
   )
   ```
3. Accès : **`web.access: none`** (valeur de PROD) → `https://launcher.local/` directement, sans jeton, depuis `192.168.1.0/24` ; l'Ingress
   est la seule barrière (pas de NetworkPolicy, risque accepté jusqu'à v6.4.0) → test négatif de la liste d'adresses obligatoire à la
   première installation. Seulement si `web.access: token`, URL d'amorçage — jeton jamais affiché :
   `( umask 077; . .claude/agents/environments/prod.env; U=$(mktemp -p /dev/shm claude-launcher-prod-url-XXXXXX); t=$(KUBECONFIG=$PROD_KUBECONFIG kubectl -n claude-ng2 exec statefulset/claude-launcher -- cat /share/MFS/ROOT/CLAUDE_PROJETS/.cache/claude-launcher-ng2/cluster/desktop-web-token); printf 'https://launcher.local/?t=%s\n' "$t" > "$U"; unset t; ( sleep 600; rm -f "$U" ) >/dev/null 2>&1 & echo "URL : $U (cat par l'utilisateur, effacée dans 10 min)" )`
   — fichier en mémoire (tmpfs), jamais sur le partage ni affiché par un agent.
4. Contrôles à la première installation : test négatif de la liste d'adresses (comme en QUALIF, avec `launcher.local`) ;
   prise de main hub ↔ cluster sur un projet réel, faite par l'utilisateur.
5. Mise à jour : rejouer l'étape 2 avec le nouveau `vX.Y.Z` (nouvelle empreinte) — l'ancien pod s'arrête avant le démarrage du nouveau.

## Authentification (v7.0.0, #172, #254) — avant et après `helm upgrade`

Contrat : `contracts/helm-values.md` (« Valeurs `auth.*` », `externalIngress.*`), `contracts/auth.md`. Valeurs de PROD : `PRIVATE/PROD/values-local.yaml`
(auth GitHub + compte local interne, `externalIngress` sur `coupel.net` + `pathPrefix: /claude`, sans certificat : celui de `coupel.net` par SNI).
Release `claude-launcher` (namespace `claude-ng2`), révision courante **9** (v6.9.0).

1. **Même GitHub App que QUALIF** : fichier source `PRIVATE/GLOBAL/github-app.txt` ; `PRIVATE/GLOBAL/github-client-id|secret` (600, sans fin de ligne) ;
   paire validée (`access_token` + `code=invalid` → `bad_verification_code`, corps `client_id=…&client_secret=…&code=invalid` joint par `&`, `--data @fichier`).
   Copies identiques par environnement (`PRIVATE/QUALIF/`) conservées : chaque procédure lit son dossier ; en cas de rotation du secret, mettre à jour GLOBAL, QUALIF et les deux Secrets.
2. **Secret `claude-launcher-github` — créé AVANT `helm upgrade`** (fait le 2026-11-26 ; ne coupe pas la v6.9.0) :
   `kubectl -n claude-ng2 create secret generic claude-launcher-github --from-file=client-id=PRIVATE/GLOBAL/github-client-id --from-file=client-secret=PRIVATE/GLOBAL/github-client-secret`
   (nom = `<release>-github`, clés `client-id`/`client-secret` ; le `cookie-secret` est généré par le chart). Contrôle : `kubectl -n claude-ng2 get secret claude-launcher-github -o json` (longueurs 20 / 40, jamais les valeurs).
3. **Callbacks de la GitHub App** (à enregistrer par l'utilisateur, en plus de QUALIF) : `https://launcher.local/oauth2/callback` et `https://coupel.net/claude/oauth2/callback`.
   `auth.admins: [CCoupel]` : casse exacte de GitHub (proxy sensible à la casse ; `curl -s https://api.github.com/users/<login>` → `login`).
4. **Chemins de `coupel.net`** : `/claude` (Exact) et `/claude/` (Prefix) ne chevauchent aucun Ingress existant (`/emby /radarr /sonarr /lidarr /prowlarr /chaptarr /ombi /seerr /automation-factory`) ;
   `/claude-qualif` (QUALIF) n'est pas capturé (Exact + Prefix par segment). Certificat par SNI : contrôler `curl -vI https://coupel.net/claude/` (Let's Encrypt, pas le certificat par défaut de Traefik).
5. **Mot de passe `admin@local`** (Secret `claude-launcher-local-admin` généré par le chart ; interne seulement) : lu à la demande dans un fichier tmpfs pour l'utilisateur, jamais affiché ni journalisé :
   `( umask 077; . .claude/agents/environments/prod.env; U=$(mktemp -p /dev/shm claude-launcher-prod-admin-XXXXXX); KUBECONFIG=$PROD_KUBECONFIG kubectl -n claude-ng2 get secret claude-launcher-local-admin -o jsonpath='{.data.password}' | base64 -d > "$U"; ( sleep 600; rm -f "$U" ) >/dev/null 2>&1 & echo "$U (cat par l'utilisateur, effacé dans 10 min)" )`
   (nom de clé à vérifier dans `contracts/auth.md` §8).
6. **Contrôles après upgrade** : rollout ; `/healthz` interne au pod (`kubectl -n claude-ng2 exec statefulset/claude-launcher -c launcher -- curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8082/healthz` → `200`) ;
   sans identité, `https://launcher.local/` et `https://coupel.net/claude/` ne répondent que la connexion (`401` sur `/api/`, `302` ailleurs) ; connexion GitHub réelle par l'utilisateur ; pods de projets en marche conservés (P6, signalés sur les cartes, recréés à la réouverture) ; sessions claude en cours **non coupées**.
7. **Retour arrière** : `helm rollback claude-launcher 9 -n claude-ng2` (v6.9.0, sans authentification) ; le Secret `claude-launcher-github` est inoffensif et peut rester. Un `helm upgrade` jamais avec `--reuse-values`.
8. **Ingress externe** : ACTIVÉ en PROD v7.0.0 (décision utilisateur, `coupel.net` + `/claude`, risque d'origine partagée accepté) ; contrôler d'abord le test externe `Host: launcher.local` hors LAN (403 attendu) tant que la v6.9.0 tourne.
9. Sidecar figé par empreinte : `quay.io/oauth2-proxy/oauth2-proxy:v7.15.6@sha256:c3415e8660af553f5e18f5d90db050614c02493c0ac6260ac59442f6d9b12048` ; contrôler l'image rendue du sidecar comme celle du launcher (étape 2 ci-dessus).

## `helm upgrade` — jamais `--reuse-values` (constaté en QUALIF 6.4.0.12, vital pour la PROD v6.4.0)

`--reuse-values` n'applique pas les défauts du chart de la nouvelle version : une clé **nouvelle** (ex. `externalIngress.enabled`,
`networkPolicy.*`, `secrets.persistence.*` en v6.4.0) est absente et le rendu échoue (« nil pointer evaluating interface {}.enabled »,
rien n'est appliqué). Le passage en PROD change le chart : utiliser la procédure ci-dessus (`-f values-prod.yaml -f $PROD_VALUES_LOCAL`,
sans `--reuse-values`), ou pour un upgrade qui garde les valeurs courantes `--reset-then-reuse-values` (retrait d'une valeur :
`--set <clé>=null`). Contrôler d'abord : `helm upgrade … --dry-run=client`. Pour la v6.4.0 en PROD, noter aussi : NetworkPolicy active par
défaut (admet `traefik`), PVC du coffre `<release>-secrets` créé, ingress externe refusé au rendu.
