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

## v6.4.0 — NetworkPolicy, coffre, identifiants, ingress externe préparé **[CHANGED]** (#174, #193, #194, #200)

> Révision GATE 2 n°2 (rev2) : **aucune authentification** en v6.4.0 (#172 en v7.0.0). Détail :
> `contracts/web-server.md`, `contracts/backends-remote.md`. **Aucun BREAKING** : `web.access` garde son défaut
> `none`, et `ingress.*` de v6.3.0 est inchangé.

| Valeur | Type | Défaut | Obligatoire | Rôle |
|--------|------|--------|-------------|------|
| `networkPolicy.enabled` | booléen | **`true`** | — | entrée du pod seulement depuis `networkPolicy.ingressNamespaces`, sur le **port nommé `http` du conteneur** (8080 ; pas `service.port`) ; sondes du kubelet admises ; sortie non restreinte (backends distants, GitHub) |
| `networkPolicy.ingressPodSelector` | table de labels | `{app.kubernetes.io/name: traefik}` | — | labels des pods du contrôleur dans le namespace (ET avec `ingressNamespaces`) : un autre pod du namespace `traefik` n'est pas admis ; relevé en lecture seule sur le Traefik RKE2 du cluster (`app.kubernetes.io/name=traefik`) ; une clé à `null` la retire |
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

## v6.5.0 — coffre des secrets partagé **[BREAKING]** (#201)

| Clé | Type | Défaut | Contrôle | Effet |
|-----|------|--------|----------|-------|
| `secrets.persistence.*` | — | — | — | **[BREAKING]** le PVC `<release>-secrets` n'est plus le coffre : `CL_SECRETS_DIR` n'est plus posé, le coffre est `<rootBase>/.config/claude-launcher-ng2/secrets` (`contracts/backends-remote.md`) |
| `secrets.legacy.enabled` | booléen | `true` | — | **[NEW]** monte l'ancien PVC `<release>-secrets` (s'il existe) en lecture seule sur `/var/lib/claude-launcher/secrets-legacy` et pose `CL_SECRETS_LEGACY_DIR` (migration) ; PVC toujours `keep`, plus jamais créé par une nouvelle installation |

- Conteneur d'initialisation `secrets-perms` : supprimé (droits posés par le launcher sur le partage, uid `runAsUser`).
- `runAsUser` doit être le propriétaire du partage (déjà requis) : sinon le coffre `0700` est illisible (refus clair).

## v7.0.0 — authentification, ingress externe, pods projets **[BREAKING]** (#172, #251, #252, #253, #254)

