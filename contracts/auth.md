# Contrat — accès authentifié du launcher de cluster (v7.0.0)

> Milestone v7.0.0 « Accès authentifié du launcher (mono-utilisateur) » : #172, #251, #252, #254. **Révision 1
> (GATE 2, 2026-11-25)** : P1–P7 = options recommandées, P8 = interne ET externe, compte local de secours
> `admin@local` (§8). Plan :
> `.claude/workflow/plan-v7.0.0.md`. Décisions de cadrage D2 (oauth2-proxy en sidecar, launcher en boucle locale), D3
> (GitHub OAuth), D4 (plus de jeton derrière le proxy), D6 (ingress externe activable). Les points `P<n>` renvoient au
> plan (GATE 2) : la valeur écrite ici est l'option **recommandée** ; elle sera corrigée si le GATE 2 en retient une autre.
>
> Complète `contracts/web-server.md` (fichiers, version du serveur), `contracts/desktop-web.md` (modes `token` /
> `none`), `contracts/helm-values.md` (chart, section v7.0.0), `contracts/backends-remote.md` (règle R1).
> **Hub Linux, exe Windows et fenêtre native : inchangés** (aucune identité, modes `token` / `none` comme en v6.9.0).

## 1. Zones de confiance

```
navigateur ──https──► Ingress (Traefik) ──http──► pod launcher : [auth-proxy :8080] ──http 127.0.0.1──► [launcher :8081]
                                                                  oauth2-proxy (sidecar)                  ne lit l'identité
                                                                  cookie de session, GitHub OAuth         que de ce proxy
kubelet ──http──► [launcher :8082] /healthz /readyz seulement (aucune autre route, aucune information)
```

- Le launcher n'écoute l'interface **que sur la boucle locale du pod** (`127.0.0.1:8081`) : seul le sidecar, qui partage
  l'espace réseau du pod, peut l'atteindre. Un en-tête d'identité envoyé directement au pod (IP du pod, port `8080`)
  arrive à oauth2-proxy, qui le remplace ; le port `8081` n'est pas joignable hors du pod.
