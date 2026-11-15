# Procedure de Test — Taille des caracteres par zone (Ctrl+molette) (#150)

**Version** : 6.2.0
**Date** : 2026-11-15
**Testeur** : QA

## Prerequis

- [ ] Environnement : LOCAL — interface native Windows (Wails), poste Windows (QUALIF)
- [ ] Donnees : projets factices (`CL_ROOTS`), faux `claude` (`CL_CLAUDE_BIN`), tmux isole — jamais de vrais projets
- [ ] Un projet factice ouvert (terminaux visibles) et plusieurs categories dans l'arbre ; cartes ◈ Etat affichees
- [ ] Acces : cache du launcher (`desktop-ui.json`) en lecture ; premier lancement sans `treeFontSize`/`cardsFontSize`

## Scenarios

### Scenario 1 — Ctrl+molette sur l'arbre : seule la zone survolee change

**Validation** : HUMAIN — **Issues** : #150

**Objectif** : le navigateur (en-tete, filtre, arbre, « Nouveau projet », pied) change de taille ; ni les cartes ni les terminaux.

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Noter les trois tailles de depart | Arbre 13, cartes 12, terminaux 13 (defauts) | | |
| 2 | Ctrl+molette vers le haut sur l'arbre | Texte de l'arbre grossit d'un pas, hauteur de ligne suivie ; message bref « Taille de l'arbre : N » | | |
| 3 | Observer cartes et terminaux | Inchanges | | |
| 4 | Observer la page | Aucun zoom de la page | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Ctrl+molette sur les cartes

**Validation** : HUMAIN — **Issues** : #150

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ctrl+molette vers le haut puis vers le bas sur une carte ◈ Etat | Seul le texte des cartes change ; message « Taille des cartes : N » | | |
| 2 | Observer arbre et terminaux | Inchanges | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — Ctrl+molette sur les terminaux (non-regression #140)

**Validation** : HUMAIN — **Issues** : #150

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ctrl+molette sur la zone des terminaux | Seuls les terminaux changent ; message « Taille des caracteres : N » | | |
| 2 | Observer arbre et cartes | Inchanges | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — Bornes 8 a 28 par zone

**Validation** : HUMAIN — **Issues** : #150

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Pour chaque zone : Ctrl+molette vers le haut jusqu'a la butee | Arret a 28 | | |
| 2 | Pour chaque zone : vers le bas jusqu'a la butee | Arret a 8, texte encore lisible, mise en page non cassee | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 5 — Reprise au demarrage, par poste

**Validation** : AUTO (equivalent automatise : `ng2/ui/desktop/prefs_areafont_test.go`) — **Issues** : #150

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Regler arbre 16, cartes 10, terminaux 15 | Trois tailles distinctes appliquees | | |
| 2 | Lire `desktop-ui.json` | Contient `treeFontSize`: 16, `cardsFontSize`: 10, `termFontSize`: 15 | | |
| 3 | Fermer puis relancer l'application | Les trois tailles sont reprises | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 6 — Reglage dans Reglages › Ce poste

**Validation** : HUMAIN — **Issues** : #150

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ouvrir Reglages › Ce poste › Affichage | Trois lignes de taille (terminaux, arbre, cartes) avec − / valeur / + | | |
| 2 | Changer la taille de l'arbre par +, saisir 40 pour les cartes | Arbre applique ; 40 ramene a 28 (borne) | | |
| 3 | Ctrl+molette sur l'arbre puis rouvrir Ce poste | La valeur affichee suit la molette | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 7 — Aucune touche retiree aux terminaux

**Validation** : HUMAIN — **Issues** : #150

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Dans un terminal : Ctrl+C, Ctrl+B puis une touche tmux, Ctrl+V, Ctrl+molette | Toutes les touches agissent comme avant (v6.1.0) | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Trois tailles independantes, bornees 8 a 28, memorisees par poste
- [ ] Pas de zoom de la page ; aucune touche retiree aux terminaux
- [ ] Aucune regression sur #140 (`tests/procedures/ui-desktop/taille-caracteres/`)

## Notes QA

La coque Linux n'est pas concernee (taille portee par le terminal).
