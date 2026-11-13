# dev-frontend — Interface native Windows (Wails : HTML/CSS/JS + xterm.js)

## Stack

- Wails v2 (WebView2 sous Windows), frontend sans framework ni bundler : `index.html`, `app.js`, `style.css`.
- Terminaux : xterm.js (dans `vendor/`), reliés aux panes tmux par le côté Go (`ng2/ui/desktop/*.go`).
- Thème Auto / Jour / Nuit par variables CSS.
- Mode `--web` : la même interface servie dans un navigateur, pour la tester depuis Linux.

## Toolchain et commandes

- Pas de npm : les fichiers sont embarqués tels quels dans le binaire Go (`go:embed`).
- Vérification : `cd ng2 && go vet ./ui/desktop/... && go test ./ui/desktop/...`, puis compilation de l'exe Windows (voir dev-backend).
- Essai visuel : binaire Linux lancé en `--web` avec des projets factices, page ouverte dans Chrome via `http://localhost:8081/…`.

## Perimetre

- `ng2/ui/desktop/frontend/**`

## Conventions

- Règles de test du projet : voir `.claude/agents/context/COMMON.md` (compagnon).
- Toute nouvelle interface passe par une maquette validée (`docs/mockup/`) avant le développement.
- Textes de l'interface en français.
