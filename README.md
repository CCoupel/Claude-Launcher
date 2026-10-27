# Claude Launcher

Hub tmux pour piloter plusieurs projets Claude Code en parallèle. Compagnon de
[`claude_project_template`](https://github.com/CCoupel/claude_project_template) — mais utilisable
avec n'importe quel projet Claude Code.

**Site** : https://ccoupel.github.io/Claude-Launcher/

Le launcher est **Claude-Launcher-NG** : un binaire Go, réactif, qui a remplacé l'ancien menu fzf en v3.
Depuis la **v4**, il est entièrement en Go : la mise en page des teams et les vignettes Teamleaders, qui
passaient encore par le script bash, sont intégrées au binaire, et l'ancien menu (`--legacy`) est
supprimé. `~/claude-launcher.sh` n'est plus qu'une redirection de l'ancienne commande vers le NG.

---

## Claude-Launcher-NG

Un navigateur de projets à gauche, tmux à droite :

- **Navigateur** : vos projets en arbre par catégorie (les sous-dossiers de `GITHUB_DIRS`), filtre à la
  frappe, souris. Badges git **local** (`●` au repos sur main, `✎` en cours, `◆` tout commité, `!` modifié
  sur main) et **remote** (`○` jamais poussé, `↑N` non poussé, `⇡` non mergé, `✓` publié, `↓N` en retard),
  badge template (`✔` à jour, `⚠` en retard, `⊘` sans template), consommation du compte Claude en bas.
- **`◈ État`, sous le navigateur** : suit la sélection et détaille le projet sans l'ouvrir ni changer la
  zone de droite — git, version locale et dernière release, version du template
  (`v3.9.1 → v3.26.0`), panes tmux, fichiers avec leur marqueur git. Instantané : l'état est préparé
  en tâche de fond et rafraîchi quand le projet change.
- **Onglet `⌗ Teamleaders`** : une vignette interactive par projet ouvert, à la couleur du projet.
- **Un onglet par projet** : le teamleader et ses teammates, rangés automatiquement (voir
  [Mise en page des teams](#mise-en-page-des-teams)).
- **Création de projet** : double-clic sur une catégorie (ou « ✦ Nouveau projet ») — nom, puis type :
  **géré** (remote facultatif, `git init`, claude démarre sur `/init-project`) ou **brut** (dossier
  et `CLAUDE.md` minimal, sans git ni `/init-project`) ; le projet est ensuite lancé.
- **Mises à jour** : au lancement, le NG installe la dernière release (binaire et redirection
  `~/claude-launcher.sh`, vérifiés contre `SHA256SUMS`) ; une version sortie pendant qu'il tourne est annoncée et s'installe d'un clic. Un nouveau
  template est annoncé (masquable).
- **Connexion claude.ai surveillée** : ligne jaune 3 jours avant son expiration, rouge une fois expirée ;
  un clic ouvre `claude /login`.

| Action | Effet |
|---|---|
| `↑` `↓` `PgUp` `PgDn`, molette, clic sur un projet | sélectionner → détail sous le navigateur (un clic sur un projet ouvert bascule sur sa fenêtre) |
| `Entrée`, double-clic sur un projet | ouvrir / rejoindre le projet |
| clic hors projet (catégorie, zone vide, en-tête) | onglet `⌗ Teamleaders` |
| double-clic sur une catégorie ou une racine | créer un projet dans ce dossier |
| frappe | filtrer |
| `Ctrl+←` `Ctrl+→` ou glisser la bordure | redimensionner le navigateur (mémorisé) |
| `Tab`, `Alt+←/→` | passer du navigateur à tmux |
| `Ctrl+B R` dans un projet | remettre en ordre les panes de la team |
| double-clic sur un pane d'une team | plein écran (comme `Ctrl+B z`) ; double-clic ou `Ctrl+B z` à nouveau pour revenir en taille normale |
| clic sur `↑ NG … cliquer pour installer` (vert) | installer la nouvelle version et redémarrer le navigateur (projets conservés) |
| clic sur `↑ tpl … cliquer pour installer` (vert) | adopter la nouvelle version du template |
| `Ctrl+U`, clic sur `✕` | masquer une annonce de mise à jour |
| clic sur `⚠ connexion claude.ai …` / `✗ … expirée` | ouvrir une fenêtre `login` qui lance `claude /login` |
| `F5` | rechercher les mises à jour et relire la consommation maintenant (réaffiche les annonces masquées) |
| `F1` | aide : commandes et raccourcis, dans une fenêtre par-dessus le hub (une touche la ferme) |
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

Prérequis : `tmux`, `git`, `bash` (lecture de la config) et `claude`. Plus besoin de `fzf`, `jq` ni `xxd`.

**2. Configuration** — au premier lancement, le launcher crée `~/.config/claude-launcher.conf` :

```bash
claude-launcher-ng configure   # crée la config si besoin et l'ouvre dans $EDITOR (nano par défaut)
claude-launcher-ng help        # commandes et raccourcis (F1 dans le launcher)
```

**3. Lancement** :

```bash
claude-launcher-ng        # ou : ~/claude-launcher.sh, s'il est installé (redirige vers le NG)
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
| `LAYOUT_3ROW_LEADER_NUM`, `LAYOUT_3ROW_MID_NUM`, `LAYOUT_3ROW_DEN` | Au-delà de 5 teammates : fractions leader / ligne du milieu (défaut 3/7 et 2/7, la dernière ligne a le reste) |
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
  resynchronise depuis GitHub au démarrage puis à chaque adoption d'un nouveau template ; hors ligne,
  l'ancienne version est conservée) ; si le template du projet est en retard, `claude` démarre sur
  `/init-project` après confirmation.
- **Reprise après crash** : si le projet a une conversation de moins de `RESUME_MAX_AGE_H` heures
  (terminal fermé, crash, launcher quitté…), le launcher propose de la reprendre
  (`↺ session précédente (il y a 12 min) — la reprendre ? [O/n]`, ou `⚠ session interrompue` si sa
  team montre que le leader a disparu sans se fermer). La reprise relance `claude --resume <id>` :
  la conversation, sa team et sa liste de tâches sont retrouvées, sans question `/init-project`. Les
  teammates, eux, ne peuvent pas être repris par Claude Code : le teamleader reçoit une consigne pour
  les relancer d'après la liste de tâches. Rien n'est proposé si la conversation tourne encore ailleurs.
- Le NG se met à jour au lancement (s'il trouve une release publiée), puis vérifie GitHub toutes les
  10 minutes (2 minutes tant qu'une release est attendue, ou sur `F5`) : une nouvelle version du launcher
  ou du template est annoncée en orange dès le tag, puis en vert (cliquable pour l'installer) quand la
  release est publiée.

**Consommation du compte Claude :** sous la liste des projets, le navigateur affiche les fenêtres de quota
du compte (session, semaine, limites par modèle) avec leur réinitialisation — même source que la page
« Utilisation » de claude.ai et le milestone-panel du template. Rafraîchi toutes les `USAGE_REFRESH_SEC` s, 2 min par défaut (cache
`~/.cache/claude-launcher/usage.json`, **partagé avec les panneaux milestone des projets** : le launcher est
le seul à appeler l'endpoint ; un panneau ne fait son propre appel que si le cache manque ou a plus de 1,5 × cet intervalle,
et sa réponse fraîche sert alors au launcher et aux autres projets ; après un échec — 429, token expiré —
plus personne n’appelle pendant max(5 min, 2 × l’intervalle)) ; `F5` relit l'endpoint aussitôt,
et un nouveau `/login` lève la pause. Nécessite une connexion claude.ai (`claude` puis `/login`), sinon un
message l'indique. La connexion dure environ 30 jours : 3 jours avant son expiration, une ligne jaune
`⚠ connexion claude.ai : expiration …` l'annonce, rouge `✗ connexion claude.ai expirée` ensuite (ou si
l'endpoint refuse le jeton) ; un clic dessus ouvre une fenêtre `login` qui lance `claude /login`.
Données périmées (échec, launcher fermé) : barres grisées + horodatage.

**Mettre à jour :**
```bash
# rien à faire : le NG installe la dernière release à chaque lancement
claude-launcher-ng --no-update     # démarrer sans mise à jour (ou CL_NO_UPDATE=1)
```

---

## Mise en page des teams

Dans l'onglet d'un projet, le NG range les panes à chaque arrivée ou départ d'un teammate et à chaque
changement d'activité (d'après `~/.claude/teams/*/config.json`) :

- **en haut** : le teamleader, et les agents de type `cdp` ;
- **dessous** : les teammates, les **actifs d'abord** (le dernier à s'être mis au travail en tête),
  puis les inactifs par nom ; sur une ligne jusqu'à 5 teammates, sur deux au-delà ;
- teammates actifs **éclaircis**, inactifs **assombris**, et le titre de l'onglet s'allume tant qu'un
  agent travaille.

Pour forcer une remise en ordre : `Ctrl+B R` dans l'onglet du projet (liaison posée par le launcher dans
`claude-hub`, rien à ajouter à `~/.tmux.conf`), ou `claude-launcher-ng relayout <window_id>`.

---

## Passage à la v4

- **L'ancien menu fzf est supprimé** : `~/claude-launcher.sh --legacy`, `CLAUDE_LAUNCHER_LEGACY=1` et
  `--update` n'existent plus. `~/claude-launcher.sh` redirige vers le NG (`--configure` →
  `claude-launcher-ng configure`) ; il peut aussi être supprimé.
- Une installation v3 passe en v4 d'elle-même au lancement suivant (mise à jour automatique) ; les
  projets déjà ouverts gardent leur mise en page bash jusqu'à leur réouverture.
- `CLAUDE_LAUNCHER_NG=/chemin/claude-launcher-ng` indique à la redirection un binaire hors du `PATH`.

---

## Migration depuis claude_project_template

Le launcher vivait auparavant dans `claude_project_template`. Un `~/claude-launcher.sh` installé
depuis là-bas cherche encore ses mises à jour dans ce repo-là : il suffit d'installer le NG
(étape 1 de l'installation) et de le lancer par `claude-launcher-ng`. L'ancien script peut être
supprimé ; `~/.config/claude-launcher.conf` est repris tel quel.

---

## Utiliser un autre template

Le launcher fetche `init-project.md` depuis `claude_project_template` par défaut. Pour pointer
vers un template différent, modifier la constante `templateRepo` de `ng/update.go` et recompiler —
ce n'est pas exposé dans la config à ce jour.

---

## Relation avec claude_project_template

Ce repo ne contenait, jusqu'à récemment, que le launcher au sein de
[`claude_project_template`](https://github.com/CCoupel/claude_project_template) — il en a été
extrait pour permettre un cycle de release indépendant (le launcher est un outil tmux générique,
le template est du contenu Markdown pur : agents, commandes, contextes). L'historique complet des
commits touchant `claude-launcher.sh` a été préservé lors de la migration (le menu bash complet
reste consultable dans l'historique git, jusqu'à la v3.0.17).

`claude_project_template` reste la seule source pour `TEMPLATE_claude/` et `/init-project`.
