# Claude-Launcher-NG2 (v5)

Binaire Go qui pilote plusieurs projets Claude Code dans tmux : navigateur de
projets à gauche, tmux à droite, une fenêtre par projet (teamleader +
teammates). La v5 ajoute l'**exécution des projets dans des conteneurs** —
podman, docker, podman ou docker par ssh, Kubernetes — avec un tmux par
projet, imbriqué dans le hub et piloté par son canal de contrôle (`tmux -C`).
Sans configuration, un projet tourne sur l'hôte comme en v4.

NG2 a ses propres noms, et peut tourner à côté d'un launcher v4 : binaire
`claude-launcher-ng2`, coque `claude-ui-ng2`, serveur tmux intérieur
`claude-ng2` (session `claude-hub-ng2`), config
`~/.config/claude-launcher-ng2/`, cache `~/.cache/claude-launcher-ng2/`.
**Changer de version majeure (v4 ↔ v5) uniquement avec tous les projets
fermés.**

Vue d'ensemble, installation et configuration : [README du repo](../README.md).
Ce document détaille le fonctionnement et le dépannage.

## Installation

### Depuis une release

Chaque tag `v5.*` (et suivants) publie un binaire par plateforme (linux/darwin,
amd64/arm64) et un `SHA256SUMS` :

```bash
os=$(uname -s | tr A-Z a-z); arch=$(uname -m)
case "$arch" in x86_64) arch=amd64 ;; aarch64) arch=arm64 ;; esac
curl -fsSL "https://github.com/CCoupel/Claude-Launcher/releases/latest/download/claude-launcher-ng2-$os-$arch" \
  -o ~/.local/bin/claude-launcher-ng2 && chmod +x ~/.local/bin/claude-launcher-ng2
claude-launcher-ng2 version
```

### Depuis les sources

```bash
cd ng2
go build -o ~/.local/bin/claude-launcher-ng2 .
# version : go build -ldflags "-X main.ngVersion=v5.0.0" -o ... .
```

Prérequis : `tmux`, `git`, `bash` (lecture de la config) et `claude`. Pour les projets en
conteneur, en plus : le client du moteur (`podman`, `docker`, `kubectl`) ou `ssh`, selon le
backend ; rien n'est à préinstaller dans les images.

## Lancement

```bash
claude-launcher-ng2                    # = claude-launcher-ng2 shell
claude-launcher-ng2 configure          # crée/édite ~/.config/claude-launcher-ng2/config.conf ($EDITOR)
claude-launcher-ng2 relayout WIN_ID    # remet en ordre les panes d'une fenêtre projet (= Ctrl+B R)
claude-launcher-ng2 help               # commandes et raccourcis (= F1 dans le launcher)
claude-launcher-ng2 version
claude-launcher-ng2 backends          # backends, conteneurs et diagnostic
```

Au lancement, les teams mortes de `~/.claude/teams` (et leurs tâches) sont supprimées : une session
Claude coupée laisse la sienne. Une team n'est supprimée que si elle a des teammates avec un pane tmux
et qu'aucun de ces panes n'existe encore ; les teams sans teammate, ou tmux injoignable, ne sont jamais
touchées, ni celles dont la session du leader peut encore être reprise (voir ci-dessous).
Si ce nettoyage dure (stockage réseau), une barre de progression s'affiche au-delà d'une demi-seconde.

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
`~/.claude/claude-launcher-ng2/clean-exit/<chemin encodé>` (la date seule compte) si claude a rendu la main au
shell de lui-même ou sur `Ctrl+C` (statut < 128 ou 130). Un crash qui emporte le terminal (fenêtre fermée,
WSL tué) ou claude tué par un autre signal (OOM, segfault) ne pose rien. Une conversation qui n'a
pas bougé depuis ce marqueur est close comme par `/end-session` ; reprise ensuite, elle redevient en
suspens. Un **projet non géré** (sans template) reçoit en plus, à l'ouverture, un
`.claude/commands/end-session.md` minimal s'il n'en a pas (résumé de fin, sans rien modifier) : `/end-session`
s'y tape comme ailleurs ; une commande existante n'est jamais écrasée.

