# Adaptations projet — Claude-Launcher

## Tests : jamais de vrais projets ni de vrai `claude`

Les tests du launcher n'utilisent **que** des projets factices, un faux `claude` et des serveurs tmux isolés.
Ils ne touchent jamais les vrais projets, le vrai `claude`, ni la session `claude-hub` réelle.

- Variables de test : `CL_ROOTS` (racines factices), `CL_CLAUDE_BIN` (faux claude nommé `claude`), `CL_HUB`,
  `CL_INNER_SOCKET`, `CL_OUTER_SOCKET`, `CL_SCRIPT`, `CL_RAW_BASE`, `CL_FAKE_LATEST_TPL`, `CL_FAKE_LATEST_LAUNCHER`,
  `XDG_CACHE_HOME`. Garder les caches factices « frais » (< 3 min), sinon le launcher relit l'API d'usage réelle.
- Chaque test qui démarre un serveur tmux déclare son arrêt en dernier `t.Cleanup` (processus des panes puis serveur)
  et vérifie qu'aucun processus ne survit.
- Masquer tout `ghp_…` dans une capture d'écran.
- Pourquoi : des tests lancés sur de vrais projets ont démarré un vrai `claude --dangerously-skip-permissions` et
  modifié des fichiers suivis par git dans deux dépôts réels.

## Push depuis le conteneur

`git -c credential.helper='!gh auth git-credential' push …` (le dépôt est en HTTPS, l'authentification passe par `gh`).

## Règles permanentes du projet

- **Milestone non démarré** (pas de due date) : ses issues ne portent aucun label de phase ; une issue reportée vers
  un milestone futur perd le sien (sinon le panneau milestone le croit démarré).
- **Maquette avant le développement** : toute nouvelle interface passe par une maquette validée par l'utilisateur,
  ouverte dans Chrome par le serveur web du launcher (`commands/context/MOCKUPS.md` §14.9), onglet laissé ouvert.
- **Fichiers ouverts dans Chrome** (maquettes, plans de test HTML) : toujours sous `_work/mockup/` — le `/files` du
  launcher ne sert que ce dossier pour `_work/`.
- **README dans la PR même** : le README est mis à jour dans la PR qui change le comportement, avant fusion et release.
- **Mesure Windows réelle** : ce qui est mesuré sous Linux (latence, I/O) se vérifie sur l'exe Windows réel
  (wsl.exe, partage 9P) avant de conclure — trois refus du GATE LOT-5 (v6.5.0) venaient de mesures simulées.
- **Issue rouverte après validation** (retours utilisateur) : elle s'intègre au lot en cours ; sa maquette est refaite
  d'abord.
- **Concision** : messages `DONE` au teamleader en 2 lignes au plus, le détail va dans le rapport `_work/reports/`.

## Release

Une release = PR fusionnée dans `main` (README à jour dans la PR même), tag `vX.Y.Z`, CI `release.yml` verte,
binaires vérifiés contre `SHA256SUMS`, binaire du hub remplacé, milestone clôturé avec le tag.
Détails : `.claude/agents/environments/deploy.prod.md`.
