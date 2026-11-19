# Procedure de Test — Mise à jour et redémarrage du serveur vus par le client web (#198)

**Version** : 6.4.0
**Testeur** : QA
**Contrat** : `contracts/web-server.md` « Version du serveur » — maquette `docs/mockup/` (`web_client__maj_serveur.html`) — tests auto : `ng2/ui/desktop/server198_test.go`
**Environnement** : QUALIF (pod `claude-launcher-0`), page ouverte par l'ingress interne

## Prerequis

- [ ] Image QUALIF 6.4.0.x déployée, page du client web ouverte **avant** la mise à jour, avec au moins un terminal de projet ouvert
- [ ] Une image 6.4.0.(x+1) disponible pour `helm upgrade`

## Scenarios

### Scenario 1 — Contrôles automatisés

**Validation** : AUTO | **Issues** : #198

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | `go test ./ui/desktop -run 198` dans `ng2` | PASS : `server{version,boot}` dans init et l'instantané, boot stable par processus et différent à chaque démarrage, logique client (node) |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 2 — Mise à jour : bandeau sans F5

**Validation** : HUMAIN | **Issues** : #198

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | Laisser la page ouverte, lancer `helm upgrade` vers 6.4.0.(x+1) | le pod redémarre ; la page perd puis retrouve le flux |
| 2 | Observer la page **sans F5** | bandeau « Serveur mis à jour (v…) — Recharger » (conforme à la maquette) |
| 3 | Cliquer « Recharger » | page rechargée, nouvelle version affichée dans l'interface, terminaux ré-ouverts |
| 4 | Ne pas cliquer : continuer à utiliser la page | le bandeau reste visible, rien ne se casse |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 3 — Redémarrage simple : reconnexion seule

**Validation** : HUMAIN | **Issues** : #198

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | Page ouverte avec un terminal, `kubectl -n claude-qualif delete pod claude-launcher-0` | flux coupé |
| 2 | Observer la page | bandeau « Serveur redémarré — reconnexion des terminaux… » (pas de « Recharger ») |
| 3 | Attendre le retour du pod | terminaux rattachés automatiquement à leurs sessions tmux, contenu conservé ; **bandeau retiré** à la fin |
| 4 | Taper dans un terminal | la saisie est reçue |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 4 — Annonce de release en cluster non cliquable

**Validation** : HUMAIN | **Issues** : #198

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | Quand une release plus récente est annoncée | annonce « mise à jour par helm upgrade », **aucun bouton** |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 5 — Hub (jeton) et fenêtre native inchangés

**Validation** : HUMAIN | **Issues** : #198

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | Hub Linux avec client web (`token`) : redémarrer le hub | comportement de v6.3.0 (session en mémoire) ; aucun bandeau parasite |
| 2 | Fenêtre native (exe) | aucun bandeau serveur |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Une mise à jour du serveur est signalée par le client web sans F5
- [ ] Un simple redémarrage ne demande pas de rechargement et rattache les terminaux

## Notes QA

Contexte : après 6.4.0.1 → 6.4.0.3 un F5 était nécessaire.
