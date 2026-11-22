# Memoire projet — Claude-Launcher

> Source de verite unique au demarrage d'une session (/start-session). Mise a jour par /end-session.

## Etat courant

- **Version** : **6.6.0 publiée en PROD le 2026-11-22** — tag `v6.6.0` sur b3ec811, release
  https://github.com/CCoupel/Claude-Launcher/releases/tag/v6.6.0. Milestone #30 « v6.6.0 — Gestion des projets » clos
  (11 issues : #214–#219, #224, #225–#227). Notes de version : release + PR #230. Site gh-pages v6.6.0 : PR #229
  (https://ccoupel.github.io/Claude-Launcher/).
- **Branche** : main.
- **Environnements** :
  - PROD : namespace `claude-ng2`, helm révision 4, pod v6.6.0.
  - QUALIF : `claude-qualif`, dernière valeur connue 6.5.0.5 (non mise à jour dans cette session).
  - Hub : `/share/MFS/ROOT/CLAUDE_PROJETS/claude-launcher-ng2` + `.exe` en v6.6.0 (sauvegarde `claude-launcher-ng2.bak-v6.5.0`).
  - CA interne cert-manager `claude-launcher-ca` (nameConstraints `launcher.local` / `launcher-qualif.local`).
  - Kubeconfig Rancher ADMIN dans `PRIVATE/{QUALIF,PROD}/kubeconfig` (risque accepté).
- **Travail en cours** : aucun milestone en développement ; branche main.

## Decisions de la session (v6.6.0)

- **#215 trois sources** : « Projet brut » (git init + remote si fourni), « Template projet » (git init + /init-project
  proposé), « Dépôt Git » ; option « géré » supprimée.
- **Déplacer / renommer, backend / image et env.yaml** refusés si le projet est ouvert ou son conteneur actif ; backend
  injoignable : env.yaml autorisé avec avertissement, backend / image / déplacement fail-closed.
- **#225** : config.conf relu à chaud ; Roots et CLAUDE_CONFIG_DIR exigent toujours une relance.
- **#226** : pastille d'effet par option + bulle ; note conservée pour backends.yaml / hub / env.yaml.
- **#227** : œil commun à tous les éléments cachés : révélation refusée en accès distant, 30 s, 10/min, journal sans valeur
  ni empreinte ; `kubectl port-forward` = accès local.
- **Tests** : garde d'isolation (`CL_TEST_ISOLATION`, écritures limitées à `os.TempDir`), jetons `ghp_FAKE_TEST_…`,
  faux podman/docker par défaut dans ui/desktop et ui/tmux.
- **Process** : validation du dernier lot = feu vert PROD (PUBLISH + DEPLOY sans GATE 4 ni relecture de PR) ;
  `publish.prod` exige une section `## [X.Y.Z] — date` dans CHANGELOG avant le merge.

## Decisions techniques de la session (v6.5.0)

- 5 lots : LOT-1 CI/tests fiables (#204 contrôle Chart.yaml = version du milestone dans `scripts/ci.sh` ; #203/#207/#208 ;
  `stopNative` attend `App.bg`/`waitBackground`) ; LOT-2 #201 ; LOT-3 #205 (balayage `^[0-9a-f]{16}\.json\.[0-9]+\.tmp$`
  > 24 h) et #206 (drainClose : goroutine de fond jusqu'au retour de Close) ; LOT-4/LOT-5 #209 ; LOT-5 #210.
- **#201 coffre des secrets PARTAGÉ** (exigence utilisateur, BREAKING) : un seul coffre en clair `0700/0600` sous
  `<CL_ROOT_BASE>/.config/claude-launcher-ng2/secrets/` pour hub, exe et pod (risque du stockage en clair accepté) ;
  secrets propres à un projet → `<projet>/PRIVATE/` (jamais écrits par le launcher) ; migration auto des coffres v6.4.0
  avec conservation de l'ancien (`MigrateLegacySecrets`) ; verrou `.vault.lock` ; liens pendants résolus ; `/files`
  n'expose ni le coffre ni `PRIVATE/` ; chart : plus de `CL_SECRETS_DIR`, ancien PVC monté en lecture seule
  (`secrets.legacy.enabled`, `CL_SECRETS_LEGACY_DIR`). Audit security reporté à v7.0.0 (risque accepté). En PROD et
  QUALIF l'ancien PVC était vide (0 backend migré).
