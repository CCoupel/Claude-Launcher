# Procedure de Test — config.conf pris en compte a chaud, settings.json jamais perime (#225)

**Version** : 6.6.0
**Date** : 2026-11-21
**Testeur** : utilisateur (validation humaine, label `VALID-HUMAINE`)

Reference : `contracts/desktop-dispatch.md` § v6.6.0 — LOT-4 « Prise en compte a chaud de `config.conf` ». Cas vise : changer `CLAUDE_DISABLE_MOUSE` **a la main** dans `config.conf`, rouvrir un projet, et constater que la souris suit sans relancer le launcher.

## Prerequis

- [ ] Environnement : QUALIF — exe natif **Windows** 6.6.0.x **et** hub (coque tmux)
- [ ] Donnees : un projet factice `alpha` (jamais un vrai projet) ; sauvegarde de `config.conf` et du `settings.json` de claude (`%USERPROFILE%\.claude\` ou `CLAUDE_CONFIG_DIR`) avant de commencer
- [ ] Acces : un editeur de texte sur `config.conf` (dossier de config du launcher) ; le launcher **reste lance** pendant toute la procedure

## Scenarios

### Scenario 1 — Souris 0 → 1 a la main, puis ouverture d'un projet (exe Windows)

**Validation** : HUMAIN — **Issues** : #225

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Lancer l'exe ; dans `config.conf`, mettre `CLAUDE_DISABLE_MOUSE=0` (enregistrer), rouvrir `alpha`, lire `settings.json` | La variable `CLAUDE_CODE_DISABLE_MOUSE` est **absente** de `env` ; la souris fonctionne dans le terminal du projet | | |
| 2 | **Sans relancer le launcher**, editer `config.conf` dans l'editeur externe : `CLAUDE_DISABLE_MOUSE=1`, enregistrer | Aucune action dans le launcher | | |
| 3 | Attendre ~2 s, ouvrir **un autre** projet (ou fermer/rouvrir `alpha`) | Le projet s'ouvre normalement | | |
| 4 | Lire `settings.json` | `env.CLAUDE_CODE_DISABLE_MOUSE` = `"1"` (la **nouvelle** valeur, pas l'ancienne) | | |
| 5 | Dans le projet ouvert, essayer la souris (clic, molette) | Souris **desactivee** comme demande | | |
| 6 | Remettre `CLAUDE_DISABLE_MOUSE=0` dans l'editeur, enregistrer, rouvrir un projet | Variable retiree de `settings.json` ; souris active | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Autres reglages relus sans relance (exe Windows)

**Validation** : HUMAIN — **Issues** : #225

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Launcher lance, editeur externe : changer `TEAM_HIDE_IDLE` (ex. 30 → 60) ou une couleur de projet, enregistrer | | | |
| 2 | Attendre ~2 s, ouvrir l'ecran Reglages › Configuration | La valeur affichee est la **nouvelle** (sans « Rafraichir » ni relance) | | |
| 3 | Changer `GITHUB_DIR` (racines) a la main | **Non** applique a chaud (racines lues au demarrage) : l'ecran ne pretend pas le contraire | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — config.conf invalide : l'ancienne copie est gardee

**Validation** : HUMAIN — **Issues** : #225

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `CLAUDE_DISABLE_MOUSE=1` valide en place, projet ouvert une fois (settings.json a `"1"`) | | | |
| 2 | Dans l'editeur externe, introduire une erreur de syntaxe (ex. ajouter la ligne `if then ((( fi`), enregistrer | | | |
| 3 | Attendre ~2 s, regarder le pied de fenetre | Message « config.conf invalide : derniere copie valide conservee » (une seule fois) | | |
| 4 | Ouvrir un projet, lire `settings.json` | `CLAUDE_CODE_DISABLE_MOUSE` **toujours a `"1"`** (ni efface ni remis par defaut) | | |
| 5 | Corriger l'erreur, enregistrer, attendre ~2 s | Message disparu ; les nouvelles valeurs sont prises | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — Enregistrement depuis les Reglages (exe Windows)

**Validation** : HUMAIN — **Issues** : #225

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Editeur externe : `CLAUDE_DISABLE_MOUSE=1`, enregistrer ; **sans attendre**, dans Reglages › Configuration changer une AUTRE option et Enregistrer | Enregistrement reussi, pas d'avertissement de recopie | | |
| 2 | Lire `settings.json` | `CLAUDE_CODE_DISABLE_MOUSE` = `"1"` : la valeur du fichier edite a la main n'est pas ecrasee par une copie plus ancienne | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 5 — Coque du hub (tmux)

**Validation** : HUMAIN — **Issues** : #225

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Hub lance (`claude-launcher-ng2`), `CLAUDE_DISABLE_MOUSE=0` dans `config.conf`, ouvrir `alpha` (Entree) | `settings.json` sans la variable | | |
| 2 | **Sans relancer le hub**, passer `CLAUDE_DISABLE_MOUSE=1` dans `config.conf` (autre terminal) | | | |
| 3 | Attendre ~2-4 s (un tour du navigateur), ouvrir un autre projet | `settings.json` : `"1"` | | |
| 4 | Erreur de syntaxe dans `config.conf`, attendre un tour | Ligne d'etat : « config.conf invalide : derniere copie valide conservee » ; reglages inchanges | | |
| 5 | Modifier `GITHUB_DIR` | Non pris a chaud (relance du launcher necessaire, voir #226) | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] La souris (et les autres variables recopiees) suit `config.conf` sans relancer le launcher, exe et hub
- [ ] Aucune ouverture n'ecrit dans `settings.json` une valeur plus ancienne que `config.conf`
- [ ] Un `config.conf` invalide ne change rien et le dit

## Notes QA

Restaurer `config.conf` et `settings.json` sauvegardes.
