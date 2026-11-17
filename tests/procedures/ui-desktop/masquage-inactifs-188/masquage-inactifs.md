# Procedure de Test — Commutateur du masquage des teammates inactifs (#188)

**Version** : 6.3.0
**Date** : 2026-11-17
**Testeur** : utilisateur (validation humaine) — fenetre native **Windows et Linux**, puis coque tmux Linux

Reference : `contracts/tmux-options.md` (« Masquage desactive »), `contracts/desktop-dispatch.md` (#188), maquette validee **variante A** (`_work/mockup/188/config_ui__masquage_inactifs.propositions.html`, avec la carte TEAM : **une puce « masquage <N> s » seulement quand le masquage est actif, aucune puce quand il est desactive**). Equivalents automatises : `ng2/ui/desktop/hideidle188_test.go`.

## Prerequis

- [ ] Environnement : QUALIF — poste Windows (exe natif) et poste/hub Linux ; un projet **factice** ouvert avec une team d'au moins 4 teammates (faux `claude`), dont certains inactifs ; jamais de vrais projets
- [ ] `config.conf` de depart : `TEAM_HIDE_IDLE=30` (defaut), cle `TEAM_HIDE_IDLE_ENABLED` absente (= active) ; pour accelerer : `TEAM_HIDE_IDLE=10`
- [ ] Sauvegarde de `config.conf` (restauration en fin de test)

## Scenarios

### Scenario 1 — Etat par defaut = comportement de la v6.2.0

**Validation** : HUMAIN — **Issues** : #188

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Reglages > Launcher > groupe « teams » | Le commutateur « Cacher les teammates inactifs » est **allume** ; le champ du delai affiche la valeur du fichier (30 s) ; conforme a la maquette A | | |
| 2 | Carte TEAM du projet ouvert | Une puce **« masquage 30 s »** est affichee | | |
| 3 | Laisser un teammate inactif plus longtemps que le delai | Il est cache automatiquement (comme en v6.2.0) | | |
| 4 | Fichier de la v6.2.0 avec `TEAM_HIDE_IDLE=0` (sans la nouvelle cle) | Commutateur allume, delai `0` (« jamais ») : **comportement inchange** | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Desactiver : tout reapparait, sans relance

**Validation** : HUMAIN — **Issues** : #188

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Cacher a la main un teammate inactif (double-clic sur sa ligne) ; un autre est cache automatiquement | Deux teammates caches | | |
| 2 | Reglages > Launcher > eteindre « Cacher les teammates inactifs » ; enregistrer | **Sans relancer** : les deux teammates **reapparaissent** dans la fenetre (au tour suivant) | | |
| 3 | Carte TEAM | **Aucune puce** de masquage (pas d'autre indicateur d'etat) ; aucune ligne cachee | | |
| 4 | Reglages : le champ du delai | Grise mais **conserve** (30 s) | | |
| 5 | Double-clic sur la ligne d'un teammate inactif | **Refuse** avec le message « masquage desactive : l'activer dans les Reglages » ; rien n'est cache | | |
| 6 | Laisser un teammate inactif tres longtemps | Jamais cache | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — Reactiver : le delai d'avant s'applique

**Validation** : HUMAIN — **Issues** : #188

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Rallumer le commutateur ; enregistrer | Le delai **30 s** est applique sans le ressaisir ; la puce « masquage 30 s » revient sur la carte | | |
| 2 | Teammate inactif depuis plus longtemps que le delai | Cache des le tour suivant | | |
| 3 | Delai a `0` puis eteindre, puis rallumer | A l'allumage avec un delai `0`, l'interface **propose 30 s** | | |
| 4 | Double-clic de masquage a la main | Fonctionne de nouveau | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — Coque tmux Linux et cohabitation

**Validation** : HUMAIN — **Issues** : #188

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Hub Linux (coque tmux) sur la meme racine : `config.conf` avec `TEAM_HIDE_IDLE_ENABLED=0` | Meme effet : plus aucun masquage automatique, l'etat se lit dans la coque | | |
| 2 | Fenetre native et coque tmux sur le meme projet | Les deux appliquent le **meme** reglage (une seule valeur pour tous les launchers) | | |
| 3 | Un launcher en **lecture seule** sur le projet | N'ecrit rien ; affiche l'etat recu | | |
| 4 | Fenetre Windows : meme parcours (scenarios 1 a 3) | Identique a Linux | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Defaut et fichiers de la v6.2.0 inchanges (`TEAM_HIDE_IDLE=0` compris)
- [ ] Desactive : tout reapparait sans relance, delai conserve, double-clic de masquage refuse
- [ ] Carte TEAM : puce « masquage <N> s » **seulement** quand actif, aucune sinon
- [ ] Windows, Linux (fenetre native) et coque tmux conformes a la maquette A

## Notes QA

Restaurer `config.conf` sauvegarde. Un launcher de version anterieure ignore la nouvelle cle et applique l'ancienne regle aux projets dont il a la main (documente).
