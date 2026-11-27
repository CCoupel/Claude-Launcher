# Changelog des contrats

> Changements de contrats (API, schemas) : `BREAKING` | `CHANGED` | `ADDED`. Lu par le CDP au GATE 2.

## [20261127] — v7.1.0 LOT-3.1 (#257) : `backendSecret*` dans l'espace de l'utilisateur

- **[CHANGED]** `auth.md` §4 : `backendSecretSet` / `backendSecretDelete` **hors R1** pour un `user` sur les backends de son
  `backends.yaml`, limités à `users/<uid>/vault/<backend>/` ; coffre commun (admin), `backendKnownHostsSet` et
  `secretReveal` restent R1 / refusés. `user-backends.md` §7 aligné.

## [20261127] — v7.1.0 LOT-3.1 (#257) : bornage des hôtes, kubeconfig obligatoire, limites

- **[CHANGED]** `user-backends.md` §3 : backend d'un utilisateur = **Internet public seulement** (loopback, link-local,
  métadonnées, plages privées et réservées, noms internes refusés ; contrôle à l'enregistrement et avant chaque appel,
  cache DNS 30 s) ; admin sans contrainte. Backend k8s d'un utilisateur : `kubeconfig: true` obligatoire (jamais le
  ServiceAccount du launcher). Environnement des appels nettoyé, erreurs filtrées, `proxy-url` refusé, kubeconfig ≤ 64 Kio.
  `pod.affinity` admis. Audit `backend.call`.
- **[NEW]** Helm : `users.maxBackends` (défaut 10), `users.backendTestsPerMinute` (défaut 6).

## [20261127] — v7.1.0 LOT-2 (#255) : `backends.yaml` d'un utilisateur en liste blanche, conflit de version

- **[CHANGED]** `user-backends.md` §3 : validation en **liste blanche**, échec fermé : rubrique `backends:` seule ; `type` ∈
  {k8s, docker, podman} ; docker/podman : `host` obligatoire en `tcp://` (`https://` non admis), ni pool, hosts, engine,
  engine_host, shared, volumes, context ; k8s : `namespace` libre, pas de `host` ; ssh et host refusés. Remplace le
  refus par liste noire (contournable par un pool ou un `host` absent, revue LOT-2 cycle 2).
