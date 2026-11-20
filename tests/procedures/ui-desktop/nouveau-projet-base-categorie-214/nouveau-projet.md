# Procedure de Test — Nouveau projet : base et categorie en listes (#214)

**Version** : 6.6.0
**Date** : 2026-11-20
**Testeur** : utilisateur (validation humaine, label `VALID-HUMAINE`)

Reference : `contracts/desktop-dispatch.md` § v6.6.0 (`ResolveProjectPath`, `projectBases`, `projectCategories`, `categoryCreate`, `create`) ; maquette retenue de `docs/mockup/INDEX.md`. Equivalents automatises : `ng2/core/newproject214_test.go`, `ng2/ui/desktop/create214_test.go`, JS `ng2/ui/desktop/testdata/new_project.js`. Les refus de chemin (`..`, absolu, `\`, lien sortant, base inconnue, appel forge) sont automatises : ne pas les rejouer a la main.

## Prerequis

- [ ] Environnement : QUALIF — exe natif **Windows** 6.6.0.x, et **coque tmux** du hub
- [ ] Donnees : bases factices uniquement (jamais de vrais projets) — base A locale `C:\cl-test\PROJETS` (avec `INFRA\` contenant un projet, `VIDE\` vide), base B sur partage 9P `\\wsl.localhost\<distro>\home\test\PROJETS` ; les deux declarees dans `config.conf`
- [ ] Acces : ecriture sur les deux bases

## Scenarios

### Scenario 1 — Exe Windows : listes base et categorie, chemin final

**Validation** : HUMAIN — **Issues** : #214

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | « Nouveau projet » | Dialogue conforme a la maquette retenue : listes Base et Categorie, champ Nom, chemin final affiche | | |
| 2 | Ouvrir la liste Base | Les deux bases dans l'ordre de `config.conf` ; une base absente du disque est signalee (grisee) | | |
| 3 | Choisir la base A | Liste Categorie rechargee : `INFRA` (1 projet), `VIDE` (0) ; aucun projet ni dossier cache dans la liste | | |
| 4 | Choisir `INFRA`, nom `essai-214` | Chemin final affiche = `C:\cl-test\PROJETS\INFRA\essai-214` | | |
| 5 | Taper une categorie nouvelle `PRO\CLIENT_A` (ou « + nouvelle ») sans valider | Rien n'est cree sur le disque tant que le dialogue n'est pas valide | | |
| 6 | Valider | Projet cree et ouvert ; categorie creee ; apparait dans l'arbre sous la bonne base | | |
| 7 | Recommencer avec la meme categorie | Categorie existante reutilisee, sans erreur (fusion) ; contenu intact | | |
| 8 | Nom `..\x` ou `a/b` | Erreur lisible sous le champ Nom, bouton de validation bloque, aucun dossier cree | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Exe Windows : base sur partage 9P (`\\wsl.localhost\…`)

**Validation** : HUMAIN — **Issues** : #214

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Nouveau projet, base B (9P) | Categories chargees en un temps acceptable (< 3 s), sans gel de la fenetre | | |
| 2 | Creer `LAB\essai-9p` | Projet cree sous le partage ; chemin final correct (separateurs coherents) ; ouverture sans erreur | | |
| 3 | Pre-selection : double-clic sur une categorie de l'arbre puis « Nouveau projet » | Base et categorie pre-selectionnees | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — Coque tmux du hub

**Validation** : HUMAIN — **Issues** : #214

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Touche « nouveau projet » de la coque | Etape categorie : liste + « ＋ nouvelle », apres le choix de la base | | |
| 2 | Choisir une categorie existante, nom `essai-214` | Projet cree sous `<base>/<categorie>/essai-214`, fenetre ouverte | | |
| 3 | « ＋ nouvelle » avec nom `..` | Refus lisible, rien cree | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Aucun dossier cree hors des bases configurees
- [ ] Erreurs affichees sous le champ concerne
- [ ] Aucune regression sur la creation d'un projet sans categorie

## Notes QA

Verifier les dossiers de test a la fin et les supprimer (`C:\cl-test`, base 9P).
