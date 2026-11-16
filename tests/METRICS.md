# Metriques de tests

> Une ligne par verdict QA (tenu par le CDP) — sert a mesurer le taux de retours dus a la regression.

| Date | Milestone | Feature | Cycle | Verdict | feature | regression | quarantaine | environnement | flaky |
|------|-----------|---------|-------|---------|---------|------------|-------------|---------------|-------|

## Garde LOT-1 — nombre de tests et couverture (#179)

> Controle manuel/QA : ni le nombre de tests ni la couverture par paquet ne baissent par rapport a la reference
> mesuree par `dev-backend` (tache 1, `_work/reports/dev-backend-lot1-reference-*.md`). Procedure :
> `tests/procedures/ci/ci-60s-179/mesure-ci-60s.md`. Ne pas modifier la reference sans decision du CDP.

Releves (depuis `ng2/`) :

```bash
# nombre de tests par paquet
for p in $(go list ./...); do echo "$p $(go test -list '.*' $p | grep -cE '^(Test|Example|Fuzz)')"; done
# couverture par paquet
go test -cover ./... | awk '/coverage:/{print $2, $5}'
```

| Paquet | Tests (reference) | Couverture % (reference) | Tests (apres) | Couverture % (apres) | OK ? |
|--------|-------------------|--------------------------|---------------|----------------------|------|
| core | 286 | 71,5 | 286 | 71,5 | OK (dev-backend) |
| platform | 13 | 62,4 | 13 | 62,4 | OK (dev-backend) |
| ui/tmux | 50 | 30,5 | 50 | 30,5 | OK (dev-backend) |
| ui/desktop | 160 | 77,8 | 160 | 78,2 (QA, HEAD a1a1204) | OK (dev-backend, QA) |
| main (racine) | 1 (releve QA, HEAD a1a1204) | 1,5 | 1 | 1,5 | OK (QA) |

Regle : `Tests (apres)` >= `Tests (reference)` ; `Couverture (apres)` >= `Couverture (reference)` − 0,5 point. Un ecart = NOT VALIDATED.

Reference : HEAD `e147090`, rapport `_work/reports/dev-backend-lot1-reference-20261116-111907.md`. Colonnes « apres » = mesures de
`dev-backend` (tache 2) ; QA les rejoue a sa verification (le relevé `main` manquait a la reference : le fixer au premier passage QA).

| Duree `scripts/ci.sh` (3 passes) | Passes (s) | Moyenne (s) |
|----------------------------------|------------|-------------|
| Avant (reference e147090) | 120, 118, 120 | 119,3 (ui/desktop 110 -race ; core 16,5 ; ui/tmux 29 ; ~9 vet/build/govulncheck) |
| Apres tache 2 (dev-backend) | 57, 56, 56 | 56,3 (ui/desktop 47,9) — marge faible vs 60 s |
