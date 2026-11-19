# Procedure de Test — Onglets de projets : couleur, titre stable, ordre (#189, #190, #191)

**Version** : 6.4.0
**Testeur** : QA
**Maquette** : `docs/mockup/` `tab_bar__couleur_titre_ordre.html` (retenue B) — tests auto : `ng2/ui/desktop/tabs189_test.go`, `testdata/tabs_bar.js`
**Environnement** : fenêtre native Windows (WebView2) **et** interface web (navigateur) — même code ; coque tmux non concernée

## Prerequis

- [ ] Au moins 4 projets de couleurs différentes ouverts (onglets), dont un avec un agent au travail
- [ ] Windows : exe 6.4.0.x ; navigateur : interface web du hub ou du pod

## Scenarios

### Scenario 1 — Contrôles automatisés

**Validation** : AUTO | **Issues** : #189 #190 #191

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | `go test ./ui/desktop -run 189` dans `ng2` | PASS (titre, couleur, ordre, persistance, Ctrl+Tab, machine du glisser) |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 2 — Couleur du projet hors sélection (#189)

**Validation** : HUMAIN | **Issues** : #189

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | Regarder les onglets non sélectionnés | liseré bas de 3 px + contour discret à la couleur de chaque projet (conforme à la maquette B) |
| 2 | Sélectionner un onglet | fond plein à sa couleur ; les autres gardent leur liseré |
| 3 | Thèmes clair et sombre | couleurs lisibles dans les deux |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 3 — Titre et largeur stables (#190)

**Validation** : HUMAIN | **Issues** : #190

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | Sélectionner puis désélectionner un onglet, observer sa largeur | **aucun changement de largeur ni de position des voisins** |
| 2 | Un agent passe au travail / au repos dans un onglet non sélectionné | titre = nom seul ; emplacement d'état (⚙) apparaît dans l'espace réservé, sans décalage ; « agent au travail » en infobulle |
| 3 | État ⏳ / ⚠ / 👁 | s'affichent dans le même emplacement, sans décalage |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 4 — Glisser-déposer (#191)

**Validation** : HUMAIN | **Issues** : #191

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | Clic simple sur un onglet | sélection, pas de déplacement |
| 2 | Glisser un onglet de plus de 5 px | l'onglet suit ; trait bleu au point de dépôt |
| 3 | Déposer | nouvel ordre ; Ctrl+Tab suit l'ordre affiché |
| 4 | Glisser puis Échap | annulé, ordre inchangé |
| 5 | Cliquer le × | détache sans jamais lancer de glisser |
| 6 | Essayer de glisser Teamleaders | impossible ; reste fixe à gauche ; aucun onglet ne se place avant lui |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 5 — Ordre conservé et nouveau projet

**Validation** : HUMAIN | **Issues** : #191

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | Recharger la page / relancer l'exe | l'ordre est conservé (clé `cl.tabOrder` du stockage local) |
| 2 | Ouvrir un nouveau projet | onglet ajouté **à la fin** |
| 3 | Fermer un projet puis le rouvrir | retrouve sa place si encore connu, sinon à la fin |
| 4 | Navigation privée / stockage bloqué (navigateur) | l'interface fonctionne, l'ordre n'est simplement pas conservé |
| 5 | Autre navigateur / poste | ordre propre à ce poste (pas partagé) |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Fenêtre native Windows et navigateur identiques
- [ ] Aucun saut de largeur des onglets à la sélection ni au changement d'état

## Notes QA
