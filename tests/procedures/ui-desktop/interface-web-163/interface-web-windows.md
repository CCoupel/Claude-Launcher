# Procedure de Test — Interface web servie par la fenetre native, Windows (#163)

**Version** : 6.2.0
**Date** : 2026-11-15
**Testeur** : QA

## Prerequis

- [ ] Environnement : QUALIF — poste Windows, interface native (Wails) ; un navigateur (Chrome ou Edge) sur le meme poste ; si possible un **second poste** du meme reseau
- [ ] Donnees : projets factices, faux `claude` — jamais de vrai projet ; `backends.yaml` du poste editable (Reglages › Ce poste)
- [ ] Acces : ports libres (par defaut 8770 pour l'interface, 8766 pour le serveur des fichiers sous Windows)
- [ ] Equivalents automatises (critical) : `TestDesktopWebProtectionsLoopback`, `TestDesktopWebProtectionsNonLoopback`, `TestDesktopWebOriginPerCall`, `TestDesktopWebTwoClientsShareState` (`ng2/ui/desktop/web163_test.go`) ; `security_lot33_test.go` reste vert

## Scenarios

### Scenario 1 — Desactive par defaut, activation dans Reglages › Ce poste

**Validation** : HUMAIN — **Issues** : #163

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Premier lancement sans reglage `desktop_web`. Reglages › Ce poste | Champ « Interface web : ecoute » vide, etat « desactive » ; `netstat -ano` : rien n'ecoute sur 8770 | | |
| 2 | Saisir `8770` (= 127.0.0.1:8770), enregistrer | Etat « actif · 127.0.0.1:8770 » sans redemarrer ; l'adresse repond (`netstat`) | | |
| 3 | Lire l'etat affiche | Jamais le jeton dans l'ecran (ni dans l'etat, ni dans le champ) | | |
| 4 | Saisir `off`, enregistrer | Etat « desactive », le port est libere | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — « Ouvrir dans le navigateur »

**Validation** : HUMAIN — **Issues** : #163

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Reglage actif, cliquer « Ouvrir dans le navigateur » | Le navigateur par defaut s'ouvre sur l'interface (adresse d'amorcage avec jeton construite par le launcher) ; l'interface s'affiche | | |
| 2 | Recharger la page, ou ouvrir l'adresse sans le jeton | Recharge : fonctionne (cookie) ; sans jeton : « jeton manquant » (403) | | |
| 3 | Dans la page web, tenter de quitter ou changer le theme de la fenetre | Refus : la fenetre native n'est pas fermee ni modifiee par le navigateur | | |
| 4 | Comparer la page web et la fenetre native | Memes projets, meme selection, memes onglets ; les evenements arrivent aux deux | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — Reglage change a chaud, jeton conserve

**Validation** : HUMAIN — **Issues** : #163

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Navigateur ouvert sur 8770. Changer l'ecoute en `8771` dans Reglages › Ce poste | Ancien port libere, nouveau actif, sans redemarrer la fenetre | | |
| 2 | Rouvrir via « Ouvrir dans le navigateur » | Nouvelle adresse avec **le meme jeton** qu'avant | | |
| 3 | Fermer puis relancer la fenetre native | L'interface web revient sur le meme port ; le navigateur deja ouvert se reconnecte sans nouveau jeton | | |
| 4 | Saisir un port deja pris par une autre application | Etat « erreur » avec un message ; la fenetre native continue de fonctionner | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — Meme adresse que le serveur des fichiers : refus

**Validation** : HUMAIN — **Issues** : #163 (equivalent automatise : `TestValidateBackendsDesktopWeb`)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Saisir pour l'interface web l'adresse du serveur des fichiers (8766 sous Windows, ou celle de `browser_server`) | Enregistrement **refuse** avec une erreur claire (deux serveurs distincts) | | |
| 2 | Saisir « 8766 » ou « 127.0.0.1:8766 » | Meme refus (meme adresse sous deux formes) | | |
| 3 | Saisir un port different | Accepte | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 5 — Ecoute hors boucle locale et second poste

**Validation** : HUMAIN — **Issues** : #163 — **Prerequis** : second poste du meme reseau (sinon etapes 3-4 non jouees : le noter)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Saisir `0.0.0.0:8770` et enregistrer | Avertissement visible a l'enregistrement et dans l'etat (« ecoute hors boucle locale ») ; accepte | | |
| 2 | Depuis le poste local : `http://localhost:8770/?t=<jeton>` puis `http://<nom-de-la-machine>:8770/?t=<jeton>` | Les deux fonctionnent | | |
| 3 | Depuis le second poste : `http://<nom-de-la-machine>:8770/?t=<jeton>` (jeton recupere sur le poste hote) | L'interface s'affiche (le pare-feu Windows peut demander une autorisation) ; sans jeton : refus | | |
| 4 | Depuis le second poste, requete avec un en-tete Host different (ex. `curl -H "Host: evil.example" ...`) | Refus 403 (parade au DNS rebinding) | | |
| 5 | Remettre `127.0.0.1:8770` | L'acces depuis le second poste est coupe | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 6 — Presse-papiers du poste non partage avec le navigateur

**Validation** : HUMAIN — **Issues** : #163

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Copier un texte repere (« SECRET-POSTE ») dans le presse-papiers de Windows | | | |
| 2 | Dans la fenetre native, coller dans un terminal (Ctrl+V) | Le texte est colle (comportement #155 inchange) | | |
| 3 | Dans la page web (meme poste), coller dans un terminal | Le texte colle est celui que fournit le **navigateur** (son propre presse-papiers, contexte securise localhost), **jamais** celui lu par le launcher ; aucun message d'erreur de WebView2 | | |
| 4 | Dans la page web, copier une selection de terminal | Le presse-papiers de Windows n'est **pas** modifie par le launcher (verifier dans un editeur : colle ce qui a ete copie par le navigateur seulement si le navigateur le permet) | | |
| 5 | Repeter 2 apres les etapes 3-4 | La fenetre native garde son presse-papiers (la regle est par appel, pas par processus) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 7 — Taille des terminaux avec deux clients

**Validation** : HUMAIN — **Issues** : #163

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Projet ouvert dans la fenetre native et dans la page web | Memes terminaux | | |
| 2 | Redimensionner la fenetre native puis cliquer dans un terminal | Sa taille s'applique ; la page web affiche la geometrie recue sans renvoyer la sienne | | |
| 3 | Cliquer dans un terminal de la page web (prend le clavier), redimensionner le navigateur | La taille du navigateur s'applique ; la fenetre native affiche la geometrie recue | | |
| 4 | Basculer plusieurs fois d'un client a l'autre | Pas de va-et-vient de taille, pas de texte coupe | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Interface web desactivee par defaut ; activee, arretee, deplacee a chaud par Reglages › Ce poste
- [ ] « Ouvrir dans le navigateur » ; jeton conserve par poste, jamais affiche
- [ ] Meme adresse que le serveur des fichiers refusee ; avertissement hors boucle locale
- [ ] Toutes les protections de `--web` conservees (jeton, cookie, JSON, meme origine, Host)
- [ ] Presse-papiers du poste jamais servi au navigateur ; `quit`, theme, `webUIOpen` refuses au navigateur
- [ ] Deux clients sur le meme etat ; taille fixee par le client qui a le clavier

## Notes QA

Les etapes 3-4 du scenario 5 (second poste) peuvent ne pas etre jouables : le consigner sans bloquer le lot.
