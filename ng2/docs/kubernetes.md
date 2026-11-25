# Déploiement dans un cluster Kubernetes

> Autres guides : [Installation et lancement](installation.md) · [Configuration et projets en conteneur](configuration.md) · [Launcher Windows natif](windows.md) · [Interface](interface.md) · [Versions, mises à jour et template](versions.md) · [Architecture](architecture.md) · [Dépannage](depannage.md) · [Tests](tests.md). Vue d'ensemble : [README](../README.md).

**Sommaire**

- [Launcher dans un cluster (v6.3.0)](#launcher-dans-un-cluster-v630)
  - [Installer le launcher dans Kubernetes par Helm (accès interne)](#installer-le-launcher-dans-kubernetes-par-helm-accès-interne)
    - [1. Prérequis](#1-prérequis)
    - [2. Valeurs](#2-valeurs)
    - [3. Installation](#3-installation)
    - [4. Accès](#4-accès)
    - [5. Vérifications obligatoires](#5-vérifications-obligatoires)
    - [6. Mise à jour et retour arrière](#6-mise-à-jour-et-retour-arrière)
    - [7. Cohabitation : cluster, hub Linux et exe Windows sur la même racine](#7-cohabitation--cluster-hub-linux-et-exe-windows-sur-la-même-racine)
    - [8. Backends distants depuis le pod](#8-backends-distants-depuis-le-pod)
    - [9. Limites de v6.4.0 (risques acceptés)](#9-limites-de-v640-risques-acceptés)

---

## Launcher dans un cluster (v6.3.0)

Le launcher peut tourner dans un pod Kubernetes, comme **serveur** de l'interface web (`desktop --web`), à côté des
projets qu'il ouvre. Il se pilote depuis un navigateur, derrière un Ingress (adresse publique : réglage `desktop_web.url`,
voir « Interface web de la fenêtre native » plus haut).

**L'image** (#167) : `ghcr.io/ccoupel/claude-launcher` (paquet public), tags `vX.Y.Z` et `vX.Y.Z.a`. Elle contient le
binaire statique, `bash`, `git`, `gh` et `kubectl` (version fixée, somme vérifiée à la construction). **Ni `claude` ni
`tmux`** : les sessions des projets tournent dans leurs conteneurs ou leurs pods.

- Construction : `deploy/image/build.sh <vX.Y.Z[.a]>` (buildah). Le binaire est compilé avec la même commande que
  `release.yml`, puis mis dans l'image. `OCI_ARCHIVE=1` produit aussi une archive OCI et son `SHA256SUMS`.
- Utilisateur non root, uid:gid `1000:1000` (remplaçables par le chart). `HOME=/home/launcher` n'est **pas** persistant.
- Port `8080`. `CL_UPDATE_MODE=notify` : la nouvelle version est annoncée, mais le pod ne se met pas à jour seul, c'est
  l'image qui change.
- **`CL_ROOT_BASE` est obligatoire** : le point d'entrée (`entrypoint.sh`) refuse de démarrer sans elle (code `64`), et
  aussi si elle ne désigne pas un dossier, par exemple un volume non monté (code `66`). Le chart la pose (`rootBase`).
- Le point d'entrée lance `desktop --web --addr 0.0.0.0:8080 --no-open`, avec `--url` si `CL_WEB_URL` est posée. Il
  laisse passer `version`, `backends` et `help` pour le diagnostic.

**Config au démarrage** : si `config.conf` n'existe pas sous la racine, il est créé dans
`<racine>/.config/claude-launcher-ng2`, avec `GITHUB_DIR="${CL_ROOT_BASE}/PROJETS"` et
`CLAUDE_CONFIG_DIR="${CL_ROOT_BASE}/.claude"` (aucun chemin absolu écrit). Le fichier est partagé par le hub Linux, l'exe
Windows et le pod, chacun développe `${CL_ROOT_BASE}` avec sa propre forme de la racine. Si `CL_ROOT_BASE` n'est pas
dans l'environnement (exe Windows, mode portable), la racine est déduite de l'emplacement du fichier, pour cette lecture
seulement. Si elle ne correspond pas, les chemins qui en dérivent sont écartés et `backends` affiche « ⚠ config.conf
utilise ${CL_ROOT_BASE}, non résolue… », sans jamais retomber sur un `/PROJETS` silencieux.

**Détection et identité** :

- Le launcher se croit dans un cluster quand `KUBERNETES_SERVICE_HOST` est posé et que le jeton du ServiceAccount
  (`/var/run/secrets/kubernetes.io/serviceaccount/token`) est lisible. `CL_IN_CLUSTER=1` ou `0` force le choix.
- Un backend k8s **sans `context`** utilise le ServiceAccount du pod (`kubectl` sans `--context` ni `KUBECONFIG`). Un
  backend **sans `namespace`** prend celui du pod.
- En cluster, le `context` d'un backend k8s est **ignoré** (#168) : le pod n'agit que sur le cluster de son
  ServiceAccount. Un backend copié depuis un autre poste, qui vise un autre cluster, agirait donc sur celui du pod :
  retirez-le du `backends.yaml` du pod.
- Sans `backends.yaml`, un backend k8s implicite est proposé, avec les volumes de `CL_K8S_VOLUMES` au format
  `<racine>=<pvc>[,<racine>=<pvc>…]`.
- Le backend `host` est refusé dans le pod (claude n'y est pas installé), sauf `CL_ALLOW_HOST_BACKEND=1`.

**« Tester » un backend k8s** : les droits sont vérifiés un par un (`kubectl auth can-i`) : `statefulsets`, `pods`,
`pods/exec`, `events`, `persistentvolumeclaims`. Un droit absent affiche « droit manquant : verbe ressource » avec le
conseil de mettre à jour le Role du chart (`helm upgrade`). Si la vérification elle-même échoue, l'étape affiche
« droit non vérifiable » avec la cause renvoyée par `kubectl`. Les volumes déclarés doivent être présents, `Bound`,
`ReadWriteMany`, et montés dans le pod du launcher au chemin prévu.

**Jeton de l'interface** : en cluster, le jeton n'est **jamais imprimé** dans les journaux du pod. Le journal indique
seulement le fichier où le lire : `<cache>/desktop-web-token` (0600), créé au premier démarrage et conservé d'un
redémarrage à l'autre. `CL_DESKTOP_TOKEN` le remplace s'il est posé. Pour le lire, passer par `kubectl exec` sur le
pod, et placer `CL_CACHE_DIR` sur le volume de la racine (convention du chart) : sans cela, le jeton est perdu au
redémarrage du pod.

**Connexion claude.ai et GitHub** (#175) : le pod n'a pas `claude`. La connexion claude.ai ne se fait donc pas dans le
pod : elle se fait dans un projet ouvert depuis le launcher, en lançant `/login` dans son leader. Elle est partagée
par la config de claude commune (`CLAUDE_CONFIG_DIR`). Le pod doit pouvoir joindre `claude.ai` et GitHub : si une
sortie réseau est bloquée (NetworkPolicy, proxy, DNS du cluster), le launcher le signale dans le message d'erreur,
au lieu d'un simple « injoignable ».

**Arrêt** : `SIGTERM` (envoyé par le kubelet) et `SIGINT` ferment le serveur proprement, détachent les terminaux et
sortent avec le code 0. Les projets restent ouverts dans leurs pods.

**Mode d'accès `none`** (#192) : `desktop_web.access` (ou `CL_WEB_ACCESS`) vaut `token` par défaut, avec jeton. `none`
supprime le jeton : l'interface s'ouvre sans lien d'amorçage. Il n'est accepté qu'en boucle locale, ou avec une adresse
publique en `https` (`url`) ; une adresse en `http` est refusée, même en boucle locale. Au passage en `none`, le fichier
de jeton précédent est supprimé. **Risque accepté** : en `none` hors cluster, tout utilisateur ou processus de la
machine pilote le launcher ; en cluster, l'Ingress et sa liste d'adresses sont la seule barrière. Le chart pose
`web.access: none` par défaut et refuse `none` sans Ingress.

**Installer par Helm** (#170, #173) : le chart `deploy/helm/claude-launcher` déploie le launcher dans le namespace. Il crée
un StatefulSet à une réplique (la mise à jour supprime l'ancien pod avant de créer le nouveau), un Service `ClusterIP`
seul, un ServiceAccount avec un Role limité au namespace (aucun ClusterRole), et un Ingress interne Traefik ou nginx.
Les valeurs obligatoires (`rootBase`, `volume.existingClaim`, `ingress.controller`, `ingress.className`,
`ingress.host`, `ingress.allowedSourceRanges`, un moyen de TLS) font échouer `helm install` avec un message qui les
nomme. Le détail des valeurs et des objets est dans `contracts/helm-values.md` ; le guide d'installation complet viendra
avec #185.

- **Ingress** : une liste d'adresses admises, non vide (un `/0` est refusé), et HTTPS obligatoire, HTTP redirigé.
- **Racine** : le chart ne crée ni ne supprime le PVC des projets. Le pod a un système de fichiers racine en lecture
  seule : il n'écrit que dans `/home/launcher`, `/tmp` (emptyDir) et la racine partagée.
- **Variables du pod** : `CL_ROOT_BASE`, `CL_HUB_NAME`, `CL_CACHE_DIR`, `CL_K8S_VOLUMES`, `CL_WEB_URL` et
  `CL_ALLOW_HOST_BACKEND` sont posées par le chart. Les valeurs réservées ne peuvent pas être redéfinies par `extraEnv`.
- **Désinstallation** : `helm uninstall` retire les objets du chart, mais ne touche ni au PVC, ni aux StatefulSets des
  projets, ni aux fichiers de la racine.

### Installer le launcher dans Kubernetes par Helm (accès interne)

Ce guide installe le launcher dans un namespace Kubernetes, accessible **seulement depuis le réseau interne**, avec le
chart `deploy/helm/claude-launcher`. Les valeurs complètes sont dans `contracts/helm-values.md`. Le guide couvre l'état
livré en **v6.4.0** : **aucune authentification** (elle arrive en v7.0.0, #172). La liste d'adresses de l'Ingress
interne est la seule barrière d'accès.

Le schéma ci-dessous montre les zones de confiance : le poste et le LAN, le cluster, et ce qui reste hors cluster.
Les flèches qui franchissent une frontière portent ce qui les protège. Le launcher du cluster ne remplace pas les
versions natives : le hub Linux et l'exe Windows peuvent tourner **en même temps** sur la même racine partagée.

<p align="center">
  <img src="docs/k8s-zones.svg" width="100%" alt="Zones de confiance : poste et LAN, cluster RKE2, hors cluster. Le hub Linux et l'exe Windows fonctionnent en même temps que le launcher du cluster, sur la même racine partagée.">
</p>

*Légende : bleu, launchers, cluster et pods ; vert, protection et arbitrage (NetworkPolicy, coffre, témoin de présence) ; ocre, hors cluster ; pointillé gris, v7.0.0 (préparé, refusé).*

#### 1. Prérequis

- Un cluster RKE2 (ou équivalent) avec un Ingress Traefik ou nginx, et cert-manager. Un namespace dédié : le Role du chart
  est limité à ce namespace, mais il équivaut à administrer le namespace. N'y mettez que le launcher et ses projets.
- Un volume RWX pour la racine des projets, sous l'une de ces formes : un PVC existant (`volume.existingClaim`) ; un PVC
  créé par le chart (`volume.create.enabled: true`) lié à un PV fourni par l'administrateur (`volume.create.persistentVolume.name`) ;
  ou un PV créé aussi par le chart (`volume.create.persistentVolume.enabled: true`, droits d'administrateur). Une
  StorageClass RWX convient pour un dossier neuf (`volume.create.storageClassName`).
- **Un PV créé par le chart** se déclare avec la **source du PV de production**, recopiée telle quelle (`source`, bloc `csi`,
  `nfs` ou `hostPath`) ; seul le chemin change. Exemple MooseFS :
  ```yaml
  persistentVolume:
    name: projets-pv
    enabled: true
    source:
      csi:
        driver: csi.moosefs.com
        volumeHandle: projets-pv            # identifiant unique du PV : celui du PV de PROD, adapté
        volumeAttributes:
          mfsSubDir: /projets               # dossier MooseFS de la racine
  ```
  **Le dossier doit exister avant `helm install`** (créé avec des droits sur la racine MooseFS, par exemple depuis un poste
  qui la monte). Sans lui, le pod reste en `Init:0/1` et l'événement affiche `bad mount point … No such file or directory`.
  Le guide ne crée aucun dossier à votre place. Si aucun poste ne monte la racine MooseFS, c'est l'administrateur du
  stockage qui crée (ou supprime) ce dossier : un PV temporaire `mfsSubDir: /` monté dans un pod d'administration, puis
  `mkdir` (ou `rm -rf`) d'un **seul** nom de dossier, puis suppression du PV temporaire. Ne montez jamais la racine entière
  dans un pod de l'application.
- Un nom d'accès en `.local` (mDNS), par exemple `launcher.local`. Les pods ne résolvent pas les noms `.local`.
- Une CA interne pour cert-manager, avec des contraintes de noms (`nameConstraints`) sur `launcher.local`. Le certificat
  **public** de cette CA s'installe une fois sur chaque poste qui y accède. La clé ne sort jamais du cluster. Un nom d'essai
  doit être un sous-domaine de `launcher.local` (ex. `guide-test.launcher.local`) : la CA refuse les autres.
- Une image publiée : l'empreinte est jointe à la release, dans `claude-launcher-image-vX.Y.Z.digest`, sous la forme
  `dépôt:tag@sha256:…`. Pour n'en garder que `sha256:…` : `sed -nE 's/.*@(sha256:[0-9a-f]+).*/\1/p' claude-launcher-image-vX.Y.Z.digest`.
- Le namespace doit exister avant l'installation (le guide n'utilise pas `--create-namespace`) : `kubectl create namespace claude-ng2`,
  avec le niveau de Pod Security que fixe l'administrateur (le pod ne demande aucun privilège).

#### 2. Valeurs

```yaml
# values.yaml — exemple complet, à adapter
rootBase: /data/projets                        # chemin absolu de la racine des projets, dans le pod
hubName: cluster                               # unique parmi les launchers de cette racine
image:
  repository: ghcr.io/ccoupel/claude-launcher  # défaut ; un miroir interne le remplace
  # tag et digest : posés à l'installation par --set (§3), jamais dans ce fichier
volume:
  existingClaim: projets                       # nom du PVC des projets (créé ci-dessous si create.enabled)
  create:
    enabled: true                              # false si le PVC existe déjà
    storage: 100Gi
    storageClassName: ""                       # "" = liaison statique au PV ci-dessous
    persistentVolume:
      name: projets-pv                         # obligatoire si storageClassName est ""
      enabled: true                            # false si le PV est fourni par l'administrateur
      source:                                  # obligatoire si enabled: true (voir §1)
        csi:
          driver: csi.moosefs.com
          volumeHandle: projets-pv
          volumeAttributes:
            mfsSubDir: /projets
ingress:
  controller: traefik                          # ou nginx
  className: traefik
  host: launcher.local                         # ou guide-test.launcher.local pour un essai
  allowedSourceRanges: ["192.0.2.0/24"]        # seules adresses admises, non vide ; minimum /8 (IPv4), /16 (IPv6)
  tls:
    clusterIssuer: claude-launcher-ca          # ou secretName, un des deux
  annotations: {}
web:
  access: none                                 # défaut du chart : pas de jeton, l'Ingress est la barrière
networkPolicy:
  enabled: true                                # défaut : entrée au pod seulement depuis les pods Traefik
  ingressNamespaces: [traefik]
  ingressPodSelector: {app.kubernetes.io/name: traefik}
  ingressCIDRs: []                             # Traefik en réseau d'hôte : adresses des nœuds
secrets:
  legacy:
    enabled: true                              # v6.5 : ancien coffre v6.4 (PVC <release>-secrets, s'il existe) monté en lecture seule pour la migration
rbac:
  extraNamespaces: []                          # autres namespaces du même cluster (Role + RoleBinding, pas de ClusterRole)
backends:
  kubeconfigs: []                              # [{name, secretName, key}] : kubeconfig d'un autre cluster, en lecture seule
  docker:
    secretName: ""                             # Secret TLS docker/podman (ca.pem, cert.pem, key.pem)
terminationGracePeriodSeconds: 30              # minimum 30 : le launcher attend jusqu'à 20 s les ouvertures en cours
externalIngress:
  enabled: false                               # true est refusé au rendu tant qu'il n'y a pas d'authentification (v7.0.0)
```

Valeurs obligatoires : `rootBase`, `volume.existingClaim`, `ingress.controller`, `ingress.className`, `ingress.host`,
`ingress.allowedSourceRanges`, et l'un de `ingress.tls.secretName` ou `ingress.tls.clusterIssuer`. Une absente fait échouer
`helm install` avec un message qui la nomme. `allowedSourceRanges` refuse tout `/0` et tout préfixe plus large.
`terminationGracePeriodSeconds` en dessous de 30 est refusé par le schéma. `externalIngress.enabled: true` fait échouer le rendu.

**Droits** : un Role et un RoleBinding limités au namespace (et à chaque namespace de `rbac.extraNamespaces`), jamais de
ClusterRole. Le launcher n'a aucun droit sur les Secrets Kubernetes : son coffre est un dossier de la racine partagée (`<rootBase>/.config/claude-launcher-ng2/secrets/`, en clair, voir [secrets.md](secrets.md)).

#### 3. Installation

Installez l'image **par son empreinte** : le chart la référence `dépôt:tag@sha256:…`, telle qu'elle a été publiée.

> **Pré-version (QUALIF, essai)** : le chart OCI publié et le fichier `.digest` de release n'existent qu'après une release.
> Pour une pré-version, installez le chart local de la branche, avec l'image **de qualif** et l'empreinte relevée sur le
> déploiement QUALIF : `--set image.repository=ghcr.io/ccoupel/claude-launcher-qualif`. Sans cette valeur, le chart tire
> l'image de production (`ghcr.io/ccoupel/claude-launcher`) :
> `helm upgrade --install claude-launcher ./deploy/helm/claude-launcher -n <ns> -f values.yaml --set image.repository=ghcr.io/ccoupel/claude-launcher-qualif --set image.tag=<tag> --set image.digest=sha256:<empreinte>`.

```bash
VERSION=X.Y.Z                                  # version du chart et du launcher
TAG=v$VERSION
DIGEST=$(sed -nE 's/.*@(sha256:[0-9a-f]+).*/\1/p' claude-launcher-image-$TAG.digest)   # empreinte jointe à la release
[ -n "$DIGEST" ] || { echo "empreinte absente : arrêter" >&2; exit 1; }
CHART=oci://ghcr.io/ccoupel/charts/claude-launcher

helm template claude-launcher "$CHART" --version "$VERSION" -n claude-ng2 \
  -f values.yaml --set image.tag="$TAG" --set image.digest="$DIGEST" | grep 'image:'
# contrôle : ghcr.io/ccoupel/claude-launcher:$TAG@$DIGEST — sinon arrêter

helm upgrade --install claude-launcher "$CHART" --version "$VERSION" -n claude-ng2 \
  -f values.yaml --set image.tag="$TAG" --set image.digest="$DIGEST"
kubectl -n claude-ng2 rollout status statefulset/claude-launcher --timeout=5m
kubectl -n claude-ng2 get certificate                  # READY True
```

Le chart ne crée ni ne supprime aucun PVC existant. Les pods de projets sont créés par le launcher, pas par le chart.

#### 4. Accès

- **`web.access: none`** (défaut) : ouvrez `https://launcher.local/` depuis une adresse de la liste. Il n'y a pas de jeton
  ni de lien d'amorçage. Contrôle : `kubectl -n claude-ng2 exec statefulset/claude-launcher -- printenv CL_WEB_ACCESS` → `none`.
- **`web.access: token`** : le jeton ne s'affiche jamais dans les journaux du pod. Lisez-le dans le fichier
  `<rootBase>/.cache/claude-launcher-ng2/<hubName>/desktop-web-token` du pod, par `kubectl exec`, puis donnez-le à l'utilisateur
  par un canal sûr. Ne l'écrivez pas sur un partage.
- **Fichiers des projets** : `https://launcher.local/files/<projet>/<chemin relatif>`. `<projet>` n'est pas le nom court :
  c'est le nom du projet dans le launcher, `<catégorie>-<projet>` (ex. `guide-p1` pour le projet `p1` de la catégorie `guide`).
  `/files/p1/…` et `/files/GUIDE/p1/…` donnent `404`. Le projet doit figurer dans l'arbre des projets du launcher : par
  exemple un dossier `<rootBase>/PROJETS/<catégorie>/<projet>` (un dépôt `git init` suffit pour un essai), que le launcher
  voit et nomme `<catégorie>-<projet>`. Si deux projets portent le même nom, rien n'est servi (`404`, journalisé côté serveur). Les sessions du cluster utilisent
  `CL_WEB_URL/files` ; le chemin interne du pod n'apparaît jamais. Les fichiers sont servis avec une CSP `sandbox` (origine
  opaque) et les fichiers sensibles sont exclus (voir §9).
- La CA doit être installée sur le poste, sinon le navigateur refuse le certificat.

#### 5. Vérifications obligatoires

Avant de déclarer l'accès « réservé au réseau interne », vérifiez la liste d'adresses :

1. Traefik doit garder l'adresse du client : `kubectl get svc -A -o custom-columns=NS:.metadata.namespace,NOM:.metadata.name,POLITIQUE:.spec.externalTrafficPolicy | grep -i traefik`
   → `Local`. Sur RKE2, Traefik est dans le namespace `traefik`. Avec `Cluster`, Traefik voit l'adresse d'un nœud, la liste ne sert à rien : ne pas valider.
2. Depuis une adresse de la liste : `curl -sk -o /dev/null -w '%{http_code}' https://launcher.local/healthz` → `200`.
   Si le LB Traefik n'est pas joignable depuis le poste d'essai, testez le NodePort de Traefik (`kubectl -n traefik get svc`) :
   `curl -sk --resolve "launcher.local:<port>:<IP d'un nœud>" -o /dev/null -w '%{http_code}' https://launcher.local:<port>/healthz`.
   Depuis un pod, la même commande que §5.4 (`--resolve`) sert de variante.
3. Depuis une adresse hors liste (autre réseau, VPN) : même commande → `403`.
4. Depuis un pod du cluster qui passe par l'Ingress. **Ce contrôle ne prouve la liste que si les nœuds du cluster n'en font
   pas partie.** Traefik voit l'adresse source : pour un pod, c'est l'adresse du nœud où il tourne. Si cette adresse est dans
   `allowedSourceRanges`, la réponse est `200` : la liste admet les nœuds, ce n'est pas une faille. Dans ce cas, n'en concluez
   rien, ou réduisez la liste pour exclure les nœuds. Les pods ne résolvent pas `.local` : passez `--resolve`.
   ```bash
   IP_LB=$(kubectl -n traefik get svc traefik -o jsonpath='{.status.loadBalancer.ingress[0].ip}')   # adresse du LB Traefik
   kubectl -n claude-ng2 run curl-test --rm -i --restart=Never --image=curlimages/curl -- \
     curl -sk -o /dev/null -w '%{http_code}\n' --resolve "launcher.local:443:$IP_LB" https://launcher.local/healthz
   ```
   Si Traefik est exposé autrement (NodePort), adaptez la commande à `kubectl -n traefik get svc`. En terminal, `-it` remplace `-i`.
5. Un en-tête `X-Forwarded-For` ne change rien : le contrôle porte sur l'adresse de la connexion. Depuis une adresse
   hors liste, contrôlez-le ainsi :
   ```bash
   curl -sk -o /dev/null -w '%{http_code}\n' -H 'X-Forwarded-For: 192.0.2.10' https://launcher.local/healthz   # → 403
   ```
6. **NetworkPolicy** : un pod qui n'est pas Traefik ne joint pas le launcher, même dans le namespace du launcher. Depuis un pod de test :
   `kubectl -n claude-ng2 run np-test --rm -i --restart=Never --image=curlimages/curl -- curl -s -m 5 http://claude-launcher.claude-ng2.svc:8080/healthz`
   → échec (délai). Une entrée par l'Ingress reste admise.

#### 6. Mise à jour et retour arrière

- **Mise à jour** : relancez `helm upgrade --install` avec le nouveau `image.tag` et le nouveau `image.digest`. Ne réutilisez
  jamais un tag déjà publié.
- **Jamais `--reuse-values` seul** : il ne reprend que les valeurs enregistrées et n'applique pas les défauts du chart de la
  nouvelle version (une clé ajoutée, comme `networkPolicy.*`, resterait absente). Utilisez `-f values.yaml` complet, comme
  à l'installation, ou `--reset-then-reuse-values` (même règle que `deploy.*.md`).
- **Retour arrière** : `helm rollback claude-launcher <révision> -n claude-ng2`. Les projets restent dans leurs pods.
- **Désinstallation** : `helm uninstall` retire les objets du chart. Il ne touche ni au PVC ni au PV créés par le chart
  (`keep`, purge : voir l'essai ci-dessus), ni à l'ancien PVC du coffre de la v6.4 (`<release>-secrets`, `keep`, conservé), ni au coffre commun de la racine, ni aux StatefulSets des projets, ni aux fichiers de la racine.
- **Essai sur une autre racine** : testez d'abord sur une racine dédiée (un autre `rootBase` et `volume`, un `hubName` et un
  namespace de test, par exemple `claude-guide-test`). N'ouvrez jamais un essai sur la racine de production. Créez le dossier
  de la racine d'essai sur MooseFS **avant** `helm install` (§1).
- **Purge d'un essai** : `helm uninstall` garde les volumes. Supprimez le PVC des projets et, s'il existe (installation venue de la v6.4), l'ancien PVC du coffre
  (`kubectl -n <ns> delete pvc <existingClaim> <release>-secrets`), puis les PV restés `Released` (`kubectl get pv` : celui du
  chart, et celui du coffre, dynamique, classe `retain`, retrouvé par `kubectl -n <ns> get pvc <release>-secrets -o jsonpath='{.spec.volumeName}'`).
  Le dossier du PV du coffre se trouve sur MooseFS sous `/pv_data/volumes/<nom du PV>` (provisionneur de la classe `retain` ;
  le nom du PV est celui donné par le `jsonpath` ci-dessus). Supprimez-le avec le dossier de la racine d'essai, dans le même
  cadre de droits que §1. Ne purgez jamais la racine de production.

#### 7. Cohabitation : cluster, hub Linux et exe Windows sur la même racine

Trois launchers peuvent partager la même racine : celui du cluster (interface web, `web.access: none`), le hub Linux (coque
tmux) et l'exe Windows (fenêtre native). Chacun a son cache et son nom (`hubName` ; `CL_HUB_NAME` sur le poste). Deux
launchers de même nom sont refusés.

- **Un seul launcher pilote un projet à la fois** : un témoin de présence indique qui pilote. Les autres affichent le projet
  « ouvert ailleurs » et proposent la **prise de main**.
- **Serveur de maquettes local** (hub et exe, pas le cluster) : `127.0.0.1:8765` par défaut pour le hub Linux, `127.0.0.1:8766`
  par défaut pour l'exe Windows. Le port se règle par `browser_server.listen` (par système : `hubs.<machine>.<système>.browser_server.listen`).
  Ce n'est pas « seul sur le poste » qui décide : l'exe reste sur 8766 tant qu'il n'a pas été réglé autrement.
- **`config.conf` créé par v6.3.0 sous une racine de base** : ses chemins sont écrits `${CL_ROOT_BASE}`, que chaque launcher
  développe avec sa propre forme de la racine. Un `config.conf` **existant** (la racine de PROD, par exemple) garde ses
  chemins tels quels : il n'est pas réécrit.
- **Limite** : ne partagez pas une racine **neuve** créée par v6.3.0 avec un launcher antérieur à v6.3.0. Il ne développe
  pas `${CL_ROOT_BASE}` et ne trouve donc pas ses projets.

#### 8. Backends distants depuis le pod

- **`k8s`** : fonctionne par défaut (ServiceAccount du pod, cluster du pod).
- **kubeconfig par backend** (`backends.<nom>.kubeconfig`) : un autre cluster se déclare avec son kubeconfig, déposé dans le
  coffre depuis les Réglages (`kubeconfig: true`) ou monté par le chart (`backends.kubeconfigs`, `kubeconfig: /chemin`). Sans
  repli sur le ServiceAccount si le fichier est illisible. Le launcher refuse les kubeconfigs qui exécutent une commande
  (`exec`, `auth-provider`) ou lisent un fichier du poste (`client-certificate`, `client-key`, `tokenFile`,
  `certificate-authority`). Il signale `insecure-skip-tls-verify` et `proxy-url`. Les contextes sont listés, le contenu jamais
  affiché.
- **ssh** : les clés et les empreintes se saisissent dans les Réglages du backend, par adresse **IP** plutôt que par nom
  court. Clé privée jamais relue ni affichée, phrase de passe refusée, empreinte confirmée explicitement, écritures journalisées
  sans contenu (`<CacheDir>/secrets-journal.jsonl`). Le client `ssh` est dans l'image.
- **podman et docker** : les clients sont dans l'image, mais ni démon ni service n'y tourne. Un `docker` en TLS se monte par
  `backends.docker.secretName` (`DOCKER_CERT_PATH`, `DOCKER_TLS_VERIFY=1`). Un `tcp://…:2375` sans TLS reste accepté.
- **« Tester »** : vérifie le client présent, la clé et l'empreinte (ssh), puis l'accès.

#### 8b. Pods projets (v7.0.0, breaking)

Un pod projet ne voit plus la racine partagée. Pour chaque volume du backend, seuls sont montés, en `subPath` et au même
chemin que dans le launcher :

- le **dossier du projet** ;
- **`CLAUDE_CONFIG_DIR`**.

Les dossiers « local » (emptyDir) restent montés par-dessus le dossier du projet. Sont **absents** du pod : `<racine>/.config`
(dont le coffre des secrets, voir [secrets.md](secrets.md)), `backends.yaml`, `.kube`, `.cache`, les binaires, les autres
projets et la racine elle-même. Un chemin hors de tout volume du backend est refusé (erreur « stockage partagé ») : aucun
repli sur la racine. Un dossier égal à la racine d'un volume est refusé.

- **Jeton de ServiceAccount** : non monté (`automountServiceAccountToken: false`). Pour le remonter, poser
  `pod.automountServiceAccountToken: true` dans `backends.yaml` ; le diagnostic le signale (« jeton de ServiceAccount monté »).
- **Pods en marche à l'ancien modèle** (sans l'annotation `cl.mounts: "v2"`) : conservés, leur session continue. La carte
  affiche « Montages d'avant v7.0.0 : fermer puis rouvrir le projet pour les restreindre », et le diagnostic le signale.
  Un pod arrêté, ou à recréer, est recréé au nouveau modèle.
- Docker, podman et ssh : inchangés (ils ne montent déjà que le projet et `CLAUDE_CONFIG_DIR`).

#### 9. Limites de v6.4.0 (risques acceptés)

Ces risques sont acceptés pour v6.4.0 et levés par la règle R1 de **v7.0.0** (#172) : session authentifiée exigée pour toute
écriture et tout accès aux fichiers.

- **Pas d'authentification.** Tout poste de la liste d'adresses de l'Ingress pilote les projets. Ne publiez jamais l'Ingress
  hors du réseau interne, et n'exposez pas le port d'écoute autrement. L'ingress externe (`externalIngress`) reste refusé.
- **Écriture des secrets depuis le LAN** (clés ssh, `known_hosts`, kubeconfig) : une machine admise peut redéclarer un backend
  ssh, y déposer sa clé et son empreinte, puis lancer un projet qui exécute des commandes sur un hôte qu'elle contrôle. Atténuations :
  liste d'adresses, NetworkPolicy, journal des écritures, relevé d'empreinte limité aux backends déclarés (ni loopback, ni
  link-local, ni adresse du pod).
- **`/files` lisible depuis le LAN** : les fichiers des projets sont servis, sauf les éléments sensibles (`PRIVATE`, `.git`,
  `.claude`, `.env*`, clés, identifiants d'outils comme `.git-credentials`, `.npmrc`, `.docker`, `*.p12`, `*.pfx`, `*.jks`),
  les dossiers d'état du launcher et `_work/tmp`. Dans `_work`, seul `_work/mockup` est servi. Les requêtes cross-site sont
  refusées, sauf les sous-ressources de maquette et la navigation de premier niveau (ouverture d'une maquette par l'extension ou un lien), acceptée de toute provenance.
- **Script cross-site d'une maquette** : une page tierce peut exécuter un fichier `.js` d'un projet servi sous `/files` et lire
  ses variables globales. Les scripts de maquette ne sont pas restreints en `connect-src` (CDN et API publiques).
- **`web.access: token`** : le jeton reste dans `<cache>/desktop-web-token`, sur le volume partagé de la racine : tout pod qui
  monte ce volume peut le lire. Les pods projets ne montent plus ce volume depuis v7.0.0 (§8b). Avec `web.access: none`, aucun jeton n'existe.
- **Saturation possible** : 32 flux `/api/events` au total, ouvrables par tout client qui atteint le port.
- **Contexte k8s** : sans `kubeconfig` du backend, le `context` d'un backend `k8s` est ignoré en cluster. Déclarez seulement
  des backends `k8s` du cluster où tourne le launcher.
- **Hors cluster, `web.access: none`** : tout processus local pilote le launcher. Hors cluster, `web.access` vaut `token` par défaut.
- **Tag mutable** : si `image.digest` n'est pas posé, `image.tag` peut désigner une image différente demain. Posez toujours
  l'empreinte. Le paquet ghcr de l'image est public : toute personne peut la tirer.
- **Types de Service** : le chart refuse `NodePort` et `LoadBalancer` ; seul `ClusterIP` est admis.
- **Arrêt** : le launcher attend au plus 20 s les ouvertures de projet en cours, puis détache les terminaux ; avec le délai de
  grâce de 30 s, l'arrêt tient en 25 s au pire. Les sessions tmux continuent dans leurs conteneurs.