**Config portable** : si un `config.conf` se trouve dans `.config/claude-launcher-ng2/` **à côté du
binaire** (liens symboliques résolus), c'est ce dossier qui est utilisé (`config.conf` et
`backends.yaml`) ; sinon `~/.config/claude-launcher-ng2/`. Exemple, launcher posé sur un stockage
partagé et utilisable de toutes les machines :

```
/share/MFS/ROOT/CLAUDE_PROJETS/
├── claude-launcher-ng2
└── .config/claude-launcher-ng2/
    ├── config.conf
    └── backends.yaml
```

Les affectations des projets (hôte ou backend, `~/.local/state/claude-launcher-ng2/projects.json`)
restent propres à chaque machine.

**Config de claude** : `CLAUDE_CONFIG_DIR` dans `config.conf` l'impose à toutes les sessions du
launcher (hôte et conteneurs) ; un chemin relatif l'est par rapport au dossier de `config.conf`, ce
qui garde l'ensemble portable. Une variable `CLAUDE_CONFIG_DIR` déjà définie au lancement l'emporte.

```bash
# /share/MFS/ROOT/CLAUDE_PROJETS/.config/claude-launcher-ng2/config.conf
PROJECT_BASE="/share/MFS/ROOT/CLAUDE_PROJETS/PROJETS/"
GITHUB_DIRS=("$PROJECT_BASE")
CLAUDE_CONFIG_DIR="../../.claude"          # = /share/MFS/ROOT/CLAUDE_PROJETS/.claude
```

La configuration est `config.conf` (relue par bash : `GITHUB_DIRS`,
`CLAUDE_OPTIONS`, `RESUME_MODE`, `COLOR_PALETTE`, `USAGE_REFRESH_SEC`, `LAYOUT_*`, `PANE_*_STEP`…), créée au
premier lancement : **copie de la config v4** (`~/.config/claude-launcher.conf`) si elle existe, sinon les
valeurs par défaut. Les backends d'exécution sont dans `~/.config/claude-launcher-ng2/backends.yaml`
(voir [Projets en conteneur](#projets-en-conteneur-v5)).

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

Un projet configuré se lance **uniquement** sur son backend : pour le lancer
ailleurs, le reconfigurer par F2. Si sa configuration n'est plus valable
(backend retiré de `backends.yaml`, `env.yaml` absent ou illisible), il est
marqué `⚠` et son ouverture est refusée — jamais de repli sur l'hôte.

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

Pour passer une installation locale sur le stockage partagé (projets, `~/.claude`,
conversations, mémoire, réglages par projet), suivre le guide pas à pas
[MIGRATION.md](MIGRATION.md), avec exemples et retour arrière : copier les projets ne
suffit pas.

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

### Plusieurs launchers sur un même projet

Un projet en conteneur (surtout distant) peut être atteint par plusieurs
launchers, sur plusieurs machines ; **un seul le pilote** : son propriétaire,
noté dans la session tmux du conteneur et entretenu par son launcher (battement
de cœur toutes les 5 s, parti au-delà de 20 s). Un autre launcher qui l'ouvre
pendant ce temps demande :

| Choix | Effet |
|---|---|
| `l` | **rejoindre en lecture seule** : on voit la session (partage d'écran), sans pouvoir taper ni la réarranger |
| `p` | **prendre la main** : l'autre launcher passe en **lecture seule** (sa fenêtre reste ouverte et l'indique) et ce launcher devient propriétaire |
| `a` | annuler |

Le launcher qui a perdu la main reprend la sienne par **Entrée** sur le
projet : sa fenêtre en lecture seule est rouverte et la même question est
posée. Au démarrage, un launcher ne rattache que les sessions dont il est
propriétaire. Et un projet qui tourne dans un conteneur piloté par un autre
launcher ne peut pas être lancé sur l'hôte (deux claude dans le même dossier
partagé).

### Config de claude dans les conteneurs

Les sessions en conteneur utilisent `CLAUDE_CONFIG_DIR=~/.claude`, donc
`~/.claude/.claude.json` (un fichier monté seul se détacherait : claude le
réécrit par remplacement). Au premier conteneur, ce fichier est **copié** depuis
`~/.claude.json`, jamais déplacé : les sessions de l'hôte gardent le leur.

### Limites connues

- Le panneau milestone des projets lit la consommation du compte dans
  `~/.cache/claude-launcher/usage.json`, monté dans les conteneurs **locaux** ;
  dans un conteneur distant (ssh, docker distant, k8s), il l'interroge lui-même.
- Le copier-coller OSC 52 à travers les deux niveaux de tmux n'est pas vérifié.
- La carte Team lit la version de claude du leader dans le conteneur (une fois
  par minute) ; celles des teammates viennent du nom de leur processus.

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
  `~/.cache/claude-launcher-ng2/milestones-v2.json`.
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
  L'en-tête indique les versions : `NG2 v5.0.0 · tpl 4.2.1` (NG, dernier tag du template).
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
de la plateforme est téléchargé, vérifié contre `SHA256SUMS`, remplacé,
puis le NG se relance. Un navigateur resté sur l'ancien binaire est redémarré ; les projets
ouverts ne sont pas touchés. Hors ligne ou en cas d'échec, il démarre avec la version en place.
Désactiver : `claude-launcher-ng2 --no-update` ou `CL_NO_UPDATE=1`. Un binaire compilé à la
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
- **Version majeure commune** : à partir de la v5, launcher et template partagent leur version
  majeure (le template v5 suppose un launcher v5). NG2 ne retient que les tags du template dont
  la majeure ne dépasse pas la sienne : un template v6 ne sera jamais proposé à un NG2 v5.
- **`init-project.md`** : resynchronisé au démarrage (au commit adopté) puis à chaque adoption
  (`~/init-project.md`, copié dans `.claude/commands/` à l'ouverture d'un projet).
  Hors ligne, l'ancienne version est conservée.

## Architecture

- Deux serveurs tmux dédiés : l'« intérieur » (socket `claude-ng2`, session `claude-hub-ng2` —
  jamais le serveur par défaut, celui du launcher v4) et une « coque » (socket `claude-ui-ng2`) qui
  découpe le terminal en navigateur + `tmux attach`. La coque n'a pas de préfixe : toutes les
  touches passent au tmux intérieur, dont le préfixe est aiguillé (`Ctrl+B` vers le conteneur pour un
  projet en conteneur).
