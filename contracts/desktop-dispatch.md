# Contrat — méthodes `Dispatch` de l'interface native

> Liaison frontend ↔ Go de `ng2/ui/desktop` : `App.Dispatch(method, args JSON) (résultat JSON, erreur)`, appelée par
> Wails (`Bridge.Call`) et par le mode `--web` (`POST /api/call`). Ce fichier ne décrit que les méthodes ajoutées ou
> modifiées à partir de v6.1.0. Le backend peut ajuster un champ pour une contrainte technique, en le notant dans
> `contracts/CHANGELOG.md`.

## v6.1.0 — LOT-2 : carte TEAM et panes cachés (#136)

### `cards` (évènement et réponse existants) — champ `team` **[CHANGED]**

Une ligne par teammate : chaque instance lancée, puis les teammates du template non démarrés.

```json
{ "name": "dev-backend", "tag": "v5.9.2", "companion": false, "model": "sonnet", "effort": "medium",
  "claude": "2.1.87", "pane": "%7", "started": true, "active": false, "hidden": true, "pinned": false,
  "idleLeft": 0 }
```

- `pane` : identifiant tmux, vide si non démarré. `hidden` : pane caché (mis de côté). `pinned` : visible fixé à la main.
- `idleLeft` : secondes avant masquage automatique (0 si sans objet).
- `count` est retiré (plus de regroupement par type).
- **[cycle 4] État du template** (ajoute à la ligne) : `"tplState": "ok"|"late"|""`, `"tplTarget": "v5.9.2"`,
  `"companionFile": "dev-backend.md"`. `tag` reste la version enregistrée de l'agent
  (`agents.registry[].tag`). `tplState` : `ok` = `tag` égal à `tplTarget` (● vert), `late` = différent (▲ orange),
  `""` = neutre (projet sans registre d'agents, `tag` ou cible inconnus). `tplTarget` : version cible (tag du
  template adopté par le launcher, `.template-version`), vide si inconnue (infobulle « version cible inconnue »).
  `companionFile` : fichier compagnon existant (`<nom>.md`, `generic.<nom>.md` pour une instance générique),
  vide s'il n'y en a pas ; `companion` (existant) vaut vrai dès qu'il y en a un (« + » devant le nom). Les
  teammates non démarrés portent les mêmes champs.

### `teamToggle` **[NEW]**

Arguments : `{ "path": "<projet>", "pane": "%7" }`.
Résultat : `{ "ok": true }` ou `{ "ok": false, "msg": "<raison affichée en pied de volet>" }` (teammate actif,
leader, lecture seule, projet fermé).

### Évènement `term` / `layout` **[inchangé]**

Un pane caché n'est plus dans la fenêtre : il disparaît de `layout.panes` (son terminal est libéré côté interface)
et y revient au retour (contenu recapturé).

## v6.1.0 — LOT-3.1 : configuration structurée, global (#118)

### `configModel` **[NEW]**

Arguments : `{ "kind": "backends.yaml" }` (plus tard `config.conf`, `env.yaml`, `project-env` avec `path`).
Résultat : `{ "path", "mtime", "exists", "problems": [ConfigProblem], "model": { … } }`.
`model` pour `backends.yaml` : `backends` (liste ordonnée : `name`, `type`, `context`, `host`, `hosts`, `pool`,
`engine`, `engine_host`, `shared`, `namespace`, `volumes`, `spread`, `pod`, `implicit`), `pools`
(`name`, `hosts`, `drain`, `usedBy`), `defaults` (`backend`, `image`, `browser_paths`, `browser_server`), `images`.

### `configApply` **[NEW]**

Arguments : `{ "kind", "path", "mtime", "changes": [ { "op": "set"|"delete"|"rename", "at": ["backends","srv1"],
"value": … } ] }`.
Effet : les changements sont appliqués au texte du fichier **en conservant commentaires et ordre**, puis le texte
passe par `ValidateConfig` et `SaveConfig` (mêmes règles, même contrôle de conflit, même copie `.bak`).
Résultat : identique à `configSave` — `{ "saved", "problems", "note", "mtime" }`, `{ "conflict", "mtime" }` ou
`{ "invalid", "problems" }`.

### `backendTest` **[NEW]**

Arguments : `{ "backend": { …champs du formulaire… } }` (backend non encore enregistré accepté).
Résultat : `{ "steps": [ { "label": "outil requis : docker", "ok": true, "detail": "…", "fix": "" } ] }`.
Étapes : outil requis, moteur joignable (mêmes contrôles que le diagnostic existant), stockage partagé ; pour
`k8s` : namespace, droits, PVC.

### Précisions d'implémentation (LOT-3.1)

- `configModel` : `kind` seulement `backends.yaml` pour l'instant (autre : erreur d'appel). Fichier absent :
  `exists: false`, modèle vide (l'exemple commenté du modèle n'est pas lu). `model.backends` et `model.pools` sont
  dans l'ordre du fichier ; `pools[].usedBy` liste les backends qui citent le pool ; `defaults.browser_paths` /
  `browser_server` absents quand non déclarés.
- `configApply` : `changes[].op` = `set` (crée ou remplace la valeur au chemin `at` ; un objet est fusionné clé à
  clé, les clés absentes de la valeur sont retirées, les valeurs vides omises), `delete`, `rename` (`value` : nouveau
  nom de la dernière clé). Un changement impossible (chemin inconnu, nom pris…) rend `{ "invalid": true,
  "problems": [...] }` comme une erreur de validation. Les changements sont appliqués au contenu **sur disque** ; un
  fichier modifié depuis `mtime` rend `{ "conflict": true, "mtime" }`.
