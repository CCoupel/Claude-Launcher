# Procedure de Test — Reglages : Backends, Pools, Defauts (#118, LOT-3.1)

**Version** : 6.1.0
**Date** : 2026-11-14
**Testeur** : QA (scenarios 3 et 7 : utilisateur, backend reel)

Reference : `contracts/desktop-dispatch.md` (configModel, configApply, backendTest, backendStates), maquette `docs/mockup/v6.1.0/ui/config_ui__interface_structuree.html` (A — Fenetre Reglages a rubriques).

## Prerequis

- [ ] Environnement : QUALIF — exe Windows natif ; le mode `--web` peut servir aux etapes sans reel
- [ ] Donnees : `backends.yaml` d'essai, copie du vrai fichier **sauvegardee avant** ; il contient des commentaires (en tete, entre deux backends, en fin de ligne), au moins un backend `docker` distant, un `k8s`, un pool avec `drain`, des `images`, `defaults`. Jamais de vrais projets ouverts pendant les essais
- [ ] Acces : un backend reel joignable (scenarios 3 et 7) ; le reste utilise des noms factices

## Scenarios

### Scenario 1 — Ouverture de la fenetre Reglages et lecture des trois fichiers

**Validation** : HUMAIN — **Issues** : #118

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Cliquer « ⚙ config » puis, separement, Ctrl+, | Fenetre « Reglages » modale large avec rubriques Backends, Pools, Defauts | | |
| 2 | Rubrique Backends | Liste ordonnee comme dans le fichier : nom, type, adresse/groupe/pool, contexte ; l'implicite `host` signale | | |
| 3 | Rubrique Pools | Machines de chaque pool, drain par machine, charge en cours en lecture seule | | |
| 4 | Rubrique Defauts | Backend par defaut, image par defaut, liste des images, correspondances navigateur | | |
| 5 | Fichier absent (renommer `backends.yaml`) puis rouvrir | Fenetre ouverte sans erreur, listes vides, hote seul | | |
| 6 | Pied de fenetre | « ✓ valide » et le nom du fichier ; si le fichier a des problemes, ils sont listes | | |
| 7 | Chronometrer l'ouverture (⚙ config et Ctrl+,) | La fenetre s'ouvre en moins d'une demi-seconde, « Chargement… » affiche pendant la lecture, puis le contenu ; interface non figee | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Formulaire par type de backend, ajouter / dupliquer / supprimer

