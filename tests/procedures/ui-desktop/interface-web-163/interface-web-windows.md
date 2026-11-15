# Procedure de Test — Interface web servie par la fenetre native, Windows (#163)

**Version** : 6.2.0
**Date** : 2026-11-15
**Testeur** : QA

## Prerequis

- [ ] Environnement : QUALIF — poste Windows, interface native (Wails) ; un navigateur (Chrome ou Edge) sur le meme poste ; si possible un **second poste** du meme reseau
- [ ] Donnees : projets factices, faux `claude` — jamais de vrai projet ; `backends.yaml` du poste editable (Reglages › Ce poste)
- [ ] Acces : ports libres (par defaut 8770 pour l'interface, 8766 pour le serveur des fichiers sous Windows)
- [ ] Equivalents automatises (critical) : `TestDesktopWebProtectionsLoopback`, `TestDesktopWebProtectionsNonLoopback`, `TestDesktopWebOriginPerCall`, `TestDesktopWebTwoClientsShareState`, `TestDesktopWebNonLoopbackRefusedWithoutAllowLAN` (`ng2/ui/desktop/web163_test.go`) ; `security_lot33_test.go` reste vert

## Scenarios

### Scenario 1 — Desactive par defaut, activation dans Reglages › Ce poste

**Validation** : HUMAIN — **Issues** : #163

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Premier lancement sans reglage `desktop_web`. Reglages › Ce poste | Champ « Interface web : ecoute » vide, case « Autoriser l'acces depuis le reseau » decochee, etat « desactive » ; `netstat -ano` : rien n'ecoute sur 8770 | | |
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

### Scenario 3 — Reglage change a chaud, jeton renouvele quand l'adresse change

**Validation** : HUMAIN — **Issues** : #163 (equivalent automatise : `TestDesktopWebHotReloadStartStopMove`, `TestDesktopWebTokenPerHostKeptAndPrivate`)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Navigateur ouvert sur 8770. Changer l'ecoute en `8771` dans Reglages › Ce poste | Ancien port libere, nouveau actif, sans redemarrer la fenetre | | |
| 2 | Retourner sur l'onglet du navigateur (ancienne adresse) | Plus de reponse : connexion perdue / session revoquee (les navigateurs doivent repasser par « Ouvrir dans le navigateur ») | | |
| 3 | Essayer l'ancien lien (ancien jeton) sur la nouvelle adresse | **Refuse** (403) : **le jeton est renouvele a chaque changement d'adresse d'ecoute** | | |
| 4 | Rouvrir via « Ouvrir dans le navigateur » | Nouvelle adresse avec le **nouveau** jeton ; l'interface s'affiche | | |
| 5 | Fermer puis relancer la fenetre native, sans changer le reglage | L'interface web revient sur le meme port avec **le meme jeton** (persiste par poste) : le navigateur deja ouvert se reconnecte sans nouveau jeton | | |
| 6 | Saisir un port deja pris par une autre application | Etat « erreur » avec un message ; la fenetre native continue de fonctionner | | |

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

### Scenario 5 — Ecoute hors boucle locale : refusee sans « Autoriser l'acces depuis le reseau » (allow_lan)

**Validation** : HUMAIN — **Issues** : #163 — **Prerequis** : second poste du meme reseau (sinon etapes 6-8 non jouees : le noter) (equivalents automatises : `TestValidateBackendsDesktopWeb`, `TestDesktopWebNonLoopbackRefusedWithoutAllowLAN`, `TestDesktopWebProtectionsNonLoopback`)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Reglages › Ce poste : saisir `0.0.0.0:8770` **sans** cocher « Autoriser l'acces depuis le reseau » | **Refuse** : erreur a l'enregistrement citant `allow_lan` avec un message d'aide ; l'interface web reste eteinte (etat « erreur » / rien n'ecoute sur 8770, verifier `netstat -ano`) | | |
| 2 | Cocher « Autoriser l'acces depuis le reseau » puis enregistrer | Accepte avec un **avertissement** (acces reseau, risque) ; etat « active sur 0.0.0.0:8770 — ouverte au reseau » (orange) | | |
| 3 | Observer la fenetre native | **Banniere** d'avertissement visible **tant que** l'ecoute est hors boucle locale | | |
| 4 | Depuis le poste local : `http://localhost:8770/?t=<jeton>` puis `http://<nom-de-la-machine>:8770/?t=<jeton>` | Les deux fonctionnent | | |
| 5 | Depuis le poste local : `http://<adresse-IP-de-la-machine>:8770/?t=<jeton>` | **Refuse** (403) par le controle de Host : acces par le **nom de la machine**, pas par l'IP brute | | |
| 6 | Depuis le second poste : `http://<nom-de-la-machine>:8770/?t=<jeton>` | L'interface s'affiche (le pare-feu Windows peut demander une autorisation) ; sans jeton : refus | | |
| 7 | Depuis le second poste, requete avec un en-tete Host different (ex. `curl -H "Host: evil.example" ...`) | Refus 403 (parade au DNS rebinding) | | |
| 8 | Decocher « Autoriser l'acces depuis le reseau » (ecoute toujours 0.0.0.0) puis enregistrer | Refus comme a l'etape 1 ; l'acces depuis le second poste est coupe | | |
| 9 | Remettre `127.0.0.1:8770` | Banniere disparue ; l'acces depuis le second poste est coupe | | |
| 10 | Lancer `claude-launcher-ng2 desktop --web --addr 0.0.0.0:8790` (mode navigateur seul) | Avertissement affiche au lancement ; controle de Host applique (nom de la machine, `localhost`) | | |

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

### Scenario 8 — « Regenerer le jeton »

**Validation** : HUMAIN — **Issues** : #163 (equivalent automatise : `TestWebSessionRevokedOnStopAndTokenRotates`)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Interface web active ; ouvrir la page dans le navigateur A (via « Ouvrir dans le navigateur ») et la garder ouverte ; noter le lien (jeton) | La page fonctionne | | |
| 2 | Reglages › Ce poste : « Regenerer le jeton » (fenetre native) | Message « jeton regenere » ; le navigateur A perd sa connexion (flux d'evenements ferme, session revoquee) | | |
| 3 | Dans le navigateur A, recharger ou faire un appel | Refuse (403) : il doit repasser par « Ouvrir dans le navigateur » | | |
| 4 | Essayer l'**ancien lien** | **Invalide** (403) | | |
| 5 | « Ouvrir dans le navigateur » | Nouveau lien avec le nouveau jeton : l'interface s'affiche | | |
| 6 | Fermer puis relancer la fenetre native | Le **nouveau** jeton est conserve (meme lien valable) | | |
| 7 | Dans une page web, chercher « Regenerer le jeton » | Absent / sans effet : le bouton est reserve a la fenetre native (voir scenario 9) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 9 — Reglages de l'interface web en lecture seule dans un client web

**Validation** : HUMAIN — **Issues** : #163 (equivalent automatise : `TestWebClientCannotChangeDesktopWeb`)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Dans la page web : Reglages › Ce poste | Les champs « Interface web : ecoute », la case « Autoriser l'acces depuis le reseau » et l'ecoute du **serveur des fichiers** (`browser_server`) sont en **lecture seule** (info-bulle : a modifier dans la fenetre native du poste) | | |
| 2 | Dans la page web, tenter d'enregistrer une modification de `desktop_web` ou `browser_server` (edition YAML de backends.yaml comprise) | **Refuse** avec le message « se modifient que depuis la fenetre native » ; fichier inchange | | |
| 3 | Dans la page web, modifier un autre reglage (ex. `hub_cache`, tailles) | Permis | | |
| 4 | Dans la page web : quitter, changer le theme de la fenetre, « Ouvrir dans le navigateur », « Regenerer le jeton » | Tous **refuses** ; la fenetre native n'est ni fermee ni modifiee | | |
| 5 | Dans la fenetre native : modifier ces memes reglages | Permis | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 10 — Cookie de session et expiration

**Validation** : HUMAIN — **Issues** : #163 (equivalents automatises : `TestWebCookieIsASessionNotTheToken`, `TestDesktopWebProtectionsLoopback`) — **Prerequis** : DevTools du navigateur ; l'expiration de 8 h peut etre verifiee par controle de la duree ou n'etre pas rejouee (a noter)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ouvrir l'interface via « Ouvrir dans le navigateur » ; DevTools › Application › Cookies | Cookie `cl_session_<port>` : **valeur ≠ jeton** (identifiant aleatoire long), `HttpOnly`, `SameSite=Strict` ; **aucun cookie ne contient le jeton** | | |
| 2 | Chercher le jeton dans l'URL apres chargement, l'historique et l'en-tete Referer | Le jeton n'est utilise que sur la page d'amorcage ; les appels (`/api/call`, flux d'evenements) n'acceptent que le cookie de session | | |
| 3 | Dans DevTools, supprimer le cookie puis recharger la page (sans jeton dans l'URL) | « jeton manquant » (403) | | |
| 4 | Se creer un second cookie de session (autre navigateur ou profil) | Les deux sessions fonctionnent ; un grand nombre de sessions successives (plus de 64) fait oublier les plus anciennes | | |
| 5 | Laisser une session **sans aucun appel pendant 8 h**, puis agir | La session a expire : repasser par « Ouvrir dans le navigateur » (si non rejoue, le noter) | | |
| 6 | Un site d'une autre origine (page sur un autre port de la machine) tente un appel vers l'interface | Refuse (Origin / SameSite) | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Interface web desactivee par defaut ; activee, arretee, deplacee a chaud par Reglages › Ce poste
- [ ] « Ouvrir dans le navigateur » ; jeton conserve par poste, jamais affiche
- [ ] Meme adresse que le serveur des fichiers refusee ; avertissement hors boucle locale
- [ ] Toutes les protections de `--web` conservees (jeton, cookie, JSON, meme origine, Host)
- [ ] Presse-papiers du poste jamais servi au navigateur ; `quit`, theme, `webUIOpen` refuses au navigateur
- [ ] Ecoute hors boucle locale refusee sans « Autoriser l'acces depuis le reseau » ; avec : avertissement, banniere tant que l'ecoute est ouverte, acces par le nom de la machine
- [ ] Jeton renouvele quand l'adresse change ou sur « Regenerer le jeton » (ancien lien invalide, clients deconnectes) ; conserve apres relance a adresse identique
- [ ] `desktop_web` et `browser_server` en lecture seule dans un client web ; `quit`, theme, `webUIOpen`, regeneration refuses au web
- [ ] Cookie de session `cl_session_<port>` (jamais le jeton), session expiree apres 8 h sans appel
- [ ] Deux clients sur le meme etat ; taille fixee par le client qui a le clavier

## Notes QA

Les etapes 6-8 du scenario 5 (second poste) et l'etape 5 du scenario 10 (8 h) peuvent ne pas etre jouables : le consigner sans bloquer le lot.
