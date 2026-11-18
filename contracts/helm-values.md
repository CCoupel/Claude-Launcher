# Contrat — chart Helm du launcher (v6.3.0)

> Chart : `deploy/helm/claude-launcher/` (`Chart.yaml`, `values.yaml`, `values.schema.json`, `templates/`).
> Interface entre le chart (`infra`), le launcher (`contracts/launcher-env.md`, `contracts/desktop-web.md`), les
> procédures de déploiement et le guide #185. Une valeur obligatoire absente fait échouer `helm install` /
> `helm template` avec un message qui la nomme.

## Versions

| Élément | Release `vX.Y.Z` | Version candidate (QUALIF) |
|---------|------------------|----------------------------|
| `Chart.yaml` `version` | `X.Y.Z` | `X.Y.Z` (chart pris dans l'arbre de travail) |
| `Chart.yaml` `appVersion` | `vX.Y.Z` | `vX.Y.Z` |
| Image | `ghcr.io/ccoupel/claude-launcher:vX.Y.Z` | `image.tag=vX.Y.Z.a` |
| Publication du chart | `oci://ghcr.io/ccoupel/charts/claude-launcher`, version `X.Y.Z` ; archive `.tgz` jointe à la release GitHub | aucune |

## Valeurs

| Valeur | Type | Défaut | Obligatoire | Rôle |
|--------|------|--------|-------------|------|
| `image.repository` | chaîne | `ghcr.io/ccoupel/claude-launcher` | — | image |
| `image.tag` | chaîne | `appVersion` du chart | — | tag ; la mise à jour du launcher se fait en le changeant |
| `image.pullPolicy` | chaîne | `IfNotPresent` | — | — |
| `image.digest` | chaîne | `""` | — | empreinte `sha256:…` : image référencée `dépôt:tag@empreinte` (installée exactement telle que publiée) ; le label `app.kubernetes.io/version` garde le tag seul |
| `imagePullSecrets` | liste | `[]` | — | secrets de tirage (registre privé ou interne) |
| `nodeSelector` | table | `{}` | — | nœud imposé au pod — nécessaire quand l'image candidate n'est importée que sur certains nœuds (`image.pullPolicy: Never`) |
| `hubName` | chaîne | nom de la release | — | `CL_HUB_NAME` — unique parmi les launchers de la même racine |
| `rootBase` | chemin absolu | — | **oui** | `CL_ROOT_BASE` |
| `volume.existingClaim` | chaîne | — | **oui** | nom du PVC des projets (RWX) : existant, ou créé par le chart si `volume.create.enabled` |
| `volume.create.enabled` | booléen | `false` | — | le chart crée le PVC `existingClaim` (annotation `helm.sh/resource-policy: keep` : jamais supprimé par `helm uninstall`) |
| `volume.create.storage` | quantité | `100Gi` | — | taille demandée |
| `volume.create.storageClassName` | chaîne | `""` | — | `""` = liaison statique au PV `persistentVolume.name` ; une StorageClass (provisionnement dynamique, dossier neuf, pas `rootBase`) exclut `persistentVolume` |
| `volume.create.persistentVolume.name` | chaîne | — | **oui** si `volume.create.enabled` et `storageClassName: ""` | nom du PV auquel le PVC est lié (`volumeName`, toujours rendu) : PV créé par l'administrateur (`persistentVolume.enabled: false`) ou par le chart (`true`). Sans lui, refus (le PVC se lierait à n'importe quel PV RWX libre). Interdit avec une `storageClassName` |
| `volume.create.persistentVolume.enabled` | booléen | `false` | — | le chart crée aussi le PV (objet du **cluster** : droits d'administrateur au déploiement), `Retain` + `keep`, `claimRef` vers le PVC |
| `volume.create.persistentVolume.source` | table | `{}` | **oui** si PV créé | source du volume (bloc `csi` / `nfs` / `hostPath`…) recopiée du PV de PROD, chemin = `rootBase` ; `capacity`, `accessModes`, `claimRef`, `persistentVolumeReclaimPolicy`, `storageClassName`, `volumeMode` refusés (posés par le chart) |
| `volume.mountPath` | chemin absolu | `rootBase` | — | chemin de montage du PVC dans le pod. `rootBase` doit être ce chemin ou un sous-dossier |
| `runAsUser`, `runAsGroup` | entiers | `1000`, `1000` | — | propriétaire des fichiers créés. `fsGroup` n'est jamais posé (pas de changement de propriétaire récursif) |
| `allowHostBackend` | booléen | `false` | — | `CL_ALLOW_HOST_BACKEND` |
| `web.access` | `none` \| `token` | `none` | — | **NEW** (LOT-4.4) — mode d'accès de l'interface web, devient `CL_WEB_ACCESS`. `none` : sans jeton, l'Ingress et sa liste d'adresses sont la seule barrière. `none` avec `ingress.enabled: false` est refusé au rendu (« accès sans jeton sans Ingress : poser web.access: token ») |
| `service.port` | entier | `8080` | — | Service `ClusterIP` — type non réglable |
| `ingress.enabled` | booléen | `true` | — | `false` : aucun Ingress (diagnostic par `kubectl port-forward` seulement) |
| `ingress.controller` | `traefik` \| `nginx` | — | **oui** si Ingress | décide de la forme de la liste d'adresses et des annotations |
| `ingress.className` | chaîne | — | **oui** si Ingress | `ingressClassName` |
| `ingress.host` | nom DNS | — | **oui** si Ingress | nom dédié ; devient `CL_WEB_URL=https://<host>` (en-tête `Host` attendu par le serveur web). Réseau du projet : DNS dynamique mDNS, domaine `.local` (`launcher-qualif.local`, `launcher.local`) — non résolu depuis les pods |
| `ingress.allowedSourceRanges` | liste de CIDR | — | **oui** si Ingress, **non vide** | seules adresses admises. Chaque entrée est validée (IPv4 : octets 0–255, masque /8 à /32 ; IPv6 : masque /16 à /128) : tout /0, quelle que soit son écriture, et tout préfixe plus large est refusé |
| `ingress.tls.secretName` | chaîne | — | l'un des deux | Secret TLS fourni |
| `ingress.tls.clusterIssuer` | chaîne | — | l'un des deux | émetteur cert-manager ; le Secret est alors `<release>-tls` |
| `ingress.annotations` | table | `{}` | — | annotations ajoutées |
| `resources` | table | demandes `100m` / `128Mi`, limite mémoire `512Mi` | — | — |
| `extraEnv` | liste | `[]` | — | variables ajoutées au conteneur |
| `terminationGracePeriodSeconds` | entier | `30` | — | minimum `30` : l'arrêt attend jusqu'à 20 s les ouvertures de projet en cours |

Il n'y a **pas** de valeur pour le nombre de répliques : le StatefulSet en a toujours une.

## Objets créés

| Objet | Contenu |
|-------|---------|
| ServiceAccount | un par release |
| Role + RoleBinding | limités au namespace, seulement les appels de `core/k8s.go` et `core/close.go` : `apps/statefulsets` (get, list, watch, create, patch, delete) ; `statefulsets/scale` (get, patch, update) ; `pods` (get, list, watch, delete) ; `pods/exec` (create) ; `events` (list) ; `persistentvolumeclaims` (get). **Aucun ClusterRole**. Ces droits équivalent à administrer le namespace : n'y mettre que le launcher et ses projets (QUALIF : namespace `claude-qualif`, racine dédiée `/share/MFS/ROOT/CLAUDE-QUALIF`) |
| StatefulSet | 1 réplique ; `runAsNonRoot`, `allowPrivilegeEscalation: false`, capacités retirées ; volume `existingClaim` monté à `volume.mountPath` ; `emptyDir` pour `/home/launcher` et `/tmp` ; sondes `httpGet /healthz` (vivant) et `/readyz` (prêt) |
| Service | `ClusterIP`, port `service.port` |
| Ingress | si `ingress.enabled` : hôte, TLS, classe ; HTTP redirigé vers HTTPS |
| Middleware Traefik `ipAllowList` | si `ingress.controller: traefik` ; référencé par l'annotation `traefik.ingress.kubernetes.io/router.middlewares` |
| Certificate cert-manager | si `ingress.tls.clusterIssuer` |
| PersistentVolumeClaim | si `volume.create.enabled` : RWX, conservé à la désinstallation |
| PersistentVolume | si `volume.create.persistentVolume.enabled` : RWX, `Retain`, conservé à la désinstallation |

Avec `ingress.controller: nginx` : annotations `nginx.ingress.kubernetes.io/whitelist-source-range`,
`proxy-buffering: "off"`, `proxy-read-timeout` et `proxy-send-timeout` à `3600`.

**Non créés en v6.3.0** (risques acceptés, à rappeler dans le guide) : NetworkPolicy ; Secret du jeton de
l'interface (avec `web.access: token`, le jeton reste dans le cache, sur le volume partagé ; avec `web.access: none`,
il n'y a pas de jeton).

**Avec `web.access: none`** (défaut, décision utilisateur du 2026-11-17) : tout poste de la liste d'adresses de
l'Ingress pilote les projets, et tout pod du cluster atteint le Service sans passer par l'Ingress. Risques acceptés
jusqu'à l'authentification de v6.4.0 (#172). `NOTES.txt` donne l'adresse seule, sans lien d'amorçage, et rappelle
cette limite.

## Variables posées dans le pod

| Variable | Valeur |
|----------|--------|
| `CL_ROOT_BASE` | `rootBase` |
| `CL_HUB_NAME` | `hubName` |
| `CL_CACHE_DIR` | `<rootBase>/.cache/claude-launcher-ng2/<hubName>` |
| `CL_K8S_VOLUMES` | `<volume.mountPath>=<volume.existingClaim>` |
| `CL_WEB_URL` | `https://<ingress.host>` si Ingress |
| `CL_WEB_ACCESS` | `web.access` |
| `CL_ALLOW_HOST_BACKEND` | `1` si `allowHostBackend` |

## Désinstallation

`helm uninstall` retire les objets du tableau ci-dessus, **sauf** le PVC et le PV créés par le chart (`keep`). Il ne touche ni au PVC, ni aux StatefulSets des projets,
ni aux fichiers de `rootBase`.

## Fichiers de valeurs par environnement

`deploy/helm/values-qualif.yaml` et `deploy/helm/values-prod.yaml` : valeurs non secrètes de chaque environnement
(nom d'hôte, classe, CIDR, `rootBase`, `web.access: none` écrit explicitement). Aucun secret : un certificat fourni reste dans `PRIVATE/<ENV>/`.

## v6.4.0 — NetworkPolicy, coffre, identifiants, ingress externe préparé **[CHANGED]** (#174, #193, #194)

> Révision GATE 2 n°2 (rev2) : **aucune authentification** en v6.4.0 (#172 en v7.0.0). Détail :
> `contracts/web-server.md`, `contracts/backends-remote.md`. **Aucun BREAKING** : `web.access` garde son défaut
> `none`, et `ingress.*` de v6.3.0 est inchangé.

| Valeur | Type | Défaut | Obligatoire | Rôle |
|--------|------|--------|-------------|------|
| `networkPolicy.enabled` | booléen | **`true`** | — | entrée du pod seulement depuis `networkPolicy.ingressNamespaces`, port `service.port` ; sondes du kubelet admises ; sortie non restreinte (backends distants, GitHub) |
| `networkPolicy.ingressCIDRs` | liste de CIDR | `[]` | — | sources `ipBlock` en plus (Traefik en réseau d'hôte : adresses des nœuds) ; sans namespace ni CIDR, le rendu est refusé |
| `networkPolicy.ingressNamespaces` | liste | `[traefik]` | — | namespace du contrôleur d'Ingress (cluster du projet : `traefik`, DaemonSet hors réseau d'hôte) |
| `secrets.persistence.size` | quantité | `10Mi` | — | PVC `ReadWriteOnce` `<release>-secrets` (coffre, `helm.sh/resource-policy: keep`), monté en `/var/lib/claude-launcher/secrets`, `CL_SECRETS_DIR` |
| `secrets.persistence.storageClassName` | chaîne | `""` (classe par défaut) | — | classe RWO, **jamais** la racine partagée |
| `rbac.extraNamespaces` | liste | `[]` | — | Role + RoleBinding identiques dans chaque namespace listé (#193) |
| `backends.docker.secretName` | chaîne | `""` | — | Secret TLS docker/podman (`ca.pem`, `cert.pem`, `key.pem`), monté en `/var/run/claude-launcher/docker` (#194), `defaultMode: 0444` (pas de `fsGroup` : `0400` serait illisible par l'uid 1000 ; tmpfs du pod, lecture seule) ; pose `DOCKER_CERT_PATH` et `DOCKER_TLS_VERIFY=1` |
| `backends.kubeconfigs` | liste `{name, secretName, key}` | `[]` | — | kubeconfigs montés en `/var/run/claude-launcher/kube/<name>/config` (#193, lecture seule, `defaultMode: 0444`), alternative au coffre |
| `externalIngress.enabled` | booléen | `false` | — | **préparé, refusé** : `true` fait échouer le rendu (« ingress externe refusé : aucune authentification avant v7.0.0 (#172) ») |
| `externalIngress.className`, `.host`, `.tls.clusterIssuer` (`letsencrypt-prod`) \| `.tls.secretName`, `.annotations` | — | — | — | déclarés dans `values.yaml` et le schéma, inutilisés en v6.4.0 ; le template `ingress-external.yaml` existe mais n'est rendu qu'après le contrôle ci-dessus (donc jamais en v6.4.0) |

Objets ajoutés : NetworkPolicy ; PVC `<release>-secrets` ; Role/RoleBinding par namespace supplémentaire. Montages de
Secret : en lecture seule, hors de `volume.mountPath` (le schéma refuse un chevauchement).

Variable ajoutée dans le pod : `CL_SECRETS_DIR`.

`NOTES.txt` : rappelle que l'interface n'a pas d'authentification (liste d'adresses de l'ingress interne seule) et
que l'ingress externe attend v7.0.0.
