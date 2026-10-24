# Claude Launcher

Hub tmux pour piloter plusieurs projets Claude Code en parallèle. Compagnon de
[`claude_project_template`](https://github.com/CCoupel/claude_project_template) — mais utilisable
avec n'importe quel projet Claude Code.

**Site** : https://ccoupel.github.io/Claude-Launcher/

Depuis la v3, le launcher est **Claude-Launcher-NG** : un binaire Go, réactif, qui remplace l'ancien
menu fzf. Le script `claude-launcher.sh` reste dans ce repo : le NG s'en sert (layout des agents,
vignettes), et l'ancienne commande `~/claude-launcher.sh` **redirige vers le NG**.

---

## Claude-Launcher-NG

Un navigateur de projets à gauche, tmux à droite :

- **Navigateur** : vos projets en arbre par catégorie (les sous-dossiers de `GITHUB_DIRS`), filtre à la
  frappe, souris. Badges git (`●` à jour, `○` sans amont, `↓N` en retard sur l'amont) et template
  (`✓` à jour, `⚠` en retard), consommation du compte Claude en bas.
- **Onglet `◈ État`** : suit la sélection et détaille le projet sans l'ouvrir — git, version du template
  (`v3.9.1 → v3.26.0`), panes tmux, fichiers avec leur marqueur git. Instantané : l'état est préparé
  en tâche de fond et rafraîchi quand le projet change.
- **Onglet `⌗ Teamleaders`** : une vignette interactive par projet ouvert, à la couleur du projet.
- **Un onglet par projet** : le teamleader et ses teammates, comme avant.
- **Création de projet** : double-clic sur une catégorie (ou « ✦ Nouveau projet ») — nom, remote git
  facultatif ; le dossier est créé, initialisé (`git init`) et lancé.
- **Mises à jour signalées** : une nouvelle version du launcher ou du template est annoncée (masquable) ;
  le NG signale, il ne se met pas à jour tout seul.

| Action | Effet |
|---|---|
| `↑` `↓` `PgUp` `PgDn`, molette, clic sur un projet | sélectionner → onglet `◈ État` |
| `Entrée`, double-clic sur un projet | ouvrir / rejoindre le projet |
| clic hors projet (catégorie, zone vide, en-tête) | onglet `⌗ Teamleaders` |
| double-clic sur une catégorie ou une racine | créer un projet dans ce dossier |
| frappe | filtrer |
| `Ctrl+←` `Ctrl+→` ou glisser la bordure | redimensionner le navigateur (mémorisé) |
| `Tab`, `Alt+←/→` | passer du navigateur à tmux |
| `Ctrl+U`, clic sur `✕` | masquer une annonce de mise à jour |
| `Esc` | effacer le filtre ; sinon quitter : `l` launcher seul (projets conservés), `t` puis `o` tout fermer |

Détails, architecture et dépannage : [`ng/README.md`](ng/README.md).

---

## Installation

**1. Le NG** — un binaire statique par plateforme (linux et macOS, amd64 et arm64), publié avec chaque
release, accompagné d'un `SHA256SUMS` :

```bash
os=$(uname -s | tr A-Z a-z); arch=$(uname -m)
case "$arch" in x86_64) arch=amd64 ;; aarch64) arch=arm64 ;; esac
curl -fsSL "https://github.com/CCoupel/Claude-Launcher/releases/latest/download/claude-launcher-ng-$os-$arch" \
  -o ~/.local/bin/claude-launcher-ng && chmod +x ~/.local/bin/claude-launcher-ng
```

**2. Le script compagnon** (requis : le NG réutilise ses modes `--layout-watch` et `--mirror`) :

```bash
curl -fsSL \
  https://raw.githubusercontent.com/CCoupel/Claude-Launcher/main/claude-launcher.sh \
  -o ~/claude-launcher.sh && chmod +x ~/claude-launcher.sh
```

Prérequis : `tmux`, `git`, `bash`, `jq`, `xxd` (vignettes) et `claude`. `fzf` n'est nécessaire que
pour l'ancien menu (`--legacy`).

**3. Configuration** — au premier lancement, le launcher crée `~/.config/claude-launcher.conf`
(partagé avec l'ancien launcher) :

```bash
~/claude-launcher.sh --configure   # édite la config (GITHUB_DIR, token…)
```

**4. Lancement** :

```bash
claude-launcher-ng        # ou : ~/claude-launcher.sh (sans argument, redirige vers le NG)
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
| `EXTRA_ENVS` | Variables d'environnement supplémentaires |
| `COLOR_PALETTE`, `PROJECT_COLORS` | Couleurs de fond des projets (indices tmux 256) |
| `USAGE_REFRESH_SEC` | Secondes entre deux lectures de la consommation du compte (min. 60, défaut 120). Lue aussi par le milestone-panel (repli 5 min si absente) ; après un échec (429…), pause de max(5 min, 2 × cette valeur) |

`EXTRA_ENVS` permet de passer n'importe quelle variable d'env à Claude Code (clés d'API tierces, URLs…) :

```bash
EXTRA_ENVS=(
  "ANTHROPIC_API_KEY=sk-ant-..."
  "MY_API_URL=https://api.example.com"
)
```

**Comportements à l'ouverture d'un projet :**
- `init-project.md` est copié dans `.claude/commands/` du projet (depuis le cache, que le launcher
  resynchronise depuis GitHub au démarrage puis à chaque nouveau tag du template ; hors ligne,
  l'ancienne version est conservée) ; si le template du projet est en retard, `claude` démarre sur
  `/init-project` après confirmation.
- Le NG signale (sans l'appliquer) une nouvelle version du launcher ou du template : vérification
  GitHub au démarrage puis toutes les 10 minutes.

**Consommation du compte Claude :** sous la liste des projets, le navigateur affiche les fenêtres de quota
du compte (session, semaine, limites par modèle) avec leur réinitialisation — même source que la page
« Utilisation » de claude.ai et le milestone-panel du template. Rafraîchi toutes les `USAGE_REFRESH_SEC` s, 2 min par défaut (cache
`~/.cache/claude-launcher/usage.json`, **partagé avec les panneaux milestone des projets** : le launcher est
le seul à appeler l'endpoint ; un panneau ne fait son propre appel que si le cache manque ou a plus de 1,5 × cet intervalle,
et sa réponse fraîche sert alors au launcher et aux autres projets ; après un échec — 429, token expiré —
plus personne n’appelle pendant max(5 min, 2 × l’intervalle)) ; nécessite une connexion claude.ai (`claude` puis `/login`),
sinon un message l'indique. Données périmées (échec, launcher fermé) : barres grisées + horodatage.

**Mettre à jour :**
```bash
# le NG : relancer la commande d'installation du binaire (étape 1)
~/claude-launcher.sh --update      # le script compagnon : remplace le script, préserve la config
```

---

## Ancien menu fzf (`--legacy`)

L'ancien launcher bash est conservé :

```bash
~/claude-launcher.sh --legacy        # force l'ancien menu pour cette exécution
CLAUDE_LAUNCHER_LEGACY=1 ~/claude-launcher.sh   # idem, en permanence
```

Sans binaire `claude-launcher-ng` installé, `~/claude-launcher.sh` démarre l'ancien menu comme avant.
`CLAUDE_LAUNCHER_NG=/chemin/claude-launcher-ng` indique un binaire hors du `PATH`. Les modes internes
(`--menu`, `--mirror`, `--layout-watch`, `--do-layout`…), `--configure` et `--update` ne sont jamais
redirigés. L'ancien menu s'auto-met à jour depuis les releases ; le NG, lui, signale seulement.

---

## Migration depuis claude_project_template

Le launcher vivait auparavant dans `claude_project_template`. Si votre `~/claude-launcher.sh`
a été installé **avant** cette migration, il continue de chercher ses mises à jour là-bas — ce qui
cessera de fonctionner une fois le launcher retiré de ce repo. **Une seule commande à relancer,
une seule fois** (c'est l'étape 2 de l'installation) :

```bash
curl -fsSL \
  https://raw.githubusercontent.com/CCoupel/Claude-Launcher/main/claude-launcher.sh \
  -o ~/claude-launcher.sh && chmod +x ~/claude-launcher.sh
```

Elle remplace le script en place, votre `~/.config/claude-launcher.conf` n'est pas touché. Une fois cette
version installée, l'auto-update pointe vers ce repo.

**Comment savoir si vous êtes concerné ?**
```bash
grep -c LAUNCHER_REPO ~/claude-launcher.sh
```
`0` → ancienne version, à migrer avec la commande ci-dessus. `1` ou plus → déjà à jour.

---

## Utiliser un autre template

Le launcher fetche `init-project.md` depuis `claude_project_template` par défaut. Pour pointer
vers un template différent, éditer la constante `TEMPLATE_REPO` dans `claude-launcher.sh` — ce
n'est pas exposé via `--configure` à ce jour.

---

## Relation avec claude_project_template

Ce repo ne contenait, jusqu'à récemment, que le launcher au sein de
[`claude_project_template`](https://github.com/CCoupel/claude_project_template) — il en a été
extrait pour permettre un cycle de release indépendant (le launcher est un outil tmux générique,
le template est du contenu Markdown pur : agents, commandes, contextes). L'historique complet des
commits touchant `claude-launcher.sh` a été préservé lors de la migration.

`claude_project_template` reste la seule source pour `TEMPLATE_claude/` et `/init-project`.
