# Contrat — options tmux partagées entre launchers

> État d'une fenêtre de projet partagé par le hub Linux (`ui/tmux`) et l'interface native (`ui/desktop`).
> Les deux lisent ces options ; **seul le launcher qui a la main** (`@cl_owner`) les applique.
> Règle et lecture/écriture : `ng2/core` (un seul code pour les deux interfaces).

## Existant (rappel, inchangé)

| Option | Portée | Rôle |
|---|---|---|
| `@cl_owner`, `@cl_owner_at` | fenêtre | launcher qui a la main, battement de cœur |
| `@cl_relayout` | fenêtre | compteur de remise en page forcée (Ctrl+B R en conteneur) |

## v6.1.0 — panes cachés (#136)

### Technique

Un pane caché est **mis de côté** : `break-pane -d` le déplace dans une fenêtre d'une **session de réserve** du même
serveur tmux ; `join-pane -d` le ramène dans la fenêtre du projet. Le `%id` et le processus sont conservés ; le
teammate continue de tourner et de recevoir ses messages (essai réel du 2026-11-13).

- Session de réserve : `cl-hidden`, créée à la demande, sans client attaché ; une fenêtre par pane caché, à la
  taille de la fenêtre du projet.
- Le pane caché n'apparaît plus dans `list-panes` de la fenêtre du projet : tout recensement des panes d'un projet
  (carte TEAM, comptage à la fermeture, versions de claude) ajoute les panes de la réserve dont `@cl_home` désigne
  ce projet.
- Le pane caché survit à `kill-session` / `kill-window` du projet : la fermeture d'un projet tue aussi ses panes de
  la réserve, puis la session de réserve si elle est vide.

### `@cl_home` (option de pane) — fenêtre d'origine

Posée sur un pane au moment où il est caché : identifiant de sa fenêtre d'origine (`@N`) dans le hub ; dans le tmux
d'un conteneur (un seul projet), la valeur fixe `ctr`. Retirée au retour. C'est elle qui rattache un pane de la
réserve à son projet.

### `@cl_panevis` (option de fenêtre du projet) — choix manuels

Valeur : entrées séparées par un espace, `<pane>:<mode>`, par exemple `%7:h %9:s`.

| Mode | Sens | Posé par | Retiré quand |
|---|---|---|---|
| `h` | caché à la main | double-clic sur un inactif visible | le teammate redevient actif, ou second double-clic |
| `s` | visible fixé | double-clic sur un inactif caché | second double-clic (l'entrée passe à `h`), ou le teammate redevient actif |

- Absence d'entrée : règle automatique (caché après `TEAM_HIDE_IDLE` secondes d'inactivité ; 0 = jamais).
- Une entrée dont le pane n'existe plus est retirée par le launcher qui a la main.
- Écriture : par un launcher **qui a la main** sur le projet ; un launcher en lecture seule refuse la bascule
  (message à l'utilisateur) et n'écrit rien.
- Une bascule demandée sur un teammate actif, le leader, un agent `cdp` ou un pane sans agent est refusée.

### `@cl_idle_at` (option de pane) — début d'inactivité

Heure Unix (secondes) à laquelle le launcher qui a la main a vu le teammate passer inactif. Retirée quand il
redevient actif. Sert au délai `TEAM_HIDE_IDLE` ; suit le pane dans la réserve et reste valable si la main change
de launcher.

### Règle (ordre de priorité)

1. Leader, agent `cdp`, pane sans agent : toujours visible.
2. Teammate actif : toujours visible — ramené s'il était caché ; ses entrées `@cl_panevis` et `@cl_idle_at` sont
   retirées.
3. Teammate inactif avec `s` : visible. Avec `h` : caché.
4. Teammate inactif sans entrée : caché si `TEAM_HIDE_IDLE > 0` et inactif depuis au moins ce délai, sauf s'il a
   le focus clavier ; visible sinon.

Après chaque changement, les panes visibles sont remis en page par la règle existante (`Classify`, `ArrangeTeam`).

### Compatibilité

Un launcher antérieur à v6.1.0 qui prend la main ignore ces options : les panes cachés restent dans la réserve
(toujours vivants) jusqu'au retour d'un launcher v6.1.0, qui réapplique la règle. La fermeture du projet par un
launcher antérieur laisse les panes cachés en vie : limite connue, notée au README.
