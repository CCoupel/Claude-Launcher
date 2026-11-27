# Procedure de Test — Espace par utilisateur (#255)

**Version** : 7.1.0
**Date** : 2026-11-26
**Testeur** : QA

## Prerequis

- [ ] Environnement : LOCAL (tests Go) puis QUALIF (scenario 6)
- [ ] Donnees : faux serveur GitHub, HOME factice, aucun vrai projet ; aucun secret reel

## Scenarios

### Scenario 1 — Identifiant GitHub : resolution, cache, login repris

**Validation** : AUTO — **Issues** : #255

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `cd ng2 && go test ./core -run 'GhID255|ResolveUID'` | PASS : login → `github-<id>`, `users/index.json` en cache, login dont l'id change refuse (401 + audit), erreurs reseau sans acces accorde | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Etat par utilisateur `users/<uid>/`

**Validation** : AUTO — **Issues** : #255

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `cd ng2 && go test ./core -run 'UserSpace255'` | PASS : `backends.yaml`, `vault/` (0600), `config.json`, `traces.json` seulement ; dossier 0700 ; ecriture atomique + verrou ; aucune donnee projet ; identifiants absents des journaux/audit | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — Isolement de deux identites

**Validation** : AUTO — **Issues** : #255

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `cd ng2 && go test ./ui/desktop -run Spaces255` | PASS : aucun evenement SSE, terminal, `/files` ni etat d'interface croise ; `userList` refusee a un utilisateur ; utilisateur sans backend = etat « sans backend » ; instance liberee apres la duree (horloge injectee) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — Admin et repli `users.enabled: false` inchanges (NR)

**Validation** : AUTO — **Issues** : #255

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `cd ng2 && go test ./ui/desktop -run "Spaces255|Proxy172"` | PASS : l'admin garde l'espace actuel ; `users.enabled: false` = comportement v7.0.0 ; tests v7.0.0 inchanges | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 5b — Editeur de backends d'un utilisateur : sequence exacte de l'interface (retour QUALIF)

**Validation** : AUTO — **Issues** : #255

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `cd ng2 && go test ./ui/desktop -run Editor255` | PASS : `configModel`/`backendStates` sans erreur « indisponible », modele limite a son `backends.yaml`, `configApply` k8s/docker valides acceptes, champs R1 (kubeconfig local, host, context, volumes, pod sensible) refuses sans ecriture, autres rubriques et coffre refuses avec message clair, aucun element de l'admin dans les reponses | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 5 — Chart : `users.enabled`

**Validation** : AUTO — **Issues** : #255

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `helm template` avec `users.enabled=false` puis `true` (test de chart a ecrire avec infra : le repli v7.0.0 n'a pas de code Go) | variable d'environnement correspondante dans le StatefulSet ; schema valide | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 6 — QUALIF : deux comptes GitHub (jugement humain)

**Validation** : HUMAIN — **Issues** : #255

**Objectif** : constater, avec deux vrais comptes, que chacun ne voit que ses backends et son interface.

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Se connecter a QUALIF avec le compte A (autorise) | Interface sans backend : message « Ajoute ton premier backend » ; identite affichee | | |
| 2 | Dans un autre navigateur (profil prive), se connecter avec le compte B | Meme etat vide pour B, identite de B affichee | | |
| 3 | Compte admin : ouvrir la liste des utilisateurs | A et B listes avec nombre de backends et etat, jamais d'identifiant | | |
| 4 | Compte admin : verifier son arbre de projets | Espace habituel de l'admin, inchange | | |
| 5 | Compte A : ouvrir une adresse `/files/...` d'un projet de l'admin | Refus (404/403), rien de l'espace admin visible | | |
| 6 | Compte A : cliquer « + Ajouter un backend » | Reglages › Backends s'ouvre SANS « Lecture impossible » ni « indisponible dans votre espace » ; liste vide, bouton d'ajout actif | | |
| 7 | Compte A : dans « Mon espace › Backends », ajouter un backend docker `host: unix:///var/run/docker.sock` puis « Enregistrer » | Enregistrement refuse avec un message clair (moteur local interdit a un utilisateur), backend non cree | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Reporte en LOT-3.1 (#257) — base du plan de test LOT-3.1

Decision utilisateur au GATE LOT-2 : l'isolement est valide en LOT-2 ; la creation complete d'un backend est testee en LOT-3.1.
**Ne PAS valider en LOT-2** (un k8s sans kubeconfig replie sur le ServiceAccount du launcher, interdit en LOT-3.1).

### Scenario LOT-3.1 — Enregistrer un backend k8s valide

**Validation** : HUMAIN — **Issues** : #257

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Compte A : « Mon espace › Backends », ajouter un backend k8s (nom, namespace, PVC), coller un kubeconfig a jeton, « Tester » puis « Enregistrer » | Enregistrement accepte (aucune erreur), le backend est liste apres relecture, l'ecran « Ajoute ton premier backend » disparait | | |
| 2 | Recharger la page | Backend toujours present ; kubeconfig jamais reaffiche (empreinte seulement) ; rubriques reservees a l'admin non proposees ou refusees clairement | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Tous les scenarios passent
- [ ] Aucune regression sur l'espace admin et les tests v7.0.0

## Notes QA

