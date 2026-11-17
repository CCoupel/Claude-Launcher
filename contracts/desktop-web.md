# Contrat — interface web : réglage, commutateur, proxy inverse (v6.3.0)

> **v6.4.0** : route `/files`, version du serveur, ingress externe préparé mais refusé (pas d'authentification avant v7.0.0) — voir `contracts/web-server.md`.

> Complète `contracts/desktop-dispatch.md` (« v6.2.0 — LOT-4 : interface web servie par la fenêtre native »). Ce
> fichier décrit ce que #183 et #171 ajoutent. Les protections de #163 restent toutes actives : session en mémoire,
> cookie `HttpOnly` et `SameSite=Strict`, contrôle de `Host`, contrôle d'origine, corps JSON, en-têtes de sécurité.

## Réglage `desktop_web` de `backends.yaml` **[CHANGED]** (#183, #171)

Emplacements inchangés : `defaults.desktop_web`, `hubs.<machine>.desktop_web`, `hubs.<machine>.<système>.desktop_web`.

```yaml
desktop_web:
  enabled: true                  # NEW (#183) — absent : voir la règle ci-dessous
  listen: 127.0.0.1:8787         # inchangé
  allow_lan: false               # inchangé
  url: https://launcher.exemple  # NEW (#171) — adresse publique derrière un proxy inverse
  access: token                  # NEW (LOT-4.4) — token (défaut) | none : voir « Mode d'accès »
```

**`enabled`** (commutateur)

| `enabled` | `listen` | Interface web |
|-----------|----------|---------------|
| absent | vide ou `off` | éteinte (v6.2.0) |
| absent | renseigné | allumée (v6.2.0) |
| `false` | quelconque | **éteinte**, `listen` conservé |
| `true` | renseigné | allumée |
| `true` | vide ou `off` | erreur de réglage : « interface web activée sans adresse d'écoute » |

- L'interface enregistre toujours `enabled` explicitement quand l'utilisateur touche au commutateur.
- Allumer ou éteindre s'applique à l'enregistrement, sans relancer le launcher (`applyWebUI`). Le serveur des
  maquettes (`browser_server`) n'est pas touché, et inversement.
- Quand l'utilisateur allume le commutateur sans adresse saisie, l'interface propose `127.0.0.1:8787`.
- Mode serveur (`desktop --web`) : `enabled` est ignoré, l'interface est le processus lui-même.

**`url`** (adresse publique)

- Forme : `http://hôte[:port]` ou `https://hôte[:port]`, sans chemin, sans paramètres, sans identifiants. Autre
  forme : erreur de réglage, interface éteinte, raison dans `webUI.msg`.
- Sources, par priorité : option `--url` > variable `CL_WEB_URL` > `desktop_web.url`.
- L'interface est toujours servie à la racine du nom (pas de préfixe de chemin).

**Modification depuis un client web** : refusée pour tout `desktop_web` (`enabled` et `url` compris) et pour
`browser_server`, comme en v6.2.0.

## Effets de l'adresse publique (#171)

| Contrôle | Sans `url` (v6.2.0) | Avec `url` |
|----------|---------------------|------------|
| `Host` admis | boucle locale, hôte d'écoute, nom de la machine — sur le port d'écoute | les mêmes, **plus** l'hôte de `url` sur le port de `url` (ou sans port si `url` n'en a pas) |
| Origine admise | `http://<Host>` | `http://<Host>` pour un hôte local ; **l'origine de `url` exactement** (schéma, hôte, port) pour l'hôte public |
| Cookie de session | `HttpOnly`, `SameSite=Strict` | les mêmes, **plus `Secure` sur toute réponse** quand `url` est en `https` |
| Lien d'amorçage affiché | `http://<écoute>/?t=…` | `<url>/?t=…` |

- Conséquence du cookie `Secure` : un accès direct en HTTP au port du pod ne garde pas de session, et ne donne
  aucun cookie réutilisable en HTTPS.
- **Aucun en-tête `X-Forwarded-*`, `Forwarded` ou `X-Real-IP` n'est lu.** Le schéma et l'hôte publics viennent
  seulement de `url`.
- Un autre nom d'hôte, ou une autre origine, reçoit `403` comme aujourd'hui.

## Routes HTTP **[CHANGED]**

