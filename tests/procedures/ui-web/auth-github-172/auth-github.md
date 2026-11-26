# Procedure de Test — Acces authentifie du launcher de cluster : GitHub, compte local, R1, audit (#172, #251, #252)

**Version** : 7.0.0
**Date** : 2026-11-26
**Testeur** : QA (scenarios AUTO) + utilisateur (scenarios HUMAIN)

Reference : `contracts/auth.md` (§1 zones de confiance, §2 mode `proxy`, §4 regle R1, §5 audit, §6 pages, §8 compte local), `contracts/helm-values.md` « v7.0.0 », maquettes retenues M1 B (interface), M2 A (pages d'erreur et `/signed-out`), M7 B (connexion a deux colonnes).

## Prerequis

- [ ] Environnement : conteneur de dev (scenario 1) ; **QUALIF** (launcher du cluster en v7.0.0, `auth.enabled: true`, `auth.localAdmin.enabled: true`) pour les scenarios 2 a 12
- [ ] Variables : `<ns>` namespace QUALIF, `<rel>` nom de la release Helm, `<pod>` = `<rel>-0`, `<HOTE>` = hote interne (`ingress.host`), `<URL>` = `https://<HOTE>`
- [ ] Donnees : **aucun projet reel** ; compte GitHub administrateur (`auth.admins`), compte GitHub **non liste**, et si disponible un compte GitHub `auth.users` (scenarios HUMAIN). Les applications GitHub (M4 a M6) etant fournies par l'utilisateur, tant qu'elles manquent les scenarios GitHub (7 a 10, 12) sont reportes ; le compte local suffit a jouer 2 a 6 et 11
- [ ] QUALIF pour l'expiration (scenario 10) : `auth.sessionDuration: 15m` et `auth.sessionRefresh: 5m`, remis ensuite aux valeurs par defaut
- [ ] Acces : `kubectl` (exec, logs, get secret) sur `<ns>` ; `curl` ; `go`, `node`, `helm`, `chrome-headless-shell` (scenario 1)
- [ ] **Login GitHub : le copier avec sa casse exacte** (`CCoupel`, tel que GitHub l'affiche) dans `auth.admins` / `auth.users` : oauth2-proxy compare `--github-user` en respectant la casse (le chart la conserve), alors que le launcher compare sans casse ; un login saisi en minuscules ne passerait pas le proxy
- [ ] Mot de passe local : `PW=$(kubectl -n <ns> get secret <rel>-local-admin -o jsonpath='{.data.password}' | base64 -d)` — ne jamais l'afficher dans un rapport ni l'ecrire dans un fichier

## Scenarios

### Scenario 1 — Go, chart et navigateur sans tete (automatise)

**Validation** : AUTO — **Issues** : #172 #251 #252

```bash
cd ng2 && go test -race -count=1 -run 'Auth172|Audit251|Proxy172|R1172' ./core/ ./ui/desktop/ && go test -count=1 -run 'ChartAuth172|SignIn172|ErrorsMiddlewareOnly429' . && cd .. && tests/integration/ui-web/auth-proxy-172/run.sh && echo OK
```

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Commande ci-dessus | identite par `X-Forwarded-User` (401 absent/vide/invalide/multiple, 403 inconnu, `admin@local` admis seulement sur l'hote interne) ; aucun jeton ni cookie du launcher ; controles Host/Origin/JSON inchanges ; `init.user` / `clientInfo.user` ; regle R1 recentree (serveur et options globales : coffre, known_hosts, config.conf / env.yaml / hub, champs R1 de backends.yaml, relaunch, login) refusee a un `user` avec `forbidden: admin`, `user` libre sur projets et backends, methode inconnue = R1, enumeration des `case` du dispatch, refus du mode proxy (quit, webUI*, clipboard*, teamAgentSet, secretReveal) ; audit (une entree par appel, agregation, > 60 appels/min, `deny`, `target`, copie sur la sortie standard, 5 generations de 10 Mio, XFF 256 octets, echec = R1 refusees) ; port de sante ; refus de demarrer ; chart (sidecar, secrets, compte local, refus de rendu, `rateLimit`) ; navigateur : pastille, menu, deconnexion, `user` en lecture seule, modale de session expiree | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Aucun en-tete d'identite forge n'est cru (critical)

**Validation** : AUTO — **Issues** : #172 #251

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `curl -si <URL>/api/call -H 'X-Forwarded-User: ccoupel' -H 'Content-Type: application/json' -d '{"method":"clientInfo","args":{}}'` (depuis le poste, sans cookie) | `401` ou redirection vers `/oauth2/…` : **jamais** `200` ; le corps ne contient aucune donnee du launcher | | |
| 2 | `kubectl -n <ns> exec <pod> -c launcher -- sh -c 'wget -S -qO- --header="X-Forwarded-User: ccoupel" http://127.0.0.1:8080/api/ 2>&1 \| head -3'` (ou `curl -si`), vers le **proxy** du pod, en-tete forge | refus de oauth2-proxy (`401`), pas de reponse du launcher (pas d'en-tete `X-Launcher-Refusal`) | | |
| 3 | Depuis un autre pod du namespace : `kubectl -n <ns> run t172 --rm -i --restart=Never --image=curlimages/curl --command -- curl -sm 5 -o /dev/null -w '%{http_code}' http://<IP du pod>:8081/` puis `:8080` avec l'en-tete forge | `8081` injoignable (refus/delai), `8080` : `401` ; aucune reponse `200` | | |
| 4 | Sur le port de sante : meme pod, `curl -sm 5 http://<IP du pod>:8082/healthz` puis `/api/call`, `/` | `healthz` : `ok` ; toute autre route : `404` sans contenu | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — Connexion par le compte local (formulaire) et identite

**Validation** : AUTO — **Issues** : #172 #251

`J=$(mktemp -p _work/tmp)` (cookie jar, supprime a la fin).

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `curl -s -c $J -o /dev/null -w '%{http_code}' -L <URL>/` | page de connexion oauth2-proxy (`200`, deux colonnes : texte « Compte local » present) | | |
| 2 | `curl -s -c $J -b $J -o /dev/null -w '%{http_code}' -d "username=admin@local&password=$PW" <URL>/oauth2/sign_in` | redirection (`302`) vers `/` ; cookie `_cl_auth` pose (`HttpOnly`, `Secure`, `SameSite=Lax`) | | |
| 3 | `curl -s -b $J -H 'Content-Type: application/json' -d '{"method":"clientInfo","args":{}}' <URL>/api/call` | `user.login` = `admin@local`, `user.role` = `admin`, `remote` = `true`, `logout` = `/oauth2/sign_out?rd=%2Fsigned-out` | | |
| 4 | `curl -s -b $J -H 'Content-Type: application/json' -d '{"method":"configSave","args":{}}' <URL>/api/call` | **pas** de `forbidden` (admin : R1 permise ; echec metier d'arguments accepte) | | |
| 5 | `curl -s -b $J -o /dev/null -w '%{http_code}' '<URL>/oauth2/sign_out?rd=%2Fsigned-out'` puis `curl -s -b $J -o /dev/null -w '%{http_code}' <URL>/api/call -d '{}'` | deconnexion : `302` vers `/signed-out` ; l'appel suivant sans cookie valide : `401` | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — Mauvais mot de passe et rafale d'essais (429)

**Validation** : AUTO — **Issues** : #172

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `curl -s -c $J -o /dev/null -w '%{http_code}' -d 'username=admin@local&password=mauvais' <URL>/oauth2/sign_in` | pas de cookie `_cl_auth` ; page de connexion reaffichee avec « Identifiant ou mot de passe incorrect. » (statut 401 ; absent a la premiere visite, statut 200) ; ce message n'est jamais remplace par la page « Trop de tentatives » (seul le 429 l'est) | | |
| 2 | Rafale : `for i in $(seq 30); do curl -s -o /dev/null -w '%{http_code}\n' -d 'username=admin@local&password=mauvais' <URL>/oauth2/sign_in; done \| sort \| uniq -c` | au plus ~20 reponses normales (rafale `burst`), les suivantes **`429`** ; au moins un `429` ; le corps d'un `429` est la page « Trop de tentatives » du launcher (`/too-many`, middleware `errors`), sans identite | | |
| 3 | Attendre 1 minute puis un essai avec le bon mot de passe | connexion de nouveau possible (pas de blocage definitif) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 5 — Le mot de passe local n'apparait ni dans les journaux, ni dans l'audit, ni dans NOTES (critical)

**Validation** : AUTO — **Issues** : #172 #251

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `kubectl -n <ns> logs <pod> -c auth-proxy \| grep -cF "$PW"` ; idem `-c launcher` ; idem `--previous` s'il existe | `0` pour chacun ; l'identifiant `admin@local` et le resultat (succes/echec) apparaissent dans le journal `auth-proxy` | | |
| 2 | `kubectl -n <ns> exec <pod> -c launcher -- sh -c 'grep -cF "$0" "$CL_CACHE_DIR/audit.jsonl"' "$PW"` | `0` ; les lignes portent `"user":"admin@local"` | | |
| 3 | `helm -n <ns> get notes <rel> \| grep -cF "$PW"` ; `helm -n <ns> get values <rel> \| grep -cF "$PW"` ; `helm -n <ns> get manifest <rel> \| grep -cF "$PW"` | `0` pour chacun (le Secret n'est pas dans le manifeste en clair : seulement sa reference) ; les notes donnent la **commande** de lecture, jamais la valeur | | |
| 4 | `kubectl -n <ns> exec <pod> -c launcher -- env \| grep -cF "$PW"` | `0` | | |
| 5 | `kubectl -n <ns> exec <pod> -c launcher -- ls -l "$CL_CACHE_DIR/audit.jsonl"` | mode `-rw-------` ; hors du coffre (`ls "$CL_SECRETS_DIR"` ne le contient pas) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 6 — Audit et journal des secrets nominatifs

**Validation** : AUTO — **Issues** : #251 #252

Session du scenario 3 (cookie `admin@local`) rejouee, puis lecture du journal.

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Appels `refresh` (non R1) puis `configSave {}` (R1) avec le cookie, puis `kubectl … exec … -c launcher -- tail -n 20 "$CL_CACHE_DIR/audit.jsonl"` | une ligne par appel : `ts`, `user` = `admin@local`, `role` = `admin`, `src` (adresse du pod proxy + `X-Forwarded-For`), `method`, `ok` ; **aucun argument**, aucune valeur, aucun chemin interne | | |
| 2 | 30 appels `termInput` (ou `init`) puis attendre 1 minute et relire | **une** ligne `"kind":"agg"` avec `count` ≈ 30 et `since`, pas 30 lignes | | |
| 3 | `backendSecretSet` avec un backend factice et une valeur factice (admin), puis `grep '"user"' "$CL_CACHE_DIR/secrets-journal.jsonl" \| tail -1` | la ligne porte `user: "admin@local"` et la source ; **aucun contenu** de la valeur | | |
| 4 | `kubectl … exec … -- sh -c 'rm -f $CL_CACHE_DIR/audit.jsonl; mkdir $CL_CACHE_DIR/audit.jsonl'` puis `configSave {}` en admin | `{"ok":false,"msg":"journal d'audit inaccessible"}` ; un appel non R1 (`refresh`) continue ; rien n'est ecrit ; **remettre** : `rmdir $CL_CACHE_DIR/audit.jsonl` | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 7 — Connexion GitHub reelle (administrateur)

**Validation** : HUMAIN — **Issues** : #172 #251 (parcours reel, lisibilite de la redirection et de la pastille)

Applications GitHub de la QUALIF fournies ; navigateur sans session du launcher.

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ouvrir `<URL>/` dans un navigateur (fenetre privee) | page de connexion (M7 B : deux colonnes « Compte GitHub » / « Compte local ») ; texte clair, sans ressource externe cassee | | |
| 2 | « Se connecter avec GitHub », autoriser l'application avec le compte administrateur | retour sur l'interface du launcher, sans saisie supplementaire ; aucun jeton dans la barre d'adresse | | |
| 3 | Lire la ligne de version | pastille avec le login GitHub ; menu : « Connecté avec GitHub », « administrateur », « Se déconnecter » | | |
| 4 | Ouvrir les Reglages, modifier puis enregistrer un reglage inoffensif, le remettre | ecritures permises (aucun grisage) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 8 — Compte GitHub non autorise (page M2)

**Validation** : HUMAIN — **Issues** : #172 (page d'erreur « Accès refusé »)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Fenetre privee, se connecter avec un compte GitHub **absent** de `auth.admins` et `auth.users` | page « Accès refusé » en francais (M2 A), compte GitHub nomme, bouton « Se déconnecter » ; aucune donnee du launcher | | |
| 2 | « Se déconnecter » | arrivee sur `/signed-out` | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 9 — Deconnexion

**Validation** : HUMAIN — **Issues** : #172 #251 (page `/signed-out`, lisibilite)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Connecte (scenario 7 ou 11), menu de la pastille › « Se déconnecter » | page `/signed-out` : « déconnecté du launcher », la session GitHub du navigateur reste ouverte (dit par la page), bouton « Se reconnecter » | | |
| 2 | « Se reconnecter » | retour a la connexion ; peut aboutir sans saisie (session GitHub), comme annonce | | |
| 3 | Bouton « Precedent » du navigateur apres la deconnexion | aucune donnee du launcher visible (la page n'est plus servie sans identite) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 10 — Expiration de la session

**Validation** : HUMAIN — **Issues** : #172 #251 (modale, terminaux figes)

QUALIF avec `auth.sessionDuration: 15m`, `auth.sessionRefresh: 5m` ; un terminal de projet factice ouvert.

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Se connecter, ouvrir un terminal, attendre l'expiration (supprimer le cookie `_cl_auth` dans les outils du navigateur pour aller plus vite) puis cliquer dans l'interface | modale bloquante « Session expirée — Se reconnecter » (M1 B) ; terminaux affiches mais figes | | |
| 2 | Taper au clavier dans le terminal fige | rien n'est envoye (verifier : aucune frappe dans le pod projet) | | |
| 3 | « Se reconnecter » | rechargement, connexion, interface a jour | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 11 — Connexion `admin@local` par le formulaire (M7 B)

**Validation** : HUMAIN — **Issues** : #172 #251 (page de connexion a deux colonnes, menu « Compte local »)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Hote interne, fenetre privee : page de connexion | deux colonnes de meme poids ; formulaire identifiant / mot de passe a droite ; lisible, sans debordement | | |
| 2 | Saisir `admin@local` et le mot de passe lu par `kubectl` | interface ; pastille `admin@local` ; menu « Compte local », « administrateur » | | |
| 3 | Reglages : ecriture permise | aucun grisage (admin) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 12 — Mauvais mot de passe, rafale et role `user`

**Validation** : HUMAIN — **Issues** : #172 #252 (messages, page « Trop de tentatives », lecture seule)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Saisir un mauvais mot de passe | message unique « Identifiant ou mot de passe incorrect. » (sans indiquer lequel est faux), affiche a chaque echec, pas a la premiere visite | | |
| 2 | Valider 25 fois de suite le formulaire (ou laisser le scenario 4 tourner) | page « Trop de tentatives — réessayer plus tard » lisible (M2) | | |
| 3 | Avec un compte GitHub `auth.users` (si disponible), ouvrir les Reglages | seules les actions du serveur et des options globales (config.conf, env.yaml, hub, coffre et known_hosts des backends, champs R1 de backends.yaml, relance, connexion claude) sont grisees avec l'infobulle « réservé à un administrateur » ; le reste des Reglages, les projets et les backends restent utilisables | | |
| 4 | Toujours en `user` : creer un projet factice (image et backend au choix), le deplacer, modifier un backend (hote ssh, image), lancer `backendTest` | **permis** (le `user` est libre sur ses projets et backends) ; ecrire une cle ssh / un known_hosts, changer `config.conf`, ou ajouter un champ R1 a un backend (`volumes`, `namespace`, `kubeconfig`, `serviceAccountName`, type `host`) : refuse, message R1, rien d'ecrit | | |
| 5 | Toujours en `user` : `quit` (arret du serveur) | refuse ; le serveur continue de repondre | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 13 — Le chemin « Basic » d'oauth2-proxy est ferme (MAJ-1, critical)

**Validation** : AUTO — **Issues** : #172

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `for i in $(seq 30); do curl -s -o /dev/null -w '%{http_code}\n' -u admin@local:faux <URL>/ ; done \| sort \| uniq -c` puis idem sur `<URL>/api/call` | aucune reponse `200` ; l'en-tete `Authorization` est retire avant le proxy (`401` / redirection vers la connexion) ; au moins un `429` sur la rafale | | |
| 2 | `curl -s -o /dev/null -w '%{http_code}' -u "admin@local:$PW" <URL>/api/call -H 'Content-Type: application/json' -d '{"method":"clientInfo","args":{}}'` (**bon** mot de passe, sans passer par le formulaire) | pas d'acces : `401` ou redirection, jamais `200` | | |
| 3 | `kubectl -n <ns> get middleware <rel>-headers -o jsonpath='{.spec.headers.customRequestHeaders}'` | `{"Authorization":""}` ; meme middleware reference par chaque Ingress (`kubectl get ingress -o jsonpath='{..router\.middlewares}'`) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 14 — Sources, reseau et hote backend (MAJ-3, critical)

**Validation** : AUTO — **Issues** : #172 #253

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Depuis un **pod projet** (factice) : `kubectl -n <ns> exec <pod-projet> -- sh -c 'for p in 8080 8081 8082 8083; do timeout 5 wget -qO- --header="X-Forwarded-User: <login admin>" http://<IP du pod launcher>:$p/ >/dev/null 2>&1; echo "$p=$?"; done'` | les quatre ports refuses ou sans reponse (NetworkPolicy : entree depuis le controleur d'Ingress seulement) ; aucune reponse du launcher | | |
| 2 | `curl -s -o /dev/null -w '%{http_code}' -H 'Host: launcher.local' https://<IP publique de l'Ingress>/ -k` | `403` (hote refuse) ou refus de l'Ingress ; jamais l'interface | | |
| 3 | `kubectl -n <ns> exec <pod> -c launcher -- env \| grep -c CL_ALLOW_HOST_BACKEND` | `0` : le backend `host` est absent derriere le proxy | | |
| 4 | `helm upgrade <rel> … --set allowHostBackend=true --dry-run` | refus : « backend host interdit derriere le proxy d'authentification » | | |
| 5 | Hote avec casse et point final : `curl -s -o /dev/null -w '%{http_code}' --resolve ... -H 'Host: <HOTE en MAJUSCULES>.' ` avec un cookie `admin@local` | memes decisions que sans variation (admis sur l'hote interne ; jamais admis sur l'externe) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 15 — Cookie de l'instance interne rejoue sur l'hote externe (m1)

**Validation** : AUTO — **Issues** : #172 #254 (si l'ingress externe est active en QUALIF ; sinon reporte au LOT-3)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Connexion `admin@local` sur l'hote interne (scenario 3), relever la valeur du cookie `_cl_auth` | session interne valide | | |
| 2 | Rejouer ce cookie sur l'hote externe : `curl -s -o /dev/null -w '%{http_code}' -H "Cookie: _cl_auth=<valeur>" https://<hote externe>/api/call -d '{}'` (et sous le nom `_cl_auth_ext`) | `401` / redirection : aucune session ; le compte local est refuse aussi par le launcher (`403`, `X-Launcher-Refusal: user`) si l'en-tete arrivait | | |
| 3 | `curl -s https://<hote externe>/oauth2/sign_in` | pas de formulaire de mot de passe (colonne GitHub seule) | | |
| 4 | `kubectl -n <ns> get secret <rel>-auth-cookie <rel>-auth-cookie-ext -o name` | deux Secrets, valeurs differentes | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 16 — 100 000 appels : les entrees de l'administrateur survivent (MAJ-4)

**Validation** : AUTO — **Issues** : #251

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Marquer : une ecriture R1 identifiable de l'admin (`configSave` `config.conf` sans changement, scenario 6 etape 1) | entree dans `audit.jsonl` et sur la sortie du conteneur | | |
| 2 | `seq 100000 \| xargs -P 16 -I{} curl -s -o /dev/null -b $J -H 'Content-Type: application/json' -d '{"method":"refresh","args":{}}' <URL>/api/call` (cookie du scenario 3) | le serveur reste disponible ; **au plus** quelques dizaines de milliers d'octets ajoutes par minute (appels > 60/min agreges) | | |
| 3 | `kubectl -n <ns> logs <pod> -c launcher \| grep '^audit ' \| grep -c 'configSave'` | l'entree de l'etape 1 est presente dans la sortie standard (collectee, non effacable par l'application), meme si `audit.jsonl` a tourne | | |
| 4 | `kubectl … exec … -- ls -l "$CL_CACHE_DIR" \| grep audit` | au plus `audit.jsonl` et `.1` a `.5` ; taille totale bornee (~60 Mio) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 17 — Rotation du mot de passe local invalide les sessions (m5)

**Validation** : AUTO — **Issues** : #172

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Cookie valide `admin@local` (scenario 3), puis `kubectl -n <ns> delete secret <rel>-local-admin <rel>-auth-cookie` et `helm upgrade <rel> … ` (memes valeurs) | pod redemarre avec de nouveaux Secrets | | |
| 2 | Rejouer l'ancien cookie | `401` / redirection (session invalidee) | | |
| 3 | Ancien mot de passe sur le formulaire | echec ; le **nouveau** (relu par `kubectl`) ouvre une session | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 18 — Charge ambigue (casse, doublons de cles) : R1 non contournable (CRIT-1, critical)

**Validation** : AUTO — **Issues** : #172 #252

Cookie d'un compte `user` (GitHub, fourni par l'utilisateur) dans `$JU`. Contenus valides et reconnaissables (marqueur `POC-CRIT-1`) ; `<projet>` = projet factice.

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `curl -s -b $JU -H 'Content-Type: application/json' -d '{"method":"configSave","args":{"kind":"project-env","Kind":"config.conf","path":"<projet>","content":"x","CONTENT":"# POC-CRIT-1"}}' <URL>/api/call` | `400` (cles ambigues) ou `forbidden: admin` ; **rien d'ecrit** | | |
| 2 | Idem avec `{"kind":"project-env","kind":"config.conf",…}` (cle en double), `KIND":"env.yaml"`, et `configApply` avec `Changes` / `CHANGES` sur `backends.yaml` (`pod.serviceAccountName`, `volumes`, `type: host`) | meme resultat a chaque fois | | |
| 3 | `kubectl -n <ns> exec <pod> -c launcher -- sh -c 'grep -rl POC-CRIT-1 "$CL_CONFIG_DIR" ~/.config 2>/dev/null; echo fin'` | aucun fichier ne contient le marqueur (seule la ligne `fin`) | | |
| 4 | Journal : `kubectl … exec … -- tail -n 5 "$CL_CACHE_DIR/audit.jsonl"` | refus journalises (`ok:false`, `err:forbidden`, `target` court) ou `400` sans execution ; jamais un `ok:true` de ce user pour une methode R1 | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 19 — Les fichiers de projet gardent leur CSP `sandbox` derriere Traefik (CRIT-2, critical)

**Validation** : AUTO — **Issues** : #172

Projet factice contenant `x.html` (page statique) et `evil.html` (script : `fetch('/api/call',{method:'POST',headers:{'Content-Type':'application/json'},body:'{"method":"clientInfo","args":{}}'}).then(r=>document.title='LU:'+r.status).catch(e=>document.title='BLOQUE')`).

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `curl -sI -b $J <URL>/files/<projet>/x.html \| grep -i content-security-policy` (cookie admin du scenario 3) | l'en-tete contient **`sandbox`** (`allow-scripts …` : voir `contracts/web-server.md`) ; pas seulement `frame-ancestors` | | |
| 2 | Idem sur l'hote externe si active (`<URL externe>`) | meme CSP `sandbox` | | |
| 3 | `curl -sI <URL>/ \| grep -i content-security-policy` et `curl -sI <URL>/oauth2/sign_in \| grep -i content-security-policy` | `/` : aucun CSP du middleware Traefik (celui du launcher, `frame-ancestors 'none'`) ; pages du proxy : CSP strict | | |
| 4 | Navigateur sans tete ou onglet : ouvrir `<URL>/files/<projet>/evil.html` | le titre devient `BLOQUE` ou `LU:403` : la page ne s'execute **pas** dans l'origine du launcher (origine opaque), aucun appel authentifie n'aboutit | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 20 — Inondation de refus R1 et champs geants (MAJ-A)

**Validation** : AUTO — **Issues** : #251

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Marquer une ecriture R1 de l'admin (scenario 6 etape 1), puis `seq 100000 \| xargs -P 16 -I{} curl -s -o /dev/null -b $JU -H 'Content-Type: application/json' -d '{"method":"configSave","args":{"kind":"config.conf","content":"x"}}' <URL>/api/call` (compte `user`) | le serveur repond ; au plus 60 refus par minute ecrits ligne a ligne, le reste agrege ou ecarte | | |
| 2 | `kubectl -n <ns> exec <pod> -c launcher -- sh -c 'ls -l $CL_CACHE_DIR \| grep audit; grep -c POC-ADMIN "$CL_CACHE_DIR"/audit.jsonl*'` | taille totale du journal bornee (loin des 60 Mio) ; l'ecriture R1 de l'admin y figure toujours | | |
| 3 | `curl -s -b $J -H 'Content-Type: application/json' -d "{\"method\":\"$(head -c 1000000 /dev/zero \| tr '\0' M)\",\"args\":{}}" <URL>/api/call -o /dev/null -w '%{http_code}'` puis `tail -c 2000 "$CL_CACHE_DIR/audit.jsonl"` | la ligne d'audit est courte, `method` : `unknown` ; `target` et `project` d'une autre methode : 128 caracteres au plus | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 21 — `backendTest` : un user teste un backend declare, pas des champs R1 (revue, majeur)

**Validation** : AUTO — **Issues** : #252

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Compte `user` : `backendTest` du backend declare, tel quel (champs de connexion et R1 identiques) | permis : etapes du diagnostic renvoyees | | |
| 2 | Compte `user` : `backendTest` du meme backend avec un `context`, un `namespace`, un `kubeconfig`, des `volumes` ou un `host` differents, ou un nouveau backend portant un champ R1 | refuse (`forbidden: admin`) ; **aucun** `kubectl` / `ssh` lance dans le pod (`kubectl logs … -c launcher` sans trace de commande ; `ps` du conteneur) | | |
| 3 | Compte `admin` : memes appels | permis | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 22 — Le flux d'evenements est revalide

**Validation** : AUTO — **Issues** : #172 #251

Mesure : `auth.sessionRefresh: 5m` en QUALIF (sinon valeur par defaut).

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `curl -sN -b $J <URL>/api/events -o /dev/null -w '%{time_total}'` (cookie admin) | le serveur ferme le flux apres `sessionRefresh` environ ; le client web se reconnecte | | |
| 2 | Retirer le login de `auth.admins` (`helm upgrade`), rouvrir `/api/events` avec l'ancien cookie | `403` (`X-Launcher-Refusal: user`) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

## Criteres de Validation

- [ ] Aucun en-tete d'identite forge n'est accepte ; le port `8081` n'est pas joignable hors du pod
- [ ] Le compte local ouvre une session admin, les essais sont limites (`429`), le mot de passe n'apparait nulle part
- [ ] La regle R1 (serveur et options globales) refuse ces ecritures a un `user`, libre sur ses projets et backends ; l'audit est nominatif, sans argument, copie sur la sortie standard
- [ ] Le chemin `Authorization: Basic` est ferme ; le cookie interne n'ouvre rien sur l'hote externe ; la rotation invalide les sessions
- [ ] Les pages de connexion, d'erreur, de deconnexion et la modale d'expiration sont lisibles

## Notes QA

[Espace pour observations]
