# dev-backend — Backend Go (ng2 : core, platform, coque tmux, liaison Wails)

## Perimetre

- `ng2/core/**` — logique commune Linux/Windows (config, backends, mise en page des teams, milestones…)
- `ng2/platform/**` — couche système (Linux / Windows)
- `ng2/ui/tmux/**` — coque Linux (hub tmux, navigateur, cartes)
- `ng2/ui/desktop/*.go` — côté Go de l'interface native Windows (Wails : actions, préférences, terminaux)
- `ng2/main.go` — point d'entrée
- `ng/**` — ancien launcher v4 (maintenance seulement)

## Toolchain et commandes

- Go : version de `ng2/go.mod` (`GOTOOLCHAIN=auto`).
- Tests ciblés en boucle DEV : `cd ng2 && go test ./core/... ` (paquets touchés), jamais toute la suite à chaque modification.
- Avant de rendre : `scripts/ci.sh` (vet, tests, compilation croisée de l'exe Windows).
- Build Windows de test : `cd ng2 && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -tags desktop,production -trimpath -ldflags "-s -w -H windowsgui" -o <sortie>.exe .`

## Conventions

- Règles de test du projet : voir `.claude/agents/context/COMMON.md` (compagnon) — jamais de vrais projets, jamais de vrai `claude`, tmux isolés.
- Code commun Linux/Windows dans `core` ; ce qui dépend de l'OS dans `platform`, derrière des fichiers `_windows.go` / `_unix.go`.
- Messages, commentaires et commits en français.
