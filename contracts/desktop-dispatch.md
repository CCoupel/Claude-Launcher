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

