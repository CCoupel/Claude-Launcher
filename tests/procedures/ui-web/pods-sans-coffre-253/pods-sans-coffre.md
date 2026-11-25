# Procedure de Test — Pods projets sans coffre ni jeton de ServiceAccount (#253)

**Version** : 7.0.0
**Date** : 2026-11-25
**Testeur** : QA (scenarios AUTO) + utilisateur (scenario HUMAIN)

Reference : `contracts/helm-values.md` « Pods projets (backend k8s) » (montages `subPath`, `automountServiceAccountToken`, annotation `cl.mounts: "v2"`, ancien modele P6), plan `.claude/workflow/plan-v7.0.0.md` LOT-1.

## Prerequis

- [ ] Environnement : conteneur de dev (scenario 1) ; QUALIF, namespace du launcher, `kubectl` du poste de QA (scenarios 2 a 5)
- [ ] Donnees : **projet factice uniquement** (nom contenant « factice »), ouvert sur le backend k8s de la QUALIF ; supprime en fin de procedure (`kubectl delete statefulset <nom>`)
- [ ] Acces : `go` (AUTO) ; `kubectl` avec droit `exec` sur les pods du namespace ; `<RACINE>` = racine MooseFS declaree dans `volumes` du backend, `<PROJET>` et `<NOM-POD>` releves sur la carte
- [ ] Pour le scenario 4 : un pod projet **ouvert avec la v6.9.0** avant la mise a jour QUALIF en v7.0.0 (ou, a defaut, annotation `cl.mounts` retiree par `kubectl annotate statefulset <nom> cl.mounts-` puis pod recree a la main avec l'ancien manifeste) — le consigner dans « Notes QA »

## Scenarios

### Scenario 1 — Manifeste et ancien modele (automatise)

**Validation** : AUTO — **Issues** : #253

```bash
cd ng2 && go test -race -count=1 -run 'K8s253' ./core/ && echo OK
```

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Commande ci-dessus | seuls le dossier du projet et `CLAUDE_CONFIG_DIR` sont montes, en `subPath`, au meme chemin que le launcher ; aucune racine de volume montee ; volumes deduplies ; chemin hors volume ou evade par `..` refuse (« stockage partage ») ; dossiers « local » (emptyDir) montes apres le projet ; `automountServiceAccountToken: false` par defaut, la valeur explicite de `pod:` l'emporte, signalee par le diagnostic ; `cl.mounts: "v2"` ; pod en marche a l'ancien modele conserve et signale, pod arrete recree au nouveau modele | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Un pod projet ne voit ni le coffre ni les acces du launcher (critical)

**Validation** : AUTO — **Issues** : #253

Pod projet cree (ou recree) par la v7.0.0.

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `kubectl -n <ns> exec <NOM-POD> -- ls -A <RACINE>` | **aucune** entree autre que le dossier du projet et celui de `CLAUDE_CONFIG_DIR` (au plus leurs parents vides) ; ni `.config`, ni `.cache`, ni `.kube`, ni les autres projets | | |
| 2 | `kubectl -n <ns> exec <NOM-POD> -- sh -c 'ls -A <RACINE>/.config/claude-launcher-ng2 2>&1; ls -A <RACINE>/.config/claude-launcher-ng2/secrets 2>&1'` | `No such file or directory` pour les deux ; **aucun** `backends.yaml`, `secrets`, cle ni jeton | | |
| 3 | `kubectl -n <ns> exec <NOM-POD> -- sh -c 'ls -A <RACINE>/.kube 2>&1; ls -A $HOME/.kube 2>&1'` | `No such file or directory` (pas de kubeconfig) | | |
| 4 | `kubectl -n <ns> exec <NOM-POD> -- sh -c 'ls /var/run/secrets/kubernetes.io/serviceaccount 2>&1'` | `No such file or directory` : pas de jeton de ServiceAccount | | |
| 5 | `kubectl -n <ns> exec <NOM-POD> -- sh -c 'kubectl get pods 2>&1 \| head -3'` (ou `wget -qO- https://kubernetes.default.svc 2>&1`) | aucun acces authentifie a l'API (kubectl absent ou refus) | | |
| 6 | `kubectl -n <ns> get pod <NOM-POD> -o jsonpath='{.spec.automountServiceAccountToken} {.metadata.annotations.cl\.mounts}'` | `false v2` | | |
| 7 | `kubectl -n <ns> get pod <NOM-POD> -o jsonpath='{range .spec.containers[0].volumeMounts[*]}{.mountPath} {.subPath}{"\n"}{end}'` | une ligne par montage : le projet (`subPath` = son chemin relatif), `CLAUDE_CONFIG_DIR` (`subPath` = `.claude` ou son equivalent), les emptyDir « local » sans `subPath` ; **jamais** la racine avec `subPath` vide | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — Non-regression k8s : le projet fonctionne comme en v6.9.0

**Validation** : AUTO — **Issues** : #253

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ouvrir le projet factice sur le backend k8s ; `kubectl -n <ns> wait --for=condition=Ready pod/<NOM-POD> --timeout=300s` | pod pret, aucune erreur dans le journal du launcher | | |
| 2 | `kubectl -n <ns> exec <NOM-POD> -- sh -c 'cd <PROJET> && echo ok > .nr253 && cat .nr253'` puis `cat <PROJET>/.nr253` depuis le launcher | `ok` ; le fichier est visible des deux cotes (le dossier du projet reste partage) | | |
| 3 | `kubectl -n <ns> exec <NOM-POD> -- sh -c 'echo $CLAUDE_CONFIG_DIR; touch $CLAUDE_CONFIG_DIR/.nr253 && echo ecrit'` | chemin de la config de claude, `ecrit` ; le fichier apparait cote launcher | | |
| 4 | Dossier « local » (`local:` du template) : `kubectl … exec … -- sh -c 'touch <PROJET>/<dossier-local>/x; ls <PROJET>/<dossier-local>'`, puis `ls` du meme dossier cote launcher | present dans le pod uniquement (masque par l'emptyDir), absent cote launcher | | |
| 5 | Lancer `claude` dans la session du projet (message simple) | reponse normale, connexion partagee conservee (`.claude.json` vu) ; la liste des projets et la carte restent a jour | | |
| 6 | Fermer le projet, le rouvrir | pod reutilise ou recree sans erreur ; aucun avertissement « Montages d'avant v7.0.0 » | | |
| 7 | Nettoyer : `rm <PROJET>/.nr253 $CLAUDE_CONFIG_DIR/.nr253` | fichiers supprimes | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — Pod d'avant v7.0.0 : conserve, signale, recree a la reouverture (P6)

**Validation** : AUTO — **Issues** : #253

Pod projet **en marche** cree par la v6.9.0 (sans annotation `cl.mounts`), launcher mis a jour en v7.0.0.

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `kubectl -n <ns> get pod <NOM-POD> -o jsonpath='{.metadata.annotations.cl\.mounts}'` apres mise a jour du launcher | vide (ancien modele) | | |
| 2 | Rouvrir/rattacher le projet depuis le launcher v7.0.0 | la session continue ; le pod **n'est pas recree** (meme `metadata.uid` et `status.startTime` qu'avant) | | |
| 3 | Journal du launcher et diagnostic du backend (`claude-launcher-ng2 backends` ou page de diagnostic) | ligne « Montages d'avant v7.0.0 : fermer puis rouvrir le projet pour les restreindre » pour ce pod | | |
| 4 | Fermer le projet (arret du pod), le rouvrir | pod **recree** : `cl.mounts` = `v2`, `automountServiceAccountToken` = `false`, montages du scenario 2 etape 7 | | |
| 5 | Rejouer le scenario 2 etapes 1 a 4 sur ce pod | memes resultats que pour un pod neuf | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 5 — Avertissement de la carte pour un pod d'avant v7.0.0

**Validation** : HUMAIN — **Issues** : #253 (lisibilite et clarte du message, place sur la carte)

Pod du scenario 4 etape 2, avant sa fermeture.

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Regarder la carte du projet (fenetre native ou coque tmux) | l'avertissement « Montages d'avant v7.0.0 : fermer puis rouvrir le projet pour les restreindre » est visible, lisible, non tronque, distinct d'une erreur | | |
| 2 | Suivre la consigne (fermer, rouvrir) | l'avertissement disparait apres la reouverture ; l'action demandee est comprehensible sans aide | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

## Criteres de Validation

- [ ] Aucun pod projet ne voit le coffre, `backends.yaml`, `.kube/` ni un jeton de ServiceAccount
- [ ] Un projet k8s s'ouvre, ecrit dans son dossier et dans `CLAUDE_CONFIG_DIR`, comme en v6.9.0
- [ ] Un pod d'avant v7.0.0 n'est jamais coupe, est signale, puis recree au nouveau modele

## Notes QA

[Espace pour observations]