> Contrat du launcher : `contracts/auth.md`. Points `P<n>` : plan `.claude/workflow/plan-v7.0.0.md` (valeur écrite =
> option recommandée, à corriger après le GATE 2). `Chart.yaml` : `version: 7.0.0`, `appVersion: v7.0.0` (règle #204).

### Valeurs `auth.*` **[NEW]**

| Valeur | Type | Défaut | Obligatoire | Rôle |
|--------|------|--------|-------------|------|
| `auth.enabled` | booléen | **`true`** (P1) | — | sidecar oauth2-proxy, launcher en `CL_WEB_ACCESS=proxy` sur la boucle locale. `false` : comportement de v6.9.0 (`web.access` honoré) et règle R1 « lecture seule » des réglages depuis l'interface |
| `auth.github.enabled` | booléen | `true` | — | **[rev1]** connexion GitHub ; `false` permis seulement avec `auth.localAdmin.enabled` (QUALIF avant les applications GitHub, secours) |
| `auth.existingSecret` | chaîne | — | **oui** si `auth.github.enabled` | Secret du namespace, clés `client-id`, `client-secret` (application **GitHub App** de l'environnement, P8) ; jamais dans les valeurs ; absent du cluster → le pod ne démarre pas (nommé par `NOTES.txt`) |
| — (cookie) | — | — | — | **[rev1]** `cookie-secret` généré par le chart (Secret `<release>-auth-cookie`, 32 caractères, `keep`, réutilisé par `lookup`) |
| `auth.github.clientId` / `auth.github.clientSecret` | chaînes | `""` | — | **[addendum 3, LOT-3]** alternative à `auth.existingSecret` : le chart crée le Secret `<release>-github` (`client-id`, `client-secret`). Réservé à `PRIVATE/<ENV>/values-local.yaml` (jamais commité). **Avertissement** (`values.yaml`, `NOTES.txt`, guide) : `helm get values` et `helm template` l'affichent en clair, il est stocké dans le Secret de release Helm ; `existingSecret` reste **recommandé**. Rendu : les deux posés → refus (« auth.existingSecret et auth.github.clientId/clientSecret sont exclusifs ») ; `clientId` sans `clientSecret` (ou l'inverse) → refus ; GitHub activé sans aucun des deux → refus. `NOTES.txt` ne l'affiche jamais. Contrôle `scripts/ci.sh` : `clientSecret` non vide dans un fichier suivi par git (`deploy/helm/values-*.yaml`, tout `*.yaml` suivi) → échec (Helm ne sait pas d'où vient une valeur) |
| `auth.admins` | liste de logins GitHub | `[]` | **oui**, non vide, si `auth.github.enabled` | rôle `admin` (R1) ; chaque entrée validée (forme d'un login, sans doublon) |
| `auth.localAdmin.enabled` | booléen | `true` (P9) | — | **[rev1]** compte `admin@local` (`contracts/auth.md` §8) |
| `auth.localAdmin.existingSecret` | chaîne | `""` | — | **[rev1]** Secret fourni (clé `htpasswd`, ligne `admin@local:$2[aby]$<coût≥10>$…`, contrôlée au rendu par `lookup` quand le cluster est joignable ; mot de passe ≥ 20 caractères aléatoires : consigne) ; vide : Secret `<release>-local-admin` généré (P11) |
| `auth.localAdmin.hosts` | liste | `[<ingress.host>]` | — | **[rev1]** hôtes où le compte local est admis (`CL_AUTH_LOCAL_ADMIN_HOSTS`) ; l'hôte externe y est **refusé au rendu** (P10) |
| `auth.users` | liste de logins GitHub | `[]` | — | rôle `user` (P2) : terminaux et projets, pas d'écriture R1 ; un login présent dans les deux listes est refusé au rendu |
| `auth.sessionDuration` | durée (`h`, `m`) | `8h` | — | `--cookie-expire` ; minimum `15m`, maximum `168h` |
| `auth.sessionRefresh` | durée | `1h` | — | `--cookie-refresh` (< `sessionDuration`, `0` interdit). **[addendum 2, m6]** Le fournisseur GitHub n'a pas de jeton de renouvellement : rien n'est revérifié chez GitHub ; la révocation est la liste du launcher (`CL_AUTH_*`, relue à chaque requête) ou la rotation du `cookie-secret` |
| `auth.proxy.image.repository` | chaîne | `quay.io/oauth2-proxy/oauth2-proxy` | — | image du sidecar |
| `auth.proxy.image.tag` | chaîne | `v7.15.6` (rapport `infra` du 2026-11-25) | — | jamais `latest` (refusé au rendu) ; digest relevé au build |
| `auth.proxy.image.digest` | chaîne | `""` | — | `sha256:…` facultatif, comme `image.digest` |
| `auth.proxy.resources` | table | demandes `10m` / `32Mi`, limite mémoire `128Mi` | — | — |
| `auth.rateLimit.average` / `.burst` | entiers | `10` / `20` | — | Traefik `rateLimit` (par adresse source, par minute) sur les routes `/oauth2/` des ingress : essais répétés bloqués (#172) ; nginx : `limit-rpm` sur un Ingress `/oauth2` séparé |

- **[cycle 1, m6]** `ingress.controller: nginx` avec `auth.localAdmin.enabled` → refus au rendu (le retrait de `Authorization` et la limite d'essais passent par un snippet souvent désactivé).
- `auth.enabled: true` et `web.access` posé explicitement → refus (« web.access est imposé à proxy par auth.enabled ;
  le retirer »).
- `auth.enabled: true` avec `ingress.enabled: false` et `externalIngress.enabled: false` → refus (aucune adresse https
  pour le retour OAuth).

### Pod du launcher avec `auth.enabled: true`

| Conteneur | Port | Rôle |
|-----------|------|------|
| `auth-proxy` (oauth2-proxy) | `http` 8080 (seul port du Service) | `--provider=github`, `--github-user=<admins ∪ users>`, `--email-domain=*`, `--upstream=http://127.0.0.1:8081/`, `--http-address=0.0.0.0:8080`, `--reverse-proxy=true`, `--pass-user-headers=true`, `--pass-access-token=false`, `--skip-provider-button=true`, `--api-route=^/api/`, `--skip-auth-route=GET=^/signed-out$`, `--cookie-name=_cl_auth`, `--cookie-secure=true`, `--cookie-samesite=lax`, `--cookie-expire`, `--cookie-refresh`, `--custom-templates-dir=/etc/cl-auth/templates`, `--request-logging=false`, `--auth-logging=true`, `--ping-path=/ping` ; identifiants par `envFrom`/`secretKeyRef` du Secret (`OAUTH2_PROXY_CLIENT_ID`, `_CLIENT_SECRET`, `_COOKIE_SECRET`) ; `runAsNonRoot`, `readOnlyRootFilesystem`, capacités retirées ; sondes `httpGet /ping` |
| `auth-proxy-ext` (oauth2-proxy, **[rev1]** si `externalIngress.enabled`) | `http-ext` 8083 | mêmes arguments que `auth-proxy`, **sans** `--htpasswd-file` ni formulaire ; seule cible de l'Ingress externe |
| `launcher` | `health` 8082 (non exposé par le Service) | `CL_WEB_ACCESS=proxy`, `CL_WEB_ADDR=127.0.0.1:8081`, `CL_HEALTH_ADDR=0.0.0.0:8082` ; sondes `startup` / `liveness` / `readiness` sur `health` |

- ConfigMap `<release>-auth-templates` : `error.html`, `sign_in.html` (`contracts/auth.md` §6), montée en lecture seule.
- Redirection OAuth : `https://<hôte de la requête>/oauth2/callback` (hôte interne et, s'il est activé, hôte externe) ;
  **GitHub App** par environnement (P8 : plusieurs URL de retour), permission « Email addresses : read-only »,
  expiration des jetons utilisateur désactivée. GitHub ne se connecte jamais à ces URL : le retour est une redirection
  du navigateur ; seul oauth2-proxy appelle `github.com` (sortie du pod).
- Compte local activé : `--htpasswd-file` (Secret monté en lecture seule, `defaultMode: 0444` — **[écart justifié]** le chart ne pose jamais `fsGroup` (changement de propriétaire récursif du partage) : un fichier `root:root 0440` serait illisible par l'uid du pod ; le Secret n'est monté que dans le conteneur `auth-proxy`, jamais dans `launcher`), `--display-htpasswd-form=true`,
  `--skip-provider-button=false` ; ConfigMap des gabarits rendue avec ou sans bouton GitHub.
- **[addendum 2]** Règles de rendu : `allowHostBackend: true` avec `auth.enabled: true` → refus (« backend host interdit
  derrière le proxy d'authentification », MAJ-3).
- **[addendum 2]** Middleware Traefik `<release>-headers` sur **les deux** ingress : `customRequestHeaders:
  {Authorization: ""}` (MAJ-1 : chemin Basic-auth fermé) et en-têtes de réponse HSTS, `nosniff`, `Referrer-Policy:
  no-referrer` (m7) — **jamais de CSP** sur `/` (cycle 1, CRIT-2 : elle écraserait le CSP `sandbox` de `/files`) ; le CSP strict
  ne vaut que pour `<release>-headers-oauth` (route `/oauth2`) ; `ipAllowList.ipStrategy.depth: 0` et `rateLimit.sourceCriterion.ipStrategy.depth: 0` (champ valide du CRD Traefik : un `rateLimit.ipStrategy` serait élagué). nginx :
  `proxy_set_header Authorization ""` par l'annotation dédiée du chart. Tests de rendu : middleware référencé par
  chaque ingress.
- **[addendum 2, réserves dev-frontend]** Gabarits : la ConfigMap `<release>-auth-templates` porte `error.html` et
  `sign_in.html` ; la clé `sign_in.html` est rendue depuis `files/auth-templates/sign_in.html` (GitHub + compte local,
  M7 B) ou `files/auth-templates/sign_in.local-only.html` quand `auth.github.enabled: false`. L'instance externe a sa
  propre ConfigMap `<release>-auth-templates-ext` (`sign_in.html` = colonne GitHub seule ; page jamais vue en pratique :
  `--skip-provider-button=true` sur l'externe, faute de formulaire).
- **[addendum 2]** Page `429` : middleware Traefik `<release>-errors` (`errors`, `status: ["429"]`, `service:` le Service
  du launcher port `http`, `query: /too-many`) chaîné **avant** `rateLimit` sur la route `/oauth2/` de chaque ingress (Traefik exécute la chaîne dans l'ordre listé : `headers-oauth`, `errors`, `ratelimit` ; listé après, il ne voit jamais le `429`) ;
  `/too-many` est servie par le launcher sans identité (route exemptée par oauth2-proxy). nginx : `429` brut (corps du
  contrôleur), documenté.
- Variables ajoutées : `CL_AUTH_SESSION_DURATION`, `CL_AUTH_SESSION_REFRESH` (réservées).
- **[addendum 2, m1]** Secrets de cookie distincts : `<release>-auth-cookie` (`_cl_auth`) et `<release>-auth-cookie-ext`
  (`_cl_auth_ext`), générés, `keep`, `lookup`.
- **[addendum 2, m3]** Service : ports `http` (8080) et, si l'externe est activé, `http-ext` (8083) ; **jamais**
  `health` (8082) ni 8081. NetworkPolicy : entrée sur `http` et `http-ext` depuis le contrôleur d'Ingress seulement
  (vérifié en QUALIF depuis un pod projet : 8080, 8082, 8083 refusés).
- NetworkPolicy : inchangée (entrée sur le port `http` depuis le contrôleur d'Ingress) ; sortie non restreinte
  (oauth2-proxy appelle `github.com` et `api.github.com`).
- `NOTES.txt` : adresse(s), « authentification GitHub : N administrateur(s) », nom du Secret attendu ; plus de mention
  « aucune authentification ».

### Ingress externe **[CHANGED]** (#254)

| Valeur | Type | Défaut | Obligatoire | Rôle |
|--------|------|--------|-------------|------|
| `externalIngress.enabled` | booléen | `false` | — | **activable** : `true` avec `auth.enabled: false` → refus (« ingress externe refusé : authentification désactivée (auth.enabled: false) ») |
| `externalIngress.className`, `.host` | chaînes | — | **oui** si activé | classe et nom DNS public |
| `externalIngress.tls.clusterIssuer` \| `.tls.secretName` | chaînes | — | l'un des deux, si activé | émetteur public (ex. `letsencrypt-prod`) ou Secret fourni ; jamais l'émetteur interne `claude-launcher-ca` (contraintes de nom `launcher.local`) — refus si `clusterIssuer` = celui de `ingress.tls` |
| `externalIngress.annotations` | table | `{}` | — | mêmes annotations réservées que `ingress.annotations` |

- Aucune liste d'adresses (accès Internet) ; middlewares : redirection HTTPS, `rateLimit` sur `/oauth2/`.
- **[rev1]** cible : port `http-ext` (instance `auth-proxy-ext` sans compte local) ; `externalIngress.host` dans
  `auth.localAdmin.hosts` → refus au rendu.
- Activé : `CL_WEB_URL=https://<externalIngress.host>` (P7), `CL_WEB_ALT_URLS=https://<ingress.host>` si l'ingress
  interne est aussi activé. Désactivé : aucun objet externe rendu (inchangé).

### Pods projets (backend k8s) **[CHANGED]** (#253)

Manifeste produit par le launcher (`core/k8s.go`), sans valeur du chart :

- **Montages** : plus aucune racine de volume montée entière. Pour chaque PVC de `volumes`, seuls sont montés, en
  `subPath`, au même chemin que dans le launcher : le **dossier du projet** et **`CLAUDE_CONFIG_DIR`** (P5). Jamais
  `<racine>/.config`, `.cache`, `.kube`, les binaires ni les autres projets. Les dossiers « local » (emptyDir) restent
  montés par-dessus le dossier du projet.
- Un chemin à monter hors de tout volume du backend → erreur existante « stockage partagé » (aucun repli sur la racine).
- Le dossier du projet **ou** `CLAUDE_CONFIG_DIR` égal à la racine d'un volume/PVC → refusé (`stockage partagé : … est la
  racine du volume … monter un sous-dossier …, jamais la racine`). Deux racines identiques déclarées avec des PVC
  différents → erreur « deux PVC » (résolution déterministe, pas de choix au hasard).
- **Jeton de ServiceAccount** : `automountServiceAccountToken: false` dans le pod, sauf si `pod.automountServiceAccountToken`
  est posé explicitement dans `backends.yaml` (signalé par le diagnostic « jeton de ServiceAccount monté »).
- Annotation `cl.mounts: "v2"` (modèle de montage). Pod **en marche** sans elle : conservé (sa session continue),
  carte « Montages d'avant v7.0.0 : fermer puis rouvrir le projet pour les restreindre » et ligne du diagnostic ;
  pod arrêté ou à recréer : recréé au nouveau modèle (P6).
- Docker / podman / ssh : inchangés (ils ne montent déjà que le projet et `CLAUDE_CONFIG_DIR`).
