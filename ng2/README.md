# Claude-Launcher-NG2 (v5, en développement)

> **En cours** — suivi : [#46](https://github.com/CCoupel/Claude-Launcher/issues/46).
> NG2 = NG v4 + exécution des projets dans des conteneurs (podman, docker, ssh, k8s),
> tmux imbriqué piloté par `tmux -C`. Binaire `claude-launcher-ng2`, serveurs tmux
> `claude-ui-ng2` (coque) et `claude-ng2` (intérieur, session `claude-hub-ng2`),
> config `~/.config/claude-launcher-ng2/config.conf` (importée de la config v4 à la
> création), cache `~/.cache/claude-launcher-ng2/` : NG2 et v4 peuvent tourner côte
> à côte. **Changement de version majeure : uniquement avec tous les projets fermés.**
> La suite de ce document décrit encore NG v4 et sera réécrite avant la sortie.

Nouvelle génération de Claude-Launcher : un binaire Go qui a remplacé le menu fzf du
script bash (v3), puis ses derniers modes (mise en page des teams, vignettes : v4). Même modèle (une session tmux `claude-hub`, une fenêtre par projet,
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

Prérequis : `tmux`, `git`, `bash` (lecture de la config) et `claude`. Ni fzf, ni jq, ni
xxd, ni script compagnon.

## Lancement

```bash
claude-launcher-ng                    # = claude-launcher-ng shell
claude-launcher-ng configure          # crée/édite ~/.config/claude-launcher.conf ($EDITOR)
claude-launcher-ng relayout WIN_ID    # remet en ordre les panes d'une fenêtre projet (= Ctrl+B R)
claude-launcher-ng help               # commandes et raccourcis (= F1 dans le launcher)
claude-launcher-ng version
~/claude-launcher.sh                  # ancienne commande : redirige vers le NG
```

Au lancement, les teams mortes de `~/.claude/teams` (et leurs tâches) sont supprimées : une session
Claude coupée laisse la sienne. Une team n'est supprimée que si elle a des teammates avec un pane tmux
et qu'aucun de ces panes n'existe encore ; les teams sans teammate, ou tmux injoignable, ne sont jamais
touchées, ni celles dont la session du leader peut encore être reprise (voir ci-dessous).

### Reprise après crash

À l'ouverture d'un projet, si sa dernière conversation
(`~/.claude/projects/<chemin encodé>/<id>.jsonl`) a moins de `RESUME_MAX_AGE_H` heures (24 par
défaut), le launcher demande s'il faut la reprendre (`RESUME_MODE=ask`, ou `always` / `never`).
Reprendre lance `claude --resume <id>` : même id de session, donc même team
(`session-<8 premiers caractères>`) et même liste de tâches, que le nettoyage ci-dessus a épargnées.
Les teammates ne sont pas restaurés par Claude Code : le teamleader repris reçoit en premier message
la liste des teammates perdus et la consigne de respawner **tout** l'effectif attendu de la team, pas
seulement les agents des tâches en cours (une correction de QA en aura besoin) ; dans un projet du template,
via `/start-session` étape 4 puis 4bis (le template reconnaît la formule « ont été perdus »). La
question n'est pas posée si la team indique un leader encore vivant (session ouverte ailleurs), ni si
la conversation a été close par `/end-session` (commande tapée, même suivie d'échanges de fin : la
team de cette session n'est alors plus épargnée), ni si elle s'est terminée par une sortie propre, et
`/init-project` n'est jamais proposé sur une reprise.

**Sortie propre** : la commande `claude` du pane est suivie d'une commande bash qui pose
`~/.cache/claude-launcher/clean-exit/<chemin encodé>` (la date seule compte) si claude a rendu la main au
shell de lui-même ou sur `Ctrl+C` (statut < 128 ou 130). Un crash qui emporte le terminal (fenêtre fermée,
WSL tué) ou claude tué par un autre signal (OOM, segfault) ne pose rien. Une conversation qui n'a
pas bougé depuis ce marqueur est close comme par `/end-session` ; reprise ensuite, elle redevient en
suspens. Un **projet non géré** (sans template) reçoit en plus, à l'ouverture, un
`.claude/commands/end-session.md` minimal s'il n'en a pas (résumé de fin, sans rien modifier) : `/end-session`
s'y tape comme ailleurs ; une commande existante n'est jamais écrasée.

La configuration reste `~/.config/claude-launcher.conf` (relue par bash : `GITHUB_DIRS`,
`CLAUDE_OPTIONS`, `RESUME_MODE`, `COLOR_PALETTE`, `USAGE_REFRESH_SEC`, `LAYOUT_*`, `PANE_*_STEP`…), créée par
défaut au premier lancement.

Sous-commandes internes, lancées par tmux : `nav`, `state`, `fixwidth` (coque), `mirror WIN NOM`
(vignette Teamleaders), `layout-watch WIN LEADER DOSSIER` (mise en page d'une fenêtre projet).

## Projets en conteneur (v5)

Un projet peut tourner dans **son propre conteneur** au lieu de l'OS hôte : les
outils, bibliothèques et paquets qu'il installe restent dans le conteneur, et
chaque projet choisit sa distribution (Debian, Rocky, Alpine…). Sans
déclaration, rien ne change : le projet tourne sur l'hôte comme en v4.

### Déclarer l'environnement : `<projet>/.claude-launcher/env.yaml`

Versionné avec le projet :

```yaml
image: docker.io/library/debian:12   # obligatoire ; image « vanilla », rien à préinstaller
packages: [python3-venv, libpq-dev]  # en plus de tmux git bash sudo curl (implicites)
packages_by_manager:                 # noms qui diffèrent selon la distribution
  dnf: [libpq-devel]
setup:                               # exécuté une fois, en utilisateur, dans le projet
  - python3 -m venv .venv && .venv/bin/pip install -r requirements.txt
mounts:                              # dossiers en plus (lecture seule par défaut)
  - path: ../claude_project_template
prefer: podman                       # backend proposé par défaut (facultatif)
```

Le conteneur ne monte que **le dossier du projet** (au même chemin que sur
l'hôte) et `~/.claude` (conversations, teams, réglages, connexion : partagés).

### Configurer l'exécution d'un projet

Le formulaire **Exécution** propose `0 hôte` puis les socles (`↑↓` ou un
chiffre), et le backend conteneur (`←→`). Choisir un socle écrit un
`env.yaml` minimal et commenté (ou change sa ligne `image:` s'il existe) et
affecte le projet à ce backend ; `0 hôte` le remet sur l'OS (l'`env.yaml` est
gardé). Il s'ouvre :

- à la **création** (« ✦ Nouveau projet »), après le type de projet ;
- à la **première ouverture** d'un projet jamais configuré : le choix est fait
  une fois, puis ↵ ouvre le projet (s'il n'existe aucun backend conteneur, rien
  n'est demandé) ;
- par **F2** sur un projet fermé, **prérempli avec son état** (socle et backend
  actuels, ou « hôte ») : c'est la reconfiguration.

Le mode et le socle **présélectionnés** se règlent dans `backends.yaml` :

```yaml
defaults:
  backend: podman                         # host : « hôte » présélectionné
  image: docker.io/library/debian:12
images:                                   # socles proposés (facultatif)
  - docker.io/library/debian:12
  - docker.io/library/ubuntu:24.04
  - docker.io/library/rockylinux:9
  - docker.io/library/alpine:3.20
```

Sans `images`, ce sont ces quatre-là (bootstrap vérifié sur chacun). Un
`defaults.image` hors de la liste y est ajouté en tête.

### Backends : `backends.yaml`

Sans `~/.config/claude-launcher-ng2/backends.yaml`, les backends conteneur sont podman et docker
**s'ils répondent** (un `docker` de Docker Desktop sans intégration WSL est
ignoré). Pour les nommer ou viser un contexte docker :

```yaml
backends:
  podman: { type: podman }
  docker: { type: docker, context: default }
```

#### Backends distants

Quatre façons d'atteindre un moteur de conteneurs, plus Kubernetes :

```yaml
backends:
  srv1-docker: { type: docker, host: tcp://192.168.1.211:2375,  shared: [/share/MFS/ROOT] }
  srv2-podman: { type: podman, host: tcp://192.168.1.212:8888,  shared: [/share/MFS/ROOT] }
  srv1-sshd:   { type: ssh, host: srv1, engine: docker,          shared: [/share/MFS/ROOT] }
  srv2-sshp:   { type: ssh, host: srv2, engine: podman,          shared: [/share/MFS/ROOT] }
```

| Type | Accès | Côté machine distante |
|---|---|---|
| `docker` + `host` | **direct** au démon (`docker -H`, ou `context`) | démon exposé (`daemon.json` → `"hosts": [… "tcp://0.0.0.0:2375"]`, TLS : 2376) |
| `podman` + `host` | **direct** au service podman (`podman --url`) | `podman system service --time=0 tcp://0.0.0.0:8888` (API non authentifiée ni chiffrée : réseau de confiance) |
| `ssh`, `engine: docker` | `ssh <host> docker …` | docker dans le PATH d'une commande ssh, utilisateur dans le groupe `docker` |
| `ssh`, `engine: podman` (défaut) | `ssh <host> podman …` | podman dans le PATH d'une commande ssh |

#### Kubernetes

```yaml
backends:
  lab: { type: k8s, context: k8s, namespace: claude-ng2,
         volumes: { /share/MFS/ROOT: claude-shared } }   # racine partagée → PVC ReadWriteMany
```

Un projet = un **StatefulSet** (1 réplique) `clx-<projet>-<empreinte>` ; son pod
`…-0` monte chaque PVC de `volumes` **au chemin de sa racine**, donc aux mêmes
chemins que sur la machine du launcher (les racines de `volumes` tiennent lieu
de `shared`). Le pod tourne en root pour le bootstrap ; les commandes
utilisateur passent par `su` (`kubectl exec` n'a pas de `-u`). Le chemin du
projet est en annotation (`cl.project`), le listing par labels
(`cl.launcher=ng2`, `cl.backend`).

Prérequis : `kubectl` et son contexte, un namespace où l'on peut créer des
StatefulSets, des PVC ReadWriteMany liés (ex. MooseFS : PV `csi.moosefs.com`
avec `volumeAttributes.mfsSubDir`), et un namespace qui autorise les pods root.
`claude-launcher-ng2 backends` signale ce qui manque, commande à l'appui.

Pod perdu (nœud, éviction) : le StatefulSet le recrée, la fenêtre se ferme ;
à la réouverture, le bootstrap est refait et la reprise de la conversation est
proposée (le launcher constate que le pod n'a plus de session : le leader est
mort, même si son PID n'est pas vérifiable d'ici).

Le client du moteur doit être installé sur la machine du launcher pour les
accès directs (le client podman ne pilote pas un démon docker). En ssh,
`engine_host` donne l'adresse du démon côté machine distante (docker rootless :
`unix:///run/user/<uid>/docker.sock`, service podman…). Accès ssh par clé, sans
mot de passe.

**`shared`** : racines du stockage partagé (NFS, MooseFS…), montées **aux mêmes
chemins** sur la machine du launcher et sur la machine distante. `~/.claude`
(ou `CLAUDE_CONFIG_DIR`) et le projet doivent s'y trouver : le launcher lit
lui-même conversations, teams et marqueurs, et trouve les projets dans son
navigateur. Les dossiers sont montés avec `--mount` : s'ils manquent de l'autre
côté, la création échoue (au lieu d'y créer un dossier vide).

Un backend inutilisable est signalé `⚠` dans le navigateur, et
`claude-launcher-ng2 backends` détaille chaque problème avec la façon d'y
remédier : client ou moteur absent, accès au socket docker refusé, démon
distant non exposé, accès ssh, podman absent de la machine distante, stockage
partagé non monté ou non déclaré, `~/.claude` hors du partage.

L'affectation (y compris « hôte ») est mémorisée par machine
(`~/.local/state/claude-launcher-ng2/projects.json`), pas dans le projet.

| Icône | Signification |
|---|---|
| `⬡` gris | `env.yaml` présent, projet sur l'hôte (ou conteneur pas encore créé) |
| `⬢` cyan | conteneur en marche |
| `⬢` gris | conteneur arrêté (redémarré à l'ouverture) |

### À l'ouverture

1. Le conteneur `cl-<projet>-<empreinte>` est créé (ou démarré), avec les
   labels `cl.launcher=ng2`, `cl.project`, `cl.backend`, `cl.image`.
2. **Bootstrap** (idempotent, rejoué seulement si `env.yaml` change) : paquets
   système (`apt`, `dnf`, `apk`, `zypper`), utilisateur aux uid/gid de l'hôte
   avec sudo, **installation de claude**, étapes `setup`. Compter ~20 s la
   première fois (image en cache), une fraction de seconde ensuite.
3. Le démarrage habituel (questions `/init-project` et reprise, claude, marqueur
   de sortie propre) s'exécute **dans le tmux du conteneur**, attaché dans la
   fenêtre du projet. Les teammates y sont créés et mis en page comme en local.

Prérequis de l'image : un gestionnaire de paquets connu et **tmux ≥ 3.2** dans
ses dépôts (Debian 12, Rocky/Alma 9, Alpine 3.16+ ; pas UBI sans abonnement
RHEL, ni RHEL 8 et ses dérivés).

### Clavier dans un projet en conteneur

| Touche | Effet |
|---|---|
| `Ctrl+B` + touche | agit dans le conteneur (`z` zoom, flèches, `[` copie, `R` remise en ordre…) |
| `Ctrl+B Ctrl+B` + touche | préfixe du hub (`n` / `p` : fenêtre suivante / précédente…) |
| double-clic | zoom du pane, comme en local |

### Launcher arrêté, hub fermé

La session du conteneur (claude **et** ses teammates) continue de tourner. Au
prochain lancement, NG2 retrouve les conteneurs par leurs labels et recrée la
fenêtre de chaque projet dont la session est vivante : on retrouve la session
telle quelle. Fermer la fenêtre d'un projet en conteneur ne l'arrête pas non
plus : Entrée dans le navigateur s'y rattache. Pour l'arrêter, quitter claude
(`/exit`, ou `/end-session` puis `/exit`).

### Config de claude dans les conteneurs

Les sessions en conteneur utilisent `CLAUDE_CONFIG_DIR=~/.claude`, donc
`~/.claude/.claude.json` (un fichier monté seul se détacherait : claude le
réécrit par remplacement). Au premier conteneur, ce fichier est **copié** depuis
`~/.claude.json`, jamais déplacé : les sessions de l'hôte gardent le leur.

### Changement de version majeure

v4 ne connaît pas les conteneurs : son nettoyage des teams et sa reprise
peuvent prendre la session d'un conteneur pour morte. **Passer de v4 à v5 (ou
revenir) uniquement avec tous les projets fermés.**

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
  Badge milestone `⚑` (projets GitHub), calculé avec **la règle du panneau milestone des projets**
  (`skills/milestone-panel`), pour que launcher et projet ouvert soient d'accord : **jaune** = milestone
  en cours (le premier ouvert, par échéance puis version, ayant une issue ouverte avec un label de phase
  `PLANNING`…`DONE`) ; **orange** = à défaut, un milestone ouvert dont toutes les issues sont fermées (à
  publier) ; **gris** = des milestones ouverts mais aucun en cours (le panneau : « Aucun milestone en
  cours ») ; **vert** = plus rien d'ouvert, dernier milestone clos (publié) ; rien sans milestone.
  Badge PR `⇄N` (violet, projets GitHub) : N pull requests ouvertes en attente (`gh pr list --state
  open`, brouillons compris ; la légende précise « dont N brouillon(s) ») ; rien sans PR ouverte.
  La carte **Projet** de la vue `◈ État` (et l'aperçu) commence par la légende des icônes de la ligne
  du projet : chaque icône, dans sa couleur et dans l'ordre du navigateur, suivie de sa signification
  (`!   local : modifié directement sur main`, `↺   session non close (il y a 12 min) : reprise
  proposée à l'ouverture`…). Ligne et légende sont tirées de la même source (`legend.go`).
  La carte **Milestone** de la vue `◈ État` (et l'aperçu) donne le milestone en cours, sa barre
  d'avancement (issues fermées / total), ses issues ouvertes par phase et les bloquées ; sans milestone
  en cours, la liste des milestones ouverts avec leur avancement. Trois appels `gh` par projet (ceux du
  panneau : milestones, issues ouvertes ; plus les PR ouvertes) toutes les 5 min, `F5` pour forcer ; cache partagé
  `~/.cache/claude-launcher/milestones-v2.json`.
  Un projet fermé dont la dernière conversation est restée en suspens (non close par `/end-session`
  ni par une sortie propre, de moins de `RESUME_MAX_AGE_H` heures, ouverte nulle part) porte `↺` (rouge si sa team montre un
  crash) : c'est la session que son ouverture proposera de reprendre.
  Un dossier de projet créé, renommé ou supprimé hors du launcher apparaît ou disparaît sans
  `Ctrl+R` : seuls les racines et dossiers de catégorie sont surveillés (inotify sur disque Linux ;
  sur `/mnt/c`, où inotify est muet, leur date comparée toutes les 3 s, soit quelques `stat`), et
  l'arbre n'est relu qu'après un changement (`treewatch.go`).
  Sous la consommation, une ligne jaune `⚠ connexion claude.ai : expiration …` apparaît 3 jours
  avant l'expiration de la connexion claude.ai (jeton de renouvellement, ~30 jours), rouge
  `✗ connexion claude.ai expirée` ensuite ou si l'endpoint refuse le jeton : un clic ouvre une
  fenêtre `login` qui lance `claude /login`. Après un `/login`, la consommation revient sans
  attendre la fin de la pause qui suit un échec ; `F5` la relit immédiatement.
  L'en-tête indique les versions : `NG v4.0.0 · tpl 3.26.0` (NG, dernier tag du template).
- **Sous le navigateur** : `◈ État`, qui suit la sélection (git, versions, template, team —
  teammates du registre du template, version de leur copie, `+` si compagnon, modèle et effort,
  version de claude des teammates lancés —, fichiers) — lu depuis un cache préparé en tâche de fond. La zone de droite ne bouge pas.
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
| `Ctrl+B R` dans un projet | remettre en ordre les panes de la team |
| double-clic sur un pane d'une team | plein écran (comme `Ctrl+B z`) ; double-clic ou `Ctrl+B z` à nouveau pour revenir en taille normale |
| clic sur `↑ NG … cliquer pour installer` (vert) | installer la nouvelle version et redémarrer le navigateur (projets conservés) |
| clic sur `↑ tpl … cliquer pour installer` (vert) | adopter la nouvelle version du template |
| `Ctrl+U`, clic sur `✕` | masquer une annonce de mise à jour |
| clic sur `⚠ connexion claude.ai …` / `✗ … expirée` | ouvrir une fenêtre `login` qui lance `claude /login` |
| `F5` | rechercher les mises à jour et relire la consommation maintenant (réaffiche les annonces masquées) |
| `F1` | aide : commandes et raccourcis, dans un `display-popup` tmux par-dessus la coque |
| `Esc` | effacer le filtre ; sinon quitter : `l` launcher seul (projets conservés), `t` puis `o` tout fermer |

Création d'un projet : le formulaire demande le nom, puis le type de projet :

- **géré** (`g`, par défaut) : remote git facultatif ; `git init`, `origin` ajouté si un remote
  est donné, `init-project.md` déposé, et claude démarre directement sur `/init-project` ;
- **brut** (`b`) : seulement le dossier et un `CLAUDE.md` minimal, ni git ni `/init-project`.

Le projet est ensuite lancé. (Rouvert plus tard, un projet brut reçoit `init-project.md` comme
les autres : `/init-project` reste possible pour le convertir.)

## Versions, mises à jour et template

**Au lancement**, le NG se met à jour si la dernière release GitHub est plus récente : binaire
de la plateforme et `~/claude-launcher.sh` (s'il existe : simple redirection) sont téléchargés, vérifiés contre `SHA256SUMS`, remplacés,
puis le NG se relance. Un navigateur resté sur l'ancien binaire est redémarré ; les projets
ouverts ne sont pas touchés. Hors ligne ou en cas d'échec, il démarre avec la version en place.
Désactiver : `claude-launcher-ng --no-update` ou `CL_NO_UPDATE=1`. Un binaire compilé à la
main (version `dev` ou suffixée) ne se remplace jamais.

Si le tag est déjà poussé mais que sa release n'est pas encore publiée (CI en cours), le NG
démarre avec la version en place.

Ensuite, vérification GitHub 6 s après le démarrage, puis toutes les 10 minutes (2 minutes tant
qu'une release est attendue). Chaque nouvelle version est annoncée en deux temps :
en **orange** dès le tag (`↑ NG vX · mise à jour bientôt prête ✕`, non cliquable), puis en
**vert** quand la release est publiée (`↑ NG vX · cliquer pour installer ✕`).

- **NG** : un clic sur l'annonce verte installe la version et redémarre le navigateur sur place
  (projets et zone tmux conservés) ; `✕` la masque.
- **Template** (`↑ tpl vX …`) : un clic sur l'annonce verte adopte la version : les caches
  `.template-commit` / `.template-version` (dans `~`) passent au
  tag, `init-project.md` est resynchronisé et les badges `✓`/`⚠` recalculés. Un projet en
  retard propose ensuite `/init-project` à son lancement. Sans cache (première installation),
  le dernier tag est adopté d'office.
- **`init-project.md`** : resynchronisé au démarrage (au commit adopté) puis à chaque adoption
  (`~/init-project.md`, copié dans `.claude/commands/` à l'ouverture d'un projet).
  Hors ligne, l'ancienne version est conservée.

## Architecture

- Deux serveurs tmux : le serveur par défaut (session `claude-hub`, inchangée) et une
  « coque » dédiée (socket `claude-ui`) qui découpe le terminal en navigateur + `tmux attach`.
  La coque n'a pas de préfixe : toutes les touches passent au tmux intérieur.
- `cache.go` : état des projets calculé en tâche de fond (2 calculs en parallèle) et écrit
  dans `~/.cache/claude-launcher/state/`, que le pane `◈ État` lit.
- `mirrors.go` : gestionnaire de l'onglet `⌗ Teamleaders` (une vignette par projet ouvert,
  grille, pane d'attente ; l'onglet garde ses panes terminés jusqu'au passage suivant pour ne
  jamais se vider, ce qui fermerait `claude-hub`).
- `mirror.go` : une vignette — terminal en mode brut, un seul appel tmux par rafraîchissement
  (focus, curseur et contenu du leader), touches relayées en octets bruts (`send-keys -H`).
- `layout.go` : mise en page d'une fenêtre projet. Un watcher par fenêtre (1 s) lit la team
  (`~/.claude/teams/*/config.json`, la plus récente pour le projet) et l'état tmux en un seul
  `list-panes` ; il réordonne (`swap-pane -d`, sans voler le focus), applique la géométrie
  (`select-layout` avec somme de contrôle tmux) et teinte les panes. Ordre MRU des actifs dans
  `$TMPDIR/claude_layout_<win>.order`, verrou par fenêtre partagé avec `relayout`.
- `update.go` : versions, annonces, resynchronisation de `init-project.md`.
- `legend.go` : indicateurs d'un projet (icônes de sa ligne et leur signification).
- `milestone.go` : badge ⚑ (état des milestones GitHub du projet) et badge ⇄N (PR ouvertes).
- `treewatch.go` : surveillance de l'arborescence (projets ajoutés / supprimés hors du launcher).
- `resume.go` : reprise après crash (dernière conversation du projet, sa team, question au lancement).

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
- **Panes d'une team en désordre** : `Ctrl+B R` dans l'onglet du projet, ou
  `claude-launcher-ng relayout <window_id>`.
- **Quitter** : `Esc` puis `l` arrête le launcher (serveur `claude-ui`) en laissant les projets
  tourner dans `claude-hub` ; `Esc`, `t` puis `o` ferme aussi `claude-hub` et ses sessions groupées.
- **Pas de zone tmux à droite** : relancer `claude-launcher-ng`, le pane manquant est recréé.
- **Essayer sans toucher à `claude-hub`** :
  `CL_HUB=claude-hub-test CL_OUTER_SOCKET=claude-ui-test claude-launcher-ng`.
- **Les annonces de version n'apparaissent pas** : GitHub est interrogé sans jeton ; en cas de
  limite de débit ou hors ligne, le NG réessaie 10 minutes plus tard.

## Tests

`go test ./...` : comparaison de versions, resynchronisation de `init-project.md`, mise en page
(géométrie, somme de contrôle, ordre MRU, teintes comparées au bash v3), reprise après crash
(encodage des chemins, dernière conversation, team, teams épargnées), et une sonde
`inotify` qui s'ignore sur `9p`. Variables de test : `CL_ROOTS` (racines factices),
`CL_CLAUDE_BIN` (faux `claude`), `CL_HUB`, `CL_INNER_SOCKET`, `CL_OUTER_SOCKET`, `CL_SCRIPT`,
`CL_RAW_BASE` (serveur de fichiers bruts), `CL_API_BASE` (API GitHub), `CL_RELEASE_BASE`
(fichiers de release), `CL_FAKE_LATEST_TPL`, `CL_FAKE_LATEST_LAUNCHER`, `CL_FAKE_TPL_RELEASED`,
`XDG_CACHE_HOME`.

`docs/maquette.html` est la maquette interactive d'origine de l'interface.
