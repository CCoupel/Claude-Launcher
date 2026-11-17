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
| Racine (`rootBase`) | `/share/MFS/ROOT/CLAUDE-PROJECTS-QUALIF` | `/share/MFS/ROOT/CLAUDE_PROJETS` |
| PVC | `claude-projects-qualif` (ne monte que la racine de QUALIF) | `claude-projets` |
| `backends.yaml` | celui de la racine de QUALIF (ci-dessous) | le réel, inchangé |
| Nom de hub | `cluster-qualif` | `cluster` |
| Compte helm | `PRIVATE/QUALIF/ng2-deploy.kubeconfig` | `PRIVATE/PROD/ng2-deploy.kubeconfig` |

### Racine de QUALIF (créée une fois, puis réutilisée)

Prérequis : dossier `/share/MFS/ROOT/CLAUDE-PROJECTS-QUALIF` créé sur MooseFS (fiche administrateur), propriétaire `1000:1000`.

```
/share/MFS/ROOT/CLAUDE-PROJECTS-QUALIF/
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
  srv1-docker: {type: docker, host: 'tcp://192.168.1.211:2375', shared: [/share/MFS/ROOT/CLAUDE-PROJECTS-QUALIF]}
  # même forme que la PROD (context: k8s) : en cluster le contexte est ignoré, le ServiceAccount du pod est utilisé
  k8s: {type: k8s, context: k8s, namespace: claude-qualif, volumes: {/share/MFS/ROOT/CLAUDE-PROJECTS-QUALIF: claude-projects-qualif}}
```

Projets factices : `git init` ; `.claude-launcher/env.yaml` avec une étape `setup` qui pose un faux `claude` dans
`~/.local/bin` (script qui affiche une invite et lit l'entrée) — jamais de vraie session claude.

### Installation et contrôles

1. Sauvegarde : rien à sauvegarder côté PROD (aucun fichier de PROD n'est lu ni écrit) ; vérifier
   `kubectl --kubeconfig PRIVATE/QUALIF/ng2-deploy.kubeconfig get pvc claude-projects-qualif` → `Bound` RWX.
2. Image candidate : `deploy/image/build.sh <X.Y.Z.a>`, puis
   `sudo buildah --storage-driver vfs push --tls-verify=false localhost/claude-launcher:<X.Y.Z.a> docker://192.168.1.211:5000/claude-launcher:<X.Y.Z.a>`.
3. `KUBECONFIG=PRIVATE/QUALIF/ng2-deploy.kubeconfig helm upgrade --install claude-launcher-qualif deploy/helm/claude-launcher
   -n claude-qualif -f deploy/helm/values-qualif.yaml --set image.tag=<X.Y.Z.a>` ; `kubectl rollout status statefulset/claude-launcher-qualif`.
4. URL d'amorçage : `kubectl exec statefulset/claude-launcher-qualif -- cat /share/MFS/ROOT/CLAUDE-PROJECTS-QUALIF/.cache/claude-launcher-ng2/cluster-qualif/desktop-web-token`.
5. Diagnostic du pod : backend `k8s` → contexte ignoré, chaque droit OK dans `claude-qualif` ; ouvrir/fermer/rouvrir un factice
   (StatefulSet dans `claude-qualif` uniquement).
6. Risque accepté (H2) : image désignée par tag, sans digest (réseau interne de confiance).
7. **Contrôle de la liste d'adresses (H3) — obligatoire avant de déclarer l'Ingress « réservé au réseau interne »** :
   1. `kubectl -n kube-system get svc -o custom-columns=NOM:.metadata.name,POLITIQUE:.spec.externalTrafficPolicy | grep -i traefik`
      → attendu `Local` ; `Cluster` : Traefik voit l'adresse d'un nœud, la liste est inopérante → le signaler, ne pas valider.
   2. Depuis un poste de `192.168.1.0/24` : `curl -sk -o /dev/null -w '%{http_code}' https://launcher-qualif.coupel.lan/healthz` → `200`.
   3. Depuis une adresse **hors** de `192.168.1.0/24` (autre réseau, VPN, partage de connexion) : même commande → `403`.
   4. Depuis un pod du cluster passant par l'Ingress (`kubectl -n claude-qualif exec <pod> -- curl -sk …`) → `403`.
   5. `curl -sk -H 'X-Forwarded-For: 192.168.1.10' …` depuis une adresse hors liste → `403` (pas d'`ipStrategy` : Traefik juge
      l'adresse de la connexion, pas l'en-tête).
   Consigner les résultats dans le rapport de QUALIF.
8. Après validation : `helm uninstall claude-launcher-qualif -n claude-qualif`, fermer les factices
   (`kubectl -n claude-qualif delete sts -l cl.launcher=ng2,cl.backend=k8s`). La racine de QUALIF et son `backends.yaml` sont
   **gardés** pour la QUALIF suivante ; seul le dossier d'état `…/.cache/claude-launcher-ng2/cluster-qualif` (jeton) est supprimé.