| Route | Session | Changement |
|-------|---------|------------|
| `GET /healthz` | aucune, **sans contrôle de `Host`** | **NEW** — `200`, corps `ok`. Vivant : le serveur répond |
| `GET /readyz` | aucune, sans contrôle de `Host` | **NEW** — `200` `ok` quand l'application a fini son chargement initial, `503` avant |
| `GET /api/events` | cookie | en-têtes ajoutés : `X-Accel-Buffering: no`, `Cache-Control: no-store, no-transform` |
| autres | inchangé | — |

- `/healthz` et `/readyz` ne rendent aucune information (ni version, ni nom, ni état des projets). Elles sont
  exemptées du contrôle de `Host` parce que les sondes du cluster appellent l'adresse du pod.
- Flux d'évènements : battement `: ping` au plus toutes les 20 s (inchangé, compatible avec un délai de proxy de
  60 s).

## Reconnexion du flux côté interface **[CHANGED]** (#171)

- Coupure du flux : nouvelle tentative après 1,5 s, puis délai doublé à chaque échec jusqu'à 15 s ; remis à 1,5 s
  dès qu'une connexion aboutit. Après reconnexion : `init` puis remise à jour de l'affichage (inchangé).
- Réponse `401` ou `403` à `/api/events` ou `/api/call` : la session est perdue. L'interface arrête de réessayer et
  affiche « session expirée — rouvrir le lien d'accès ».

## Instantané — champ `webUI` **[CHANGED]** (#183, #171)

```json
"webUI": { "state": "off" | "on" | "error", "listen": "127.0.0.1:8787", "lan": false, "warn": "", "msg": "",
           "enabled": true, "url": "https://launcher.exemple", "server": false, "access": "token" }
```

| Champ | Sens |
|-------|------|
| `enabled` | **NEW** — valeur effective du commutateur |
| `url` | **NEW** — adresse publique effective, vide sinon |
| `access` | **NEW** (LOT-4.4) — mode d'accès effectif : `"token"` ou `"none"` |
| `server` | **NEW** — vrai en mode serveur (`desktop --web`) : `state` vaut alors toujours `on`, `listen` est l'adresse du lancement |

Jamais le jeton, comme aujourd'hui.

## `configModel` / `configApply`, `kind: "hub"` **[CHANGED]** (#183)

`settings.desktop_web` gagne `enabled` (booléen ou absent), `url` (chaîne) et `access` (`token` | `none` | absent,
LOT-4.4). Les clés de changement `desktop_web.enabled`, `desktop_web.url` et `desktop_web.access` suivent les règles des clés existantes `desktop_web.listen` et
`desktop_web.allow_lan` (validation, refus à un client web, surcharge par système en lecture seule).

## Valeurs effectives par système **[CHANGED]** (#183, exigence de cohérence des Réglages)

`browser_server` et `desktop_web` se résolvent champ par champ : `hubs.<machine>.<système>` > `hubs.<machine>` >
`defaults`. Les rubriques « Serveur de maquettes » et « Interface web » affichent et modifient la valeur
**effective** pour ce poste et ce système, jamais une valeur masquée.

- `configModel`, `kind: "hub"` : nouveau champ `model.resolved` =
  `{ "browser_server": {listen,url,allow}, "desktop_web": {listen,allow_lan,enabled,url},
  "browser_paths": [{container,host,os}], "browser_device": "…", "sources": {...} }`.
  `browser_paths` et `browser_device` suivent la même règle de priorité mais **sans fusion** : la couche la plus
  prioritaire qui les pose les remplace entièrement (`browser_paths` : `defaults` aussi ; `browser_device` :
  `hubs.<machine>` et surcharge seulement). `sources` donne, pour chaque champ (`browser_server.listen`,
  `desktop_web.enabled`, `browser_paths`, `browser_device`…), la couche qui gagne :
  `"defaults"`, `"hub"` (`hubs.<machine>`), le nom du système (`"windows"`, `"linux"`, `"darwin"` : surcharge) ou
  `""` (posé nulle part). `settings` et `overrides` restent inchangés (lecture des surcharges conservée).
- `configApply`, `kind: "hub"` : les changements `set`/`delete` d'`at` = `["browser_server"]`, `["desktop_web"]`,
  `["browser_paths"]`, `["browser_device"]`
  (objet effectif entier) ou `["<réglage>", "<champ>"]` sont portés **sur la couche qui gagne** pour le système du
  launcher : si `hubs.<machine>.<système>` définit le champ, c'est lui qui est écrit, sinon `hubs.<machine>`. Un
  champ absent de l'objet remplaçant est retiré de chaque couche qui le pose. Un `at` qui commence par un système
  (`["windows", …]`) est appliqué tel quel.
  Pour `browser_paths` et `browser_device`, `set` écrit la surcharge du système si elle les pose, sinon
  `hubs.<machine>` ; `delete` (retour au défaut) les retire de la surcharge et de `hubs.<machine>`.
