# Procedure de Test — Acces depuis Internet du launcher de cluster : ingress externe, GitHub App (#254, addendum 3)

**Version** : 7.0.0
**Date** : 2026-11-26
**Testeur** : QA (scenarios AUTO) + utilisateur (scenarios HUMAIN)

Reference : `contracts/helm-values.md` « Ingress externe » et `auth.github.clientId` / `clientSecret`, `contracts/auth.md` §2 (adresses publiques, P7), §8 (P10 : jamais de compte local sur l'hote externe), maquettes M2 (pages d'erreur) et M7 B (connexion). Complete `tests/procedures/ui-web/auth-github-172/auth-github.md` (scenarios 7, 8 et 15 : N/T du LOT-2, a jouer ici).

## Prerequis

- [ ] Environnement : **QUALIF** avec `externalIngress.enabled: true` (hote public `<EXT>`, `tls.clusterIssuer` public ou `tls.secretName`), `auth.enabled: true`, compte local active sur l'hote interne `<INT>` ; DNS public et redirection du port 443 vers l'Ingress externe en place
- [ ] **GitHub App de l'environnement** (fournie par l'utilisateur) avec **deux URL de retour** : `https://<INT>/oauth2/callback` et `https://<EXT>/oauth2/callback` ; cles posees par `auth.existingSecret` (recommande) ou par `auth.github.clientId` / `clientSecret` dans `PRIVATE/<ENV>/values-local.yaml` (jamais commite)
- [ ] Un poste **hors du LAN** (reseau mobile, partage de connexion) pour les scenarios HUMAIN ; un compte GitHub administrateur (`auth.admins`) et un compte GitHub non liste
- [ ] Donnees : **aucun projet reel** ; projet factice ouvert (scenario 7) ; mot de passe `admin@local` lu par l'utilisateur seul (`kubectl -n <ns> get secret <rel>-local-admin -o jsonpath='{.data.password}' | base64 -d`), jamais ecrit dans un rapport

## Scenarios

### Scenario 1 — Chart, adresses annoncees et garde de la CI (automatise)

**Validation** : AUTO — **Issues** : #254

