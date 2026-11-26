# Adaptations projet — deploy QUALIF (Claude-Launcher)

QUALIF = binaires de test pour les essais réels de l'utilisateur, déposés à côté du binaire du hub, **sans le remplacer**.

- Compiler depuis le candidat : Linux `claude-launcher-ng2-<X.Y.Z.a>` et Windows `claude-launcher-ng2-<X.Y.Z.a>.exe`
  (`-tags desktop,production -H windowsgui`, voir `scripts/ci.sh`).
- Racine : `ROOT_BASE=${CL_ROOT_BASE:-/share/MFS/ROOT/CLAUDE_PROJETS}` (`CL_ROOT_BASE` de `qualif.env` si posée, #166).
- Déposer dans `$ROOT_BASE/` (cible déclarée), jamais par-dessus `claude-launcher-ng2` ni `.exe`.
- Donner à l'utilisateur les chemins exacts et ce qu'il doit tester ; la validation se fait sur ses essais Windows/Linux.
- Après la release PROD, supprimer les binaires de test QUALIF de ce dossier (garder seulement les binaires de prod et le `.bak`).

## Launcher du cluster (v6.3.0) — QUALIF dans `claude-qualif`, racine dédiée

Décision utilisateur (2026-11-17) : la QUALIF du cluster ne touche **jamais** la racine de PROD.

| | QUALIF | PROD |
|---|---|---|
| Namespace | `claude-qualif` | `claude-ng2` |
| Racine (`rootBase`) | `/share/MFS/ROOT/CLAUDE-QUALIF` | `/share/MFS/ROOT/CLAUDE_PROJETS` |
| Volume | PVC `claude-projects-qualif` **créé par le chart** (`volume.create`, conservé à la désinstallation), lié au PV `claude-qualif-pv` créé par l'administrateur ; ne monte que la racine de QUALIF | PVC `claude-projets` existant |
| `backends.yaml` | celui de la racine de QUALIF (ci-dessous) | le réel, inchangé |
| Nom de hub | `cluster-qualif` | `cluster` |
| Nom d'accès | `https://launcher-qualif.local` (mDNS, domaine `.local`) | `https://launcher.local` |
| Compte helm | `PRIVATE/QUALIF/kubeconfig` (administrateur, risque accepté) | `PRIVATE/PROD/kubeconfig` (idem) |
| Émetteur | `claude-launcher-ca` (CA interne, `PRIVATE/QUALIF/values-local.yaml`) | idem (`PRIVATE/PROD/values-local.yaml`) |

> **En cluster, le contexte de TOUS les backends `k8s` est ignoré** (ServiceAccount du pod, namespace déclaré) : un backend
> `k8s` d'un **autre cluster** déclaré dans le `backends.yaml` de cette racine agirait sur le cluster du pod (dans le namespace
> déclaré, si le Role le permet). Ne déclarer dans ce fichier que des backends `k8s` du cluster où tourne le launcher.

> **Risque accepté (décision utilisateur du 2026-11-17)** : le compte de déploiement est le kubeconfig Rancher **administrateur du
> cluster** déposé dans `PRIVATE/<ENV>/kubeconfig` (le même pour QUALIF et PROD). `PRIVATE/` est sur la racine de PROD, montée dans
> tous les pods de projets (root) : un projet peut lire ce fichier et administrer le cluster. Les comptes `ng2-deploy` limités
> (`.claude-launcher/k8s-acces-deploy.sh`) restent disponibles pour réduire ce risque.

### Racine de QUALIF (créée une fois, puis réutilisée)

