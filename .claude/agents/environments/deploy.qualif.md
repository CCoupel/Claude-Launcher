# Adaptations projet — deploy QUALIF (Claude-Launcher)

QUALIF = binaires de test pour les essais réels de l'utilisateur, déposés à côté du binaire du hub, **sans le remplacer**.

- Compiler depuis le candidat : Linux `claude-launcher-ng2-<X.Y.Z.a>` et Windows `claude-launcher-ng2-<X.Y.Z.a>.exe`
  (`-tags desktop,production -H windowsgui`, voir `scripts/ci.sh`).
- Racine : `ROOT_BASE=${CL_ROOT_BASE:-/share/MFS/ROOT/CLAUDE_PROJETS}` (`CL_ROOT_BASE` de `qualif.env` si posée, #166).
- Déposer dans `$ROOT_BASE/` (cible déclarée), jamais par-dessus `claude-launcher-ng2` ni `.exe`.
- Donner à l'utilisateur les chemins exacts et ce qu'il doit tester ; la validation se fait sur ses essais Windows/Linux.
- Après la release PROD, supprimer les binaires de test QUALIF de ce dossier (garder seulement les binaires de prod et le `.bak`).

## Launcher du cluster (v6.3.0) — QUALIF dans le namespace `claude-ng2-qualif`

- Installation : `helm upgrade --install claude-launcher-qualif deploy/helm/claude-launcher -n claude-ng2-qualif
  -f deploy/helm/values-qualif.yaml --set image.tag=<X.Y.Z.a>` avec `KUBECONFIG=PRIVATE/QUALIF/ng2-deploy.kubeconfig`
  (jamais le compte de PROD). Image candidate dans le registre interne `192.168.1.211:5000`.
- Risque accepté (H2) : image désignée par tag, sans digest (réseau interne de confiance).
- **Contrôle de la liste d'adresses (H3) — obligatoire avant de déclarer l'Ingress « réservé au réseau interne »** :
  1. `kubectl -n kube-system get svc -l app.kubernetes.io/name=rke2-traefik -o jsonpath='{.items[*].spec.externalTrafficPolicy}'`
     (ou le Service du contrôleur Traefik) → attendu `Local` ; `Cluster` : Traefik voit l'adresse d'un nœud, la liste est
     inopérante → le signaler à l'administrateur, ne pas valider.
  2. Depuis un poste de `192.168.1.0/24` : `curl -sk -o /dev/null -w '%{http_code}' https://launcher-qualif.coupel.lan/healthz` → `200`.
  3. Depuis une adresse **hors** de `192.168.1.0/24` (autre réseau, VPN, partage de connexion) : même commande → `403`.
  4. Depuis un pod du cluster passant par l'Ingress (`kubectl -n claude-ng2-qualif exec <pod> -- curl -sk …`) → `403`
     (adresse de pod 10.42.x hors de la liste).
  5. En-tête falsifié depuis l'extérieur : `curl -sk -H 'X-Forwarded-For: 192.168.1.10' …` depuis une adresse hors liste → `403`
     (le chart ne pose pas d'`ipStrategy` : Traefik juge l'adresse de la connexion, pas l'en-tête).
  Consigner les résultats dans le rapport de QUALIF.
- Après validation : `helm uninstall claude-launcher-qualif -n claude-ng2-qualif` et suppression de son dossier d'état
  (`<racine>/.cache/claude-launcher-ng2/cluster-qualif`) — le jeton y est (risque accepté de v6.3.0).
