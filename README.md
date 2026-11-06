# Claude Launcher

Hub tmux pour piloter plusieurs projets Claude Code en parallèle. Compagnon de
[`claude_project_template`](https://github.com/CCoupel/claude_project_template) — mais utilisable
avec n'importe quel projet Claude Code.

**Site** : https://ccoupel.github.io/Claude-Launcher/

Le launcher est **Claude-Launcher-NG2** (v5) : un binaire Go, réactif — navigateur de projets à
gauche, tmux à droite. La v5 ajoute l'**exécution des projets dans des conteneurs** (podman, docker,
podman ou docker par ssh, Kubernetes) : un projet n'installe plus rien sur votre OS, choisit sa
distribution (Debian, Ubuntu, Rocky, Alpine…), peut tourner sur une autre machine, et sa session
survit à l'arrêt du launcher. Sans configuration, un projet tourne sur l'hôte comme avant.

> **v4 (Claude-Launcher-NG, `claude-launcher-ng`) est dépréciée.** Elle reste installable
> (voir [Revenir à la v4](#revenir-à-la-v4)) mais ne reçoit plus d'évolutions.
> **Changer de version majeure (v4 → v5, ou retour) uniquement avec tous les projets fermés.**

---

## Claude-Launcher-NG2

Un navigateur de projets à gauche, tmux à droite :

- **Navigateur** : vos projets en arbre par catégorie (les sous-dossiers de `GITHUB_DIRS`), filtre à la
  frappe, souris. Badges git **local** (`●` au repos sur main, `✎` en cours, `◆` tout commité, `!` modifié
  sur main) et **remote** (`○` jamais poussé, `↑N` non poussé, `⇡` non mergé, `✓` publié, `↓N` en retard),
  badge template (`✔` à jour, `⚠` en retard, `⊘` sans template), badge milestone GitHub (`⚑` jaune en
  cours, orange terminé mais à publier, gris aucun en cours, vert publié — même règle que le panneau
  milestone du projet), `⇄N` (violet) = N pull requests ouvertes, `↺` si une session Claude est restée
  en suspens (rouge si interrompue par un crash), **exécution** (`⬡` env.yaml / conteneur pas encore
  créé, `⬢` cyan conteneur en marche, `⬢` gris arrêté, `⚠` backend inutilisable ou configuration
  invalide), consommation du compte Claude en bas. Les dossiers ajoutés, renommés ou supprimés
  apparaissent ou disparaissent seuls.
- **`◈ État`, sous le navigateur** : suit la sélection et détaille le projet sans l'ouvrir — légende des
  icônes de sa ligne, git, versions, template, composition de la team (version du template de chaque
  teammate, modèle, effort, version de claude), fichiers.
- **Onglet `⌗ Teamleaders`** : une vignette interactive par projet ouvert, à la couleur du projet.
- **Un onglet par projet** : le teamleader et ses teammates, rangés automatiquement (voir
  [Mise en page des teams](#mise-en-page-des-teams)) — sur l'hôte ou dans le conteneur du projet.
- **Création de projet** : double-clic sur une catégorie (ou « ✦ Nouveau projet ») — nom, type
  (**géré** : `git init`, remote facultatif, claude démarre sur `/init-project` ; **brut** : dossier et
  `CLAUDE.md` minimal), puis **exécution** (hôte ou socle de conteneur).
- **Mises à jour** : au lancement, NG2 installe la dernière release (binaire vérifié contre
  `SHA256SUMS`) ; une version sortie pendant qu'il tourne est annoncée et s'installe d'un clic. Un
  nouveau template est annoncé (masquable).
- **Connexion claude.ai surveillée** : ligne jaune 3 jours avant son expiration, rouge une fois expirée ;
  un clic ouvre `claude /login`.

| Action | Effet |
|---|---|
| `↑` `↓` `PgUp` `PgDn`, molette, clic sur un projet | sélectionner → détail sous le navigateur (un clic sur un projet ouvert bascule sur sa fenêtre) |
| `Entrée`, double-clic sur un projet | ouvrir / rejoindre le projet (un projet jamais configuré demande d'abord où il tourne) |
| `F2` sur un projet fermé | reconfigurer son exécution : hôte, socle, backend |
| clic hors projet (catégorie, zone vide, en-tête) | onglet `⌗ Teamleaders` |
| double-clic sur une catégorie ou une racine | créer un projet dans ce dossier |
| frappe | filtrer |
| `Ctrl+←` `Ctrl+→` ou glisser la bordure | redimensionner le navigateur (mémorisé) |
| `Tab`, `Alt+←/→` | passer du navigateur à tmux |
| `Ctrl+B R` dans un projet | remettre en ordre les panes de la team |
| double-clic sur un pane d'une team | plein écran (comme `Ctrl+B z`) ; à nouveau pour revenir |
| dans un projet en conteneur | `Ctrl+B` + touche agit dans le conteneur ; `Ctrl+B Ctrl+B` + touche = préfixe du hub |
| clic sur `↑ NG … cliquer pour installer` (vert) | installer la nouvelle version (projets conservés) |
| clic sur `↑ tpl … cliquer pour installer` (vert) | adopter la nouvelle version du template |
| `Ctrl+U`, clic sur `✕` | masquer une annonce de mise à jour |
| clic sur `⚠ connexion claude.ai …` / `✗ … expirée` | fenêtre `login` qui lance `claude /login` |
| `F5` | rechercher les mises à jour et relire la consommation maintenant |
| `F1` | aide : commandes et raccourcis |
| `Esc` | effacer le filtre ; sinon quitter : `l` launcher seul (projets conservés), `t` puis `o` tout fermer |

Détails, architecture et dépannage : [`ng2/README.md`](ng2/README.md).

---

## Installation

**1. NG2** — un binaire statique par plateforme (linux et macOS, amd64 et arm64), publié avec chaque
release, accompagné d'un `SHA256SUMS` :

```bash
os=$(uname -s | tr A-Z a-z); arch=$(uname -m)
case "$arch" in x86_64) arch=amd64 ;; aarch64) arch=arm64 ;; esac
curl -fsSL "https://github.com/CCoupel/Claude-Launcher/releases/latest/download/claude-launcher-ng2-$os-$arch" \
  -o ~/.local/bin/claude-launcher-ng2 && chmod +x ~/.local/bin/claude-launcher-ng2
```

Prérequis : `tmux`, `git`, `bash` (lecture de la config) et `claude`. Pour les projets en conteneur,
selon le backend : `podman`, le client `docker`, `kubectl` ou `ssh` (rien à préinstaller dans les
images : le launcher y installe tmux, git, claude et les paquets du projet).

**2. Configuration** — au premier lancement, NG2 crée `~/.config/claude-launcher-ng2/config.conf`
(copie de la config v4 `~/.config/claude-launcher.conf` si elle existe). Pour un launcher portable
(binaire sur un stockage partagé), la config peut être posée **à côté du binaire**, dans
`.config/claude-launcher-ng2/` : elle est alors prioritaire.

```bash
claude-launcher-ng2 configure   # crée la config si besoin et l'ouvre dans $EDITOR (nano par défaut)
claude-launcher-ng2 backends    # backends d'exécution, conteneurs, diagnostic
claude-launcher-ng2 help        # commandes et raccourcis (F1 dans le launcher)
```

**3. Lancement** :

```bash
claude-launcher-ng2
```

**Variables disponibles dans la config :**

| Variable | Rôle |
|----------|------|
| `GITHUB_DIR` | Répertoire contenant vos projets |
| `GITHUB_DIRS` | Plusieurs dossiers racine (remplace `GITHUB_DIR`) ; les projets sont rangés par catégorie |
| `GITHUB_TOKEN` | Token GitHub (gh CLI + MCP) |
| `CLAUDE_DISABLE_MOUSE` | `1` = désactive la souris dans Claude Code |
| `CLAUDE_EXPERIMENTAL_TEAMS` | `1` = active les agent teams |
| `CLAUDE_OPTIONS` | Options passées à `claude` |
| `RESUME_MODE` | Reprise de la dernière conversation à l'ouverture d'un projet : `ask` (question, défaut), `always`, `never` |
| `RESUME_MAX_AGE_H` | Reprise proposée seulement si la conversation a moins de N heures (défaut 24) |
| `EXTRA_ENVS` | Variables d'environnement supplémentaires |
| `COLOR_PALETTE`, `PROJECT_COLORS` | Couleurs de fond des projets (indices tmux 256) |
| `PANE_ACTIVE_BRIGHTEN_STEP`, `PANE_INACTIVE_DARKEN_STEP` | Teinte des teammates actifs (éclaircis) et inactifs (assombris), 1 = léger, 5 = fort (défaut 1) |
| `LAYOUT_2ROW_LEADER_PCT` | Hauteur du teamleader (%) quand la team tient sur 2 lignes (≤ 5 teammates, défaut 50) |
| `LAYOUT_3ROW_LEADER_NUM`, `LAYOUT_3ROW_MID_NUM`, `LAYOUT_3ROW_DEN` | Au-delà de 5 teammates : fractions leader / ligne du milieu (défaut 3/7 et 2/7) |
| `USAGE_REFRESH_SEC` | Secondes entre deux lectures de la consommation du compte (min. 60, défaut 120) ; après un échec (429…), pause de max(5 min, 2 × cette valeur) |

`EXTRA_ENVS` permet de passer n'importe quelle variable d'env à Claude Code :

```bash
EXTRA_ENVS=(
  "ANTHROPIC_API_KEY=sk-ant-..."
  "MY_API_URL=https://api.example.com"
)
```

**Comportements à l'ouverture d'un projet :**
- **Projet jamais configuré** : le launcher demande d'abord où il tourne (hôte, ou un socle de
  conteneur sur un backend) ; le choix est gardé. Un projet configuré se lance **uniquement** sur son
  backend (F2 pour le changer) ; si sa configuration n'est plus valable, il est marqué `⚠` et refusé,
  jamais lancé ailleurs.
- `init-project.md` est copié dans `.claude/commands/` du projet (depuis le cache, resynchronisé depuis
  GitHub) ; si le template du projet est en retard, `claude` démarre sur `/init-project` après
  confirmation.
- **Reprise après crash** : si le projet a une conversation de moins de `RESUME_MAX_AGE_H` heures
  restée en suspens, le launcher propose de la reprendre (`claude --resume <id>`) ; le teamleader
  repris reçoit une consigne pour respawner toute sa team. Rien n'est proposé si la conversation tourne
  encore (sur l'hôte ou dans un conteneur), ni si elle a été close par `/end-session` ou quittée
  proprement (`Ctrl+C`, `/exit`). Un projet non géré par le template reçoit un `/end-session` minimal.
- NG2 se met à jour au lancement, puis vérifie GitHub toutes les 10 minutes (ou sur `F5`).

**Consommation du compte Claude :** sous la liste des projets, les fenêtres de quota du compte
(session, semaine, limites par modèle) — même source que la page « Utilisation » de claude.ai. Le cache
`~/.cache/claude-launcher/usage.json` est **partagé avec les panneaux milestone des projets** (et monté
dans les conteneurs locaux) : le launcher est le seul à appeler l'endpoint tant que ce cache est frais.

---

## Projets en conteneur

Trois fichiers :

| Fichier | Contenu |
|---|---|
| `<projet>/.claude-launcher/env.yaml` (versionné) | ce dont le projet a besoin : `image` (le socle), `packages`, `setup`, `mounts` |
| `~/.config/claude-launcher-ng2/backends.yaml` (machine) | où l'on peut l'exécuter, et les **défauts** présélectionnés (`defaults: { backend, image }`, `images:`) |
| `~/.local/state/claude-launcher-ng2/projects.json` (machine) | l'affectation de chaque projet (hôte ou backend), choisie à la création, à la 1re ouverture ou par F2 |

```yaml
# backends.yaml
backends:
  podman:      { type: podman }                                     # local
  srv1-docker: { type: docker, host: tcp://srv1:2375,  shared: [/share/MFS/ROOT] }
  srv1:        { type: ssh, host: srv1, engine: podman, shared: [/share/MFS/ROOT] }
  lab:         { type: k8s, context: k8s, namespace: claude-ng2,
                 volumes: { /share/MFS/ROOT: claude-shared } }      # PVC ReadWriteMany
defaults: { backend: podman, image: docker.io/library/debian:12 }
```

- **Socles vérifiés** : Debian 12, Ubuntu 24.04, Rocky 9 (pour RHEL), Alpine 3.20. Il faut tmux ≥ 3.2
  dans les dépôts de l'image (pas UBI sans abonnement, ni RHEL 8).
- **À distance** (docker ou podman en direct, ssh, k8s) : `~/.claude` et les projets doivent être sur
  un **stockage partagé monté aux mêmes chemins** des deux côtés (NFS, MooseFS…) ; le launcher lit
  lui-même conversations, teams et projets.
- **Un backend inutilisable** est signalé `⚠`, et `claude-launcher-ng2 backends` dit quoi corriger
  (client absent, démon non exposé, ssh, podman absent, partage non monté, namespace, PVC…).
- **Launcher arrêté** : la session du conteneur (claude et ses teammates) continue ; au relancement,
  NG2 la retrouve et rattache sa fenêtre.
- **Plusieurs launchers sur un même projet** : un seul le pilote ; les autres peuvent le rejoindre en
  lecture seule ou en prendre la main (l'ancien propriétaire passe alors en lecture seule, et reprend
  la main par Entrée sur le projet).

Détails : [`ng2/README.md` — Projets en conteneur](ng2/README.md#projets-en-conteneur-v5).

---

## Mise en page des teams

Dans l'onglet d'un projet (sur l'hôte ou dans son conteneur), NG2 range les panes à chaque arrivée ou
départ d'un teammate et à chaque changement d'activité (d'après `~/.claude/teams/*/config.json`) :

- **en haut** : le teamleader, et les agents de type `cdp` ;
- **dessous** : les teammates, les **actifs d'abord** (le dernier à s'être mis au travail en tête),
  puis les inactifs par nom ; sur une ligne jusqu'à 5 teammates, sur deux au-delà ;
- teammates actifs **éclaircis**, inactifs **assombris**, et le titre de l'onglet s'allume tant qu'un
  agent travaille.

Pour forcer une remise en ordre : `Ctrl+B R` dans l'onglet du projet, ou
`claude-launcher-ng2 relayout <window_id>`.

---

## Passage à la v5

1. **Fermer tous les projets** du launcher v4 (`Esc`, `t` puis `o`). v4 ne connaît pas les conteneurs :
   son nettoyage des teams et sa reprise pourraient prendre une session de conteneur pour morte.
2. Installer NG2 (ci-dessus) : il s'installe **à côté** de v4 (`claude-launcher-ng2`, ses propres
   sessions tmux, sa config et son cache), sans la modifier. La config v4 est copiée au premier
   lancement.
3. Lancer `claude-launcher-ng2`. Chaque projet demande une fois, à sa première ouverture, où il tourne
   (`0 hôte` pour garder le comportement de la v4).
4. Pour les conteneurs : les sessions en conteneur utilisent `CLAUDE_CONFIG_DIR=~/.claude`, donc
   `~/.claude/.claude.json`, **copié** une fois depuis `~/.claude.json` (jamais déplacé : les sessions
   de l'hôte gardent le leur). Pour un backend distant, `~/.claude` et les projets doivent passer sur
   le stockage partagé : suivre le guide pas à pas
   [**Passer ses projets et `~/.claude` sur un stockage partagé**](ng2/MIGRATION.md) (il ne suffit pas
   de copier les projets : conversations, mémoire, réglages par projet et historique sont indexés par
   leur chemin).

Le launcher v4 encore installé continue de chercher ses mises à jour : il affichera « mise à jour
incomplète » (il ne trouve pas de binaire v4 dans les releases v5) — sans conséquence ; le supprimer
(`rm ~/.local/bin/claude-launcher-ng`) une fois la v5 adoptée.

### Revenir à la v4

Avec tous les projets fermés :

```bash
curl -fsSL "https://github.com/CCoupel/Claude-Launcher/releases/download/v4.9.0/claude-launcher-ng-$os-$arch" \
  -o ~/.local/bin/claude-launcher-ng && chmod +x ~/.local/bin/claude-launcher-ng
```

---

## Migration depuis claude_project_template

Le launcher vivait auparavant dans `claude_project_template`. Un `~/claude-launcher.sh` installé
depuis là-bas cherche encore ses mises à jour dans ce repo-là : installer NG2 (étape 1 de
l'installation) et le lancer par `claude-launcher-ng2` ; l'ancien script peut être supprimé.

---

## Utiliser un autre template

Le launcher fetche `init-project.md` depuis `claude_project_template` par défaut. Pour pointer
vers un template différent, modifier la constante `templateRepo` de `ng2/update.go` et recompiler —
ce n'est pas exposé dans la config à ce jour.

---

## Relation avec claude_project_template

Ce repo ne contenait, jusqu'à récemment, que le launcher au sein de
[`claude_project_template`](https://github.com/CCoupel/claude_project_template) — il en a été
extrait pour permettre un cycle de release indépendant (le launcher est un outil tmux générique,
le template est du contenu Markdown pur : agents, commandes, contextes). L'historique complet des
commits touchant `claude-launcher.sh` a été préservé lors de la migration (le menu bash complet
reste consultable dans l'historique git, jusqu'à la v3.0.17 ; NG v4 dans `ng/`).

`claude_project_template` reste la seule source pour `TEMPLATE_claude/` et `/init-project`.