Prérequis : dossier `/share/MFS/ROOT/CLAUDE-QUALIF` et namespace `claude-qualif` (créés par l'utilisateur, PSA `baseline`), propriétaire `1000:1000` ;
paquet ghcr `claude-launcher-qualif` public (P3, une fois) ;
PV `claude-qualif-pv` vers ce dossier (administrateur, fiche).

```
/share/MFS/ROOT/CLAUDE-QUALIF/
├── .config/claude-launcher-ng2/backends.yaml   # écrit par deployer (ci-dessous) ; config.conf créé au 1er démarrage
└── PROJETS/QUALIF/factice-1, factice-2         # projets factices (git init + faux claude), jamais de vrai projet
```

`backends.yaml` de QUALIF — mêmes **types** que la PROD, chemins et namespace de QUALIF :

```yaml
defaults:
  backend: k8s
  image: docker.io/library/ubuntu:26.04
images: [docker.io/library/ubuntu:24.04, docker.io/library/ubuntu:26.04]
backends:
  podman: {type: podman}
  srv1-docker: {type: docker, host: 'tcp://192.168.1.211:2375', shared: [/share/MFS/ROOT/CLAUDE-QUALIF]}
  # même forme que la PROD (context: k8s) : en cluster le contexte est ignoré (tous les backends k8s), ServiceAccount du pod
  k8s: {type: k8s, context: k8s, namespace: claude-qualif, volumes: {/share/MFS/ROOT/CLAUDE-QUALIF: claude-projects-qualif}}
```

Projets factices : `git init` ; `.claude-launcher/env.yaml` avec une étape `setup` qui pose un faux `claude` dans
`~/.local/bin` (script qui affiche une invite et lit l'entrée) — jamais de vraie session claude.

### Certificats — tous émis et renouvelés par cert-manager

Aucun certificat n'est généré ni signé à la main : `openssl` ne sert qu'à **lire** ou **vérifier** (`x509 -noout`, `verify`).
- **CA `claude-launcher-ca`** : `Certificate` cert-manager (`isCA`, `nameConstraints` critiques : `launcher.local`, `launcher-qualif.local`)
  amorcé par l'Issuer `selfSigned`, exposé par le **ClusterIssuer `claude-launcher-ca`** (`deploy/cert-manager/claude-launcher-ca.yaml`).
  Renouvelé par cert-manager 30 jours avant l'échéance (2036), avec **la même clé** (`rotationPolicy: Never`) : les postes qui font
  confiance à la CA n'ont rien à refaire. Seul le certificat **public** est exporté (`PRIVATE/GLOBAL/claude-launcher-ca.crt`), jamais la clé.
- **Certificat de l'Ingress** : `Certificate` créé par le chart (`templates/certificate.yaml`, `issuerRef: ClusterIssuer claude-launcher-ca`
  via `PRIVATE/<ENV>/values-local.yaml`) ; émis et renouvelé automatiquement par cert-manager (durée par défaut 90 jours, renouvelé aux 2/3).
  Contrôle : `kubectl -n claude-qualif get certificate` → `READY True`.
- **Certificats d'essai** éventuels : toujours des `Certificate` cert-manager (supprimés après l'essai), jamais `openssl req`/`x509 -signkey`.

### Installation et contrôles

