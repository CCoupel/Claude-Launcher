# Claude Launcher

Hub tmux pour piloter plusieurs projets Claude Code en parallèle. Compagnon de
[`claude_project_template`](https://github.com/CCoupel/claude_project_template) — mais utilisable
avec n'importe quel projet Claude Code.

---

## Installation

```bash
curl -fsSL \
  https://raw.githubusercontent.com/CCoupel/Claude-Launcher/main/claude-launcher.sh \
  -o ~/claude-launcher.sh && chmod +x ~/claude-launcher.sh
```

Au premier lancement, le launcher crée `~/.config/claude-launcher.conf` :

```bash
~/claude-launcher.sh --configure   # édite la config (GITHUB_DIR, token…)
~/claude-launcher.sh               # démarre le hub tmux
```

**Variables disponibles dans la config :**

| Variable | Rôle |
|----------|------|
| `GITHUB_DIR` | Répertoire contenant vos projets |
| `GITHUB_TOKEN` | Token GitHub (gh CLI + MCP) |
| `CLAUDE_DISABLE_MOUSE` | `1` = désactive la souris |
| `CLAUDE_EXPERIMENTAL_TEAMS` | `1` = active les agent teams |
| `CLAUDE_OPTIONS` | Options passées à `claude` |
| `EXTRA_ENVS` | Variables d'environnement supplémentaires |

`EXTRA_ENVS` permet de passer n'importe quelle variable d'env à Claude Code (clés d'API tierces, URLs…) :

```bash
EXTRA_ENVS=(
  "ANTHROPIC_API_KEY=sk-ant-..."
  "MY_API_URL=https://api.example.com"
)
```

**Comportements automatiques à l'ouverture d'un projet :**
- Le launcher vérifie le dernier tag GitHub et se met à jour silencieusement si une nouvelle version est disponible
- `init-project.md` est rafraîchi depuis GitHub (`claude_project_template`) à chaque ouverture de projet
- Si GitHub est inaccessible et que le fichier existait déjà, l'ancienne version est conservée

**Mettre à jour le launcher :**
```bash
~/claude-launcher.sh --update      # remplace le script, préserve la config
```

---

## Utiliser un autre template

Le launcher fetche `init-project.md` depuis `claude_project_template` par défaut. Pour pointer
vers un template différent, éditer la constante `TEMPLATE_REPO` dans `claude-launcher.sh` (ou via
la config, selon la version — voir `--configure`).

---

## Relation avec claude_project_template

Ce repo ne contenait, jusqu'à récemment, que le launcher au sein de
[`claude_project_template`](https://github.com/CCoupel/claude_project_template) — il en a été
extrait pour permettre un cycle de release indépendant (le launcher est un outil tmux générique,
le template est du contenu Markdown pur : agents, commandes, contextes). L'historique complet des
commits touchant `claude-launcher.sh` a été préservé lors de la migration.

`claude_project_template` reste la seule source pour `TEMPLATE_claude/` et `/init-project`.
