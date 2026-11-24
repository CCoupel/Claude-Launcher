# Procedure de Test — Clic souris decale avec le bandeau d'attente, version web (#242)

**Version** : 6.8.2
**Date** : 2026-11-24
**Testeur** : QA

## Prerequis

- [ ] Environnement : QUALIF — interface web `https://launcher.local` dans Chrome (pod k8s), hub Linux
- [ ] Donnees : projet factice ouvert, faux claude — jamais de vrai projet ; permission de notification du site **non decidee** (Chrome : reinitialiser l'autorisation du site) et « Ne plus proposer » jamais clique
- [ ] **Simuler l'attente** : `sh tests/procedures/ui-desktop/attente-teamleader-223/simulate-attention.sh <chemin du projet> question|fin` (voir la procedure #223)
- [ ] Un terminal de l'onglet du projet contenant plusieurs lignes distinctes (ex. `seq 1 60` dans un pane), fenetre du navigateur non redimensionnee pendant le scenario

## Scenarios

### Scenario 1 — Le clic tombe sur la ligne visee, avec le bandeau d'attente

**Validation** : AUTO (`tests/integration/ui-web/clic-bandeau-242/run.sh`) puis HUMAIN — **Issues** : #242

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Sans bandeau : double-cliquer un mot sur la 3e, la 20e et la derniere ligne du pane | Le mot sous le pointeur est selectionne, a chaque fois | | |
| 2 | **[sim question]** : le bandeau « Etre prevenu quand un teamleader attend une reponse ? » apparait en haut | Bandeau visible | | |
| 3 | Reprendre l'etape 1 (3e, 20e, derniere ligne) | Le mot sous le pointeur est selectionne — **pas** celui de la ligne du dessus ou du dessous ; ecart nul en bas de pane | | |
| 4 | Dans un programme qui suit la souris (ex. `htop`, `less` avec souris), cliquer une ligne precise | La ligne cliquee reagit, avec le bandeau affiche | | |
| 5 | Cliquer « ✕ » (ne plus proposer) ou autoriser les notifications, puis refaire l'etape 3 | Meme resultat qu'a l'etape 3 (bandeau retire) | | |
| 6 | Onglet Teamleaders, vignette en attente (bandeau « ❓ question » de la vignette) : cliquer une ligne du terminal | La ligne cliquee est visee (vignette reduite : meme exigence) | | |
| 7 | **[sim fin]**, puis refaire l'etape 1 | Comme a l'etape 1 | | |

### Scenario 2 — Fenetre native (Wails) : pas de regression

**Validation** : HUMAIN — **Issues** : #242

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Exe Windows, **[sim question]** (pas de bandeau de notification dans la fenetre native), cliquer des lignes variees | Le clic tombe sur la ligne visee | | |
| 2 | Reduire la hauteur de la fenetre native (fenetre tmux plus grande que la zone, #126) puis cliquer la derniere ligne | Le clic tombe sur la ligne visee | | |
