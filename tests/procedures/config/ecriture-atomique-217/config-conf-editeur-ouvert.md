# Procedure de Test — config.conf atomique, editeur ouvert (#217)

**Version** : 6.6.0
**Date** : 2026-11-20
**Testeur** : utilisateur (validation humaine, label `VALID-HUMAINE`)

Reference : `contracts/desktop-dispatch.md` « Ecriture de `config.conf` (#217) ». Equivalents automatises : `ng2/core/atomic217_test.go`, `ng2/platform/replace217_test.go`.

## Prerequis

- [ ] Environnement : QUALIF — **poste Windows** (exe natif, version 6.6.0.x), config de test (sauvegarder `%AppData%\claude-launcher-ng2\` avant)
- [ ] Donnees : un `config.conf` existant avec au moins une racine de projets factice ; jamais de vrais projets
- [ ] Acces : un editeur de texte qui garde le fichier ouvert (Bloc-notes, VS Code)

## Scenarios

### Scenario 1 — Enregistrement pendant que config.conf est ouvert dans un editeur

**Validation** : HUMAIN — **Issues** : #217

**Objectif** : l'enregistrement aboutit (ou echoue proprement) sans jamais corrompre ni tronquer config.conf.

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ouvrir `%AppData%\claude-launcher-ng2\config.conf` dans le Bloc-notes, le laisser ouvert | Fichier affiche | | |
| 2 | Dans l'exe : Reglages > Ce poste, modifier une valeur, Enregistrer | Enregistrement reussi, ou message explicite « config.conf verrouille par un autre programme » apres quelques essais | | |
| 3 | Si reussi : relire le fichier dans l'editeur apres rechargement | Nouvelle valeur presente, fichier complet (aucune ligne perdue) | | |
| 4 | Si message de verrou : fermer l'editeur, reenregistrer | Enregistrement reussi ; l'ancien contenu etait reste intact entre-temps | | |
| 5 | Lister `%AppData%\claude-launcher-ng2\` | `config.conf.bak` present (version precedente) ; aucun `.config.conf.tmp-*` restant | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Temporaire orphelin balaye au demarrage

**Validation** : HUMAIN — **Issues** : #217

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Fermer l'exe ; creer `.config.conf.tmp-test1` dans le dossier de config ; regler sa date de modification a plus de 10 min (PowerShell : `(gi .config.conf.tmp-test1).LastWriteTime=(get-date).AddMinutes(-20)`) | Fichier present | | |
| 2 | Creer `.config.conf.tmp-test2` (date actuelle) et `notes.tmp` (date ancienne) | Fichiers presents | | |
| 3 | Lancer l'exe, attendre l'affichage de la fenetre | Fenetre normale | | |
| 4 | Lister le dossier | `-test1` supprime ; `-test2` et `notes.tmp` conserves ; `config.conf` inchange | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] config.conf jamais vide, tronque ou melange, quel que soit le moment du refus
- [ ] Message de verrou lisible, sans trace technique
- [ ] Aucun fichier hors motif exact supprime

## Notes QA

Les scenarios purement logiques (crash simule, retry, balayage, 0600) sont automatises : ne pas les rejouer a la main.
