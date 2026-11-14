# Memoire projet — Claude-Launcher

> Source de verite unique au demarrage d'une session (/start-session). Mise a jour par /end-session.

## Etat courant

- **Version** : 6.1.0 publiée en PROD le 2026-11-14 (PR #157, merge be61aa8, tag v6.1.0, release GitHub avec
  binaires linux/darwin amd64+arm64 + exe Windows). Binaire du hub remplacé (ancien en
  `claude-launcher-ng2.bak-v6.0.0`, `.bak-v5.10.0` conservé). Site gh-pages v6.1.0 en ligne (PR #160).
- **Branche** : main (56c664f)
- **Travail en cours** : aucun workflow actif. Milestone #24 v6.1.0 clos (7 issues : #118 #136 #140 #144 #148 #149
  #155). Prochain : milestone #25 v6.2.0, non démarré (pas de due date, aucun label de phase) : #150 #151 #152 #153
  #154 #156 #158.
- **Issues** : fermées #149, #155 (et le milestone #24) ; créées #156 (OSC 52 Linux), #158 (Rafraîchir complet +
  Forcer la fermeture, cas ecagent dont le pod k8s a disparu) ; #153, #154, #156 rattachées à v6.2.0.
- **Cadrage produit** : docs/CADRAGE.md (si present — workshop ou brief)

## Decisions techniques de la session

- **LOT-3.3 sécurité** (audit 95/100) : jeton GitHub jamais affiché ; dans « Éditer en YAML » de config.conf il
  apparaît `GITHUB_TOKEN="__GARDER__"` (laisser = conserver, autre valeur = remplacer, `""` = retirer ; marqueur
  refusé ailleurs). EXTRA_ENVS : variables sensibles (LD_*, PATH, proxys, ANTHROPIC_*, EDITOR, SHELL, HOME…)
  refusées en saisie structurée pour les ajouts/modifications, éléments existants conservés, ajout possible par
  l'éditeur YAML. Substitution `$NOM` limitée (HOME, USER, LOGNAME, PWD, XDG_*, GITHUB_DIR, CLAUDE_CONFIG_DIR).
  Mode `--web` : jeton d'URL seulement sur la page d'amorçage puis cookie, /api/call JSON + même origine, contrôle
  Host, en-têtes de sécurité ; backendTest limité à 2.
- **LOT-4 copier/coller** (Windows seulement) : copie à la fin de la sélection + Ctrl+Maj+C (Ctrl+C = interruption) ;
  Ctrl+V / Ctrl+Maj+V / Maj+Inser / clic droit collent (Maj+clic droit si le programme suit la souris) ;
  Ctrl+B puis Ctrl+V envoie ^V brut (image dans Claude Code) ; lecture seule : copie oui, collage refusé ;
  presse-papiers Wails côté Go, `--web` = presse-papiers du navigateur, jamais celui de l'hôte ; collage ≤ 4 Mo.
- **Interfaces** : fenêtre native et `--web` ne tournent pas dans le même processus ; deux processus sur la même
  machine se croient tous deux propriétaires (pas d'issue, jugé marginal par l'utilisateur) ; macOS = coque tmux
  + navigateur, pas de fenêtre native.
- **Outil** `chrome-headless-shell` ajouté à `.claude-launcher/env.yaml` (PR #159) pour les captures d'écran.

## Points pour la prochaine session

- Relancer le hub si pas fait (Esc puis `l`).
- ecagent affiché avec une croix (pod disparu) : l'utilisateur attend #158, ne pas nettoyer à la main.
- Cadrer v6.2.0.
- Règle « maquettes » : proposer d'emblée les variantes (horizontale ET verticale) quand une mise en page est en jeu.

## Regles critiques

- Règles permanentes du projet (tests isolés, milestone non démarré, maquette, README) : `.claude/agents/context/COMMON.md`.
- Ne jamais travailler directement sur `main` : branche milestone dédiée, PR, fusion au déploiement PROD.

## Corrections de comportement

(aucune pour le moment)
