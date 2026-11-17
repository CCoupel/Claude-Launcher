# Contrat — interface web en mode serveur : ingress interne libre, ingress externe authentifié, fichiers, version (v6.4.0)

> Révision GATE 2 du 2026-11-17 (rev1) : l'authentification de l'ingress externe est faite **devant** le launcher,
> par Traefik (`forwardAuth`) et oauth2-proxy avec GitHub comme fournisseur. Le launcher ne code aucun OAuth.
> L'ingress interne reste **sans authentification** (mode `none`, liste d'adresses du LAN).
> Complète `contracts/desktop-web.md` (v6.3.0), qui reste valable. Hub Linux et exe Windows sans réglage :
> comportement de la v6.3.0.

## Architecture (#172)

```
LAN (192.168.1.0/24) ──https://launcher.local──▶ Traefik ─[ipAllowList]──────────────────────────────┐
Internet ──https://launcher.coupel.net──▶ Traefik ─[forwardAuth → oauth2-proxy (GitHub, 1 compte)]──┤
                                                    └─ /oauth2/* ──▶ oauth2-proxy                     ▼
                                                                                Service launcher :8080 (pod)
                                         NetworkPolicy : entrée du pod seulement depuis le namespace traefik
```

- **Interne** : inchangé depuis v6.3.0 (`web.access: none`, `ipAllowList`, CA interne). Le Service Traefik est en
  `externalTrafficPolicy: Local`, donc l'adresse source d'un client Internet est conservée et refusée par la liste.
  Test critique en QUALIF : depuis Internet, `Host: launcher.local` vers l'IP publique → `403`.
- **Externe** : Ingress public (certificat `letsencrypt-prod`), middlewares Traefik **intégrés** (aucun plugin) :
  `forwardAuth` (adresse `http://<release>-oauth2-proxy.<ns>.svc/oauth2/auth`, `trustForwardHeader: false`,
  `authResponseHeaders: [X-Auth-Request-User, X-Auth-Request-Email]`), `errors` (401 → `/oauth2/sign_in?rd=…`),
  `rateLimit` (moyenne 20 req/s, rafale 50), redirection HTTPS. Le chemin `/oauth2/` du même hôte est routé vers
  oauth2-proxy.
- **oauth2-proxy** (image `quay.io/oauth2-proxy/oauth2-proxy`, version et empreinte figées dans le chart) :
  `--provider=github`, `--github-user=<compte autorisé>` (un seul), `--email-domain=*`, `--scope=read:user`,
  `--cookie-secure=true`, `--cookie-samesite=lax`, `--cookie-expire=<web.sessionTTL, défaut 8h>`,
  `--cookie-refresh=1h`, `--skip-provider-button=true` (direct vers GitHub), `--reverse-proxy=true`,
  `--set-xauthrequest=true`, `--redirect-url=https://<hôte externe>/oauth2/callback`, `--whitelist-domain=<hôte externe>` (pas de redirection `rd=` hors de cet hôte). Secrets (identifiant et secret
  client, secret de cookie de 32 octets) par Secret existant, jamais dans les valeurs.
- **Le launcher reste en mode `none` sur les deux hôtes** (aucun jeton, aucun cookie du launcher) : la session est
  celle d'oauth2-proxy. Le mode d'accès par hôte n'est donc pas nécessaire.

## Adresses publiques multiples **[CHANGED]**

- `CL_WEB_URL` (et `desktop_web.url`, `--url`) accepte une **liste** séparée par des virgules, deux adresses au plus.
  Chaque adresse suit `ParsePublicURL` (v6.3.0) et doit être en `https` en mode `none`. Avec une seule adresse,
  comportement de v6.3.0.
- Chaque `Host` admis est associé à **son** adresse (origine admise, `Sec-Fetch-Site`) ; une origine d'un hôte n'est
  pas admise sur l'autre.
- `CL_WEB_AUTH_URL` **[NEW]** = l'adresse (parmi `CL_WEB_URL`) protégée par le proxy d'authentification (l'externe).
- Instantané : `webUI.url` = première adresse ; `webUI.urls` **[NEW]** = liste.

## Identité transmise par le proxy **[NEW]**

- Sur l'hôte de `CL_WEB_AUTH_URL` **seulement**, le launcher lit `X-Auth-Request-User` (posé par Traefik depuis la
  réponse d'oauth2-proxy : la valeur d'un client est remplacée). Sur tout autre hôte, ces en-têtes sont ignorés.