1. Sauvegarde : rien à sauvegarder côté PROD (aucun fichier de PROD n'est lu ni écrit). Le PVC est créé par l'installation
   (étape 3) ; après elle : `kubectl -n claude-qualif get pvc claude-projects-qualif` → `Bound` RWX sur `claude-qualif-pv`.
2. Image candidate `v<X.Y.Z.a>` (tag **avec le « v »**, exigé par `build.sh`) construite **depuis le binaire candidat de BUILD**
   (BORE : aucun recompilage, sa version est contrôlée), puis publiée dans le paquet ghcr **de QUALIF**
   `ghcr.io/ccoupel/claude-launcher-qualif` — **jamais** dans `ghcr.io/ccoupel/claude-launcher` (PROD, écrit seulement par la CI au tag).
   Bloc à lancer d'un seul tenant (sous-shell : `set -e`, garde avant envoi, identifiants en tmpfs, déconnexion et suppression même en cas
   d'échec ; post-condition : aucun `PRIVATE/QUALIF/.ghcr-auth.json`) :
   ```bash
   (
     set -euo pipefail
     . .claude/agents/environments/qualif.env      # QUALIF_IMAGE, QUALIF_KUBECONFIG, QUALIF_VALUES_LOCAL
     V=v<X.Y.Z.a>; C=build/candidate_v<X.Y.Z>
     REF="$QUALIF_IMAGE:$V"
     # garde : jamais le paquet de PROD
     case "$REF" in ghcr.io/ccoupel/claude-launcher-qualif:*) ;; *) echo "ARRÊT : $REF n'est pas dans ghcr.io/ccoupel/claude-launcher-qualif" >&2; exit 1 ;; esac
     OUT_DIR=$C SOURCE_BIN=$C/claude-launcher-ng2-${V#v} OCI_ARCHIVE=1 deploy/image/build.sh "$V"
     # identifiants ghcr en mémoire (tmpfs /dev/shm), JAMAIS sur le partage ; déconnexion + suppression même en échec
     A=$(mktemp -p /dev/shm claude-launcher-ghcr-XXXXXX.json) && echo '{}' > "$A"   # mktemp crée un fichier vide : JSON invalide pour buildah push
     printf '{}' > "$A"                           # buildah exige un JSON valide, pas un fichier vide
     trap 'sudo buildah logout --authfile "$A" ghcr.io >/dev/null 2>&1; sudo rm -f "$A"; test ! -e PRIVATE/QUALIF/.ghcr-auth.json' EXIT
     (umask 077; gh auth token | sudo buildah login --authfile "$A" -u "$(gh api user --jq .login)" --password-stdin ghcr.io)
     sudo buildah --storage-driver vfs push --authfile "$A" --digestfile "$C/digest-$V" "localhost/claude-launcher:$V" "docker://$REF"
     sudo chown "$(id -u):$(id -g)" "$C/digest-$V"
     grep -qE '^sha256:[0-9a-f]{64}$' "$C/digest-$V" || { echo "ARRÊT : empreinte illisible ($C/digest-$V)" >&2; exit 1; }
     echo "publié : $REF@$(cat "$C/digest-$V")"
   )
   ```
   Contrôle : `gh api /users/CCoupel/packages/container/claude-launcher-qualif/versions --jq '.[].metadata.container.tags[]'` contient
   `v<X.Y.Z.a>`. Première publication : le paquet `claude-launcher-qualif` est créé **privé** → le rendre **public** une fois (P3, comme la PROD :
   aucun secret de tirage). Tirage sans jeton : `curl -s "https://ghcr.io/token?scope=repository:ccoupel/claude-launcher-qualif:pull"` rend un `token`.
3. Installation, **après contrôle du rendu** (l'image rendue doit être `ghcr.io/ccoupel/claude-launcher-qualif:<V>@sha256:<empreinte publiée>`,
   sinon arrêt). Émetteur `claude-launcher-ca` donné par `PRIVATE/QUALIF/values-local.yaml` (le dépôt porte `a-renseigner`, refusé par le chart) :
   ```bash
   (
     set -euo pipefail
     . .claude/agents/environments/qualif.env
     V=v<X.Y.Z.a>; C=build/candidate_v<X.Y.Z>; D=$(cat "$C/digest-$V")
     ARGS=(claude-launcher-qualif deploy/helm/claude-launcher -n claude-qualif -f deploy/helm/values-qualif.yaml
           -f "$QUALIF_VALUES_LOCAL" --set "image.tag=$V" --set "image.digest=$D")
     img=$(helm template "${ARGS[@]}" | sed -nE 's/^ +image: "?([^"]+)"?$/\1/p' | sort -u)
     [ "$img" = "ghcr.io/ccoupel/claude-launcher-qualif:$V@$D" ] || { echo "ARRÊT : image rendue « $img »" >&2; exit 1; }
     KUBECONFIG=$QUALIF_KUBECONFIG helm upgrade --install "${ARGS[@]}"
     KUBECONFIG=$QUALIF_KUBECONFIG kubectl -n claude-qualif rollout status statefulset/claude-launcher-qualif --timeout=5m
   )
   ```
4. Accès à l'interface (v7.0.0) : avec `auth.enabled: true` (défaut du chart), **`web.access` est imposé à `proxy` — ne pas le poser**.
   `https://launcher-qualif.local/` (depuis une adresse de `192.168.1.0/24`) affiche la connexion (GitHub, ou compte local
   `admin@local` à l'étape 2 ci-dessous), jamais l'interface sans identité. Barrières : liste d'adresses de l'Ingress + authentification
   oauth2-proxy. **NetworkPolicy présente** (valeurs par défaut du chart : `networkPolicy.enabled: true`, entrée depuis le namespace
   `traefik` et les pods `app.kubernetes.io/name: traefik`, `ingressCIDRs: []`) ; le test négatif H3 (étape 7) reste **obligatoire** avant validation
   (si Traefik est en réseau d'hôte, les adresses des nœuds doivent figurer dans `networkPolicy.ingressCIDRs`).
   Contrôle : `kubectl -n claude-qualif exec statefulset/claude-launcher-qualif -- printenv CL_WEB_ACCESS` → `proxy`.
   Seulement si `auth.enabled: false` (`web.access: token`, non utilisé en QUALIF) — URL d'amorçage, **jeton jamais affiché ni écrit sur le partage** : lu dans le pod, écrit dans un fichier en mémoire
   (tmpfs `/dev/shm`, 600) que **l'utilisateur** ouvre dans son terminal ; le fichier s'efface seul au bout de 10 min :
   ```bash
   ( umask 077; . .claude/agents/environments/qualif.env
     U=$(mktemp -p /dev/shm claude-launcher-qualif-url-XXXXXX)
     t=$(KUBECONFIG=$QUALIF_KUBECONFIG kubectl -n claude-qualif exec statefulset/claude-launcher-qualif -- \
           cat /share/MFS/ROOT/CLAUDE-QUALIF/.cache/claude-launcher-ng2/cluster-qualif/desktop-web-token)
     printf 'https://launcher-qualif.local/?t=%s\n' "$t" > "$U"; unset t
     ( sleep 600; rm -f "$U" ) >/dev/null 2>&1 &
     echo "URL d'amorçage : $U (à ouvrir par l'utilisateur : cat $U ; effacée dans 10 min)" )
   ```
   Les agents ne lisent jamais ce fichier ; l'utilisateur peut l'effacer dès l'ouverture (`rm -f /dev/shm/claude-launcher-qualif-url-*`).
5. Diagnostic du pod : backend `k8s` → contexte ignoré, chaque droit OK dans `claude-qualif` ; ouvrir/fermer/rouvrir un factice
   (StatefulSet dans `claude-qualif` uniquement).
6. Image désignée par son **empreinte** (étape 3) : l'ancien risque H2 (tag seul, registre HTTP interne) ne s'applique plus.
7. **Contrôle de la liste d'adresses (H3) — obligatoire avant de déclarer l'Ingress « réservé au réseau interne »** :
   1. `kubectl -n kube-system get svc -o custom-columns=NOM:.metadata.name,POLITIQUE:.spec.externalTrafficPolicy | grep -i traefik`
      → attendu `Local` ; `Cluster` : Traefik voit l'adresse d'un nœud, la liste est inopérante → le signaler, ne pas valider.
   2. Depuis un poste de `192.168.1.0/24` : `curl -sk -o /dev/null -w '%{http_code}' https://launcher-qualif.local/healthz` → `200`.
   3. Depuis une adresse **hors** de `192.168.1.0/24` (autre réseau, VPN, partage de connexion) : même commande → `403`.
   4. Depuis un pod du cluster passant par l'Ingress → `403` (adresse de pod 10.42.x hors de la liste). Les pods ne résolvent pas
      les noms `.local` (pas de mDNS) : `kubectl -n claude-qualif exec <pod> -- curl -sk --resolve launcher-qualif.local:443:<IP de
      Traefik> https://launcher-qualif.local/healthz` (IP : `getent hosts launcher-qualif.local` sur un poste).
   5. `curl -sk -H 'X-Forwarded-For: 192.168.1.10' …` depuis une adresse hors liste → `403` (pas d'`ipStrategy` : Traefik juge
      l'adresse de la connexion, pas l'en-tête).
   Consigner les résultats dans le rapport de QUALIF.
8. Après validation : `helm uninstall claude-launcher-qualif -n claude-qualif` (le PVC `claude-projects-qualif` est **conservé** :
   `helm.sh/resource-policy: keep`), fermer les factices
   (`kubectl -n claude-qualif delete sts -l cl.launcher=ng2,cl.backend=k8s`). La racine de QUALIF et son `backends.yaml` sont
   **gardés** pour la QUALIF suivante ; seul le dossier d'état `…/.cache/claude-launcher-ng2/cluster-qualif` (jeton) est supprimé.

## Authentification (v7.0.0, #172) — avant et après `helm upgrade`

Contrat : `contracts/helm-values.md` (« Valeurs `auth.*` »), `contracts/auth.md`. Valeurs de QUALIF dans `PRIVATE/QUALIF/values-local.yaml`
(`auth.enabled: true` ; `auth.github.enabled: true` — défaut du chart, GitHub App de QUALIF appairée le 2026-11-26, Secret créé à l'étape 1 ;
compte local `admin@local` maintenu en secours). Valeurs réelles de `values-local.yaml` (`auth.admins`, `auth.existingSecret`, `auth.github.enabled`)
**non relues** par la doc : à confirmer par `infra` avant le prochain `helm upgrade`.

1. **Secret GitHub OAuth — créé AVANT `helm upgrade`** (seulement quand `auth.github.enabled: true` ; absent → le pod ne démarre pas).
   `client-id` et `client-secret` de l'application GitHub **de QUALIF** sont fournis par l'utilisateur dans `PRIVATE/QUALIF/`
   (jamais inventés, jamais affichés) ; URL de retour de l'application : `https://launcher-qualif.local/oauth2/callback`.
   Le `cookie-secret` n'est **pas** à créer : le chart le génère (`<release>-auth-cookie`, contrat rev1). Création, sans écho :
   ```bash
   ( set -euo pipefail; . .claude/agents/environments/qualif.env
     KUBECONFIG=$QUALIF_KUBECONFIG kubectl -n claude-qualif create secret generic claude-launcher-qualif-github \
       --from-file=client-id=PRIVATE/QUALIF/github-client-id --from-file=client-secret=PRIVATE/QUALIF/github-client-secret )
   ```
   (nom retenu : `claude-launcher-qualif-github`) puis reporter ce nom dans `auth.existingSecret` et `auth.github.enabled: true` + `auth.admins` (M4, logins fournis par l'utilisateur).
2. **Mot de passe `admin@local`** (Secret généré par le chart, P11) — lu à la demande, **jamais affiché dans un rapport ni un journal** :
   écrit dans un fichier tmpfs que l'utilisateur lit, effacé au bout de 10 min (clé `password` ; vérifier le nom de clé dans `contracts/auth.md` §8) :
   ```bash
   ( umask 077; . .claude/agents/environments/qualif.env
     U=$(mktemp -p /dev/shm claude-launcher-qualif-admin-XXXXXX)
     KUBECONFIG=$QUALIF_KUBECONFIG kubectl -n claude-qualif get secret claude-launcher-qualif-local-admin \
       -o jsonpath='{.data.password}' | base64 -d > "$U"
     ( sleep 600; rm -f "$U" ) >/dev/null 2>&1 &
     echo "mot de passe admin@local : $U (cat par l'utilisateur ; effacé dans 10 min)" )
   ```
3. **Contrôle `/healthz`** (port santé `CL_HEALTH_ADDR`, 8082 dans le pod, non exposé par le Service ni l'Ingress) :
   `kubectl -n claude-qualif exec statefulset/claude-launcher-qualif -c launcher -- curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8082/healthz` → `200`
   (si `curl` est absent du conteneur : `kubectl port-forward pod/claude-launcher-qualif-0 8082` puis `curl localhost:8082/healthz`).
   Sans identité, l'adresse publique ne répond que la connexion (jamais l'interface) : `curl -sk -o /dev/null -w '%{http_code}' https://launcher-qualif.local/`
   → redirection/connexion, jamais `200` sur l'interface.
4a. **Accès public (#254, QUALIF)** : `externalIngress` sur `coupel.net` + `pathPrefix: /claude-qualif` (hôte partagé avec d'autres applications ; aucun chemin
   existant ne chevauche : `/emby /radarr /sonarr /lidarr /prowlarr /chaptarr /ombi /seerr /automation-factory`). Le chart refuse `tls.clusterIssuer`
   avec un `pathPrefix` et ne crée aucun Certificat : le certificat de `coupel.net` (Secret `coupel.net-tls`, namespace `media`, hors de notre périmètre) est
   choisi par SNI — **à contrôler après déploiement** (`curl -vI https://coupel.net/claude-qualif/` : émetteur Let's Encrypt, pas le certificat par défaut de Traefik).
   Callbacks à enregistrer dans la GitHub App : `https://coupel.net/claude-qualif/oauth2/callback` et `https://launcher-qualif.local/oauth2/callback`.
   Secret `claude-launcher-qualif-github` créé le 2026-11-26 (clés `client-id` 20 o, `client-secret` 40 o) ; fichiers `PRIVATE/QUALIF/github-client-id|secret` en 600.
4. Image du sidecar figée par empreinte : `quay.io/oauth2-proxy/oauth2-proxy:v7.15.6@sha256:c3415e8660af553f5e18f5d90db050614c02493c0ac6260ac59442f6d9b12048`
   (relevée le 2026-11-25) ; l'image rendue du sidecar est contrôlée comme celle du launcher (étape 3 ci-dessus).

## `helm upgrade` — jamais `--reuse-values` (constaté en 6.4.0.12)

`--reuse-values` réutilise **uniquement** les valeurs enregistrées à l'installation précédente, sans les défauts du chart de
cette version : une clé **nouvelle** du chart (ex. `externalIngress.enabled`, v6.4.0) est alors absente et le rendu échoue
(`cl.validate` : « nil pointer evaluating interface {}.enabled », aucun changement appliqué).
Pour un upgrade qui garde les valeurs courantes (changement d'image seul, valeur ajoutée ou retirée à la main) :
`helm upgrade … --reset-then-reuse-values --set image.tag=… --set image.digest=…` (défauts du nouveau chart + valeurs
utilisateur précédentes). Retirer une valeur : `--set <clé>=null`. La procédure ci-dessus (`-f values-qualif.yaml -f $QUALIF_VALUES_LOCAL`)
n'est pas concernée ; contrôler avant (`helm upgrade … --dry-run=client`).
