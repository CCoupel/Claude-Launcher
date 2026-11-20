# Procedure de Test — Masquage des teammates inactifs par projet (#209)

**Version** : 6.5.0
**Date** : 2026-11-20
**Testeur** : QA + utilisateur (scenarios HUMAIN)

Reference : `contracts/desktop-dispatch.md` « v6.5.0 », maquette v2 `docs/mockup/v6.5.0/ui/team_card__masquage_par_projet_v2.html` (proposition A « bouton enfoncé » : oeil 3 etats, a droite du titre, sans puce), `docs/mockup/DECISIONS.md` (team_card).

## Prerequis

- [ ] Environnement : conteneur de dev (AUTO) ; exe Windows reel (HUMAIN) ; projets factices uniquement
- [ ] Donnees : 2 projets avec une team (un teammate actif, un inactif depuis > delai) ; `TEAM_HIDE_IDLE=30`
- [ ] Acces : `go`, `tmux`, `node`

## Scenarios

### Scenario 1 — Regles et API

**Validation** : AUTO — **Issues** : #209

```bash
cd ng2 && go test -race -count=1 -run '209' ./core/ ./ui/desktop/ ./ui/tmux/ && echo OK
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
| 1 | Ouvrir un projet, regarder le titre de la carte TEAM | oeil **gris nu** (`default`), **dernier element du titre, aligne a droite** ; **aucune puce « masquage N s »** ; versions tronquees (…) avant l'oeil si le titre est etroit, l'oeil reste visible | | |
| 2 | Clic sur l'oeil | oeil **ferme sur fond plein accent** (« bouton enfonce » : choix du projet), nom « auto hide » ; teammate inactif masque apres le delai | | |
| 3 | Nouveau clic | oeil **ouvert sur fond plein accent**, « no hide » ; tous les teammates visibles | | |
| 4 | Nouveau clic | retour a **gris nu** (`default`), fond plein disparu | | |
| 5 | Survoler l'oeil | infobulle « Ce projet : … — clic : … · reglage global : masques apres 30 s / desactive » | | |
| 6 | Clic droit | menu des 3 etats, etat courant marque, delai et valeur globale lisibles | | |
| 7 | Choisir « no hide » dans le menu, puis double-clic sur un teammate | refus avec le message « masquage desactive pour ce projet… » | | |
| 8 | Changer le global (Reglages) | projet en `auto`/`none` inchange ; projet en `default` suit | | |
| 9 | Relancer l'exe | etat de l'oeil conserve ; l'oeil change sans relance (refresh immediat) | | |
| 10 | Theme clair puis sombre ; taille de police (#150) ; survol (`:hover`) de l'oeil plein | fond plein et icone lisibles dans les deux themes, contraste suffisant, oeil net, aligne, non rogne | | |
| 11 | Clic droit : le menu reprend le meme rendu (fond plein pour l'etat courant propre au projet) | coherent avec l'oeil | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — Coque Linux : titre de la carte

**Validation** : HUMAIN — **Issues** : #209 (lisibilite du titre)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Coque tmux : projet en `default`, `auto`, `none` (global active puis desactive) | titre « default (masques 30 s) » / « default (desactive) » / « auto hide ◆ » / « no hide ◆ » ; la losange marque le choix propre au projet | | |
| 2 | Modifier `PROJECT_HIDE_IDLE` dans `config.conf` a la main | pris en compte au tour suivant, sans relance | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Scenarios 1 a 3 PASS
- [ ] Aucune regression sur #188 (`go test -run 188 ./ui/desktop/`)

## Notes QA

Scenarios 2 et 3 : validation humaine (rendu). Automatise (scenario 1) : `hide_eye.js` (marque `own`, absence de puce), `TestHideEyeStyleV2209` (CSS), `TestHideTitle209` (libelles coque).
