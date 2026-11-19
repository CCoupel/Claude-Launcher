# Procedure de Test — Installation du launcher par Helm « en suivant le guide seul » (#185)

**Version** : 6.3.0
**Date** : 2026-11-17
**Testeur** : une personne qui **ne connait pas** le projet (ou QA qui s'interdit tout autre document) — validation humaine

**Objet** : verifier que le guide « Installer le launcher dans Kubernetes par Helm (acces interne) » (`ng2/docs/kubernetes.md`, lien depuis `README.md`) suffit, **a lui seul**, pour installer, utiliser, mettre a jour et desinstaller le launcher dans le cluster — sur une **autre racine** et un **autre namespace** que la QUALIF et la PROD (rien de reel n'est touche). Le but est de trouver les **ecarts du guide** (etape manquante, commande fausse, valeur non expliquee, limite non dite), pas de valider le launcher (deja fait par `qualif-cluster-177`).

## Regles du test (a respecter strictement)

- [ ] **Seul le guide** (la section du README, ses liens internes) sert de source. Interdits : `deploy.qualif.md`, `_work/`, `contracts/`, les procedures de test, la memoire du projet, un coup de fil. Une information absente du guide **est un ecart**.
- [ ] Executer chaque commande **telle qu'ecrite**. Toute adaptation necessaire (chemin, nom, option manquante) est notee dans le releve d'ecarts, puis appliquee.
- [ ] Un ecart bloquant (impossible de continuer sans autre source) est note, puis contourne **avec l'aide d'une personne du projet**, et le contournement est consigne (il devra entrer dans le guide).
- [ ] Tenir le **releve d'ecarts** (tableau en fin de procedure) au fil de l'eau : etape du guide, ce qui manque / est faux, gravite (bloquant, gene, cosmetique).

## Prerequis (poses par l'organisateur, hors guide)

- [ ] Acces au cluster de test avec un compte pouvant installer le chart (celui que le guide demande de se procurer) ; un poste du reseau interne
- [ ] **Autre racine factice** : `/share/MFS/ROOT/CLAUDE-GUIDE-TEST` (dossier vide cree sur le stockage partage, proprietaire `1000:1000`), **autre namespace** `claude-guide-test`, **autre nom de release** `guide-test`, **autre nom d'hote** `launcher-guide.local`, **autre nom de hub** : ne pas reprendre `cluster` ni `cluster-qualif`
- [ ] Sauvegarde horodatee des fichiers de la racine de PROD et de QUALIF (comparaison finale) ; la PROD et la QUALIF ne sont jamais ciblees par une commande du test

## Scenarios

### Scenario 1 — Le guide est trouvable et complet sur le papier

**Validation** : HUMAIN — **Issues** : #185

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Depuis `README.md` (racine), trouver le lien vers le guide | Un lien visible (section Documentation / Kubernetes) menant a la section du guide dans `ng2/docs/kubernetes.md` | | |
| 2 | Lire le guide en entier **sans rien executer** ; lister les prerequis qu'il annonce | Le guide annonce : compte de deploiement, namespace et Pod Security, volume partage (RWX) et PV de l'administrateur, controleur d'Ingress (Traefik) et liste d'adresses, nom d'hote interne en `.local` (mDNS, non resolu par les pods), CA interne et son certificat a installer sur les postes, image du paquet ghcr (empreinte), outils (`helm`, `kubectl`, `buildah`/`gh` si construction) | | |
| 3 | Chercher dans le guide chaque **valeur du chart** utilisee plus loin (`rootBase`, `volume.*`, `hubName`, `ingress.*`, `web.access`, `image.tag` / `image.digest`) | Chacune est expliquee (role, obligatoire ou non, defaut) ; aucune valeur utilisee sans explication | | |
| 4 | Chercher la section **« Limites de v6.3.0 »** | Presente, avec les risques acceptes listes en 5 (scenario 5) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Prerequis : le guide permet de les verifier soi-meme

**Validation** : HUMAIN — **Issues** : #185

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Executer les commandes de verification des prerequis donnees par le guide (namespace, PSA, PV, PVC/StorageClass, Ingress, DNS, CA) | Chaque verification existe, est executable telle quelle et dit **quoi attendre** ; sinon : ecart | | |
| 2 | Certificat de la CA : suivre le guide pour l'installer sur le poste puis verifier (`openssl x509 … -text`) | Le guide dit ou le recuperer, comment l'installer (poste Linux / Windows / navigateur), et **comment verifier la contrainte de noms** (deux noms seulement) | | |
| 3 | Nom d'hote : suivre le guide pour que `launcher-guide.local` soit resolu | Le guide explique le mDNS (postes seulement), l'entree DNS / hosts a poser et **que les pods ne resolvent pas `.local`** | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — Installation (autre racine, autre namespace)

**Validation** : HUMAIN — **Issues** : #185

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Preparer le fichier de valeurs **comme le guide l'indique** (racine `/share/MFS/ROOT/CLAUDE-GUIDE-TEST`, PVC, nom de hub, hote `launcher-guide.local`, emetteur de la CA interne, liste d'adresses du poste) | Le guide fournit un exemple de fichier complet, copiable, avec les valeurs a remplacer **identifiees** | | |
| 2 | Rendu de controle (`helm template …`) comme le guide le recommande | Image rendue = celle attendue (par empreinte), namespace `claude-guide-test`, aucune valeur refusee ; un refus du chart est **compris grace au guide** (message nomme la valeur) | | |
| 3 | `helm upgrade --install guide-test … -n claude-guide-test` | Installation sans erreur ; un seul pod `Ready` ; PVC cree par le chart et lie au PV (`volume.create`) ; le guide dit **quoi regarder** (`kubectl get pod,pvc,ingress,certificate`) | | |
| 4 | Le guide explique-t-il le premier demarrage ? | `config.conf` cree sous la racine ; la racine contient son `backends.yaml` : le guide dit **ou** et **ce qu'il faut y mettre** (types de backends, `k8s` → namespace du pod, volumes), en precisant que le **contexte d'un backend k8s est ignore en cluster** (ServiceAccount du pod) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — Utilisation : acces sans jeton, projet, mise a jour, retour arriere

**Validation** : HUMAIN — **Issues** : #185

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ouvrir l'adresse donnee par le guide | L'interface s'ouvre **directement** (`web.access: none`, defaut du chart) : le guide ne parle d'**aucun jeton** ni lien d'amorcage (sauf pour dire que le mode `token` existe) ; cadenas valide si la CA est installee | | |
| 2 | Suivre le guide pour creer un projet factice et l'ouvrir (backend `k8s`) | Pod de projet cree dans `claude-guide-test` ; terminal utilisable | | |
| 3 | « Tester » le backend `k8s` comme le guide l'indique | Etapes vertes ; en cas de droit manquant, le guide explique le message et la correction (`helm upgrade`) | | |
| 4 | **Mise a jour** : changer `image.tag` / `image.digest` comme le guide l'indique | L'ancien pod s'arrete **avant** le nouveau (un seul pod) ; le guide l'explique | | |
| 5 | **Retour arriere** : `helm rollback` comme le guide l'indique | Retour au tag precedent, **sans redemarrage en boucle** ; le guide dit quoi verifier (`restartCount` = 0) | | |
| 6 | **Cohabitation avec l'exe Windows** : suivre le guide (`--root-base`, nom de hub different) | Les deux launchers partagent la racine ; un second de **meme nom** est refuse ; « projet ouvert par un autre launcher » et prise de main expliques | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 5 — Les limites de la v6.3.0 sont dites (risques acceptes)

**Validation** : HUMAIN — **Issues** : #185 — verifier la **presence et la clarte** de chaque point dans le guide

| Etape | Point a trouver dans le guide | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|-------------------------------|-----------------|----------------|------|
| 1 | **Accès sans jeton** : l'Ingress et sa liste d'adresses sont la **seule barriere** ; tout poste de la liste pilote les projets | Dit explicitement | | |
| 2 | **Pas de NetworkPolicy** : tout pod du cluster atteint le Service sans passer par l'Ingress (v6.4.0, #172) | Dit explicitement | | |
| 3 | **Mode `none` hors cluster** (hub, exe, boucle locale) : tout utilisateur ou processus local pilote le launcher | Dit ; le defaut hors chart reste `token` | | |
| 4 | **32 flux d'evenements** au plus : un client peut les saturer (deni de service local) | Dit | | |
| 5 | **Contexte k8s ignore en cluster** : un backend `k8s` d'un autre cluster agirait sur le cluster du pod | Dit | | |
| 6 | **Ne jamais exposer hors du reseau interne** (pas de LoadBalancer/NodePort, pas d'Ingress public) | Dit ; le chart refuse NodePort/LoadBalancer et `0.0.0.0/0` | | |
| 7 | **Image installee par empreinte** (le tag seul est mutable) ; paquet public | Dit | | |
| 8 | **Serveur de maquettes** (9999) non expose en cluster (#174, v6.4.0) | Dit | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 6 — Desinstallation et isolation

**Validation** : HUMAIN — **Issues** : #185

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `helm uninstall guide-test -n claude-guide-test` comme le guide l'indique | Objets du chart retires ; **PVC et PV conserves** (le guide le dit, et dit comment les supprimer volontairement) ; fichiers de la racine intacts | | |
| 2 | Comparer la racine de PROD et de QUALIF a la sauvegarde | **Identiques** : le test n'a rien touche ; `kubectl -n claude-ng2 get all` et `-n claude-qualif get all` inchanges | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Releve des ecarts du guide (a remplir)

| # | Etape du guide | Ecart (manque / faux / ambigu) | Gravite | Contournement utilise | Correction proposee |
|---|----------------|--------------------------------|---------|----------------------|---------------------|
| 1 | | | | | |

## Criteres de Validation

- [ ] Installation, utilisation, mise a jour, retour arriere et desinstallation menees **a bien avec le seul guide** (aucun ecart bloquant)
- [ ] Toutes les limites de v6.3.0 sont dans le guide (scenario 5)
- [ ] Le guide ne mentionne plus de jeton comme chemin d'acces par defaut en cluster
- [ ] La PROD et la QUALIF n'ont pas ete touchees

## Notes QA

Duree : environ 2 h. Desinstaller le namespace `claude-guide-test` et supprimer `/share/MFS/ROOT/CLAUDE-GUIDE-TEST` (apres accord de l'administrateur : PV) a la fin. Les ecarts relevés sont transmis a doc-updater.
