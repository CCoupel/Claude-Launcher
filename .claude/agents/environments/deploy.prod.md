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
  Sans cela le cluster ne peut pas tirer l'image (aucun secret de tirage prévu) ni `helm` lire le chart.
  Contrôle (sans jeton) : `curl -s "https://ghcr.io/token?scope=repository:ccoupel/claude-launcher:pull"` rend un `token`
  (et non `DENIED`) ; idem avec `ccoupel/charts/claude-launcher`.
- Comptes `ng2-deploy`, nom mDNS `launcher.local` (DNS dynamique du réseau), émetteur, Pod Security : fiche administrateur
  (`_work/reports/infra-admin-noeuds-20261117-090000.md`).

1. Image et chart de release : construits et publiés par la CI sur le tag (travail `release-ng2-cluster` de `release.yml`) :
   image `ghcr.io/ccoupel/claude-launcher:vX.Y.Z` (amd64 + arm64, empreinte jointe à la release :
   `claude-launcher-image-vX.Y.Z.digest`), chart `oci://ghcr.io/ccoupel/charts/claude-launcher` version `X.Y.Z` et archive
   `claude-launcher-X.Y.Z.tgz` jointe. Prérequis à la 1re release : paquets ghcr **publics** (P3, ci-dessus). Contrôle :
   `gh release view vX.Y.Z` liste le `.tgz` et le `.digest`.
2. `. .claude/agents/environments/prod.env` puis `KUBECONFIG=$PROD_KUBECONFIG helm upgrade --install claude-launcher deploy/helm/claude-launcher
   -n claude-ng2 -f deploy/helm/values-prod.yaml -f $PROD_VALUES_LOCAL` (tag = `appVersion` du chart, `vX.Y.Z`
   avec le « v » ; le forcer au besoin par `--set image.tag=vX.Y.Z`) ; `kubectl rollout status statefulset/claude-launcher`.
   Le nom de l'émetteur n'est pas dans le dépôt (`values-*.yaml` portent `a-renseigner`, refusé par le chart) : le donner par
   `--set ingress.tls.clusterIssuer=<nom réel>`, ou par un fichier de valeurs local **`PRIVATE/PROD/values-local.yaml`**
   (`ingress: {tls: {clusterIssuer: <nom réel>}}`, jamais commité) ajouté en dernier : `-f PRIVATE/PROD/values-local.yaml`.
3. URL d'amorçage : `kubectl -n claude-ng2 exec statefulset/claude-launcher -- cat /share/MFS/ROOT/CLAUDE_PROJETS/.cache/claude-launcher-ng2/cluster/desktop-web-token`.
4. Contrôles à la première installation : test négatif de la liste d'adresses (comme en QUALIF, avec `launcher.local`) ;
   prise de main hub ↔ cluster sur un projet réel, faite par l'utilisateur.
5. Mise à jour : changer le tag (nouveau `vX.Y.Z`) — l'ancien pod s'arrête avant le démarrage du nouveau.
