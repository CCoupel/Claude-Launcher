# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Les changements de contrats (API, schémas) sont suivis à part dans `contracts/CHANGELOG.md`.

## [Unreleased]

### Added
- Réglages › Ce poste : les rubriques « Serveur de maquettes » et « Interface web » sont séparées. Le commutateur `desktop_web.enabled` allume ou éteint l'interface web sans effacer son adresse, sans relance du launcher (#183)

### Changed
- `desktop_web` et `browser_server` s'affichent et se modifient en valeur effective pour ce poste et ce système, avec la provenance de chaque réglage (`defaults`, `hubs.<machine>`, surcharge par système). « Retour au défaut » retire le champ de toutes les couches (#183)
- `scripts/ci.sh` tient en moins de 60 s hors étape image : 56 s en moyenne sur trois passes, contre 119 s. Les tests de `ui/desktop` attendent désormais des évènements au lieu de dormir ; la couverture et le nombre de tests sont inchangés (#179)

## [6.2.0] — 2026-11-16

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
- Bascule de mise en page (#153) : la latence dépassait 15 s sur un partage réseau Windows ; elle est désormais immédiate dans l'interface native (`50101a7`, `d1bd4d2`). Sous Windows, la mise en page est appliquée avant la persistance de `config.conf`, les commandes tmux sont regroupées (deux allers-retours), et les clics rapides sont sérialisés par projet : le dernier clic gagne (`dd6642f`, `ac0e3a0`)
- Course de données sur `InitSynced` et `init-project.md` pendant les vérifications de mises à jour concurrentes : protégés par un verrou (`fa200eb`)
- Les lectures git du launcher ne prennent plus de verrou (`GIT_OPTIONAL_LOCKS=0`) et n'exécutent plus `git describe --dirty` : plus de `index.lock` laissé par le launcher (#154)
- Tests : les sockets tmux de test sont isolés dans un dossier dédié ; `scripts/ci.sh` échoue sur un socket de test résiduel ou sur un `index.lock` orphelin (#154)
- `scripts/ci.sh` échoue aussi sur un socket de test laissé dans `/tmp/tmux-<uid>` (préfixes des tests seulement) (#154)
- Backend injoignable : la fermeture forcée est proposée avec un avertissement, le nettoyage reste local et aucun arrêt du conteneur n'est demandé (#158)
- Rafraîchissement des conteneurs : plus de course entre la relecture et la lecture de l'état des conteneurs (#158)
- Rafraîchissement, étape GitHub : le milestone est relu sans attendre la relecture de masse ; `gh` absent, non connecté ou dépôt sans remote GitHub = étape verte avec la raison ; une panne réelle = étape rouge avec la cause renvoyée par `gh` (#158)
- Test `TestProjectApplyForeignOwnerTimeout` : race de test corrigée (la goroutine est attendue avant de restaurer `foreignOwner`) (#164)
- Windows, dépôt sur un partage : git refusait « dubious ownership » ; le launcher autorise désormais ce seul dossier de projet, pour ses appels `git` et `gh` (`safe.directory` par appel, configuration globale inchangée). Si git refuse encore, l'étape affiche la commande à exécuter pour vos propres dépôts (#158)

### Validation
Cinq lots (#150, #154 ; #158 ; #151, #152, #162 ; #163 ; #153) ont été validés en QUALIF, chacun après revue de code, QA et contrôle de sécurité quand le lot le demandait (audit de `--web`, 78/100 puis corrections M1 et M2). La CI passe avec `go test -race` sous Linux. La réserve restante est reportée à v6.3.0 : la durée de `scripts/ci.sh` (#179, au-dessus du budget indicatif de ~60 s).
