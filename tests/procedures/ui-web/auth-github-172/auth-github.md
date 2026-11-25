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
- [ ] Mot de passe local : `PW=$(kubectl -n <ns> get secret <rel>-local-admin -o jsonpath='{.data.password}' | base64 -d)` — ne jamais l'afficher dans un rapport ni l'ecrire dans un fichier

## Scenarios

### Scenario 1 — Go, chart et navigateur sans tete (automatise)

**Validation** : AUTO — **Issues** : #172 #251 #252

```bash
cd ng2 && go test -race -count=1 -run 'Auth172|Audit251|Proxy172' ./core/ ./ui/desktop/ && go test -count=1 -run 'ChartAuth172' . && cd .. && tests/integration/ui-web/auth-proxy-172/run.sh && echo OK
```

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Commande ci-dessus | identite par `X-Forwarded-User` (401 absent/vide/invalide/multiple, 403 inconnu, `admin@local` admis seulement sur l'hote interne) ; aucun jeton ni cookie du launcher ; controles Host/Origin/JSON inchanges ; `init.user` / `clientInfo.user` ; regle R1 (12 methodes) refusee a un `user` avec `forbidden: admin` ; audit (une entree par appel, agregation, rotation 10 Mio, echec = R1 refusees) ; port de sante ; refus de demarrer ; chart (sidecar, secrets, compte local, refus de rendu, `rateLimit`) ; navigateur : pastille, menu, deconnexion, `user` en lecture seule, modale de session expiree | | |

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
| 1 | `curl -s -c $J -o /dev/null -w '%{http_code}' -d 'username=admin@local&password=mauvais' <URL>/oauth2/sign_in` | pas de cookie `_cl_auth` ; page de connexion reaffichee avec « Identifiant ou mot de passe incorrect » | | |
| 2 | Rafale : `for i in $(seq 30); do curl -s -o /dev/null -w '%{http_code}\n' -d 'username=admin@local&password=mauvais' <URL>/oauth2/sign_in; done \| sort \| uniq -c` | au plus ~20 reponses normales (rafale `burst`), les suivantes **`429`** ; au moins un `429` | | |
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
| 1 | Saisir un mauvais mot de passe | message unique « Identifiant ou mot de passe incorrect » (sans indiquer lequel est faux) | | |
| 2 | Valider 25 fois de suite le formulaire (ou laisser le scenario 4 tourner) | page « Trop de tentatives — réessayer plus tard » lisible (M2) | | |
| 3 | Avec un compte GitHub `auth.users` (si disponible), ouvrir les Reglages | titre « … · lecture seule », actions grisees avec l'infobulle « réservé à un administrateur » ; les terminaux et projets restent utilisables | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

## Criteres de Validation

- [ ] Aucun en-tete d'identite forge n'est accepte ; le port `8081` n'est pas joignable hors du pod
- [ ] Le compte local ouvre une session admin, les essais sont limites (`429`), le mot de passe n'apparait nulle part
- [ ] La regle R1 refuse les ecritures sensibles a un `user` ; l'audit est nominatif et sans argument
- [ ] Les pages de connexion, d'erreur, de deconnexion et la modale d'expiration sont lisibles

## Notes QA

[Espace pour observations]