- Rôle : **affichage** (compte dans l'en-tête) et **requête de confiance** pour les réglages sensibles
  (voir `contracts/backends-remote.md`, « Règle des secrets »). Une requête de confiance exige les trois :
  hôte = `CL_WEB_AUTH_URL`, `X-Auth-Request-User` = `CL_WEB_AUTH_USER` (le compte autorisé, sans casse),
  `CL_WEB_AUTH_PROXY=1` (posé par le chart quand oauth2-proxy est déployé). Ce n'est jamais une autorisation
  d'accès : l'accès est décidé par le proxy.
- Confiance justifiée par la NetworkPolicy (le pod n'est joignable que par Traefik) ; sans NetworkPolicy
  (`networkPolicy.enabled: false`), `CL_WEB_AUTH_PROXY` n'est pas posé (aucune requête de confiance).
- Instantané : `webUI.user` **[NEW]** = compte lu (vide sinon), `webUI.logout` **[NEW]** =
  `/oauth2/sign_out?rd=/` sur l'hôte authentifié, vide sinon.

## Session, déconnexion, limitation des essais

| Besoin (#172) | Réponse |
|---------------|---------|
| identité vérifiée, un seul compte | oauth2-proxy + GitHub, `--github-user` |
| durée de session | `--cookie-expire` (`web.sessionTTL`, défaut 8 h) ; survit aux redémarrages du launcher et d'oauth2-proxy (secret de cookie stable) |
| déconnexion | « Se déconnecter » dans l'en-tête → `webUI.logout` (efface le cookie du proxy). La session GitHub du navigateur n'est pas fermée : c'est voulu, et c'est dit dans l'infobulle |
| limitation des essais | l'identification a lieu chez GitHub (mot de passe, 2FA, protections GitHub) ; un autre compte reçoit le `403` d'oauth2-proxy ; `rateLimit` Traefik sur tout l'hôte externe |
| aucun jeton dans l'URL ni les journaux | pas de jeton du launcher ; oauth2-proxy ne journalise ni cookie ni code (`--auth-logging` sans secrets, `--request-logging=false`) ; journaux d'accès Traefik : la query de `/oauth2/callback` (code à usage unique, durée de vie de 10 min) peut y figurer → journaux d'accès du Traefik partagé à vérifier par infra |
| 401 / session expirée dans l'interface | `/api/*` reçoit `401` du proxy : la page se recharge (`location.reload()`), ce qui repasse par la connexion GitHub ; plus de « session expirée — rouvrir le lien » sur l'hôte authentifié |

## Fichiers des projets par l'interface **[NEW]** (#174)

- Route `GET /files/<chemin interne>` (segments encodés) du serveur web : mêmes règles que le serveur de maquettes
  (lecture seule, racines servies, liste de dossier, rien hors racine, pas de lien symbolique qui en sort).
  En-têtes `Content-Security-Policy: sandbox`, `X-Content-Type-Options: nosniff`, `Cache-Control: no-store`.
- Contrôles : `Host` et `Sec-Fetch-Site` comme une page ; en mode `token` (hub, exe), session exigée ; en `none`,
  pas de session (la protection est l'ingress : allowlist ou proxy).
- En mode serveur dans un cluster, les sessions claude reçoivent
  `CL_BROWSER_PATH_MAP=[{"container":"/","host":"<adresse navigateur>/files","os":"url"}]` et la consigne
  `CL_BROWSER_HINT` correspondante. `<adresse navigateur>` = `CL_WEB_BROWSER_URL` si posé (l'une des adresses
  publiques), sinon `CL_WEB_AUTH_URL`, sinon la première adresse.
- Sur l'hôte externe, Chrome envoie le cookie d'oauth2-proxy : la maquette s'ouvre sans nouvelle connexion ; sans
  session, le proxy redirige vers GitHub, et le fichier n'est pas servi.
- Appariement : `GET /files/__claude-launcher__/pair?device=<id>&token=<jeton d'appariement>` (jeton d'appariement
  inchangé, v5.10.0).
- Le serveur de maquettes en boucle locale (`127.0.0.1:8765`) reste celui du hub et de l'exe ; en cluster, il n'est plus
  annoncé aux sessions (démarrage conservé ou retiré au choix du backend, à noter dans le CHANGELOG).

## Version du serveur **[NEW]** (#198)

- `init` et l'instantané portent `server = { "version": "v6.4.0", "boot": "<aléatoire par démarrage>" }`.
- Client web : il garde le `server` du premier `init` de la page. Après une reconnexion du flux :
  - `version` différente → bandeau « Serveur mis à jour (vX) — Recharger » ;
  - même `version`, `boot` différent → bandeau « Serveur redémarré — reconnexion des terminaux… », terminaux
    rattachés automatiquement à leurs sessions tmux, bandeau retiré à la fin.
- Session : en `none` il n'y a pas de session du launcher, et la session du proxy n'est pas touchée par un
  redémarrage du pod : rien à refaire.
- En cluster (`CL_UPDATE_MODE=notify`), l'annonce d'une nouvelle release du launcher reste non cliquable
  (« mise à jour par helm upgrade »).
