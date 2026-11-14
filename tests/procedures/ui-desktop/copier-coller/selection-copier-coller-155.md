# Procedure de Test — Selection, copier / coller dans les terminaux (#155, LOT-4)

**Version** : 6.1.0
**Date** : 2026-11-14
**Testeur** : QA (scenarios joues sur l'exe Windows reel, en QUALIF, par l'utilisateur)

Reference : `_work/reports/plan-lot4.md` (options recommandees), `contracts/desktop-dispatch.md` (`clipboardWrite`, `clipboardRead`).

Raccourcis du plan : copie a la fin de la selection + `Ctrl+Maj+C` ; collage par `Ctrl+V`, `Ctrl+Maj+V` et clic droit
(le clic droit ne colle que si le programme ne suit pas la souris : sinon `Maj`+clic droit) ; selection dans un seul pane ;
lecture seule : copie permise, collage refuse avec un message.

## Prerequis

- [ ] Environnement : QUALIF — exe Windows natif (et mode `--web` sur `localhost` pour le scenario 11)
- [ ] Donnees : **projets factices et faux `claude`** pour tout ce qui n'exige pas un vrai Claude Code ; un vrai Claude Code seulement aux scenarios marques « vrai claude » (projet factice, aucune action destructrice demandee)
- [ ] Un projet ouvert avec une team de 2 teammates, deux panes : un dont le programme **suit la souris** (Claude Code vrai ou faux claude qui active le suivi de souris `printf '\e[?1000h\e[?1006h'`) et un shell qui **ne la suit pas**
- [ ] Bloc-notes ouvert a cote pour verifier le presse-papiers ; un texte d'essai de 20 lignes numerotees (`ligne 01` … `ligne 20`) dans le Bloc-notes
- [ ] Aucun `ghp_…` visible dans les captures

## Scenarios

### Scenario 1 — Selection et copie dans un pane qui ne suit pas la souris

