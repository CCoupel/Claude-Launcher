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
  `msg` résume (« pod absent — projet fermé », « GitHub non relu : délai dépassé »). Un instantané (`snapshot`) et
  les cartes à jour suivent.
- Un onglet dont la session ou le conteneur n'existe plus est retiré ; la ligne du projet reprend l'état relu.
- La ligne du projet porte `"refreshing": true` dans l'instantané pendant l'opération.
- `refresh` (F5, global) est inchangé ; le frontend appelle en plus `refreshProject` sur le projet sélectionné.

### `forceClose` **[NEW]** et `forceCloseConfirm` **[NEW]**

`forceClose` — arguments `{ "path" }`. Résultat : `{ "question": "…", "items": ["onglet", "session tmux locale", …] }`
(ce qui sera nettoyé), ou `{ "msg": "…" }` si l'action est refusée ou sans objet.
`forceCloseConfirm` — arguments `{ "path" }`. Résultat : `{ "ok": true, "msg": "…" }` ou `{ "ok": false, "msg": "…" }`.

- Proposée (ligne du projet : `"canForce": true`) quand le projet est affiché ouvert ou en erreur — onglet présent,
  ou pastille `✕`, `▲`, `●` orange — alors que son conteneur, son pod ou sa session n'existe plus ou ne répond pas.
  `refreshProject` recalcule `canForce`.
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
- Sécurité : toutes les règles du mode `--web` s'appliquent sans changement (jeton accepté par la page d'amorçage
  seule, cookie `HttpOnly` `SameSite=Strict`, `/api/call` en JSON et même origine, en-têtes). Écoute hors boucle
  locale : contrôle de `Host` sur l'hôte d'écoute, le nom de la machine et `localhost` (comme `browser_server`),
  avertissement à l'enregistrement et dans `backends`. Le jeton est tiré une fois par poste et conservé
  (`CacheDir()/desktop-web-token`, `0600`) pour qu'un navigateur déjà ouvert reste valable après une relance.

### Instantané — champ `webUI` **[NEW]**

`"webUI": { "state": "off" | "on" | "error", "listen": "127.0.0.1:8770", "msg": "" }` — jamais le jeton.

### `webUIOpen` **[NEW]**

Sans argument ; fenêtre native seulement (refusé pour un client web). Ouvre l'interface dans le navigateur par
défaut du poste, avec l'adresse d'amorçage (jeton) construite côté Go. Résultat : `{ "ok": true }` ou
`{ "ok": false, "msg": "…" }`.

### Appels d'un client web dans le processus de la fenêtre native

- Chaque appel porte son origine (fenêtre ou web) : `clipboardRead` / `clipboardWrite` répondent `unavailable` à un
  client web (règle du LOT-4 de la v6.1.0, appliquée par appel et non plus par processus) ; `quit`, `setTheme`,
  `webUIOpen` sont refusés à un client web.
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

