# Procedure de Test — NetworkPolicy par défaut et ingress externe refusé (#200)

**Version** : 6.4.0
**Testeur** : QA
**Contrat** : `contracts/helm-values.md` « v6.4.0 », `contracts/web-server.md` — tests auto : `ng2/chart_networkpolicy200_test.go`
**Environnement** : QUALIF (ns `claude-qualif`) ; un pod tiers jetable dans un autre namespace ; un poste hors du LAN (Internet)

## Prerequis

- [ ] Chart 6.4.0.x déployé en QUALIF avec les valeurs par défaut (`networkPolicy.enabled: true`, `ingressNamespaces: [traefik]`)
- [ ] Droits pour créer un namespace et un pod jetables ; adresse IP du pod / du Service du launcher

## Scenarios

### Scenario 1 — Contrôles automatisés

**Validation** : AUTO | **Issues** : #200

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | `go test . -run 'Chart'` dans `ng2` | PASS : NetworkPolicy par défaut (entrée seule, port du Service, namespaces réglables), désactivable, jamais ouverte à tous ; `externalIngress.enabled=true` → échec « ingress externe refusé … v7.0.0 » ; `terminationGracePeriodSeconds` ≥ 30 |
| 2 | `kubectl -n claude-qualif get networkpolicy` | une NetworkPolicy `<release>` |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 2 — L'interface reste joignable par l'ingress interne

**Validation** : HUMAIN | **Issues** : #200

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | Poste du LAN : ouvrir `https://launcher-qualif.local` | interface affichée (Traefik → pod admis) |
| 2 | Sondes : `kubectl -n claude-qualif get pod claude-launcher-0` | `Ready` (liveness / readiness passent malgré la policy) |
| 3 | Terminaux d'un projet ouvert | fonctionnent (flux SSE inclus) |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 3 — Pod tiers d'un autre namespace refusé

**Validation** : HUMAIN | **Issues** : #200

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | `kubectl create ns np-test && kubectl -n np-test run curl --image=curlimages/curl --restart=Never --command -- sleep 600` | pod prêt |
| 2 | `kubectl -n np-test exec curl -- curl -m 5 -s -o /dev/null -w '%{http_code}' http://<IP-service-launcher>:<port>/healthz` | **échec / délai dépassé** (pas de réponse) |
| 3 | Même appel depuis un pod du namespace `traefik` (ou via l'Ingress) | réponse 200 |
| 4 | Sortie du launcher non restreinte : « Tester » un backend ssh / vérifier la version GitHub depuis le pod | fonctionne |
| 5 | Nettoyer : `kubectl delete ns np-test` | — |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 4 — Désactivation

**Validation** : HUMAIN | **Issues** : #200

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | `helm upgrade … --set networkPolicy.enabled=false` | NetworkPolicy supprimée ; le pod tiers atteint de nouveau le Service |
| 2 | Remettre `true` | refus rétabli |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 5 — Depuis Internet : 403 et ingress externe refusé

**Validation** : HUMAIN | **Issues** : #200

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | Depuis un poste hors LAN : `curl -ik --resolve launcher-qualif.local:443:<IP publique> https://launcher-qualif.local/` | **403** (liste d'adresses de l'ingress interne) |
| 2 | `helm template … --set externalIngress.enabled=true --set externalIngress.host=… ` | échec « ingress externe refusé : aucune authentification avant v7.0.0 (#172) » |
| 3 | `kubectl -n claude-qualif get ingress` | un seul Ingress (l'interne) |
| 4 | `helm upgrade` : arrêt du pod | arrêt propre des terminaux dans le délai de 30 s |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Seul le namespace du contrôleur d'Ingress atteint le pod
- [ ] Aucun accès depuis Internet (403), ingress externe impossible en v6.4.0

## Notes QA

Traefik est un DaemonSet hors réseau d'hôte dans ce cluster : `networkPolicy.ingressCIDRs` reste vide.