- `target.go` : cible tmux d'un projet — le hub lui-même (exécution directe) ou le tmux de son
  conteneur par canal de contrôle (`ctl.go`, une commande par ligne) ; `backend.go`, `k8s.go`,
  `ctr.go`, `bootstrap.sh`, `diag.go`, `owner.go`, `pidspace.go` : moteurs, bootstrap, diagnostic,
  propriétaire, espaces de PID.
- `cache.go` : état des projets calculé en tâche de fond (2 calculs en parallèle) et écrit
  dans `~/.cache/claude-launcher-ng2/state/`, que le pane `◈ État` lit.
- `mirrors.go` : gestionnaire de l'onglet `⌗ Teamleaders` (une vignette par projet ouvert,
  grille, pane d'attente ; l'onglet garde ses panes terminés jusqu'au passage suivant pour ne
  jamais se vider, ce qui fermerait `claude-hub-ng2`).
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
- **stockage réseau** — 9p (`/mnt/c`…), FUSE (MooseFS, sshfs…), NFS, SMB, Ceph : poser
  `inotify` sur toute l'arborescence y est lent (25 s pour 23 projets sur MooseFS) et il ne voit
  pas les écritures faites d'une autre machine ou d'un conteneur. Une signature bon marché (quelques `stat` : `HEAD`, `index`, entrées de
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
  `claude-launcher-ng2 relayout <window_id>`.
- **Quitter** : `Esc` puis `l` arrête le launcher (serveur `claude-ui-ng2`) en laissant les projets
  tourner dans `claude-hub-ng2` ; `Esc`, `t` puis `o` ferme aussi `claude-hub-ng2` et ses sessions groupées.
- **Pas de zone tmux à droite** : relancer `claude-launcher-ng2`, le pane manquant est recréé.
- **Essayer sans toucher à `claude-hub-ng2`** :
  `CL_HUB=claude-hub-test CL_OUTER_SOCKET=claude-ui-test claude-launcher-ng2`.
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
