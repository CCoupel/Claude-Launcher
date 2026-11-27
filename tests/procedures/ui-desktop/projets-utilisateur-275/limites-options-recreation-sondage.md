# Procedure de Test — Projets d'utilisateur : options, limites, reseaux prives, manifeste, sondage (#275, LOT-3.2 Batch 0)

**Version** : 7.1.0
**Date** : 2026-11-27
**Testeur** : QA

## Prerequis

- [ ] Environnement : LOCAL (faux kubectl/docker, DNS factice, canaris `CANARY-…`, aucun vrai projet)
- [ ] Tests ecrits AVANT le code (TDD) ; les tests sous etiquette `s275api` supposent l'API proposee dans leur en-tete (a adopter ou renommer)

## Scenarios

### Scenario 1 — E1b : `users.allowPrivateNetworks` (CL_USERS_ALLOW_PRIVATE)

**Validation** : AUTO — **Issues** : #275

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `cd ng2 && go test ./core -run Private275` | PASS : sans le parametre tout est refuse ; avec : 10/8, 172.16/12, 192.168/16, fc00::/7 et noms internes admis ; loopback, localhost*, 0.0.0.0, link-local et metadonnees (dont fd00:ec2::254), multicast, CGNAT, reservees TOUJOURS refuses ; valeurs du parametre (seul `true` active) ; DNS ; revalidation avant chaque appel avec et sans le parametre ; admin sans contrainte | | |
| 2 | `cd ng2 && go test . -run TestChartProjects275` (helm requis) | PASS : `users.maxProjects` 30 (1–1000), `users.allowPrivateNetworks` false, `CL_USERS_ALLOW_PRIVATE`, schema | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — E2 : manifeste et argv d'un projet d'utilisateur

**Validation** : AUTO — **Issues** : #275

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `cd ng2 && go test ./core -run 'TestE2(K8s|Images|Docker|Aucun)'` | PASS : StatefulSet avec labels `cl.*` (dont `cl.user`), PVC declare monte en subPath (projet + `.claude`), `automountServiceAccountToken: false`, aucun champ dangereux, commande fixe, images validees ; docker/podman : `-v <volume>:/space`, `--workdir`, labels, aucune option dangereuse ni montage hote, injection refusee, aucun stockage cree | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — E1, E1c, E3 (etiquette `s275api`)

**Validation** : AUTO — **Issues** : #275

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `cd ng2 && go test -tags s275api ./core -run 'TestE1|TestE3'` | PASS : liste blanche fermee (chemin exact des refus), aucun plafond CPU/memoire (`cpu: 64`, `memory: 1Ti` acceptes), quantites invalides refusees, defauts, 31e projet refuse (30), recreation P27 (confirmation, deroule, retour arriere, volume intact), sondage (1 appel en vol, attente 10 s → 5 min, goroutines bornees) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

## Base du plan de test HUMAIN du GATE LOT-3

`ccoupel` (utilisateur) sur les backends de test de QUALIF (parametre LAN actif) : projet cree, demarre, terminal, F5, projet arrete, Purger,
options de pod / conteneur (a completer au GATE).

## Notes QA

Le rouge « avant » est mesure sur le code de LOT-3.1 ; les tests `s275api` ne compilent pas tant que l'API n'existe pas (voir le handoff).
