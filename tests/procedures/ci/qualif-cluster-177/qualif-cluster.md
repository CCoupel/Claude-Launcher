# Procedure de Test — QUALIF de bout en bout du launcher du cluster (#177 ; #167, #168, #170, #171, #175, #176)

**Version** : 6.3.0
**Date** : 2026-11-17
**Testeur** : QA (scenarios AUTO) et utilisateur (scenarios HUMAIN) — avec l'administrateur du cluster pour les prerequis

Cette procedure enchaine toute la QUALIF du launcher du cluster. Elle **reprend sans les recopier** :
`tests/procedures/ci/image-launcher-167/image-launcher.md` (image : utilisateur, version, outils, point d'entree, arret propre) et
`tests/procedures/ci/chart-qualif-170/qualif-chart.md` (exposition, liste d'adresses H3, PVC/PV, isolation QUALIF/PROD).
Commandes de deploiement : `.claude/agents/environments/deploy.qualif.md`. Tests sans cluster : `go test ./ng2/...` et
`scripts/ci.sh` (dont `TestChart*`).

## Environnement de QUALIF (decisions en vigueur)

| | QUALIF |
|---|---|
| Namespace | `claude-qualif` (PROD : `claude-ng2`, jamais touche) |
| Racine | `/share/MFS/ROOT/CLAUDE-QUALIF` (PROD : `/share/MFS/ROOT/CLAUDE_PROJETS`, jamais touchee) |
| Volume | PVC `claude-projects-qualif` cree par le chart, lie au PV `claude-qualif-pv` (administrateur) |
| Nom de hub / release | `cluster-qualif` / `claude-launcher-qualif` |
| Image | `192.168.1.211:5000/claude-launcher:v<X.Y.Z.a>` (registre interne, HTTP, sans digest : risque accepte H2) |
| Acces | `https://launcher-qualif.coupel.lan` (Ingress Traefik, `192.168.1.0/24`) |
| Projets | factices seulement : `<racine>/PROJETS/QUALIF/factice-1`, `factice-2` (faux `claude`) |

## Prerequis (verifies avant tout, un par un)

- [ ] **Administrateur** (fiche `_work/reports/infra-admin-noeuds-20261117-090000.md`) : namespace `claude-qualif` ; PSA `baseline` sur `claude-ng2` **et** `claude-qualif` ; PV `claude-qualif-pv` vers `/share/MFS/ROOT/CLAUDE-QUALIF` (**source verifiee** : jamais `CLAUDE_PROJETS`) ; compte `ng2-deploy` dans les deux namespaces ; DNS `launcher-qualif.coupel.lan` ; `externalTrafficPolicy: Local` sur le Service Traefik ; ClusterIssuer interne reel (nom dans `PRIVATE/QUALIF/values-local.yaml`) ; registre `192.168.1.211:5000` demarre et noeuds configures pour le tirage HTTP
- [ ] **Acces** : `PRIVATE/QUALIF/ng2-deploy.kubeconfig` (`kubectl -n claude-qualif auth can-i create statefulsets` = `yes`) ; un poste dans `192.168.1.0/24`, un poste **hors** de la plage, un pod de test
- [ ] **Racine de QUALIF** : dossier cree (proprietaire `1000:1000`), `backends.yaml` de QUALIF (memes types que la PROD, `k8s` → `claude-qualif`, volumes → `claude-projects-qualif`), projets factices avec faux `claude` — voir `deploy.qualif.md`
- [ ] **Sauvegarde** horodatee des fichiers de la racine de PROD (`config.conf`, `backends.yaml`, `projects.json`, jeton) pour la comparaison finale (scenario 12)
- [ ] Binaire candidat compile (`deploy/image/build.sh v<X.Y.Z.a>`) depuis le **HEAD du milestone**

## Scenarios

### Scenario 1 — CI : tests, chart et image (budgets D1)

**Validation** : AUTO — **Issues** : #177, #179

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Trois passes de `scripts/ci.sh` dans le conteneur de developpement (voir `tests/procedures/ci/ci-60s-179/`) | Trois passes vertes ; duree de chaque etape affichee ; **hors etape image** : moyenne <= 60 s | | |
| 2 | Duree de l'**etape image a chaud** (relevee dans la ligne `durees`) | <= 20 s (budget D1) ; a froid : relevee, sans budget | | |
| 3 | `helm lint --strict` + rendu : les 29 `TestChart*` verts (`go test -run TestChart ./ng2`) | Verts ; chaque refus (adresses, `rootBase`, valeurs reservees, TLS, nom du PV…) prouve par `helm template` | | |
| 4 | Garde : sans `buildah` ou sans `helm`, `ci.sh` sous Linux | **Echec explicite** (message nommant l'outil), jamais un saut silencieux ; sous Windows : pas d'image | | |
| 5 | Garde : l'image construite par `ci.sh` est supprimee apres usage (stockage vfs) | `buildah images` ne garde pas d'image de travail apres la passe | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Image candidate construite et publiee dans le registre interne

**Validation** : AUTO — **Issues** : #167, #177

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `deploy/image/build.sh v<X.Y.Z.a>` | Binaire statique par la commande de `release.yml`, image locale ; durees affichees | | |
| 2 | Controles de l'image : scenarios 1 a 3 de `image-launcher-167/image-launcher.md` | Non root `1000:1000`, version = tag, `git gh kubectl bash` presents, ni `tmux` ni `claude`, entrypoint 64/66, interface servie avec dossier monte, `config.conf` cree sous la racine montee, aucun jeton imprime, SIGTERM propre | | |
| 3 | Pousser : `sudo buildah --storage-driver vfs push --tls-verify=false localhost/claude-launcher:v<X.Y.Z.a> docker://192.168.1.211:5000/claude-launcher:v<X.Y.Z.a>` | Pousse ; `curl http://192.168.1.211:5000/v2/claude-launcher/tags/list` liste le tag | | |
| 4 | Le tag de PRODUCTION n'est pas touche | Aucun tag `vX.Y.Z` (sans `.a`) cree ni modifie ; rien sur ghcr | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — Installation et premier demarrage

**Validation** : AUTO — **Issues** : #167, #170

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `helm upgrade --install claude-launcher-qualif deploy/helm/claude-launcher -n claude-qualif -f deploy/helm/values-qualif.yaml -f PRIVATE/QUALIF/values-local.yaml --set image.tag=v<X.Y.Z.a>` (kubeconfig `ng2-deploy`) | Installation sans erreur ; `kubectl rollout status` | | |
| 2 | `kubectl -n claude-qualif get pod,pvc,svc,ingress,certificate` | **Un seul** pod `Ready` (sonde `/readyz`) ; PVC `claude-projects-qualif` `Bound` RWX sur `claude-qualif-pv` ; Service `ClusterIP` ; Certificate `Ready` | | |
| 3 | Journaux du pod | Message de mode serveur, **aucun jeton en clair**, aucun refus « launcher deja actif » | | |
| 4 | `<racine>/.config/claude-launcher-ng2/config.conf` | Cree au premier demarrage ; chemins sous la racine de QUALIF | | |
| 5 | Droits : `kubectl auth can-i --list --as=system:serviceaccount:claude-qualif:claude-launcher-qualif -n claude-qualif` | Les droits du Role seulement ; rien dans `claude-ng2` ni au niveau du cluster | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — Acces par l'Ingress : autorise et refuse (H3)

**Validation** : AUTO — **Issues** : #170, #171 — voir `qualif-chart.md` scenario 2 (a jouer en entier)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Poste de `192.168.1.0/24` : `curl -sk https://launcher-qualif.coupel.lan/healthz` | `200` | | |
| 2 | IP hors plage ; depuis un pod via l'Ingress ; `X-Forwarded-For` forge | `403` dans les trois cas | | |
| 3 | Lien d'amorçage : `https://launcher-qualif.coupel.lan/?t=<jeton>` (jeton lu par `kubectl exec … cat <CL_CACHE_DIR>/desktop-web-token`) | `303` vers `/`, cookie `__Host-` `Secure`, jeton absent de l'URL et de l'historique | | |
| 4 | Un autre nom d'hote / une autre origine (`curl -H 'Host: evil.test'`, `Origin` etrangere) | `403` distincts (« hote refuse », « origine refusee ») | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 5 — Projet factice : ouvrir, utiliser, fermer

**Validation** : HUMAIN — **Issues** : #168, #175

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ouvrir l'interface de QUALIF (poste autorise) ; l'arbre montre `QUALIF/factice-1` et `factice-2` | Seuls les factices (aucun projet de PROD) | | |
| 2 | Ouvrir `factice-1` (backend `k8s`) | StatefulSet et pod crees **dans `claude-qualif`** ; terminal affiche l'invite du faux `claude` ; saisie et affichage fonctionnent | | |
| 3 | Utiliser : saisir, redimensionner, onglet teammates, carte GITHUB | Aucune erreur ; evenements a jour sans recharger | | |
| 4 | Connexion claude.ai / consommation depuis le pod | Message adapte au pod (la connexion se fait dans un projet ouvert) ; sorties reseau bloquees : message clair (claude.ai, GitHub) | | |
| 5 | Fermer le projet (Suppr / fermeture forcee) | StatefulSet et pod supprimes ; `kubectl -n claude-ng2 get sts` **inchange** | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 6 — « Tester » le backend k8s depuis le pod

**Validation** : HUMAIN — **Issues** : #168

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Reglages > Backends > `k8s` > Tester | Etapes vertes : cluster joignable (ServiceAccount du pod), droits un par un, PVC `Bound` `ReadWriteMany`, PVC monte dans le pod du launcher ; ligne d'information « contexte « k8s » ignore » | | |
| 2 | Retirer un droit au Role (`kubectl edit role`) puis retester | « droit manquant : <verbe> <ressource> » + conseil `helm upgrade` ; restaurer ensuite (`helm upgrade`) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 7 — Pod recree : jeton et reglages conserves

**Validation** : AUTO — **Issues** : #167, #169

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Noter le jeton, un reglage (ex. couleur d'un projet), les `backends.yaml`/`config.conf` | — | | |
| 2 | `kubectl -n claude-qualif delete pod claude-launcher-qualif-0` ; attendre `Ready` | Nouveau pod (**nouveau nom de machine** si recree sur un autre noeud) ; meme nom de hub `cluster-qualif`, aucun refus de presence | | |
| 3 | Relire le jeton et les reglages | **Identiques** (jeton conserve dans `<CL_CACHE_DIR>/desktop-web-token`, reglages dans la racine) ; l'ancien lien d'acces fonctionne toujours | | |
| 4 | Projet factice ouvert avant la suppression | Le StatefulSet du projet survit ; le launcher le retrouve et le reprend (identite stable : pas de « piloté par un autre ») | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 8 — Cohabitation avec un autre launcher sur la racine de QUALIF

**Validation** : HUMAIN — **Issues** : #176 — (l'utilisateur n'a plus de hub Linux : l'exe Windows sert de second launcher)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Sur le poste Windows : lancer l'exe avec `--root-base` vers le partage de la racine de QUALIF et `CL_HUB_NAME=win-qualif` | Demarre ; liste les memes factices | | |
| 2 | Lancer un second exe avec `CL_HUB_NAME=cluster-qualif` (meme nom que le pod) | **Refuse de demarrer** avec un message nommant le launcher deja actif | | |
| 3 | Ouvrir `factice-2` depuis le pod, puis depuis l'exe Windows | L'exe signale « projet ouvert par un autre launcher » ; **prise de main** possible sans perte ; l'inverse aussi | | |
| 4 | Modifier un reglage de projet depuis chaque launcher en alternance (10 fois) | `projects.json` et `settings.json` valides, **aucune ecriture perdue** | | |
| 5 | Arreter l'exe ; verifier le pod | Pod inchange ; aucun verrou orphelin (`*.lock`, `hub-*.presence` repris apres peremption) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 9 — Une heure de session a travers le vrai Ingress

**Validation** : HUMAIN — **Issues** : #171 — voir `tests/procedures/ui-desktop/proxy-https-171/endurance-proxy-1h.md` (scenarios 1 et 2, a jouer sur l'Ingress reel au lieu du proxy de test)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Interface ouverte via `launcher-qualif.coupel.lan`, un factice ouvert, 1 h sans action | Aucune erreur ; flux d'evenements vivant malgre le delai de lecture de l'Ingress (`readTimeout` Traefik) ; battement <= 20 s | | |
| 2 | Toutes les 10 min : action dans le terminal du factice | Reponse immediate | | |
| 3 | Couper l'Ingress 30 s (`kubectl -n <ns-traefik> rollout restart deploy/traefik`) | Reconnexion automatique (1,5 s → 15 s) ; terminaux repris | | |
| 4 | Regenerer le jeton depuis le pod (Reglages > Interface web) | Onglet : « session expiree — rouvrir le lien d'acces » ; nouveau lien fonctionnel | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 10 — Mise a jour par changement de tag (mode notify)

**Validation** : AUTO — **Issues** : #167, #170, #176

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Construire et pousser un second tag candidat `v<X.Y.Z.b>` ; interface de QUALIF ouverte | Annonce « ↑ NG v<…> disponible · mise a jour par l'image », **sans** bouton d'installation | | |
| 2 | `helm upgrade --install … --set image.tag=v<X.Y.Z.b>` en surveillant `kubectl get pod -w` | **L'ancien pod est arrete avant le demarrage du nouveau** (un seul pod a tout instant : pas de RollingUpdate) ; aucun refus « launcher deja actif » | | |
| 3 | Version du nouveau pod : `kubectl exec … -- claude-launcher-ng2 version` | `v<X.Y.Z.b>` | | |
| 4 | Jeton, reglages, factices apres la mise a jour | Conserves ; session de l'interface a rouvrir (lien d'acces inchange) | | |
| 5 | Retour au tag precedent (`helm rollback` ou `--set image.tag=…`) | Fonctionne de la meme facon | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 11 — Desinstallation

**Validation** : AUTO — **Issues** : #170

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Fermer les projets factices ; `helm uninstall claude-launcher-qualif -n claude-qualif` | Objets du chart retires (StatefulSet, Service, Ingress, Role, RoleBinding, ServiceAccount, Certificate/Middleware) | | |
| 2 | `kubectl -n claude-qualif get pvc` ; `kubectl get pv claude-qualif-pv` | **PVC `claude-projects-qualif` et PV conserves** (`helm.sh/resource-policy: keep`) ; fichiers de la racine intacts | | |
| 3 | Reinstaller (scenario 3) | Reprise avec le meme PVC, memes reglages, meme jeton | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 12 — Isolation QUALIF / PROD

**Validation** : AUTO — **Issues** : #170, #176 — voir `qualif-chart.md` scenario 5 (a jouer en entier)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Source du PV, `ls /share/MFS/ROOT`, `test ! -e` des fichiers de PROD depuis le pod | Racine de PROD invisible, aucun fichier de PROD lisible | | |
| 2 | Comparer les fichiers de la racine de PROD a la sauvegarde d'avant | **Identiques** ; le launcher de PROD n'a pas ete interrompu ; `kubectl -n claude-ng2 get all` inchange | | |
| 3 | Noms de hub `cluster` (PROD) et `cluster-qualif` distincts | Les deux coexistent sans refus | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] CI verte (<= 60 s hors image, image a chaud <= 20 s), chart controle par `helm template`
- [ ] Image candidate non root, publiee dans le registre interne, tag de production intact
- [ ] Installation, acces autorise, refus hors plage / depuis un pod / `X-Forwarded-For` forge
- [ ] Projet factice ouvert, utilise, ferme dans `claude-qualif` ; « Tester » nomme chaque droit
- [ ] Pod recree : jeton et reglages conserves ; cohabitation sans perte d'ecriture ; un seul pilote
- [ ] Une heure a travers l'Ingress reel ; reconnexion apres coupure
- [ ] Mise a jour par tag : ancien pod arrete avant le nouveau ; desinstallation : PVC/PV conserves
- [ ] PROD intacte (fichiers identiques a la sauvegarde, aucun objet modifie dans `claude-ng2`)

## Notes QA

Duree : environ 3 h (scenario 9 en tache de fond). Risques acceptes (H2 : tag sans digest ; pas de NetworkPolicy ; jeton dans le dossier d'etat) a rappeler dans le guide. Apres la QUALIF : desinstaller si inutile ; la racine de QUALIF est separee, aucune urgence pour la PROD.
