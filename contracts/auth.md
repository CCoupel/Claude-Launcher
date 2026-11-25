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

### Identité d'une requête

- En-tête **`X-Forwarded-User`** posé par oauth2-proxy (`--pass-user-headers`) = **login GitHub**. Aucun autre en-tête
  n'est lu pour l'identité (`X-Forwarded-Email`, `X-Forwarded-Preferred-Username`, `X-Auth-Request-*` ignorés).
- Valeur validée : `^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$` (forme d'un login GitHub), comparée **sans casse**
  (forme canonique : minuscules), **ou** exactement `admin@local` (compte local, §8 ; un login GitHub ne contient
  jamais `@`, aucune collision). Plusieurs en-têtes `X-Forwarded-User` → refus.
- **Rôle** : `admin` si le login est dans `CL_AUTH_ADMINS` ou s'il vaut `admin@local` et que le compte local est
  admis pour l'hôte de la requête (§8), `user` s'il est dans `CL_AUTH_USERS` (P2), sinon refus.
  Listes : logins séparés par des virgules, espaces ignorés, entrée invalide → refus de démarrer en la nommant.

| Requête (toutes routes sauf santé et `/signed-out`) | Réponse |
|------------------------------------------------------|---------|
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
  exemptée par le proxy : `--skip-auth-route=GET=^/signed-out$`). Texte (maquette M2) : déconnecté du launcher ; la
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

## 4. Règle R1 — écritures sensibles **[NEW]** (#252)

Méthodes **R1** (P4, liste recommandée) : `configSave`, `configApply`, `projectSettings`, `projectApply`, `execApply`,
`envTrustAnswer`, `initProposalAnswer`, `backendSecretSet`, `backendSecretDelete`, `backendKnownHostsSet`,
`backendHostScan`, `relaunch`.

| Client | R1 autorisée |
|--------|--------------|
| fenêtre native (hub, exe) | oui (inchangé) |
| client web du hub / de l'exe, mode `token` (boucle locale, jeton du propriétaire) | oui (inchangé — le jeton désigne le propriétaire du poste) |
| client web du cluster, mode `proxy`, rôle `admin` | oui |
| client web du cluster, mode `proxy`, rôle `user` | **non** |
| client web, mode `none` (cluster sans authentification, `auth.enabled: false`, P1) | **non** : configuration et secrets en lecture seule depuis l'interface |

- Refus : résultat `{ "ok": false, "forbidden": "admin", "msg": "réservé à un administrateur authentifié (règle R1)" }`,
  sans `error` ; rien n'est écrit. Le refus est journalisé (§5, `ok: false`, `err: "forbidden"`).
- `backendSecretGet` reste permis (il ne rend jamais de contenu).
- **Journal des secrets** `<CacheDir>/secrets-journal.jsonl` : champ **`user`** ajouté (login, ou `fenêtre native`,
  ou `web local`) à chaque écriture, à côté de la source existante.
- **`WebExposureChanged`** : l'empreinte inclut désormais la clé `kubeconfig` de chaque backend (chemin ou `true`) : un
  client web, même administrateur, ne la modifie pas par `configSave` / `configApply` (message existant
  `errWebDesktopWeb`, étendu au kubeconfig). Le dépôt d'un kubeconfig dans le coffre (`backendSecretSet {kubeconfig}`)
  reste permis à un administrateur.

## 5. Journal d'audit **[NEW]** (#251)

Fichier `<CacheDir>/audit.jsonl` (hors du coffre, `0600`, écrit en ajout, une ligne JSON par entrée). Mode `proxy`
seulement (rien n'est écrit en `token`, `none` ni par la fenêtre native).

```json
{"ts":"2026-12-01T10:00:00.123Z","user":"ccoupel","role":"admin","src":"10.42.0.7 (X-Forwarded-For: 192.168.1.20)","method":"configSave","project":"cl-claude-launcher","ok":true}
{"ts":"2026-12-01T10:01:00.000Z","user":"ccoupel","kind":"agg","method":"termInput","count":312,"since":"2026-12-01T10:00:00Z"}
```

- **Une entrée par appel** (`/api/call`) pour toutes les méthodes, sauf les méthodes **fréquentes**, agrégées par
  (utilisateur, méthode) et par minute (`kind: "agg"`, `count`) — P3 : `termInput`, `termResize`, `termFocus`,
  `termSync`, `termZoom`, `resizePane`, `launchInput`, `cardsFor`, `select`, `init`, `clientInfo`, `backendStates`,
  `setPrefs` ; et les lectures `/files` (`method: "files"`). Flux `/api/events` : une entrée à l'ouverture
  (`method: "events"`).
- `project` : **nom** du projet dans le launcher (forme de `/files`), jamais un chemin interne, présent si l'appel porte
  un projet ; aucun autre argument n'est écrit (ni contenu, ni valeur, ni commande, ni frappe).
- `ok` / `err` : `err` court (`forbidden`, `invalid`, `error`), jamais le message complet.
- Rotation : au-delà de 10 Mio, `audit.jsonl` → `audit.jsonl.1` (une génération gardée).
- **Échec d'écriture** du journal : les méthodes R1 sont **refusées** (`{ok:false, msg:"journal d'audit inaccessible"}`,
  comme le journal des secrets) ; les autres appels continuent, un avertissement unique part sur la sortie d'erreur.
- Connexions et déconnexions : journal d'oauth2-proxy (`--auth-logging`, sortie du conteneur `auth-proxy`) ; le
  launcher ne les déduit pas.
- Jamais servi par `/files` (dossier d'état du launcher, règle existante).

## 6. Pages d'oauth2-proxy (chart)

Gabarits personnalisés en français (`--custom-templates-dir`, ConfigMap du chart), maquette M2 :

- `error.html` : « Accès refusé » (compte GitHub non autorisé : login affiché si connu, « Se déconnecter » → `/oauth2/sign_out?rd=%2Fsigned-out`), et erreurs génériques (code + message court).
- `sign_in.html` (maquette M7, retenue **B**) : **affichée** dès que le compte local est activé
  (`--skip-provider-button=false`) : deux colonnes de même poids, « Compte GitHub » (bouton « Se connecter avec
  GitHub », absente si `auth.github.enabled: false`) et « Compte local » (formulaire identifiant / mot de passe, absente
  sur l'instance de l'hôte externe) ; une seule colonne présente → carte simple. Compte local désactivé : `--skip-provider-button=true` (redirection directe vers
  GitHub, page jamais vue). Message d'échec unique « Identifiant ou mot de passe incorrect ».
- `error.html` : aussi la page « Trop de tentatives — réessayer plus tard » (`429` de Traefik, page d'erreur d'Ingress).
- Aucune ressource externe, aucun script ; CSS inline.

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
  par adresse source) → `429` ; bcrypt coût ≥ 10. L'ingress interne garde sa liste d'adresses (LAN).
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
- Rotation : supprimer le Secret `<release>-local-admin` puis `helm upgrade` (nouveau mot de passe) ; sessions en cours
  valides jusqu'à leur expiration (cookie) — documenté.