- `delete` d'un champ (`at: ["<réglage>", "<champ>"]`) le retire de **toutes** les couches qui le posent
  (surcharge du système et `hubs.<machine>`) : aucune valeur masquée ne réapparaît. `defaults` ne s'édite pas
  depuis « Ce poste » : un champ qui ne vient que de `defaults` n'est ni modifié ni retiré par `delete` (sans
  erreur ; sa provenance `"defaults"` l'indique dans `sources`).
- Client web : l'empreinte d'exposition (refus de toute modification) couvre `desktop_web`, `browser_server`,
  `browser_paths` et `browser_device`, dans `defaults`, `hubs.<machine>` et les surcharges par système.

## Amorçage, cookie, adresse publique stricte, refus **[CHANGED]** (#171, cycle 1 — sécurité M1/F1/F2, revue M1)

- **Amorçage** : `GET /?t=<jeton>` (ou `/index.html?t=…`) ne sert jamais de page : jeton valide → cookie de session posé
  et `303 See Other` vers `/` (sans `t`), `Cache-Control: no-store`. Jeton invalide sans session → `403` (`session`) ;
  jeton invalide avec session valide → `303` vers `/` sans nouveau cookie. `GET /` sans `t`, avec cookie valide, sert
  la page sans toucher au cookie.
- **Nom du cookie** : `__Host-cl_session_<port>` (ou `__Host-cl_session`) quand l'adresse publique est en https
  (le navigateur impose alors `Secure`, `Path=/`, aucun `Domain`) ; `cl_session_<port>` sinon, inchangé.
- **`ParsePublicURL`** : l'hôte est une IP littérale ou un nom RFC 1123 (lettres, chiffres, `-`, `.`) ; un nom
  internationalisé est converti en punycode (`golang.org/x/net/idna`, déjà dans les dépendances : passé de indirect à
  direct, aucun module ajouté) ; `%`, `_`, espaces et toute autre ponctuation sont refusés. Le port par défaut du
  schéma (443 en https, 80 en http) est retiré : `https://h:443` ≡ `https://h` (`Origin` et `Host` du navigateur ne
  portent jamais ce port).
- **Refus 403** : l'en-tête `X-Launcher-Refusal` vaut `host` (hôte non admis), `origin` (origine non admise),
  `session` (cookie absent, expiré ou révoqué, jeton d'amorçage invalide) ou `method` (méthode non permise sur
  `/api/call`) ; le corps est un texte court (`hôte refusé`, `origine refusée`, `session absente ou expirée : …`,
  `méthode refusée`). Seul `session` signifie « session perdue » ; `host` et `origin` ne la sont pas.

## Sortie de démarrage de `desktop --web` en cluster **[CHANGED]** (#167)

Dans un pod, la sortie standard ne porte pas le jeton : adresse (publique si `url`) sans `?t=`, indication du fichier
`<cache>/desktop-web-token` ou de `CL_DESKTOP_TOKEN`, message d'arrêt par `SIGTERM`, pas d'avertissement
d'écoute hors boucle locale. Le lien d'amorçage complet reste celui de `webUIOpen` (fenêtre) ; hors cluster, la sortie
est inchangée.

## Mode d'accès : `token` ou `none` **[NEW]** (LOT-4.4 — décision utilisateur du 2026-11-17)

**Réglage** : `desktop_web.access`, valeurs `token` (défaut) et `none`. Sources, par priorité : variable
`CL_WEB_ACCESS` > `desktop_web.access` (couches `hubs.<machine>.<système>` > `hubs.<machine>` > `defaults`).
Valeur inconnue : erreur de réglage, interface éteinte, raison dans `webUI.msg`.

- Défaut du launcher (hub Linux, exe Windows) : `token` — comportement inchangé.
- Au démarrage ou au passage en `none`, le fichier `<cache>/desktop-web-token` d'un mode `token` précédent est **supprimé**
  (une suppression impossible n'empêche pas le démarrage : le fichier n'est plus lu). Retour en `token` : nouveau jeton.
