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

### `backendStates` **[NEW]**

Arguments : aucun. Résultat : `{ "states": { "<backend>": { "ok": true, "text": "joignable" } },
"load": { "<pool>": { "<machine>": 3 } } }` — depuis le cache du diagnostic, sans bloquer l'affichage.

## v6.1.0 — LOT-3.2 : réglages d'un projet (issue fille de #118)

### `projectSettings` **[NEW]**

Arguments : `{ "path" }`. Résultat : `{ "open": false, "backend", "backends": [], "image", "images": [],
"color", "palette": [], "env": { …champs de env.yaml du projet… }, "envPath", "envMtime",
"merged": [ { "text": "  - gh", "from": "template"|"launcher"|"project" } ], "problems": [] }`.

### `projectApply` **[NEW]**

Arguments : `{ "path", "backend"?, "image"?, "color"?, "env"?: { "mtime", "changes": [...] } }`.
Changer `backend` ou `image` d'un projet ouvert est refusé (`{ "ok": false, "msg" }`), comme `execApply`.

## v6.1.0 — LOT-3.3 : ce poste, launcher, conteneurs (issue fille de #118)

- `configModel` / `configApply` acceptent `config.conf` (réglages groupés ; `GITHUB_TOKEN` n'est **jamais** renvoyé
  en clair : `{ "set": true }` ; il n'est écrit que si l'utilisateur en saisit un nouveau), `env.yaml` du launcher et
  la section `hubs.<machine>` de `backends.yaml` (`hub` : nom de la machine résolue, surcharges par système
  signalées).
- Taille des caractères : préférence par poste `termFontSize` (méthodes existantes `init` / `setPrefs`).
