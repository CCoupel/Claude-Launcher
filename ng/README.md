# Claude-Launcher-NG

Nouvelle génération de Claude-Launcher : un binaire Go qui remplace le menu fzf du
script bash. Même modèle (une session tmux `claude-hub`, une fenêtre par projet,
teamleader + teammates), mais une interface réactive : navigateur de projets à
gauche, tmux à droite.

Vue d'ensemble, installation et configuration : [README du repo](../README.md).
Ce document détaille le fonctionnement et le dépannage.

## Installation

### Depuis une release

Chaque tag `v*` publie un binaire par plateforme (linux/darwin, amd64/arm64) et un
`SHA256SUMS` :

```bash
os=$(uname -s | tr A-Z a-z); arch=$(uname -m)
case "$arch" in x86_64) arch=amd64 ;; aarch64) arch=arm64 ;; esac
curl -fsSL "https://github.com/CCoupel/Claude-Launcher/releases/latest/download/claude-launcher-ng-$os-$arch" \
  -o ~/.local/bin/claude-launcher-ng && chmod +x ~/.local/bin/claude-launcher-ng
claude-launcher-ng version
```

### Depuis les sources

```bash
cd ng
go build -o ~/.local/bin/claude-launcher-ng .
# version : go build -ldflags "-X main.ngVersion=v3.0.1" -o ... .
```

Prérequis : `tmux`, `git`, `bash`, `jq`, `xxd` (vignettes), `claude`, et le script
`~/claude-launcher.sh` (le NG réutilise ses modes `--layout-watch` et `--mirror`).
Aucune dépendance fzf.

## Lancement

```bash
claude-launcher-ng            # = claude-launcher-ng shell
~/claude-launcher.sh          # l'ancienne commande redirige vers le NG
~/claude-launcher.sh --legacy # force l'ancien menu fzf (ou CLAUDE_LAUNCHER_LEGACY=1)
claude-launcher-ng version
```

La configuration reste `~/.config/claude-launcher.conf` (relue par bash : `GITHUB_DIRS`,
`CLAUDE_OPTIONS`, `COLOR_PALETTE`, `USAGE_REFRESH_SEC`…).

## Interface

- **Gauche** : navigateur (40 colonnes par défaut). Projets en arbre, deux badges git
  puis le badge template (`✔`, `⊘`, `⚠`), consommation Claude :
  - **local** : `●` au repos sur main · `✎` branche, non commité · `◆` branche, tout
    commité · `!` modifié directement sur main ;
  - **remote** (état du dernier fetch) : `○` jamais poussé · `↑N` non poussé · `⇡` poussé,
    non mergé dans main · `✓` à jour / mergé · `↓N` en retard.

  Le détail affiche aussi la version locale (`git describe --tags --dirty`) et la dernière
  release (dernier tag atteignable depuis `origin/main`).
  Un projet déjà ouvert est marqué `●` vert, son nom en gras sur la couleur de son pane tmux.
  L'en-tête indique les versions : `NG v3.0.0 · sh 3.0.0 · tpl 3.26.0` (NG, script compagnon,
  dernier tag du template).
- **Sous le navigateur** : `◈ État`, qui suit la sélection (git, versions, template, panes,
  fichiers) — lu depuis un cache préparé en tâche de fond. La zone de droite ne bouge pas.
- **Droite** : tmux, un onglet par projet, plus l'onglet épinglé `⌗ Teamleaders` (une
  vignette interactive par projet ouvert).

| Action | Effet |
|---|---|
| `↑` `↓` `PgUp` `PgDn`, molette, clic sur un projet | sélectionner → détail sous le navigateur (un clic sur un projet ouvert bascule sur sa fenêtre) |
| `Entrée`, double-clic sur un projet | ouvrir / rejoindre le projet |
| clic hors projet (catégorie, zone vide, en-tête) | onglet `⌗ Teamleaders` |
| double-clic sur une catégorie ou une racine | créer un projet dans ce dossier |
| `✦ Nouveau projet` | créer un projet à la racine (choix de la racine si plusieurs) |
| frappe | filtrer |
| `Ctrl+←` `Ctrl+→` ou glisser la bordure | redimensionner la zone navigateur (4 colonnes par appui) |
| `Tab`, `Alt+←/→` | passer du navigateur à tmux |
| clic sur `↑ NG … cliquer pour installer` | installer la nouvelle version et redémarrer le navigateur (projets conservés) |
| `Ctrl+U`, clic sur `✕` | masquer une annonce de mise à jour |
| `Esc` | effacer le filtre ; sinon quitter : `l` launcher seul (projets conservés), `t` puis `o` tout fermer |

Création d'un projet : le formulaire demande le nom, puis le type de projet :

- **géré** (`g`, par défaut) : remote git facultatif ; `git init`, `origin` ajouté si un remote
  est donné, `init-project.md` déposé, et claude démarre directement sur `/init-project` ;
- **brut** (`b`) : seulement le dossier et un `CLAUDE.md` minimal, ni git ni `/init-project`.

Le projet est ensuite lancé. (Rouvert plus tard, un projet brut reçoit `init-project.md` comme
les autres : `/init-project` reste possible pour le convertir.)

