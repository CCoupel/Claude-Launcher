# Procedure de Test — Nouveau projet : Projet brut, Template projet, Depot Git (#215)

**Version** : 6.6.0
**Date** : 2026-11-21
**Testeur** : utilisateur (validation humaine, label `VALID-HUMAINE`)

Reference : `contracts/desktop-dispatch.md` § v6.6.0 (`create` source `git` / `template`, `newJob`, `createCancel`, `initProposal`). Equivalents automatises (faux `git`, jamais de reseau) : `ng2/core/clone215_test.go`, `ng2/core/clone_test.go`, `ng2/ui/desktop/create215_test.go`. Automatise, donc a ne pas rejouer a la main : URL refusees, jeton absent de argv / `.git/config` / messages, nettoyage complet sur echec ou annulation, une seule creation a la fois.

## Prerequis

- [ ] Environnement : QUALIF — exe natif **Windows** 6.6.0.x, puis **pod du launcher** du cluster QUALIF (interface web)
- [ ] Donnees : un **depot Git de test** que tu possedes (petit, public ou prive) ; pour le prive : `GITHUB_TOKEN` renseigne dans `config.conf` (jamais colle dans l'URL) ; base factice (`C:\cl-test\PROJETS`, puis base 9P `\\wsl.localhost\<distro>\…`)
- [ ] Acces : ecriture sur la base ; aucun vrai projet de travail

## Scenarios

### Scenario 1 — Clone reel depuis l'exe Windows (base locale)

**Validation** : HUMAIN — **Issues** : #215

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Nouveau projet > source « Depot Git » (⎇), URL HTTPS du depot de test, categorie neuve `CLONES`, nom `essai-215` | Ligne d'authentification : jeton present / absent, sans jamais afficher sa valeur | | |
| 2 | Valider | Progression visible (etape + pourcentage) et fenetre reactive pendant le clonage | | |
| 3 | Attendre la fin | Projet ouvert ; dossier `…\CLONES\essai-215` contient le depot ; categorie creee | | |
| 4 | Ouvrir `.git\config` du projet dans un editeur | Une seule origine = l'URL, **aucun jeton**, aucun credential helper | | |
| 5 | Chercher le jeton dans les messages de la fenetre (bandeau, journal) | Introuvable | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Echec d'authentification et annulation (exe Windows)

**Validation** : HUMAIN — **Issues** : #215

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Retirer le jeton de `config.conf`, creer depuis un depot **prive**, categorie neuve `ECHEC` | Message lisible « authentification refusee (HTTPS, jeton GITHUB_TOKEN de config.conf) » + dossier supprime | | |
| 2 | Verifier le disque | Ni le dossier du projet ni la categorie `ECHEC` ; rien de nouveau dans l'arbre | | |
| 3 | Lancer un gros clonage puis cliquer « Annuler » | Etat « annulee — dossier supprime », processus git absent du Gestionnaire des taches, dossier et categorie neuve retires | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — Clone sur base 9P (`\\wsl.localhost\…`)

**Validation** : HUMAIN — **Issues** : #215

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Cloner le depot de test dans la base 9P | Clonage termine sans erreur de chemin ou de permission ; duree acceptable | | |
| 2 | Ouvrir le projet | Ouverture normale, arbre git lisible | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — Clone depuis le pod QUALIF (cle ssh du coffre)

**Validation** : HUMAIN — **Issues** : #215

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Interface web du pod, source Git en HTTPS (jeton) | Clonage reussi sur le volume partage ; projet ouvert | | |
| 2 | Source Git en `git@github.com:…` avec cle et `known_hosts` du coffre pour le backend | Clonage reussi ; hote inconnu → message « known_hosts du coffre a completer » | | |
| 3 | Dans le pod : `ps` pendant un clonage | Aucun jeton dans les arguments des processus | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 5 — Source « Template projet » : proposition de /init-project

**Validation** : HUMAIN — **Issues** : #215

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Nouveau projet > source « Template projet » (◆), nom `tpl-215` (remote facultatif : laisser vide) | Projet cree (dossier + `git init`, aucun `CLAUDE.md` minimal) et ouvert ; `/init-project` n'est **pas** lance d'office | | |
| 2 | Observer la fenetre | Bandeau « Lancer /init-project ? » apparait une seule fois | | |
| 3 | Cliquer « Plus tard » | Bandeau retire ; ne revient pas tant que le launcher tourne | | |
| 4 | Relancer le launcher, rouvrir le projet | Bandeau propose de nouveau (marqueur `.claude-launcher/init-proposed` conserve) | | |
| 5 | Cliquer « Lancer » | `/init-project` tape dans le leader et execute ; marqueur supprime ; bandeau ne revient plus | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 6 — Depot clone avec un `env.yaml` : approbation avant tout demarrage, redemandee a chaque changement (audit)

**Validation** : HUMAIN — **Issues** : #215

Prerequis du scenario : un **depot de test** contenant `.claude-launcher/env.yaml` avec une commande `setup` inoffensive qui laisse une preuve (ex. `touch /tmp/PREUVE-ENV-215`), un montage hors du projet (ex. `/tmp`), une commande `on_start` **tres longue** (plus de 200 caracteres, avec la fin reperable : `…FIN-DE-COMMANDE`), des `packages_by_manager` (ex. `apt: [jq]`) et du texte piege dans un champ : `<b>gras</b>`, la sequence ESC `\x1b[31mROUGE`, et des caracteres invisibles / bidirectionnels (U+202E inversion de sens, U+200B espace nul, U+2066). Pour le pod : memes contenus, preuve dans le pod.

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Cloner ce depot (source Git) sur un backend **conteneur**, socle au choix | Clonage termine ; **rien n'est demarre** : aucun conteneur, la preuve `/tmp/PREUVE-ENV-215` n'existe pas | | |
| 2 | Ouvrir le projet | Dialogue d'approbation (maquette A) : resume (image, paquets **y compris `packages_by_manager`**, commandes, montages hors projet, risque) + fichiers bruts ; boutons Accepter / Ouvrir sans / Annuler | | |
| 3 | Lire le texte piege et la commande longue | `<b>gras</b>` affiche tel quel ; sequence ESC non interpretee (pas de couleur, ni fenetre ni coque) ; caracteres bidi / invisibles neutralises (visibles comme un signe, texte non retourne) ; la commande longue est affichee **en entier** (la fin `FIN-DE-COMMANDE` est lisible, defilement ou retour a la ligne) | | |
| 4 | « Annuler » | Rien n'est ecrit ni ouvert ; la question revient a la prochaine ouverture | | |
| 5 | « Ouvrir sans » | Projet ouvert avec le seul socle choisi : la commande `setup`, le montage et la preuve sont absents | | |
| 6 | Fermer. Modifier `env.yaml` **dans le depot** (simuler un `git pull` : ajouter une commande `setup`), rouvrir | La question **revient** (approbation non definitive : toute modification, apres « Ouvrir sans » comme apres « Accepter ») ; le contenu modifie n'est pas applique avant reponse | | |
| 7 | « Accepter » puis refaire l'etape 6 | La question revient encore une fois | | |
| 8 | Dans le launcher : Reglages du projet > modifier l'environnement (image, paquets) puis enregistrer, rouvrir | **Aucune** nouvelle question : une modification faite par le launcher lui-meme n'est pas redemandee | | |
| 9 | Cloner un depot **sans** `env.yaml`, l'ouvrir en conteneur (aucune question), puis ajouter `.claude-launcher/env.yaml` dans le depot (pull) et rouvrir | La question apparait (un environnement apparu apres coup n'est pas de confiance) | | |
| 10 | Corrompre `trust.json` (dossier de config du launcher : tronquer le fichier ou y ecrire `{`), rouvrir un projet clone en conteneur | Refus ferme : message clair, rien n'est demarre, le fichier n'est **pas reecrit** (le comparer a la copie faite avant) ; restaurer ensuite le fichier | | |
| 11 | Reglages du projet > « Revoir » (projet ferme) puis rouvrir | La question revient ; « Accepter » : le projet demarre avec l'environnement du depot, la preuve apparait | | |
| 12 | Projet affecte a l'hote : cloner le meme depot sur l'hote | Aucune question (env.yaml inutilise) ; elle apparait au premier passage en conteneur (F2) | | |
| 13 | **Registre partage** (exe Windows et hub sur la **meme racine de base** `CL_ROOT_BASE`) : verifier que `<racine>\.config\claude-launcher-ng2\trust.json` existe et contient l'entree du projet clone (`pending`, `accepted` ou `ignored`) ; aucun `trust.json` dans le dossier de config local de l'un ou l'autre poste | Un seul registre, sous la racine de base | | |
| 14 | Cloner depuis l'**exe Windows** un depot avec `env.yaml`, **Accepter**. Ouvrir le meme projet depuis le **hub** (coque tmux, backend conteneur) | **Aucune** nouvelle question : l'approbation donnee depuis l'exe est vue par le hub | | |
| 15 | Cloner un autre depot avec `env.yaml` depuis l'exe, ouvrir depuis le **hub** et **Annuler** puis « Ouvrir sans » | Le hub memorise « ouvrir sans » ; depuis l'**exe**, l'ouverture suivante du meme projet ne demande rien et n'applique pas l'`env.yaml` du depot (socle seul) | | |
| 16 | Poser un ancien `trust.json` **local** (dossier de config du poste, entree `accepted` pour un projet deja `pending` dans le registre partage) puis relancer le launcher | Migration au demarrage : le registre partage garde l'etat **le plus restrictif** (`pending`), `trust.json.bak` cree, l'ancien fichier local est conserve ; l'approbation locale ne vaut pas | | |
| 17 | Meme parcours dans la **coque tmux** (hub) : touches `a` accepter, `s` ouvrir sans, `Echap` annuler, `v` fichiers bruts | Memes resultats (dont commande longue entiere, `packages_by_manager`, bidi neutralise) ; texte piege non interprete dans le terminal | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 7 — Source « Projet brut » : dossier + git init, avec et sans remote

