# Memoire projet — Claude-Launcher

> Source de verite unique au demarrage d'une session (/start-session). Mise a jour par /end-session.

## Etat courant

- **Version** : **6.3.0 publiée le 2026-11-17** — PR #195 (merge 6bea26b), tag v6.3.0, release run 44110550235,
  milestone #26 clos (18 issues). Image `ghcr.io/ccoupel/claude-launcher` et chart OCI
  `ghcr.io/ccoupel/charts/claude-launcher` publics. Site gh-pages v6.3.0 en ligne (PR #197). README racine avec
  Kubernetes (PR #196, f1f23ad).
- **Branche** : main (f1f23ad).
- **Environnements** :
  - QUALIF cluster : namespace `claude-qualif`, racine `/share/MFS/ROOT/CLAUDE-QUALIF`, PV `claude-qualif-pv`, image
    `ghcr.io/ccoupel/claude-launcher-qualif:v6.3.0.9`, `https://launcher-qualif.local`.
  - PROD cluster : namespace `claude-ng2`, launcher v6.3.0 installé (`https://launcher.local`). Noms mDNS `.local`.
  - CA interne cert-manager `claude-launcher-ca` (nameConstraints `launcher.local` / `launcher-qualif.local`).
  - Kubeconfig Rancher ADMIN dans `PRIVATE/{QUALIF,PROD}/kubeconfig` (risque accepté). Registre local abandonné
    (`registries.yaml` géré par Rancher).
- **Travail en cours** : aucun workflow actif. Milestone #26 v6.3.0 clos.
- **Prochain** : milestone v6.4.0 (#28) = #172, #174, #178 (authentification), #189, #190, #191 (onglets), #193
  (kubeconfig par backend), #194 (clients ssh/podman/docker dans l'image). v7.0.0 (#27) = SaaS.

## Decisions techniques de la session

- **v6.3.0** « Kubernetisation du launcher » : livrée en lots (#179 tests `ui/desktop` 119 s → 56 s ; #183/#171/#186/#187 ;
  #165/#166 racine de base `--root-base` ; #169/#176 identité et cache par launcher, écritures partagées sous verrou ;
  #167/#168/#175 image et mode cluster ; #170/#173 chart Helm et Ingress interne ; #177 CI image et chart ; #192 accès
  `none` ; #185 guide d'installation ; #188 masquage des teammates désactivable).
- **Décisions v6.3.0** :
  - Accès web **sans jeton** en cluster (`web.access: none`, défaut du chart) : risque accepté jusqu'à l'auth v6.4.0,
    pas de NetworkPolicy.
  - Contexte k8s **ignoré en cluster** (ServiceAccount du pod).
  - Variables `CL_*` posées directement sur le StatefulSet (pas de ConfigMap).
  - #188 : masquage désactivé = tout réapparaît, marques « caché à la main » effacées ; délai en secondes.
- **v6.2.0** : livrée avec cinq lots (#150, #151/#152/#162, #153, #154/#158/#163/#164), chacun validé en QUALIF.

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
- **Vérifications humaines restantes** (v6.3.0) : `https://launcher.local` depuis un poste ; prise de main hub ↔
  cluster sur la racine réelle ; PSA `baseline` sur `claude-ng2` non posée.
- **À confirmer par l'utilisateur** : 3 StatefulSets de projets à 0/0 depuis 14:45 (2026-11-17).
- **Idées** : `App.Stop` n'attend pas les terminaux (socket tmux relancé dans les tests).
- **Consigne** : concision aux teammates (DONE ≤ 3 lignes, détail dans les rapports).
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
