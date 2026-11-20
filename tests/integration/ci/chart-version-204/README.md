# Lot ci/chart-version-204 (#204)

`chart-version-guard.sh` : contrôle de la version du chart dans `scripts/ci.sh` sur branche `milestone/vX.Y.Z`
(banc temporaire sous `_work/tmp/`, jamais la CI complète : < 5 s). Sortie `GARDE chart-version : OK`, code 0.
Le message attendu est celui de `release.yml` (« Chart.yaml : version=… appVersion=…, attendu … et … »).