**Validation** : HUMAIN — **Issues** : #215

Les trois sources du dialogue sont **Projet brut** (▢), **Template projet** (◆) et **Depot Git** (⎇) ; l'ancienne option « gere » n'existe plus (ni dans la fenetre native, ni dans la coque).

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Nouveau projet : lister les sources proposees | Exactement trois : Projet brut, Template projet, Depot Git ; aucune option « gere » ; textes d'aide : brut « dossier et git init… Pas de /init-project », template « …puis /init-project propose », git « clonage d'une URL » | | |
| 2 | Source « Projet brut », nom `brut-sans-remote`, **champ remote vide**, valider | Projet cree et ouvert ; `git init` fait (`.git` present) ; `git remote -v` **vide** ; aucun `CLAUDE.md` minimal cree ; **aucun** `/init-project` lance ni propose (pas de bandeau) ; pas de marqueur `.claude-launcher/init-proposed` | | |
| 3 | Source « Projet brut », nom `brut-avec-remote`, remote `git@github.com:moi/depot-test.git`, valider | Meme resultat, et `git remote -v` montre `origin` = cette URL ; toujours aucun `/init-project` | | |
| 4 | Source « Projet brut », remote **invalide** (`file:///tmp/x`, ou `-u`) | Refus lisible sous le champ ; dossier non cree (ou supprime) ; rien dans l'arbre | | |
| 5 | Comparer avec « Template projet » (nom `tpl-comp`, meme remote) | `git init` + remote identiques a « brut » ; en plus : marqueur `init-proposed` et bandeau « Lancer /init-project ? » a la 1re ouverture (scenario 5) | | |
| 6 | Meme parcours dans la **coque tmux** : touches `v` (brut), `t` (template), `g` (depot git) a l'etape source | Memes resultats (brut avec et sans remote : `git init`, remote seulement si fourni, pas de `/init-project`) | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Le jeton n'apparait nulle part (argv, `.git/config`, messages, journaux)
- [ ] Aucun dossier ni categorie residuels apres un echec ou une annulation
- [ ] `/init-project` jamais lance sans reponse de l'utilisateur ; jamais propose pour « Projet brut »
- [ ] Aucune option « gere » ni `CLAUDE.md` minimal ; « Projet brut » = dossier + `git init` (+ remote si fourni)
- [ ] L'environnement d'un depot clone n'est jamais applique sans accord explicite ; tout changement du depot (apres accepter comme apres ouvrir sans) est redemande, sauf s'il vient du launcher lui-meme
- [ ] Un registre d'approbation illisible refuse l'ouverture (echec ferme) sans etre reecrit

## Notes QA

Supprimer les dossiers de test a la fin. Ne jamais utiliser un jeton ayant des droits d'ecriture sur un depot de production.
