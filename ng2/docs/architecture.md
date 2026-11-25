# Architecture

> Pour l'évolution vers plusieurs utilisateurs (agent d'espace, plan de contrôle), voir [saas.md](saas.md).

> Autres guides : [Installation et lancement](installation.md) · [Configuration et projets en conteneur](configuration.md) · [Launcher Windows natif](windows.md) · [Déploiement dans un cluster Kubernetes](kubernetes.md) · [Interface](interface.md) · [Versions, mises à jour et template](versions.md) · [Dépannage](depannage.md) · [Tests](tests.md). Vue d'ensemble : [README](../README.md).

**Sommaire**

- [Architecture](#architecture)
  - [Structure du code (v6)](#structure-du-code-v6)
  - [Détection des changements d'un projet](#détection-des-changements-dun-projet)

---

## Architecture

### Structure du code (v6)

Le code est réparti en packages, pour partager entre la coque tmux et le launcher Windows tout ce
qui ne dépend ni de tmux ni de l'OS :

| Package | Rôle |
|---|---|
| `main.go` | aiguillage des sous-commandes (`shell`, `nav`, `state`, `mirror`, `layout-watch`, `ctr-attach`, `backends`, `version`…) |
| `core/` | config, `backends.yaml`, moteurs et k8s, placement, pools et drain, nommage, `env.yaml`/`env-template.yaml`, bootstrap (`bootstrap.sh`), projets, teams et leader, cache d'état, conso, GitHub (releases, milestones, mises à jour), variables vers `settings.json`, `browser_paths`, serveur web et appariement, sessions tmux **dans les conteneurs** (canal de contrôle `ctl.go`, propriétaire) |
| `platform/` | ce qui dépend de l'OS : espaces de PID (`/proc`, `kill`), disque réseau (`statfs` ; Windows : UNC, `GetDriveType`), terminal en mode brut (termios), taille du terminal, verrou de fichier, relance du binaire (`execve` ; Windows : `.exe` renommé `.old`), pseudo-terminal (`pty.go` : creack/pty, ConPTY), chemin du binaire ; implémentation Linux/macOS (`_unix`, `_linux`, `_darwin`) et Windows (`_windows`), la logique restant dans des fichiers communs testés partout |
| `ui/tmux/` | la coque tmux : coque et navigateur, cartes (`◈ État`), vignettes (`mirror`, `mirrors`), mise en page (`layout`), `ctr-attach`, aide, légende, relais de touches |
| `ui/desktop/` | l'interface native (v6, #111) : état et appels (`app.go`, `actions.go`, `update.go`), onglet d'un projet (`projterm.go`), client et décodeur du mode contrôle de tmux (`tmuxclient.go`, `ctrlmode.go`), ponts Wails (`wails_on.go`) et web (`web.go`), interface HTML/CSS/JS et xterm.js embarqués (`frontend/`) |

Partagé par les deux interfaces, dans `core` : la légende des icônes (`legend.go` : icône, couleur
sémantique, signification, sans rendu), la mise en page des teams (`layout.go` : classement,
`TeamRows` en ratios, `LayoutString`, teintes, `ArrangeTeam`), l'état des conteneurs
(`ctrview.go`), la cible tmux d'un conteneur (`target.go`), l'ouverture en conteneur
(`ctropen.go`), le script de démarrage et ses questions (`start.go`), la fermeture
(`closeproj.go`), la préparation des mises à jour (`prefetch.go`, `CheckUpdatesNow`) et la
création d'un projet (`newproject.go`).

`core` ne dépend ni de `ui/tmux` ni d'un appel système propre à Linux :
`GOOS=windows go vet ./core/... ./platform/...` et `GOOS=windows go build ./...` doivent rester
propres. Le workflow `ng2-tests` les vérifie à chaque PR vers `main` et `v6`, avec
`go test ./core/... ./platform/...` et les tests de `ui/desktop` sur un runner `windows-latest`.
Pour l'interface Windows, `core` expose notamment `CheckTools` (outils requis et commande winget),
`LocalRefusal`, `HubPaths`/`RemotePath`/`LocalPath`, `CtrUser`, `InstallBinary`, et `platform`
`StartTerminal` (terminal redimensionnable), `OnNetworkFS`, `CleanupOldExe`.

- Deux serveurs tmux dédiés : l'« intérieur » (socket `claude-ng2`, session `claude-hub-ng2` —
  jamais le serveur par défaut, celui du launcher v4) et une « coque » (socket `claude-ui-ng2`) qui
  découpe le terminal en navigateur + `tmux attach`. La coque n'a pas de préfixe : toutes les
  touches passent au tmux intérieur, dont le préfixe est aiguillé (`Ctrl+B` vers le conteneur pour un
  projet en conteneur).
- `ui/tmux/target.go` : cible tmux d'un projet — le hub lui-même (exécution directe) ou le tmux de son
  conteneur par canal de contrôle (`core/ctl.go`, une commande par ligne) ; `core/backend.go`, `core/k8s.go`,
  `core/ctr.go`, `core/bootstrap.sh`, `core/diag.go`, `core/owner.go`, `platform/pidspace*.go` : moteurs, bootstrap, diagnostic,
  propriétaire, espaces de PID ; `ui/tmux/ctr.go` : `ctr-attach`.
- `core/cache.go` : état des projets calculé en tâche de fond (2 calculs en parallèle) et écrit
  dans `~/.cache/claude-launcher-ng2/state/`, que le pane `◈ État` lit.
- `ui/tmux/mirrors.go` : gestionnaire de l'onglet `⌗ Teamleaders` (une vignette par projet ouvert,
  grille, pane d'attente ; l'onglet garde ses panes terminés jusqu'au passage suivant pour ne
  jamais se vider, ce qui fermerait `claude-hub-ng2`).
- `ui/tmux/mirror.go` : une vignette — terminal en mode brut, un seul appel tmux par rafraîchissement
  (focus, curseur et contenu du leader — plus petit `%id` de la fenêtre, `winState.leader`),
  touches relayées en octets bruts (`send-keys -H`).
- `ui/tmux/layout.go` : mise en page d'une fenêtre projet. Un watcher par fenêtre (1 s) lit la team
  (`~/.claude/teams/*/config.json`, la plus récente pour le projet) et l'état tmux en un seul
  `list-panes` ; il réordonne (`swap-pane -d`, sans voler le focus), applique la géométrie
  (`select-layout` avec somme de contrôle tmux) et teinte les panes. Ordre MRU des actifs dans
  `$TMPDIR/claude_layout_<win>.order`, verrou par fenêtre partagé avec `relayout`.
- `core/update.go` : versions, annonces, resynchronisation de `init-project.md`.
- `ui/tmux/legend.go` : indicateurs d'un projet (icônes de sa ligne et leur signification).
- `core/milestone.go` : badge ⚑ (état des milestones GitHub du projet) et badge ⇄N (PR ouvertes).
- `core/treewatch.go` : surveillance de l'arborescence (projets ajoutés / supprimés hors du launcher).
- `core/resume.go` : reprise après crash (dernière conversation du projet, sa team, question au lancement).

### Détection des changements d'un projet

`inotify` ne livre aucun événement sur `/mnt/c` sous WSL (système `9p`), même pour une
écriture faite depuis Linux (vérifié par `core/inotify_probe_test.go`). Le NG distingue donc :

- **disque Linux natif** : `inotify` récursif sur le projet et `.git` ;
- **stockage réseau** — 9p (`/mnt/c`…), FUSE (MooseFS, sshfs…), NFS, SMB, Ceph : poser
  `inotify` sur toute l'arborescence y est lent (25 s pour 23 projets sur MooseFS) et il ne voit
  pas les écritures faites d'une autre machine ou d'un conteneur. Une signature bon marché (quelques `stat` : `HEAD`, `index`, entrées de
  premier niveau) interrogée toutes les 2 s pour le projet sélectionné, 8 s pour les projets
  ouverts ;
- dans les deux cas, un recalcul complet périodique sert de filet (sélectionné : 10 s au
  minimum, relevé à 3 × la durée du calcul ; ouverts : 45 s ; autres : 5 min), car modifier un
  fichier existant en sous-dossier ne change ni l'index ni le dossier.
