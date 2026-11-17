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

1. Image de release : construite et publiée par la CI sur le tag (`ghcr.io/ccoupel/claude-launcher:vX.Y.Z`, paquet public).
2. `KUBECONFIG=PRIVATE/PROD/ng2-deploy.kubeconfig helm upgrade --install claude-launcher deploy/helm/claude-launcher
   -n claude-ng2 -f deploy/helm/values-prod.yaml` (tag = `appVersion` du chart) ; `kubectl rollout status statefulset/claude-launcher`.
3. URL d'amorçage : `kubectl -n claude-ng2 exec statefulset/claude-launcher -- cat /share/MFS/ROOT/CLAUDE_PROJETS/.cache/claude-launcher-ng2/cluster/desktop-web-token`.
4. Contrôles à la première installation : test négatif de la liste d'adresses (comme en QUALIF, avec `launcher.coupel.lan`) ;
   prise de main hub ↔ cluster sur un projet réel, faite par l'utilisateur.
5. Mise à jour : changer le tag (nouveau `vX.Y.Z`) — l'ancien pod s'arrête avant le démarrage du nouveau.
