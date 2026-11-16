# Memoire projet — Claude-Launcher

> Source de verite unique au demarrage d'une session (/start-session). Mise a jour par /end-session.

## Etat courant

- **Version** : **6.2.0 publiée en PROD le 2026-11-16** — PR #180 (merge 6a44e52), tag v6.2.0, release GitHub
  (linux/darwin amd64+arm64, exe Windows, SHA256SUMS), run release 43836671891. Binaire du hub remplacé
  (`claude-launcher-ng2.bak-v6.1.0` conservé ; l'ancien exe Windows n'a PAS été sauvegardé). Site gh-pages v6.2.0 en
  ligne (PR #181, 78d9e06, section #v62).
- **Branche** : main (6a44e52).
- **Travail en cours** : aucun workflow actif. Milestone #25 v6.2.0 clos (10 issues : #150 #151 #152 #153 #154 #158
  #162 #163 #164 ; #156 fermée « not planned », déjà fonctionnelle nativement).
- **Prochain** : milestone #26 **v6.3.0 « Kubernetisation du launcher »**, non démarré (pas de due date, aucun label de
  phase) : #165 à #179 (15 issues, 5 lots indicatifs). #179 (accélérer les tests `ui/desktop`) est à prendre en premier.

## Decisions techniques de la session

- **v6.2.0** : livrée avec cinq lots (#150, #151/#152/#162, #153, #154/#158/#163/#164), chacun validé en QUALIF.
- **#154** : verrous git = `describe --dirty` supprimé (GIT_OPTIONAL_LOCKS=0, arrêt SIGTERM). `scripts/ci.sh` isole
  tmux dans un dossier dédié sous `/tmp` et échoue sur socket de test ou `index.lock` résiduel (préfixes de test
  seulement : le launcher réel utilise `claude-desk-ng2`, `cl`, `cl-hidden`). `ci.sh` lance `go test -race` (gcc) et
  `node --check` (node) ; ces deux outils sont ajoutés à `.claude-launcher/env.yaml`.
- **#158** : Rafraîchir relit tout sans caches. Forcer la fermeture : nettoyage local, `projects.json` jamais touché,
  refusée si un autre launcher pilote ou si le projet revit avant la confirmation (question 2 min). **Backend
  injoignable : proposée avec avertissement, nettoyage local seul.** Git « dubious ownership » sous Windows :
  `safe.directory` passé par appel (GIT_CONFIG_*) pour les seuls appels git/gh du launcher ; config globale jamais écrite.
- **#163** : interface web servie par la fenêtre native, `desktop_web` désactivé par défaut. Hors boucle locale refusé
  sans `allow_lan: true`. Cookie = session en mémoire (`cl_session_<port>`, 64 max, 8 h) ; jeton renouvelé au
  changement d'adresse. Client web : pas de presse-papiers, quit, thème, webUIOpen, webUIRegen, ni modification de
  `desktop_web` / `browser_server`. Syntaxe : port seul `"8770"` = boucle locale ; `":8770"` = toutes les interfaces.
- **#153** : organisations `top` (défaut, identique à v6.1.0) et `left`. `LAYOUT_MODE`, `LAYOUT_LEFT_LEADER_PCT` (46),
  exception `PROJECT_LAYOUTS` (clé = nom du dossier). Bouton de bascule à droite des onglets. Sous Windows : mise en
  page avant persistance, commandes tmux regroupées (2 allers-retours), dernier clic gagnant. Traces « bascule : … »
  dans `desktop-perf.log` du cache.
- **#151 / #152 / #162** : versions dans le titre de la carte TEAM, `⚠` remplace `▲`. En-tête des panes vert/gris
  (ligne de titre tmux par pane sous Linux). Onglet Teamleaders isolé, réduit à l'icône au débordement.
- **#150** : Ctrl+molette par zone (arbre, cartes, terminaux), 8–28, mémorisé par poste.
- **Sécurité `--web`** (audit 95/100 sur LOT-3.3, puis 78/100 sur LOT-4 avec corrections M1, M2) : jeton d'URL
  seulement sur la page d'amorçage, puis cookie ; `/api/call` JSON + même origine ; contrôle Host ; en-têtes de sécurité ;
  `backendTest` limité à 2.
- **LOT-3.3 `config.conf`** : jeton GitHub jamais affiché ; `GITHUB_TOKEN="__GARDER__"` dans « Éditer en YAML »
  (laisser = conserver, autre valeur = remplacer, `""` = retirer). EXTRA_ENVS : variables sensibles (LD_*, PATH, proxys,
  ANTHROPIC_*, EDITOR, SHELL, HOME…) refusées en saisie structurée, ajout possible par l'éditeur YAML. Substitution
  `$NOM` limitée (HOME, USER, LOGNAME, PWD, XDG_*, GITHUB_DIR, CLAUDE_CONFIG_DIR).
- **Copier/coller Windows (LOT-4 v6.1.0)** : copie à la fin de la sélection + Ctrl+Maj+C (Ctrl+C = interruption) ;
  Ctrl+V / Ctrl+Maj+V / Maj+Inser / clic droit collent ; Ctrl+B puis Ctrl+V envoie ^V brut ; lecture seule : copie oui,
  collage refusé ; presse-papiers Wails côté Go, `--web` = presse-papiers du navigateur ; collage ≤ 4 Mo.
- **Interfaces** : fenêtre native et `--web` ne tournent pas dans le même processus ; deux processus sur la même machine
  se croient tous deux propriétaires (pas d'issue, jugé marginal par l'utilisateur). macOS = coque tmux + navigateur,
  pas de fenêtre native.
- **Outil** `chrome-headless-shell` ajouté à `.claude-launcher/env.yaml` (PR #159) pour les captures d'écran.

## Points pour la prochaine session

- Relancer le hub si pas fait (Échap puis `l`) ; `pkill -f 'claude-launcher-ng2 mirror'` si d'anciens mirror/layout-watch
  tournent.
- Démarrer v6.3.0 par #179 : `scripts/ci.sh` à 117 s, `-race` ui/desktop à 111 s, budget indicatif 60 s.
- Dossier du hub : 20 binaires QUALIF 6.2.0.1–6.2.0.10 et deux `.bak` (dont `.bak-v6.1.0`) — nettoyage à décider.
- Suites mineures notées, non créées en issues : verrou d'écriture de la bascule sans borne si le partage est mort ;
  message de repli `top` répété ; `config.conf` écrit non atomiquement ; procédure de déploiement : l'exe Windows n'est
  pas sauvegardé avant remplacement.
- Idée discutée, non retenue pour l'instant : opérateur k8s + CRD `ClaudeProject` (piste après v6.3.0).
- Maquettes : proposer d'emblée les variantes (horizontale ET verticale) quand une mise en page est en jeu.

## Regles critiques

- Règles permanentes du projet (tests isolés, milestone non démarré, maquette, README) : `.claude/agents/context/COMMON.md`.
- Branches, PR et fusion vers `main` : `CLAUDE.md` « Conventions Git ».

## Corrections de comportement

- Les agents doivent vérifier sur Windows réel ce qu'ils mesurent sur Linux : trois refus du GATE LOT-5 viennent de
  mesures simulées (tour lent, FS local) qui ne reproduisaient pas wsl.exe / partage 9P.
