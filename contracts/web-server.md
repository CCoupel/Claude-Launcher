# Contrat — interface web en mode serveur : fichiers, version du serveur ; ingress externe préparé (v6.4.0)

> Révision GATE 2 n°2 (rev2, 2026-11-17) : **aucune authentification en v6.4.0**. L'authentification (#172) est
> déplacée en v7.0.0 (voir « Piste v7.0.0 » en fin de fichier). Le seul ingress actif est l'ingress interne (LAN,
> `web.access: none`, liste d'adresses, CA interne), comme en v6.3.0. Complète `contracts/desktop-web.md`, qui reste
> valable. Hub Linux et exe Windows sans réglage : comportement de la v6.3.0.

## Exposition en v6.4.0

- **Ingress interne** : inchangé depuis v6.3.0 (`ipAllowList` 192.168.1.0/24, `claude-launcher-ca`). Le Service
  Traefik est en `externalTrafficPolicy: Local` (adresse source conservée). Test critique en QUALIF : depuis Internet,
  `Host: launcher-qualif.local` vers l'IP publique → `403`.
- **NetworkPolicy** activée par défaut (`contracts/helm-values.md`) : le pod n'est joignable que depuis le namespace
  `traefik`. Plus aucun pod du cluster ne contourne l'Ingress.