```bash
cd ng2 && go test -count=1 -run 'Ext254|Auth254|GithubKeys254|CI254|ChartAuth172' . && go test -count=1 -run 'BrowserEnv254' ./core/ && echo OK
```

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Commande ci-dessus | ingress externe : refus (authentification coupee, hote ou TLS manquants ou invalides, emetteur interne, hote externe dans `auth.localAdmin.hosts`), rendu accepte (Ingress `/` et `/oauth2`, `http-ext`, sans liste d'adresses, redirection HTTPS, `rateLimit`, `CL_WEB_URL` externe et `CL_WEB_ALT_URLS` interne) ; cles GitHub : `existingSecret` seul, `clientId` + `clientSecret` seuls (Secret `<release>-github`), les deux = refus, paire incomplete = refus, valeur absente de `NOTES.txt` et des autres objets ; garde de `scripts/ci.sh` sur un `clientSecret` non vide suivi par git (13 variantes de contournement, placeholders et chemins exclus) ; isolation interne / externe (aucune regle sans hote, l'externe ne vise que `http-ext`, l'interne reste sous `ipAllowList`) ; sommes de controle du pod ; adresse annoncee aux sessions = adresse principale | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — L'hote externe n'offre que GitHub (critical)

**Validation** : AUTO — **Issues** : #254 #172

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `curl -sI https://<EXT>/` | redirection vers `/oauth2/…` (jamais `200` sans session) ; `Strict-Transport-Security` present ; aucune information de version | | |
| 2 | `echo \| openssl s_client -connect <EXT>:443 -servername <EXT> 2>/dev/null \| openssl x509 -noout -issuer -dates` | emetteur **public** (pas `claude-launcher-ca`), dates valides, nom `<EXT>` | | |
| 3 | `curl -s https://<EXT>/oauth2/sign_in \| grep -ci 'type="password"'` | `0` : aucun formulaire de mot de passe sur l'hote externe | | |
| 4 | `curl -s -o /dev/null -w '%{http_code}' -d 'username=admin@local&password=x' https://<EXT>/oauth2/sign_in` puis avec le **bon** mot de passe (lu par `kubectl`, via variable) | refus (`401`/`403`/redirection), jamais de cookie `_cl_auth_ext` ; le bon mot de passe n'ouvre rien non plus | | |
| 5 | `for i in $(seq 30); do curl -s -o /dev/null -w '%{http_code}\n' https://<EXT>/oauth2/sign_in; done \| sort \| uniq -c` | au moins un `429` (limite d'essais de l'ingress externe) ; la page `429` est celle du launcher | | |
| 6 | `curl -s -o /dev/null -w '%{http_code}' -H 'X-Forwarded-User: admin@local' https://<EXT>/api/call -d '{}'` | refus (`401`) ; l'en-tete forge n'est pas cru | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — Adresse annoncee aux sessions claude (P7)

**Validation** : AUTO — **Issues** : #254

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Dans un projet factice ouvert : `kubectl -n <ns> exec <pod projet> -- sh -c 'echo $CL_BROWSER_PATH_MAP; echo $CL_BROWSER_HINT'` | adresses `https://<EXT>/files/<projet>` (l'adresse principale), **jamais** `<INT>` ni `localhost:8765` | | |
| 2 | `kubectl -n <ns> exec <pod launcher> -c launcher -- env \| grep -E 'CL_WEB_URL\|CL_WEB_ALT_URLS\|CL_AUTH_LOCAL_ADMIN_HOSTS'` | `CL_WEB_URL=https://<EXT>`, `CL_WEB_ALT_URLS=https://<INT>`, `CL_AUTH_LOCAL_ADMIN_HOSTS=<INT>` (jamais `<EXT>`) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — Cles de la GitHub App en valeurs Helm : le secret ne fuit pas

**Validation** : AUTO — **Issues** : #254

Variante de deploiement par `auth.github.clientId` / `clientSecret` (fichier `PRIVATE/<ENV>/values-local.yaml`).

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `helm upgrade <rel> … -f PRIVATE/<ENV>/values-local.yaml` puis `kubectl -n <ns> get secret <rel>-github -o jsonpath='{.data}' \| head -c 80` | Secret `<rel>-github` avec `client-id` et `client-secret` ; le pod redemarre et s'authentifie aupres de GitHub | | |
| 2 | `helm -n <ns> get notes <rel> \| grep -cF "<valeur du clientSecret>"` ; `kubectl logs <pod> -c auth-proxy \| grep -cF …` ; idem `-c launcher` | `0` ; les notes citent le nom du Secret et avertissent que `helm get values` affiche la valeur | | |
| 3 | `git ls-files '*.yaml' '*.yml' \| xargs grep -nE '^\s*clientSecret:\s*["'"'"']?[^"'"'"'#[:space:]]'` | aucune sortie : aucun `clientSecret` non vide dans un fichier suivi | | |
| 4 | Remplacer par `auth.existingSecret` (retirer les deux cles) puis `helm upgrade` | accepte ; le Secret `<rel>-github` n'est plus rendu ; poser les deux ensemble est refuse | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 5 — Ouvrir l'adresse depuis Internet

**Validation** : HUMAIN — **Issues** : #254 (page de connexion de l'hote externe, certificat, lisibilite)

Poste **hors du LAN** (reseau mobile), fenetre privee.

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ouvrir `https://<EXT>/` | cadenas valide, aucun avertissement de certificat ; arrivee sur la connexion GitHub **ou** sur la page du launcher listant « Compte GitHub » seul | | |
| 2 | Chercher un moyen de saisir un mot de passe (page de connexion, `https://<EXT>/oauth2/sign_in`) | **aucun** formulaire identifiant / mot de passe, aucune mention du compte local ; page lisible, en francais | | |
| 3 | Taper une adresse inexistante ou `https://<EXT>/oauth2/auth` sans session | refus propre (pas de donnee du launcher, pas de trace technique) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 6 — Connexion GitHub reelle par la GitHub App (hote externe)

**Validation** : HUMAIN — **Issues** : #254 #172 (parcours reel, retour sur l'hote externe)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | « Se connecter avec GitHub », autoriser l'application avec le compte administrateur | retour sur `https://<EXT>/…` (pas sur l'hote interne), sans saisie supplementaire ; aucun jeton dans la barre d'adresse | | |
| 2 | Lire la ligne de version | pastille avec le login GitHub ; menu « Connecté avec GitHub », « administrateur », duree de session | | |
| 3 | Ouvrir un terminal du projet factice, taper une commande inoffensive | terminal reactif depuis Internet ; rien de saisi n'est affiche en clair dans le journal d'audit (frappes agregees) | | |
| 4 | Ouvrir les Reglages, enregistrer un reglage inoffensif | ecriture permise (administrateur) | | |
| 5 | Se deconnecter (menu de la pastille) | page `/signed-out` sur `<EXT>` ; l'hote interne reste dans son etat (sessions separees) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 7 — Compte GitHub non autorise et cookie interne rejoue

**Validation** : HUMAIN — **Issues** : #254 #172 (page « Accès refusé », absence de session croisee)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Fenetre privee, hote externe, connexion avec un compte GitHub **absent** de `auth.admins` et `auth.users` | page « Accès refusé » en francais, compte nomme, « Se déconnecter » ; aucune donnee du launcher | | |
| 2 | Connecte sur l'hote **interne** (`admin@local` ou GitHub), copier le cookie `_cl_auth` (outils du navigateur) ; dans une autre fenetre privee sur l'hote externe, creer ce cookie (`document.cookie` ou outils) puis recharger | aucune session sur l'externe : retour a la connexion GitHub ; le launcher ne repond pas aux appels | | |
| 3 | Meme essai avec le cookie sous le nom `_cl_auth_ext` | idem | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 8 — BLOQUANT avant d'ouvrir 80/443 : l'hote interne n'est pas joignable depuis Internet (audit LOT-3, M-2)

**Validation** : HUMAIN — **Issues** : #254 #172 (a jouer **hors du LAN**, par l'utilisateur ; ne pas ouvrir 80/443 en production tant qu'il echoue)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Depuis un reseau exterieur (mobile) : `curl -sk --resolve <INT>:443:<IP publique> https://<INT>/` | **`403`** (ou refus de connexion / delai) ; **jamais** la page de connexion ni le formulaire `admin@local` | | |
| 2 | Idem sur `https://<INT>/oauth2/sign_in` et `https://<INT>/api/call` | meme refus ; aucune reponse du launcher | | |
| 3 | Relever `kubectl -n <ns-traefik> get svc <traefik> -o jsonpath='{.spec.externalTrafficPolicy}'` | `Local` (l'adresse source est conservee ; sinon la liste d'adresses de l'ingress interne voit une adresse du LAN et laisse passer) | | |
| 4 | `curl -sk -H 'X-Forwarded-For: 192.168.1.20' --resolve <INT>:443:<IP publique> https://<INT>/` depuis l'exterieur | toujours `403` (l'adresse de la connexion TCP decide, jamais l'en-tete) | | |
| 5 | `curl -sk -H 'Host: <INT>' https://<EXT>/` depuis l'exterieur (instance externe, Host de l'interne) | jamais le formulaire du compte local ; refus ou page de connexion GitHub de l'externe | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 9 — Hote externe : en-tete d'adresse forge, CSP des fichiers, compte hors liste

**Validation** : AUTO — **Issues** : #254 #172

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Session GitHub administrateur sur `<EXT>`, appel `curl -s -b $JE -H 'X-Forwarded-For: 1.2.3.4' -H 'Content-Type: application/json' -d '{"method":"refresh","args":{}}' https://<EXT>/api/call`, puis lire `audit.jsonl` | la ligne d'audit porte la **vraie** adresse source (queue de `X-Forwarded-For`), pas `1.2.3.4` seule ; l'entrypoint Traefik n'a ni `forwardedHeaders.insecure` ni `trustedIPs` large | | |
| 2 | `curl -sI -b $JE https://<EXT>/files/<projet>/x.html \| grep -i content-security-policy` | contient `sandbox` (CSP des fichiers de projet non ecrase par Traefik) | | |
| 3 | Compte GitHub hors `auth.admins` / `auth.users` : appels directs avec sa session | `403` `X-Launcher-Refusal: user` partout, hors `/signed-out` et `/too-many` | | |
| 4 | Changement de `auth.github.clientSecret` puis `helm upgrade` | le pod `launcher` est recree (annotations `checksum/github-secret`, `checksum/auth-templates`) ; sans changement de valeur, pas de redemarrage inutile | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 10 — Acces par sous-chemin `https://coupel.net/claude-qualif` : routage, cookie, absence de HSTS (addendum 4)

**Validation** : AUTO — **Issues** : #254

`<EXT>` = `coupel.net`, `<BASE>` = `/claude-qualif`. Le prefixe n'est jamais retire par Traefik ; l'Ingress ne couvre que `<BASE>`.

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `cd ng2 && go test -count=1 -run 'Prefix254' . ./ui/desktop/ && go test -count=1 -run 'BrowserEnv254\|ServerFilesBase254' ./core/ && cd .. && tests/integration/ui-web/auth-prefix-254/run.sh` | validation du prefixe, aucun StripPrefix, aucune capture de `/`, arguments `--proxy-prefix` / `--cookie-path` / `--redirect-url`, cookie `__Secure-cl_auth_ext`, pas de HSTS, base injectee, redirections, SSE, `/files`, Origin strict, `X-Forwarded-Prefix` ignore ; navigateur sans tete derriere un reverse proxy a prefixe : aucune requete hors de la base | | |
| 2 | `curl -sI https://<EXT><BASE>/` | redirection vers la connexion GitHub ou `/oauth2/…` **sous la base** (`Location: <BASE>/oauth2/…`) ; **aucun** `Strict-Transport-Security` | | |
| 3 | `curl -sI https://<EXT><BASE>` (sans `/` final) | `308` vers `<BASE>/` | | |
| 4 | `curl -sI https://<EXT>/emby/` et `https://<EXT>/` (autres applications de l'hote) | **meme reponse qu'avant l'activation** (aucune capture) : relever les codes et en-tetes avant et apres `helm upgrade` | | |
| 5 | `curl -sI https://<EXT>/claude-qualifier/` et `https://<EXT>/claude-qualif-autre/` | pas de reponse du launcher (le segment est `<BASE>` exactement : `404` de l'application voisine ou de Traefik) | | |
| 6 | Cookie : `curl -sI -c - https://<EXT><BASE>/oauth2/start` (ou outils du navigateur apres connexion) | `__Secure-cl_auth_ext` avec `Path=<BASE>`, `Secure`, `HttpOnly`, `SameSite=Lax`, **sans** `Domain` ; jamais `__Host-` | | |
| 7 | Cookie vers une autre application : `curl -sI -H 'Cookie: __Secure-cl_auth_ext=x' https://<EXT>/emby/` puis relire les cookies recus par `/emby` dans le navigateur | le cookie du launcher n'est pas envoye a `/emby` (Path) ; le launcher ne journalise jamais l'en-tete `Cookie` | | |
| 8 | `curl -s https://<EXT><BASE>/oauth2/sign_in \| grep -ci 'type="password"'` | `0` : aucun formulaire de mot de passe (le compte local n'est PAS permis sur l'externe) | | |
| 9 | `curl -s -o /dev/null -w '%{http_code}' -H 'X-Forwarded-Prefix: /evil' -H 'X-Forwarded-User: admin@local' https://<EXT><BASE>/api/call -d '{}'` | refus (`401`) ; le prefixe de l'en-tete n'est ni lu ni repris dans la reponse | | |
| 10 | `curl -s https://<EXT><BASE>/signed-out \| grep -o 'href="[^"]*"'` | liens relatifs a la base (`<BASE>/` ou relatifs), jamais `href="/"` | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 11 — Parcours GitHub reel sous le sous-chemin

**Validation** : HUMAIN — **Issues** : #254 #172 (parcours reel : retour OAuth, liens, deconnexion, SSE sous la base) — **N/T tant que la GitHub App n'existe pas**

La GitHub App porte l'URL de retour `https://coupel.net/claude-qualif/oauth2/callback` (en plus de celle de l'hote interne). Le compte local `admin@local` n'est PAS permis sur l'hote externe : seul GitHub ouvre une session.

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Hors du LAN, fenetre privee : ouvrir `https://coupel.net/claude-qualif/` | connexion GitHub (colonne GitHub seule) ; certificat valide pour `coupel.net` ; aucune barre d'adresse sortant de `/claude-qualif/` | | |
| 2 | Autoriser l'application avec le compte administrateur | retour sur `https://coupel.net/claude-qualif/` (pas sur la racine de `coupel.net`) ; interface chargee, pastille avec le login | | |
| 3 | Utiliser l'interface : ouvrir un projet factice, un terminal, les Reglages, un fichier de maquette (`files/…`) | tout fonctionne sous le sous-chemin ; le terminal reste vivant (flux d'evenements) ; aucun lien ne mene a la racine de `coupel.net` | | |
| 4 | Menu de la pastille › « Se deconnecter » puis « Se reconnecter » | `/claude-qualif/signed-out` lisible, retour dans `/claude-qualif/` | | |
| 5 | Dans un autre onglet, ouvrir une autre application de `coupel.net` (ex. `/emby/`) | inchangee ; aucune session du launcher n'y apparait | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

## Criteres de Validation

- [ ] **Bloquant** : depuis Internet, `Host: <INT>` donne 403 (scenario 8) avant toute ouverture de 80/443
- [ ] Sous-chemin : aucune autre route de `coupel.net` n'est capturee, pas de HSTS, cookie `__Secure-` limite a `/claude-qualif` (scenario 10)
- [ ] L'hote externe ne propose que GitHub : ni formulaire, ni compte local, ni liste d'adresses ; certificat public valide
- [ ] Un cookie de l'instance interne n'ouvre aucune session sur l'externe ; l'adresse annoncee aux sessions est l'externe
- [ ] Le secret de la GitHub App n'apparait ni dans `NOTES.txt`, ni dans les journaux, ni dans un fichier suivi par git

## Notes QA

[Espace pour observations]
