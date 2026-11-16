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
| `imagePullSecrets` | liste | `[]` | — | secrets de tirage (registre privé ou interne) |
| `nodeSelector` | table | `{}` | — | nœud imposé au pod — nécessaire quand l'image candidate n'est importée que sur certains nœuds (`image.pullPolicy: Never`) |
| `hubName` | chaîne | nom de la release | — | `CL_HUB_NAME` — unique parmi les launchers de la même racine |
| `rootBase` | chemin absolu | — | **oui** | `CL_ROOT_BASE` |
| `volume.existingClaim` | chaîne | — | **oui** | PVC partagé des projets (RWX), déjà existant. Le chart ne crée ni ne supprime aucun PVC |
| `volume.mountPath` | chemin absolu | `rootBase` | — | chemin de montage du PVC dans le pod. `rootBase` doit être ce chemin ou un sous-dossier |
| `runAsUser`, `runAsGroup` | entiers | `1000`, `1000` | — | propriétaire des fichiers créés. `fsGroup` n'est jamais posé (pas de changement de propriétaire récursif) |
| `allowHostBackend` | booléen | `false` | — | `CL_ALLOW_HOST_BACKEND` |
| `service.port` | entier | `8080` | — | Service `ClusterIP` — type non réglable |
| `ingress.enabled` | booléen | `true` | — | `false` : aucun Ingress (diagnostic par `kubectl port-forward` seulement) |
| `ingress.controller` | `traefik` \| `nginx` | — | **oui** si Ingress | décide de la forme de la liste d'adresses et des annotations |
| `ingress.className` | chaîne | — | **oui** si Ingress | `ingressClassName` |
| `ingress.host` | nom DNS | — | **oui** si Ingress | nom interne dédié ; devient `CL_WEB_URL=https://<host>` |
| `ingress.allowedSourceRanges` | liste de CIDR | — | **oui** si Ingress, **non vide** | seules adresses admises. Chaque entrée est validée (IPv4 : octets 0–255, masque /8 à /32 ; IPv6 : masque /16 à /128) : tout /0, quelle que soit son écriture, et tout préfixe plus large est refusé |
| `ingress.tls.secretName` | chaîne | — | l'un des deux | Secret TLS fourni |
| `ingress.tls.clusterIssuer` | chaîne | — | l'un des deux | émetteur cert-manager ; le Secret est alors `<release>-tls` |
| `ingress.annotations` | table | `{}` | — | annotations ajoutées |
| `resources` | table | demandes `100m` / `128Mi`, limite mémoire `512Mi` | — | — |
| `extraEnv` | liste | `[]` | — | variables ajoutées au conteneur |
| `terminationGracePeriodSeconds` | entier | `30` | — | — |

Il n'y a **pas** de valeur pour le nombre de répliques : le StatefulSet en a toujours une.

## Objets créés

| Objet | Contenu |
|-------|---------|
| ServiceAccount | un par release |
| Role + RoleBinding | limités au namespace, seulement les appels de `core/k8s.go` et `core/close.go` : `apps/statefulsets` (get, list, watch, create, patch, delete) ; `statefulsets/scale` (get, patch, update) ; `pods` (get, list, watch, delete) ; `pods/exec` (create) ; `events` (list) ; `persistentvolumeclaims` (get). **Aucun ClusterRole**. Ces droits équivalent à administrer le namespace : n'y mettre que le launcher et ses projets (QUALIF : namespace à part `claude-ng2-qualif`) |
| StatefulSet | 1 réplique ; `runAsNonRoot`, `allowPrivilegeEscalation: false`, capacités retirées ; volume `existingClaim` monté à `volume.mountPath` ; `emptyDir` pour `/home/launcher` et `/tmp` ; sondes `httpGet /healthz` (vivant) et `/readyz` (prêt) |
| Service | `ClusterIP`, port `service.port` |
| Ingress | si `ingress.enabled` : hôte, TLS, classe ; HTTP redirigé vers HTTPS |
| Middleware Traefik `ipAllowList` | si `ingress.controller: traefik` ; référencé par l'annotation `traefik.ingress.kubernetes.io/router.middlewares` |
| Certificate cert-manager | si `ingress.tls.clusterIssuer` |

Avec `ingress.controller: nginx` : annotations `nginx.ingress.kubernetes.io/whitelist-source-range`,
`proxy-buffering: "off"`, `proxy-read-timeout` et `proxy-send-timeout` à `3600`.

**Non créés en v6.3.0** (risques acceptés, à rappeler dans le guide) : Secret du jeton de l'interface (le jeton
reste dans le cache, sur le volume partagé), NetworkPolicy.

## Variables posées dans le pod

| Variable | Valeur |
|----------|--------|
| `CL_ROOT_BASE` | `rootBase` |
| `CL_HUB_NAME` | `hubName` |
| `CL_CACHE_DIR` | `<rootBase>/.cache/claude-launcher-ng2/<hubName>` |
| `CL_K8S_VOLUMES` | `<volume.mountPath>=<volume.existingClaim>` |
| `CL_WEB_URL` | `https://<ingress.host>` si Ingress |
| `CL_ALLOW_HOST_BACKEND` | `1` si `allowHostBackend` |

## Désinstallation

`helm uninstall` retire les objets du tableau ci-dessus. Il ne touche ni au PVC, ni aux StatefulSets des projets,
ni aux fichiers de `rootBase`.

## Fichiers de valeurs par environnement

`deploy/helm/values-qualif.yaml` et `deploy/helm/values-prod.yaml` : valeurs non secrètes de chaque environnement
(nom d'hôte, classe, CIDR, `rootBase`). Aucun secret : un certificat fourni reste dans `PRIVATE/<ENV>/`.
