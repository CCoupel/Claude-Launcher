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

## Fichiers dans Chrome

Le launcher sert les fichiers à `http://localhost:8081<chemin absolu>` (jamais `file://`, refusé par l'extension).
Toute maquette publiée est ouverte dans Chrome, onglet laissé ouvert.

## Release

Une release = PR fusionnée dans `main` (README à jour dans la PR même), tag `vX.Y.Z`, CI `release.yml` verte,
binaires vérifiés contre `SHA256SUMS`, binaire du hub remplacé, milestone clôturé avec le tag.
Détails : `.claude/agents/environments/deploy.prod.md`.
