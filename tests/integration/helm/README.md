# Lot helm (#170, #173)

Les tests Go de ce lot vivent dans le package `ng2` (un test Go ne peut pas être rangé hors de son module) :
`ng2/chart_helm_test.go` — `helm lint --strict` et `helm template` seulement (aucune installation), ignorés si `helm` est
absent. Lancer : `cd ng2 && go test -run 'TestChart' .`. Référence : `contracts/helm-values.md`.