- **Ingress externe : préparé, désactivé.** Le chart déclare `externalIngress.*`, mais `externalIngress.enabled: true`
  fait **échouer le rendu** (`fail` Helm) avec le message « ingress externe refusé : aucune authentification avant
  v7.0.0 (#172) ». Le launcher n'a qu'une adresse publique (`CL_WEB_URL`, v6.3.0).

## Fichiers des projets par l'interface **[NEW]** (#174)

- Route `GET /files/<chemin interne>` (segments encodés) du serveur web : mêmes règles que le serveur de maquettes
  (`browser_server`) — lecture seule, racines servies, liste de dossier, rien hors racine, pas de lien symbolique qui
  en sort. En-têtes `Content-Security-Policy: sandbox`, `X-Content-Type-Options: nosniff`, `Cache-Control: no-store`.
- Contrôles : `Host` et `Sec-Fetch-Site` comme une page. En mode `token` (hub, exe), session exigée ; en `none`, pas
  de session (la protection est l'ingress interne : liste d'adresses).
- En mode serveur dans un cluster, les sessions claude reçoivent
  `CL_BROWSER_PATH_MAP=[{"container":"/","host":"<CL_WEB_URL>/files","os":"url"}]` et la consigne `CL_BROWSER_HINT`
  correspondante.
- Appariement : `GET /files/__claude-launcher__/pair?device=<id>&token=<jeton d'appariement>` (jeton d'appariement
  inchangé, v5.10.0).
- Le serveur de maquettes en boucle locale (`127.0.0.1:8765`) reste celui du hub et de l'exe ; en cluster, il n'est plus
  annoncé aux sessions (démarrage conservé ou retiré au choix du backend, à noter dans le CHANGELOG).

## Version du serveur **[NEW]** (#198)

- `init` et l'instantané portent `server = { "version": "v6.4.0", "boot": "<aléatoire par démarrage>" }`.
- **Évènement d'accueil du flux SSE** : dès l'ouverture de `/api/events` (après `: ok`), avant tout autre évènement, le
  serveur envoie `data: {"name":"hello","data":{"server":{"version":"…","boot":"…"}}}`. Le client le traite comme un
  `server` reçu : si son `init` a échoué pendant le redémarrage (502/503 de l'ingress), il compare quand même version et
  boot au premier `init` de la page (bandeau « Serveur mis à jour » ou « Serveur redémarré ») et rejoue `init`. Un
  client qui ne connaît pas `hello` l'ignore.
- Client web : il garde le `server` du premier `init` de la page. Après une reconnexion du flux :
  - `version` différente → bandeau « Serveur mis à jour (vX) — Recharger » (rechargement de la page) ;
  - même `version`, `boot` différent → bandeau « Serveur redémarré — reconnexion des terminaux… », terminaux
    rattachés automatiquement à leurs sessions tmux, bandeau retiré à la fin.
- En `none`, il n'y a pas de session à conserver ; en `token` (hub), comportement de v6.3.0 (session en mémoire).
- En cluster (`CL_UPDATE_MODE=notify`), l'annonce d'une nouvelle release du launcher reste non cliquable
  (« mise à jour par helm upgrade »).

### Appel `reattach` **[NEW]** (#198, `ui/desktop/actions.go` `reattachPath`, commit 3c0b868)

`reattach {path}` → `{ "ok": true, "tab": "<path>" }` ou `{ "ok": false }`. Appel de `/api/call` et de la liaison
Wails ; il n'y a jamais d'erreur (`error` absent) : tout échec est `ok: false`, sans message.

- **Rôle** : après « Serveur redémarré » (même `version`, autre `boot`), le client web l'appelle pour chaque onglet
  qu'il avait ouvert, afin de rattacher l'onglet à la session tmux **qui a survécu** au redémarrage.
- **Ne crée jamais rien** : ni session tmux, ni conteneur, ni pod ; la commande du projet (claude) n'est pas relancée
  (aucun `send-keys`). Si la session existe, l'onglet est ouvert par le chemin normal (`open`, `LaunchNormal`) sur
  cette session.
- **Session vivante** :
  - backend `host` : `tmux -L <socket du launcher> has-session -t =<session du projet>` réussit (refus si le backend
    `host` est interdit, par exemple en cluster sans `CL_ALLOW_HOST_BACKEND`) ;
  - autre backend : le backend est trouvé et son diagnostic en cache est vide, un conteneur ou pod du projet tourne,
    sa session existe, et son propriétaire est **ce** launcher (ou n'est pas marqué).

| Situation | Résultat |
|-----------|----------|
| chemin inconnu ou pas un projet | `ok: false` |
| onglet déjà ouvert, en écriture, pas en erreur | `ok: true` (rien n'est refait) |
| onglet ouvert en lecture seule ou en erreur | `ok: false` (ni détaché ni rouvert : le client passe par `open`, qui pose la question « rejoindre ») |
| aucune session vivante (session tmux absente, conteneur arrêté, backend injoignable) | `ok: false`, rien n'est créé |
| session d'un autre launcher | `ok: false` |
| session vivante, ouverture réussie | `ok: true`, `tab` = chemin |
| session vivante, mais `open` échoue ou ne rend pas d'onglet | `ok: false` |

- **Côté client** (`app.js`, sur « restart ») :
  - les vues ouvertes sont mises en attente ; une vue dont l'onglet revient de lui-même (le serveur se rattache seul
    au démarrage, `App.reattach`) est resynchronisée par `termSync` ;
  - **repli au bout d'environ 5 s** : pour chaque vue encore en attente, le client appelle `reattach {path}` ; si la
    réponse est `ok: true`, il appelle `termSync` ;
  - **abandon à 20 s** (`REATTACH_MS`) : une vue encore en attente (`ok: false`, ou pas de retour) est retirée, et le
    bandeau affiche « N terminaux non rattachés — recharger » ;
  - un seul appel `reattach` par vue et par redémarrage, sans autre tentative ; le projet reste dans l'arbre et se
    rouvre par `open` (qui, lui, crée ce qu'il faut).
- Maquette : `_work/mockup/6.4.0/ui/web_client__maj_serveur.html` (versée dans `docs/mockup/` par le CDP) (bandeaux seuls).

## Piste v7.0.0 — authentification de l'ingress externe (#172, hors v6.4.0)

Étudiée au GATE 2 rev1 (plan `_work/reports/plan-20261117-163459.md`, « Comparaison des options »), **option A
recommandée** : Traefik `forwardAuth` + oauth2-proxy (fournisseur GitHub, `--github-user` = un seul compte, cookie de
session de 8 h, `/oauth2/sign_out`, `--whitelist-domain`), middlewares Traefik intégrés (aucun plugin : Traefik
v3.7.18 du cluster n'en a pas d'activé), une OAuth App GitHub par environnement (URL de retour
`https://<nom public>/oauth2/callback`). Le launcher resterait en `none`, et lirait `X-Auth-Request-User` sur l'hôte
externe seulement (affichage, requêtes de confiance pour les secrets : règle R1, `contracts/backends-remote.md`).
À reprendre au plan de v7.0.0, avec le modèle multi-utilisateur.
