# Contrat — version du template : comparaison, vérification GitHub, affichage (v6.4.0, #199)

> Base : `_work/reports/planner-analyse-version-template-20261117-155050.md` (repris dans l'issue #199).
> Concerne la coque Linux (tmux), l'interface native et l'interface web (code commun `core/`).

## Versions comparées

| Nom | Source | Rôle |
|-----|--------|------|
| version du projet | `TEMPLATE_claude/.template-source.json` ou `.claude/.template-source.json` (`tag`), à défaut `template_version.tag` de `.claude/project-config.json` | ce que le projet a réellement |
| version adoptée | `~/.template-version` / `~/.template-commit` (dossier de `CL_SCRIPT` ou `~/claude-launcher.sh`) | ce que le launcher installe par `/init-project` ; **ne change qu'au clic** sur l'annonce verte (décision utilisateur) |
| dernière publiée connue | `tag` du résultat de vérification partagé (ci-dessous), à défaut la version adoptée | sert à savoir si une mise à jour existe |
| version d'un agent | `agents.registry[].tag` de `project-config.json` | ligne de la carte TEAM |

**Règle d'ordre** : toutes les comparaisons passent par `VersionNewer(a, b)` (b strictement plus récent que a),
jamais par l'égalité de commits ou de tags, sauf quand un tag manque (repli sur l'égalité de commits, comportement
actuel). `verParts` lit au plus 4 champs `X.Y.Z.a` et compare `X.Y.Z` (le 4e est ignoré) : `5.11.1.3` donne
`[5,11,1]`.

## État du template d'un projet (`ReadTemplate`)

| Condition | État |
|-----------|------|
| pas de `.template-source.json` (règles actuelles) | `TplNone` / `TplMissing` inchangés |
| version adoptée **strictement plus récente** que la version du projet | `TplOutdated` (badge ⚠, question `/init-project` au lancement) |
| égale, ou projet **plus récent** que la version adoptée | `TplOK` — jamais de question ni de proposition d'un retour en arrière |
| tag manquant d'un côté | égalité des commits (v6.3.0) |

La question au lancement (`core/start.go`) ne propose jamais une cible égale ou plus ancienne que la version du projet.

## Carte TEAM (`cards.teamHead`, `team[]`) **[CHANGED]**

- `teamHead.tpl` = **version du projet** (était : version adoptée).
- `teamHead.tplNewer` **[NEW]** = dernière publiée connue si elle est strictement plus récente que `teamHead.tpl`,
  sinon `""`. L'interface affiche un ⚠ orange après « tpl vX » quand `tplNewer` n'est pas vide, infobulle
  « template du projet vX — vY disponible (/init-project au lancement) ».
- `team[].odd` contient `"tpl"` quand le tag de l'agent diffère de `teamHead.tpl` (version du projet).
- `team[].tplState` : `late` si `VersionNewer(tag, teamHead.tpl)`, `ok` si égal, neutre sinon (agent plus récent que
  le projet : neutre).
- `team[].tplTarget` = `teamHead.tpl`.
- Coque Linux : même calcul (`core.ApplyTeamHead`), même affichage dans le titre de la carte.

## Annonce d'un nouveau template (`CheckUpdatesNow`) **[CHANGED]**

- `TplTag` n'est rempli que si le dernier tag (≤ majeure du launcher) est **strictement plus récent** que la version
  adoptée. Adoption : au clic seulement (inchangé, sauf première installation sans cache).

## Vérification GitHub **[CHANGED]**

1. **Jeton** : appels `api.github.com` authentifiés quand un jeton est disponible, par priorité `GH_TOKEN` >
   `GITHUB_TOKEN` (environnement) > `GITHUB_TOKEN` de `config.conf`. Jamais journalisé. Sans jeton : appels
   anonymes (v6.3.0).
2. **Requêtes conditionnelles** : `If-None-Match` avec l'ETag mémorisé ; un `304` réutilise la réponse mémorisée.
3. **Résultat partagé par poste** : `<CacheDir>/update-check.json`, écrit de façon atomique :
   ```json
   { "checked_at": "2026-11-17T16:20:00Z", "launcher": {"tag": "v6.4.0", "etag": "…", "released": true},
     "template": {"tag": "v5.11.1", "sha": "4cf4bbd…", "etag": "…", "released": true},
     "error": "", "retry_after": "" }
   ```
   Une instance qui trouve un résultat de moins de 10 min l'utilise sans appeler GitHub (F5 force l'appel).
4. **Quota** : `403` ou `429` avec `X-RateLimit-Remaining: 0` → `error: "quota"`, `retry_after` = `X-RateLimit-Reset`.
   Aucun nouvel appel avant cette heure (F5 compris : message immédiat). Alerte visible en continu (pas seulement
   après F5) : « ⚠ vérification GitHub impossible (quota, reprise HH:MM) » — même zone que les annonces de mise
   à jour, orange, non cliquable. Autre échec : « ⚠ GitHub injoignable », visible aussi hors F5.
5. `UpdMsg` gagne `Err` (`""` | `"quota"` | `"network"`) et `RetryAt` (heure) ; `Ok` garde son sens.

## Tests (règle)

Tout test qui touche `TplVersionPath()` / `TplCommitPath()` règle `CL_SCRIPT` vers un dossier temporaire : aucun
test n'écrit dans le vrai `~`.
