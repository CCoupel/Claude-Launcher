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

Résultat : `{ "ok": true, "note": "…", "env": { "saved": true, "problems": [], "mtime": "…" } }` (`env` seulement s'il
y avait des changements) ; refus : `{ "ok": false, "msg": "…" }` ; `env` invalide ou en conflit :
`{ "ok": false, "env": { "invalid": true, "problems": [...] } }` ou `{ "ok": false, "env": { "conflict": true, "mtime": "…" } }`.

## v6.1.0 — LOT-3.3 : ce poste, launcher, conteneurs (issue fille de #118)

- `configModel` / `configApply` acceptent `config.conf` (réglages groupés ; `GITHUB_TOKEN` n'est **jamais** renvoyé
  en clair : `{ "set": true }` ; il n'est écrit que si l'utilisateur en saisit un nouveau), `env.yaml` du launcher et
  la section `hubs.<machine>` de `backends.yaml` (`hub` : nom de la machine résolue, surcharges par système
  signalées).
- Taille des caractères : préférence par poste `termFontSize` (méthodes existantes `init` / `setPrefs`).