**Validation** : HUMAIN — **Issues** : #155

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Dans le shell : `printf 'alpha beta\ngamma delta\n'`, puis glisser sur « beta » | Texte surligne (couleur de selection) | | |
| 2 | Relacher le bouton | Message bref « Copie » ; presse-papiers = « beta » | | |
| 3 | Coller dans le Bloc-notes (Ctrl+V) | « beta » exactement, sans retour a la ligne ni espace en trop | | |
| 4 | Selection sur plusieurs lignes | Colle dans le Bloc-notes sur 2 lignes (retours a la ligne conserves, pas de remplissage d'espaces en fin de ligne) | | |
| 5 | Clic simple sans selection | Presse-papiers **inchange** (pas ecrase par une selection vide) | | |
| 6 | Double-clic sur un mot, triple-clic sur une ligne | Mot / ligne selectionnes et copies | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Pane dont le programme suit la souris : `Maj`+glisser

**Validation** : HUMAIN — **Issues** : #155

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Glisser sans `Maj` dans le pane qui suit la souris | Le programme recoit la souris (pas de selection locale) | | |
| 2 | `Maj`+glisser sur du texte | Selection locale du texte, copie a la fin | | |
| 3 | Verifier dans le Bloc-notes | Texte copie correct | | |
| 4 | Apres la copie, le programme continue de recevoir clics et molette | Aucun blocage de la souris du programme | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — `Ctrl+Maj+C`

**Validation** : HUMAIN — **Issues** : #155

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Selectionner un mot, puis copier autre chose dans le Bloc-notes (presse-papiers ecrase) | | | |
| 2 | `Ctrl+Maj+C` dans le terminal (selection toujours visible) | Le mot selectionne revient dans le presse-papiers ; message « Copie » | | |
| 3 | `Ctrl+Maj+C` sans selection | Rien copie, presse-papiers inchange, pas d'erreur | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — `Ctrl+C` interrompt toujours

**Validation** : HUMAIN — **Issues** : #155

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Dans le shell : `sleep 100`, puis `Ctrl+C` | Interrompu (`^C`) | | |
| 2 | Meme chose **avec une selection visible** | Interrompu aussi ; la selection n'avale pas l'interruption | | |
| 3 | Dans Claude Code (vrai claude, projet factice) : `Ctrl+C` pendant une reponse | Interrompt la reponse comme avant | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 5 — Collage d'une ligne puis de vingt lignes (shell)

**Validation** : HUMAIN — **Issues** : #155

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Copier `echo ok1` depuis le Bloc-notes, `Ctrl+V` dans le shell | Texte insere a la ligne de commande, **sans** validation automatique | | |
| 2 | `Ctrl+Maj+V` | Meme resultat | | |
| 3 | Clic droit (le shell ne suit pas la souris) | Colle ; pas de menu contextuel du navigateur | | |
| 4 | Copier les 20 lignes numerotees, `Ctrl+V` dans le shell (`cat > /tmp/essai`) | Les 20 lignes arrivent en un bloc, dans l'ordre, sans doublon ni perte ; pas de validation ligne par ligne parasite | | |
| 5 | `Ctrl+Maj+V` puis clic droit avec le meme texte | Meme resultat (un seul collage a chaque fois, jamais deux) | | |
| 6 | Verifier le fichier (`wc -l`, `head`, `tail`) | 20 lignes, `ligne 01` … `ligne 20` | | |
| 7 | Caracteres speciaux : accents, `$`, guillemets, tabulation, emoji | Inseres tels quels | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 6 — Collage dans Claude Code (vrai claude)

**Validation** : HUMAIN — **Issues** : #155

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Coller 1 ligne (`Ctrl+V`, `Ctrl+Maj+V`, clic droit avec `Maj`) | Texte dans la zone de saisie, pas d'envoi | | |
| 2 | Coller 20 lignes | Un seul bloc (« [Pasted text #1 +20 lines] » ou equivalent), pas de validation ligne par ligne ; le contenu complet est envoye a la validation de l'utilisateur | | |
| 3 | Clic droit **sans** `Maj` dans Claude Code (suit la souris) | Pas de collage involontaire : le clic va au programme ; `Maj`+clic droit colle | | |
| 4 | Collage d'une image (Ctrl+V avec une image dans le presse-papiers) | Comportement note (le plan retire `Ctrl+V` au programme) : consigne au README ; `Ctrl+B` puis touche reste un chemin d'envoi brut | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 7 — Lecture seule

**Validation** : HUMAIN — **Issues** : #155

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ouvrir un projet tenu par un autre launcher (hub Linux ayant la main) : affichage en lecture seule | Bandeau / etat lecture seule | | |
| 2 | Selectionner du texte | Selection et copie permises (presse-papiers rempli) | | |
| 3 | `Ctrl+V`, `Ctrl+Maj+V`, clic droit | Collage **refuse** avec un message en pied de volet ; rien envoye au pane (verifier cote hub : aucune frappe) | | |
| 4 | Saisie clavier normale en lecture seule | Toujours ignoree comme avant, mais le message l'explique | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 8 — Vignette Teamleaders et terminal d'ouverture

**Validation** : HUMAIN — **Issues** : #155

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Vignette Teamleaders : selectionner / copier / coller | Meme comportement qu'un pane | | |
| 2 | Terminal d'ouverture d'un projet (avant l'arrivee de claude) : selectionner, copier, coller | Meme comportement | | |
| 3 | Coller dans le terminal d'ouverture une reponse a la question (ex. `o`) | Recu par le programme d'ouverture, pas de double saisie | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 9 — Selection apres plein ecran, redimensionnement, retour d'un pane

**Validation** : HUMAIN — **Issues** : #155

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Selectionner, puis double-clic sur l'en-tete (plein ecran) | La copie deja faite reste dans le presse-papiers ; une nouvelle selection fonctionne en plein ecran | | |
| 2 | Redimensionner la fenetre ou une bordure | Selection possible juste apres, copie correcte | | |
| 3 | Pane cache puis ramene (carte TEAM) | Selection / copie / collage fonctionnent dans le pane ramene | | |
| 4 | Selectionner pendant que le pane defile (sortie continue) | Pas de plantage ; la copie contient le texte vu au relachement | | |
| 5 | Texte provenant de l'historique de defilement (molette vers le haut) | Selection et copie correctes | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 10 — Affichage reduit et non-regression des actions souris

**Validation** : HUMAIN — **Issues** : #155

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Affichage reduit de la zone (`transform: scale`, #126) : selectionner du texte | Selection alignee sur le texte (pas de decalage), copie correcte | | |
| 2 | Glisser d'une **bordure** entre deux panes | Redimensionne le pane ; ne selectionne pas de texte | | |
| 3 | Double-clic sur l'en-tete d'un pane | Plein ecran | | |
| 4 | Clic simple sur l'en-tete | Donne le focus au pane | | |
| 5 | `Ctrl`+molette sur un terminal | Taille des caracteres change (#140), pas de zoom de la page, pas de collage ni de copie | | |
| 6 | Molette seule, clics dans un programme qui suit la souris | Inchanges | | |
| 7 | Clic droit sur l'arbre des projets | Menu contextuel de l'arbre (#148) inchange ; le clic droit dans un terminal n'ouvre aucun menu | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 11 — Mode `--web` sur localhost

**Validation** : HUMAIN — **Issues** : #155

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Lancer `--web` ; ouvrir `http://localhost:<port>` dans Chrome | Interface identique | | |
| 2 | Selection, copie, `Ctrl+Maj+C`, collage une ligne et vingt lignes | Comme sous Windows, par le presse-papiers du **navigateur** ; une invite d'autorisation du navigateur peut apparaitre la premiere fois | | |
| 3 | Ouvrir par une adresse non securisee (ex. `http://<ip-de-la-machine>:<port>` en HTTP) | Copie et collage indisponibles avec un **message clair** ; aucune erreur dans la console | | |
| 4 | Presse-papiers de la machine qui heberge `--web` | Jamais lu ni expose au navigateur (verifier : le contenu colle vient bien du presse-papiers du navigateur, pas de la machine hote) | | |
| 5 | Requete `clipboardRead` envoyee a `/api/call` par curl avec le cookie | Reponse « indisponible », jamais le contenu du presse-papiers de la machine | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 12 — Aide, README et coque Linux

**Validation** : HUMAIN — **Issues** : #155

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `F1` dans l'interface native | Les raccourcis de copie et de collage sont listes (selection, `Maj`+glisser, `Ctrl+Maj+C`, `Ctrl+V`, `Ctrl+Maj+V`, clic droit, lecture seule) | | |
| 2 | README (section « Interface native ») | Meme liste, limites Windows, consigne Linux (`Maj`+glisser dans le terminal hote, selection qui traverse les panes) | | |
| 3 | Coque Linux (hub) | Comportement inchange : aucun changement de souris ni de presse-papiers | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

## Criteres de Validation

- [ ] Scenarios 1 a 12 passent sur l'exe Windows reel (scenario 11 : `localhost`)
- [ ] `Ctrl+C` interrompt toujours ; aucun collage double ; collage de 20 lignes en un seul bloc
- [ ] Lecture seule : copie oui, collage refuse avec message, rien envoye au pane
- [ ] Aucune regression : bordures, en-tete, `Ctrl`+molette, souris du programme, menu de l'arbre

## Notes QA

Presse-papiers de la machine hote jamais expose en `--web`. Masquer tout `ghp_…` dans une capture d'ecran.
