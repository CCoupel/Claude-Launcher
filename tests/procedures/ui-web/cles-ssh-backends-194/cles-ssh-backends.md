# Procedure de Test — Clés ssh des backends distants depuis le pod (#194)

**Version** : 6.4.0
**Testeur** : QA
**Contrat** : `contracts/backends-remote.md` — tests auto : `ng2/ui/desktop/secrets194_test.go`, `ng2/chart_secrets194_test.go`
**Environnement** : QUALIF (pod `claude-launcher-0`, ns `claude-qualif`), interface par l'**ingress interne** (LAN)

## Prerequis

- [ ] Image QUALIF 6.4.0.x déployée (openssh-client, docker, podman présents), PVC `<release>-secrets` créé
- [ ] Backends déclarés avec des **IP** (les noms courts ne résolvent pas dans les pods) : `ssh` 192.168.1.210, `ssh` 192.168.1.211, `podman` via ssh, `docker` `tcp://192.168.1.211:2375` (sans TLS, accepté)
- [ ] Accès : poste du LAN admis par la liste d'adresses ; accès `authorized_keys` des hôtes 192.168.1.210/211 (clé publique à ajouter pendant la recette)

## Scenarios

### Scenario 1 — Contrôles automatisés (pod et chart)

**Validation** : AUTO | **Issues** : #194

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | `go test ./ui/desktop -run 194` et `go test . -run 194` dans `ng2` | tous PASS |
| 2 | `kubectl -n claude-qualif exec claude-launcher-0 -- sh -c 'ssh -V; docker --version; podman --version'` | les trois clients répondent |
| 3 | `kubectl … exec … -- sh -c 'echo $CL_SECRETS_DIR; ls -ld $CL_SECRETS_DIR; df $CL_SECRETS_DIR'` | `/var/lib/claude-launcher/secrets`, `drwx------`, volume distinct de la racine MooseFS |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 2 — Générer une paire pour 192.168.1.210 (interface interne)

**Validation** : HUMAIN | **Issues** : #194

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | Ouvrir l'interface par l'ingress interne, Réglages › Backends › backend ssh 192.168.1.210 › encadré « Identifiants ssh » | encadré visible, aucune authentification demandée (règle provisoire) |
| 2 | « Générer une paire » | clé publique `ssh-ed25519 …` affichée en entier et copiable ; « clé privée enregistrée · SHA256:… » ; **aucune clé privée affichée** |
| 3 | Copier la clé publique dans `~/.ssh/authorized_keys` de 192.168.1.210 | — |
| 4 | Recharger la page, rouvrir l'encadré | la clé publique et l'empreinte sont toujours là, la clé privée jamais |
| 5 | Observer la réponse de l'appel (outils du navigateur, onglet Réseau) | aucune réponse ne contient `PRIVATE KEY` |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 3 — Coller une paire existante pour 192.168.1.211, refus

**Validation** : HUMAIN | **Issues** : #194

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | Coller la clé privée d'une paire et la clé **publique d'une autre** | refus explicite, rien d'enregistré |
| 2 | Coller une clé privée protégée par une phrase de passe | refus nommant la phrase de passe |
| 3 | Coller une paire cohérente sans phrase de passe | enregistrée, empreinte affichée |
| 4 | Ajouter la clé publique aux `authorized_keys` de 192.168.1.211 | — |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 4 — Empreinte de l'hôte confirmée

**Validation** : HUMAIN | **Issues** : #194

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | « Tester » avant toute empreinte | étape « empreinte de l'hôte connue » en échec avec correctif ; aucune connexion acceptée silencieusement |
| 2 | « Relever l'empreinte » | empreintes (`SHA256:…`, type) listées ; **rien n'est encore enregistré** (`known_hosts` absent du coffre) |
| 3 | Comparer avec `ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub` sur l'hôte, puis confirmer | `known_hosts` enregistré, empreinte affichée |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 5 — « Tester » ssh, podman, docker depuis le pod

**Validation** : HUMAIN (résultats réels sur le LAN ; non rejouable hors QUALIF) | **Issues** : #194

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | « Tester » le backend ssh 192.168.1.210 puis 192.168.1.211 | « client ssh présent », « clé ssh enregistrée », « empreinte connue », connexion : tout OK |
| 2 | « Tester » le backend podman (via ssh) | « client podman présent » puis étapes OK |
| 3 | « Tester » le backend docker `tcp://192.168.1.211:2375` | « client docker présent » puis étapes OK, aucune demande de TLS |
| 4 | Ouvrir un projet sur chacun de ces backends | le terminal s'ouvre |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 6 — Journal, remplacement, suppression, persistance

**Validation** : HUMAIN | **Issues** : #194

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | Lire le journal du pod (`kubectl logs`) après les écritures | une ligne par écriture : backend, type, empreinte, adresse source ; **aucun contenu de clé** |
| 2 | Réglages : « modifié le … » sous la clé | date de la dernière écriture |
| 3 | Remplacer la paire | l'ancienne est remplacée entièrement (clé privée et publique) |
| 4 | « Supprimer » (tout) | `id`, `id.pub`, `known_hosts` effacés (`ls` dans le pod) |
| 5 | Remettre une clé puis `kubectl delete pod claude-launcher-0` | après redémarrage, la clé et le `known_hosts` sont toujours là (PVC) ; `helm uninstall` conserve le PVC |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 7 — Hub Linux et fenêtre native (même règle)

**Validation** : HUMAIN | **Issues** : #194

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | Hub Linux (fenêtre/client web) : saisir une clé pour un backend | accepté ; coffre dans `~/.config/claude-launcher/secrets` (0700), jamais sous la racine partagée |
| 2 | Pointer `CL_SECRETS_DIR` sous la racine partagée | refus clair, rien d'écrit |
| 3 | Exe Windows : saisir une clé | accepté ; coffre sous `%APPDATA%\claude-launcher\secrets` |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Clé privée jamais renvoyée ni affichée, par aucun appel
- [ ] « Tester » réussit pour chaque backend ssh, podman, docker
- [ ] Coffre hors partage, persistant au redémarrage du pod
- [ ] Journal sans contenu

## Notes QA

Risque accepté v6.4.0 : tout poste du LAN admis peut déposer une clé (règle provisoire, R1 en v7.0.0).
