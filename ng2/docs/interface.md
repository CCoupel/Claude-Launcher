# Interface

> Autres guides : [Installation et lancement](installation.md) · [Configuration et projets en conteneur](configuration.md) · [Launcher Windows natif](windows.md) · [Déploiement dans un cluster Kubernetes](kubernetes.md) · [Versions, mises à jour et template](versions.md) · [Architecture](architecture.md) · [Dépannage](depannage.md) · [Tests](tests.md). Vue d'ensemble : [README](../README.md).

**Sommaire**

- [Interface](#interface)

---

## Interface

- **Gauche** : navigateur (40 colonnes par défaut). Projets en arbre, deux badges git
  puis le badge template (`✔`, `⊘`, `⚠`), consommation Claude :
  - **local** : `●` au repos sur main · `✎` branche, non commité · `◆` branche, tout
    commité · `!` modifié directement sur main ;
  - **remote** (état du dernier fetch) : `○` jamais poussé · `↑N` non poussé · `⇡` poussé,
    non mergé dans main · `✓` à jour / mergé · `↓N` en retard.

  Le détail affiche aussi la version locale (`git describe --tags`, suffixe `-dirty` déduit de `git status`) et la dernière
  release publiée sur GitHub, avec sa date (`v5.7.4 GitHub, il y a 2 j`) ; un dernier tag git
  d'`origin/main` différent est signalé en jaune (`· tag git v5.7.1` : tags pas récupérés, ou tag
  sans release). Sans release GitHub connue (dépôt hors GitHub, pas encore lue), le tag git seul,
  marqué `(tag git)`. La même release GitHub est affichée à côté du nom du projet dans le
  navigateur (calée à droite dans le bandeau d'un projet ouvert), si la ligne a la place après les badges.
  Le point devant le nom est l'état du projet : `○` rien ne tourne, `●` bleu conteneur prêt sans
  claude, `▲` vert ouvert ici (bandeau à la couleur de son pane tmux sur toute la largeur de la ligne, badges à droite), `●` vert lecture seule,
  `■` orange piloté par un autre hub, `●` orange personne ne pilote, `✕` rouge problème (voir
  [Fermer un projet](configuration.md#fermer-un-projet)). Ctrl+clic ou Suppr ferme le projet ; sans conteneur, pod ni session, Suppr
  propose la fermeture forcée (voir [Rafraîchir et forcer la fermeture](configuration.md#rafraîchir-et-forcer-la-fermeture-v620-158)).
  Badge milestone `⚑` (projets GitHub), calculé avec **la règle du panneau milestone des projets**
  (`skills/milestone-panel`), pour que launcher et projet ouvert soient d'accord : **jaune** = milestone
  en cours : le milestone ouvert portant une **due date** (posée par MILESTONE START, la plus ancienne) ;
  sans aucune due date (convention antérieure), le premier par version ayant une issue ouverte dans une
  phase `PRET`…`DONE` — des issues en `PLANNING` seules ne démarrent pas un milestone (v5.6.1) ;
  **orange** = le milestone daté a toutes ses issues fermées (ou, sans due date, à défaut un milestone
  ouvert dont toutes les issues sont fermées) : à publier, jusqu'à sa clôture ; **gris** = des milestones ouverts mais aucun en cours (le panneau : « Aucun milestone en
  cours ») ; **vert** = plus rien d'ouvert, dernier milestone clos (publié) ; rien sans milestone.
  Badge PR `⇄N` (violet, projets GitHub) : N pull requests ouvertes en attente (`gh pr list --state
  open`, brouillons compris ; la légende précise « dont N brouillon(s) ») ; rien sans PR ouverte.
  Les cartes de la vue `◈ État` ont leur titre sur un bandeau de fond gris foncé, sur toute la largeur
  (nom en gras, précision comme `· lu il y a 18 h` atténuée), pour que chaque carte ressorte (v5.6.2).
  La carte **Projet** de la vue `◈ État` (et l'aperçu) commence par la légende des icônes de la ligne
  du projet : chaque icône, dans sa couleur et dans l'ordre du navigateur, suivie de sa signification
  (`!   local : modifié directement sur main`, `↺   session non close (il y a 12 min) : reprise
  proposée à l'ouverture`…). Ligne et légende sont tirées de la même source (`ui/tmux/legend.go`).
  La carte **Milestone** de la vue `◈ État` (et l'aperçu) donne le milestone en cours, sa barre
  d'avancement (issues fermées / total ; elle prend la largeur du pane, de 10 à 40 cellules), ses issues ouvertes par phase et les bloquées ; sans milestone
  en cours, la liste des milestones ouverts, une ligne chacun, colonnes alignées : `v1.2.0 — Titre  n/m  ███░░░
  P%` (v5.6.2). Pane étroit : on retire dans l'ordre la barre, puis la fin du titre (`…`, la version
  reste entière), puis le pourcentage ; au plus étroit, `v1.2.0 n/m`. Quatre appels `gh` par projet (ceux du
  panneau : milestones, issues ouvertes ; plus les PR ouvertes et la dernière release) toutes les 5 min, `F5` pour forcer ; cache partagé
  `~/.cache/claude-launcher-ng2/milestones-v2.json`.
  Un projet fermé dont la dernière conversation est restée en suspens (non close par `/end-session`
  ni par une sortie propre, de moins de `RESUME_MAX_AGE_H` heures, ouverte nulle part) porte `↺` (rouge si sa team montre un
  crash) : c'est la session que son ouverture proposera de reprendre.
  Un dossier de projet créé, renommé ou supprimé hors du launcher apparaît ou disparaît sans
  `Ctrl+R` : seuls les racines et dossiers de catégorie sont surveillés (inotify sur disque Linux ;
  sur `/mnt/c`, où inotify est muet, leur date comparée toutes les 3 s, soit quelques `stat`), et
  l'arbre n'est relu qu'après un changement (`core/treewatch.go`).
  Sous la consommation, une ligne jaune `⚠ connexion claude.ai : expiration …` apparaît 3 jours
  avant l'expiration de la connexion claude.ai (jeton de renouvellement, ~30 jours), rouge
  `✗ connexion claude.ai expirée` ensuite ou si l'endpoint refuse le jeton : un clic ouvre une
  fenêtre `login` qui lance `claude /login`. Après un `/login`, la consommation revient sans
  attendre la fin de la pause qui suit un échec ; `F5` la relit immédiatement.
  L'en-tête indique les versions : `NG2 v5.0.0 · tpl 4.2.1` (NG, dernier tag du template).
- **Sous le navigateur** : `◈ État`, qui suit la sélection (git, versions, template, team —
  teammates du registre du template, version de leur copie, `+` si compagnon, modèle et effort,
  version de claude des teammates lancés —, fichiers) — lu depuis un cache préparé en tâche de fond. La zone de droite ne bouge pas.
- **Carte Team** (dans `◈ État`) : une ligne par teammate lancé, plus les teammates du template non démarrés
  (`●` démarré, `○` non démarré), sans regroupement ; son titre porte les versions une seule fois, « TEAM · N · TPL vX ·
  CLAUDE Y » (template, claude du leader), et les lignes n'ont plus de colonne de version : la carte est plus étroite (#151).
  `⚠` orange sur un teammate dont la version diffère (template ou claude) ; sous Windows, le détail est en infobulle.
  **Modèle et effort** (v6.9.0, #250) : les colonnes modèle et effort de chaque ligne se règlent depuis la carte. Fenêtre
  native : clic → menu ; coque tmux : clic sur la colonne → `display-menu`. Chaque entrée écrit le **compagnon projet**
  `.claude/agents/<agent>.md` (`model:` / `effort:`) ; la valeur prise effet au **prochain spawn** du teammate (⟳ sur la
  ligne tant qu'un teammate tourne avec une autre valeur). Entrée « Défaut (template) » : retire la clé du compagnon, le
  teammate suit le template. Un effort non pris en charge par le modèle est grisé (sans raccourci). Le **teamleader**
  réglé aussi `.claude/settings.json` (`model`, `effortLevel`) : la session en cours garde son modèle, à changer avec
  `/model` / `/effort`. Accès distant (navigateur hors boucle locale) : réglage refusé, réservé à la fenêtre native.
  Raccourcis du menu tmux : `d` Défaut modèle, `o` opus, `s` sonnet, `h` haiku, `f` fable ; `0` Défaut effort, `1`–`5`
  low → max ; `t` « Tout par défaut » (modèle et effort). Message de résultat 6 s sous la liste (vert ; jaune pour le
  teamleader ou un refus). Contrat : `contracts/desktop-dispatch.md` § v6.9.0.
  **Surbrillance** = son pane est visible dans la fenêtre du
  projet. Double-clic sur un teammate inactif : visible ↔ caché. Un teammate caché sort de la fenêtre du projet,
  mais continue de tourner et reçoit ses messages. Règle : un teammate **actif est toujours visible** (double-clic
  refusé, message dans la carte) ; un inactif est caché après `TEAM_HIDE_IDLE` secondes (`config.conf`, 30 par
  défaut, `0` = jamais) et revient dès qu'il redevient actif. Un inactif rendu visible à la main reste visible
  jusqu'au second double-clic ; s'il redevient actif, il reprend la règle normale. Le leader n'est jamais caché.
  En lecture seule (un autre launcher a la main), le double-clic est refusé avec un message. Fermer le projet
  ferme aussi ses teammates cachés.

  **Commutateur** (v6.3.0, #188) : Réglages › Launcher, groupe « teams », « Cacher les teammates inactifs »
  (`TEAM_HIDE_IDLE_ENABLED` dans `config.conf`, **activé par défaut**). Le délai `TEAM_HIDE_IDLE` est en **secondes** et
  n'est pas modifié quand le commutateur est éteint. Éteint, tout teammate reste visible, les marques « caché à la main »
  sont effacées, et le double-clic est refusé avec un message. L'état du masquage est porté par l'œil du titre de la carte TEAM (voir « Réglage par projet »). Le rallumage reprend la règle avec le délai conservé.

  **Réglage par projet** (v6.5.0, #209) : un **œil** à droite du titre de la carte TEAM porte le choix du projet.
  Œil **ouvert** = *no hide* (aucun teammate masqué dans ce projet) ; œil **fermé** = *auto hide* (inactifs masqués
  après le délai `TEAM_HIDE_IDLE`) ; œil **grisé** = *default* (le projet suit le commutateur global ; le dessin montre
  son effet). Quand le projet a son propre choix (*auto hide* ou *no hide*), l'œil est en **bouton enfoncé** (fond plein de la couleur d'accent) ; en *default* il reste gris, sans fond. L'œil est le dernier élément du titre, aligné à droite : les versions sont tronquées avant lui. Un **clic** fait défiler default → auto hide → no hide → default ; un **clic droit** (ou la touche Menu)
  ouvre un menu pour choisir directement. L'infobulle donne l'état, le suivant et la valeur globale. Le choix est
  enregistré dans `PROJECT_HIDE_IDLE` de `config.conf` et pris en compte sans relance ; changer le réglage global ne
  touche que les projets en *default*. En *no hide*, le double-clic pour cacher un teammate est refusé.
  **État du template** (par ligne, #136) : `●` vert = la copie de l'agent est à jour ; `⚠` orange = écart de version
  (template en retard sur la version cible, le launcher ayant adopté une version plus récente, ou claude différent de
  celui du leader) ; `○` = non démarré. Les versions sont dans le titre de la carte (#151). `+` devant le nom
  = l'agent a un compagnon projet (`.claude/agents/<agent>.md`, ou `generic.<nom>.md` pour une instance générique).
  Un projet sans registre d'agents (`agents.registry`) garde l'affichage d'avant : ● neutre, pas de version.
  *Limites (v6.1.0)* : lors d'une prise de main sur un conteneur, deux launchers peuvent appliquer la règle
  pendant au plus 5 s (effet borné : une opération en erreur est ignorée et rejouée au tour suivant). Un launcher
  antérieur à v6.1.0 ignore les teammates cachés : ils restent vivants dans la réserve jusqu'au retour d'un
  launcher v6.1.0, et sa fermeture de projet ne les tue pas.
- **Teamleader en attente** (v6.7.0, #223) : quand le teamleader d'un projet pose une question (`AskUserQuestion`)
  ou demande une permission, le projet porte un badge orange « ❓ en attente » (arbre, pastille de l'onglet, `❓` dans
  le titre et soulignement de l'onglet, pastille `⌗ Teamleaders` en contour orange, vignette en bordure orange avec le
  texte de la question au-dessus du terminal). Le badge disparaît à la réponse. Les notifications système et du
  navigateur sont décrites dans [Notifications d'attente](configuration.md#notifications-dattente-du-teamleader-v670-223).
  Un clic sur la notification ouvre l'onglet du projet. Dans la coque tmux : badge seul, sans notification système.
  Limite : si claude est tué alors qu'il attend, le badge reste jusqu'à la prochaine réponse de la session, ou 24 h.
- **Droite** : tmux, un onglet par projet, plus l'onglet épinglé `⌗ Teamleaders` (une
  vignette interactive par projet ouvert). Le teamleader d'une fenêtre est son premier pane
  créé (plus petit `%id`), jamais le pane actif ni le premier par position : rouvrir un projet
  en conteneur alors qu'un teammate a le focus n'affiche plus ce teammate à la place du leader (v5.7.1).
  La vignette relit aussi toutes les 2 s si la fenêtre est en conteneur : née avant que
  l'ouverture du pod pose `@cl_backend`/`@cl_ctr`, elle ne reste plus figée sur le pane du hub
  (toute la fenêtre du conteneur, dont elle ne montrait que le bas : les teammates) (v5.7.2).
- **En-tête des panes de teammates** (#152) : une ligne de titre tmux en tête de chaque pane (`pane-border-status top`,
  posé par le launcher qui a la main sur le projet). Bandeau vert si le teammate est actif, gris s'il est inactif, neutre
  pour le leader ; l'état suit la mise en page sans relance. Un launcher antérieur ignore ces options : il n'affiche pas
  les bandeaux, sans autre effet.
- **Mise en page des panes** (#153) : `top` (défaut) ou `left` (leader à gauche), réglable globalement et par projet ;
  voir [Organisation des panes](configuration.md#organisation-des-panes--top-ou-left-v620-153).

| Action | Effet |
|---|---|
| `↑` `↓` `PgUp` `PgDn`, molette, clic sur un projet | sélectionner → détail sous le navigateur (un clic sur un projet ouvert bascule sur sa fenêtre) |
| `Entrée`, double-clic sur un projet | ouvrir / rejoindre le projet |
| clic hors projet (catégorie, zone vide, en-tête) | onglet `⌗ Teamleaders` |
| double-clic sur une catégorie ou une racine | créer un projet dans ce dossier |
| `✦ Nouveau projet` | créer un projet à la racine (choix de la racine si plusieurs) |
| frappe | filtrer |
| `Ctrl+←` `Ctrl+→` ou glisser la bordure | redimensionner la zone navigateur (4 colonnes par appui) |
| `Tab`, `Alt+←/→` | passer du navigateur à tmux |
| `Ctrl+B R` dans un projet | remettre en ordre les panes de la team |
| double-clic sur un pane d'une team | plein écran (comme `Ctrl+B z`) ; double-clic ou `Ctrl+B z` à nouveau pour revenir en taille normale |
| `Maj`+glisser (terminal hôte) | sélectionner en contournant tmux (la souris est captée par tmux) ; la sélection traverse les panes, bordures comprises. Copie dans le presse-papiers : renvoi à #156 |
| double-clic sur un teammate inactif (carte Team) | visible ↔ caché ; refusé pour un teammate actif (message dans la carte) |
| clic sur la colonne modèle ou effort (carte Team, coque tmux) | menu de réglage ; touches `d` `o` `s` `h` `f` (modèle), `0` `1`–`5` (effort), `t` (tout par défaut) |
| clic sur `↑ NG … cliquer pour installer` (vert) | installer la nouvelle version, déjà téléchargée et contrôlée, et redémarrer le navigateur (projets conservés) |
| clic sur `↑ NG … réessayer` (jaune) | relancer tout de suite le téléchargement de la nouvelle version |
| clic sur `↑ tpl … cliquer pour installer` (vert) | adopter la nouvelle version du template |
| `Ctrl+U`, clic sur `✕` | masquer une annonce de mise à jour |
| clic sur `⚠ connexion claude.ai …` / `✗ … expirée` | ouvrir une fenêtre `login` qui lance `claude /login` |
| `F5` | rechercher les mises à jour, relire la consommation et la liste des projets maintenant (réaffiche les annonces masquées) ; relit aussi le projet sélectionné, pied « ⟳ … relu » (#158) |
| `F1` | aide : commandes et raccourcis, dans un `display-popup` tmux par-dessus la coque |
| `Esc` | effacer le filtre ; sinon quitter : `l` launcher seul (projets conservés), `t` puis `o` tout fermer |

Les commandes `Esc`, `Ctrl+C`, `F1`, `F2`, `F5`, `Ctrl+R`, `Ctrl+U`, `Tab`, `Entrée`, `Suppr` et
`Ctrl+←/→` marchent aussi dans le pane des cartes (sous le navigateur) : elles sont relayées au
navigateur, qui prend le focus pour la suite (question de fermeture après `Esc`, etc.). Les flèches,
`PgUp`/`PgDn`, `j`/`k` y font défiler les cartes ; `q` ne ferme plus ce pane.

Création d'un projet : le formulaire demande le nom, puis le type de projet :

- **géré** (`g`, par défaut) : remote git facultatif ; `git init`, `origin` ajouté si un remote
  est donné, `init-project.md` déposé, et claude démarre directement sur `/init-project` ;
- **brut** (`b`) : seulement le dossier et un `CLAUDE.md` minimal, ni git ni `/init-project`.

Le projet est ensuite lancé. (Rouvert plus tard, un projet brut reçoit `init-project.md` comme
les autres : `/init-project` reste possible pour le convertir.)

**Démarrer la session (`/start-session`, v6.7.0, #231).** Au lancement neuf de claude (`Entrée` sur le projet, sans
reprise de conversation), un projet géré qui n'a pas encore de team demande, dans le terminal du projet :

```
session non démarrée — lancer /start-session ? [O/n]
```

- `o` ou `Entrée` : claude démarre sur `/start-session` ; autre réponse : claude démarre sans.
- **Ordre des questions** : la question `/init-project` (template en retard ou manquant) passe **avant**. Si vous
  l'acceptez, claude part sur `/init-project` et `/start-session` n'est pas proposé (un seul argument possible).
- **Critère** : le projet est géré (`.claude/commands/start-session.md` présent) et sa team n'existe pas. La team est
  cherchée dans `teams/<team_name>/config.json` du `CLAUDE_CONFIG_DIR` du launcher, `team_name` lu dans
  `.claude/project-config.json` (à défaut, le premier membre dont le `cwd` est le projet).
- **Pas de question** : projet non géré, team existante (reprise), reprise de conversation choisie, création depuis le
  launcher, rattachement à une session tmux déjà vivante.
- **Limite** : le launcher ne voit que le `CLAUDE_CONFIG_DIR` qu'il connaît. Si claude utilise un autre répertoire de
  config (projet ou conteneur), la question peut être posée à tort ; refusez-la, `/start-session` détecte lui-même une
  reprise.
- La question n'est posée qu'une fois par lancement de claude.
