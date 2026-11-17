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
| Compte helm | `PRIVATE/QUALIF/ng2-deploy.kubeconfig` | `PRIVATE/PROD/ng2-deploy.kubeconfig` |

> **En cluster, le contexte de TOUS les backends `k8s` est ignoré** (ServiceAccount du pod, namespace déclaré) : un backend
> `k8s` d'un **autre cluster** déclaré dans le `backends.yaml` de cette racine agirait sur le cluster du pod (dans le namespace
> déclaré, si le Role le permet). Ne déclarer dans ce fichier que des backends `k8s` du cluster où tourne le launcher.

### Racine de QUALIF (créée une fois, puis réutilisée)

Prérequis : dossier `/share/MFS/ROOT/CLAUDE-QUALIF` et namespace `claude-qualif` (créés par l'utilisateur), propriétaire `1000:1000` ;
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
   (BORE : aucun recompilage, sa version est contrôlée), archive OCI et empreinte à côté de lui ; le tag de production
   (`vX.Y.Z`, ghcr) n'est jamais touché :
   ```bash
   . .claude/agents/environments/qualif.env        # QUALIF_REGISTRY, QUALIF_KUBECONFIG, QUALIF_VALUES_LOCAL
   OUT_DIR=build/candidate_v<X.Y.Z> SOURCE_BIN=build/candidate_v<X.Y.Z>/claude-launcher-ng2-<X.Y.Z.a> OCI_ARCHIVE=1 \
     deploy/image/build.sh v<X.Y.Z.a>
   sudo buildah --storage-driver vfs push --tls-verify=false localhost/claude-launcher:v<X.Y.Z.a> \
     docker://$QUALIF_REGISTRY/claude-launcher:v<X.Y.Z.a>
   ```
   Contrôle : `curl -s http://$QUALIF_REGISTRY/v2/claude-launcher/tags/list` contient `v<X.Y.Z.a>`.
3. `KUBECONFIG=$QUALIF_KUBECONFIG helm upgrade --install claude-launcher-qualif deploy/helm/claude-launcher
   -n claude-qualif -f deploy/helm/values-qualif.yaml -f $QUALIF_VALUES_LOCAL --set image.tag=v<X.Y.Z.a>` ;
   `kubectl rollout status statefulset/claude-launcher-qualif`.
   Le nom de l'émetteur n'est pas dans le dépôt (`values-*.yaml` portent `a-renseigner`, refusé par le chart) : le donner par
   `--set ingress.tls.clusterIssuer=<nom réel>`, ou par un fichier de valeurs local **`PRIVATE/QUALIF/values-local.yaml`**
   (`ingress: {tls: {clusterIssuer: <nom réel>}}`, jamais commité) ajouté en dernier : `-f PRIVATE/QUALIF/values-local.yaml`.
4. URL d'amorçage : `kubectl exec statefulset/claude-launcher-qualif -- cat /share/MFS/ROOT/CLAUDE-QUALIF/.cache/claude-launcher-ng2/cluster-qualif/desktop-web-token`.
5. Diagnostic du pod : backend `k8s` → contexte ignoré, chaque droit OK dans `claude-qualif` ; ouvrir/fermer/rouvrir un factice
   (StatefulSet dans `claude-qualif` uniquement).
6. Risque accepté (H2) : image désignée par tag, sans digest (réseau interne de confiance).
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
