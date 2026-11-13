# Memoire projet — Claude-Launcher

> Source de verite unique au demarrage d'une session (/start-session). Mise a jour par /end-session.

## Etat courant

- **Version** : 6.0.0
- **Branche** : main
- **Travail en cours** : milestone v6.1.0 « Configuration et panes repliables » (#24) planifié, non démarré (pas de due date) — à requalifier avec le workflow du template
- **Issues actives** : #118 (UI de configuration Windows), #136 (panes cachés), #140 (taille des caractères des terminaux), #144 (nettoyage TestShellWithoutNav) — décisions de l'utilisateur dans les commentaires « Décisions GATE 2 » des issues
- **Cadrage produit** : docs/CADRAGE.md (si present — workshop ou brief)

## Regles critiques

- Tests : jamais de vrais projets, jamais de vrai `claude`, tmux isolés (`.claude/agents/context/COMMON.md`).
- Milestone non démarré (pas de due date) : ses issues ne portent aucun label de phase.
- Toute maquette est validée par l'utilisateur, ouverte dans Chrome via `http://localhost:8081<chemin>`, avant le développement.
- README à jour dans la PR même, avant fusion et release.

## Corrections de comportement

(aucune pour le moment)
