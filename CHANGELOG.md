# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Les changements de contrats (API, schémas) sont suivis à part dans `contracts/CHANGELOG.md`.

## [Unreleased] — v6.2.0 en cours

### Added
- Ctrl+molette sur l'arbre des projets et sur les cartes ◈ État : tailles de texte indépendantes par zone (8 à 28), mémorisées par poste et réglables dans Réglages › Ce poste › Affichage (#150)
- Menu du projet « ⟳ Rafraîchir » (`F5`) : relit tout le projet sans les caches (conteneur ou pod, session, git, GitHub, template, consommation), avec un bandeau d'étapes en tête des cartes ; une erreur reste affichée jusqu'à `×` (#158)
- Menu du projet « Forcer la fermeture… » : proposée quand un projet ouvert ou en erreur n'a plus de conteneur, de pod ni de session (pod k8s disparu) ; confirmation avec la raison et le détail du nettoyage local ; `projects.json` n'est jamais modifié (#158)
- Coque Linux : `F5` relit le projet sélectionné, `Suppr` propose la fermeture forcée (#158)
- Carte TEAM : titre « TEAM · N · TPL vX · CLAUDE Y » (versions une seule fois), `⚠` orange sur un teammate dont la version diffère (template ou claude ; détail en infobulle sous Windows) (#151)
- En-tête des panes de teammates : bandeau vert si actif, gris si inactif, neutre pour le leader ; suit l'état sans relance ; sous Linux, une ligne de titre tmux en tête de chaque pane (`pane-border-status top`) (#152)
- Onglet Teamleaders (interface native) isolé, épinglé à gauche, sans `×` ; réduit à son icône quand les onglets de projets débordent (#162)
- Interface web dans la fenêtre native : réglage `desktop_web` (désactivé par défaut, Réglages › Ce poste › Navigateur), écoute hors boucle locale refusée sans `allow_lan: true`, bannière, régénération du jeton, sessions par cookie (`cl_session_<port>`), lecture seule pour un client web (#163)
- Organisation « leader à gauche » des panes de projet (leader et `cdp` empilés à gauche, teammates à droite) : réglage global `LAYOUT_MODE` (`top` | `left`, défaut `top`), largeur `LAYOUT_LEFT_LEADER_PCT` (20–80), exception par projet `PROJECT_LAYOUTS` (champ « Mise en page » des Réglages du projet) ; bouton de bascule à droite de la barre d'onglets de l'interface native (enregistre l'exception du projet affiché) ; remise en page sans relance, repli sur `top` si tmux refuse (#153)

### Changed
- « Fermer » du menu projet est disponible dès que la ligne est ouverte ou que sa pastille n'est pas `○` (#158)
- `scripts/ci.sh` lance `go test -race ./...` sous Linux ; il exige gcc (cgo) et signale explicitement son absence (#164)
- Carte TEAM : les colonnes de version disparaissent des lignes, la carte est plus étroite (#151)

### Fixed
- Course de données sur `InitSynced` et `init-project.md` pendant les vérifications de mises à jour concurrentes : protégés par un verrou (`fa200eb`)
- Les lectures git du launcher ne prennent plus de verrou (`GIT_OPTIONAL_LOCKS=0`) et n'exécutent plus `git describe --dirty` : plus de `index.lock` laissé par le launcher (#154)
- Tests : les sockets tmux de test sont isolés dans un dossier dédié ; `scripts/ci.sh` échoue sur un socket de test résiduel ou sur un `index.lock` orphelin (#154)
- `scripts/ci.sh` échoue aussi sur un socket de test laissé dans `/tmp/tmux-<uid>` (préfixes des tests seulement) (#154)
- Backend injoignable : la fermeture forcée est proposée avec un avertissement, le nettoyage reste local et aucun arrêt du conteneur n'est demandé (#158)
- Rafraîchissement des conteneurs : plus de course entre la relecture et la lecture de l'état des conteneurs (#158)
- Rafraîchissement, étape GitHub : le milestone est relu sans attendre la relecture de masse ; `gh` absent, non connecté ou dépôt sans remote GitHub = étape verte avec la raison ; une panne réelle = étape rouge avec la cause renvoyée par `gh` (#158)
- Test `TestProjectApplyForeignOwnerTimeout` : race de test corrigée (la goroutine est attendue avant de restaurer `foreignOwner`) (#164)
- Windows, dépôt sur un partage : git refusait « dubious ownership » ; le launcher autorise désormais ce seul dossier de projet, pour ses appels `git` et `gh` (`safe.directory` par appel, configuration globale inchangée). Si git refuse encore, l'étape affiche la commande à exécuter pour vos propres dépôts (#158)

### Notes de lot — LOT-1 (#150, #154)

- Version de développement indicative : 6.2.0 (le numéro de build QUALIF exact est rapporté par `deployer`). Cette documentation ne modifie aucune version.
- QA (`_work/reports/qa-lot1-v6.2.0.md`, HEAD `0a8333a`, puis complément sur la garde) : **VALIDATED WITH RESERVATIONS**.
  - Tests #154 (gitlock, tmuxsock) : 6/6 PASS ; tests #150 (prefs_areafont, `TestConfigModelHubAreaFontSizes`) : 4/4 PASS.
  - Non-régression : `scripts/ci.sh` CI OK (31 s), govulncheck sans vulnérabilité, `GOOS=windows go vet -tags desktop,production ./...` PASS.
  - Garde `scripts/ci.sh` : échoue bien sur un socket `cl-*` et sur un `index.lock` simulés ; une session tmux hors du dossier dédié ne la déclenche pas.
  - Aucune régression ; échecs de la première passe (15) dus à l'environnement (sockets unix et inotify sur MooseFS avec `TMPDIR` dans le projet), pas au code.
- Réserves :
  - R-A (résolue en `667d2b3`) : `scripts/ci.sh` suivait `$TMPDIR` ; le dossier de sockets est désormais créé sous `/tmp` en dur.
  - R-B (ouverte, à décider par l'utilisateur) : `/tmp/tmux-1000` contient environ 965 sockets `cl-*` périmés issus d'anciennes passes ; non supprimés.
- Procédure à jouer à la main sur un poste Windows (QUALIF, `tests/procedures/ui-desktop/taille-zones-150/ctrl-molette-zones.md`) : Ctrl+molette sur l'arbre, sur les cartes, sur les terminaux (non-régression #140), bornes 8–28 par zone, Réglages › Ce poste (−/+, saisie 40 → 28), reprise au démarrage (couverte par test automatique, relance réelle à confirmer), aucune touche retirée aux terminaux. Aussi à rejouer : `tests/procedures/ui-desktop/taille-caracteres/` (#140).

### Notes de lot — LOT-2 (#158)

- Version non modifiée par cette documentation ; le numéro de build QUALIF exact est rapporté par `deployer`.
- QA (`_work/reports/qa-lot2-cycle5-v6.2.0.md`, HEAD `ecea4aa`, cycle 5) : **VALIDATED WITH RESERVATIONS** — code vert ; réserve résolue en `c069b8c` : la procédure Windows couvre désormais « dubious ownership » (scénario 9 : UNC, cas résiduel, non-régression). Le cycle 5 remplace le verdict du cycle 4.
  - Tests #158 ciblés (core, desktop, tmux) : PASS, y compris les tests de l'étape GitHub (`TestRefreshGitHubNotLoggedInIsNotRed`, `TestRefreshGitHubRealFailureShowsCause`, `TestForcedMilestoneReadDoesNotQueueBehindBulkRead`).
  - `CGO_ENABLED=1 go test -race ./...` : PASS sur toutes les suites, aucune race ; `TestProjectApplyForeignOwnerTimeout` est vert (#164).
  - `scripts/ci.sh` (désormais en `-race`) : CI OK (57 s), govulncheck sans vulnérabilité, build croisé Windows OK. Résidus #154 : aucun socket `cl-*` nouveau dans `/tmp/tmux-1000`, aucun serveur `cl-desk*`, aucun index.lock.
- Réserve :
  - R-C (résolue en `56603a8`) : la procédure Windows `tests/procedures/ui-desktop/rafraichir-forcer-158/rafraichir-forcer-windows.md` reçoit le scénario 8 (étape GitHub : cause affichée quand elle est rouge, cas légitimes verts).
- Procédures à jouer à la main en QUALIF sur 6.2.0.4 (`tests/procedures/`) :
  - Windows : S1, S2, S3 (pod supprimé à la main, projet k8s jetable), S4, S6 (backend injoignable : Rafraîchir conserve l'onglet, Forcer avec avertissement, nettoyage local seul, annulation, rétablissement), S7 (le projet revit entre la question et la confirmation : refus, rien tué, reprise ↺ conservée), S8 (étape GitHub du rafraîchissement), S9 (« dubious ownership » sur un dépôt du partage). S5 est couvert par un test automatique.
  - Linux (hub) : S1 à S4.

### Notes de lot — LOT-3 (#151, #152, #162)

- Version non modifiée par cette documentation ; le numéro de build QUALIF exact est rapporté par `deployer`.
- QA final (`_work/reports/qa-lot3-v6.2.0.md`, sur `5763b66`, après la revue) : **VALIDATED WITH RESERVATIONS** (réserves : scénarios manuels en QUALIF).
  - Tests feature #151, #152, #162 (core, ui/tmux, ui/desktop) : PASS ; `CGO_ENABLED=1 go test -race ./...` PASS sans tolérance ; `scripts/ci.sh` CI OK (60 s, au budget indicatif de ~60 s).
  - Rendu réel du format des bordures de pane dans un client tmux : `teamleader · leader`, `dev-backend · actif`, `qa · inactif`, sans chaîne cassée.
  - Correctifs de revue : `d12de8c` (onglet Teamleaders recalculé à toute variation de largeur de la barre, #162) et `5763b66` (le surveillant de mise en page rend la main quand la fenêtre disparaît, `TestLayoutWatchReturnsWhenWindowGone`, aucun processus survivant ; titre de la carte TEAM avec la cible du template, #151/#152).
  - Le JavaScript de l'onglet Teamleaders (`app.js`) n'a pas été exécuté en QA : `node` est absent du conteneur. Le comportement est couvert par un test Go (`TestTeamleadersTabOutsideScrollAndNoOscillation`) ; l'absence d'oscillation reste à voir au scénario Windows S4.
- Tests adaptés :
  - `TestTeamCardTemplateState` (décision du GATE 2) : la ligne de la carte TEAM ne porte plus `▲` ni `vA→vB` (les versions sont dans le titre).
  - `TestHostProjectWithTmux` (décision utilisateur, LOT-3) : avec `pane-border-status top`, le leader de la rangée du haut fait une ligne de moins (cellule − 1), `y` reste la cellule.
- Procédures à jouer à la main en QUALIF (`tests/procedures/`) :
  - Windows (`ui-desktop/team-lisible-151-152-162/team-lisible-windows.md`) : S1 à S5 (carte TEAM, en-têtes de panes, onglet Teamleaders, réduction à l'icône sans oscillation, deux launchers sur un projet en conteneur).
  - Linux (`ui-tmux/team-lisible-151-152/team-lisible-linux.md`) : S1 à S4 (carte TEAM, ligne de titre par pane, launcher antérieur ou lecture seule, deux launchers en QUALIF).

### Notes de lot — LOT-4 (#163)

- Version non modifiée par cette documentation ; le numéro de build QUALIF exact est rapporté par `deployer`.
- Audit sécurité (`_work/reports/security-lot4-v6.2.0.md`) : **78/100**, aucun critique, deux majeurs. M1 (contrôle de `Host` désactivé sur une écoute hors boucle locale réglée dans `desktop_web`) et M2 (jeton permanent envoyé à tous les ports de `localhost`) sont corrigés : `f2e71e3` (Host hors boucle locale, session en mémoire, rotation du jeton, `O_EXCL`, serveur HTTP borné), `0b2166d` (allow_lan obligatoire hors boucle locale), `d120710` (sessions bornées et expirées, abonnés SSE, jeton réécrit atomiquement). Les mineurs restants sont listés dans le rapport d'audit.
- QA (`_work/reports/qa-lot4-v6.2.0.md`, HEAD `6429c86`) : **VALIDATED WITH RESERVATIONS**.
  - Tests feature (core `desktopweb*`, ui/desktop `web163`, `websec_*`, `security_lot33`) : PASS ; `CGO_ENABLED=1 go test -race ./...` PASS sans tolérance ; `scripts/ci.sh` CI OK (70 s, au-dessus du budget indicatif de ~60 s : à découper si la tendance continue).
  - Contrôle par mutation : 9/9 protections détectées (Host, cookie de session, `desktop_web`/`browser_server` refusés au client web, `webUIRegen`/`webUIOpen` refusés au web, plafond de sessions, verrou `InitSynced` sous `-race`, `allow_lan`, `sameOrigin`).
  - La procédure Windows `interface-web-163/interface-web-windows.md`, obsolète à la date du QA, est mise à jour par `0f4e322` (scénario 5 : `allow_lan`, bannière, régénération du jeton, lecture seule au client web, cookie de session). Réserve levée.
- Procédures à jouer à la main en QUALIF (`tests/procedures/ui-desktop/interface-web-163/`) : Windows S1 à S7 et les scénarios ajoutés (S5 réécrit : `allow_lan`) ; un second poste réseau est nécessaire pour S5. S4 a un équivalent automatique (`TestValidateBackendsDesktopWeb`).

### Notes de lot — LOT-5 (#153)

- Version non modifiée par cette documentation ; le numéro de build QUALIF exact est rapporté par `deployer`.
- Revue de code (`_work/reports/code-reviewer-lot5-v6.2.0.md`) : **APPROUVE AVEC RÉSERVES** (0 critique, 0 majeur, 5 mineurs, dont : repli `top` déclenché pour toute erreur de `select-layout` et répété à chaque remise en page ; écriture non atomique de `config.conf` lue à moitié ; clé de projet indexée par nom de dossier, comme la couleur).
- QA (`_work/reports/qa-lot5-v6.2.0.md`, HEAD `32b6558`) : **VALIDATED WITH RESERVATIONS**.
  - Tests feature PASS ; `CGO_ENABLED=1 go test -race ./...` PASS sans tolérance ; `scripts/ci.sh` CI OK.
  - Organisation `top` identique à la v6.1.0 : 165 chaînes de layout comparées octet pour octet (5 tailles de fenêtre × 0–10 teammates × 1–3 panes du haut).
  - Rendu réel « leader à gauche » dans tmux isolé : 1, 4, 5 et 8 teammates, aucun chevauchement, fenêtre couverte.
- Réserves :
  - R-F : le repli sur `top` quand tmux refuse la chaîne n'est couvert par aucun test ; à ajouter (test unitaire avec une cible factice dont le premier `select-layout` échoue).
  - R-G : `scripts/ci.sh` à 83 s, au-dessus du budget indicatif de ~60 s ; suivi par l'issue #179 (découpage ou parallélisation des tests `ui/desktop`).
- Procédures à jouer à la main en QUALIF (`tests/procedures/`) :
  - Windows (`ui-desktop/organisations-153/organisations-windows.md`) : S1 à S4 (réglage global, exception par projet, 1 à 6 teammates avec panes cachés, deux launchers). La parité `confLite` de `PROJECT_LAYOUTS` ne se teste pas hors Windows.
  - Linux (`ui-tmux/organisations-153/organisations-linux.md`) : S1 à S4 (global avec `Ctrl+B R`, exception, panes cachés avec lignes de titre, deux launchers).