**Validation** : HUMAIN — **Issues** : #118

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ajouter un backend de type `podman` | Seuls les champs utiles s'affichent (pas de contexte ni de namespace) | | |
| 2 | Changer le type en `docker` | Adresse (hote ou groupe ou pool), stockage partage | | |
| 3 | Changer le type en `ssh` | Moteur ssh, adresse | | |
| 4 | Changer le type en `k8s` | Contexte, namespace, volumes → PVC, repartition, options du pod | | |
| 5 | Dupliquer un backend | Copie avec un nom propose distinct ; nom en doublon refuse avec message | | |
| 6 | Nom vide ou invalide | Erreur en pied ; Enregistrer bloque | | |
| 7 | Supprimer le backend designe par `defaults.backend`, puis un backend utilise par un projet | Comportement attendu : retire des defauts dans le meme enregistrement, ou refus explicite (voir commits 0f486f0 / 60bfadc) ; suppression d'un pool cite par un backend refusee | | |
| 8 | Annuler | Aucun changement dans le fichier | | |
| 9 | Backend dont la ligne d'etat est tres longue (ex. erreur de connexion detaillee) ; fenetre etroite puis large | La ligne est coupee par « … » avec l'etat complet en infobulle ; elle ne deborde pas sur la carte voisine ni ne casse la liste | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — « Tester » sur un backend reel (joue par l'utilisateur)

**Validation** : HUMAIN — **Issues** : #118

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | « Tester » sur un backend `docker` distant joignable | Etapes : outil requis ✓, moteur joignable ✓, stockage partage ✓ | | |
| 2 | « Tester » sur un backend `k8s` reel | Etapes : outil, namespace, droits, PVC | | |
| 3 | « Tester » sur un backend dont l'outil est absent (ex. `docker` retire du PATH) | Etape « outil requis » en echec avec la correction proposee (`fix`) ; les etapes suivantes ne plantent pas | | |
| 4 | « Tester » sur un backend inexistant (adresse factice) | Echec lisible sur « moteur joignable » ; delai raisonnable, interface non figee | | |
| 5 | « Tester » un backend non encore enregistre (formulaire en cours) | Teste avec les valeurs du formulaire | | |
| 6 | Etats dans la liste (joignable / injoignable) | Affiches sans bloquer l'ouverture de la fenetre | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — Enregistrement reel : commentaires et ordre conserves

**Validation** : HUMAIN — **Issues** : #118

**Objectif** : l'ecriture structuree ne perd rien. Equivalent automatise : tests `core` (critical) sur des fichiers de test ; ici controle sur le vrai fichier.

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Copier `backends.yaml` (`cp backends.yaml avant.yaml`) | | | |
| 2 | Modifier un champ d'un backend (ex. namespace), Enregistrer | Message d'enregistrement + note de rechargement a chaud | | |
| 3 | `diff avant.yaml backends.yaml` | La valeur modifiee differe ; tous les commentaires et l'ordre des backends intacts. Bruit de mise en forme tolere sur les autres lignes (lignes vides retirees, alignement des commentaires, espaces des `{ }`, guillemets ajoutes) : aucun commentaire ni valeur ne change | | |
| 4 | Ajouter un backend en fin, Enregistrer, diff | Ajout seul ; commentaires intacts | | |
| 5 | Dupliquer puis supprimer le doublon, Enregistrer, diff | Fichier revenu a l'etat precedent (hors espaces) | | |
| 6 | Renommer un backend, Enregistrer | Nom change a sa place, references mises a jour si le contrat le prevoit | | |
| 7 | Fichier `backends.yaml.bak` | Copie de l'etat precedent presente | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 5 — Validation, conflit, « Editer en YAML » et retour « ← Reglages »

**Validation** : HUMAIN — **Issues** : #118

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Saisir une valeur invalide (type inconnu, pool sans machine, drain hors pool) | Probleme en pied, Enregistrer bloque, fichier inchange | | |
| 2 | Ouvrir les Reglages, modifier le fichier a la main dans un editeur, puis Enregistrer dans la fenetre | Conflit signale (recharger / ecraser) ; « recharger » reprend le fichier modifie ; « ecraser » ecrit mes changements | | |
| 3 | « Editer en YAML » | Ouvre l'editeur de texte (YAML) sur `backends.yaml` ; bouton « ← Reglages » visible | | |
| 4 | Fichier edite a la main avec une erreur de syntaxe YAML | Fenetre ouverte avec le probleme signale, pas de plantage | | |
| 5 | Dans l'editeur YAML, texte **non modifie** : « ← Reglages » | Retour immediat a la rubrique d'origine (Backends, Pools ou Defauts selon la source), sans question ; contenu rafraichi | | |
| 6 | Texte **modifie** (ex. commentaire ajoute), « ← Reglages », accepter d'enregistrer | Confirmation d'enregistrement ; fichier ecrit ; retour a la rubrique d'origine avec le changement visible | | |
| 7 | Texte **modifie**, « ← Reglages », refuser (abandon) | Modifications abandonnees ; fichier inchange sur disque ; retour a la rubrique d'origine | | |
| 8 | Texte modifie en **erreur de validation** (ex. type de backend inconnu), « ← Reglages » puis accepter d'enregistrer | Reste dans l'editeur, probleme(s) affiche(s) ; fichier inchange | | |
| 9 | Texte modifie, fichier change sur disque entre-temps (conflit), « ← Reglages » puis accepter d'enregistrer | Reste dans l'editeur, conflit signale (recharger / ecraser) ; aucun ecrasement silencieux | | |
| 10 | Dans l'en-tete de la fenetre Reglages | Aucun bouton « Fichiers YAML » (supprime) ; « Editer en YAML » n'existe qu'en pied de fenetre ; « ← Reglages » ramene a la fenetre Reglages | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 6 — Pools et Defauts

**Validation** : HUMAIN — **Issues** : #118

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ajouter une machine a un pool, Enregistrer | Pool mis a jour, backends qui l'utilisent listes (`usedBy`) | | |
| 2 | Mettre une machine en drain | Marquee drain ; la charge en cours reste en lecture seule | | |
| 3 | Retirer la derniere machine d'un pool | Refus (« pool sans machine ») | | |
| 4 | Defauts : changer le backend par defaut (valeur inconnue → refus), l'image par defaut, ajouter/retirer une image | Enregistre, commentaires conserves | | |
| 5 | Defauts : correspondances navigateur par defaut | Ajout / suppression pris en compte | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 7 — Prise en compte a chaud sur un projet reel (joue par l'utilisateur)

**Validation** : HUMAIN — **Issues** : #118

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Enregistrer un nouveau backend, puis ouvrir un projet factice dessus (F2) | Le backend apparait sans relancer le launcher | | |
| 2 | Si la prise en compte exige un redemarrage | La note affichee l'indique, comme l'editeur actuel | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 8 — Echange de noms et chaine de renommages dans un meme enregistrement

**Validation** : HUMAIN — **Issues** : #118

**Objectif** : l'ordonnancement des renommages de l'interface (`stOrderRenames`, JS, sans test automatique) ne perd ni n'ecrase aucun backend. Prerequis : trois backends factices `A`, `B`, `C` de types differents (ex. `A` docker, `B` podman, `C` ssh), `defaults.backend: A`, et si possible un pool cite par `A`. Sauvegarde du fichier avant (`cp backends.yaml avant.yaml`).

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Sans enregistrer : renommer `A` en `B` et `B` en `A` (echange), puis Enregistrer | Pas de refus « nom pris » ; le fichier contient `A` (ancien contenu de `B`, podman) et `B` (ancien contenu de `A`, docker) ; trois backends ; commentaires conserves | | |
| 2 | Verifier les references (`defaults.backend`, pool, backend par defaut affiche) | Suivent le renommage voulu par l'interface (ou restent coherentes) : aucune reference pendante, fichier « ✓ valide » | | |
| 3 | Restaurer `avant.yaml`, puis renommer `A`→`B` et `B`→`C` (chaine) dans le meme enregistrement | Resultat : l'ancien `B` s'appelle `C`, l'ancien `A` s'appelle `B` ; aucun ecrase ; trois backends | | |
| 4 | Chaine plus longue `A`→`B`, `B`→`C`, `C`→`D` | Quatre noms finaux corrects (`B`, `C`, `D` + inchange) sans perte | | |
| 5 | Cycle a trois `A`→`B`, `B`→`C`, `C`→`A` | Contenus permutes correctement, aucun backend perdu | | |
| 6 | Renommage vers un nom deja existant sans que ce dernier soit renomme (`A`→`B` seul) | Refus clair (nom pris), fichier inchange | | |
| 7 | `diff avant.yaml backends.yaml` apres chaque cas accepte | Seuls les noms (et les references suivies) changent ; contenu de chaque backend rattache au bon nom ; commentaires conserves | | |
| 8 | Backend `.bak` | Copie de l'etat precedent | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

## Criteres de Validation

- [ ] Scenarios 1 a 8 passent sur Windows natif
- [ ] Aucun commentaire ni ordre perdu, jamais de fichier invalide ecrit
- [ ] « Editer en YAML » et « ← Reglages » restent fonctionnels pour les rubriques non couvertes

## Notes QA

Restaurer le `backends.yaml` sauvegarde en fin de session. Masquer tout `ghp_…` dans une capture d'ecran.
