# Procedure de Test — Sauvegarde de l'exe avant mise a jour, retour arriere manuel (#218)

**Version** : 6.6.0
**Date** : 2026-11-20
**Testeur** : utilisateur (validation humaine, label `VALID-HUMAINE`)

Reference : `contracts/desktop-dispatch.md` « Sauvegarde avant mise a jour (#218) » ; guide de mise a jour `ng2/docs/` (retour arriere manuel). Equivalents automatises : `ng2/platform/selfreplace218_test.go`, `ng2/core/backup218_test.go`.

> Rappel : la premiere sauvegarde n'existe qu'a partir de la mise a jour **qui suit** v6.6.0 (v6.6.0 sauvegarde le binaire qu'il remplace ; les versions anterieures ne savent pas le faire).

## Prerequis

- [ ] Environnement : QUALIF — **poste Windows**, exe natif en 6.6.0.x avec mise a jour automatique active ; une version plus recente disponible (ou publiee en QUALIF)
- [ ] Donnees : dossier de l'exe accessible en ecriture ; sauvegarde prealable de `%AppData%\claude-launcher-ng2\`

## Scenarios

### Scenario 1 — Mise a jour : sauvegarde de la version precedente

**Validation** : HUMAIN — **Issues** : #218

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Noter la version courante (`claude-launcher-ng2.exe version`) et lister le dossier de l'exe | Aucun `*.bak-v*` (ou un seul, d'une mise a jour precedente) | | |
| 2 | Lancer la mise a jour (bandeau « mettre a jour ») puis laisser la fenetre se relancer | Nouvelle version affichee | | |
| 3 | Lister le dossier de l'exe | `claude-launcher-ng2.exe.bak-v<version precedente>` present, executable, une seule sauvegarde `.bak-v*` ; plus de `.old` apres quelques secondes | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Retour arriere manuel selon le guide

**Validation** : HUMAIN — **Issues** : #218

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Suivre le guide (`ng2/docs/`) : quitter le launcher, renommer l'exe courant, renommer `.bak-v<precedente>` en `claude-launcher-ng2.exe` | Operations possibles (exe non verrouille apres la sortie) | | |
| 2 | Relancer | Version precedente affichee ; projets et reglages intacts | | |
| 3 | Verifier la mise a jour automatique | Elle repropose la version plus recente (aucune version « ecartee », contrat) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — Sauvegarde verrouillee retentee au demarrage

**Validation** : HUMAIN — **Issues** : #218

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Poser une ancienne `claude-launcher-ng2.exe.bak-v6.0.0` et la garder ouverte (ex. un `copy /b` en pause, ou ouvrir le fichier dans un outil qui le verrouille) | Fichier verrouille | | |
| 2 | Faire une mise a jour | Mise a jour reussie malgre le verrou ; message d'erreur absent | | |
| 3 | Liberer le verrou, relancer le launcher | `bak-v6.0.0` supprimee au demarrage ; une seule sauvegarde restante | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Jamais d'exe absent ou tronque, meme si la mise a jour echoue
- [ ] Une seule sauvegarde conservee

## Notes QA

Les cas purement logiques (nom, purge, echec d'ecriture restaure l'original, motifs voisins intacts) sont automatises.
