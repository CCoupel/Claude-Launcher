# Installation et lancement

> Autres guides : [Configuration et projets en conteneur](configuration.md) · [Launcher Windows natif](windows.md) · [Déploiement dans un cluster Kubernetes](kubernetes.md) · [Interface](interface.md) · [Versions, mises à jour et template](versions.md) · [Architecture](architecture.md) · [Dépannage](depannage.md) · [Tests](tests.md). Vue d'ensemble : [README](../README.md).

**Sommaire**

- [Installation](#installation)
  - [Depuis une release](#depuis-une-release)
  - [Depuis les sources](#depuis-les-sources)
- [Lancement](#lancement)
  - [Reprise après crash](#reprise-après-crash)

---

## Installation

### Depuis une release

Chaque tag `v5.*` (et suivants) publie un binaire par plateforme (linux/darwin,
amd64/arm64), depuis la v6 `claude-launcher-ng2.exe` (windows/amd64, interface
native), et un seul `SHA256SUMS` :

```bash
os=$(uname -s | tr A-Z a-z); arch=$(uname -m)
case "$arch" in x86_64) arch=amd64 ;; aarch64) arch=arm64 ;; esac
curl -fsSL "https://github.com/CCoupel/Claude-Launcher/releases/latest/download/claude-launcher-ng2-$os-$arch" \
  -o ~/.local/bin/claude-launcher-ng2 && chmod +x ~/.local/bin/claude-launcher-ng2
claude-launcher-ng2 version
```

Windows : télécharger `claude-launcher-ng2.exe` de la même release, dans un dossier où l'on peut
écrire ; installation pas à pas (WebView2, outils, `backends.yaml`) dans le
[README du repo — Windows](../../README.md#windows).

### Depuis les sources

```bash
cd ng2
go build -o ~/.local/bin/claude-launcher-ng2 .
# version : go build -ldflags "-X claude-launcher-ng2/core.NgVersion=v6.0.0" -o ... .
```

Prérequis : `tmux`, `git`, `bash` (lecture de la config) et `claude`. Pour les projets en
conteneur, en plus : le client du moteur (`podman`, `docker`, `kubectl`) ou `ssh`, selon le
backend ; rien n'est à préinstaller dans les images. L'exe Windows : voir
[Construire l'exécutable](windows.md#interface-native-desktop).

## Lancement

```bash
claude-launcher-ng2                    # = claude-launcher-ng2 shell
claude-launcher-ng2 desktop            # interface native, dans le navigateur sous Linux (v6, voir Windows)
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
# /share/MFS/ROOT/CLAUDE_PROJETS/.config/claude-launcher-ng2/config.conf   (exemple de racine)
PROJECT_BASE="/share/MFS/ROOT/CLAUDE_PROJETS/PROJETS/"
GITHUB_DIRS=("$PROJECT_BASE")
CLAUDE_CONFIG_DIR="../../.claude"          # = /share/MFS/ROOT/CLAUDE_PROJETS/.claude
```

**Racine de base** (v6.3.0, #165). `/share/MFS/ROOT/CLAUDE_PROJETS` n'est qu'un exemple : aucun chemin n'est supposé.
Pour une autre racine, on passe `--root-base <dossier>` (ou `CL_ROOT_BASE=<dossier>`) ; l'option l'emporte sur la
variable. Elle est reportée dans `CL_ROOT_BASE` du processus, donc conservée à chaque relance, et transmise aux
sous-commandes lancées par tmux (`nav`, `state`, `mirror`…).

Dossier de configuration, par ordre de priorité :

1. `CL_CONFIG_DIR` ;
2. `<racine>/.config/claude-launcher-ng2`, retenu même si `config.conf` n'y existe pas encore ;
3. `.config/claude-launcher-ng2` à côté du binaire, s'il contient un `config.conf` (portable, inchangé) ;
4. le dossier personnel (inchangé).

Sous la racine, le launcher place donc sa configuration (`.config/claude-launcher-ng2` : `config.conf`,
`backends.yaml`, `projects.json`), la config de claude (`.claude`) et les projets (`PROJETS`, proposé à la création de
`config.conf`). `CLAUDE_CONFIG_DIR` vaut `<racine>/.claude` tant que `config.conf` ne le pose pas, et une valeur déjà
définie au lancement l'emporte.

- Racine absente, qui n'est pas un dossier ou qui est illisible : erreur qui nomme le chemin et sa source (`CL_ROOT_BASE`
  ou `--root-base`), code de sortie non nul, rien n'est créé.
- Option sans valeur (`--root-base`, `--root-base=`) : erreur explicite, la variable n'est pas effacée en silence.
- `claude-launcher-ng2 backends` affiche en tête la section « Racine de base » : racine et sa source, configuration,
  cache, config de claude, projets.
- Avec `--root-base` seul, le cache (jeton, état, journaux) reste sous le dossier personnel `~/.cache/claude-launcher-ng2`
  (`$XDG_CACHE_HOME` ou `~/.cache`). C'est un état propre à chaque launcher, jamais partagé : seul `CL_CACHE_DIR` le
  déplace. La ligne « cache » de la section « Racine de base » indique sa source (`CL_CACHE_DIR` ou « défaut »).

**Identité et cache d'un launcher** (v6.3.0, #169) :

- `CL_HUB_NAME` donne au launcher un nom à la place de celui de la machine. Ce nom sert de clé `hubs.<nom>` dans
  `backends.yaml`, d'identité de propriétaire des sessions (`LauncherID`) et d'hôte admis des serveurs web. Il doit
  respecter `^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$` ; une valeur invalide arrête le démarrage, rien n'est créé.
- Le nom réel de la machine reste celui des espaces de PID des teams, écrits par Claude Code : `CL_HUB_NAME` ne change
  que l'identité du launcher.
- `CL_CACHE_DIR` est le dossier de cache, utilisé tel quel, et il est créé s'il manque. Il contient les jetons, les
  préférences, le navigateur apparié, l'état par projet, le cache des milestones (`milestones-v2.json`) et le témoin de
  présence. Convention du chart : `CL_CACHE_DIR=<rootBase>/.cache/claude-launcher-ng2/<hubName>`.

**Plusieurs launchers sur une même racine** (v6.3.0, #176). Ce qui est **commun** : la racine, `projects.json`, le
`settings.json` de `CLAUDE_CONFIG_DIR`, l'`env.yaml` des projets. Ce qui est **propre** à chaque launcher : le cache,
le témoin de présence et l'identité.

- **Garde de nom** : avec `CL_HUB_NAME` posé, un témoin `hub-<nom>.presence` est tenu dans `CL_CACHE_DIR`, renouvelé
  toutes les 3 s. Un second launcher de même nom refuse de démarrer (`desktop`), avec un message qui nomme le launcher
  déjà actif, code 1. Le témoin est repris après 10 s, ou tout de suite si son processus est mort sur la même machine.
  Sans `CL_HUB_NAME`, aucun contrôle : deux launchers sans nom sur le même état démarrent tous deux, comme en v6.2.0.
  Deux launchers qui partagent une racine doivent donc porter des noms différents.
- **Un seul pilote** : les écritures partagées (`projects.json`, `settings.json`, `env.yaml`) se font sous un verrou
  court (`<fichier>.lock`, création exclusive, repris après 20 s), puis en écriture atomique (fichier temporaire puis
  renommage). Un verrou périmé (60 s) est repris sous un second verrou (`<fichier>.reclaim`). Les écarts d'horloge entre
  launchers doivent rester très inférieurs à 60 s, car la péremption se mesure sur la date du fichier.
- **`settings.json`** : chaque launcher tient la liste des clés qu'il a posées dans son propre fichier
  (`claude-launcher/settings-env-<nom>.json` avec `CL_HUB_NAME`, `settings-env.json` sans). Il ne retire que ses clés.
  Si deux launchers posent la même clé avec des valeurs différentes, **le dernier écrivain gagne**.

**Variables de cluster** (v6.3.0, #168, #169) : `CL_IN_CLUSTER=1` ou `0` force la détection du cluster (absente :
automatique, d'après `KUBERNETES_SERVICE_HOST` et le jeton du ServiceAccount). `CL_ALLOW_HOST_BACKEND=1` autorise le
backend `host` dans un launcher en cluster, où il est refusé par défaut (claude n'y est pas installé). Les tests fixent
`CL_IN_CLUSTER=0`, car le conteneur de développement est lui-même un pod. Le détail est dans `contracts/launcher-env.md`.

La configuration est `config.conf` (relue par bash : `GITHUB_DIRS`,
`CLAUDE_OPTIONS`, `RESUME_MODE`, `CTR_STOP_ON_EXIT`, `TEAM_HIDE_IDLE`, `COLOR_PALETTE`, `USAGE_REFRESH_SEC`, `LAYOUT_*`, `PANE_*_STEP`…), créée au
premier lancement : **copie de la config v4** (`~/.config/claude-launcher.conf`) si elle existe, sinon les
valeurs par défaut. Les backends d'exécution sont dans `~/.config/claude-launcher-ng2/backends.yaml`
(voir [Projets en conteneur](configuration.md#projets-en-conteneur-v5)).

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
