# Procedure de Test — Fermeture des projets k8s ancien modele et plans de test servis (#269, #271)

**Version** : 7.1.0
**Date** : 2026-11-26
**Testeur** : QA

## Prerequis

- [ ] Environnement : LOCAL (faux kubectl, aucun vrai cluster ni vrai projet) puis QUALIF pour le scenario 5
- [ ] Donnees : tests Go de `ng2/` (voir ci-dessous)

## Scenarios

### Scenario 1 — Pod a l'ancien modele : avertissement, pas un probleme bloquant

**Validation** : AUTO — **Issues** : #269

**Objectif** : un StatefulSet en marche sans `cl.mounts` ne rend pas le backend inutilisable.

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `cd ng2 && go test ./core -run K8s269` | PASS : `k8sProblems` vide, avertissement « Montages d'avant v7.0.0 » present, vrai probleme (droit refuse) toujours bloquant, `ReadCtrView` liste le pod (OldMounts), 2 StatefulSets de meme projet listes | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Fermer et Forcer fermer arretent le pod ; la carte reste visible

**Validation** : AUTO — **Issues** : #269

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `cd ng2 && go test ./ui/desktop -run Close269` | PASS : `scale statefulset p-a --replicas=0` execute par Fermer et par Forcer fermer ; carte `Warn` = « Montages d'avant v7.0.0… » ; pod sain au modele courant non arrete | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — `_work/test-plan` servi, autres dossiers de `_work` refuses

**Validation** : AUTO — **Issues** : #271

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `cd ng2 && go test ./core -run TestPlan271` | PASS : `/files/<projet>/_work/test-plan/x.html` 200 ; `_work/mockup` 200 ; `reports`, `handoff`, `tasks`, `tmp` 404 et masques des listes ; `.env` / `PRIVATE` dans test-plan restent 404 | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — Non-regression du serveur de fichiers

**Validation** : AUTO — **Issues** : #271

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `cd ng2 && go test ./core -run 'TestSensible|TestMaquettes'` | PASS (tests existants inchanges) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 5 — QUALIF : fermeture d'un pod d'ancien modele et plan de test servi

**Validation** : AUTO — **Issues** : #269 #271 (rejoue par QA sur QUALIF, aucun jugement humain)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Sur un pod QUALIF sans `cl.mounts`, `kubectl get sts -o yaml` puis Fermer | StatefulSet a 0 replique | | |
| 2 | `curl -s -o /dev/null -w '%{http_code}' <QUALIF>/files/<projet>/_work/test-plan/<plan>.html` | 200 | | |
| 3 | Meme requete sur `_work/reports/<f>` | 404 | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Tous les scenarios passent
- [ ] Aucune regression sur les tests existants de `core` et `ui/desktop`

## Notes QA