## Versions, mises à jour et template

**Au lancement**, comme l'ancien launcher bash, le NG se met à jour si la dernière release
GitHub est plus récente : binaire de la plateforme et `claude-launcher.sh` (toujours utilisé
pour le layout et les vignettes) sont téléchargés, vérifiés contre `SHA256SUMS`, remplacés,
puis le NG se relance. Un navigateur resté sur l'ancien binaire est redémarré ; les projets
ouverts ne sont pas touchés. Hors ligne ou en cas d'échec, il démarre avec la version en place.
Désactiver : `claude-launcher-ng --no-update` ou `CL_NO_UPDATE=1`. Un binaire compilé à la
main (version `dev` ou suffixée) ne se remplace jamais.

Ensuite, vérification GitHub 6 s après le démarrage, puis toutes les 10 minutes :

- **NG** : si une release plus récente sort pendant que le launcher tourne, une annonce
  `↑ NG vX · cliquer pour installer ✕` apparaît : un clic sur le texte installe la version
  et redémarre le navigateur sur place (projets et zone tmux conservés) ; `✕` la masque.
- **Template** : un nouveau tag de `claude_project_template` est annoncé
  (`↑ template vX disponible ✕`), les caches `.template-commit` / `.template-version` (à côté
  du script, partagés avec le bash) sont mis à jour et les badges `✓`/`⚠` recalculés.
  Un projet en retard propose `/init-project` à son lancement.
- **`init-project.md`** : resynchronisé au démarrage puis à chaque nouveau tag du template
  (fichier à côté du script, copié dans `.claude/commands/` à l'ouverture d'un projet).
  Hors ligne, l'ancienne version est conservée.

## Architecture

- Deux serveurs tmux : le serveur par défaut (session `claude-hub`, inchangée) et une
  « coque » dédiée (socket `claude-ui`) qui découpe le terminal en navigateur + `tmux attach`.
  La coque n'a pas de préfixe : toutes les touches passent au tmux intérieur.
- `cache.go` : état des projets calculé en tâche de fond (2 calculs en parallèle) et écrit
  dans `~/.cache/claude-launcher/state/`, que le pane `◈ État` lit.
- `mirrors.go` : gestionnaire de l'onglet `⌗ Teamleaders`, qui réutilise les vignettes du
  script (`claude-launcher.sh --mirror`).
- `update.go` : versions, annonces, resynchronisation de `init-project.md`.

### Détection des changements d'un projet

`inotify` ne livre aucun événement sur `/mnt/c` sous WSL (système `9p`), même pour une
écriture faite depuis Linux (vérifié par `inotify_probe_test.go`). Le NG distingue donc :

- **disque Linux natif** : `inotify` récursif sur le projet et `.git` ;
- **9p** (`/mnt/c`…) : une signature bon marché (quelques `stat` : `HEAD`, `index`, entrées de
  premier niveau) interrogée toutes les 2 s pour le projet sélectionné, 8 s pour les projets
  ouverts ;
- dans les deux cas, un recalcul complet périodique sert de filet (sélectionné : 10 s au
  minimum, relevé à 3 × la durée du calcul ; ouverts : 45 s ; autres : 5 min), car modifier un
  fichier existant en sous-dossier ne change ni l'index ni le dossier.

## Dépannage

- **`Ctrl+←/→` ne fait rien** : votre terminal les intercepte. Glissez la bordure à la souris.
- **L'état d'un projet est en retard** : recalcul d'au plus ~10 s pour le projet sélectionné ;
  `Ctrl+R` relance le scan des projets.
- **Retrouver l'ancien menu** : `~/claude-launcher.sh --legacy`.
- **Quitter** : `Esc` puis `l` arrête le launcher (serveur `claude-ui`) en laissant les projets
  tourner dans `claude-hub` ; `Esc`, `t` puis `o` ferme aussi `claude-hub` et ses sessions groupées.
- **Pas de zone tmux à droite** : relancer `claude-launcher-ng`, le pane manquant est recréé.
- **Essayer sans toucher à `claude-hub`** :
  `CL_HUB=claude-hub-test CL_OUTER_SOCKET=claude-ui-test claude-launcher-ng`.
- **Les annonces de version n'apparaissent pas** : GitHub est interrogé sans jeton ; en cas de
  limite de débit ou hors ligne, le NG réessaie 10 minutes plus tard.

## Tests

`go test ./...` : comparaison de versions, resynchronisation de `init-project.md`, et une sonde
`inotify` qui s'ignore sur `9p`. Variables de test : `CL_ROOTS` (racines factices),
`CL_CLAUDE_BIN` (faux `claude`), `CL_HUB`, `CL_INNER_SOCKET`, `CL_OUTER_SOCKET`, `CL_SCRIPT`,
`CL_RAW_BASE` (serveur de fichiers bruts), `CL_FAKE_LATEST_TPL`, `CL_FAKE_LATEST_LAUNCHER`,
`XDG_CACHE_HOME`.

`docs/maquette.html` est la maquette interactive d'origine de l'interface.
