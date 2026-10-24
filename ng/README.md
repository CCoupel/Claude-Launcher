# Claude-Launcher-NG

Nouvelle génération de Claude-Launcher : un binaire Go qui remplace le menu fzf du
script bash. Même modèle (une session tmux `claude-hub`, une fenêtre par projet,
teamleader + teammates), mais une interface réactive : navigateur de projets à
gauche, tmux à droite.

## Installation

```bash
cd ng
go build -o ~/.local/bin/claude-launcher-ng .
# version : go build -ldflags "-X main.ngVersion=v3.0.1" -o ... .
claude-launcher-ng version
```

### Depuis une release

Chaque tag `v*` publie un binaire par plateforme (linux/darwin, amd64/arm64) et un
`SHA256SUMS` :

```bash
os=$(uname -s | tr A-Z a-z); arch=$(uname -m)
case "$arch" in x86_64) arch=amd64 ;; aarch64) arch=arm64 ;; esac
curl -fsSL "https://github.com/CCoupel/Claude-Launcher/releases/latest/download/claude-launcher-ng-$os-$arch" \
  -o ~/.local/bin/claude-launcher-ng && chmod +x ~/.local/bin/claude-launcher-ng
```

Prérequis : `tmux`, `git`, `bash`, `jq` et le script `~/claude-launcher.sh` (le NG réutilise
ses modes `--layout-watch` et `--mirror`). Aucune dépendance fzf.

## Lancement

```bash
claude-launcher-ng            # = claude-launcher-ng shell
~/claude-launcher.sh          # l'ancienne commande redirige vers le NG
~/claude-launcher.sh --legacy # force l'ancien menu fzf (ou CLAUDE_LAUNCHER_LEGACY=1)
```

La configuration reste `~/.config/claude-launcher.conf` (relue par bash : `GITHUB_DIRS`,
`CLAUDE_OPTIONS`, `COLOR_PALETTE`, `USAGE_REFRESH_SEC`…).

## Interface

- **Gauche** : navigateur (40 colonnes par défaut). Projets en arbre, badges git
  (`●` à jour, `○` sans amont, `↓N` en retard) et template (`✓`, `⚠`), consommation Claude.
- **Droite** : tmux, un onglet par projet, plus deux onglets épinglés :
  - `◈ État` : suit la sélection (git, template, panes, fichiers) — lu depuis un cache
    préparé en tâche de fond ;
  - `⌗ Teamleaders` : une vignette interactive par projet ouvert.

| Action | Effet |
|---|---|
| `↑` `↓` `PgUp` `PgDn`, molette, clic sur un projet | sélectionner → onglet `◈ État` |
| `Entrée`, double-clic sur un projet | ouvrir / rejoindre le projet |
| clic hors projet (groupe, zone vide, en-tête) | onglet `⌗ Teamleaders` |
| double-clic sur une catégorie ou une racine | créer un projet dans ce dossier |
| `✦ Nouveau projet` | créer un projet (choix de la racine) |
| frappe | filtrer |
| `Ctrl+←` `Ctrl+→` ou glisser la bordure | redimensionner la zone navigateur |
| `Tab`, `Alt+←/→` | passer du navigateur à tmux |
| `Ctrl+U`, clic sur `✕` | masquer une annonce de mise à jour |
| `Esc` | effacer le filtre ; sinon demande de détacher (`o` pour confirmer) |

## Architecture

- Deux serveurs tmux : le serveur par défaut (session `claude-hub`, inchangée) et une
  « coque » dédiée (socket `claude-ui`) qui découpe le terminal en navigateur + `tmux attach`.
- `cache.go` : état des projets calculé en tâche de fond. `inotify` n'existe pas sur `/mnt/c`
  (9p) : détection par signature `stat` + recalcul périodique ; `inotify` sinon.
- Mises à jour : le NG signale (tags GitHub du launcher et du template), il ne se met pas à jour.

## Tests

`go test ./...`. Variables de test : `CL_ROOTS` (racines factices), `CL_CLAUDE_BIN`
(faux `claude`), `CL_HUB`, `CL_INNER_SOCKET`, `CL_OUTER_SOCKET`, `CL_SCRIPT`,
`CL_FAKE_LATEST_TPL`, `CL_FAKE_LATEST_LAUNCHER`, `XDG_CACHE_HOME`.
`claude-launcher-ng-test` ouvre une répétition dans `claude-hub-test` sans toucher à `claude-hub`.