- `backendTest` : `backend` porte les champs du formulaire (`name`, `type`, `context`, `host`, `hosts`, `pool`,
  `engine`, `engine_host`, `shared`, `namespace`, `volumes`, `spread`, `pod`) ; ses pools sont ceux du fichier. Une
  forme invalide rend une seule étape en échec. Délai global 45 s (étape « délai »). Étapes : `outil requis : <outil>`
  (arrêt s'il manque), `moteur joignable`, `stockage partagé` ; k8s : `cluster joignable, namespace …`, `droits dans
  …`, `volumes partagés`, `PVC … (<racine>)`. Pool : étapes préfixées par le serveur.
- `backendStates` : `pending: true` = diagnostic jamais fait ou expiré, lancé en tâche de fond (relire quelques
  secondes plus tard) ; `load` : conteneurs en marche par machine de chaque pool, tous backends du pool confondus.

### `backendStates` **[NEW]**

Arguments : aucun. Résultat : `{ "states": { "<backend>": { "ok": true, "text": "joignable", "pending": false } },
"load": { "<pool>": { "<machine>": 3 } } }` — depuis le cache du diagnostic, sans bloquer l'affichage.

## v6.1.0 — LOT-3.2 : réglages d'un projet (issue fille de #118)

### `projectSettings` **[NEW]**

Arguments : `{ "path", "changes"?: [ …mêmes changements que configApply… ] }`. `changes` (facultatif) donne l'aperçu
fusionné d'un `env.yaml` MODIFIÉ, sans rien écrire (aperçu en direct du formulaire).

Résultat :
```json
{ "open": false, "assigned": true, "backend": "srv1-docker", "backends": ["srv1-docker", "lab"],
  "image": "docker.io/library/debian:12", "images": ["docker.io/library/debian:12"],
  "color": "53", "colorAuto": true, "palette": ["17", "52"],
  "env": { "image": "", "packages": [], "packages_by_manager": {}, "setup": [], "watch": [], "on_start": [],
           "local": [], "mounts": [ { "path": "/data", "ro": true } ], "prefer": "" },
  "envPath": "<projet>/.claude-launcher/env.yaml", "envMtime": "", "envExists": false,
  "templatePath": "<projet>/.claude-launcher/env-template.yaml", "hasTemplate": true,
  "merged": [ { "text": "image: debian:12", "from": "project" }, { "text": "packages:", "from": "" },
              { "text": "  - gh", "from": "launcher" } ],
  "problems": [] }
```
- `backend` : nom du backend attribué (`host` : sur l'hôte ; vide : jamais configuré) ; `backends` : backends à
  conteneurs, plus `host` quand il n'est pas refusé sur ce système. `assigned` : un backend a été choisi pour ce projet.
- `image` : socle de l'environnement fusionné (vide : aucun) ; `images` : socles proposés (celui du projet inclus).
- `color` : couleur effective (indice tmux 256 sous forme de texte) ; `colorAuto` vrai tant qu'aucune couleur n'est
  fixée dans `PROJECT_COLORS` ; `palette` : couleurs proposées (`COLOR_PALETTE`).
- `env` : champs de l'`env.yaml` du PROJET seul (pas du template ni du launcher), `mounts[].ro` absent = lecture
  seule par défaut. `envMtime` vide tant que le fichier n'existe pas.
- `merged` : environnement fusionné, ligne par ligne, avec la provenance de chaque ÉLÉMENT : `template`
  (`env-template.yaml`), `launcher` (`env.yaml` du launcher : `packages`, `packages_by_manager`, `setup`, `on_start`,
  placés avant le reste comme au bootstrap) ou `project` ; les lignes d'intitulé (`packages:`) ont `from` vide.
  Même fusion que le démarrage d'un conteneur (listes réunies sans doublon, étapes du launcher puis du template puis
  du projet, `image` et `prefer` du projet l'emportent, `mounts` réunis par chemin).
- `problems` : validation de l'`env.yaml` du projet (même code que le chargement réel).

### `projectApply` **[NEW]**

Arguments : `{ "path", "backend"?, "image"?, "color"?, "env"?: { "mtime", "changes": [...] } }` — chaque partie est
facultative et indépendante.
- `backend` / `image` : refusés si le projet est ouvert (`{ "ok": false, "msg": "projet ouvert — le fermer avant de
  changer de backend" }`, comme `execApply`). `backend: "host"` : exécution sur l'hôte (l'image est ignorée) ; sinon
  `image` est le socle (par défaut celui de l'environnement actuel ; sans image connue : refus) et le backend doit être
  un backend à conteneurs connu.
- `color` : indice de la palette (`"0"`–`"255"`) ; chaîne vide : couleur automatique (clé retirée de `PROJECT_COLORS`).
  Seule cette clé de `config.conf` est écrite (les autres lignes, commentaires compris, sont conservés ; même
  validation, copie `.bak`).
- `env.changes` : mêmes opérations que `configApply` (`set` / `delete` / `rename`), appliquées à l'`env.yaml` du
  projet (commentaires et ordre conservés, reformatage accepté) ; `env.mtime` : date à l'ouverture
  (`envMtime`), conflit si le fichier a changé depuis.
- **Règles figées** :
  1. un montage sans `ro` est en lecture seule (le modèle ne rend `ro` que s'il est écrit dans le fichier) ;
  2. `color: ""` = couleur automatique (l'entrée est retirée de `PROJECT_COLORS`), `color` absent = inchangé ;
  3. `backend: "host"` : l'image est ignorée ; backend conteneur sans `image` : l'image de l'environnement actuel
     est reprise, et s'il n'y en a pas, refus `{ "ok": false, "msg": "image de base à choisir pour un backend
     conteneur" }`. `image` seule (sans `backend`) garde le backend actuel (refus « backend à choisir avec l'image »
     s'il n'y en a pas ou s'il est `host`).
- Ordre : contrôles sans écriture (refus projet ouvert, backend, image, couleur), puis `env`, puis `color`, puis
  `backend`/`image`. Un échec de `env` (invalide, conflit) arrête tout avant toute autre écriture.

**Résultat partiel** : si une étape POSTÉRIEURE à l'écriture d'`env.yaml` échoue (couleur, backend/image), la
réponse est `{ "ok": false, "msg": "…", "env": { "saved": true, "problems": [], "mtime": "<nouveau mtime>" },
"colorSaved": true? }` : `env` dit ce qui est écrit (l'interface rafraîchit son modèle et reprend `env.mtime`,
sinon son enregistrement suivant serait en conflit avec le sien) ; `colorSaved` vaut vrai quand `config.conf` a
été écrite. Un échec AVANT toute écriture n'a ni `env` ni `colorSaved`.
`colorSaved` s'applique aussi SANS changement d'env : `{ "ok": false, "msg": "…", "colorSaved": true }` (couleur
écrite puis backend/image en échec : l'interface recharge, sans `env`).
La vérification « projet ouvert par un autre launcher » est bornée à 5 s ; au délai, backend/image sont refusés :
`{ "ok": false, "msg": "impossible de vérifier si le projet est ouvert ailleurs (backend injoignable)" }` (couleur et
`env.yaml` ne passent pas par cette vérification).
L'image demandée entre dans les changements d'`env.yaml` : le fichier n'est écrit qu'une fois.
**Chemin** : `path` doit être un projet de l'arbre (sous les racines) ; sinon l'appel échoue (`projet inconnu`),
comme `configFiles` / `configOpen` / `configValidate` / `configSave` quand ils visent un projet. `open` (et le refus
backend/image) vaut vrai aussi quand un autre launcher a la session du projet vivante dans son conteneur.
**Couleur** : `PROJECT_COLORS` est modifié dans toutes ses formes bash d'affectation (littéral de tableau avec ou sans
`declare`/`typeset`/`-gA`, `+=`, affectations d'éléments, déclaration seule) sans toucher aux autres couleurs ; une
forme non reconnue (boucle, `eval`, `unset`…) est refusée avec un message. Limite : la couleur d'un projet déjà
ouvert n'est prise qu'à sa prochaine ouverture.

Résultat : `{ "ok": true, "note": "…", "env": { "saved": true, "problems": [], "mtime": "…" } }` (`env` seulement s'il
y avait des changements) ; refus : `{ "ok": false, "msg": "…" }` ; `env` invalide ou en conflit :
`{ "ok": false, "env": { "invalid": true, "problems": [...] } }` ou `{ "ok": false, "env": { "conflict": true, "mtime": "…" } }`.

## v6.1.0 — LOT-3.3 : ce poste, launcher, conteneurs (issue fille de #118)

`configModel` et `configApply` (LOT-3.1) acceptent trois `kind` de plus ; mêmes résultats que pour `backends.yaml`
(`{ path, mtime, exists, problems, model }` / `{ saved, problems, note, mtime }` | `{ conflict, mtime }` |
`{ invalid, problems }`), même validation que le chargement réel, même contrôle de conflit, copie `.bak`. Aucun chemin
ni nom de projet n'est fourni par le client : les fichiers sont ceux du poste.

### `kind: "config.conf"` — réglages du launcher

`model` : `{ "groups": [ { "id": "teams", "label": "Teams et panes" } ], "settings": [ … ], "unrecognized": [ "KEY" ] }`.
Chaque réglage :
```json
{ "key": "TEAM_HIDE_IDLE", "group": "teams", "label": "Cacher les teammates inactifs après (s)",
  "help": "0 = jamais", "type": "int", "value": 30, "default": 30, "set": false, "editable": true,
  "min": 0, "max": null, "options": [] }
```
- `type` : `string`, `path`, `int`, `bool` (fichier : `1`/`0`), `enum` (`options`), `list` (valeur : tableau de
  chaînes), `intlist` (`COLOR_PALETTE`), `secret` (`GITHUB_TOKEN`).
- `value` : valeur écrite dans le fichier telle quelle (`$HOME/GITHUB` n'est pas développé), sinon la valeur par défaut ;
  `set` : le fichier l'écrit. `editable: false` (+ `why`) : forme bash non reconnue (boucle, `eval`, `+=`, valeur
  composée…) → champ en lecture seule avec renvoi à « Éditer en YAML ». `unrecognized` : leurs clés.
- **`GITHUB_TOKEN` n'est jamais renvoyé** : `{ "key": "GITHUB_TOKEN", "type": "secret", "set": true }` sans `value`.
  Il n'est écrit que par un `set` explicite avec la nouvelle valeur ; `delete` le retire. Aucun message de
  problème ne le contient (remplacé par `***`).
- Réglages : `GITHUB_DIR`, `GITHUB_DIRS`, `GITHUB_TOKEN`, `CLAUDE_OPTIONS`, `CLAUDE_DISABLE_MOUSE`,
  `CLAUDE_EXPERIMENTAL_TEAMS`, `CLAUDE_CONFIG_DIR`, `EXTRA_ENVS`, `RESUME_MODE`, `RESUME_MAX_AGE_H`, `TEAM_HIDE_IDLE`,
  `TEAM_HIDE_IDLE_ENABLED` (v6.3.0, #188),
  `LAYOUT_2ROW_LEADER_PCT`, `LAYOUT_3ROW_LEADER_NUM`, `LAYOUT_3ROW_MID_NUM`, `LAYOUT_3ROW_DEN`,
  `PANE_ACTIVE_BRIGHTEN_STEP`, `PANE_INACTIVE_DARKEN_STEP`, `COLOR_PALETTE`, `CTR_STOP_ON_EXIT`, `USAGE_REFRESH_SEC`.
  `PROJECT_COLORS` est réglé par projet (`projectApply`).

`configApply` : `changes` = `{ "op": "set", "at": ["KEY"], "value": … }` (valeur typée : nombre, booléen, chaîne ou
tableau) ou `{ "op": "delete", "at": ["KEY"] }` (retour à la valeur par défaut : l'affectation est retirée). La
valeur est contrôlée avant toute écriture (type, bornes, cohérence de la mise en page des teams, syntaxe d'une
variable) ; une valeur refusée rend `{ "invalid": true, "problems": [ … ] }`. Seule la ligne de la clé est réécrite :
commentaires et autres lignes conservés ; clé absente : ajoutée sous son exemple commenté, sinon en fin de fichier.
Les valeurs ne peuvent contenir ni `"`, ni antislash, ni guillemet inverse, ni `$(`, ni retour à la ligne ; `$NOM` et
`${NOM}` restent permis. Le fichier est écrit en `0600`. `config.conf` absent (Windows) : créé depuis le modèle par
défaut à l'enregistrement.

### `kind: "env.yaml"` — outillage ajouté à tous les conteneurs

`model` : `{ "packages": [], "packages_by_manager": {}, "setup": [], "on_start": [] }` (seules clés du fichier du
launcher). `configApply` : mêmes `changes` que `projectApply.env` (`set` / `delete` / `rename` par chemin, par
`yaml.v3` Node : commentaires et ordre conservés).

### `kind: "hub"` — ce poste (`hubs.<machine>` de `backends.yaml`)

`model` :
```json
{ "hub": "MON-PC", "present": true, "goos": "windows",
  "settings": { "paths": [ { "local": "…", "remote": "…" } ], "browser_paths": [ { "container": "…", "host": "…", "os": "…" } ],
                "browser_server": { "listen": "", "url": "", "allow": [] },
                "remote_user": { "uid": null, "gid": null, "name": "", "home": "" },
                "hub_cache": "", "browser_device": "" },
  "overrides": { "windows": { "keys": ["paths"], "settings": { … } }, "linux": …, "darwin": … },
  "effective": { "paths": "windows" },
  "termFontSize": 13 }
```
- `hub` : nom de la machine résolu (clé existante du fichier, insensible à la casse et au domaine, sinon le nom de
  l'ordinateur) ; `present` faux : la section n'existe pas encore (créée au premier `set`).
- `settings` : réglages communs ; `overrides.<système>` : surcharges propres à un système (clé à clé, `keys` les
  nomme) — celle du système courant (`goos`) prime : `effective` dit, par clé, quelle source s'applique (`""` :
  réglage commun, sinon le système). Une surcharge se modifie par `at` : `["windows", "paths"]`.
- `termFontSize` : préférence d'affichage par poste (`desktop-ui.json`), lue ici pour l'écran ; elle s'écrit par
  `setPrefs` (inchangé).
`configApply` : `at` est relatif à la section de la machine (`["paths"]`, `["windows", "hub_cache"]`) ; mêmes
opérations et résultat que pour `backends.yaml` (fichier, validation et conflit communs : `mtime` est celui de
`backends.yaml`).

### Règles figées (LOT-3.3)

1. **Formes des valeurs envoyées** (config.conf) : `int` en nombre, `bool` en booléen, `string`/`path`/`enum` en
   chaîne, `list` en tableau de chaînes, `intlist` (`COLOR_PALETTE`) en tableau de nombres OU de chaînes numériques
   (le modèle les rend en chaînes `["17","52"]`).
2. **Valeur vide = `delete`** : pour retirer un réglage, envoyer `{ "op": "delete", "at": [clé] }` (config.conf :
   retour au défaut, sans effet si la clé n'est pas écrite). Pour `hub`, `env.yaml` et `backends.yaml`, un `delete`
   d'une clé ABSENTE est une erreur (`invalid`, « introuvable ») : ne le demander que si le modèle la montre écrite.
3. **Objets entiers** : `browser_server` (`{ listen, url, allow }`) et `remote_user` (`{ uid, gid, name, home }`) sont
   envoyés en un seul `set` d'objet ; il est fusionné clé à clé (les clés absentes ou vides sont retirées, `uid` et
   `gid` en nombres). `paths` : `[ { local, remote } ]` ; `browser_paths` : `[ { container, host, os? } ]` ; les listes
   sont remplacées en bloc.
4. **env.yaml du launcher** : `at` = `["packages"]`, `["setup"]`, `["on_start"]` ou `["packages_by_manager"]` (toute
   autre clé est refusée par la validation).

### Sécurité (LOT-3.3, audit)

- **Éditeur de texte et jeton** : `configOpen` d'un `config.conf` remplace la valeur de `GITHUB_TOKEN` par le marqueur
  `__GARDER__` (`GITHUB_TOKEN="__GARDER__"`) ; à l'enregistrement (`configSave`), une ligne qui vaut encore le marqueur
  reprend le jeton actuel (le jeton ne quitte jamais le poste) ; une nouvelle valeur le remplace, une valeur vide le
  retire ; sans jeton actuel, le marqueur n'est jamais écrit. L'interface peut l'indiquer : « jeton masqué : laisser
  `__GARDER__` le conserve ». Les messages de problème (`configOpen`, `configValidate`, `configSave`, `configApply`)
  ne contiennent jamais le jeton (un jeton de moins de 8 caractères remplace le message entier).
- **Chemins** : `configApply` (comme `projectSettings` / `projectApply` / `config*` visant un projet) n'accepte qu'un
  projet de l'arbre ; sinon `projet inconnu`.
- **Valeurs de `config.conf`** : `$NOM` / `${NOM}` ne développent que `HOME`, `USER`, `LOGNAME`, `PWD`, `XDG_*`,
  `GITHUB_DIR`, `CLAUDE_CONFIG_DIR` (jamais un secret) ; `EXTRA_ENVS` refuse `LD_*`, `DYLD_*`, `PATH`, `BASH_ENV`, `ENV`,
  `IFS`, `PS4`, `PROMPT_COMMAND`, `SHELLOPTS`, `BASHOPTS`, `GIT_SSH*`, `GIT_EXEC_PATH`, `NODE_OPTIONS`, `NODE_PATH`,
  `PYTHONSTARTUP`, `PYTHONPATH`, `PERL5OPT`, `RUBYOPT`.
- **backends.yaml** : `namespace` (DNS-1123), noms de PVC et `context` k8s validés ; `ssh://` dont la destination
  commence par « - » refusé (`host`, `hosts`, `engine_host`). `backendTest` : deux tests à la fois au plus ; au-delà de
  3 s d'attente : une étape `{ "label": "test refusé", "ok": false }`.
- **Mode `--web`** : l'en-tête `Host` doit désigner la boucle locale (`127.0.0.1`, `localhost`, `[::1]`, et le port
  d'écoute) quand le serveur écoute sur la boucle locale ; le jeton de l'URL n'est accepté que par la page
  d'amorçage (puis cookie) — `/api/call` et `/api/events` n'acceptent que le cookie ; en-têtes `Referrer-Policy:
  no-referrer`, `X-Content-Type-Options`, `X-Frame-Options`, `Content-Security-Policy: frame-ancestors 'none'`.
- **Droits** : `config.conf.bak` préexistant plus ouvert que `0600` est resserré ; un `.bak` qui est un lien est remplacé.

### Sécurité — compléments (contrôle LOT-3.3)

- **`/api/call` (mode `--web`)** exige `Content-Type: application/json` (sinon `415` : une requête « simple » n'a pas de
  pré-requête CORS) et refuse (`403`) un appel dont l'en-tête `Origin`, s'il est présent, n'est pas exactement
  `http://<Host servi>` (même schéma, hôte et port) : une autre page de la machine — même sur `127.0.0.1` — ne peut pas
  émettre d'appel avec le cookie. Même contrôle d'`Origin` pour `/api/events`. L'interface envoie du JSON (vérifié par test).
- **Éditeur de texte et jeton** : le jeton est masqué (`__GARDER__`) partout où il figure — commentaire, tableau, autre
  clé comprise — pour un jeton d'au moins 8 caractères. À l'enregistrement le marqueur ne se restaure que dans
  `GITHUB_TOKEN` : laissé dans un commentaire il y reste (le commentaire perd l'ancien jeton) ; dans toute autre
  instruction l'enregistrement est refusé (`invalid`, « le marqueur __GARDER__ ne convient que pour GITHUB_TOKEN »).
  Un jeton actuel de forme dangereuse (écrit à la main avec `$`, `` ` `` ou `"`) n'est pas réécrit : refus explicite.
- **`EXTRA_ENVS`** refuse en plus : `GIT_CONFIG*`, `GIT_PROXY*`, `*_ASKPASS`, `*_PROXY` (`HTTP_PROXY`, `ALL_PROXY`…),
  `*PAGER`, `*_EDITOR`, `EDITOR`, `VISUAL`, `BROWSER`, `SHELL`, `HOME`, `BASH_FUNC_*`, `ANTHROPIC_*`, `PYTHONHOME`,
  `PERL5LIB`, `RUBYLIB`, `JAVA_TOOL_OPTIONS`, `_JAVA_OPTIONS`, `LUA_INIT`, `NODE_EXTRA_CA_CERTS`, `CURL_HOME`,
  `OPENSSL_CONF`. La liste reste une barrière partielle : le champ garde la portée de « code au lancement » pour qui
  édite le fichier en texte (« Éditer en YAML »), qui accepte toutes les variables, `ANTHROPIC_API_KEY` comprise.
- **Revue (LOT-3.3)** : `set GITHUB_TOKEN` refuse le marqueur `__GARDER__` (il écraserait le jeton actuel) ; le jeton
  recopié dans une autre valeur (`EXTRA_ENVS`, `CLAUDE_OPTIONS`…) ne sort pas de `configModel` : valeur masquée (`***`),
  `editable: false` (« contient le jeton GitHub »), clé dans `unrecognized` ; un ancien jeton de forme `ghp_…` /
  `github_pat_…` laissé en commentaire est masqué par `__GARDER__` dans l'éditeur de texte ; la cohérence de la mise en
  page des teams n'est contrôlée que si l'une des trois clés `LAYOUT_3ROW_*` change (une combinaison déjà invalide ne
  bloque pas les autres réglages) ; la copie `.bak` est recréée en `0600` d'emblée (l'ancienne est supprimée avant).
- **`EXTRA_ENVS` existant (M6)** : la liste de refus ne vise que les éléments NOUVEAUX ou MODIFIÉS (chaîne `NOM=valeur`
  différente de celle du fichier) ; un élément déjà écrit tel quel (`HTTPS_PROXY`, `ANTHROPIC_BASE_URL`…) ne bloque pas
  l'édition du reste de la liste, et peut être retiré. Les ajouts restent refusés ; « Éditer en YAML » reste la voie.

## v6.1.0 — LOT-4 : presse-papiers et collage dans les terminaux (#155)

### `clipboardWrite` **[NEW]**

Arguments : `{ "text": "…" }`. Copie `text` dans le presse-papiers du poste.
Résultat :
- `{ "ok": true }` ;
- `{ "ok": false, "unavailable": true, "msg": "…" }` : pas de presse-papiers côté Go (mode `--web`, ou binaire sans
  Wails) — le frontend prend alors `navigator.clipboard` du navigateur (contexte sécurisé : `localhost` convient ; en
  `http` sur une autre adresse il est indisponible : afficher le `msg`) ;
- `{ "ok": false, "msg": "…" }` : refus ou échec (texte de plus de 8 Mo, erreur du système) — à afficher tel quel.

### `clipboardRead` **[NEW]**

Arguments : aucun. Résultat : `{ "ok": true, "text": "…" }` (chaîne vide si le presse-papiers ne contient pas de texte),
`{ "ok": false, "unavailable": true, "msg": "…" }` (comme ci-dessus) ou `{ "ok": false, "msg": "…" }` (échec, ou
contenu de plus de 4 Mo : taille maximale d'un collage).

- **Fenêtre native Wails** (Windows) : presse-papiers du poste lu et écrit par le runtime Wails, côté Go — pas d'invite
  d'autorisation de WebView2.
- **Mode `--web` et binaire sans Wails** : les deux méthodes répondent toujours `unavailable` ; le presse-papiers de la
  machine qui héberge le serveur n'est **jamais** lu ni écrit pour un client web (même si le serveur tourne sur un poste
  qui en a un).
- Aucune dépendance au projet ni à l'état de lecture seule : copier est toujours permis, y compris dans un onglet en
  lecture seule (le collage, lui, est refusé par `termInput`).

### `termInput` **[CHANGED]**

Retour (auparavant toujours vide) : vide quand la saisie est partie ; sinon `{ "ok": false, "reason": "readonly" |
"toobig", "msg": "…" }` :
- `readonly` : le projet est piloté par un autre launcher (lecture seule) — la saisie n'est plus ignorée en silence : le
  message est renvoyé ET affiché en pied de volet (au plus une fois par seconde par onglet) ;
- `toobig` : `data` dépasse 4 Mo (taille maximale d'un collage), rien n'est envoyé.
Un collage volumineux (milliers de lignes) est envoyé en commandes `send-keys -H` de 256 octets au plus, dans l'ordre,
sans perte ; le frontend applique le collage entre crochets (`term.paste`) avant d'appeler `termInput`.

## v6.2.0 — LOT-1 : taille des caractères par zone (#150)

### Préférences `treeFontSize`, `cardsFontSize` **[NEW]**

Deux clés de `desktop-ui.json`, écrites par `setPrefs` (inchangé) et rendues par `init` avec les autres préférences.
Nombres entiers, mêmes bornes que `termFontSize` (8 à 28). Absentes : tailles actuelles (arbre 12,5 px, cartes 12 px),
ce qui correspond à la valeur 13 pour l'arbre et 12 pour les cartes. `termFontSize` est inchangée.

- `treeFontSize` : zone du navigateur (en-tête, filtre, arbre, « Nouveau projet », pied de volet).
- `cardsFontSize` : zone des cartes ◈ État.
- `configModel` `kind: "hub"` rend aussi `treeFontSize` et `cardsFontSize` à côté de `termFontSize` (lecture pour
  l'écran « Ce poste » ; écriture par `setPrefs`).

## v6.2.0 — LOT-2 : carte TEAM, en-têtes de panes, onglet Teamleaders (#151, #152, #162)

### `cards` — champ `teamHead` **[NEW]** et `team[].odd` **[NEW]** (#151)

```json
"teamHead": { "tpl": "v5.9.4", "claude": "2.1.87", "odd": 2 }
```

- `tpl` : version cible du template du projet (`tplTarget`), vide si inconnue. `claude` : version de claude du
  leader, vide si le projet est fermé. `odd` : nombre de teammates non conformes.
- Chaque ligne de `team` reçoit `"odd": ["tpl", "claude"]` (liste vide ou absente si conforme) : `tpl` quand son
  `tag` diffère de `teamHead.tpl` (les deux connus), `claude` quand il est démarré et que sa version diffère de
  `teamHead.claude` (les deux connues). Les champs existants (`tag`, `claude`, `tplState`, `tplTarget`…) sont
  conservés pour l'infobulle.
- Calcul dans `core` (même résultat pour la carte de la coque Linux).

### Évènement `term` / `layout` — `panes[].state` **[NEW]** (#152)

`"state": "lead" | "active" | "idle" | ""` — `lead` : leader ou agent `cdp` ; `active` / `idle` : teammate selon
`isActive` ; `""` : pane sans agent. `role` (texte affiché) et `active` (pane qui a le clavier) sont inchangés.
Le frontend ne déduit plus l'état du texte de `role`. Rendu de l'en-tête : maquette retenue
(`docs/mockup/v6.2.0/ui/pane_header__etat_activite.html`).

### Onglet Teamleaders (#162)

Aucun changement de contrat : `tabs` ne contient toujours que les projets ; la vue `leaders` reste côté frontend.
Si la maquette retenue affiche un compteur : il se déduit de `tabs` (`busy`, longueur).

## v6.2.0 — LOT-3 : rafraîchissement d'un projet et fermeture forcée (#158)

### `refreshProject` **[NEW]**

Arguments : `{ "path": "<projet>" }`. Résultat immédiat : `{ "ok": true }`, ou `{ "ok": false, "msg": "…" }` si un
rafraîchissement de ce projet est déjà en cours. Le travail se fait en arrière-plan ; l'avancement arrive par
l'évènement `refresh` :

```json
{ "path": "<projet>", "steps": [ { "id": "ctr", "state": "ok" }, { "id": "git", "state": "run" } ],
  "done": false, "msg": "" }
```

- Étapes (`id`), dans l'ordre : `ctr` (existence et état du conteneur ou du pod, diagnostic du backend), `session`
  (session tmux, propriétaire, reprise en attente), `git`, `github` (milestone et issues), `tpl` (version du
  template, registre des agents, team), `usage` (consommation). `state` : `wait` | `run` | `ok` | `err`.
- Chaque étape ignore son cache (diagnostic 60 s, template 60 s, registre 60 s, team 10 s, état direct 30 s,
  milestones) et réécrit le cache avec la valeur relue. `usage` respecte le délai minimal entre deux appels à l'API
  (`usageMinBackoff`) : dans ce délai, l'étape rend `ok` avec la valeur en cache.
- Dernier évènement : `done: true`, toujours émis (y compris sur erreur ou dépassement de délai — 30 s au total) ;
  `msg` résume (« pod absent — projet fermé », « GitHub non relu : délai dépassé »). Étape `github` : gh absent ou non connecté, dépôt hors GitHub = cas légitime (`ok`, raison dans `msg`) ; « dubious ownership » (projet d'un autre compte sur un partage, git refuse malgré le `safe.directory` posé par appel) = `err` avec la commande d'aide `git config --global --add safe.directory '<projet>'`. Un instantané (`snapshot`) et
  les cartes à jour suivent.
- Un onglet dont la session ou le conteneur n'existe plus est retiré ; la ligne du projet reprend l'état relu.
- La ligne du projet porte `"refreshing": true` dans l'instantané pendant l'opération.
- `refresh` (F5, global) est inchangé ; le frontend appelle en plus `refreshProject` sur le projet sélectionné.

### `forceClose` **[NEW]** et `forceCloseConfirm` **[NEW]**

`forceClose` — arguments `{ "path" }`. Résultat : `{ "question": "…", "why": "son pod k8s n'existe plus",
"items": ["onglet", "session tmux locale", …] }` (`why` : raison affichée dans la boîte de confirmation ; `items` : ce
qui sera nettoyé), ou `{ "msg": "…" }` si l'action est refusée ou sans objet.
`forceCloseConfirm` — arguments `{ "path" }`. Résultat : `{ "ok": true, "msg": "…" }` ou `{ "ok": false, "msg": "…" }`.

- Proposée (ligne du projet : `"canForce": true`) quand le projet est affiché ouvert ou en erreur — onglet présent,
  ou pastille `✕`, `▲`, `●` orange — alors que son conteneur, son pod ou sa session n'existe plus ou ne répond pas.
  `refreshProject` recalcule `canForce`.
- Backend qui ne répond pas (lecture des conteneurs non aboutie : `Errs`, `Down` ou `Diag`) : « absent » n'est pas
  prouvé. La fermeture forcée est quand même **proposée, avec avertissement** : `why` =
  « backend injoignable : le projet tourne peut-être encore », `items` sans arrêt ni kill côté backend (onglet,
  marque de sortie propre, nettoyage de l'état local). La confirmation ne fait que ce nettoyage local : aucun
  `Engine.Stop`, aucune session tuée côté backend. `refreshProject` ne retire **jamais** l'onglet si la lecture a
  échoué (état inconnu : la raison est dans l'étape `ctr` / `session`).
- Re-vérification à la confirmation : l'état est relu juste avant d'agir ; si le projet a revécu depuis la question
  (session locale ou de conteneur de nouveau vivante, conteneur de nouveau en marche) → `{ "ok": false, "msg": "… rien
  n'a été fermé" }`. Un conteneur en marche et sain n'est jamais arrêté par la fermeture forcée. Une question
  non confirmée au bout de 2 minutes est abandonnée.
- Refusée si un autre launcher vivant pilote le projet (`SessElse`, `SessReadOnly`) : message « prendre la main
  d'abord ».
- Effet, sans `/exit` ni attente : onglet retiré ; session tmux locale tuée (projet sur l'hôte : `p-<nom>`) ; panes
  cachés du projet tués ; marque de sortie propre posée (la reprise `↺` est abandonnée, la conversation reste dans
  `/resume`) ; verrou de création `<projet>/.claude-launcher/.lock` supprimé s'il est périmé ou posé par ce
  launcher ; fichier d'ordre des panes supprimé ; arrêt du conteneur demandé s'il existe encore (`Engine.Stop`,
  erreur ignorée et rapportée dans `msg`).
- `projects.json` (attribution du backend) n'est **pas** modifié : il ne porte aucun état d'ouverture.
- `close` / `closeConfirm` sont inchangés.

## v6.2.0 — LOT-4 : interface web servie par la fenêtre native (#163)

### Réglage `desktop_web` **[NEW]** (`backends.yaml`)

Même emplacement et même priorité que `browser_server` : `hubs.<machine>.<système>.desktop_web` >
`hubs.<machine>.desktop_web` > `defaults.desktop_web` > défaut (`listen: off`).

```yaml
hubs:
  MON-PC:
    desktop_web: { listen: "127.0.0.1:8770" }   # « 8770 » = 127.0.0.1:8770 ; off = désactivé (défaut)
```

- `listen` : `off`, un port seul (boucle locale) ou `hôte:port`. Un `listen` identique à celui de `browser_server`
  est refusé à la validation (deux serveurs distincts : fichiers en lecture seule sans jeton / interface complète
  avec jeton).
- Fenêtre native : le serveur de l'interface démarre **dans le même processus**, sur la même `App` ; réglage relu à
  chaud (démarrage, arrêt, changement de port). `desktop --web` sans `--addr` utilise ce `listen` s'il est fixé
  (sinon `127.0.0.1:0`, inchangé) ; `--addr` l'emporte.
- `configModel` `kind: "hub"` : `settings.desktop_web: { "listen": "" }` (et dans `overrides.<système>`).
- Sécurité : toutes les règles du mode `--web` s'appliquent (jeton accepté par la page d'amorçage seule,
  `/api/call` en JSON et même origine, en-têtes), avec ces précisions (audit sécurité LOT-4) :
  - **Session** : la page d'amorçage échange le jeton de l'URL contre un **identifiant de session aléatoire, gardé
    en mémoire** ; le cookie (`cl_session_<port>`, `HttpOnly`, `SameSite=Strict`) porte cet identifiant, **jamais le
    jeton**. Les sessions sont révoquées à l'arrêt du serveur, au changement d'adresse d'écoute et à la
    régénération du jeton ; un navigateur doit alors repasser par l'URL d'amorçage (`webUIOpen`). Dans
    `desktop --web` (processus seul), la relance après une mise à jour révoque aussi les sessions.
  - **Jeton** : tiré une fois par poste et conservé (`CacheDir()/desktop-web-token`, créé `O_EXCL`, `0600`) ; **renouvelé
    à chaque changement d'adresse d'écoute** ; action `webUIRegen` (fenêtre native seulement) : fichier
    supprimé, nouveau tirage, sessions et flux révoqués. Une erreur d'écriture du fichier est dite dans `webUI.msg`
    (le jeton tiré reste valable jusqu'à l'arrêt).
  - **Écoute hors boucle locale** : refusée tant que `desktop_web.allow_lan: true` n'est pas posé (résolution,
    validation de `backends.yaml` et modèle « Ce poste » : `settings.desktop_web.allow_lan`). Acceptée, elle applique le
    contrôle de `Host` (hôte d'écoute, nom de la machine, `localhost`, sur le port — aussi dans `desktop --web`) et
    `webUI.lan` vaut vrai et `webUI.warn` porte l'avertissement (HTTP en clair, jeton seul) ; accès par le nom de la machine ; sur une écoute
    joker (`0.0.0.0`, `::`) l'adresse IP est refusée par le contrôle de `Host`.
  - **Sessions** : 64 au plus (la plus ancienne est évincée), expirées après 8 h d'inactivité. **Flux d'évènements** :
    émission sans attente (un client dont le tampon est plein est abandonné et se reconnecte), verrou distinct de
    l'authentification.
  - Un flux d'évènements est fermé avec sa session (expiration — un flux ouvert n'est pas une activité —, éviction,
    révocation) ; chaque envoi a une échéance d'écriture de 10 s (connexion figée = flux fermé) et un gestionnaire compte dans
    le plafond de 32 flux jusqu'à sa vraie sortie.
  - **Jeton persisté** : la régénération est réécrite atomiquement sur disque ; la dernière adresse d'écoute est gardée
    (`desktop-web-listen`) : un changement d'adresse entre deux lancements renouvelle aussi le jeton. Un échec d'écriture
    est dit dans `webUI.msg`.
  - `desktop --web --addr <hors boucle locale>` reste permis (choix explicite de la ligne de commande) mais avertit si
    `allow_lan` n'est pas posé.
  - Serveur HTTP : délais d'en-têtes (10 s) et d'inactivité (2 min), en-têtes limités à 64 Ko, 32 flux d'évènements au plus
    (503 au-delà).

### Instantané — champ `webUI` **[NEW]**

`"webUI": { "state": "off" | "on" | "error", "listen": "127.0.0.1:8770", "lan": false, "warn": "", "msg": "" }` — jamais
le jeton. `lan` : écoute hors de la boucle locale active (confirmée par `allow_lan`) ; `warn` : texte de l'avertissement
qui l'accompagne (bannière) ; `msg` : erreur (écoute impossible, réglage refusé, jeton non conservé).

### `webUIRegen` **[NEW]**

Sans argument ; fenêtre native seulement. Régénère le jeton de l'interface web : l'ancien lien meurt, les sessions et
flux des navigateurs sont révoqués. Résultat : `{ "ok": true, "msg": "…" }` (`msg` : le fichier n'a pas pu être écrit, sinon vide).

### `webUIOpen` **[NEW]**

Sans argument ; fenêtre native seulement (refusé pour un client web). Ouvre l'interface dans le navigateur par
défaut du poste, avec l'adresse d'amorçage (jeton) construite côté Go. Résultat : `{ "ok": true }` ou
`{ "ok": false, "msg": "…" }`.

### Appels d'un client web dans le processus de la fenêtre native

- Chaque appel porte son origine (fenêtre ou web) : `clipboardRead` / `clipboardWrite` répondent `unavailable` à un
  client web (règle du LOT-4 de la v6.1.0, appliquée par appel et non plus par processus) ; `quit`, `setTheme`,
  `webUIOpen` sont refusés à un client web, ainsi que `webUIRegen` et toute modification des réglages
  `desktop_web` **et `browser_server`** (serveur de fichiers : mêmes garde et empreinte, fail-closed) (`configSave` / `configApply` sur `backends.yaml` ou le modèle « Ce poste » : refus
  `{ "invalid": true }` avec la raison) — un navigateur ne rouvre pas l'interface sur le réseau. Mise à jour du
  binaire, autres réglages, fermeture forcée : permis (le binaire installé vient toujours de la release officielle :
  le client ne fournit que le type d'annonce).
- Les évènements sont émis aux deux interfaces. La sélection, les onglets et l'état des projets sont communs.
- Taille des terminaux : `termResize` du dernier client qui a redimensionné ou pris le clavier s'applique ; l'autre
  affiche la géométrie reçue (`layout.cols`, `layout.rows`) sans renvoyer la sienne tant qu'il n'a pas le clavier.

## v6.2.0 — LOT-5 : organisations de la fenêtre d'un projet (#153)

Choix du 2026-11-15 : deux organisations, réglage global avec exception par projet, pas de mise en page libre.
Maquette : `docs/mockup/v6.2.0/ui/project_layout__organisations.html`.

### Valeurs

| Valeur | Libellé | Disposition des panes **visibles** |
|---|---|---|
| `top` (défaut) | Leader en haut | Règle actuelle, inchangée : leader en haut ; teammates dessous, une ligne jusqu'à 5, deux au-delà (`LAYOUT_2ROW_*`, `LAYOUT_3ROW_*`) |
| `left` | Leader à gauche | Leader à gauche sur toute la hauteur (`LAYOUT_LEFT_LEADER_PCT` % de la largeur, défaut 46) ; teammates à droite, une colonne jusqu'à 4, deux colonnes égales à partir de 5 (la première reçoit ⌈n/2⌉ teammates) |

Sans teammate visible : le leader occupe toute la fenêtre, dans les deux cas. Les panes du haut de `Classify`
(leader, puis agents `cdp`) forment la zone du leader : en `left`, ils s'empilent dans la colonne de gauche.
L'ordre des teammates (`Classify`) est le même pour les deux valeurs ; en `left`, il se lit de haut en bas, colonne
après colonne.

### Clés de `config.conf` **[NEW]**

- `LAYOUT_MODE` : `top` | `left` (défaut `top`). Valeur inconnue : `top`, avec un problème de validation.
- `LAYOUT_LEFT_LEADER_PCT` : entier 20 à 80 (défaut 46).
- `PROJECT_LAYOUTS` : tableau associatif `["<nom du projet>"]="top|left"`, mêmes formes d'écriture et mêmes règles
  que `PROJECT_COLORS` (seule cette clé est écrite, commentaires conservés, forme non reconnue refusée).
- Lues par bash (`loadConfigScript`) et par `confLite` ; présentes dans `defaultConfig` (en commentaire pour
  `PROJECT_LAYOUTS`) et dans le modèle `kind: "config.conf"` (`LAYOUT_MODE` : `enum`, groupe de la mise en page).
- Organisation effective d'un projet : `PROJECT_LAYOUTS[nom]` si présent et valide, sinon `LAYOUT_MODE`. Une seule
  fonction de `core`, appelée par la coque Linux et par l'interface native.
- Prise en compte : au tour suivant du surveillant de mise en page de chaque projet ouvert (relecture de la
  configuration quand le fichier change), sans relance ; `relayout` (Ctrl+B R) l'applique tout de suite.

### `projectSettings` — champs **[NEW]**

`"layout": "left"`, `"layoutAuto": false`, `"layouts": [ { "id": "top", "label": "Leader en haut" },
{ "id": "left", "label": "Leader à gauche" } ]` — `layout` : organisation effective ; `layoutAuto` vrai tant
qu'aucune entrée n'existe dans `PROJECT_LAYOUTS` (le projet suit le réglage global).

### `projectApply` — argument `layout` **[NEW]**

`"layout": "top" | "left"` : écrit l'entrée de `PROJECT_LAYOUTS` ; `"layout": ""` : retire l'entrée (« Comme le
réglage global ») ; absent : inchangé. Permis sur un projet ouvert (appliqué au tour suivant). Écrit dans la même
passe que `color` (un seul enregistrement de `config.conf`) ; `colorSaved` devient vrai dès que `config.conf` a été
écrite, pour la couleur ou pour l'organisation. Valeur inconnue : `{ "ok": false, "msg": "…" }` avant toute écriture.

### Plusieurs launchers

L'organisation est appliquée par le launcher qui a la main, avec **sa** configuration ; un launcher en lecture seule
affiche la disposition reçue. Aucune option tmux nouvelle.

## v6.3.0 — LOT-5 : commutateur du masquage des teammates inactifs (#188)

### Réglage `TEAM_HIDE_IDLE_ENABLED` de `config.conf` **[NEW]**

```json
{ "key": "TEAM_HIDE_IDLE_ENABLED", "group": "teams", "label": "Cacher les teammates inactifs",
  "type": "bool", "value": true, "default": true, "set": false, "editable": true }
```

- Fichier : `1` / `0`. Absent : activé (comportement de la v6.2.0).
- `TEAM_HIDE_IDLE` garde le délai, en secondes ; il n'est pas modifié quand le commutateur est éteint.
- Le commutateur ne se trouve que dans les Réglages (page « Launcher », groupe « teams »). La carte TEAM affiche
  l'état, sans action.
- Quand l'utilisateur allume le commutateur alors que le délai vaut `0`, l'interface propose `30`.
- Pris en compte sans relance : `config.conf` est relu à l'enregistrement. Règle appliquée :
  `contracts/tmux-options.md` « Masquage désactivé ».

### `cards` — champ `teamHead.hideIdle` **[NEW]**

```json
"teamHead": { "tpl": "v5.9.4", "claude": "2.1.87", "odd": 2, "hideIdle": { "enabled": true, "delay": 30 } }
```

- `enabled` : valeur effective de `TEAM_HIDE_IDLE_ENABLED`. `delay` : `TEAM_HIDE_IDLE`, en secondes, conservé même
  quand `enabled` est faux.
- Masquage effectif : `enabled` vrai **et** `delay > 0`.
- Calcul dans `core` (même résultat pour la carte de la coque Linux).

### `cards.team[]` quand le masquage est désactivé **[CHANGED]**

`hidden` vaut toujours faux et `idleLeft` vaut `0`. `pinned` garde son sens.

### `teamToggle` **[CHANGED]**

Nouveau cas de refus : la bascule qui cacherait un teammate alors que le masquage est désactivé rend
`{ "ok": false, "msg": "masquage désactivé : l'activer dans les Réglages" }`. Les autres cas sont inchangés.

## v6.5.0 — masquage des teammates inactifs par projet (#209)

### Clé `PROJECT_HIDE_IDLE` de `config.conf` **[NEW]**

- Tableau associatif `["<nom du projet>"]="auto|none"`, mêmes formes d'écriture et mêmes règles que `PROJECT_LAYOUTS`
  (seule cette clé est écrite, commentaires conservés, forme non reconnue refusée). Absente pour un projet = `default`.
- Lue par bash (`loadConfigScript`) et par `confLite` ; en commentaire dans `defaultConfig`.
- Masquage effectif d'un projet (une seule fonction de `core`, coque Linux et interface native) :
  `auto` → activé ; `none` → désactivé ; absent (`default`) → `TEAM_HIDE_IDLE_ENABLED` (#188). Le délai est toujours
  `TEAM_HIDE_IDLE`. **Délai `0`** : le masquage **automatique** est désactivé dans tous les cas (`teamHead.hideIdle.enabled` vaut faux), mais le masquage **manuel** reste permis — un projet `auto` ou `default` activé avec un délai `0` n'est jamais masqué tout seul, et `teamToggle` n'est pas refusé (comme #188) ; seul le mode `none` (ou `default` avec le global désactivé) refuse la bascule.
- Changer `TEAM_HIDE_IDLE_ENABLED` ne modifie pas `PROJECT_HIDE_IDLE` : seuls les projets en `default` suivent.
- Prise en compte sans relance, au tour suivant du surveillant du projet.

### `cards` — champ `teamHead.hideIdle.mode` **[NEW]**

```json
"hideIdle": { "enabled": false, "delay": 30, "mode": "none", "global": true }
```

- `mode` : `auto` | `default` | `none` (choix du projet). `enabled` devient la valeur **effective pour le projet**
  (**[CHANGED]**, rétrocompatible : identique à v6.3.0 pour un projet en `default`). `global` : valeur de
  `TEAM_HIDE_IDLE_ENABLED` (infobulle).

### `projectApply` — argument `hideIdle` **[NEW]**

`"hideIdle": "auto" | "none"` : écrit l'entrée de `PROJECT_HIDE_IDLE` ; `"hideIdle": "default"` : retire l'entrée ;
absent : inchangé. Permis sur un projet ouvert. Même passe d'écriture que `color` / `layout`. Valeur inconnue :
`{ "ok": false, "msg": "…" }` avant toute écriture. `projectSettings` gagne `"hideIdle": "auto|default|none"`.

### `teamToggle` **[CHANGED]**

Refus quand le masquage effectif du projet est désactivé : message
`masquage désactivé pour ce projet : choisir « auto hide » dans le titre de la carte TEAM`.

## v6.6.0 — Gestion des projets (#214 à #219)

> Mêmes méthodes pour la fenêtre native (Wails) et l'interface web (`POST /api/call`). La coque tmux appelle les
> mêmes fonctions de `core` (pas de Dispatch). **Toute** méthode qui reçoit une base, une catégorie ou un nom les
> repasse par `core.ResolveProjectPath` (ci-dessous) : aucun chemin produit hors des bases configurées, même par un
> appel forgé.

### Règle commune — `core.ResolveProjectPath(base, category, name)` **[NEW]**

- `base` : doit être **égale** (après `filepath.Clean`) à l'une des racines `LoadConfig().Roots` ; sinon
  `base inconnue`. Une racine absente du disque (montage perdu) : `base inaccessible`.
- `category` : chemin **relatif** à la base, segments séparés par `/` (catégories imbriquées admises, profondeur
  `maxScanDepth`), vide = directement sous la base. Chaque segment : non vide après `TrimSpace`, ni `.` ni `..`, ni
  `\`, ni caractère de contrôle, ne commence pas par `.`. Chemin absolu refusé.
- `name` : `ValidProjectName` (inchangé) + pas de `..`.
- **Portabilité** (les bases en partage 9P / SMB sont lues par Windows) : sont refusés, sur toutes les plateformes, les
  caractères `: * ? " < > |`, un point ou un espace final, et les noms réservés de Windows — `CON`, `PRN`, `AUX`, `NUL`,
  `CONIN$`, `CONOUT$`, `COM0`–`COM9`, `LPT0`–`LPT9` et leurs variantes en exposant (`COM¹ ²  ³`, `LPT¹ ²  ³`), avec ou sans
  extension, insensible à la casse. Cette règle ne vaut que pour les segments **créés par l'appel** (le nom du
  projet, une nouvelle catégorie) : une catégorie **existante** aux caractères non portables reste listée
  (`projectCategories`) et utilisable si elle passe les contrôles de sécurité (traversée, liens, dossier caché).
- Résultat `<base>/<category>/<name>` ; contrôle final après `filepath.EvalSymlinks` du **parent existant le plus
  profond** : il doit rester sous `EvalSymlinks(base)` (un lien symbolique de catégorie qui sort de la base est
  refusé : `chemin hors des bases`).
- Erreur → `{ "ok": false, "field": "base"|"category"|"name", "msg": "<raison affichée sous le champ>" }`, jamais
  d'écriture disque.

### `projectBases` **[NEW]**

Arguments : `{}`. Résultat :
```json
{ "bases": [ { "path": "/share/MFS/ROOT/CLAUDE_PROJETS/PROJETS", "missing": false } ], "default": "/share/MFS/ROOT/CLAUDE_PROJETS/PROJETS" }
```
`bases` = racines configurées, dans l'ordre de `config.conf`. `default` = la seule base s'il n'y en a qu'une
accessible, sinon la base du projet sélectionné dans l'arbre, sinon `""`.

### `projectCategories` **[NEW]**

Arguments : `{ "base": "<base>" }`. Résultat :
```json
{ "categories": [ { "path": "INFRA", "projects": 4 }, { "path": "PRO/CLIENT_A", "projects": 0 } ] }
```
Sous-dossiers réels de la base **qui ne sont pas des projets** (`isProjectDir` faux), récursivement jusqu'à
`maxScanDepth`, dossiers cachés (`.`) exclus, triés. Une catégorie vide (0 projet) est listée. Base invalide :
erreur de la règle commune.

### `categoryCreate` **[NEW]**

Arguments : `{ "base", "category" }`. Effet : `MkdirAll(<base>/<category>, 0755)` après la règle commune.
Résultat : `{ "ok": true, "path": "INFRA", "existed": false }` — une catégorie existante (même nom, casse comprise)
n'est pas une erreur : `existed: true` (fusion). Un fichier de ce nom : `{ "ok": false, "field": "category", … }`.
L'interface ne l'appelle **qu'à la validation** du dialogue (pas à la frappe) : `create` accepte aussi une catégorie
nouvelle et la crée lui-même (champ `newCategory`).

### `newInfo` **[CHANGED]**

Ajoute : `"bases"` et `"default"` (comme `projectBases`), `"auth": { "token": true, "sshKey": { "<backend>": true } }`
(présence seulement — jamais la valeur du jeton ni la clé), `"template": "v5.11.1"` (tag du template adopté, vide
si inconnu). `roots` reste (alias de `bases[].path`, retiré en v7).

### `create` **[CHANGED]**

Arguments :
```json
{ "base": "<base>", "category": "INFRA", "newCategory": false, "name": "supervision-k8s",
  "source": "empty" | "git" | "template",
  "remote": "git@github.com:moi/projet.git",
  "url": "https://github.com/CCoupel/supervision-k8s.git",
  "image": "…", "backend": "…" }
```
- `base` + `category` + `name` passent par la règle commune ; `base` **n'est plus** un dossier quelconque (avant :
  catégorie ou racine passée telle quelle) — un ancien appel `{base: "<racine>/INFRA"}` est refusé (`base inconnue`) :
  l'interface et le backend sont livrés ensemble.
- `source` (défaut `empty`) :
  - `empty` — « Projet brut » : dossier, `git init`, `remote` `origin` s'il est fourni ; **ni** marqueur
    `init-proposed`, **ni** `/init-project`, **ni** CLAUDE.md minimal. **[CHANGED]** l'interface n'émet plus `raw` (un
    `raw` reçu est ignoré).
  - `template` : `git init` + `remote` facultatif ; marqueur `init-proposed` ; à la **première ouverture**,
    `/init-project` est **proposé** (évènement `initProposal`), jamais lancé d'office.
  - `git` : `url` requise (`https://…` ou `git@…:…` / `ssh://…` ; `file://`, chemin local et options `-…` refusés) ;
    `git clone --` dans le dossier final. Authentification : HTTPS → jeton `GITHUB_TOKEN` de `config.conf` passé par
    `GIT_ASKPASS` (jamais dans l'URL, ni dans `.git/config`, ni dans un journal) ; ssh → clé et `known_hosts` du
    coffre pour `backend` (ou `host`), `GIT_SSH_COMMAND` avec `IdentitiesOnly=yes`, `StrictHostKeyChecking=yes`.
    Exécuté par le processus qui sert l'interface (hub, exe, pod). `remote` ignoré (l'origine est `url`).
- **Asynchrone** pour `git` : résultat immédiat `{ "job": "<id>" }` ; avancement par l'instantané (`newJob`,
  ci-dessous) ; à la fin : projet ouvert comme pour les autres sources. `empty`/`template` : résultat `{ "tab" }`
  comme avant.
- **Échec** (toute source, toute étape) : suppression du dossier du projet **créé par cet appel**, et de la
  catégorie si `newCategory` l'a créée et qu'elle est vide ; rien n'est écrit dans `projects.json`. Le message
  dit ce qui a été nettoyé.

### Instantané — champ `newJob` **[NEW]**

```json
"newJob": { "id": "…", "name": "supervision-k8s", "step": "réception", "pct": 34, "state": "running"|"done"|"failed",
            "msg": "authentification refusée (HTTPS, jeton) — dossier supprimé" }
```
Absent sans création en cours ; une création terminée (`done`/`failed`) reste dans l'instantané 30 s (ou jusqu'à
`createCancel` sur son id, qui la retire). Une création à la fois par launcher (deuxième `create` git : refus
`création en cours`, la place est réservée avant toute écriture).

**`step`** — libellés EXACTS émis (texte affichable, minuscules, accentués) et `pct` global (0–100) :

| `step` | `pct` | quand |
|---|---|---|
| `préparation` | 0 | démarrage, comptage / énumération / compression côté serveur |
| `réception` | 0 → 80 | `Receiving objects` (téléchargement) |
| `résolution des deltas` | 80 → 95 | `Resolving deltas` |
| `extraction` | 95 → 100 | `Updating files` / `Checking out files` |
| `terminé` | 100 | `state: "done"` (projet ouvert) |

`state: "failed"` conserve le dernier `step` et porte `msg` (raison courte, jamais le jeton, + ce qui a été nettoyé).
Un clonage réussi dont l'**ouverture** du projet échoue est `failed` (`msg` : « cloné, mais ouverture impossible : … »),
le dépôt cloné restant en place. En cas d'échec ou d'annulation, **aucune** entrée n'est laissée dans `projects.json`
(l'affectation du backend n'est écrite qu'après un clonage réussi).

### `createCancel` **[NEW]**

Arguments : `{ "job": "<id>" }`. Arrête le clonage (processus tué) puis nettoie comme un échec.

### Évènement `initProposal` **[NEW]** et `initProposalAnswer` **[NEW]**

À la première ouverture d'un projet créé en `template` : `{ "path" }`. Réponse `initProposalAnswer
{ "path", "run": true|false }` : `true` tape `/init-project` dans le leader (refusé sans retirer le marqueur tant que le
leader n'est pas prêt) ; `false` (« Plus tard ») ne le tape pas. Le marqueur `.claude-launcher/init-proposed` (exclu de
git par `.git/info/exclude`, jamais commité) reste tant que `/init-project` n'a pas été lancé : « Plus tard » ⇒
reproposé au lancement suivant du launcher (une fois par processus). Seul un projet connu portant le marqueur est
accepté. Coque tmux : même marqueur, même règle (question `[o/N]` à l'ouverture, retiré sur `o`).

### `projectMoveInfo` **[NEW]** (complété 2026-11-21)

Arguments : `{ "path" }`, et facultativement la destination envisagée `{ "base", "category", "newCategory", "name" }`
(appelée à la frappe par le dialogue, maquette B « ce qui change », sans rien écrire). Résultat :
```json
{ "base": "…", "category": "INFRA", "name": "supervision-k8s", "key": "infra-supervision-k8s",
  "open": false, "blockers": [ "projet ouvert dans ce launcher" ],
  "dest": { "ok": true, "path": "/srv/projets/INFRA/supervision-k8s", "key": "infra-supervision-k8s",
            "keyChanged": false, "crossDevice": true, "exists": false, "field": "", "msg": "",
            "impacts": [ { "what": "dir", "from": "…", "to": "…" }, { "what": "ctr", "from": "infra-supervision-k8s", "to": "" },
                         { "what": "assign", "value": "srv1-docker" }, { "what": "conf", "keys": ["PROJECT_COLORS"] },
                         { "what": "history", "from": "…", "to": "…" },
                         { "what": "trust", "label": "Approbation de l'environnement : conservée" } ] },
  "pending": { "id": "m-20261121-0943-1", "from": "…/INFRA/supervision-k8s", "to": "…/MONITORING/supervision",
               "fromKey": "infra-supervision-k8s", "toKey": "monitoring-supervision",
               "step": "trust", "done": ["dir", "assign", "conf", "history"], "msg": "…", "at": "2026-11-21T09:43:00Z" } }
```
- `key` : clé actuelle (`CtrName`, nom tmux / pod / `/files/<clé>`). `dest` : absent sans destination ; `dest.key` =
  clé **recalculée** pour la destination (`CtrName` du nouveau chemin), `keyChanged` si elle diffère ; `crossDevice` :
  la destination est sur un autre système de fichiers (copie puis suppression) ; `exists` : la destination existe
  déjà (`ok: false`) ; erreur de la règle commune → `ok: false`, `field`, `msg`. `impacts` : lignes du tableau « ce
  qui change » (texte `label` affiché tel quel pour `trust`, présent seulement si le projet a une entrée dans `trust.json` ; seules les étapes qui changent quelque chose : `conf` absent si la clé ne change pas et qu'aucune
  entrée n'existe, `ctr` absent sans conteneur ni StatefulSet).
- `blockers` : projet ouvert ici (fenêtre tmux, onglet), ouvert par un autre launcher (présence #176), conteneur ou
  pod **en cours d'exécution**, déplacement inachevé (`pending`). Vide = déplaçable.
- `pending` : entrée de `moves.json` dont `from` **ou** `to` est `path` (le projet peut déjà être sous sa
  destination si l'étape `dir` est faite) ; absent sinon. `done` : étapes terminées et non annulées, dans l'ordre ; `step` : la première étape non faite — celle d'où « Reprendre » rejoue.

### `projectMove` **[NEW]**

Arguments : `{ "path", "base", "category", "newCategory": false, "name" }`. Reprise et annulation d'un déplacement
inachevé : `projectMoveResume` / `projectMoveAbort` (ci-dessous).
- Refus si `blockers` non vide (`{ "ok": false, "msg" }`), destination identique, destination existante, ou règle
  commune KO. Pas de fermeture implicite : l'interface propose « Fermer le projet » (`close` existant) avant.
- Étapes, dans l'ordre, consignées dans le journal `moves.json` (dossier de config, sous verrou `WithFileLock`) :
  1. `dir` : `os.Rename` ; si `EXDEV` (autre système de fichiers) → copie complète vers `<dest>.moving-<id>`,
     contrôle (nombre et taille des fichiers), rename en `<dest>`, puis suppression de la source ;
  2. `assign` : clé de `projects.json` (chemin) reportée ;
  3. `conf` : entrées de `config.conf` du projet (`PROJECT_COLORS`, organisation, `PROJECT_HIDE_IDLE`) reportées
     si la clé change (une seule écriture, `SaveConfig`) ;
  4. `history` : dossier `projects/<chemin encodé>` de la config de claude (`ProjectDirName`) renommé ;
  5. `trust` : la clé `RemotePath` du projet dans `trust.json` suit (état et empreinte conservés — un projet cloné
     approuvé ne redemande rien ; sous verrou, `.bak` avant écriture ; registre illisible : l'étape échoue) ;
  6. `ctr` : ancien conteneur / StatefulSet + pod `<ancienne clé>` (arrêtés) supprimés — recréés au nouveau nom à
     la prochaine ouverture.
  Nom tmux, URL `/files/<clé>/…` et `CL_BROWSER_PATH_MAP` dérivent du chemin : rien à écrire.
- Échec à l'étape N : étapes N-1…1 annulées dans l'ordre inverse (l'étape `ctr`, non réversible, est la dernière). Étapes : `dir`, `assign`, `conf`, `history`, `trust`, `ctr`.
  Résultat `{ "ok": false, "step": "ctr", "msg", "rolledBack": true }`. Retour arrière lui-même en échec :
  `{ "ok": false, "rolledBack": false, "state": "partial", "done": ["dir","assign"], "msg" }` ; l'entrée du journal
  reste, le projet porte le blocker « déplacement inachevé » et l'action propose « Reprendre » (rejoue depuis
  l'étape en échec) ou « Annuler » (retour arrière).
- Succès : `{ "ok": true, "path": "<nouveau chemin>", "key": "<nouvelle clé>" }`, arbre relu, sélection suivie.

### `projectMoveResume` **[NEW]** et `projectMoveAbort` **[NEW]** (2026-11-21)

Arguments : `{ "id" }` (`pending.id` de `projectMoveInfo`). Sous le verrou du journal ; l'entrée doit exister (sinon
`{ "ok": false, "msg": "aucun déplacement inachevé" }`) et le projet ne doit avoir **aucun autre** blocker (ouvert,
conteneur actif).
- `projectMoveResume` (« Reprendre ») : rejoue les étapes restantes à partir de `pending.step` (les étapes de
  `done` ne sont pas refaites ; chaque étape est idempotente : `dir` déjà à destination, entrée déjà reportée, ancien
  conteneur déjà absent = succès). Résultat comme `projectMove` : succès `{ "ok": true, "path", "key" }` (entrée
  retirée du journal) ; nouvel échec → retour arrière comme `projectMove` (`rolledBack: true`, entrée retirée) ou
  `state: "partial"` (entrée mise à jour : `step`, `done`, `msg`).
- `projectMoveAbort` (« Annuler ») : annule les étapes de `done` dans l'ordre inverse (l'étape `ctr`, si faite, ne
  s'annule pas : l'ancien conteneur est recréé à la prochaine ouverture). Succès `{ "ok": true, "path": "<chemin
  d'origine>", "key": "<clé d'origine>", "rolledBack": true }` (entrée retirée) ; échec `{ "ok": false, "state":
  "partial", "done", "msg" }` (entrée mise à jour).
- Les deux relisent l'arbre et suivent la sélection. Aucun traitement automatique au démarrage : l'entrée reste en
  `pending` jusqu'au choix de l'utilisateur (blocker « déplacement inachevé » affiché sur le projet).
- Coque tmux : même choix depuis F2 → « déplacer / renommer » sur un projet en `pending` (touches `r` reprendre,
  `a` annuler).

### `launcherInfo` **[NEW]** (#219)

Arguments : `{}`. Résultat :
```json
{ "running": "v6.5.0", "installed": "v6.6.0", "binChanged": true, "mode": "shell" | "desktop" | "web" | "pod" }
```
- `binChanged` : le fichier de `SelfExe()` n'est plus celui du démarrage (inode/date/taille, ou `(deleted)`).
- `installed` : version du binaire sur disque (`<exe> version`, délai 3 s ; vide si illisible).
- Instantané : `launcher` reprend `running`, `installed`, `binChanged` pour le bandeau (une seule branche : « vX
  installée — Relancer »). **[révision GATE 2]** pas de champ `stale` ni de branche « processus anciens » dans
  l'interface web / native : les processus `mirror` / `layout-watch` n'existent que dans la coque, qui a sa propre
  annonce (ci-dessous). Pas de champ `backup` (aucun retour arrière dans l'interface).

### Processus anciens de la coque — `core.FindStaleProcs` (#219)

Coque Linux seulement (hors Linux : liste vide, pas de parcours de `/proc`). Retient un processus de `/proc` si :
- `argv[0]` a pour **nom de fichier le nom du binaire courant** (`claude-launcher-ng2`) et `argv[1]` ∈ {`mirror`,
  `layout-watch`} — un processus d'un autre programme portant ces arguments n'est jamais retenu ;
- son `CL_INNER_SOCKET` (environnement, défaut `claude-ng2`) est celui de ce launcher (pas d'action sur le serveur
  tmux d'un autre launcher ou d'un test) ;
- son `/proc/<pid>/exe` n'est **pas** le binaire courant (`os.SameFile`).

### Coque — touche `ctrl+r` contextuelle **[CHANGED]** (#219)

- Annonce affichée (binaire changé, ou processus anciens restants) : `ctrl+r` demande la confirmation (« Relancer ?
  … sessions gardées (o/y) ») puis relance comme `relaunch` en mode `shell`. Un clic sur l'annonce fait de même.
- Sans annonce : `ctrl+r` garde son rôle actuel (rescan).

### `relaunch` **[NEW]** (#219)

Arguments : `{ "confirm": true }` (sans `confirm` : refus — la confirmation est faite par l'interface).
Effet selon `mode` :
- `desktop` / `web` : `restartApp` (binaire installé relancé, `CL_NO_UPDATE=1`, port libéré, ancien processus
  quitté).
- `shell` : panes de la coque (`@cl` nav, cartes, état) relancés par `respawn-pane -k` avec le binaire installé ;
  vignettes `mirror` relancées par `respawn-pane -k` ; `layout-watch` anciens (`FindStaleProcs`) arrêtés (`SIGTERM`,
  puis `SIGKILL` après 2 s) et relancés par `run-shell -b` pour leur fenêtre. **Jamais** de `kill-session` ni de
  `kill-server` de `claude-hub` : sessions et panes des projets intacts.
- `pod` : sortie du processus (code 0) — le conteneur du launcher est redémarré par Kubernetes ; les pods des
  projets ne sont pas touchés.
Résultat : `{ "ok": true }` avant la relance (l'interface se reconnecte), ou `{ "ok": false, "msg" }`.

### ~~`rollback`~~ **[RETIRÉ]** (#218, révision GATE 2)

Aucun retour arrière dans l'interface (ni Dispatch, ni bandeau, ni touche de coque). Le retour à la version
précédente est une **procédure manuelle documentée** (guide `ng2/docs/`, section mise à jour) : quitter le
launcher, renommer `<exe>.bak-v<précédente>` en `<exe>`, relancer ; l'exe sauvegardé (v6.6.0) peut être gardé à côté.

### Sauvegarde avant mise à jour (#218) **[CHANGED]**

`InstallBinary` / `ReplaceExecutable` : avant remplacement, le binaire courant devient
`<exe>.bak-v<version courante>` (Windows : renommage de l'exe en cours d'exécution ; Linux : copie atomique, le
renommage par-dessus restant le mécanisme de remplacement). **Une seule** sauvegarde gardée : les autres
`<exe>.bak-v*` sont supprimées après une installation réussie (une sauvegarde verrouillée est retentée au démarrage).
Même convention que la sauvegarde du hub par `deployer` (`claude-launcher-ng2.bak-v<précédente>`). Échec de
l'écriture du nouveau binaire : l'exe d'origine est remis en place (comportement `swapRunningExe` conservé). Pas de
version « écartée » (`update-skip` retiré avec le retour arrière).

### Écriture de `config.conf` (#217) **[CHANGED]**

`SaveConfig` (et donc `ApplyProjectConf*`, `configApply`, `projectApply`), `EnsureConfig` : temporaire
`.config.conf.tmp-*` du même dossier, `0600`, `fsync`, `rename`, puis `fsync` du dossier (Unix). Windows : rename
retenté (5 essais, attentes 50/100/200/400 ms) sur `ERROR_ACCESS_DENIED` / `ERROR_SHARING_VIOLATION`, puis erreur explicite
(`config.conf verrouillé par un autre programme`) ; l'ancien fichier reste intact. La copie `.bak` est écrite de
la même façon. `EnsureConfig` : création exclusive par temporaire + lien/rename sans écrasement. Au démarrage, les
`.config.conf.tmp-*` de plus de 10 min du dossier de config sont supprimés. Aucun changement d'API.

### Approbation de l'environnement d'un projet cloné (#215, amendement audit LOT-2.2) **[NEW]**

Un dépôt cloné n'est pas de confiance : son `.claude-launcher/env.yaml` et son `env-template.yaml` (image, paquets,
`setup`, `on_start`, `watch`, `mounts`, `local`, `prefer`) ne sont **jamais appliqués sans accord**.

**Registre `trust.json`** (dossier de config **partagé** `<CL_ROOT_BASE>/.config/claude-launcher-ng2/`, à côté de
`projects.json`, comme le coffre des secrets #201 : l'exe Windows, le hub et le pod partagent les approbations ;
`CL_CONFIG_DIR` l'emporte, sans racine de base : dossier de config du launcher ; **jamais dans le projet**, qu'un
dépôt pourrait fournir) — clés = chemin du projet vu des backends (`RemotePath`, comme `projects.json`), écriture
atomique sous `WithFileLock`, **copie `trust.json.bak` avant chaque écriture** :
```json
{ "/share/…/INFRA/supervision-k8s": { "state": "pending" | "accepted" | "ignored" | "none", "sha": "<sha256>",
  "source": "https://github.com/CCoupel/supervision-k8s.git", "image": "debian:12", "at": "2026-11-20T10:00:00Z" } }
```
- `sha` : SHA-256 du contenu de `env-template.yaml` puis `env.yaml` (absent = vide). **Comparé à chaque ouverture.**
- `image` : socle choisi à la création (gardé par « Ouvrir sans »).
- **TOUT projet cloné est enregistré** : l'entrée `pending` est posée **avant** le clonage (un clone interrompu ne devient
  jamais de confiance ; retirée par l'annulation de la création si le clonage échoue), puis, à la fin d'un clonage
  réussi, elle reste `pending` si le dépôt porte `env.yaml` / `env-template.yaml`, ou devient **`none`** (aucun
  environnement ; `sha` des fichiers absents). Pas d'entrée = projet de confiance (projets existants, `empty`, `template`).
- **Re-demande sur changement d'empreinte (décision utilisateur, remplace « choix définitif »)** : état effectif =
  `pending` dès que `sha` ≠ empreinte actuelle des fichiers, pour `accepted`, `ignored` **et `none`** (un `env.yaml`
  qui apparaît après un `git pull` est redemandé). **Aucune différence n'est affichée** : la question présente
  l'environnement courant (`changed: true` dit seulement « a changé depuis l'approbation »). Les écritures de
  `env.yaml` **faites par le launcher lui-même** (F2, réglages du projet) mettent `sha` à jour — sauf si les fichiers
  avaient déjà été modifiés par le dépôt avant l'écriture (jamais béni). « Revoir » remet l'entrée à `pending`.
- **Migration** : au démarrage (`shell`, `desktop`), un `trust.json` resté dans le dossier de config LOCAL du poste (et
  absent du registre partagé) est **fusionné** dans le registre partagé : jamais de perte — une entrée absente est
  ajoutée ; en cas de désaccord, l'état le plus **RESTRICTIF** l'emporte (`pending` > `ignored` > `accepted` = `none` ;
  même sévérité mais empreintes différentes, ou `accepted` face à `none` → `pending`) — le registre partagé peut donc
  être resserré par un registre local plus strict, jamais assoupli. Écart journalisé, `trust.json.bak` avant l'écriture,
  l'ancien fichier est conservé. Registre partagé illisible : rien n'est réécrit (échec fermé ci-dessous).
- **Échec fermé** : `trust.json` présent mais vide ou illisible → tout projet portant un environnement est `pending`
  (`ErrTrustBroken`, qui est aussi `ErrEnvNotApproved`), le fichier n'est **jamais réécrit** (toute écriture est
  refusée) ; restaurer `trust.json.bak` ou supprimer le fichier. Un projet sans environnement n'est pas concerné.
  **Message de reprise** (fenêtre native, web et coque — erreur de `LoadEnv`, réponse de `envTrustAnswer`, champ
  `broken` d'`envTrustInfo` / de l'évènement `envTrust`, ligne rouge de l'écran de la coque) : « registre d'approbation
  illisible (<chemin de trust.json>) : restaurez trust.json.bak ou supprimez trust.json pour repartir de zéro ».
  Supprimer `trust.json` repart de zéro : les projets déjà clonés n'ont plus d'entrée (de confiance) ; restaurer
  `trust.json.bak` rend l'état précédent la dernière écriture.

**`core.EnvTrust(path)`** → état effectif : `trusted` (pas d'entrée) | `pending` | `accepted` | `ignored` | `none`.
**`LoadEnv`** : `pending` → refus (`ErrEnvNotApproved` ; `ErrEnvChanged` quand l'empreinte a changé) pour toute
préparation de conteneur ; `ignored` (empreinte à jour) → `EnvSpec{Image: entrée.image}` seul (rien d'autre du dépôt).
Projet affecté à l'**hôte** : aucune question (env.yaml inutilisé) ; elle vient au premier passage en conteneur (F2) ou
à l'ouverture en conteneur.

### `open` **[CHANGED]** — garde d'approbation

Projet en conteneur et `EnvTrust` = `pending` : rien n'est démarré, résultat
`{ "needTrust": true, "path" }` et évènement `envTrust` (ci-dessous). Les autres cas : inchangés.

### Évènement `envTrust` **[NEW]** et `envTrustInfo` **[NEW]**

`envTrustInfo { "path" }` (et charge utile de l'évènement) :
```json
{ "path": "…", "state": "pending", "source": "https://github.com/…", "sha": "<sha courant>",
  "chosenImage": "debian:12",
  "spec": { "image": "…", "packages": [], "packagesByManager": {}, "setup": [], "onStart": [], "watch": [],
            "mounts": [ { "path": "/var/run/docker.sock", "ro": false } ], "local": [], "prefer": "" },
  "files": { "env.yaml": "<texte>", "env-template.yaml": "" },
  "changed": false, "broken": "",
  "risk": { "exec": 3, "hostMounts": 1, "packages": 4, "watch": 1, "local": 1, "scripts": ["./scripts/install.sh"] },
  "scriptNote": "ce script n'est pas couvert par l'approbation : sa version courante sera exécutée" }
```
`risk.exec` = nombre de commandes `setup` + `on_start`, `risk.hostMounts` = montages hors du projet, `risk.packages` =
paquets installés (`packages` + `packages_by_manager`), `risk.watch` / `risk.local` = entrées `watch` / `local`. `risk.scripts` = scripts **du dépôt** que `setup` / `on_start`
appellent (jetons de chemin relatif — `./x`, `../x`, `dir/x` — ou d'extension de script `.sh` `.py` `.ps1`…, hors URL,
option, chemin absolu ; heuristique qui signale) ; `scriptNote` (non vide s'ils existent) : « ce script n'est pas couvert
par l'approbation : sa version courante sera exécutée » — seuls `env.yaml` / `env-template.yaml` sont couverts par
l'empreinte, jamais les scripts qu'ils appellent.
`broken` : message de reprise quand le registre est illisible (vide sinon). `changed` : la question revient parce que les fichiers ont changé depuis l'approbation (message « l'environnement du
dépôt a changé depuis l'approbation », sans différences). Textes affichés tels quels, échappés (jamais interprétés en
HTML ; en terminal : ESC, C0, C1, DEL **et caractères invisibles ou bidirectionnels** U+200B–200F, U+202A–202E,
U+2066–2069, U+061C, U+FEFF remplacés par `·`, texte retourné à la ligne, jamais tronqué : une commande exécutée se lit
en entier).

### `envTrustAnswer` **[NEW]**

Arguments : `{ "path", "choice": "accept" | "ignore" | "cancel", "sha": "<sha de envTrustInfo>" }`.
- `sha` différent du contenu actuel (fichier modifié pendant la question) : `{ "ok": false, "msg": "l'environnement a
  encore changé", "retry": true }` — l'interface recharge `envTrustInfo`.
- `accept` → `state: accepted` (empreinte courante), puis ouverture ; `ignore` → `state: ignored`, ouverture avec le seul
  socle `image` ; `cancel` → rien n'est écrit, rien n'est ouvert.
- Résultat : comme `open` (`{ "tab" }`) ou `{ "ok": true }` pour `cancel`.

### `projectSettings` / `projectApply` — `envTrust` **[NEW]**

`projectSettings` gagne `"envTrust": { "state", "source", "at" }` (absent pour un projet de confiance).
`projectApply { "envTrust": "review" }` remet l'entrée à `pending` (« Revoir » : la question revient à la prochaine
ouverture). Permis projet fermé seulement.

### Coque tmux

Même garde à l'ouverture : écran de la question (image, paquets, commandes, montages),
touches `a` accepter, `s` ouvrir sans, `Échap` annuler, `v` fichiers bruts (maquette retenue A,
`docs/mockup/v6.6.0/ui/clone_trust__approbation_env.html`).