- Port de santé `8082` (`CL_HEALTH_ADDR`) : `GET|HEAD /healthz` et `/readyz` (même réponse qu'aujourd'hui) ; toute autre
  route → `404` sans contenu. La NetworkPolicy ne l'ouvre qu'au kubelet (règle du CNI, comme aujourd'hui).
- Hors du pod, aucun composant ne peut se faire passer pour le proxy : c'est la propriété qui rend l'en-tête de confiance
  acceptable (aucun secret partagé proxy ↔ launcher).

## 2. Mode d'accès `proxy` du launcher **[NEW]**

`CL_WEB_ACCESS=proxy` (nouvelle valeur, à côté de `token` et `none`). Refus de démarrer (message, code ≠ 0) si :

| Condition | Message |
|-----------|---------|
| écoute hors boucle locale (`--addr` / `CL_WEB_ADDR` autre que `127.0.0.1`, `::1`, `localhost`) | « accès proxy : écoute en boucle locale exigée (le proxy d'authentification est le seul client) » |
| `CL_WEB_URL` absente ou non `https` | « accès proxy : adresse publique https exigée (CL_WEB_URL) » |
| `CL_AUTH_ADMINS` vide après validation **et** compte local désactivé | « accès proxy : aucun administrateur (CL_AUTH_ADMINS ou compte local) » |
| `desktop_web.access: proxy` hors cluster (fichier `backends.yaml`) | « accès proxy : réservé au launcher de cluster » — la fenêtre native et le hub n'ont pas ce mode |
| `CL_ALLOW_HOST_BACKEND` posé **[addendum 2, MAJ-3]** | « backend host interdit derrière le proxy d'authentification » : une session tmux dans le conteneur du launcher pourrait joindre `127.0.0.1:8081` et forger l'identité ; le chart refuse aussi `allowHostBackend: true` avec `auth.enabled` |

### Identité d'une requête

- En-tête **`X-Forwarded-User`** posé par oauth2-proxy (`--pass-user-headers`) = **login GitHub**. Aucun autre en-tête
  n'est lu pour l'identité (`X-Forwarded-Email`, `X-Forwarded-Preferred-Username`, `X-Auth-Request-*` ignorés).
- **Casse (cycle QUALIF)** : le launcher compare les logins **sans casse** (forme canonique minuscule) ; en revanche oauth2-proxy compare le
  login GitHub à `--github-user` **en respectant la casse** : le chart transmet donc les logins de `auth.admins` / `auth.users` **tels
  que saisis** (copier le login tel qu'affiché par GitHub) ; une même identité sous deux casses est refusée au rendu.
- Valeur validée : `^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$` (forme d'un login GitHub), comparée **sans casse**
  (forme canonique : minuscules), **ou** exactement `admin@local` (compte local, §8 ; un login GitHub ne contient
  jamais `@`, aucune collision). Plusieurs en-têtes `X-Forwarded-User` → refus.
- **Rôle** : `admin` si le login est dans `CL_AUTH_ADMINS` ou s'il vaut `admin@local` et que le compte local est
  admis pour l'hôte de la requête (§8), `user` s'il est dans `CL_AUTH_USERS` (P2), sinon refus.
  Listes : logins séparés par des virgules, espaces ignorés, entrée invalide → refus de démarrer en la nommant.

| Requête (toutes routes sauf santé et `/signed-out`) | Réponse |
|------------------------------------------------------|---------|
| connexion dont `RemoteAddr` n'est pas en boucle locale **[addendum 2, MAJ-3]** | `403`, `X-Launcher-Refusal: source` (contrôlé à **chaque** requête) |
| `X-Forwarded-User` absent, vide ou invalide | `401`, `X-Launcher-Refusal: identity`, corps « identité absente : ouvrir l'adresse du launcher » |
| login connu d'aucune liste | `403`, `X-Launcher-Refusal: user`, corps « compte GitHub non autorisé » |
| login connu | traitement normal ; contrôles `Host`, `Origin`, `Sec-Fetch-Site`, JSON **inchangés** (règles du mode `none`, hôtes de `CL_WEB_URL` et `CL_WEB_ALT_URLS`) |

- **Aucun jeton, aucune session ni cookie du launcher** en mode `proxy` : la session est celle d'oauth2-proxy (cookie
  `_cl_auth`, `HttpOnly`, `Secure`, `SameSite=Lax`, propre à l'hôte). `CL_DESKTOP_TOKEN` est ignoré, le fichier
  `desktop-web-token` d'un mode `token` précédent est supprimé (comme en `none`). `/?t=…` → `303` vers `/`.
- Clé du plafond de débit (`secretReveal`, futurs plafonds) : `user:<login>` (au lieu de la source en `none`).
- Accès distant (`remote`, #227/#250) : une requête en mode `proxy` est **toujours distante** (`remote: true`) : les
  refus distants existants (`secretReveal`, œil de `configModel`, `teamAgentSet`, réglages d'exposition web) restent
  appliqués, même à un administrateur.
- `/files/...` (#174) : identité exigée comme les autres routes ; règles de `contracts/web-server.md` inchangées.
- Sortie de démarrage : adresse publique + « Accès authentifié (oauth2-proxy, GitHub) : N administrateur(s), M
  utilisateur(s) » — jamais la liste des logins, jamais de jeton.

- **Risque accepté (MAJ-5)** : l'identité est le **login** GitHub, qui est renommable. Un administrateur qui renomme
  ou supprime son compte libère le login ; un tiers qui le recrée passerait `--github-user` et `CL_AUTH_ADMINS`.
  Consigne (guide, `NOTES.txt`) : **retirer le login de `auth.admins` / `auth.users` avant de renommer ou supprimer son
  compte GitHub** ; l'id numérique de chaque admin est consigné (`PRIVATE/GLOBAL/`). Engagement v7.1.0 :
  `CL_AUTH_ADMIN_IDS` (id numérique, source à étudier avec oauth2-proxy).
- **Comparaison d'hôtes (m2)** : `Host` (et hôtes de `CL_WEB_URL`, `CL_WEB_ALT_URLS`, `CL_AUTH_LOCAL_ADMIN_HOSTS`) mis
  en minuscules, port retiré, point final retiré, puis **égalité stricte** avec la liste ; valeur illisible → refus.

### Adresses publiques

- `CL_WEB_URL` : adresse **principale** (annoncée aux sessions claude par `CL_BROWSER_PATH_MAP` / `CL_BROWSER_HINT`,
  lien de `NOTES.txt`). Avec l'ingress externe activé, c'est l'hôte externe (P7) ; sinon l'hôte interne.
- `CL_WEB_ALT_URLS` **[NEW]** : autres adresses publiques admises (virgules), mêmes règles que `CL_WEB_URL` (https, sans
  chemin) ; leurs hôtes passent `Host` et leur origine exacte passe `Origin`. Valeur invalide → refus de démarrer.
  Posée par le chart : l'hôte interne quand l'externe est principal.

### Déconnexion et page `/signed-out`

- Lien de déconnexion : `/oauth2/sign_out?rd=%2Fsigned-out` (relatif : même hôte). oauth2-proxy efface son cookie puis
  redirige vers `/signed-out`.
- `GET /signed-out` : page **statique** du launcher (aucune donnée, aucun appel `/api`), servie sans identité (route
  exemptée par le proxy : `--skip-auth-route=GET=^/signed-out$`, et `GET=^/too-many$`). Texte (maquette M2) : déconnecté du launcher ; la
  session GitHub du navigateur reste ouverte ; bouton « Se reconnecter » (`/`). En-têtes de sécurité habituels.
- La session GitHub n'est pas fermée (hors de portée du launcher) : « Se reconnecter » peut aboutir sans saisie. Dit
  par la page.

### Session expirée

- oauth2-proxy répond `401` (et non une redirection) aux routes `^/api/` (`--api-route`), `302` vers GitHub ailleurs.
- Client web : un `401` sur `/api/call`, ou un flux `/api/events` qui échoue suivi d'un `clientInfo` en `401`, affiche le
  bandeau « Session expirée — Se reconnecter » (maquette M1) ; « Se reconnecter » recharge `/`. Les terminaux ouverts
  restent affichés (figés) jusqu'au rechargement ; aucune frappe n'est envoyée pendant l'expiration.

## 3. Appels `init` et `clientInfo` **[CHANGED]**

`init` (instantané) et `clientInfo` portent, **en mode `proxy` seulement** :

```json
"user": { "login": "ccoupel", "role": "admin", "logout": "/oauth2/sign_out?rd=%2Fsigned-out" }
```

- Absent dans les autres modes et dans la fenêtre native (le client n'affiche alors ni identité ni déconnexion).
- `role` : `admin` | `user`. Le client grise les actions réservées (§4) pour `user`, infobulle « réservé à un
  administrateur » (maquette M1) ; le serveur refuse de toute façon.

### Précisions client (addendum 2, réserves dev-frontend 4421a97) **[CHANGED]**

- **Durée de session** : `user.session = { "duration": "8h", "refresh": "1h" }` (valeurs de `auth.sessionDuration` /
  `auth.sessionRefresh`, transmises par `CL_AUTH_SESSION_DURATION` / `CL_AUTH_SESSION_REFRESH`). Pas d'heure
  d'expiration : le cookie d'oauth2-proxy est opaque pour le launcher. Le menu affiche « session : 8 h, renouvelée
  chaque heure ». `user.kind` : `github` | `local` (ligne « Connecté avec GitHub » / « Compte local » du menu).
- **Capacités** : `init` et `clientInfo` portent **dans tous les modes** du serveur web un objet `access`, seule source
  du grisage (le client ne code aucune liste en dur) :

```json
"access": { "mode": "proxy", "role": "user",
            "deny": ["configSave:config.conf", "configSave:env.yaml", "configSave:hub", "configApply:config.conf",
                     "configApply:env.yaml", "configApply:hub", "backendSecretSet", "backendSecretDelete",
                     "backendKnownHostsSet", "relaunch", "login", "quit", "webUIOpen", "webUIRegen", "teamAgentSet",
                     "secretReveal"],
            "backendsAdminFields": true }
```

  - `mode` : `proxy` | `none` | `token` ; absent pour la fenêtre native (aucun grisage). `role` : `admin` | `user` en
    `proxy`, absent sinon.
  - `deny` : méthodes refusées à **ce** client (`méthode` ou `méthode:<kind de config>`), calculées par le serveur à
    partir du classement §4 ; vide pour un administrateur (hors refus « en proxy »). En `none`, `deny` contient toutes
    les méthodes R1. Le client grise un contrôle dont la méthode (ou méthode:kind) est dans `deny`.
  - `backendsAdminFields` : vrai si le client ne peut pas changer les champs R1 de `backends.yaml` : le client grise ces
    champs dans les Réglages (liste des champs : constante du contrat §4, exposée aussi par `configModel` via
    `field.adminOnly: true`).
- **`GET /signed-out`** : page **embarquée dans le binaire** (`ui/desktop/frontend/signed-out.html`, servie par le
  routeur Go, sans identité, en mode `proxy` seulement ; `404` dans les autres modes).
- **`GET /too-many`** **[NEW]** : page statique embarquée « Trop de tentatives — réessayer dans une minute » (style M2
  A), servie sans identité en mode `proxy` (`--skip-auth-route=GET=^/too-many$`), cible de la page d'erreur `429`
  de Traefik (`contracts/helm-values.md` « v7.0.0 »).

## 4. Règle R1 — serveur et options globales **[CHANGED addendum 2]** (#252)

> **Décision utilisateur (addendum 2, remplace P4)** : « un user a toute liberté sur ses projets, backends… ; l'admin
> gère le serveur et les options globales ». R1 (rôle `admin`) = **serveur et options globales uniquement**.

### Classement des méthodes (`ng2/ui/desktop/app.go` dispatch, `secrets.go`, `reveal.go`)

| Classe | Méthodes | `admin` | `user` | mode `none` (web) |
|--------|----------|---------|--------|-------------------|
| **R1** — serveur, options globales | `configSave` / `configApply` sur `config.conf`, `env.yaml` (launcher), `hub` ; `configSave` / `configApply` sur `backends.yaml` **quand un champ R1 change** (ci-dessous) ; `backendSecretSet`, `backendSecretDelete`, `backendKnownHostsSet` ; `relaunch` ; `login` | oui | **non** | non |
| **Projets et backends** (`user` libre) | `create`, `createCancel`, `projectSettings`, `projectApply`, `execApply`, `envTrustAnswer`, `initProposalAnswer`, `projectMove`, `projectMoveResume`, `projectMoveAbort`, `categoryCreate`, `configSave` / `configApply` sur `project-env` et sur `backends.yaml` hors champs R1, `backendTest`, `backendHostScan`, `open`, `reattach`, `refreshProject`, `forceClose`, `forceCloseConfirm`, `close`, `closeConfirm`, `detach`, `refresh`, `rescan`, `teamToggle`, `relayout`, `select`, `cardsFor`, `setPrefs`, `setTheme`, `dismiss`, `alert` | oui | oui | oui |
| **Lecture** | `init`, `clientInfo`, `diag`, `tools`, `newInfo`, `projectMoveInfo`, `envTrustInfo`, `projectBases`, `projectCategories`, `configFiles`, `configOpen`, `configValidate`, `configModel`, `backendStates`, `launcherInfo`, `backendSecretGet` | oui | oui | oui |
| **Terminal** | `termInput`, `launchInput`, `resizePane`, `termResize`, `termFocus`, `termZoom`, `termSync` | oui | oui | oui |
| **Refusé en mode `proxy`** (tous rôles) | `quit` (MAJ-2 : arrêt du serveur), `webUIOpen`, `webUIRegen`, `clipboardRead`, `clipboardWrite` (indisponibles), `teamAgentSet`, `secretReveal` (refus distant existant) | non | non | inchangé v6.9.0 |

- **Arguments JSON (cycle 1, CRIT-1)** : un objet `args` portant deux clés égales **à la casse près** (à toute profondeur) est
  refusé (`400` sur `/api/call`) ; la décision R1 lit les arguments avec le même décodeur que l'exécution.
- **`backendTest` (cycle 1)** : un `user` teste un backend **déjà déclaré** tel quel (champs R1 et de connexion `host`, `hosts`,
  `pool`, `engine`, `engine_host` identiques) ou un **nouveau** backend sans champ R1 ; sinon R1 (refus).
- **Échec fermé** : une méthode absente de ce classement est traitée comme **R1**. Un test énumère les `case` du
  dispatch (et `secretMethods`) et échoue si une méthode n'est pas classée (MAJ-2).
- **Champs R1 de `backends.yaml`** (empreinte `AdminFieldsChanged(old, new)`, échec fermé : YAML illisible = changé) :
  toutes les rubriques hors `backends:` (`desktop_web`, `browser_server`, `browser_paths`, `browser_device`, `hubs`,
  défauts globaux) ; et, pour chaque backend : `kubeconfig`, `context`, `volumes`, `namespace`, `type: host`, et sous
  `pod:` les clés `serviceAccountName`, `automountServiceAccountToken`, `securityContext`, `nodeName`,
  `runtimeClassName`, `hostAliases`, `dnsConfig`, `dnsPolicy`, `tolerations`, `priorityClassName`, `schedulerName`,
  `imagePullSecrets` [cycle 1, m2]. Raison : ces clés donnent au pod d'un projet le
  ServiceAccount du launcher (droits d'administration du namespace → `pods/exec` dans le pod du launcher → coffre et
  falsification d'identité), un volume autre que le projet, ou la machine hôte. Un `user` crée, modifie et supprime
  les autres champs (image, `shared`, `host` ssh, ressources, placement, `spread`…). `WebExposureChanged` (exposition
  web, kubeconfig) reste refusé à **tout** client web, admin compris.
- Refus : `{ "ok": false, "forbidden": "admin", "msg": "réservé à un administrateur authentifié (règle R1)" }`, sans
  `error` ; rien n'est écrit ; journalisé (§5, `ok: false`, `err: "forbidden"`, `target`).
- **Journal des secrets** `<CacheDir>/secrets-journal.jsonl` : champ **`user`** ajouté.

### Secrets des backends (coffre) : **R1 (admin)** en v7.0.0 — décision proposée

- En v7.0.0 les backends et le coffre sont **communs** (un seul `backends.yaml`, un dossier par backend, aucune notion
  de propriétaire avant v7.1.0) : « ses backends » n'existe pas encore. Un `user` qui remplacerait la clé ssh ou le
  `known_hosts` d'un backend utilisé par les projets de l'administrateur détournerait ces projets vers une machine de
  son choix (homme du milieu, chaîne M2 de v6.4.0) : écriture réservée à l'admin (`backendSecretSet`,
  `backendSecretDelete`, `backendKnownHostsSet`).
- Un `user` peut **utiliser** les secrets existants (déclarer ou modifier un backend hors champs R1, `backendTest`,
  `backendHostScan` sans écriture, `backendSecretGet` sans contenu) et ouvrir ses projets dessus.
- Impact sécurité : depuis LOT-1 (#253) le coffre n'est plus visible des pods projets ; un `user` ne peut donc ni le
  lire depuis un terminal ni le modifier depuis l'interface. Il deviendra « ses backends » (secrets par utilisateur,
  côté agent) en v7.1.0 (D8 du cadrage).

### Pouvoir d'un `user` (limite assumée, m9)

R1 n'est **pas** une frontière d'exécution : un `user` obtient des terminaux dans les pods projets, choisit l'image, le
backend et l'URL de clone, approuve un `env.yaml` — il exécute donc du code **dans les pods de projets**, y lit
`GH_TOKEN` (`settings.json` de `CLAUDE_CONFIG_DIR`) et les identifiants claude montés. Ce pouvoir est **borné** par :
LOT-1 (#253 : ni coffre, ni `backends.yaml`, ni `.kube`, ni autres projets, ni jeton de ServiceAccount dans les pods),
les champs R1 ci-dessus (pas de ServiceAccount ni de volume arbitraire), l'interdiction du backend `host` derrière le
proxy (§2, MAJ-3). `termInput` n'est qu'agrégé dans l'audit (P3). À rappeler dans le guide.

## 5. Journal d'audit **[NEW, CHANGED addendum 2]** (#251)

Fichier `<CacheDir>/audit.jsonl` (hors du coffre, `0600`, écrit en ajout, une ligne JSON par entrée) **et**, pour
chaque entrée, la même ligne préfixée `audit ` sur la **sortie standard du conteneur `launcher`** (collecte Kubernetes,
non effaçable par l'application — MAJ-4). Mode `proxy` seulement.

```json
{"ts":"2026-12-01T10:00:00.123Z","user":"ccoupel","role":"admin","src":"10.42.0.7 (X-Forwarded-For: 192.168.1.20)","method":"configSave","target":"backends.yaml","ok":true}
{"ts":"2026-12-01T10:01:00.000Z","user":"ccoupel","kind":"agg","method":"termInput","count":312,"since":"2026-12-01T10:00:00Z"}
{"ts":"2026-12-01T10:02:00.000Z","kind":"deny","login":"bob-test","code":403,"count":4,"since":"2026-12-01T10:01:00Z"}
```

- **Une entrée par appel** (`/api/call`), sauf : (a) les 13 méthodes **fréquentes** agrégées par (utilisateur, méthode)
  et par minute (P3 : `termInput`, `termResize`, `termFocus`, `termSync`, `termZoom`, `resizePane`, `launchInput`,
  `cardsFor`, `select`, `init`, `clientInfo`, `backendStates`, `setPrefs`), et les lectures `/files` (`method:
  "files"`) ; (b) **toute** méthode au-delà de **60 appels par minute et par utilisateur** (entrée `agg`) — les appels
  **R1** ne sont **jamais** agrégés. Flux `/api/events` : une entrée à l'ouverture.
- **Refus d'identité** (`401`, `403` du launcher) : entrée agrégée par minute `kind: "deny"` (`login` tenté, tronqué à 64
  caractères, ou absent ; `code`, `count`), rien d'autre.
- `project` : **nom** du projet (forme de `/files`), jamais un chemin interne. `target` (méthodes R1 et refus R1) :
  identifiant court — `kind` de config (`config.conf`, `backends.yaml`…), nom de backend — **jamais une valeur**. Aucun
  autre argument (ni contenu, ni commande, ni frappe, ni cookie).
- `ok` / `err` : `err` court (`forbidden`, `invalid`, `error`).
- **Cycle 1 (MAJ-A)** : `method` borné à 64 caractères (inconnue : `unknown`), `target` et `project` à 128 ; les **refus** R1 sont
  soumis à la rafale de 60/min (agrégés au-delà), jamais les succès ; une action R1 est précédée d'une ligne d'**intention**
  (`"phase":"intent"`, journal inaccessible = refus) puis de sa ligne de résultat ; `src` garde la **queue** du
  `X-Forwarded-For`. Le flux `/api/events` du mode proxy se ferme après `CL_AUTH_SESSION_REFRESH` (le client se reconnecte).
- `src` : adresse de la connexion + `X-Forwarded-For` **tronqué à 256 octets**, caractères de contrôle retirés
  (indication, non fiable). Écriture par `encoding/json` **uniquement** (jamais de concaténation).
- Rotation : au-delà de 10 Mio, rotation sur **5 générations** (`audit.jsonl.1` … `.5`, 50 Mio au total) ; la
  conservation est celle de la rotation (et de la collecte des journaux du pod). Données personnelles : login + adresse
  source, acceptable en mono-utilisateur.
- **Échec d'écriture** du fichier : méthodes R1 **refusées** (`{ok:false, msg:"journal d'audit inaccessible"}`) ; les
  autres continuent (la sortie standard reste écrite) avec un avertissement unique.
- Connexions et déconnexions : journal d'oauth2-proxy (`--auth-logging`).
- Jamais servi par `/files` (dossier d'état du launcher).

## 6. Pages d'oauth2-proxy (chart)

Gabarits personnalisés en français (`--custom-templates-dir`, ConfigMap du chart), maquette M2 :

- `error.html` : « Accès refusé » (compte GitHub non autorisé : login affiché si connu, « Se déconnecter » → `/oauth2/sign_out?rd=%2Fsigned-out`), et erreurs génériques (code + message court).
- `sign_in.html` (maquette M7, retenue **B**) : **affichée** dès que le compte local est activé
  (`--skip-provider-button=false`) : deux colonnes de même poids, « Compte GitHub » (bouton « Se connecter avec
  GitHub », absente si `auth.github.enabled: false`) et « Compte local » (formulaire identifiant / mot de passe, absente
  sur l'instance de l'hôte externe) ; une seule colonne présente → carte simple. Compte local désactivé : `--skip-provider-button=true` (redirection directe vers
  GitHub, page jamais vue). Message d'échec unique « Identifiant ou mot de passe incorrect ».
- `error.html` : aussi la page « Trop de tentatives — réessayer plus tard » (`429` de Traefik, page d'erreur d'Ingress).
- Aucune ressource externe, aucun script ; CSS inline. **Gabarits (m7)** : uniquement `{{ .X }}` auto-échappé (jamais
  `safeHTML`), paramètre `rd` échappé en attribut ; `/signed-out` et pages du proxy : CSP `default-src 'none';
  style-src 'unsafe-inline'; form-action 'self'`, en-têtes HSTS, `frame-ancestors 'none'`, `nosniff`,
  `Referrer-Policy: no-referrer` (middleware Traefik `headers` sur les deux ingress).

## 7. Variables posées dans le pod **[NEW]**

| Variable | Valeur (chart, `auth.enabled: true`) |
|----------|---------------------------------------|
| `CL_WEB_ACCESS` | `proxy` |
| `CL_WEB_ADDR` | `127.0.0.1:8081` (lue par `entrypoint.sh` ; défaut `0.0.0.0:8080`, comportement de v6.9.0) |
| `CL_HEALTH_ADDR` | `0.0.0.0:8082` (vide : pas de port de santé séparé, sondes sur l'écoute principale comme avant) |
| `CL_WEB_URL` | `https://<hôte principal>` |
| `CL_WEB_ALT_URLS` | `https://<autre hôte>` si l'ingress externe est activé |
| `CL_AUTH_ADMINS` | `auth.admins` joints par des virgules |
| `CL_AUTH_USERS` | `auth.users` joints par des virgules (vide si absent) |
| `CL_AUTH_SESSION_DURATION`, `CL_AUTH_SESSION_REFRESH` | `auth.sessionDuration`, `auth.sessionRefresh` (affichage seulement) |
| `CL_AUTH_LOCAL_ADMIN_HOSTS` | hôtes où `admin@local` est admis (défaut : l'hôte interne `ingress.host` ; vide : compte local refusé partout) |

Toutes réservées : `extraEnv` ne peut pas les poser (liste `$reservedEnv` du chart étendue).

## 8. Compte administrateur local de secours `admin@local` **[NEW]** (rev1, exigence utilisateur GATE 2)

**Mise en œuvre retenue : le fichier htpasswd d'oauth2-proxy**, pas un second chemin d'authentification dans le
launcher. Raisons : le launcher reste un simple lecteur de l'identité posée par le proxy (une seule source, propriété
§1 conservée) ; aucun code de mot de passe, de session ni de cookie à écrire et auditer dans le launcher ; même cookie,
même durée, même déconnexion, même page d'expiration que GitHub ; bcrypt vérifié par oauth2-proxy (code éprouvé).
Contrepartie : oauth2-proxy n'a pas de limite d'essais propre → limite de débit Traefik (ci-dessous) et compte local
**inaccessible depuis Internet par construction**.

- oauth2-proxy : `--htpasswd-file=/etc/cl-auth/local/htpasswd` (une seule ligne `admin@local:<bcrypt>`),
  `--display-htpasswd-form=true`, `--skip-provider-button=false`. Identité transmise : `X-Forwarded-User: admin@local`.
- **Hôte externe** (#254) : servi par une **seconde instance** d'oauth2-proxy (`auth-proxy-ext`, port `http-ext`
  8083) **sans** `--htpasswd-file` : le formulaire n'existe pas sur Internet et aucun mot de passe n'y est vérifié
  (force brute impossible). Défense en profondeur côté launcher : `admin@local` refusé (`403`, `X-Launcher-Refusal:
  user`) si l'hôte de la requête (`Host`, transmis par le proxy, `--pass-host-header`) n'est pas dans
  `CL_AUTH_LOCAL_ADMIN_HOSTS` (P10).
- **Limite d'essais** : `rateLimit` Traefik sur `/oauth2/` de l'ingress interne (10/min en moyenne, 20 en rafale,
  par adresse source, `sourceCriterion.ipStrategy.depth: 0`) → `429`, page `errors` listée avant `rateLimit` ; bcrypt coût ≥ 10. L'ingress interne garde sa liste
  d'adresses (LAN).
- **Chemin Basic-auth fermé (MAJ-1)** : avec `--htpasswd-file`, oauth2-proxy accepte aussi `Authorization: Basic` sur
  toute route, hors limite de débit. Le chart pose sur **les deux** ingress un middleware Traefik `headers`
  `customRequestHeaders: { Authorization: "" }` (en-tête retiré avant le proxy) : le formulaire, limité, reste le seul
  chemin. Arguments explicites (m4) : `--pass-basic-auth=false`, `--pass-authorization-header=false`,
  `--set-xauthrequest=false`, `--skip-auth-strip-headers=true`, `--redirect-url=https://<hôte de l'instance>/oauth2/callback`
  (fixe), `--whitelist-domain` vide, pas de `--metrics-address`.
- **Cookies distincts (m1)** : `auth-proxy` (`_cl_auth`, Secret `<release>-auth-cookie`) et `auth-proxy-ext`
  (`__Host-_cl_auth_ext`, Secret `<release>-auth-cookie-ext`) : un cookie de l'interne n'ouvre pas de session sur l'externe.
  Le compte local est refusé sur l'externe **par construction (proxy sans htpasswd) et par le launcher** (garde `Host`).
- **Droits du fichier htpasswd** : Secret monté en `0444` dans le seul conteneur `auth-proxy` (pas de `fsGroup` dans le pod, pour ne pas faire changer de propriétaire le partage ; `0440` serait illisible par l'uid du pod) ; le conteneur `launcher` ne le monte pas.
- **Mot de passe** (P11) : généré par le chart à la première installation (32 caractères aléatoires,
  `randAlphaNum`), haché en bcrypt (`htpasswd` de Helm) dans le Secret `<release>-local-admin` (clés `password` et
  `htpasswd`, `helm.sh/resource-policy: keep`, réutilisé par `lookup` aux mises à jour). Lecture :
  `kubectl get secret <release>-local-admin -o jsonpath='{.data.password}' | base64 -d` (donnée dans `NOTES.txt`,
  jamais la valeur). Variante : `auth.localAdmin.existingSecret` (clé `htpasswd` seule, bcrypt `$2y$` coût ≥ 10, refus
  au rendu sinon) — aucun mot de passe en clair dans le cluster ; force à la charge de l'utilisateur (documentée).
- Jamais dans `values`, git, la sortie du pod, `NOTES.txt` ni l'audit (le journal porte `user: "admin@local"`, jamais
  le mot de passe ; oauth2-proxy `--auth-logging` journalise l'identifiant et le succès/échec, pas le mot de passe).
- **Rôle** : `admin` (R1 permise), quel que soit `auth.admins`. Audit (§5) et journal des secrets : `admin@local`.
- **Désactivable** : `auth.localAdmin.enabled` (défaut P9). Désactivé : aucun formulaire, `--htpasswd-file` absent,
  `CL_AUTH_LOCAL_ADMIN_HOSTS` vide.
- **Sans GitHub** (`auth.github.enabled: false`, permis seulement avec le compte local) : oauth2-proxy démarre avec un
  client factice (`--client-id=disabled`), le bouton GitHub n'est pas rendu, `auth.existingSecret` n'est pas exigé.
  Sert à la QUALIF avant la création des applications GitHub, et de secours.
- Rotation : supprimer les Secrets `<release>-local-admin` **et** `<release>-auth-cookie` puis `helm upgrade` (nouveau
  mot de passe, sessions en cours invalidées) — m5c.
- `existingSecret` : ligne bcrypt `$2a$`, `$2b$` ou `$2y$`, coût ≥ 10 (le `htpasswd` de Helm produit `$2a$`) ; mot de
  passe **≥ 20 caractères aléatoires** (non vérifiable sur un haché : consigne du guide).
- Risques acceptés (m5) : le mot de passe généré figure aussi dans le Secret de release Helm (`sh.helm.release.v1.*`),
  lisible par qui lit les Secrets du namespace ; `lookup` est vide en `helm template` / GitOps → le mot de passe change
  à chaque rendu dans ce cas (documenté).
- Session sans état (m6) : un cookie reste valide jusqu'à son expiration ; la révocation est la **liste du launcher**
  (`CL_AUTH_*`, relue à chaque requête, changée par `helm upgrade`) ou la rotation du `cookie-secret`.

## 9. Accès externe par sous-chemin (`externalIngress.pathPrefix`) **[NEW addendum 4]** (#254)

> Décision utilisateur : QUALIF publiée sous `https://coupel.net/claude-qualif` (sous-chemin d'un hôte partagé avec
> ~15 applications publiques), et non sous un sous-domaine (recommandation `infra`). Préfixe vide = comportement
> précédent (sous-domaine, chemin `/`).

### Chaîne (le préfixe n'est **jamais retiré** par Traefik)

```
navigateur ─ https://coupel.net/claude-qualif/… ─► Traefik : Host(coupel.net) && PathPrefix(/claude-qualif)
   ─► auth-proxy-ext :8083 (--proxy-prefix=/claude-qualif/oauth2, cookie Path=/claude-qualif)
   ─► launcher 127.0.0.1:8081 /claude-qualif/…  (base de l'hôte externe = /claude-qualif, retirée par le launcher)
```

- Pas de `StripPrefix` : oauth2-proxy et le launcher voient le même chemin que le navigateur (redirections, `rd`, liens
  et `Location` cohérents sans `X-Forwarded-Prefix`, qui n'est pas lu).
- **Launcher** : `CL_WEB_URL=https://coupel.net/claude-qualif` (l'adresse publique **admet désormais un chemin** :
  segments `^/[a-z0-9-]+(/[a-z0-9-]+)*$`, sans `/` final). La **base** d'une requête est le chemin de l'adresse publique
  dont l'hôte correspond (`CL_WEB_URL`, `CL_WEB_ALT_URLS`) ; hôte interne : base vide. Une requête de l'hôte externe hors
  de sa base → `404` ; `<base>` exactement (sans `/`) → `308` vers `<base>/`. Le launcher retire la base avant son routeur
  (`/api/call`, `/api/events`, `/files/…`, `/signed-out`, `/too-many`, ressources), et la remet dans **toute** URL qu'il
  émet : `Location` (`/?t=` → `<base>/`), `user.logout`
  (`<base>/oauth2/sign_out?rd=<base>%2Fsigned-out`), liens des pages `/signed-out` et `/too-many`, `CL_BROWSER_PATH_MAP`
  (`<CL_WEB_URL>/files/<projet>`) et `CL_BROWSER_HINT`.
- **Client web** : **chemins relatifs uniquement** (`api/call`, `api/events`, `files/…`, ressources `vendor/…`) et
  `<base href="<base>/">` injecté par le launcher dans `index.html` (base de l'hôte de la requête ; `/` en interne).
  Aucun chemin absolu (`/api…`, `location.href = '/'`) ; « Se reconnecter » = `location.href = document.baseURI`. Les
  URL venues du serveur (`user.logout`) sont utilisées telles quelles. Pas de WebSocket (SSE seulement).
- **Contrôles** : `Origin` attendu = origine de l'adresse publique (`https://coupel.net`, sans chemin) ; `Host` normalisé
  (m2) **et** préfixe ; `Sec-Fetch-Site` inchangé.
- **oauth2-proxy externe** : `--proxy-prefix=<base>/oauth2`, `--redirect-url=https://coupel.net<base>/oauth2/callback`,
  `--cookie-path=<base>`, `--cookie-name=__Secure-cl_auth_ext` (préfixe `__Secure-` : `Secure` exigé ; **pas**
  `__Host-`, qui impose `Path=/`), `--cookie-samesite=lax` (retour OAuth en navigation de premier niveau), CSRF
  d'oauth2-proxy sous le même chemin, `--skip-auth-route=GET=^<base>/(signed-out|too-many)$`. GitHub App : URL de retour
  `https://coupel.net/claude-qualif/oauth2/callback`.
- Pages `signed-out`, `too-many`, `sign_in`, `error` : liens relatifs à la base (variables de gabarit d'oauth2-proxy
  `.ProxyPrefix` pour ses pages).

### Sécurité de l'origine partagée (point d'attention de l'utilisateur)

| Menace | Mitigation | Reste |
|--------|------------|-------|
| (a) Cookie : `__Host-` impossible ; dépôt de cookie (« cookie tossing ») par une autre application de `coupel.net` sur `Path=/claude-qualif` (fixation de session, connexion forcée sur le compte de l'attaquant) | `__Secure-` + `Path=/claude-qualif` + `SameSite=Lax` + `HttpOnly` ; liste d'autorisation du launcher (un compte non listé est refusé) | Une application de `coupel.net` compromise peut écraser ou effacer le cookie (déni, connexion sur un compte autorisé seulement) — **risque accepté** |
| (b) CSRF / XSS depuis une autre application de la même origine vers `/claude-qualif/api/call` | `Origin`, `Sec-Fetch-Site`, corps JSON, CSP `frame-ancestors 'none'` arrêtent un **autre site** | Une **XSS dans n'importe quelle application de `coupel.net`** s'exécute dans la **même origine** : elle envoie le cookie (le chemin n'est pas une frontière de sécurité), lit les réponses et pilote le launcher (terminaux = exécution de code dans les pods projets, réglages si la victime est admin). **Aucune mitigation robuste** dans le navigateur — **risque accepté (P12)** ou sous-domaine |
| (c) `/files` | CSP `sandbox` (origine opaque) inchangée : une maquette ne lit ni cookie ni `/api` | — |
| (d) Fuite vers les autres applications | Cookie `Path=/claude-qualif` jamais envoyé à `/emby`… ; le launcher ne journalise jamais `Cookie` (les cookies des autres applications, `Path=/`, lui parviennent) | — |
| (e) Capture des autres chemins de `coupel.net` | Ingress `PathPrefix` limité au segment `/claude-qualif` (pas de `/` ni de joker), refus de rendu d'un `pathPrefix` vide avec un hôte déjà servi n'est pas vérifiable par Helm → consigne + test QUALIF (`/emby` inchangé) | — |
| HSTS | **Non posé** quand `pathPrefix` est non vide (il s'appliquerait à tout `coupel.net`, dont des applications en `http`) (P13) | — |