- Défaut du chart Helm : `none` (`contracts/helm-values.md`).
- Le serveur de maquettes (`browser_server`) et son jeton d'appariement ne sont pas concernés.

**Garde** : `none` n'est accepté que si l'écoute est en boucle locale, **ou** si une adresse publique (`url`) en
`https` est réglée. Sinon : erreur de réglage, interface éteinte (en `desktop --web` : le processus s'arrête, code
non nul), message « accès sans jeton refusé : écoute réseau sans adresse publique en https ».
Une adresse publique en `http` est **refusée même sur une écoute en boucle locale** (message « accès sans jeton
refusé : adresse publique en http (https exigé : … ») : sans jeton, rien ne protège un trafic en clair derrière le
proxy. Boucle locale sans adresse publique : admise.

**Ce que devient chaque protection**

| Protection | `token` (inchangé) | `none` |
|------------|--------------------|--------|
| Jeton, lien d'amorçage `/?t=…` | exigés | aucun : le jeton n'est ni tiré, ni lu, ni écrit ; `<cache>/desktop-web-token` n'est pas créé ; `CL_DESKTOP_TOKEN` est ignoré |
| Cookie de session | posé à l'amorçage | **aucun cookie posé** ; un cookie reçu est ignoré |
| Session en mémoire | 8 h, 64 au plus | aucune |
| Contrôle de `Host` | oui | **conservé**, identique |
| Contrôle d'origine (`/api/call`, `/api/events`) | oui | **conservé**, identique |
| `POST` seul et corps JSON exigé sur `/api/call` | oui | **conservés** |
| En-têtes de sécurité, 32 flux au plus | oui | **conservés** |
| `/healthz`, `/readyz` | oui | **conservés** |
| Réglages refusés à un client web | oui | **conservé** — `desktop_web.access` compris |

**Routes en mode `none`**

| Route | Réponse |
|-------|---------|
| `GET /`, `GET /index.html` | la page, sans jeton ni cookie |
| `GET /?t=<quelconque>` (ancien lien) | `303` vers `/`, `Cache-Control: no-store`, sans cookie |
| `POST /api/call` | pas de contrôle de session ; `Host`, origine, méthode et corps JSON contrôlés comme en `token` |
| `GET /api/events` | pas de contrôle de session ; `Host` et origine contrôlés ; le flux n'est rattaché à aucune session |

- `X-Launcher-Refusal: session` n'est jamais émis en mode `none`. `host`, `origin` et `method` sont inchangés.
- Après un redémarrage du serveur, une page ouverte se reconnecte seule (pas de « session expirée »).

**Dans les deux modes — `Sec-Fetch-Site`** : un appel à `/api/call` ou `/api/events` dont l'en-tête
`Sec-Fetch-Site` vaut `cross-site` reçoit `403`, `X-Launcher-Refusal: origin`. En-tête absent (navigateur ancien,
outil en ligne de commande) : pas de refus à ce titre.
**En mode `none` seulement** : `Sec-Fetch-Site: same-site` (autre sous-domaine du même site) reçoit aussi `403`,
`X-Launcher-Refusal: origin` — aucun jeton ne distingue cette page du navigateur de l'utilisateur ; sont admis
`same-origin`, `none` (navigation de l'utilisateur) et l'en-tête absent (règles habituelles : `Origin`, corps JSON).
En mode `token`, `same-site` reste admis.

**Actions de la fenêtre native en mode `none`**

- `webUIRegen` : `{ "ok": false, "msg": "accès sans jeton : aucun jeton à régénérer" }`.
- `webUIOpen` : ouvre l'adresse sans `?t=`.
- Rubrique « Interface web » : l'encadré « Jeton d'accès » affiche « accès sans jeton », bouton « Régénérer le
  jeton » désactivé. Aucun contrôle n'est ajouté à l'écran : le mode se règle par le fichier, la variable ou le chart.

**Sortie de démarrage de `desktop --web`** en mode `none` (en cluster ou non) : l'adresse (publique si `url`) sans
`?t=`, puis la ligne « accès sans jeton (`access: none`) ». Aucune mention du fichier du jeton ni de
`CL_DESKTOP_TOKEN`.

**Risques acceptés par l'utilisateur jusqu'à v6.4.0** (#172) : en mode `none` derrière l'Ingress, tout poste de la
liste d'adresses pilote les projets ; tout pod du cluster atteint le Service du launcher sans passer par l'Ingress
(pas de NetworkPolicy).