- **#209** : réglage par projet `PROJECT_HIDE_IDLE` (auto/none/default) ; carte TEAM = œil à 3 états aligné à droite du
  titre (plus de puce « masquage N s »), fond plein (« bouton enfoncé ») = choix propre au projet, gris nu = default ;
  clic = cycle, clic droit = menu ; coque Linux « default (masqués 30 s) » / « auto hide ◆ » / « no hide ◆ » ;
  `projectApply` invalide `teamCache`, `applyNow`, `pushTeam`. Maquette
  `docs/mockup/v6.5.0/ui/team_card__masquage_par_projet_v2.html` (v1 obsolète).
- **#210** : émetteur de cartes à drapeau « relire encore » par App (plus de `cardsMu.TryLock` global).
- Tests : garde-fou `testguard` (HOME/APPDATA/LOCALAPPDATA temporaires, échec si `~/.config/claude-launcher*` change).
- Outil : `netavark` + `aardvark-dns` ajoutés à `.claude-launcher/env.yaml` (build d'image podman de `ci.sh`).
- Site : installation en sous-onglets Linux/Windows/Docker/k8s, « Nouveautés » = version courante seule, anciennes
  nouveautés dans Fonctionnalités avec chips Linux/Windows/Web/k8s (62/62 vérifiées dans le code), matrice
  fonctionnalités × plateformes (#matrix).
- `helm upgrade` : `--reset-then-reuse-values` ou `-f`, jamais `--reuse-values` seul.
- **Leçon release** : `Chart.yaml` doit suivre la version du milestone (contrôle automatisé par #204).

### Décisions antérieures (condensées)

- **v6.4.0** « Backends distants et confort » : #199 version template (⚠ si plus récente) ; #194/#193 clés ssh et
  kubeconfig par backend (+ `rbac.extraNamespaces`) ; #189–191 onglets ; #198 bandeau MAJ client web ; #174
  `/files/<catégorie>-<projet>/…` ; #200 NetworkPolicy par défaut. Pas d'authentification (OAuth #172 → v7.0.0),
  ingress interne seul. Arrêt du launcher borné à 25 s. `ng2/README.md` découpé en guides `ng2/docs/`.
- **v6.3.0** : accès web sans jeton en cluster (`web.access: none`) ; contexte k8s ignoré en cluster ; variables `CL_*`
  sur le StatefulSet ; #188 masquage désactivé = tout réapparaît.
- **v6.2.0** : Ctrl+molette par zone ; `scripts/ci.sh` isole tmux sous `/tmp` ; sécurité `--web` (jeton d'URL puis
  cookie, contrôle Host) ; `config.conf` (jeton GitHub jamais affiché) ; copier/coller Windows.
- **Interfaces** : fenêtre native et `--web` ne tournent pas dans le même processus ; macOS = coque tmux + navigateur.
- **Outil** `chrome-headless-shell` dans `.claude-launcher/env.yaml` (PR #159).

## Points pour la prochaine session

- Relancer les sessions Claude ouvertes avant le 2026-11-21 ~10:00 (GH_TOKEN factice hérité d'un test).
- Suite ui/desktop plus lente avec les faux moteurs (95 s → 154 s) : à optimiser.
- Pastilles d'effet pour backends.yaml / hub / env.yaml : suite possible (hors v6.6.0).
- Coque : comparaison des options depuis un texte vide si config.conf disparaît (cosmétique).
- `version` de project-config.json sur main ≠ X.Y.Z publié (6.5.0.5 historiquement) : à clarifier.
- Commit local `chore(config)` (accès PROD vérifiés) sur main non poussé.
- Réservé pour plus tard : PSA point 11 ; supprimer trust.json lève toutes les approbations.
- **Consigne** : concision aux teammates (DONE ≤ 2 lignes, détail dans les rapports).
- **Maquettes** : proposer d'emblée les variantes (horizontale ET verticale) quand une mise en page est en jeu.

## Regles critiques

- Règles permanentes du projet (tests isolés, milestone non démarré, maquette, README) : `.claude/agents/context/COMMON.md`.
- Branches, PR et fusion vers `main` : `CLAUDE.md` « Conventions Git ».

## Corrections de comportement

- Les agents doivent vérifier sur Windows réel ce qu'ils mesurent sur Linux : trois refus du GATE LOT-5 viennent de
  mesures simulées (tour lent, FS local) qui ne reproduisaient pas wsl.exe / partage 9P.
- Pas de diffusion sur les réseaux sociaux : marketing = release notes + site seulement.
- Une issue rouverte après validation (retours utilisateur) s'intègre au lot en cours ; maquette refaite d'abord.
