# Procedure de Test — Arret propre et cache d'etat sain (#205 #206)

**Version** : 6.5.0
**Date** : 2026-11-19
**Testeur** : QA + utilisateur (scenario HUMAIN)

Reference : `.claude/workflow/plan-v6.5.0-rev1.md` (LOT-3).

## Prerequis

- [ ] Environnement : conteneur de dev (scenarios AUTO) ; poste Windows reel avec l'exe lance depuis `\\wsl.localhost\…` (scenario 3)
- [ ] Donnees : aucune ; aucun vrai projet
- [ ] Acces : `go`, `gcc` (`-race`) ; Gestionnaire des taches / Process Explorer (scenario 3)

## Scenarios

### Scenario 1 — #206 : vidage des canaux apres l'echeance, aucune goroutine residuelle

**Validation** : AUTO — **Issues** : #206

```bash
cd ng2 && go test -race -count=20 -run '206|TestDrainClose|TestWatchTreeUntil|StateMgr' ./core/ && echo OK
```

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Commande ci-dessus | 0 echec sur 20 passages, borne `fsCloseLimit` respectee | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — #205 : balayage des `*.json.*.tmp` orphelins

**Validation** : AUTO — **Issues** : #205

```bash
cd ng2 && go test -race -count=5 -run '205' ./core/ && echo OK        # + -tags statetmp tant que le tag n'est pas retire
```

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Commande ci-dessus | `.tmp` > 24 h supprime ; plus recent, `.json` reel et fichier non `.json.*.tmp` conserves ; age reglable ; dossier absent sans erreur | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — #206 : arret de l'exe Windows reel sur `\\wsl.localhost`

**Validation** : HUMAIN — **Issues** : #206 (partage 9P reel et handles Windows : non simulables)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Lancer l'exe depuis `\\wsl.localhost\…`, ouvrir 2-3 projets, provoquer des changements de fichiers (build, `git checkout`) | fenetre reactive | | |
| 2 | Fermer la fenetre pendant l'activite fichiers | fermeture en moins de ~20 s, sans fenetre figee | | |
| 3 | Gestionnaire des taches / Process Explorer | aucun processus `claude-launcher*` residuel | | |
| 4 | Process Explorer : handles ouverts sur le dossier surveille (Find Handle) | aucun handle residuel | | |
| 5 | Relancer l'exe aussitot, puis refaire 3 fois 1-2 | demarrage normal, arret toujours propre | | |
| 6 | Journal du launcher | au plus un message « fermeture sans reponse… abandonnee », jamais d'arret bloque | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Scenarios 1 a 3 PASS
- [ ] CI verte (Linux et Windows)

## Notes QA

Scenario 3 : validation humaine (VALID-HUMAINE).
