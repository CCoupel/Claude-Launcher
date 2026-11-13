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
# version : go build -ldflags "-X claude-launcher-ng2/core.NgVersion=v6.0.0" -o ... .
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

Les affectations des projets (hôte ou backend) sont dans `projects.json`, dans ce même dossier
(depuis la v5.4.3) : en mode portable, tous les hubs, sur toutes les machines, savent où vit chaque
projet. L'ancien `~/.local/state/claude-launcher-ng2/projects.json`, propre à la machine, est repris
une fois et laissé en place.

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
`CLAUDE_OPTIONS`, `RESUME_MODE`, `CTR_STOP_ON_EXIT`, `COLOR_PALETTE`, `USAGE_REFRESH_SEC`, `LAYOUT_*`, `PANE_*_STEP`…), créée au
premier lancement : **copie de la config v4** (`~/.config/claude-launcher.conf`) si elle existe, sinon les
valeurs par défaut. Les backends d'exécution sont dans `~/.config/claude-launcher-ng2/backends.yaml`
(voir [Projets en conteneur](#projets-en-conteneur-v5)).

**Variables d'environnement de toutes les sessions claude (v5.7.0)** : `GITHUB_TOKEN` (exporté en
`GH_TOKEN` et `GITHUB_PERSONAL_ACCESS_TOKEN`), `CLAUDE_DISABLE_MOUSE`, `CLAUDE_EXPERIMENTAL_TEAMS` et
**toutes les entrées de `EXTRA_ENVS`** (`"NOM=valeur"`, une paire de guillemets autour de la valeur
ôtée) sont recopiées par le launcher dans le bloc `env` du **`settings.json` utilisateur**
(`$CLAUDE_CONFIG_DIR/settings.json`), au démarrage du hub et à chaque ouverture de projet. Claude
lit ce fichier quel que soit son dossier de lancement : projets sur l'hôte ou en conteneur,
worktrees, et **teammates** — créés par claude dans de nouveaux panes tmux, ils n'héritent pas du
shell du premier pane, et ceux démarrés dans un sous-dossier (`.claude/agents`…) ne voient pas le
`.claude/settings.local.json` du projet. Rien à faire par projet.
- Seules les clés du launcher sont touchées, le reste du fichier est gardé ; une variable retirée de
  `config.conf` est ôtée (liste tenue dans `$CLAUDE_CONFIG_DIR/claude-launcher/settings-env.json`) ;
  rien n'est réécrit sans changement ; un `settings.json` illisible (JSON invalide) est laissé tel quel.
- Une variable secrète (nom contenant `TOKEN`, `KEY`, `SECRET`, `PASSWORD`, `CREDENTIAL`) passe le
  fichier en `0600` et n'est plus tapée dans le shell du projet (ni historique ni scrollback tmux) ;
  les autres y sont toujours exportées. Si la recopie échoue, tout est tapé comme avant.
- Un `env` du `.claude/settings.local.json` d'un projet l'emporte pour ce projet.
- Ces variables valent aussi pour un claude lancé hors du launcher avec le même `CLAUDE_CONFIG_DIR`.

```bash
EXTRA_ENVS=(
  "MY_API_URL=https://api.example.com"
  "SENTRY_AUTH_TOKEN=sntrys_..."      # secret : settings.json seulement
)
```

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
  - "[ -d .venv/bin ] || python3 -m venv .venv"
  - .venv/bin/pip install -r requirements.txt
watch: [requirements.txt]            # contenu modifié → setup rejoué (seul) à l'ouverture
on_start:                            # à chaque démarrage du projet (pas au rattachement)
  - .venv/bin/python manage.py migrate --check
local: [.venv]                       # dossiers privés au conteneur (voir ci-dessous)
mounts:                              # dossiers en plus (lecture seule par défaut)
  - path: ../claude_project_template
prefer: podman                       # backend proposé par défaut (facultatif)
```

Le conteneur ne monte que **le dossier du projet** (au même chemin que sur
l'hôte) et `~/.claude` (conversations, teams, réglages, connexion : partagés).

#### Environnement fourni par le template : `env-template.yaml` (v5.5.0)

Deux fichiers versionnés, chacun avec **un seul propriétaire** :

| Fichier | Propriétaire | Écriture |
|---|---|---|
| `.claude-launcher/env-template.yaml` | le template (`/init-project`), selon la stack | réécrit entièrement à chaque passage : ne pas le modifier |
| `.claude-launcher/env.yaml` | le projet | à la main ; le launcher n'y touche que pour `image:` (F2) |

L'un des deux suffit. NG2 les fusionne (après le bootstrap du launcher, ci-dessous) :

- `image`, `prefer` : ceux d'`env.yaml` l'emportent ; `prefer` est refusé dans
  `env-template.yaml` (choix du backend, propre au projet) ;
- `packages`, `packages_by_manager`, `watch`, `local` : réunis ;
- `setup`, `on_start` : étapes du template, puis celles du projet ;
- `mounts` : réunis par `path`, le `ro` d'`env.yaml` l'emporte.

Un projet n'enlève rien au template. Toute modification de l'un ou l'autre fichier
rejoue le bootstrap : les étapes doivent pouvoir être rejouées. Le bootstrap est
**additif** : un paquet retiré d'un fichier reste installé dans le conteneur
existant (rien n'est désinstallé, le conteneur n'est pas recréé pour cela) ; il
disparaît à la prochaine recréation (changement d'`image` ou des dossiers `local`). Un `env.yaml` seul
se lit exactement comme avant (empreinte inchangée, conteneur non recréé).

#### Bootstrap du launcher : `env.yaml` à côté de `backends.yaml`

Ce qui sert à **vous** plutôt qu'à un projet (statusline, hooks, outils de
votre `settings.json`) se déclare une fois pour tous vos conteneurs dans
`<config>/env.yaml`, à côté de `backends.yaml` (ex.
`/share/MFS/ROOT/CLAUDE_PROJETS/.config/claude-launcher-ng2/env.yaml`) :

```yaml
# bootstrap du launcher : ajouté à celui de chaque projet
packages: [python3, jq]          # ex. statusline en python
setup:
  - pip install --user --break-system-packages rich
```

Clés admises : `packages`, `packages_by_manager`, `setup`, `on_start` (les
autres, comme `image` ou `local`, regardent un projet et sont refusées avec un
message clair). Les paquets sont réunis à ceux du projet, les étapes du launcher
passent **avant** celles du projet. Modifier ce fichier rejoue le bootstrap de
chaque conteneur à sa prochaine ouverture ; sans lui, rien ne change.

Exemple vécu : une statusline `bash … statusline-command.sh` qui lit son JSON
avec `python3` reste vide dans un conteneur `ubuntu:24.04`, qui n'a pas python :
`packages: [python3]` ici la répare partout.

#### Ce qui est construit reste dans le conteneur

Le dossier du projet est partagé (hôte, autres conteneurs, autres machines) : il
ne devrait contenir que les sources, les verrous de dépendances et `env.yaml`.
Ce que `setup` construit dépend de l'image (glibc ou musl, chemins des
interpréteurs) :

- **`local:`** — chemins relatifs au projet (`node_modules`,
  `web/node_modules`, `.venv`…) **privés au conteneur** : un volume du conteneur
  (volume anonyme podman/docker, `emptyDir` en k8s) est monté par-dessus et
  **masque** le dossier du projet. Les outils les trouvent à leur place
  habituelle ; l'hôte garde son propre `node_modules` sans conflit. Ils
  appartiennent à l'utilisateur et vivent autant que le conteneur : recréé
  (image changée, liste `local` modifiée, pod perdu), il repart de volumes
  neufs, reconstruits par `setup`. Le moteur crée au besoin, dans le projet, le
  dossier vide servant de point de montage.
- **Caches** (pip, npm, go, uv…) : rien à faire, ils sont sous `$HOME`
  (`~/.cache`, `~/.npm`, `~/go`), qui est déjà propre au conteneur (seul
  `~/.claude` est partagé).
- **`watch:`** — fichiers dont le **contenu** est comparé à chaque ouverture :
  s'il a changé depuis le dernier `setup` (verrou de dépendances modifié), les
  étapes `setup` sont rejouées, seules, sans réinstaller les paquets système.
- **`on_start:`** — étapes rejouées à chaque démarrage d'une session du projet
  (jamais au rattachement à une session vivante) ; à l'auteur de les garder
  rapides (`[ -d node_modules/.bin ] || npm ci`).

Les étapes `setup` et `on_start` tournent en utilisateur, dans le projet, avec
`bash -e` : une erreur arrête l'ouverture (et `setup` sera retenté à la suivante).

### Configurer l'exécution d'un projet

Le formulaire **Exécution** propose `0 hôte` puis les socles (`↑↓` ou un
chiffre), et le backend conteneur (`←→`). Choisir un socle écrit un
`env.yaml` minimal et commenté (ou change sa ligne `image:` s'il existe ; rien
n'est écrit si `env-template.yaml` fournit déjà ce socle) et
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
(backend retiré de `backends.yaml`, `env.yaml` et `env-template.yaml` absents,
ou l'un d'eux illisible), il est
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

#### Groupe de serveurs (docker, podman, ssh)

Un backend docker, podman ou ssh peut viser **plusieurs serveurs** (`hosts:`
à la place de `host:`). Le projet est affecté au groupe (F2 propose son nom),
et **le launcher choisit le serveur** :

```yaml
backends:
  docker-pool: { type: docker, shared: [/share/MFS/ROOT],
                 hosts: [tcp://192.168.1.211:2375, tcp://192.168.1.212:2375, tcp://192.168.1.213:2375] }
  ssh-pool:    { type: ssh, engine: podman, shared: [/share/MFS/ROOT], hosts: [srv1, srv2] }
```

- **Aucun registre** : les conteneurs eux-mêmes font foi (labels
  `cl.launcher`, `cl.project`, `cl.backend`) ; le serveur d'un projet est celui
  où l'on trouve son conteneur. Rien à perdre ni à resynchroniser.
- **À l'ouverture**, tous les serveurs du groupe sont interrogés (en
  parallèle) : le projet garde le serveur de son conteneur, même arrêté (son
  bootstrap, son `$HOME` et ses dossiers `local:` y sont) ; sans conteneur, il
  va sur le serveur qui a **le moins de conteneurs NG2 en marche**. Un verrou
  (`.claude-launcher/.lock`, le temps de la création) empêche deux launchers de
  le créer sur deux serveurs.
- **Entre deux launchers**, le scan régulier du navigateur (10 s) parcourt
  tous les serveurs : un projet ouvert par l'autre apparaît en 10 s au plus,
  avec son point (`■` orange piloté ailleurs…).
- **Serveur injoignable** : le groupe reste utilisable tant qu'un serveur
  répond ; à l'ouverture d'un projet introuvable, le launcher demande s'il faut
  démarrer ailleurs (son conteneur est peut-être sur le serveur muet). Chaque
  commande de listing est bornée à 8 s.
- **Doublon** (le même projet sur deux serveurs) : point `✕` ; Entrée demande
  lequel garder, les autres sont supprimés.
- **Rééquilibrer** : supprimer le conteneur d'un projet ; il est replacé à la
  prochaine ouverture.
- `claude-launcher-ng2 backends` détaille chaque serveur : état, conteneurs,
  problèmes. En ssh, une connexion persistante (`ControlMaster`) sert toutes les
  commandes vers un même serveur.

#### Pools de machines nommés

Quand plusieurs backends visent les mêmes machines (docker **et** ssh, par
exemple), la liste se déclare **une fois** dans `pools:` ; chaque backend la
cite par `pool:` (à la place de `hosts:`) :

```yaml
pools:
  dev:
    hosts: [192.168.1.211, 192.168.1.212, 192.168.1.213]
  gros:
    hosts: [srv6, srv7, srv8]
    drain: [srv7]                # maintenance : plus de nouveaux projets

backends:
  docker-dev:  { type: docker, pool: dev,  host: "tcp://{}:2375", shared: [/share/MFS/ROOT] }
  ssh-dev:     { type: ssh,    pool: dev,  host: "cyril@{}", engine: podman, shared: [/share/MFS/ROOT] }
  docker-gros: { type: docker, pool: gros, host: "tcp://{}:2375", shared: [/share/MFS/ROOT] }
```

- Un pool est **toujours un objet** : `hosts:` (obligatoire) et `drain:`
  (facultatif). Mettre une machine de côté, c'est ajouter une ligne `drain:`,
  sans réécrire la liste ; une liste simple (`dev: [a, b]`) est refusée.
- Avec `pool:`, `host:` est un **modèle d'adresse** : `{}` y est remplacé par la
  machine (`tcp://{}:2375`, `cyril@{}`). Sans `host:`, chaque entrée du pool est
  prise telle quelle (adresses complètes).
- **`drain:`** : la machine ne reçoit plus de **nouveaux** projets ; elle reste
  interrogée, ses projets restent visibles et s'ouvrent normalement (pour la
  vider : supprimer leurs conteneurs, ils sont replacés ailleurs à la
  prochaine ouverture). Si toutes les machines sont en drain, un nouveau projet
  est refusé (« aucun serveur disponible (tous en drain) »).
- **Charge commune** : la charge d'une machine additionne les conteneurs NG2 en
  marche de **tous les backends du pool** (ici docker-dev et ssh-dev) ; le
  choix du serveur interroge donc aussi les autres backends du pool.
- Refusés au chargement : pool inconnu ou vide, machine en double, entrée de
  `drain:` absente de `hosts:`, `pool:` avec `hosts:`, `host:` sans `{}`.
- Retirer une machine du pool alors que des projets y tournent les rend
  invisibles : la passer d'abord en `drain:`, puis la vider.
- `claude-launcher-ng2 backends` affiche le pool, les machines en drain et la
  charge venue des autres backends du pool.

#### Kubernetes

```yaml
backends:
  lab: { type: k8s, context: k8s, namespace: claude-ng2,
         volumes: { /share/MFS/ROOT: claude-shared } }   # racine partagée → PVC ReadWriteMany
```

Un projet = un **StatefulSet** (1 réplique) `<catégorie>-<projet>` ; son pod
`<catégorie>-<projet>-0` (aussi son hostname, imposé par le StatefulSet) monte chaque PVC de `volumes` **au chemin de sa racine**, donc aux mêmes
chemins que sur la machine du launcher (les racines de `volumes` tiennent lieu
de `shared`). Le pod tourne en root pour le bootstrap ; les commandes
utilisateur passent par `su` (`kubectl exec` n'a pas de `-u`). Le chemin du
projet est en annotation (`cl.project`), le listing par labels
(`cl.launcher=ng2`, `cl.backend`).

Prérequis : `kubectl` et son contexte, un namespace où l'on peut créer des
StatefulSets, des PVC ReadWriteMany liés (ex. MooseFS : PV `csi.moosefs.com`
avec `volumeAttributes.mfsSubDir`), et un namespace qui autorise les pods root.
`claude-launcher-ng2 backends` signale ce qui manque, commande à l'appui.
Droits minimaux dans le namespace : StatefulSets (et `statefulsets/scale`),
pods, `pods/exec`, events et PVC en lecture, plus `get` sur le namespace
lui-même (exemple complet : `.claude-launcher/k8s-acces-pod.sh` de ce dépôt).

**Placement des pods** (#60) : par défaut, les pods NG2 d'un backend sont
**répartis sur les nodes**. NG2 ajoute une contrainte `topologySpreadConstraints`
par node (`maxSkew: 1`, `whenUnsatisfiable: ScheduleAnyway`) : un projet de plus
que de nodes ne reste jamais bloqué en `Pending`, et la répartition reste
équilibrée (2-2-1 plutôt que 3-1-1). Le reste se règle sous `pod:`, ajouté au
spec du pod :

```yaml
backends:
  lab:
    type: k8s
    context: k8s
    namespace: claude-ng2
    volumes: { /share/MFS/ROOT: claude-shared }
    spread: true                 # défaut ; false : placement laissé au scheduler
    pod:                         # facultatif
      nodeSelector: { node-role/claude: "true" }
      tolerations: [{ key: dedie, operator: Exists }]
      resources: { limits: { memory: 4Gi } }       # appliqué au conteneur
      # affinity: …  anti-affinité stricte, si on la veut vraiment
```

Clés admises sous `pod` : `nodeSelector`, `affinity`, `tolerations`,
`topologySpreadConstraints` (ajoutées à celle de `spread`), `resources`,
`priorityClassName`, `serviceAccountName`, `automountServiceAccountToken`,
`runtimeClassName`, `schedulerName`, `nodeName`, `imagePullSecrets`, `dnsPolicy`,
`dnsConfig`, `hostAliases`, `terminationGracePeriodSeconds`, `securityContext`.
Conteneurs et volumes restent gérés par NG2 : `backends.yaml` est refusé s'il
les mentionne. `claude-launcher-ng2 backends` affiche le placement et le node
de chaque pod.

Un placement modifié **n'est jamais appliqué à un pod en marche** (le recréer
couperait sa session) : son empreinte (annotation `cl.sched`) est comparée, et
il prend effet au **prochain démarrage du pod**. Pour rééquilibrer un projet,
le fermer (Ctrl+clic) puis le rouvrir.

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

L'affectation (y compris « hôte ») est mémorisée dans `projects.json`, à côté de `backends.yaml`
(partagée en mode portable), pas dans le projet.

| Icône | Signification |
|---|---|
| `⬡` gris | `env.yaml` ou `env-template.yaml` présent, projet sur l'hôte (ou conteneur pas encore créé) |
| `⬢` cyan | conteneur en marche |
| `⬢` gris | conteneur arrêté (redémarré à l'ouverture) |

Le point devant le nom dit, lui, où en est **la session claude** (voir
[Fermer un projet](#fermer-un-projet)).

### À l'ouverture

1. Le conteneur `<catégorie>-<projet>` (hostname : `<projet>`) est créé (ou démarré), avec les
   labels `cl.launcher=ng2`, `cl.project`, `cl.backend`, `cl.image`, `cl.local` (empreinte des dossiers `local`).

   **Noms (v5.6.0)** : catégorie et projet réduits à des minuscules, chiffres et
   `-` (20 et 30 caractères au plus), par exemple `CLAUDE_LAUNCHER/Claude-Launcher`
   → conteneur ou StatefulSet `claude-launcher-claude-launcher`. Le chemin complet,
   posé à la création (label `cl.project`, annotation `cl.project` sous k8s),
   départage deux projets de même catégorie et même nom sous des racines
   différentes : le second est refusé avec un message clair (renommer la
   catégorie ou le projet). **Migration** des noms d'avant v5.6.0
   (`cl-<projet>-<empreinte>`, `clx-…`), à l'ouverture du projet : un conteneur
   en marche est renommé sans coupure, et son hostname est corrigé à sa prochaine
   ouverture une fois arrêté (recréé, les dossiers `local` repartent à vide) ; un
   StatefulSet en marche est conservé tel quel, puis recréé sous le nouveau nom à
   la première ouverture après sa fermeture.
2. **Bootstrap** (idempotent, rejoué seulement si `env.yaml` ou `env-template.yaml` change) : paquets
   système (`apt`, `dnf`, `apk`, `zypper`), utilisateur aux uid/gid de l'hôte
   avec sudo, **installation de claude**, étapes `setup`. Compter ~20 s la
   première fois (image en cache), une fraction de seconde ensuite. Si seul un
   fichier `watch` a changé : étapes `setup` seulement. Puis, pour une nouvelle
   session, les étapes `on_start`.
3. Le démarrage habituel (questions `/init-project` et reprise, claude, marqueur
   de sortie propre) s'exécute **dans le tmux du conteneur**, attaché dans la
   fenêtre du projet. Les teammates y sont créés et mis en page comme en local.

Prérequis de l'image : un gestionnaire de paquets connu et **tmux ≥ 3.2** dans
ses dépôts (Debian 12, Rocky/Alma 9, Alpine 3.16+ ; pas UBI sans abonnement
RHEL, ni RHEL 8 et ses dérivés).

### Fichiers ouverts dans le navigateur : serveur web du launcher (v5.10.0)

Le template ouvre dans Chrome (`claude-in-chrome`) des fichiers du projet : maquettes, procédures de
test, aperçus. L'extension refuse les URL `file://`. Le hub sert donc ces fichiers en HTTP, en lecture
seule, et **le chemin de l'URL est le chemin interne** :

```
Claude (session ou conteneur) : /share/MFS/ROOT/x/maquette.html
URL donnée à Chrome           : http://localhost:8765/share/MFS/ROOT/x/maquette.html
```

- Chrome tourne sur la machine du launcher. Sous WSL2, le `localhost` de Windows est redirigé vers
  WSL : il n'y a pas de `\\wsl.localhost\…` à configurer. Les projets en conteneur (podman, docker,
  k8s) sont couverts aussi, puisque le stockage partagé y est monté au même chemin que sur le hub.
- **Ne sont servies que les racines connues** : racines de projets, `shared` des backends, et
  `allow`. Le reste renvoie 404, y compris les `..` et les liens symboliques qui sortent de ces
  racines. L'en-tête `Host` est contrôlé (une page piégée qui ferait pointer son nom vers
  `127.0.0.1` est refusée), il n'y a pas de CORS, et les réponses portent `Cache-Control: no-store`
  pour qu'une maquette modifiée se recharge. Un dossier affiche son `index.html` ou, à défaut, la
  liste de ses fichiers ; `.md` et `.yaml` sont affichés en texte.
- Le serveur tourne dans le pane des cartes du hub, qui est relancé s'il s'arrête. Le réglage est
  relu toutes les 10 s, et un port occupé est réessayé.
  `claude-launcher-ng2 backends` affiche l'adresse, l'état (`actif : Claude-Launcher-NG2 v…`) et les
  racines servies.

```yaml
# ~/.config/claude-launcher-ng2/backends.yaml — tout est facultatif
defaults:
  browser_server:
    listen: 127.0.0.1:8765        # défaut ; « 9000 » = 127.0.0.1:9000 ; off = désactivé
    allow: [/data/maquettes]      # dossiers servis en plus des racines connues
hubs:
  serveur:                        # par machine du launcher : l'emporte champ par champ
    browser_server: { listen: "0.0.0.0:8765", url: "http://serveur.maison.lan:8765" }
```

`url` est la base annoncée au template. Elle est déduite de `listen` par défaut : `localhost` en
boucle locale, le nom de la machine si le serveur écoute sur toutes les interfaces. Attention :
écouter ailleurs que sur `127.0.0.1` expose les racines à tout le réseau.

La correspondance est donnée à chaque session claude dans la variable **`CL_BROWSER_PATH_MAP`**,
que le template lit (`/init-project` ne pose plus la question). Avec le serveur, elle vaut
`[{"container":"/","host":"http://localhost:8765","os":"url"}]`. Pour une entrée `os: "url"`, l'URL
est `host` + `/` + le chemin relatif au `container`, chaque segment encodé.

#### Le bon navigateur : appariement (v5.10.0)

L'extension peut être connectée à plusieurs Chrome (autres PC du même compte) ; les sessions
doivent piloter celui de la machine du launcher. Les noms affichés (« Browser 1 », « Browser 2 »)
sont renumérotés selon les connexions ; le **`deviceId`** est stable d'un lancement de Chrome à
l'autre (une 2e fenêtre du même profil garde le même ; un autre profil ou une réinstallation de
l'extension en donne un autre).

- **Détection** : seul le Chrome de la machine du launcher atteint le serveur web (`127.0.0.1`).
  La première session claude qui a besoin de Chrome essaie chaque navigateur connecté sur
  `http://localhost:8765/__claude-launcher__/pair?device=<deviceId>&token=<jeton du hub>` ; la page
  `✓ navigateur du hub <machine>` désigne le bon, et le serveur enregistre son `deviceId`
  (`~/.cache/claude-launcher-ng2/browser-device`). Le jeton, tiré une fois par hub, écarte un autre
  launcher qui écouterait sur le même port d'un autre PC. Plusieurs `✓` (plusieurs profils Chrome) :
  claude vous demande lequel, une fois.
- **Ensuite** : chaque session sélectionne directement ce `deviceId`. S'il n'est pas connecté
  (Chrome fermé), claude le signale au lieu de prendre un autre navigateur.
- **Imposer un navigateur** : `hubs.<machine>.browser_device: <deviceId>` dans `backends.yaml`.
  Refaire la détection : supprimer `~/.cache/claude-launcher-ng2/browser-device`.
- **Rien à faire côté template** : la consigne est donnée par le launcher dans `CL_BROWSER_HINT`
  (posée avec `CL_BROWSER_PATH_MAP`, dans le tmux du hub et celui des conteneurs), qu'un hook
  `SessionStart` ajouté au `settings.json` utilisateur affiche au démarrage de chaque session, et
  après `/clear` ou une compaction. Le hook est en `sh` pur (aucun binaire requis dans les
  conteneurs), n'affiche rien hors launcher, et vos autres hooks sont conservés.
- `claude-launcher-ng2 backends` affiche le `deviceId` retenu et sa source.

#### Option avancée : `browser_paths` (v5.9.0)

Pour ouvrir les fichiers par un autre chemin (partage réseau en `file://`, autre serveur web), on
déclare la correspondance ; elle l'emporte alors sur le serveur. Chrome voit ce dossier ailleurs
que Claude (`\\nas\MFS\ROOT\…`, `C:\…`, `http://…`) :

```yaml
# ~/.config/claude-launcher-ng2/backends.yaml
defaults:
  browser_paths:
    - { container: /share/MFS/ROOT, host: '\\nas\MFS\ROOT' }   # NAS vu depuis Windows
hubs:                                     # facultatif : par machine du launcher (hostname)
  portable:
    browser_paths:
      - { container: /share/MFS/ROOT, host: /mnt/mfs/ROOT }     # portable Linux
```

- `container` : préfixe absolu vu par claude. `host` : le même dossier vu par le navigateur,
  sous la forme `C:\…`, `\\serveur\partage\…` (dont `\\wsl.localhost\<distro>\…`), `/…` ou une base
  `http(s)://…`. `os` (`windows`, `linux` — macOS = `linux` — ou `url`) est facultatif : il est
  déduit de la forme de `host`.
- **Global, surchargeable par poste, pas par backend** : NG2 monte le stockage partagé au même chemin
  dans tous les backends, seul le poste où tourne Chrome change le côté `host`. `backends.yaml` étant
  commun à tous vos hubs, `hubs.<machine>` (nom de la machine du launcher, nom court accepté)
  l'emporte sur `defaults`. `browser_paths: []` : aucun navigateur ne peut ouvrir ces fichiers,
  le template affiche seulement leur chemin.
- **Sans `browser_paths`** : le serveur web ci-dessus. Avec `browser_server: { listen: off }` :
  même chemin des deux côtés pour chaque racine de projets et chaque racine partagée des backends ;
  sous WSL, `host` = `wslpath -w <racine>` et `os: windows`.
- Entrée invalide (`container` relatif, `host` d'une autre forme, `os` inconnu) : ignorée et signalée
  par `claude-launcher-ng2 backends`, qui affiche aussi la correspondance effective et sa source.
- Valeur : tableau JSON compact, préfixe `container` le plus long d'abord, sans séparateur final :
  `CL_BROWSER_PATH_MAP=[{"container":"/share/MFS/ROOT","host":"\\\\nas\\MFS\\ROOT","os":"windows"}]`.
  Elle est toujours posée par un launcher v5.9.0 ou plus récent ; son absence signale un launcher
  plus ancien.
- **Où elle est posée** : dans l'environnement du tmux du hub pour les projets sur l'hôte, et dans
  celui de la session tmux du conteneur pour les autres, dès sa création puis à chaque ouverture ou
  rattachement. Le teamleader et les teammates (panes créés dans ce tmux) en héritent. Elle ne passe
  ni par le `settings.json` commun à tous les hubs, ni par le spec du conteneur ou du pod : une
  config modifiée s'applique à la prochaine ouverture du projet, sans recréer le conteneur. Un claude
  déjà en marche garde l'ancienne valeur jusqu'à sa relance.

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
plus : le point passe à `●` orange et Entrée dans le navigateur s'y rattache. Pour
l'arrêter : **Ctrl+clic** sur le projet (voir ci-dessous).

### Fermer un projet

Le point devant le nom du projet dit où en sont son conteneur et sa session
claude, et qui la pilote ; l'icône `⬡`/`⬢` reste l'état du conteneur.

| Point | État | Entrée | Ctrl+clic / Suppr |
|---|---|---|---|
| `○` gris | rien ne tourne : projet sur l'hôte non lancé, pas de conteneur, ou conteneur arrêté | démarre le projet | rien à fermer |
| `●` bleu | conteneur en marche **sans claude** : pas de tmux, ou tmux resté seul après un `/exit` | claude démarre aussitôt, sans attendre de pod (un tmux resté seul est recréé) | arrête le conteneur, sans question |
| `▲` vert | ouvert dans ce hub, **ce launcher a la main** | va à sa fenêtre | ferme, après confirmation |
| `●` vert | ouvert dans ce hub **en lecture seule** (un autre launcher a pris la main) | rejoindre en lecture seule ou prendre la main | refusé : prendre la main d'abord |
| `■` orange | claude tourne, **piloté par un autre hub** | rejoindre en lecture seule ou prendre la main | refusé : prendre la main d'abord |
| `●` orange | claude tourne, **aucun launcher ne le pilote** (hub fermé, launcher arrêté, propriétaire silencieux depuis 20 s) | s'y rattache et en prend le contrôle | ferme, après confirmation |
| `✕` rouge | problème : backend `⚠`, configuration invalide, pod en échec, session interrompue `↺` | comme avant (reprise proposée, erreur expliquée) | abandonne la reprise et arrête le conteneur, après confirmation |

Un projet sur l'hôte ne connaît que `○`, `▲` et `✕` (sa session vit dans le
hub). Le navigateur relit toutes les 10 s chaque conteneur en marche, en une
seule commande : propriétaire de la session et commande du leader (claude ou
son shell).

La question tient dans le pied du navigateur (`Fermer web-shop ? claude + 2
teammate(s) · pod k8s arrêté (rien n'est perdu) · o/N`) ; toute autre touche
qu'`o` annule. La fermeture, ensuite :

1. `/exit` est envoyé à claude (après un Échap qui interrompt une réponse en
   cours) : sortie propre, marqueur posé, pas de `↺` ensuite ;
2. attente de la fin de claude (30 s), puis fermeture de la session tmux,
   teammates compris — forcée si claude ne répond pas ;
3. arrêt du conteneur : `podman`/`docker stop` ; k8s : StatefulSet à **0
   réplique** (le pod est libéré, tout est sur le stockage partagé). L'ouverture
   suivante le redémarre (k8s : remise à 1 réplique, ~20 s).

Un projet sur l'hôte s'arrête aux étapes 1 et 2. Un projet piloté par un autre
launcher n'est jamais fermé d'ici, ce qui couperait le travail de quelqu'un
d'autre : Entrée pour prendre la main, puis Ctrl+clic.

`CTR_STOP_ON_EXIT=1` dans `config.conf` (désactivé par défaut) arrête aussi le
conteneur quand claude se termine proprement **de lui-même** (`/exit` tapé dans
le projet) : la session du conteneur se termine et la fenêtre se ferme. Un
simple détachement, lui, n'arrête rien. Sans cette option, le projet passe au
`●` bleu.

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

### Consommation du compte : un seul cache pour tous

Le cache de la consommation du compte est dans `<config claude>/claude-launcher-cache/`
(la config claude, `CLAUDE_CONFIG_DIR`, est montée au même chemin dans tous les
conteneurs, locaux ou distants, et l'endpoint est propre au compte) :

- le **hub** y lit et écrit directement ;
- le **panneau milestone** des projets lit `~/.cache/claude-launcher/usage.json` :
  - sur la machine du hub, `usage.json` et `usage.json.err` de ce dossier sont des
    **liens** vers le cache commun (le reste du dossier n'est pas touché) ; ils sont
    posés au démarrage et rétablis à chaque relevé ;
  - dans un conteneur, le dossier entier est un lien, posé à chaque ouverture du projet.

Hubs et conteneurs partagent ainsi le cache **et** les échecs : l'endpoint n'est appelé
qu'une fois par intervalle (`USAGE_REFRESH_SEC`, 120 s par défaut) pour tout le monde.
Un projet en conteneur ouvert avant la v5.4 garde son propre cache jusqu'à sa
réouverture (détacher puis Entrée).

Après un refus pour excès d'appels (HTTP 429), plus personne n'appelle avant la fin
de l'attente : `Retry-After` s'il est donné, sinon 5 min, puis 10, 20 et 30 au plus
tant que les refus se suivent (retour à la normale au premier succès). L'heure du
prochain essai est affichée (`⚠ … trop d'appels à l'endpoint, nouvel essai vers 10:52`).
Un renouvellement du jeton par claude ne lève pas cette attente ; `F5` force un appel.
Au lancement du hub, l'échec noté et l'attente sont effacés (les dernières données restent
affichées) : le premier relevé interroge l'endpoint sans attendre, même si l'échec venait d'un
autre launcher arrêté depuis.

Le jeton est lu dans la config claude (`$CLAUDE_CONFIG_DIR/.credentials.json`), le même que
celui des sessions claude ; jusqu'à la v5.4.2, `~/.claude/.credentials.json`, qui pouvait garder
un ancien jeton refusé en 429. **Un launcher v4 encore actif** lit toujours cet ancien fichier
et écrit ses échecs dans le cache commun : l'arrêter.


- Le copier-coller OSC 52 à travers les deux niveaux de tmux n'est pas vérifié.
- La carte Team lit la version de claude du leader dans le conteneur (une fois
  par minute) ; celles des teammates viennent du nom de leur processus.

### Changement de version majeure

v4 ne connaît pas les conteneurs : son nettoyage des teams et sa reprise
peuvent prendre la session d'un conteneur pour morte. **Passer de v4 à v5 (ou
revenir) uniquement avec tous les projets fermés.**

## Windows (v6, en cours)

La v6 porte le launcher en application Windows native (epic #114). Cette branche fournit la
couche système et le cœur communs (#110) ; l'interface native (Wails + xterm.js) vient avec #111.
D'ici là, sous Windows, `backends`, `configure`, `help` et `version` fonctionnent ; les
sous-commandes de la coque tmux (`shell`…) répondent par un message.

### Ce qui tourne où

Les projets tournent **toujours dans un conteneur distant** : docker/podman en `tcp://` ou
`ssh://` (ou contexte docker), backend `ssh`, `k8s`. `host` et les moteurs locaux (socket par
défaut, `unix://`, `npipe://` de Docker Desktop) sont **refusés**, avec un message explicite dans
`claude-launcher-ng2 backends` et à l'ouverture d'un projet.

Le stockage partagé doit être visible du poste (par exemple `\\wsl.localhost\Ubuntu\share\MFS\…`) :
c'est à l'utilisateur de le rendre accessible avant de lancer le launcher, qui ne démarre pas WSL.

### Correspondance de chemins : `hubs.<machine>.paths`

Le launcher lit les projets à leur chemin Windows et transmet **toujours** aux backends le chemin
Linux : montages, workdir, labels `cl.project`, nom `<catégorie>-<projet>`, `CLAUDE_CONFIG_DIR`,
`HOME`, clés de `projects.json`, contrôle du stockage partagé. Un projet a donc les mêmes nom et
labels, qu'il soit ouvert de Windows ou d'un hub Linux : pas de doublon. Déclaration par machine
dans `backends.yaml` (commun à tous les hubs en mode portable) :

```yaml
hubs:
  MON-PC:                                  # nom de la machine Windows (hostname)
    paths:
      - { local: '\\wsl.localhost\Ubuntu\share\MFS', remote: /share/MFS }
    remote_user: { uid: 1000, gid: 1000, name: moi }   # facultatif, voir plus bas
```

- `local` : préfixe Windows (`\\serveur\…`, `C:\…`), comparé sans tenir compte de la casse ni du
  séparateur ; `remote` : préfixe Linux absolu. Le préfixe le plus long l'emporte.
- **Chaque entrée vaut pour un système, d'après la forme de `local`** : forme Windows (`\\…`,
  `C:\…`) → seulement sous Windows ; forme Linux (`/…`) → seulement hors Windows. Une
  configuration portable peut ainsi être partagée par un hub Linux sous WSL et l'exe Windows du
  même poste, qui portent **le même nom de machine** (WSL reprend le hostname Windows) : l'entrée
  `\\wsl.localhost\…` ne change rien pour le hub Linux.
- `claude-launcher-ng2 backends` affiche la correspondance ; un projet ou un `CLAUDE_CONFIG_DIR`
  hors correspondance est signalé (`aucune correspondance Linux`).
- **Identité dans les conteneurs** : Windows n'a pas d'uid. Par défaut `1000:1000`,
  `HOME=/home/<nom Windows en minuscules>` ; `remote_user` (`uid`, `gid`, `name`, `home`) les fixe
  pour qu'ils correspondent au propriétaire des fichiers sur le partage.
- Sous Linux sans déclaration, rien ne change. Un hub Linux peut aussi déclarer
  `hubs.<machine>.paths` (partage monté ailleurs qu'aux chemins des conteneurs).
- Le serveur web du navigateur sert les fichiers via cette correspondance (URL = chemin Linux,
  fichier lu à son chemin Windows) ; avec `browser_server: { listen: off }`, les `browser_paths`
  par défaut en sont déduits.

### Config, cache et `config.conf`

| | Linux | Windows |
|---|---|---|
| config (`config.conf`, `backends.yaml`, `projects.json`) | `~/.config/claude-launcher-ng2/` | `%AppData%\claude-launcher-ng2\` |
| cache | `~/.cache/claude-launcher-ng2/` | `%LocalAppData%\claude-launcher-ng2\` |

Le mode portable (`<dossier du binaire>\.config\claude-launcher-ng2\config.conf`) et
`CL_CONFIG_DIR` restent prioritaires. Sous Windows, `config.conf` n'est pas lu par bash (celui de
`System32` démarrerait WSL) mais par un lecteur intégré du même format (`NOM=valeur`, tableaux
`( … )`, `declare -A`, guillemets, `$HOME`). Comme en bash, écrire les chemins UNC entre
**guillemets simples** : `GITHUB_DIRS=('\\wsl.localhost\Ubuntu\share\MFS\ROOT\CLAUDE_PROJETS')`.
`CL_ROOTS` sépare les racines par `;`.

### Configuration partagée entre un hub Linux (WSL) et Windows

Avec le mode portable, le hub Linux sous WSL et l'exe Windows peuvent lire **le même**
dossier de config (`/share/…/.config/claude-launcher-ng2/` vu de Linux,
`\\wsl.localhost\Ubuntu\share\…\.config\claude-launcher-ng2\` vu de Windows). Elle reste écrite
en chemins Linux :

```bash
PROJECT_BASE="/share/MFS/ROOT/CLAUDE_PROJETS/PROJETS/"
GITHUB_DIRS=("$PROJECT_BASE")
CLAUDE_CONFIG_DIR="$PROJECT_BASE/../.claude"
```

- Sous Windows, un réglage-chemin de `config.conf` qui commence par `/` (`GITHUB_DIR(S)`,
  `CLAUDE_CONFIG_DIR`) est un **chemin vu des backends** : variables développées, `.`/`..`
  résolus en style Linux, puis converti par `hubs.<machine>.paths` pour tout accès local
  (parcours des projets, lecture de la config de claude) ; les backends reçoivent le chemin Linux
  d'origine. Il en va de même des chemins de `backends.yaml` (`shared`, clés de `volumes`,
  `browser_server.allow`).
- Il suffit donc d'ajouter **une ligne** au `backends.yaml` partagé, sans rien changer pour le hub
  Linux :

  ```yaml
  hubs:
    MSI:                                   # nom commun au poste Windows et à WSL
      paths:
        - { local: '\\wsl.localhost\Ubuntu\share\MFS', remote: /share/MFS }
  ```

- Sans cette correspondance, `backends` le dit **une fois** (section « Chemins poste →
  backends »), avec la liste des chemins Linux concernés et l'entrée à déclarer.
- `host` n'est pas affiché sous Windows s'il n'est pas déclaré (il y serait refusé) ; déclaré
  explicitement, il reste affiché avec son refus.
- **kubectl** : sous Windows il lit `%USERPROFILE%\.kube\config`, pas le `~/.kube/config` du hub.
  `context "k8s" does not exist` → copier le fichier du hub (depuis WSL :
  `cp ~/.kube/config /mnt/c/Users/<vous>/.kube/config`) ou définir `KUBECONFIG` vers lui.
- **Serveur web du navigateur** : WSL partage le localhost de Windows ; le serveur du hub Linux
  (`hubs.MSI.browser_server.listen`, commun aux deux) y est donc joignable. Le serveur indique son
  système (en-tête `X-Claude-Launcher-OS`) : sous Windows, un port tenu par le hub Linux est
  affiché comme tel (« port tenu par le hub linux »), et le launcher Windows ne cherche pas à le lui
  disputer. Ce serveur sert les mêmes chemins Linux que ceux des sessions : le navigateur du poste
  peut s'en servir tant que le hub tourne. Un serveur propre au launcher Windows demanderait un
  autre port, ce que la configuration partagée sous un même nom de machine ne permet pas encore
  (surcharge par système à venir).

### Prérequis : outils externes

Ils ne sont pas embarqués ; `claude-launcher-ng2 backends` liste ceux qu'exigent **les backends
déclarés**, et pour chaque outil manquant la commande à lancer :

| Outil | Requis par | Installation (Windows) |
|---|---|---|
| `ssh` | backend `ssh`, docker en `ssh://` | intégré à Windows 10/11 ; sinon `Add-WindowsCapability -Online -Name OpenSSH.Client~~~~0.0.1.0` |
| `git` | le launcher (badges, template) | `winget install --id Git.Git -e` |
| `docker` | backends docker distants (client seul, sans Docker Desktop) | `winget install --id Docker.DockerCLI -e` |
| `podman` | backends podman distants | `winget install --id RedHat.Podman -e` |
| `kubectl` | backend `k8s` | `winget install --id Kubernetes.kubectl -e` |
| `gh` | facultatif (repli par l'API GitHub) | `winget install --id GitHub.cli -e` |

### Surveillance des projets

Sous `\\wsl.localhost\…` (9P) et sur tout chemin UNC ou lecteur réseau, les notifications de
Windows ne remontent pas : les projets y sont **relus périodiquement**, comme sous Linux sur
9p/FUSE/NFS (voir [Détection des changements](#détection-des-changements-dun-projet)).

### Mise à jour automatique

Le binaire Windows de la release s'appellera `claude-launcher-ng2.exe` (#112). Un `.exe` en cours
d'exécution ne peut pas être remplacé : il est renommé `claude-launcher-ng2.exe.old`, le nouveau
est posé à sa place et lancé, puis l'ancien processus se termine ; le `.old` est supprimé au
démarrage suivant. Linux et macOS : inchangé (rename atomique, puis `execve`).

## Interface

- **Gauche** : navigateur (40 colonnes par défaut). Projets en arbre, deux badges git
  puis le badge template (`✔`, `⊘`, `⚠`), consommation Claude :
  - **local** : `●` au repos sur main · `✎` branche, non commité · `◆` branche, tout
    commité · `!` modifié directement sur main ;
  - **remote** (état du dernier fetch) : `○` jamais poussé · `↑N` non poussé · `⇡` poussé,
    non mergé dans main · `✓` à jour / mergé · `↓N` en retard.

  Le détail affiche aussi la version locale (`git describe --tags --dirty`) et la dernière
  release publiée sur GitHub, avec sa date (`v5.7.4 GitHub, il y a 2 j`) ; un dernier tag git
  d'`origin/main` différent est signalé en jaune (`· tag git v5.7.1` : tags pas récupérés, ou tag
  sans release). Sans release GitHub connue (dépôt hors GitHub, pas encore lue), le tag git seul,
  marqué `(tag git)`. La même release GitHub est affichée à côté du nom du projet dans le
  navigateur (calée à droite dans le bandeau d'un projet ouvert), si la ligne a la place après les badges.
  Le point devant le nom est l'état du projet : `○` rien ne tourne, `●` bleu conteneur prêt sans
  claude, `▲` vert ouvert ici (bandeau à la couleur de son pane tmux sur toute la largeur de la ligne, badges à droite), `●` vert lecture seule,
  `■` orange piloté par un autre hub, `●` orange personne ne pilote, `✕` rouge problème (voir
  [Fermer un projet](#fermer-un-projet)). Ctrl+clic ou Suppr ferme le projet.
  Badge milestone `⚑` (projets GitHub), calculé avec **la règle du panneau milestone des projets**
  (`skills/milestone-panel`), pour que launcher et projet ouvert soient d'accord : **jaune** = milestone
  en cours : le milestone ouvert portant une **due date** (posée par MILESTONE START, la plus ancienne) ;
  sans aucune due date (convention antérieure), le premier par version ayant une issue ouverte dans une
  phase `PRET`…`DONE` — des issues en `PLANNING` seules ne démarrent pas un milestone (v5.6.1) ;
  **orange** = le milestone daté a toutes ses issues fermées (ou, sans due date, à défaut un milestone
  ouvert dont toutes les issues sont fermées) : à publier, jusqu'à sa clôture ; **gris** = des milestones ouverts mais aucun en cours (le panneau : « Aucun milestone en
  cours ») ; **vert** = plus rien d'ouvert, dernier milestone clos (publié) ; rien sans milestone.
  Badge PR `⇄N` (violet, projets GitHub) : N pull requests ouvertes en attente (`gh pr list --state
  open`, brouillons compris ; la légende précise « dont N brouillon(s) ») ; rien sans PR ouverte.
  Les cartes de la vue `◈ État` ont leur titre sur un bandeau de fond gris foncé, sur toute la largeur
  (nom en gras, précision comme `· lu il y a 18 h` atténuée), pour que chaque carte ressorte (v5.6.2).
  La carte **Projet** de la vue `◈ État` (et l'aperçu) commence par la légende des icônes de la ligne
  du projet : chaque icône, dans sa couleur et dans l'ordre du navigateur, suivie de sa signification
  (`!   local : modifié directement sur main`, `↺   session non close (il y a 12 min) : reprise
  proposée à l'ouverture`…). Ligne et légende sont tirées de la même source (`ui/tmux/legend.go`).
  La carte **Milestone** de la vue `◈ État` (et l'aperçu) donne le milestone en cours, sa barre
  d'avancement (issues fermées / total ; elle prend la largeur du pane, de 10 à 40 cellules), ses issues ouvertes par phase et les bloquées ; sans milestone
  en cours, la liste des milestones ouverts, une ligne chacun, colonnes alignées : `v1.2.0 — Titre  n/m  ███░░░
  P%` (v5.6.2). Pane étroit : on retire dans l'ordre la barre, puis la fin du titre (`…`, la version
  reste entière), puis le pourcentage ; au plus étroit, `v1.2.0 n/m`. Quatre appels `gh` par projet (ceux du
  panneau : milestones, issues ouvertes ; plus les PR ouvertes et la dernière release) toutes les 5 min, `F5` pour forcer ; cache partagé
  `~/.cache/claude-launcher-ng2/milestones-v2.json`.
  Un projet fermé dont la dernière conversation est restée en suspens (non close par `/end-session`
  ni par une sortie propre, de moins de `RESUME_MAX_AGE_H` heures, ouverte nulle part) porte `↺` (rouge si sa team montre un
  crash) : c'est la session que son ouverture proposera de reprendre.
  Un dossier de projet créé, renommé ou supprimé hors du launcher apparaît ou disparaît sans
  `Ctrl+R` : seuls les racines et dossiers de catégorie sont surveillés (inotify sur disque Linux ;
  sur `/mnt/c`, où inotify est muet, leur date comparée toutes les 3 s, soit quelques `stat`), et
  l'arbre n'est relu qu'après un changement (`core/treewatch.go`).
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
  vignette interactive par projet ouvert). Le teamleader d'une fenêtre est son premier pane
  créé (plus petit `%id`), jamais le pane actif ni le premier par position : rouvrir un projet
  en conteneur alors qu'un teammate a le focus n'affiche plus ce teammate à la place du leader (v5.7.1).
  La vignette relit aussi toutes les 2 s si la fenêtre est en conteneur : née avant que
  l'ouverture du pod pose `@cl_backend`/`@cl_ctr`, elle ne reste plus figée sur le pane du hub
  (toute la fenêtre du conteneur, dont elle ne montrait que le bas : les teammates) (v5.7.2).

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
| clic sur `↑ NG … cliquer pour installer` (vert) | installer la nouvelle version, déjà téléchargée et contrôlée, et redémarrer le navigateur (projets conservés) |
| clic sur `↑ NG … réessayer` (jaune) | relancer tout de suite le téléchargement de la nouvelle version |
| clic sur `↑ tpl … cliquer pour installer` (vert) | adopter la nouvelle version du template |
| `Ctrl+U`, clic sur `✕` | masquer une annonce de mise à jour |
| clic sur `⚠ connexion claude.ai …` / `✗ … expirée` | ouvrir une fenêtre `login` qui lance `claude /login` |
| `F5` | rechercher les mises à jour, relire la consommation et la liste des projets maintenant (réaffiche les annonces masquées) |
| `F1` | aide : commandes et raccourcis, dans un `display-popup` tmux par-dessus la coque |
| `Esc` | effacer le filtre ; sinon quitter : `l` launcher seul (projets conservés), `t` puis `o` tout fermer |

Les commandes `Esc`, `Ctrl+C`, `F1`, `F2`, `F5`, `Ctrl+R`, `Ctrl+U`, `Tab`, `Entrée`, `Suppr` et
`Ctrl+←/→` marchent aussi dans le pane des cartes (sous le navigateur) : elles sont relayées au
navigateur, qui prend le focus pour la suite (question de fermeture après `Esc`, etc.). Les flèches,
`PgUp`/`PgDn`, `j`/`k` y font défiler les cartes ; `q` ne ferme plus ce pane.

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
qu'une release est attendue ou pas encore prête). Chaque nouvelle version du NG passe par trois
couleurs :

| Annonce | État | Clic |
|---|---|---|
| **orange** `↑ NG vX · nouvelle version en préparation` | tag poussé, release pas encore publiée (CI en cours) | sans effet |
| **jaune** `↑ NG vX · ↓ version 45 %` | release publiée (selon l'API GitHub) : le binaire est **téléchargé dans le dossier temporaire** (`$TMPDIR`, sinon `/tmp`) et contrôlé ; étapes affichées : `↓ version …%`, `↓ par l'API GitHub…` (lien direct en erreur), `↓ SHA256SUMS`, `contrôle SHA-256` | sans effet |
| **jaune** `↑ NG vX · HTTP 504 · réessayer` | publiée mais inaccessible : raison de l'échec (`HTTP 504`, `délai dépassé`, `somme incorrecte`, `réseau`) ; nouvel essai à la vérification suivante (2 min) | réessayer tout de suite |
| **vert** `↑ NG vX · cliquer pour installer` | binaire prêt et contrôlé | **copie** le binaire préparé à la place de l'actuel (après un dernier contrôle de sa somme) et redémarre le navigateur |

Sur un terminal large (≥ 70 colonnes), les étapes sont en toutes lettres
(`téléchargement de la version… 45 %`, `disponible mais inaccessible (HTTP 504) · cliquer pour
réessayer`…). Un binaire préparé déjà présent (relance du launcher) n'est pas retéléchargé,
seulement recontrôlé ; s'il a disparu ou changé au moment du clic, il est retéléchargé.
Le template garde ses deux temps : orange au tag, vert à la release.

Les fichiers sont pris par le lien direct de la release
(`github.com/…/releases/download/<tag>/…`) ; s'il répond en erreur 5xx (GitHub sert
parfois un 504 constant pour certains fichiers), un second essai est fait, puis le
fichier est pris par l'API GitHub (`releases/tags/<tag>` → `releases/assets/<id>`).
Chaque fichier reste vérifié contre `SHA256SUMS`.

- **NG** : un clic sur l'annonce verte installe la version préparée et redémarre le navigateur
  sur place (projets et zone tmux conservés) ; `✕` masque l'annonce (elle revient quand elle
  change de couleur).
- **Template** (`↑ tpl vX …`) : un clic sur l'annonce verte adopte la version : les caches
  `.template-commit` / `.template-version` (dans `~`) passent au
  tag, `init-project.md` est resynchronisé et les badges `✓`/`⚠` recalculés. Un projet en
  retard propose ensuite `/init-project` à son lancement, en affichant la version du projet et
  celle disponible (`↑ template en retard : projet v4.1.0 (e925929) → disponible v5.5.0 (1520308)`). Sans cache (première installation),
  le dernier tag est adopté d'office.
- **Version majeure commune** : à partir de la v5, launcher et template partagent leur version
  majeure (le template v5 suppose un launcher v5). NG2 ne retient que les tags du template dont
  la majeure ne dépasse pas la sienne : un template v6 ne sera jamais proposé à un NG2 v5.
- **`init-project.md`** : resynchronisé au démarrage (au commit adopté) puis à chaque adoption
  (`~/init-project.md`, copié dans `.claude/commands/` à l'ouverture d'un projet).
  Hors ligne, l'ancienne version est conservée.

## Architecture

### Structure du code (v6)

Le code est réparti en packages, pour partager avec le futur launcher Windows tout ce qui ne
dépend ni de tmux ni de l'OS :

| Package | Rôle |
|---|---|
| `main.go` | aiguillage des sous-commandes (`shell`, `nav`, `state`, `mirror`, `layout-watch`, `ctr-attach`, `backends`, `version`…) |
| `core/` | config, `backends.yaml`, moteurs et k8s, placement, pools et drain, nommage, `env.yaml`/`env-template.yaml`, bootstrap (`bootstrap.sh`), projets, teams et leader, cache d'état, conso, GitHub (releases, milestones, mises à jour), variables vers `settings.json`, `browser_paths`, serveur web et appariement, sessions tmux **dans les conteneurs** (canal de contrôle `ctl.go`, propriétaire) |
| `platform/` | ce qui dépend de l'OS : espaces de PID (`/proc`, `kill`), disque réseau (`statfs` ; Windows : UNC, `GetDriveType`), terminal en mode brut (termios), taille du terminal, verrou de fichier, relance du binaire (`execve` ; Windows : `.exe` renommé `.old`), pseudo-terminal (`pty.go` : creack/pty, ConPTY), chemin du binaire ; implémentation Linux/macOS (`_unix`, `_linux`, `_darwin`) et Windows (`_windows`), la logique restant dans des fichiers communs testés partout |
| `ui/tmux/` | la coque tmux : coque et navigateur, cartes (`◈ État`), vignettes (`mirror`, `mirrors`), mise en page (`layout`), `ctr-attach`, aide, légende, relais de touches |

`core` ne dépend ni de `ui/tmux` ni d'un appel système propre à Linux :
`GOOS=windows go vet ./core/... ./platform/...` et `GOOS=windows go build ./...` doivent rester
propres. Le workflow `ng2-tests` les vérifie à chaque PR vers `main` et `v6`, avec
`go test ./core/... ./platform/...` sur un runner `windows-latest`. Pour l'interface Windows
(#111), `core` expose notamment `CheckTools` (outils requis et commande winget),
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

## Dépannage

- **`Ctrl+←/→` ne fait rien** : votre terminal les intercepte. Glissez la bordure à la souris.
- **Ctrl+clic ne ferme pas le projet** : votre terminal le garde pour lui (ouverture de liens).
  Sélectionnez le projet et appuyez sur `Suppr`.
- **L'état d'un projet est en retard** : recalcul d'au plus ~10 s pour le projet sélectionné ;
  `Ctrl+R` relance le scan des projets.
- **Panes d'une team en désordre** : `Ctrl+B R` dans l'onglet du projet, ou
  `claude-launcher-ng2 relayout <window_id>`.
- **Quitter** : `Esc` puis `l` arrête le launcher (serveur `claude-ui-ng2`) en laissant les projets
  tourner dans `claude-hub-ng2` ; `Esc`, `t` puis `o` ferme aussi `claude-hub-ng2` et ses sessions groupées.
- **Pas de zone tmux à droite** : relancer `claude-launcher-ng2`, le pane manquant est recréé.
- **Navigateur disparu** (plantage, panic, binaire illisible ou absent après un démontage du
  stockage) : depuis la v5.8.0, le navigateur et le pane des cartes tournent dans une boucle qui
  les relance en 2 s ; la cause s'affiche au redémarrage (`⚠ relancé après arrêt (2) (SIGBUS : …)`)
  et le détail (code de sortie, début de stderr) reste dans `~/.cache/claude-launcher-ng2/nav.err.last`.
  Si le pane du navigateur a quand même été fermé, le pane des cartes le recrée sous 3 s (ou dès
  qu'une commande y est tapée). Une coque plus ancienne sans navigateur est recréée à la relance
  (`⚠ navigateur absent de la coque : coque recréée`), projets conservés. Jusqu'à la v5.7.2, la
  relance échouait sur `coque sans navigateur` : `tmux -L claude-ui-ng2 kill-server` puis relancer.
- **Stockage des projets démonté** (MooseFS, NFS… tombé) : la racine reste dans le navigateur,
  marquée `⚠ inaccessible`, et elle est scrutée toutes les 3 s ; au remontage, ses projets
  reviennent seuls (`F5` force la relecture). Jusqu'à la v5.7.2, une racine perdue n'était plus
  surveillée du tout : navigateur vide jusqu'à la relance du launcher.
  Si le binaire lui-même est sur ce stockage (mode portable), un démontage peut aussi faire tomber
  le navigateur : la relance recrée alors la coque (voir ci-dessus).
- **« mise à jour : /CLAUDE_PROJETS/claude-launcher-ng2 : open … no such file or directory »**
  (jusqu'à la v5.7.3) : le stockage du binaire a été démonté puis remonté pendant que le launcher
  tournait ; le système ne donnait plus que le chemin interne au partage. Depuis la v5.7.4, le
  chemin vu au démarrage sert de repli. Contournement : `Esc` `l`, puis relancer le launcher.
- **Une maquette ne s'ouvre pas dans Chrome** : `claude-launcher-ng2 backends` doit afficher
  `actif` pour le serveur web. `écoute impossible … address already in use` : un autre service tient
  le port, changez `browser_server.listen`. Le serveur est lancé par le pane des cartes : relancez le
  hub après une mise à jour du binaire. 404 : le fichier n'est pas sous une racine connue, ajoutez son
  dossier à `browser_server.allow`. Sous WSL, si `localhost` ne répond pas depuis Windows, vérifiez
  que `localhostForwarding` n'est pas désactivé dans `.wslconfig`.
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