- **[CHANGED]** `user-backends.md` §3 : `mtime` absent avec un fichier existant = conflit (« rouvre l'éditeur »).

## [20261127] — v7.1.0 LOT-2 (#255) : `auth.admins` vide permis avec `admin@local`

- **[CHANGED]** `helm-values.md` : `auth.admins` peut être vide quand `auth.localAdmin.enabled` (admin de fait, hôte
  interne seul) ; refus conservé si GitHub activé + admins vide + compte local inactif ; `NOTES.txt` le signale.
- **[CHANGED]** `auth.md` §2 : sans administrateur GitHub, les actions R1 ne se font que depuis l'hôte interne par
  `admin@local` ; un login GitHub reste `user`. Enrôlement (demande, validation, profil) : v7.2.0 (#278, #261, #247).
- **[CHANGED]** `helm-values.md` : sans `auth.admins` ni `auth.users`, `--github-user=-` (login impossible) au lieu d'une
  valeur vide (= tout compte GitHub pour oauth2-proxy) ; l'ouverture à tout compte GitHub viendra en v7.2.0, avec le
  registre des comptes actif (#278).

## [20261126] — v7.1.0 LOT-2 (#255) : formes livrées, `userList` en R1

- **[CHANGED]** `auth.md` §4 : `userList` classée **R1** (admin). Instance d'un utilisateur : méthodes R1 → refus R1,
  méthodes non servies → « indisponible dans votre espace » ; champs R1 de son propre `backends.yaml` sans `namespace`
  (libre, #257), liste complète inchangée pour le `backends.yaml` de l'admin.
- **[CHANGED]** `user-backends.md` §9 : formes livrées de `init` / `clientInfo` (`space = {admin, login, backends[{name,
  type}], empty, error?}`), méthodes servies par l'instance d'un utilisateur, SSE par identité, `/files` → `404` pour
  un utilisateur en LOT-2 ; §11 : `userList → {users: [{login, backends, state}]}` ; §2 : cache login → id (24 h),
  repli sur le dernier id connu, `503` sans id connu.

## [20261126] — v7.1.0 (plan rév. 3, modèle sans agent, réponses GATE 2) : espace par utilisateur, backends de l'utilisateur

- **[NEW]** `contracts/user-backends.md` :
  - espace par utilisateur dans l'état du launcher (`users/<uid>/` : backends, coffre en clair, réglages globaux,
    trace minimale des projets vus) ; **aucune donnée projet côté launcher** ;
  - projets **découverts** sur les backends (labels `cl.user`) ; état d'un projet arrêté gardé sur le backend
    (annotation k8s `cl.meta`) ; F5 démarre si besoin ; pas d'arrêt automatique ; projet disparu visible en erreur avec
    « Purger » (trace seulement) ; docker/podman arrêté : état lu sans démarrer (fichier dans le volume, API archive) ;
  - approbation `env.yaml` par projet **dans le volume du projet**, scellée par HMAC (clé par utilisateur côté launcher) ;
  - identifiants **collés** dans l'éditeur de backends actuel (kubeconfig ; TLS docker/podman : CA du serveur, certificat
    et clé client), validation PEM, masqués après l'envoi ; jamais de fichier ni de chemin local ; compte k8s trop
    puissant = avertissement ;
  - backends `k8s` (kubeconfig, namespace, droits vérifiés) et `docker`/`podman` (`tcp://`, TLS activable, vérification
    désactivable, avertissement « root sur l'hôte ») ;
  - stockage = PVC / volume **nommé par l'utilisateur** (aucun stockage créé par le launcher) ;
  - jeton GitHub PAT ou OAuth ; routage par identité ; backend injoignable.
- **[CHANGED]** mode serveur : **ssh refusé pour tous** (écarté au chargement avec diagnostic, refusé à l'enregistrement) ;
  mode poste inchangé. Aucun backend ssh en QUALIF ni en PROD au 2026-11-26.
- **[NEW]** méthodes web `userBackendAdd`, `userBackendRemove`, `githubTokenMode`, `projectPurge`, `userList` ; méthodes existantes
  limitées à l'espace de l'identité.
- **[NEW]** Helm : `users.enabled`, `auth.passAccessToken`.
- **[CHANGED]** identité : clé = identifiant numérique GitHub (login résolu par l'API GitHub, login repris → refus).
- **[CHANGED]** client web : SSE, terminaux et `/files` limités à l'espace de l'identité ; `/files` d'un projet arrêté → `404`.
- **[CHANGED]** `/files` : `_work/test-plan/` servi comme `_work/mockup/` (#271).
- **[CHANGED]** diagnostic k8s : un pod projet en marche à l'ancien modèle (sans `cl.mounts`) est un **avertissement**,
  plus un problème bloquant du backend (#269) ; Fermer et Forcer fermer arrêtent ces pods.
- Aucun BREAKING de contrat : le pod launcher garde la racine, `users.enabled: false` rend le comportement v7.0.0.
  Comportement : un backend ssh de la config partagée n'est plus utilisé par le launcher de cluster (aucun en service).

## [20261125] — v7.0.0 (correctif cycle 1 LOT-2, audit sécurité final et revue)

- **[CHANGED]** audit `auth.md` §5 : toute action **R1** autorisée est précédée d'une ligne d'**intention** (`"phase":"intent"`,
  champs `ts`, `user`, `role`, `src`, `method`, `target`, `ok: true`), puis de sa ligne de résultat habituelle (sans `phase`) ;
  journal inaccessible : l'action est refusée. Refus R1 soumis à la rafale de 60/min ; `method` ≤ 64 (`unknown` si inconnue),
  `target` et `project` ≤ 128 ; `src` garde la queue du `X-Forwarded-For` ; flux `/api/events` fermé après
  `CL_AUTH_SESSION_REFRESH`.
- **[CHANGED]** `auth.md` §4 : arguments JSON à clés égales (casse comprise) refusés ; `backendTest` : backend déclaré tel
  quel ou nouveau sans champ R1, sinon R1 ; clés de pod R1 : + `tolerations`, `priorityClassName`, `schedulerName`,
  `imagePullSecrets`.
- **[CHANGED]** chart : aucune CSP sur la route `/` (préserve le `sandbox` de `/files`) ; `ingress.controller: nginx` refusé avec
  le compte local.

## [20261126] — v7.0.0 (addendum 4, LOT-3) : accès public par sous-chemin `coupel.net/claude-qualif`

- **[NEW]** `externalIngress.pathPrefix` (validé, vide = comportement précédent) ; `CL_WEB_URL` admet un chemin (base) ;
  le launcher retire et réinjecte la base (routes, `Location`, `user.logout`, `/files` des sessions) ; `<base href>`
  injecté dans `index.html`.
- **[CHANGED]** client web : chemins relatifs uniquement (plus de `/api/…` ni `location.href = '/'`).
- **[CHANGED]** oauth2-proxy externe : `--proxy-prefix`, `--cookie-path`, cookie `__Secure-cl_auth_ext_<préfixe en tirets>`, `--redirect-url`
  avec préfixe ; HSTS non posé avec un préfixe.
- **[ADDED]** risques de l'origine partagée (`contracts/auth.md` §9) : XSS d'une autre application de `coupel.net` =
  contrôle du launcher (P12), dépôt de cookie.

## [20261125] — v7.0.0 (addendum 3, LOT-3) : identifiants GitHub en valeurs Helm

- **[NEW]** `auth.github.clientId` / `auth.github.clientSecret` : le chart crée le Secret `<release>-github` ; exclusif
  avec `auth.existingSecret` (conservé, recommandé) ; GitHub activé sans aucun des deux → refus. Valeurs réservées à
  `PRIVATE/<ENV>/values-local.yaml` ; avertissement `helm get values` / `helm template` ; contrôle `scripts/ci.sh` contre
  un `clientSecret` dans un fichier suivi par git — `helm-values.md` « v7.0.0 ».

## [20261125] — v7.0.0 (addendum 2) : R1 recentrée sur le serveur, corrections de sécurité (revue du modèle de menace)

- **[BREAKING]** (par rapport au plan) R1 = **serveur et options globales** (décision utilisateur) : `config.conf`,
  `env.yaml` du launcher, `hub`, champs R1 de `backends.yaml` (rubriques globales ; `kubeconfig`, `context`, `volumes`,
  `namespace`, `type: host`, clés `pod.` de ServiceAccount, sécurité et placement d'hôte), écriture du coffre,
  `relaunch`, `login`. Un `user` garde `create`, `projectSettings`, `projectApply`, `execApply`, `envTrustAnswer`,
  `initProposalAnswer`, `projectMove*`, `categoryCreate`, `backendHostScan`, le reste de `backends.yaml`. `quit` refusé en
  mode `proxy`. Méthode non classée = R1 (test) — `contracts/auth.md` §4.
- **[CHANGED]** refus de démarrage et de rendu : mode `proxy` + `CL_ALLOW_HOST_BACKEND` ; `RemoteAddr` en boucle locale
  contrôlé à chaque requête ; comparaison d'hôtes normalisée (m2).
- **[CHANGED]** audit : doublé sur la sortie standard, 5 générations de 10 Mio, agrégation au-delà de 60 appels/min hors
  R1, entrées `deny` (401/403), `target` court, `src` borné à 256 octets, `encoding/json` seul — `auth.md` §5.
- **[CHANGED]** chart : middleware `headers` (retrait d'`Authorization`, en-têtes de sécurité), `ipStrategy` profondeur 0,
  cookies et Secrets distincts par instance (`__Host-_cl_auth_ext`, sans sous-chemin), Service/NetworkPolicy `http-ext`, `allowHostBackend`
  refusé avec l'auth, bcrypt `$2a|$2b|$2y$` ; arguments oauth2-proxy explicites (m4) — `helm-values.md` « v7.0.0 ».
- **[NEW]** `init.access` / `clientInfo.access` `{mode, role, deny[], backendsAdminFields}` (grisage reçu du serveur,
  plus de liste en dur), `configModel` `adminOnly`, `user.session {duration, refresh}`, `user.kind` ; routes Go
  embarquées `GET /signed-out` et `GET /too-many` ; chart : `sign_in.local-only.html` monté comme `sign_in.html` si
  GitHub désactivé, ConfigMap externe distincte, middleware `errors` 429 → `/too-many` (réserves dev-frontend 4421a97).
- **[ADDED]** risques acceptés écrits : login GitHub renommable (MAJ-5, `CL_AUTH_ADMIN_IDS` en v7.1.0), mot de passe local
  dans le Secret de release Helm, session non révocable avant expiration, pouvoir d'exécution d'un `user` (m9).

## [20261125] — v7.0.0 (addendum LOT-2) : estimation d'épuisement des quotas (#268, bugfix)

- **[CHANGED]** `contracts/usage-levels.md` « Calcul » : projection calculée à chaque mise à jour du compteur, session
  et hebdomadaires, sans délai d'attente ; garde-fous techniques seuls (`p` ≤ 0, temps écoulé ≤ 0, `resets_at`
  invalide/passé) ; échantillon court (< 30 min session, < 3 h hebdo, ou `p` < 3 %) → estimation **approximative** sans
  couleur (plus de rouge sur un échantillon court). `exhausts_at` n'est plus `null` sur un échantillon court.
- **[NEW]** champ `approx` (booléen) de `windows[]` (schéma reste `1` : ajout rétrocompatible, absent = `false`) ;
  `UsageRow.approx` et `UsageRow.estimate` (« ≈ épuisement ~HH:MM ») pour l'interface (`contracts/desktop-dispatch.md`).
- Consommateur (panneau du template #160) : un ancien panneau voit `exhausts_at` renseigné avec `level` = `null` ; il
  n'affiche pas de couleur (comportement inchangé).

## [20261125] — v7.0.0 (plan rev1, GATE 2) : compte local de secours, GitHub App, ingress externe sans compte local

- **[NEW]** compte `admin@local` (rôle admin) par le fichier htpasswd d'oauth2-proxy : `auth.localAdmin.enabled`
  (défaut vrai, P9), mot de passe généré (Secret `<release>-local-admin`, P11) ou `auth.localAdmin.existingSecret`
  (bcrypt), `auth.localAdmin.hosts` (hôte interne seul, P10) ; `CL_AUTH_LOCAL_ADMIN_HOSTS` ; page de connexion
  affichée (M7) — `contracts/auth.md` §8.
- **[NEW]** `auth.github.enabled` (false permis avec le compte local) ; `cookie-secret` généré par le chart
  (`auth.existingSecret` ne porte plus que `client-id` / `client-secret`).
- **[CHANGED]** P8 : GitHub App (plusieurs URL de retour) au lieu d'une OAuth App ; second sidecar `auth-proxy-ext`
  (port 8083, sans compte local) derrière l'ingress externe.
- **[CHANGED]** `auth.admins` obligatoire seulement si `auth.github.enabled`.

## [20261125] — v7.0.0 (plan) : accès authentifié du launcher de cluster (#172, #251, #252, #253, #254)

- **[BREAKING]** chart : `auth.enabled` **vrai par défaut** (P1) — `auth.existingSecret` (OAuth App GitHub) et
  `auth.admins` deviennent obligatoires ; une installation v6.x mise à jour sans eux échoue au rendu
  (`contracts/helm-values.md` « v7.0.0 »).
- **[BREAKING]** mode serveur authentifié : plus de jeton ni de cookie du launcher, identité par oauth2-proxy (sidecar),
  launcher en boucle locale `127.0.0.1:8081`, sondes sur `8082` ; nouveau mode `CL_WEB_ACCESS=proxy`
  (`contracts/auth.md` §2). `web.access` posé avec `auth.enabled` → refus.
- **[BREAKING]** règle R1 : 12 méthodes d'écriture refusées (`{ok:false, forbidden:"admin"}`) pour un client web
  `proxy` de rôle `user` et pour tout client web en `none` (cluster sans authentification = réglages en lecture seule
  depuis l'interface) ; la clé `kubeconfig` entre dans `WebExposureChanged` (`contracts/auth.md` §4,
  `contracts/backends-remote.md` « v7.0.0 »).
- **[BREAKING]** pods projets k8s : seuls le dossier du projet et `CLAUDE_CONFIG_DIR` montés (`subPath`), plus de jeton
  de ServiceAccount (`automountServiceAccountToken: false`) ; annotation `cl.mounts: "v2"` ; pod en marche conservé
  jusqu'à sa réouverture (P5, P6).
- **[CHANGED]** `externalIngress.enabled` activable (refusé si `auth.enabled: false`, émetteur interne refusé) ;
  `CL_WEB_URL` = hôte externe s'il est activé, `CL_WEB_ALT_URLS` = hôte interne (P7).
- **[NEW]** `init.user` / `clientInfo.user` `{login, role, logout}` ; réponse `401` de session expirée ; page
  `/signed-out` ; journal `<CacheDir>/audit.jsonl` (méthodes fréquentes agrégées par minute, P3) ; champ `user` du
  journal des secrets ; variables `CL_WEB_ADDR`, `CL_HEALTH_ADDR`, `CL_WEB_ALT_URLS`, `CL_AUTH_ADMINS`, `CL_AUTH_USERS` ;
  valeurs `auth.*`, ConfigMap des gabarits oauth2-proxy, `rateLimit` sur `/oauth2/`.
- Hub Linux, exe Windows, fenêtre native : **aucun changement**.

## [20261125] — v6.9.0 (plan, révisé GATE 2) : modèle et effort des teammates (#250)

- **[NEW]** méthode `teamAgentSet` `{path, agent, model, effort}` : surcharge écrite dans le frontmatter du compagnon
  `.claude/agents/<agent>.md` (clé retirée si égale au template, reste du fichier conservé octet pour octet, compagnon
  créé si absent, supprimé s'il devient vide) ; **refusée en accès distant** (`{ok:false, remote:true}`) ; effort
  contrôlé par la table de compatibilité ; teamleader : écrit aussi `model` / `effortLevel` de `.claude/settings.json`
  (valeurs résolues, réécriture textuelle minimale) — `contracts/desktop-dispatch.md` « v6.9.0 »
- **[NEW]** méthode `clientInfo` → `{remote}` ; **[NEW]** table `core.ModelEfforts`, exposée par `teamHead.modelEfforts`
  (P6 = table de la documentation de l'API) ; valeur `"default"` de `model` / `effort` = entrée « Défaut (template) »
  des menus (clé retirée, jamais grisée)
- **[CHANGED]** `TeamRow` : `model` / `effort` = valeur **résolue** pour tous les teammates (avant : modèle réel du spawn
  pour un teammate démarré) ; ajout de `agent`, `modelSrc`, `effortSrc`, `tplModel`, `tplEffort`, `running`, `editable`.
  Les clients qui affichaient `model` comme « modèle réel » affichent désormais la valeur résolue (`running` porte ce
  qui tourne) — rétrocompatible pour la forme, changement de sens assumé (décision P3).
- **[NEW]** sous-commande `claude-launcher-ng2 team-set <projet> <agent> [model=…] [effort=…]` (coque tmux, tests)
- Aucun BREAKING.

## [20261123] — v6.8.1 (plan) : quotas mutualisés, versions sans F5 (#237, #238)

- **[ADDED]** `contracts/usage-levels.md` (#237) : `$CLAUDE_CONFIG_DIR/claude-launcher-cache/usage-levels.json`, schéma 1
  (`computed_at`, `source_mtime`, `windows[]` : `kind`, `scope` brut, `resets_at`, `percent`, `exhausts_at`, `level`,
  `alert`), écrit atomiquement après chaque relevé et à la lecture si `source_mtime` ≠ mtime de `usage.json` ;
  consommé par le panneau milestone (template#160). `usage.json` inchangé.
- **[CHANGED]** seuil de l'orange (`resetLevel` = `yellow`) : épuisement prévu < **1 h** après le renouvellement pour la
  session 5 h, < **5 h** pour les hebdomadaires (`weekly_all`, `weekly_scoped`) — au lieu de 6 h pour toutes ; rouge,
  vert et « non fiable » inchangés. Même calcul pour la coque, la fenêtre native, `--web` et le fichier.
- **[CHANGED]** `contracts/template-version.md` §3 (#238) : cycle de vérification GitHub 2 min avec jeton (1 min tant
  qu'une release est attendue), **5 min sans jeton** (2 min si attendue ; décision GATE 2 : risque de 429 anonyme accepté) ; le résultat partagé n'est réutilisé que s'il
  a moins que le cycle de l'appelant − 10 s (au lieu de 10 min fixes) et la vérification suivante est calée sur son
  âge. Annonce poussée au client web par l'évènement `state` existant (pas de changement côté client).
- Aucun BREAKING.

## [20261123] — v6.8.0 : quotas colorés (#235)

- **[ADDED]** `Snapshot.usage[]` (`UsageRow`) : `resetLevel` (`red` | `yellow` | `green`, absent = projection non fiable ou données périmées) et `alert` (« ⚠ épuisé ~HH:MM », présent seulement si `resetLevel` = `red`) — à afficher sur `reset` (couleur de la date, texte d'alerte en rouge) ; calcul unique `core.ProjectExhaustion` (identique à `lib.ts` du template 0.6.0). Aucun BREAKING.

## [20261122] — v6.7.0 (plan) : fiabilisation, /start-session, teamleaders en attente (#220–#223, #231)

- **[NEW]** `contracts/attention.md` (#223) : hooks Claude Code `# cl-attention` dans `settings.json` (PreToolUse
  AskUserQuestion, Notification permission → attente ; PostToolUse, UserPromptSubmit, Stop, SessionEnd → fin),
  `CL_ATTENTION_DIR` exportée dans le script de démarrage, état `<projet>/.claude-launcher/attention/<pane>.json`
  (exclu de git), `Row.attention` / `TabInfo.attention`, évènements `attention` / `attentionEnd`, option
  `ATTENTION_NOTIFY`, notifications Windows (Wails) et navigateur ; instantané `attentionNotify` (absent = activé, suit
  le rechargement à chaud) lu par le client web ; évènement `attentionFocus` `{path, id}` (clic sur une notification
  Windows → onglet du projet sélectionné)
- **[CHANGED]** (65ca574, revue LOT-3) fin d'attente sur `PostToolUse` seulement pour une permission ou la fin de
  `AskUserQuestion` (limite : outil parallèle pendant une permission) ; coque : `ReadAttentionOf(leader de la fenêtre)`,
  leader introuvable → pas d'attente (limite : claude mort, pane resté → état jusqu'à Stop/UserPromptSubmit/SessionEnd ou 24 h)
- **[CHANGED]** (4b70f8f) boucle d'attention de la fenêtre native / `--web` : 1 s au lieu de 2 s (seuil ≤ 5 s inchangé)
- **[NEW]** option `SHARE_WRITE_TIMEOUT` (#220) ; `projectApply` / bascule d'organisation : `{ok:false, timeout:true, msg}`
  après le délai, une seule écriture abandonnée par fichier — `contracts/desktop-dispatch.md` « v6.7.0 »
- **[NEW]** question `/start-session` dans le script de démarrage d'un projet géré **sans team existante** (#231),
  toujours après `/init-project` (décision GATE 2) ; `core.TeamExists`
- Décisions GATE 2 : #220 retour à l'organisation précédente ; #223 question + permission, `ATTENTION_NOTIFY=1` par
  défaut, maquette A
- **[CHANGED]** message de repli « leader en haut » une fois par projet et par cause (#222)
- **[CHANGED]** `App.Stop` : échéance unique de 25 s, terminaux attendus (goroutines, processus), `termIn` refusé après
  l'arrêt (#221) ; arrêt forcé d'un terminal au bout de `detachWait` (5 s, plafonné à échéance − 1 s), écart
  accepté en revue
- Aucun BREAKING.

## [20261121] — v6.6.0 LOT-3 (complément #216) : env.yaml d'un projet refusé s'il tourne

- **[CHANGED]** écriture de l'`env.yaml` d'un projet (`projectApply.env`, `configSave` / `configApply` sur `project-env`, F2 coque) refusée tant qu'un blocker #216 s'applique (ouvert ici, autre launcher, conteneur actif), revérifié à l'écriture : `{ "ok": false, "field": "env", "msg", "blockers" }` ; `configValidate` permis — `contracts/desktop-dispatch.md` « env.yaml d'un projet refusé… »
- **[NEW]** `projectSettings.envLocked` / `envLockReason` (indicatifs, sans interroger les moteurs) ; `configOpen` (`project-env`) : `readOnly`, `readOnlyReason` — pas `configModel` (sans modèle structuré pour `project-env`, écart accepté)
- **[CHANGED]** (décision utilisateur) backend injoignable et projet non ouvert ici : écriture d'`env.yaml` **autorisée** avec `warn` (« état du conteneur inconnu… ») ; refus seulement sur blocker confirmé ; déplacement **et backend / image** restent fail-closed (refus) ; « non vérifiable » = délai dépassé **ou** échec rapide du backend (conteneur seulement : un projet sur l'hôte n'est jamais « non vérifiable ») ; contrôle complet hors verrou à l'écriture puis revérification rapide sous verrou sur l'état mémorisé (fenêtre courte non couverte, admise) — 60ae84f
- Exceptions : création (`create`), approbation (`envTrustAnswer`), étape `trust` de `projectMove` — jamais bloquées

## [20261121] — v6.6.0 LOT-4.2 : corrections de revue (#226, #227)

- **[CHANGED]** `note` de `configSave` / `configApply` par type : retiré pour `config.conf` (remplacé par `effect` / `apply`), **conservé** pour `backends.yaml`, `hub`, `env.yaml` (launcher) et `project-env` tant que leurs pastilles n'existent pas — `contracts/desktop-dispatch.md` « v6.6.0 — LOT-4 »
- **[CHANGED]** `configModel` : `reveal` toujours présent (`true|false`) pour une entrée secrète, `false` explicite en accès distant

## [20261121] — v6.6.0 LOT-4 (plan) : config à chaud, effet des options, éléments cachés (#225, #226, #227)

- **[CHANGED]** `config.conf` relu par date avant toute recopie dans `settings.json` et toute action qui en dépend ; `configApply` / `configSave` / `configReload` appellent `SyncLauncherEnv` (champ `sync`) ; coque : relecture à chaud sans relancer le nav — `contracts/desktop-dispatch.md` « v6.6.0 — LOT-4 »
- **[NEW]** `configModel.settings[].effect` (`immediate|project|claude|launcher`) et `effectHow` (bulle : `why`, `do`, `shell`, `action` — remplace `effectHelp`, maquette #226 A révisée) ; `configApply.apply` (`changed`, `relaunch`, `projects`) — **[CHANGED]** `ConfigReloadNote` retiré
- **[NEW]** `secretReveal {kind, key}` (maquette #227 A, H7/H8 validées) : clés `secret` seulement, jamais journalisée, refusée depuis un accès web distant, `no-store`, 10/min ; `configModel` : `reveal` par entrée secrète

## [20261120] — v6.6.0 (plan) : gestion des projets (#214 à #219)

- **[NEW]** `core.ResolveProjectPath` : base ∈ racines configurées, catégorie relative sans `..`, contrôle après liens symboliques — appliquée par `create`, `categoryCreate`, `projectMove` (et la coque tmux) — `contracts/desktop-dispatch.md` « v6.6.0 »
- **[NEW]** `projectBases`, `projectCategories`, `categoryCreate`, `createCancel`, instantané `newJob`, évènement `initProposal` / `initProposalAnswer`
- **[CHANGED]** `create` : `base` = racine configurée seulement (+ `category`, `newCategory`), `source` `empty|git|template`, `url`, asynchrone pour `git`, nettoyage complet sur échec — un appel v6.5.0 avec `base` = dossier de catégorie est refusé (interface et backend livrés ensemble, aucun client externe)
- **[CHANGED]** (amendement #215, 2026-11-21) `create` : l'interface n'émet plus `raw` (ignoré s'il est reçu) ; `source: empty` = « Projet brut » : `git init` + `remote` s'il est fourni, sans marqueur `init-proposed` ni `/init-project` ni CLAUDE.md minimal ; `template` = `git init` + `remote` facultatif + `/init-project` proposé
- **[CHANGED]** `newInfo` : `bases`, `default`, `auth` (présence du jeton / des clés, jamais leur valeur), `template` ; `roots` gardé (retrait en v7)
- **[CHANGED]** (#216, 3a36ba7) `projectMove` / reprise / annulation : verrou **par projet** (source et destination, `moves-locks/`) en plus du verrou du journal, refus « un déplacement est déjà en cours pour ce projet » ; blockers revérifiés avant `dir` et avant `ctr` ; copie entre systèmes de fichiers durable (fsync) et contrôlée par **SHA-256 par fichier** avant suppression de la source (au lieu de « nombre et taille »), dates et liens recopiés ; `moves.json` illisible → tout déplacement refusé, jamais réécrit ; `trust.json` illisible ne bloque que les projets à environnement ; `impacts[].label` aussi sur `conf` (clé partagée par deux projets de même nom)
- **[CHANGED]** (précision #216, revue LOT-3) annulation / retour arrière : `config.conf` rétabli à l'équivalent sémantique (mêmes clés et valeurs), pas à l'octet près (ordre, forme `["clé"]`, commentaire de fin de ligne)
- **[NEW]** (amendement #216, 2026-11-21, écart signalé ec0fd75) `projectMoveResume` / `projectMoveAbort {id}` (« Reprendre » / « Annuler » un déplacement inachevé de `moves.json`) ; **[CHANGED]** `projectMoveInfo` : destination facultative → `dest` (clé recalculée `key`, `keyChanged`, `crossDevice`, `exists`, `impacts` du tableau « ce qui change ») et `pending` (entrée du journal : `id`, `from`/`to`, `fromKey`/`toKey`, `step`, `done`, `msg`)
- **[NEW]** `projectMoveInfo`, `projectMove` (journal `moves.json`, retour arrière, état partiel repris)
- **[NEW]** `launcherInfo` (sans `stale` ni `backup`), `relaunch` ; coque : `ctrl+r` contextuel (relance si l'annonce est affichée, rescan sinon) ; `FindStaleProcs` filtré sur `argv[0]` = nom du binaire, `argv[1]` ∈ {mirror, layout-watch}, `CL_INNER_SOCKET` du launcher
- **[CHANGED]** sauvegarde `<exe>.bak-v<version>` avant mise à jour (une seule gardée) ; écriture atomique de `config.conf` (retentée sous Windows), balayage des `.config.conf.tmp-*`
- **[NEW]** (amendement #215, audit LOT-2.2) approbation de l'environnement d'un projet cloné : registre `trust.json` (dossier de config, jamais dans le projet), `core.EnvTrust`, `LoadEnv` refuse `pending` et réduit `ignored` au socle choisi, évènement `envTrust`, `envTrustInfo`, `envTrustAnswer` (sha contrôlé), `projectSettings.envTrust`, `projectApply.envTrust: "review"` — **[CHANGED]** `open` : `{needTrust}` pour un projet cloné non approuvé en conteneur ; choix **définitif** (pas de re-demande après un pull — H4), revu par « Revoir » des réglages
- **[RETIRÉ]** (révision GATE 2, jamais publié) `rollback`, `update-skip`, champ `stale` de l'instantané : retour arrière = procédure manuelle documentée

## [20261119] — v6.5.0 (plan rev2) : coffre partagé (#201), masquage par projet (#209)

- **[BREAKING]** coffre des secrets **commun** au hub, à l'exe Windows et au pod, en clair (`0700`/`0600`) sous `<CL_ROOT_BASE>/.config/claude-launcher-ng2/secrets/` ; refus « sous la racine partagée » **supprimé** ; secrets de projet = `<projet>/PRIVATE/` (jamais écrits par le launcher) ; migration automatique des coffres v6.4.0, ancien conservé — `contracts/backends-remote.md` « Coffre des secrets »
- **[BREAKING]** chart : `CL_SECRETS_DIR` n'est plus posé ; PVC `<release>-secrets` monté seulement pour la migration (`secrets.legacy.enabled`, `CL_SECRETS_LEGACY_DIR`) ; init `secrets-perms` supprimé — `contracts/helm-values.md` « v6.5.0 »
- **[CHANGED]** `kubeconfig.path` admis dans le coffre global ou un `PRIVATE/` de projet
- **[NEW]** `PROJECT_HIDE_IDLE` (`config.conf`), `projectApply.hideIdle` / `projectSettings.hideIdle` (`auto|default|none`), `teamHead.hideIdle.mode` et `.global` — **[CHANGED]** `teamHead.hideIdle.enabled` = valeur effective du projet (identique pour `default`) — `contracts/desktop-dispatch.md` « v6.5.0 »

## [20261118] — v6.4.0, LOT-5 : budget d'arrêt (#198)

- **[CHANGED]** arrêt du launcher borné à **25 s au pire** (ouvertures 20 s + détachements des terminaux 5 s) ; schéma du chart : `terminationGracePeriodSeconds` `minimum: 30` — l'écart signalé le 2026-11-18 est résolu — `contracts/web-server.md` « Cohérence avec le délai de grâce du pod »

## [20261118] — v6.4.0, LOT-4 : `/files/<projet>/<chemin relatif>` (#174, décision utilisateur au GATE LOT-4)

- **[BREAKING]** (interne au milestone, aucune version publiée) : `/files/<chemin interne complet>` **supprimé** ; forme unique `/files/<projet>/<chemin relatif>`, `<projet>` = nom du projet dans le launcher, projets connus seulement (inconnu → `404`) ; `/files/` sans liste (`404`) ; premiers segments en `__` réservés (appariement `/files/__claude-launcher__/pair`) — `contracts/web-server.md` « Fichiers des projets par l'interface »
- **[CHANGED]** `CL_BROWSER_PATH_MAP` / `CL_BROWSER_HINT` : une correspondance par session, racine du projet → `<CL_WEB_URL>/files/<projet>` ; serveur local 8765 du hub et de l'exe inchangé

## [20261118] — v6.4.0, LOT-4 : CSP de `/files`, sources de la NetworkPolicy (#174, #200)

- **[CHANGED]** `/files` : `Content-Security-Policy: sandbox allow-scripts allow-forms allow-modals allow-popups; frame-ancestors 'none'` au lieu de `sandbox` seul (scripts des maquettes exécutés dans une origine opaque, jamais `allow-same-origin`) — `contracts/web-server.md` (commit ffcfc72, dev-backend ; avis security en cours)
- **[NEW]** chart : `networkPolicy.ingressCIDRs` (sources `ipBlock` en plus des namespaces ; rendu refusé sans namespace ni CIDR) — `contracts/helm-values.md` « v6.4.0 » (commit 441f093, infra)

## [20261118] — v6.4.0 : appel `reattach` (#198)

- **[NEW]** `reattach {path}` → `{ok, tab}` : rattache l'onglet à une session tmux survivante après un redémarrage du serveur, sans rien créer ; appelé par le client en repli au bout d'environ 5 s (abandon à 20 s) ; `ok: false` sans erreur quand la session manque, appartient à un autre launcher, ou que l'onglet est en lecture seule ou en erreur — `contracts/web-server.md` « Appel reattach » (commit 3c0b868)
- **[CHANGED]** arrêt du launcher et ouverture d'un projet (aucun changement d'API, commit cb07c97) : bootstrap du conteneur détaché du launcher (`setsid nohup`, `/var/lib/cl/boot.{log,pid,rc}`, un seul exec, jamais relancé s'il tourne), arrêt qui attend les ouvertures en cours 20 s au plus, conteneur à moitié installé réparé au démarrage (sans session) ou à l'ouverture — `contracts/web-server.md` « Arrêt du launcher et ouverture d'un projet » ; écart signalé : `terminationGracePeriodSeconds` du schéma sans minimum face à l'attente fixe de 20 s

## [20261117] — v6.4.0 (plan, rev2 GATE 2) : fichiers et version du serveur, backends distants, coffre, NetworkPolicy, version du template

- **Retiré (rev2)** : toute authentification de v6.4.0 (#172 déplacée en v7.0.0) : oauth2-proxy, `forwardAuth`, `CL_WEB_AUTH_*`, identité transmise, adresses publiques multiples. `contracts/web-auth.md` renommé `contracts/web-server.md` ; la piste v7.0.0 y est notée.
- **[NEW]** route `/files/<chemin>`, `CL_BROWSER_PATH_MAP` des sessions vers l'adresse de l'ingress interne, appariement par `/files/__claude-launcher__/pair` (#174) — `contracts/web-server.md`
- **[NEW]** `server = {version, boot}` dans `init` et l'instantané ; bandeaux « Serveur mis à jour » / reconnexion (#198)
- **[NEW]** chart : `networkPolicy` **activée par défaut**, coffre `secrets.persistence`, `rbac.extraNamespaces`, `backends.{docker,kubeconfigs}` ; `externalIngress.*` **préparé mais refusé au rendu** — `contracts/helm-values.md` « v6.4.0 »
- **[NEW]** coffre des secrets `CL_SECRETS_DIR` ; clés ssh et `known_hosts` par backend saisies dans l'interface ; appels `backendSecretGet/Set/Delete`, `backendHostScan`, `backendKnownHostsSet` ; **règle d'écriture provisoire** (ingress interne et fenêtre native) jusqu'à v7.0.0 — `contracts/backends-remote.md` (#194)
- **[NEW]** `kubeconfig` d'un backend k8s : coffre (`kubeconfig: true`) ou chemin ; priorité sur le ServiceAccount (#193). **[CHANGED]** par rapport au texte de #193 : le champ est modifiable depuis l'interface (règle provisoire), hors de l'empreinte d'exposition en v6.4.0
- **[CHANGED]** image : clients ssh, docker (binaire statique), podman (`podman-remote`) (#194)
- **[CHANGED]** version du template : comparaisons ordonnées, titre de la carte TEAM = version du projet, `teamHead.tplNewer`, vérification GitHub authentifiée, conditionnelle, partagée par poste, quota visible — `contracts/template-version.md` (#199)
- **Aucun BREAKING.**

## [20261117] — v6.3.0, LOT-5 : commutateur du masquage des teammates inactifs (#188)

- **[NEW]** clé `TEAM_HIDE_IDLE_ENABLED` de `config.conf` (`1` par défaut) : désactive le masquage sans perdre le délai `TEAM_HIDE_IDLE` — `contracts/desktop-dispatch.md`
- **[NEW]** `cards.teamHead.hideIdle` (`enabled`, `delay`) : état du masquage sur la carte TEAM
- **[CHANGED]** règle de visibilité quand le masquage est désactivé : aucun teammate caché, entrées `h` de `@cl_panevis` retirées, panes de la réserve ramenés — `contracts/tmux-options.md` « Masquage désactivé »
- **[CHANGED]** `teamToggle` : la bascule qui cacherait un teammate est refusée quand le masquage est désactivé ; `cards.team[].hidden` faux et `idleLeft` à 0 dans ce cas
- Aucun `BREAKING` : sans la nouvelle clé, comportement de la v6.2.0 (`TEAM_HIDE_IDLE=0` compris).

## [20261117] — v6.3.0, LOT-4.4 : interface web accessible sans jeton (décision utilisateur)

- **[NEW]** `desktop_web.access` (`token` | `none`, défaut `token`), variable `CL_WEB_ACCESS`, champ `webUI.access`, clé `desktop_web.access` de `configModel` / `configApply` — `contracts/desktop-web.md` « Mode d'accès »
- **[NEW]** mode `none` : ni jeton, ni cookie, ni session ; `Host`, origine, méthode et corps JSON toujours contrôlés ; `GET /?t=…` redirigé vers `/` ; accepté seulement en boucle locale ou avec une adresse publique en `https`
- **[NEW]** chart : valeur `web.access`, variable `CL_WEB_ACCESS` du pod — `contracts/helm-values.md`
- **[CHANGED]** chart : accès **sans jeton par défaut** (`web.access: none`) ; `NOTES.txt` sans lien d'amorçage ; `none` sans Ingress refusé au rendu
- **[CHANGED]** `/api/call`, `/api/events` : `Sec-Fetch-Site: cross-site` refusé (`403`, `origin`), dans les deux modes
- **[CHANGED]** `webUIRegen` et `webUIOpen` en mode `none` : refus de la régénération, ouverture sans `?t=`
- Aucun `BREAKING` pour le hub Linux et l'exe Windows : sans réglage, le mode reste `token`. Le serveur de maquettes n'est pas concerné.
- Risques acceptés jusqu'à v6.4.0 (#172) : tout poste de la liste d'adresses de l'Ingress pilote les projets ; tout pod du cluster atteint le Service sans passer par l'Ingress (pas de NetworkPolicy).

## [20261116] — v6.3.0 : launcher dans Kubernetes (#165 à #171, #173, #175 à #177), réglages des serveurs web (#183)

- **[NEW]** `CL_ROOT_BASE` et `--root-base` : racine de base ; ordre `CL_CONFIG_DIR` > racine de base > dossier du binaire > dossier personnel (#165) — `contracts/launcher-env.md`
- **[NEW]** `CL_HUB_NAME`, `CL_CACHE_DIR` : nom et dossier d'état du launcher (#169)
- **[NEW]** `CL_IN_CLUSTER`, `CL_ALLOW_HOST_BACKEND`, `CL_K8S_VOLUMES` : launcher dans un cluster — backend k8s par le ServiceAccount du pod, namespace du pod, backend k8s implicite, backend `host` refusé (#168, #169)
- **[CHANGED]** diagnostic et « Tester » d'un backend k8s **en cluster** : contrôle de chaque droit, contrôle du montage du PVC, plus d'étape `get namespace` (#168) — hors cluster, inchangé
- **[NEW]** `CL_UPDATE_MODE` (`auto` | `notify`) : annonce de version sans installation (#167)
- **[CHANGED]** `desktop --web` **en cluster** : jeton conservé dans le cache au lieu d'un jeton neuf à chaque démarrage (#167) — hors cluster, inchangé
- **[CHANGED]** `desktop --web` : arrêt propre sur `SIGTERM` / `SIGINT` (#167)
- **[NEW]** image `ghcr.io/ccoupel/claude-launcher` (tags `vX.Y.Z`, `vX.Y.Z.a`) (#167)
- **[NEW]** `desktop_web.enabled` (commutateur) et `desktop_web.url` (adresse publique) ; option `--url`, variable `CL_WEB_URL` (#183, #171) — `contracts/desktop-web.md` ; sans `enabled`, comportement de la v6.2.0
- **[NEW]** routes `GET /healthz` et `GET /readyz`, sans session ni contrôle de `Host`, sans information (#167)
- **[CHANGED]** `GET /api/events` : en-têtes `X-Accel-Buffering: no` et `no-transform` ; reconnexion de l'interface à délai croissant ; session perdue signalée (#171)
- **[CHANGED]** cookie de session : attribut `Secure` quand l'adresse publique est en `https` (#171) — sans adresse publique, inchangé
- **[NEW]** instantané `webUI` : champs `enabled`, `url`, `server` ; `configModel` / `configApply` `kind: "hub"` : clés `desktop_web.enabled`, `desktop_web.url` (#183, #171)
- **[NEW]** chart Helm `deploy/helm/claude-launcher` et ses valeurs (#170, #173) — `contracts/helm-values.md`
- **[NEW]** chart : `volume.create` — le chart crée le PVC de la racine (et, en option, le PV statique : droits cluster), conservés à la désinstallation (`helm.sh/resource-policy: keep`) ; défaut inchangé (PVC existant) (#170)
- **[CHANGED]** chart (revue et audit LOT-4.2) : Role réduit aux appels réels (`pods/exec` create seul, `statefulsets` sans update, `events` list, PVC get) ; liste d'adresses validée entrée par entrée (/8 IPv4 et /16 IPv6 au minimum) ; `rootBase` normalisé ; `extraEnv` et `ingress.annotations` ne peuvent pas remplacer les variables et annotations posées par le chart ; `clusterIssuer: a-renseigner` refusé ; racine du conteneur en lecture seule ; QUALIF dans le namespace `claude-qualif` sur la racine dédiée `/share/MFS/ROOT/CLAUDE-QUALIF` (#170, #173)
- Aucun `BREAKING` : sans les nouvelles variables ni les nouveaux réglages, hub Linux et exe Windows se comportent comme en v6.2.0.

## [20261115] — v6.2.0 : tailles par zone (#150), carte TEAM (#151), en-têtes (#152), rafraîchir (#158), interface web (#163)

- **[NEW]** préférences par poste `treeFontSize`, `cardsFontSize` (#150) — rendues aussi par `configModel` `kind: "hub"`
- **[NEW]** `cards.teamHead` (`tpl`, `claude`, `odd`) et `cards.team[].odd` (#151) — champs existants conservés
- **[NEW]** `layout.panes[].state` (`lead` / `active` / `idle` / vide) (#152) — `role` et `active` inchangés
- **[NEW]** options tmux de pane `@cl_state`, `@cl_name` et options de fenêtre `pane-border-*` posées par le launcher qui a la main (#152) — `contracts/tmux-options.md` ; un launcher antérieur les ignore
- **[NEW]** `refreshProject` + évènement `refresh`, `forceClose`, `forceCloseConfirm` ; lignes de l'instantané : `refreshing`, `canForce` (#158)
- **[NEW]** réglage `desktop_web` de `backends.yaml` (`defaults` / `hubs.<machine>[.<système>]`), champ `webUI` de l'instantané, méthode `webUIOpen` (#163)
- **[CHANGED]** `clipboardRead` / `clipboardWrite` : `unavailable` décidé **par appel** (client web) et non plus par processus ; `quit`, `setTheme` refusés à un client web (#163) — rétrocompatible pour `desktop --web` seul
- **[CHANGED]** `desktop --web` sans `--addr` : utilise `desktop_web.listen` s'il est fixé (sinon port libre, inchangé) (#163)
- **[NEW]** clés `LAYOUT_MODE` (`top` | `left`, défaut `top`), `LAYOUT_LEFT_LEADER_PCT`, `PROJECT_LAYOUTS` de `config.conf` (#153) — sans réglage, la mise en page est inchangée
- **[NEW]** `projectSettings` : `layout`, `layoutAuto`, `layouts` ; `projectApply` : argument `layout` (#153)
- **[CHANGED]** `projectApply` : `colorSaved` vrai dès que `config.conf` est écrite, pour la couleur ou pour l'organisation (#153) — rétrocompatible
- Coque Linux (#152) : forme arrêtée — ligne de titre par pane (`pane-border-status top`), maquette A retenue
- Aucun `BREAKING`.

## [20261113] — v6.1.0 : panes cachés (#136) et configuration structurée (#118)

- **[CHANGED]** `cards.team` (interne à l'exe : frontend embarqué, aucun consommateur externe) — une ligne par teammate, champ `count` retiré, champs `pane`, `started`, `active`, `hidden`, `pinned`, `idleLeft` ajoutés (`contracts/desktop-dispatch.md`)
- **[NEW]** `clipboardWrite`, `clipboardRead` (LOT-4, #155) — presse-papiers du poste par le runtime Wails, « indisponible » en `--web` ; **[CHANGED]** `termInput` rend un refus (`readonly`, `toobig`) au lieu d'ignorer la saisie
- **[NEW]** `projectSettings`, `projectApply` (LOT-3.2, #148) — réglages d'un projet : backend, image, couleur, `env.yaml` structuré, aperçu fusionné avec provenance (`contracts/desktop-dispatch.md`)
- **[ADDED]** `cards.team[]` (cycle 4) — `tplState` (`ok`/`late`/vide), `tplTarget`, `companionFile` : état du template par teammate (`contracts/desktop-dispatch.md`)
- **[NEW]** `teamToggle` — bascule caché / visible d'un teammate inactif
- **[NEW]** options tmux `@cl_panevis` (fenêtre), `@cl_idle_at`, `@cl_home` (pane) et session de réserve `cl-hidden` — `contracts/tmux-options.md` ; un launcher antérieur les ignore (panes cachés laissés dans la réserve)
- **[NEW]** `configModel`, `configApply`, `backendTest`, `backendStates` — configuration structurée
- **[NEW]** `projectSettings`, `projectApply` — réglages d'un projet
- **[NEW]** clé `TEAM_HIDE_IDLE` de `config.conf` (défaut 30, 0 = jamais) ; préférence par poste `termFontSize`
