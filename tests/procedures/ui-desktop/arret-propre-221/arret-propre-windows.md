# Procedure de Test — Arret propre de l'interface native, Windows (#221)

**Version** : 6.7.0
**Date** : 2026-11-22
**Testeur** : QA

## Prerequis

- [ ] Environnement : QUALIF — poste Windows, exe natif (Wails) de la version candidate
- [ ] Donnees : **3 projets factices ouverts dans des onglets, dont un en conteneur (pod k8s jetable)** ; faux `claude` — jamais de vrai projet
- [ ] Gestionnaire des tâches (onglet « Détails », colonne « ID de processus » et « Ligne de commande » activees) ; PowerShell
- [ ] Acces : `kubectl` / `podman` du poste (le pod est joint par `kubectl exec`)
- [ ] Avant de commencer : noter les PID des `wsl.exe`, `kubectl.exe`, `podman.exe`, `tmux` deja presents hors du test (ils ne comptent pas)

## Scenarios

### Scenario 1 — Fermeture avec 3 projets ouverts : aucun processus orphelin, 25 s au plus

**Validation** : HUMAIN — **Issues** : #221

**Objectif** : apres fermeture de l'exe, plus aucun processus enfant du launcher (`wsl.exe`, `kubectl.exe`, `podman.exe`, client `tmux -C`) et l'arret dure au plus 25 s.

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ouvrir les 3 projets (dont le pod), attendre « ready » sur chacun ; dans le Gestionnaire des tâches, noter les `wsl.exe` / `kubectl.exe` / `podman.exe` nouveaux (1 client par projet) | Les processus enfants du launcher sont listes (PID notes) | | |
| 2 | Lancer un chronometre, fermer la fenetre (croix) | La fenetre se ferme | | |
| 3 | Attendre la disparition du processus du launcher dans le Gestionnaire des tâches ; arreter le chronometre | Le launcher disparait en **25 s au plus** | | |
| 4 | Relire la liste des processus (etape 1) | **Aucun** des PID notes n'existe encore ; aucun `wsl.exe`, `kubectl.exe`, `podman.exe` ou `tmux` enfant orphelin | | |
| 5 | Relancer l'exe, ouvrir un projet | Rattachement aux sessions existantes (elles ont continue dans tmux / le pod) ; aucun message d'erreur | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Fermeture avec un terminal bloque

**Validation** : HUMAIN — **Issues** : #221

**Objectif** : un client qui ne repond plus ne retient pas l'arret au-dela de 25 s et ne reste pas orphelin.

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ouvrir les 3 projets ; noter le PID du `wsl.exe` (ou `kubectl.exe`) d'un projet | PID note | | |
| 2 | Suspendre ce processus (`pssuspend <PID>` de Sysinternals, ou Gestionnaire des tâches > clic droit > Suspendre) | Le terminal du projet ne repond plus | | |
| 3 | Chronometre, fermer la fenetre du launcher | La fenetre se ferme | | |
| 4 | Attendre la disparition du launcher | Disparition en **25 s au plus** | | |
| 5 | Verifier le PID suspendu et ses enfants | Le processus suspendu **n'existe plus** (arret force de l'arbre) ; aucun enfant orphelin | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — Equivalent automatise (Linux, tmux isole)

**Validation** : AUTO — **Issues** : #221

Joue par `qa` (aucune action utilisateur) : `cd ng2 && CL_TEST_ISOLATION=1 go test -race ./ui/desktop -run 'Stop|TermInput' -count=1`
(`stop221_test.go`, `stop221_unix_test.go`) : aucun client `tmux -C` ni goroutine de terminal apres `Stop`, 25 s au plus
terminal bloque (SIGSTOP) compris, `termInput` refuse apres l'arret, arret pendant des ecritures de terminal sous `-race`.

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Lancer la commande ci-dessus | Tous les tests passent, sans alerte de course | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — Bascule d'organisation bornee, partage mort (#220) et message de repli unique (#222)

**Validation** : AUTO — **Issues** : #220 #222

Joue par `qa` : `cd ng2 && CL_TEST_ISOLATION=1 go test -race ./core ./ui/desktop -run 'Share|Fallback' -count=1`
(`core/sharewrite220_test.go`, `ui/desktop/share220_test.go`, `ui/desktop/fallback222_test.go`) : erreur au plus
`SHARE_WRITE_TIMEOUT` + 1 s, interface reactive, une seule ecriture abandonnee, reprise automatique, un seul message de repli.

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Lancer la commande ci-dessus | Tous les tests passent | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

## Criteres de Validation

- [ ] Scenarios 1 et 2 : aucun processus orphelin, fermeture <= 25 s
- [ ] Les sessions des projets continuent apres la fermeture (rattachement a la relance)
- [ ] Aucune regression sur la fermeture d'un onglet (detachement d'un seul projet)

## Notes QA

[Espace pour observations]
