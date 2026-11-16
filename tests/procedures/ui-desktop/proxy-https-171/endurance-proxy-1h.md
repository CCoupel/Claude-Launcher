# Procedure de Test — Interface web derriere un proxy HTTPS : endurance 1 h et stabilisation (#171, #186, #187)

**Version** : 6.3.0
**Date** : 2026-11-16
**Testeur** : QA (hors `scripts/ci.sh`, tag `slow`)

Reference : `contracts/desktop-web.md` (adresse publique, Host, origine, cookie `Secure`, flux d'evenements). Tests
automatises associes : `ng2/ui/desktop/web171_test.go` (matrices Host/origine/cookie, proxy HTTPS monte dans le test),
`ng2/core/update187_test.go`.

## Prerequis

- [ ] Environnement : LOCAL ou conteneur de developpement ; un proxy inverse HTTPS reel avec un delai de lecture de 60 s (ex. Traefik v3 / nginx, certificat auto-signe accepte par le navigateur de test) ; un nom interne (ex. `launcher.test`) resolu vers le proxy
- [ ] Donnees : projets factices (faux `claude`) ; `backends.yaml` : `desktop_web: { enabled: true, listen: "127.0.0.1:8787", url: "https://launcher.test" }` ; jamais de vrais projets
- [ ] Acces : navigateur de test ouvert sur le lien d'acces `https://launcher.test/?t=<jeton>` ; `curl` disponible

## Scenarios

### Scenario 1 — Endurance 1 h du flux d'evenements derriere le proxy

**Validation** : HUMAIN — **Issues** : #171

**Objectif** : l'interface reste utilisable pendant 1 h sans action, malgre le delai de lecture du proxy (60 s) ; le battement (`: ping` <= 20 s) maintient le flux ; en cas de coupure, la reconnexion est automatique (1,5 s puis delai doublé jusqu'a 15 s).

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ouvrir le lien d'acces public, laisser l'onglet au premier plan 1 h sans toucher | Aucune erreur affichee ; la page reste vivante | | |
| 2 | Toutes les 10 min, provoquer un changement (projet ouvert/ferme par `tmux` ou autre fenetre) | L'affichage se met a jour sans recharger | | |
| 3 | `curl -sN -H "Cookie: <session>" https://launcher.test/api/events` pendant 3 min (voir le releve des battements) | Une ligne `: ping` au plus toutes les 20 s ; aucune coupure du proxy | | |
| 4 | A mi-parcours, couper le proxy 30 s puis le relancer | Reconnexion automatique (delais 1,5 s → 3 s → … → 15 s) ; affichage remis a jour apres reconnexion | | |
| 5 | Fin de l'heure : action utilisateur (cliquer un projet) | Reponse immediate, session toujours valide | | |

Releves : nombre de coupures = ___ ; plus long delai de reconnexion = ___ s ; battement maximal observe = ___ s.

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Session perdue : message « session expiree »

**Validation** : HUMAIN — **Issues** : #171

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Onglet ouvert via le proxy ; dans la fenetre native : regenerer le jeton | — | | |
| 2 | Observer l'onglet | L'interface arrete de reessayer et affiche « session expiree — rouvrir le lien d'acces » | | |
| 3 | Rouvrir le nouveau lien d'acces | Session retablie | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — Hote et origine refuses derriere le proxy

**Validation** : AUTO — **Issues** : #171 — equivalent automatise : `TestPublicURLHostMatrix`, `TestPublicURLOriginMatrix`, `TestReverseProxyHTTPSEndToEnd`

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `curl -sk -o /dev/null -w '%{http_code}' -H 'Host: evil.test' https://launcher.test/` | 403 | | |
| 2 | Appel `/api/call` avec `Origin: https://evil.test` et la session | 403 | | |
| 3 | Acces direct `http://<pod>:8787/` avec le cookie Secure | Pas de session conservee (403 sur les appels) | | |
| 4 | `curl -s https://launcher.test/healthz` ; `/readyz` | 200 `ok`, sans cookie, sans information | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — Stabilisation #186 et #187 (boucles de repetition)

**Validation** : AUTO — **Issues** : #186, #187

**Objectif** : les correctifs rendent les tests stables ; repetition pour detecter une instabilite residuelle. Executer depuis `ng2/` avec `TMUX_TMPDIR=$(mktemp -d /tmp/clci.XXXXXX)`.

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `go test -race -count=20 -run 'TestRelayoutLeftZeroToTenTeammates' ./ui/tmux` (#186) | 20/20 verts ; `tmux -V` visible dans la sortie de `scripts/ci.sh` | | |
| 2 | `go test -race -count=30 -run 'TestInitSyncConcurrent\|TestSyncInitProjectInvalidURLDoesNotPanic' ./core` (#187 a) | 30/30 verts, aucune alerte du detecteur de courses | | |
| 3 | `go test -race -count=20 -run 'TestWebUIStateLanAndWarn' ./ui/desktop` (#187 b) | 20/20 verts (aucune course de port) | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] 1 h sans erreur derriere un proxy a delai de lecture de 60 s
- [ ] Reconnexion automatique apres coupure, « session expiree » si la session est perdue
- [ ] Hote et origine etrangers refuses ; cookie `Secure` ; sondes sans session
- [ ] Tests #186 et #187 stables sur les boucles

## Notes QA

Duree : environ 1 h 15 (scenario 1 en tache de fond pendant les autres). Hors `scripts/ci.sh` : tag `slow`.
