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
| core | A RENSEIGNER (rapport tache 1) | A RENSEIGNER | | | |
| platform | A RENSEIGNER | A RENSEIGNER | | | |
| ui/tmux | A RENSEIGNER | A RENSEIGNER | | | |
| ui/desktop | A RENSEIGNER | A RENSEIGNER | | | |
| main | A RENSEIGNER | A RENSEIGNER | | | |

Regle : `Tests (apres)` >= `Tests (reference)` ; `Couverture (apres)` >= `Couverture (reference)` − 0,5 point. Un ecart = NOT VALIDATED.
