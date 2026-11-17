# Lot ci/ci-image-chart-177 (#177)

`ci-guard.sh` : garde de `scripts/ci.sh` (Linux, conteneur de développement). **Joué par QA, jamais depuis `ci.sh`** (il le relance
deux fois : ~2 min 15). Sortie `GARDE ci.sh : OK`, code 0 ; sinon la liste des échecs.
Les refus du chart sont dans `ng2/chart_helm_test.go` (`go test -run TestChart ./ng2`).

`build-version-guard.sh` : garde de `deploy/image/build.sh` — `SOURCE_BIN` doit annoncer EXACTEMENT la version demandée
(`v6.3.0.90`, `v6.3.0.9x`… refusés pour `v6.3.0.9`) ; binaire factice et BUILDAH remplacé par un bouchon : < 1 s, aucune image.
