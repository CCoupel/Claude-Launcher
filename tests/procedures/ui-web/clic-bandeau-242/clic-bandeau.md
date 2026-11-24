# Procedure de Test — Clic souris decale avec le bandeau d'attente, version web (#242)

**Version** : 6.8.2
**Date** : 2026-11-24
**Testeur** : QA

## Prerequis

- [ ] Environnement : QUALIF — interface web `https://launcher.local` dans Chrome (pod k8s), hub Linux
- [ ] Donnees : projet factice ouvert, faux claude — jamais de vrai projet ; permission de notification du site **non decidee** (Chrome : reinitialiser l'autorisation du site) et « Ne plus proposer » jamais clique
- [ ] Poste de commande : `kubectl` sur le cluster, depot clone a la racine (pour `simulate-attention.sh`) ; navigateur Chrome sur `https://launcher-qualif.local`
- [ ] **Pod du projet** (le script s'execute DANS le pod du projet, serveur tmux par defaut, session `cl`) :
  `kubectl -n claude-qualif get pods` → noter le pod du projet factice (nom `<categorie>-<projet>`), puis `P=<pod>` et `D=<chemin du projet vu dans le pod>`
  (le dossier `$D/.claude-launcher/attention` est le `CL_ATTENTION_DIR` de la session : volume partage, lu par le hub QUALIF)
- [ ] **Lignes reperes** : dans le pane du leader du projet factice (ou un pane shell), lancer `clear; for i in $(seq 1 200); do echo "LIGNE $i"; done` — le bas du pane affiche les dernieres lignes numerotees
- [ ] Fenetre Chrome non redimensionnee pendant le scenario. Comparer **avant** (QUALIF 6.8.1.x ou antérieure : defaut visible) et **apres** (candidate 6.8.2.x : corrige)

## Scenarios

> **Constater visuellement** : dans le terminal, **double-cliquer au milieu d'une ligne repere** (« LIGNE 150 » par exemple) :
> le mot-ligne est surligne. Defaut = la surbrillance est sur la ligne **du dessus** (« LIGNE 149 »), et l'ecart grandit vers le bas du pane
> (derniere ligne : 1 a 2 lignes plus haut). Choisir 3 lignes : haut, milieu, bas du pane.

### Scenario 1 — #qbar : bandeau de la question dans la vignette Teamleaders

**Validation** : AUTO (`tests/integration/ui-web/clic-bandeau-242/run.sh`) puis HUMAIN — **Issues** : #242

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ouvrir le projet factice, puis l'onglet **Teamleaders** : double-cliquer un mot sur 3 lignes reperes du terminal de la vignette (haut, milieu, bas) | Sans attente : la surbrillance est sur la ligne cliquee | | |
| 2 | Simuler l'attente : `sh tests/procedures/ui-desktop/attente-teamleader-223/simulate-attention.sh "$D" question --pod "$P" claude-qualif` | Le script affiche `projet … leader %N` et `ecrit …/N.json : question du teamleader` ; en 5 s la vignette prend le cadre orange et le bandeau « ❓ … » | | |
| 3 | Refaire les 3 doubles-clics de l'etape 1, vignette avec le bandeau | **Apres le fix** : surbrillance sur la ligne cliquee, a chaque fois. **Avant le fix** : une ligne au-dessus (2 en bas) | | |
| 4 | **Nettoyage** : `sh tests/procedures/ui-desktop/attente-teamleader-223/simulate-attention.sh "$D" fin --pod "$P" claude-qualif` | `fin d'attente : 1 fichier(s) retire(s)` ; cadre et bandeau disparaissent ; refaire l'etape 1 : toujours exact | | |
| 5 | Verifier : `kubectl -n claude-qualif exec "$P" -- ls "$D/.claude-launcher/attention"` | Dossier vide (ou absent) : aucun etat simule ne reste | | |

### Scenario 2 — #attnbar : bandeau « Etre prevenu quand un teamleader attend une reponse ? » (vue projet)

**Validation** : HUMAIN — **Issues** : #242

Ce bandeau n'a pas besoin d'attente simulee : il s'affiche des qu'un chargement de l'interface trouve la permission de notifications du site a « demander » (et « Ne plus proposer » jamais clique).

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Remettre la permission a « demander » : Chrome → cadenas de `launcher-qualif.local` → Parametres du site → Notifications → **Reinitialiser** (ou `chrome://settings/content/siteDetails?site=https://launcher-qualif.local`). Console DevTools de la page : `localStorage.removeItem('cl.attnBarOff')` | Notifications = « Demander (par defaut) » | | |
| 2 | Recharger la page (F5), ouvrir le projet factice, onglet du projet (pas Teamleaders) | Bandeau orange « ❓ Etre prevenu… » en haut, au-dessus des onglets | | |
| 3 | 3 doubles-clics sur des lignes reperes (haut, milieu, bas) du pane | **Apres le fix** : surbrillance sur la ligne cliquee. **Avant le fix** : une a deux lignes au-dessus, ecart croissant vers le bas | | |
| 4 | Cliquer « ✕ » (ne plus proposer) : le bandeau disparait, refaire l'etape 3 | Surbrillance toujours sur la ligne cliquee ; la zone du terminal reprend sa hauteur sans F5 | | |
| 5 | **Nettoyage** : `localStorage.removeItem('cl.attnBarOff')` puis, si besoin, Reinitialiser la permission du site | Etat du navigateur comme avant le test | | |

### Scenario 3 — Les deux bandeaux ensemble, programme qui suit la souris

**Validation** : HUMAIN — **Issues** : #242

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Reprendre les etapes 2 des scenarios 1 et 2 (les deux bandeaux affiches) | Les deux bandeaux visibles | | |
| 2 | Dans le pane, lancer `less /etc/services` (souris active : `less --mouse`), cliquer / faire defiler sur une ligne precise | La ligne visee reagit | | |
| 3 | Nettoyer (scenario 1 etape 4, scenario 2 etape 5) | Etat initial | | |

### Scenario 4 — Fenetre native (Wails) : pas de regression

**Validation** : HUMAIN — **Issues** : #242

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Exe Windows, simuler l'attente (script de la procedure #223, hote), onglet Teamleaders : double-clics sur 3 lignes reperes | La surbrillance est sur la ligne cliquee | | |
| 2 | Reduire la hauteur de la fenetre native (fenetre tmux plus grande que la zone, #126), double-cliquer la derniere ligne | La surbrillance est sur la ligne cliquee | | |
