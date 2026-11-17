# Procedure de Test — Racine de base configurable, demarrage Windows (#165)

**Version** : 6.3.0
**Date** : 2026-11-16
**Testeur** : utilisateur (validation humaine, label `VALID-HUMAINE`)

Reference : `contracts/launcher-env.md` (« Racine de base », ordre de priorite du dossier de configuration, racine invalide, section `backends`). Equivalents automatises : `ng2/rootbase_test.go` (Linux).

## Prerequis

- [ ] Environnement : QUALIF — **poste Windows** (exe natif `claude-launcher-ng2.exe`, version 6.3.0)
- [ ] Donnees : un dossier de racine **factice** (ex. `C:\cl-test-racine\`) contenant `PROJETS\TEST\alpha` (depot git vide) ; jamais de vrais projets ni de vrai `claude`
- [ ] Acces : invite de commandes ; sauvegarde prealable de `%AppData%\claude-launcher-ng2\` (pour restauration)

## Scenarios

### Scenario 1 — Sans la variable : demarrage inchange (v6.2.0)

**Validation** : HUMAIN — **Issues** : #165

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Fermer le launcher, s'assurer que `CL_ROOT_BASE` n'est pas definie (`echo %CL_ROOT_BASE%`) | Variable vide | | |
| 2 | Double-clic sur l'exe (ou lancement sans argument) | La fenetre native s'ouvre comme en v6.2.0 : meme liste de projets, memes reglages, memes backends | | |
| 3 | `claude-launcher-ng2.exe backends` | Section « Racine de base : aucune » ; configuration dans `%AppData%\claude-launcher-ng2` | | |
| 4 | Parcourir Reglages > Ce poste | Aucune regression visible ; aucune racine de base proposee ni imposee | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Avec `--root-base` vers une racine factice

**Validation** : HUMAIN — **Issues** : #165

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `claude-launcher-ng2.exe backends --root-base C:\cl-test-racine` | Sortie commencant par « Racine de base : C:\cl-test-racine (--root-base) » ; configuration, cache, config de claude et projets sous cette racine | | |
| 2 | Lancer l'exe avec `--root-base C:\cl-test-racine` (ou `set CL_ROOT_BASE=…`) | La fenetre s'ouvre ; le projet factice `alpha` est liste | | |
| 3 | Verifier l'arborescence | `C:\cl-test-racine\.config\claude-launcher-ng2\config.conf` cree, chemins relatifs a la racine ; **rien de nouveau** dans `%AppData%\claude-launcher-ng2` | | |
| 4 | Option et variable en meme temps (variable vers un dossier inexistant, option valide) | L'option l'emporte, demarrage normal | | |
| 5 | Redemarrer par une mise a jour ou « relancer » depuis l'interface | La racine de base est conservee apres relance | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — Racine invalide : message clair, rien cree ailleurs

**Validation** : HUMAIN — **Issues** : #165

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `claude-launcher-ng2.exe backends --root-base C:\racine-inexistante` | Message d'erreur lisible nommant le chemin et la source (`--root-base`), code de sortie non nul | | |
| 2 | Idem avec `set CL_ROOT_BASE=C:\racine-inexistante` | Message nommant `CL_ROOT_BASE` | | |
| 3 | Verifier `C:\racine-inexistante`, `%AppData%` | Rien n'a ete cree | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Sans la variable : comportement de la v6.2.0
- [ ] Avec une racine factice : projets, configuration et backends pris sous la racine
- [ ] Racine invalide : message clair, rien cree ailleurs

## Notes QA

Restaurer `%AppData%\claude-launcher-ng2` sauvegarde et supprimer la racine factice a la fin.
