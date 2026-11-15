# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Les changements de contrats (API, schémas) sont suivis à part dans `contracts/CHANGELOG.md`.

## [Unreleased] — v6.2.0 en cours

### Added
- Ctrl+molette sur l'arbre des projets et sur les cartes ◈ État : tailles de texte indépendantes par zone (8 à 28), mémorisées par poste et réglables dans Réglages › Ce poste › Affichage (#150)

### Fixed
- Les lectures git du launcher ne prennent plus de verrou (`GIT_OPTIONAL_LOCKS=0`) et n'exécutent plus `git describe --dirty` : plus de `index.lock` laissé par le launcher (#154)
- Tests : les sockets tmux de test sont isolés dans un dossier dédié ; `scripts/ci.sh` échoue sur un socket de test résiduel ou sur un `index.lock` orphelin (#154)

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
