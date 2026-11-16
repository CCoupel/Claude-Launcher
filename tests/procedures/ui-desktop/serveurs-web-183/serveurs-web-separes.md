# Procedure de Test — Reglages des deux serveurs web separes, commutateur (#183)

**Version** : 6.3.0
**Date** : 2026-11-16
**Testeur** : utilisateur (validation humaine, label `VALID-HUMAINE`)

Reference : `contracts/desktop-web.md`, maquette retenue C' `docs/mockup/v6.3.0/ui/config_ui__serveurs_web_separes.html`
(deux rubriques « Serveur de maquettes » et « Interface web » dans Reglages > Ce poste).

## Prerequis

- [ ] Environnement : QUALIF — **poste Windows** (exe natif) puis **poste Linux** (fenetre native) ; rejouer tout le lot des deux cotes
- [ ] Donnees : projets factices (faux `claude`) ; `backends.yaml` de depart : aucun `desktop_web` ; deux ports libres (ex. 8765 maquettes, 8787 interface)
- [ ] Acces : jamais de vrais projets ni de vrai `claude` ; un navigateur sur le poste pour les liens

## Scenarios

### Scenario 1 — Les deux rubriques separees (maquette C')

**Validation** : HUMAIN — **Issues** : #183

**Objectif** : comparer la page a la maquette C' : deux rubriques distinctes, lisibles, libelles clairs.

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ouvrir Reglages > Ce poste | Deux rubriques distinctes : « Serveur de maquettes » et « Interface web » | | |
| 2 | Comparer a la maquette C' (Windows puis Linux) | Disposition, libelles, aide conformes ; rien tronque ni chevauche ; theme clair et sombre lisibles | | |
| 3 | Lire les textes d'aide | La difference entre fichiers en lecture seule (maquettes) et interface complete (jeton) est comprise | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Etat « eteinte » (defaut)

**Validation** : HUMAIN — **Issues** : #183

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Poste sans `desktop_web` | Commutateur « Interface web » **eteint** ; pas de lien d'acces ; champ adresse non actif ou vide | | |
| 2 | Tenter d'ouvrir `http://127.0.0.1:8787/` dans le navigateur | Connexion refusee (rien n'ecoute) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — Allumer a chaud, adresse proposee

**Validation** : HUMAIN — **Issues** : #183

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Allumer le commutateur sans saisir d'adresse | L'interface propose `127.0.0.1:8787` | | |
| 2 | Enregistrer | Etat « allumee », lien d'acces affiche, **sans relancer** le launcher ; le serveur des maquettes inchange | | |
| 3 | Ouvrir le lien d'acces dans le navigateur | L'interface web s'affiche (session ouverte avec le jeton) | | |
| 4 | Saisir une adresse invalide (ex. `abc`) puis enregistrer | Erreur de reglage lisible, interface non demarree, fichier non casse | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — Eteindre puis rallumer, adresse conservee

**Validation** : HUMAIN — **Issues** : #183

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Eteindre le commutateur, enregistrer | Etat « eteinte » ; le lien n'ouvre plus rien ; **l'adresse reste affichee/conservee** | | |
| 2 | Rallumer, enregistrer | Meme adresse, etat « allumee », sans relance | | |
| 3 | Verifier `backends.yaml` (onglet texte) | `desktop_web.enabled` ecrit explicitement ; `listen` present | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 5 — Independance des deux serveurs

**Validation** : HUMAIN — **Issues** : #183

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Les deux serveurs allumes ; eteindre seulement « Interface web » | Les maquettes restent servies (lien d'une maquette ouvert) | | |
| 2 | Rallumer l'interface, eteindre seulement le serveur de maquettes | L'interface web reste joignable | | |
| 3 | Mettre la meme adresse aux deux | Erreur de reglage claire (deux serveurs distincts) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 6 — Client web : reglages en lecture seule

**Validation** : HUMAIN — **Issues** : #183

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ouvrir l'interface dans le navigateur (lien d'acces), aller dans Reglages > Ce poste | Rubriques visibles mais **commutateur et adresse en lecture seule**, avec une mention explicite | | |
| 2 | Tenter de modifier | Refus lisible ; `backends.yaml` inchange | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 7 — Mode serveur (`desktop --web`)

**Validation** : HUMAIN — **Issues** : #183

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Lancer `claude-launcher-ng2 desktop --web` et ouvrir l'interface | La rubrique « Interface web » indique le mode serveur : etat **allumee**, adresse du lancement, commutateur non modifiable | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 8 — Non-regression v6.2.0

**Validation** : HUMAIN — **Issues** : #183

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `backends.yaml` avec `desktop_web: { listen: "8787" }` (sans `enabled`), relancer | Interface allumee comme en v6.2.0 ; commutateur affiche « allume » | | |
| 2 | Les autres pages de Reglages (Backends, Pools, Defauts) | Inchangees | | |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 9 — Surcharge Windows : valeur effective et provenance, aucune valeur masquee

**Validation** : HUMAIN — **Issues** : #183 — **Poste** : Windows (exe natif) ; rejouer sous Linux avec une surcharge `linux`

**Objectif** : avec une surcharge propre au systeme, la rubrique montre la valeur reellement appliquee et d'ou elle vient, et la modifier change bien la valeur appliquee.

Prerequis du scenario : `backends.yaml` avec, pour ce poste, `browser_server: { listen: "8765" }` et `desktop_web: { listen: "8787" }` communs, plus `windows: { browser_server: { listen: "9765" }, desktop_web: { listen: "9787" } }`.

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ouvrir Reglages > Ce poste, rubriques « Serveur de maquettes » et « Interface web » | Adresses affichees : **9765** et **9787** (valeurs effectives sous Windows), pas 8765 / 8787 | | |
| 2 | Regarder l'indication de provenance de chaque rubrique | Mention explicite que la valeur vient de la surcharge **Windows** (et que la valeur commune est masquee, ou editee separement) | | |
| 3 | Ouvrir les liens d'acces / d'une maquette | Servis sur 9787 / 9765 (valeurs effectives) | | |
| 4 | Modifier l'adresse de l'« Interface web » depuis la rubrique (ex. 9788), enregistrer | La nouvelle adresse est **appliquee sans relance** : le lien repond sur 9788, plus sur 9787 | | |
| 5 | Idem pour le « Serveur de maquettes » (ex. 9766) | Maquettes servies sur 9766 | | |
| 6 | Verifier `backends.yaml` (onglet texte) | Seule la surcharge `windows` a change ; les valeurs communes 8765 / 8787 inchangees | | |
| 7 | Ouvrir la meme page depuis le **client web** (navigateur) | Memes valeurs et meme provenance, en lecture seule | | |
| 8 | (Linux) Meme essai avec une surcharge `linux` | Meme comportement : valeur effective, provenance, modification effective | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

## Criteres de Validation

- [ ] Quatre etats corrects : eteinte, allumee, client web (lecture seule), mode serveur
- [ ] Commutateur a chaud, adresse conservee, sans relance
- [ ] Windows et Linux conformes a la maquette C'
- [ ] Surcharge systeme : valeur effective et provenance visibles, modification effective (aucune valeur masquee)
- [ ] Aucune regression sur les reglages existants

## Notes QA

Equivalents automatises (jouees par `qa`) : `core/desktopweb183_test.go`, `ui/desktop/web183_test.go`.
