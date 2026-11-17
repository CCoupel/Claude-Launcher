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

### Installation et contrôles

1. Sauvegarde : rien à sauvegarder côté PROD (aucun fichier de PROD n'est lu ni écrit). Le PVC est créé par l'installation
   (étape 3) ; après elle : `kubectl -n claude-qualif get pvc claude-projects-qualif` → `Bound` RWX sur `claude-qualif-pv`.
2. Image candidate `v<X.Y.Z.a>` (tag **avec le « v »**, exigé par `build.sh`) construite **depuis le binaire candidat de BUILD**
   (BORE : aucun recompilage, sa version est contrôlée), puis publiée dans le paquet ghcr **de QUALIF**
   `ghcr.io/ccoupel/claude-launcher-qualif` — **jamais** dans `ghcr.io/ccoupel/claude-launcher` (PROD, écrit seulement par la CI au tag).
   Bloc à lancer d'un seul tenant (sous-shell : `set -e`, garde avant envoi, fichier d'authentification supprimé même en cas d'échec) :
   ```bash
   (
     set -euo pipefail
     . .claude/agents/environments/qualif.env      # QUALIF_IMAGE, QUALIF_KUBECONFIG, QUALIF_VALUES_LOCAL
     V=v<X.Y.Z.a>; C=build/candidate_v<X.Y.Z>
     REF="$QUALIF_IMAGE:$V"
     # garde : jamais le paquet de PROD
     case "$REF" in ghcr.io/ccoupel/claude-launcher-qualif:*) ;; *) echo "ARRÊT : $REF n'est pas dans ghcr.io/ccoupel/claude-launcher-qualif" >&2; exit 1 ;; esac
     OUT_DIR=$C SOURCE_BIN=$C/claude-launcher-ng2-${V#v} OCI_ARCHIVE=1 deploy/image/build.sh "$V"
     A=PRIVATE/QUALIF/.ghcr-auth.json
     trap 'sudo rm -f "$A"' EXIT                   # jeton jamais laissé sur disque, même en cas d'échec
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
4. URL d'amorçage — **le jeton n'est jamais affiché** : il est lu dans le pod et écrit dans un fichier privé (600), à ouvrir sur le poste :
   ```bash
   ( umask 077; . .claude/agents/environments/qualif.env
     t=$(KUBECONFIG=$QUALIF_KUBECONFIG kubectl -n claude-qualif exec statefulset/claude-launcher-qualif -- \
           cat /share/MFS/ROOT/CLAUDE-QUALIF/.cache/claude-launcher-ng2/cluster-qualif/desktop-web-token)
     printf 'https://launcher-qualif.local/?t=%s\n' "$t" > PRIVATE/QUALIF/url-amorcage.txt
     echo "URL d'amorçage : PRIVATE/QUALIF/url-amorcage.txt (${#t} caractères de jeton)" )
   ```
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
