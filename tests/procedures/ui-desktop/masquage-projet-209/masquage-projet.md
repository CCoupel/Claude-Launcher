# Procedure de Test — Masquage des teammates inactifs par projet (#209)

**Version** : 6.5.0
**Date** : 2026-11-19
**Testeur** : QA + utilisateur (scenarios HUMAIN)

Reference : `contracts/desktop-dispatch.md` « v6.5.0 », maquette `docs/mockup/v6.5.0/ui/team_card__masquage_par_projet.html` (variante C : oeil 3 etats), `docs/mockup/DECISIONS.md` (team_card).

## Prerequis

- [ ] Environnement : conteneur de dev (AUTO) ; exe Windows reel (HUMAIN) ; projets factices uniquement
- [ ] Donnees : 2 projets avec une team (un teammate actif, un inactif depuis > delai) ; `TEAM_HIDE_IDLE=30`
- [ ] Acces : `go`, `tmux`, `node`

## Scenarios

### Scenario 1 — Regles et API

**Validation** : AUTO — **Issues** : #209

```bash
cd ng2 && go test -race -count=1 -run '209' ./core/ ./ui/desktop/ && echo OK
```

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Commande ci-dessus | priorite projet > global ; global change → projets `auto`/`none` intacts ; ecriture/relecture `PROJECT_HIDE_IDLE` ; valeur inconnue refusee sans ecriture ; `teamToggle` refuse en `no hide` (message du contrat) ; simulation node de l'oeil (cycle, menu, infobulle) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Rendu de l'oeil dans l'exe Windows

**Validation** : HUMAIN — **Issues** : #209 (rendu, conformite a la maquette)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ouvrir un projet, regarder le titre de la carte TEAM | oeil **grise** (`default`), conforme a la maquette | | |
| 2 | Clic sur l'oeil | oeil **ferme**, nom « auto hide » ; teammate inactif masque apres le delai | | |
| 3 | Nouveau clic | oeil **ouvert**, « no hide » ; tous les teammates visibles | | |
| 4 | Nouveau clic | retour a **grise** (`default`) | | |
| 5 | Survoler l'oeil | infobulle « Ce projet : … — clic : … · reglage global : masques apres 30 s / desactive » | | |
| 6 | Clic droit | menu des 3 etats, etat courant marque, delai et valeur globale lisibles | | |
| 7 | Choisir « no hide » dans le menu, puis double-clic sur un teammate | refus avec le message « masquage desactive pour ce projet… » | | |
| 8 | Changer le global (Reglages) | projet en `auto`/`none` inchange ; projet en `default` suit | | |
| 9 | Relancer l'exe | etat de l'oeil conserve ; l'oeil change sans relance (refresh immediat) | | |
| 10 | Variantes de theme / taille de police (#150) | oeil net, aligne avec le titre, non rogne | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — Coque Linux : titre de la carte

**Validation** : HUMAIN — **Issues** : #209 (lisibilite du titre)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Coque tmux : projet en `auto`, `none`, `default` | titre « masquage 30 s » / « no hide » selon l'etat effectif | | |
| 2 | Modifier `PROJECT_HIDE_IDLE` dans `config.conf` a la main | pris en compte au tour suivant, sans relance | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Scenarios 1 a 3 PASS
- [ ] Aucune regression sur #188 (`go test -run 188 ./ui/desktop/`)

## Notes QA

Scenarios 2 et 3 : validation humaine (rendu).
