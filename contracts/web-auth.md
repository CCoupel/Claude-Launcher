# Contrat — interface web en mode serveur : authentification GitHub, adresses publiques, fichiers, version (v6.4.0)

> Complète `contracts/desktop-web.md` (v6.3.0), qui reste valable pour les modes `token` et `none`. Ce fichier
> décrit le mode `github` (#172), les adresses publiques multiples (ingress interne + externe), la route des
> fichiers (#174) et l'annonce de version du serveur (#198). Le backend peut ajuster un détail pour une contrainte
> technique, en le notant dans `contracts/CHANGELOG.md`.
> **Hub Linux et exe Windows sans réglage : comportement de la v6.3.0** (mode `token`, une seule adresse).

## Mode d'accès `github` **[NEW]** (#172)

`desktop_web.access` / `CL_WEB_ACCESS` gagne la valeur `github`. Valeurs : `token` (défaut du launcher), `none`,
`github`.

| Réglage | Variable | Obligatoire en `github` | Rôle |
|---------|----------|-------------------------|------|
| identifiant client | `CL_WEB_GITHUB_CLIENT_ID` | oui | client OAuth (GitHub App recommandée, ou OAuth App) |
| secret client | `CL_WEB_GITHUB_CLIENT_SECRET_FILE` | oui | **chemin** d'un fichier (Secret monté) ; jamais la valeur dans une variable, un argument ou `backends.yaml` |
| compte autorisé | `CL_WEB_GITHUB_ALLOWED_USER` | oui | **un seul** login GitHub, comparé sans casse ; l'identifiant numérique du compte est relevé à la 1re connexion et doit ensuite rester le même (un login renommé puis repris par un autre compte est refusé) |
| clé de session | `CL_WEB_SESSION_KEY_FILE` | oui | chemin d'un fichier de 32 octets au moins (Secret monté) : signe les cookies de session |
| durée de session | `CL_WEB_SESSION_TTL` | non (défaut `8h`) | durée Go (`30m` … `720h`) ; hors bornes : erreur de réglage |

- Garde (comme `none`) : `github` exige au moins une adresse publique en `https` ; sinon erreur de réglage,
  `desktop --web` s'arrête avec un code non nul (« accès GitHub refusé : adresse publique en https exigée »).
  Réglage incomplet (client, secret, compte, clé) : même arrêt, message qui nomme la valeur manquante (jamais sa
  valeur).
- Fichiers de secret : lus au démarrage ; refusés s'ils sont sous `CL_ROOT_BASE`, sous un volume partagé déclaré
  (`CL_K8S_VOLUMES`) ou lisibles par tous (mode `o+r`, hors montage de Secret en lecture seule `0440`/`0400`).
- Le jeton d'amorçage (`/?t=`) n'existe pas en `github` : `<cache>/desktop-web-token` est supprimé au démarrage,
  `CL_DESKTOP_TOKEN` ignoré, `webUIRegen` refusé (« accès GitHub : aucun jeton à régénérer »).
- Le jeton d'accès GitHub obtenu à la connexion sert **seulement** à lire `GET /user`, puis il est oublié : il n'est
  ni conservé, ni journalisé, ni utilisé pour autre chose. Portée demandée : aucune (`scope` vide).

### Routes du mode `github`

| Route | Session | Réponse |
|-------|---------|---------|
| `GET /healthz`, `GET /readyz` | aucune | inchangé |
| `GET /auth/login` | aucune | pose un cookie `cl_oauth_state` (aléatoire, `HttpOnly`, `Secure`, `SameSite=Lax`, 10 min) puis `302` vers `https://github.com/login/oauth/authorize?client_id=…&redirect_uri=<origine de la requête>/auth/callback&state=…&allow_signup=false` |
| `GET /auth/callback?code&state` | aucune | `state` ≠ cookie `cl_oauth_state` → `400`, **sans** compter d'échec. Sinon échange du `code` (serveur → `github.com`), lecture de `/user` : compte autorisé → cookie de session, `303` vers `/` ; autre compte → page de refus (`403`), échec compté |
| `POST /auth/logout` | cookie, origine contrôlée | révoque la session, efface le cookie, `303` vers `/auth/login` |
| `GET /` et toute page | cookie | sans session : la **page de connexion** (`200`, aucun nom de hub, de projet, de version) |
| `POST /api/call`, `GET /api/events` | cookie | sans session : `401`, `X-Launcher-Refusal: session` |
| `GET /files/…` | cookie | voir « Fichiers » ; sans session : `401` |

- `redirect_uri` est construit **seulement** à partir de l'adresse publique admise qui correspond à `Host` (jamais
  d'en-tête `X-Forwarded-*`). Chaque adresse publique doit être déclarée comme URL de rappel côté GitHub
  (`<url>/auth/callback`).
- Contrôles de v6.3.0 conservés : `Host`, origine, `Sec-Fetch-Site` (`cross-site` refusé ; `same-site` refusé
  comme en `none`), `POST` + JSON sur `/api/call`, en-têtes de sécurité, 32 flux au plus.

### Session

- Cookie `__Host-cl_session` (`Secure`, `HttpOnly`, `SameSite=Strict`, `Path=/`) : identifiant de session aléatoire
  + identifiant GitHub + échéance, **signé** (HMAC-SHA256, clé `CL_WEB_SESSION_KEY_FILE`). Il survit à un redémarrage
  du serveur (#198) tant que la clé ne change pas ; changer la clé révoque toutes les sessions.
- Révocation (déconnexion) : identifiant de session ajouté à une liste de révocation, conservée dans
  `<cache>/web-revoked.json` jusqu'à l'échéance de la session (identifiants seuls, aucun secret).
- Chaque requête revérifie : signature, échéance, non révoquée, identifiant GitHub = compte autorisé.
- Un cookie de session est propre à un nom d'hôte : se connecter par l'ingress interne ne connecte pas l'externe.

### Limitation des essais

- Sont comptés : les rappels avec un `state` valide qui finissent en refus (autre compte, échange de `code` en échec).
  Un rappel sans `state` valide est rejeté **sans** compter (un tiers ne peut pas bloquer l'utilisateur à bas coût).
- 5 échecs comptés en 10 min → connexions suspendues 5 min (toutes origines : aucune adresse client n'est lue), page
  de refus avec la durée restante. Chaque échec et chaque suspension sont journalisés (login refusé, jamais de
  `code`, `state`, jeton ni cookie).

### Journaux

Jamais dans un journal ni dans la sortie : secret client, `code`, `state`, jeton GitHub, cookie, clé de session,
query string de `/auth/callback`. Journalisés : connexion réussie (login), refus (login), suspension, déconnexion.

## Adresses publiques multiples **[CHANGED]** (#172 — ingress interne + externe)

- `CL_WEB_URL` (et `desktop_web.url`, `--url`) accepte **une liste** d'adresses séparées par des virgules
  (`https://launcher.local,https://launcher.exemple.fr`), deux au plus. Chaque adresse suit les règles de
  `ParsePublicURL` (v6.3.0). Une seule adresse : comportement de la v6.3.0.
- Chaque `Host` admis est associé à **son** adresse : origine admise, cookie `Secure`, `redirect_uri` de la même
  adresse. Une origine d'une adresse n'est pas admise sur l'autre hôte.
- `webUI.url` (instantané) : la première adresse ; nouveau champ `webUI.urls` (liste complète).
- Mode `none` avec plusieurs adresses : chacune doit être en `https`.

## Fichiers des projets par l'interface **[NEW]** (#174)

- Route `GET /files/<chemin interne>` (segments encodés) du serveur web : même règle que le serveur de maquettes
  (`browser_server`) — lecture seule, racines servies, liste de dossier, aucun fichier hors racine, aucun lien
  symbolique qui en sort. Session exigée en `token` et `github` ; en `none`, mêmes contrôles de `Host` et
  `Sec-Fetch-Site` qu'une page.
- En-têtes : `Content-Security-Policy: sandbox` (pas de script de même origine que l'interface),
  `X-Content-Type-Options: nosniff`, `Cache-Control: no-store`.
- En mode serveur (`desktop --web` en cluster), les sessions claude reçoivent
  `CL_BROWSER_PATH_MAP=[{"container":"/","host":"<adresse navigateur>/files","os":"url"}]` et la consigne
  `CL_BROWSER_HINT` correspondante. `<adresse navigateur>` = `CL_WEB_BROWSER_URL` si posé (doit être l'une des
  adresses publiques), sinon la dernière adresse de `CL_WEB_URL` (l'externe quand il y en a deux).
- Appariement : `GET /files/__claude-launcher__/pair?device=<id>&token=<jeton d'appariement>` sous session. Seul un
  navigateur qui a une session ouverte peut être apparié ; jeton d'appariement inchangé (v5.10.0).
- Le serveur de maquettes en boucle locale (`127.0.0.1:8765`, `ui/desktop/app.go:235`) reste celui du hub et de
  l'exe. En mode serveur dans un cluster, il n'est plus annoncé aux sessions (la correspondance ci-dessus le remplace).
  Son démarrage dans le pod est conservé ou retiré au choix du backend, à noter dans le CHANGELOG.

## Version du serveur **[NEW]** (#198)

- Résultat de `init` et de l'instantané : champ `server = { "version": "v6.4.0", "boot": "<aléatoire par démarrage>" }`.
- Client web : il garde le `server` du premier `init` de la page. Après une reconnexion du flux :
  - `version` différente → bandeau « Serveur mis à jour (vX) — Recharger » (bouton = rechargement de la page) ;
  - même `version`, `boot` différent → bandeau « Serveur redémarré — reconnexion des terminaux… », terminaux
    rattachés automatiquement à leurs sessions tmux, bandeau retiré à la fin ; aucune action de l'utilisateur.
- En cluster (`CL_UPDATE_MODE=notify`), l'annonce d'une nouvelle release du launcher reste non cliquable, texte
  « mise à jour par helm upgrade ».
- Fenêtre native : non concernée (le serveur est le processus lui-même).
