# Memoire projet — Claude-Launcher

> Source de verite unique au demarrage d'une session (/start-session). Mise a jour par /end-session.

## Etat courant

- **Version** : **v6.9.0 livré en milestone** (milestone #36, #250 : modèle et effort des teammates depuis la carte TEAM) ;
  QUALIF 6.9.0.1 ; **PROD à venir** (`deployer` au `/deploy prod`). Précédente : 6.8.2 en PROD (PR #243, tag `v6.8.2`, helm `claude-ng2` rév. 8).
- **Session 2026-11-25** : milestone v6.9.0 (#250) livré en LOT-1 unique : CHANGELOG, `ng2/docs/interface.md`, README,
  RETEX. Le numéro de version `a` est géré par `deployer`. Détail : section « Decisions de la session (v6.9.0) ».
- **Session 2026-11-24** : v6.8.2 livrée (#242 : clic souris décalé d'une ligne avec un bandeau, web). Cause : xterm.js 6
  ignore `transform: scale()` ; deux bandeaux la déclenchaient (`#attnbar` demande de notifications, vue projet, web seul ;
  `#qbar` question du teamleader, vignette Teamleaders, web et natif) ; fix `fixScaledMouse()` + `ResizeObserver` sur
  `#views` (documenté `ng2/docs/windows.md`). CI rouge `x/net` GO-2026-5026 corrigée par le bump v0.57.0 (crypto/sys/text
  montés par `go mod tidy`).
- **Branche** : milestone/v6.9.0 (main après la PROD).
- **Environnements** :
  - PROD : namespace `claude-ng2` (v6.8.2 rév. 8 ; v6.9.0 à déployer).
  - QUALIF : 6.9.0.1.
  - Hub : `/share/MFS/ROOT/CLAUDE_PROJETS/claude-launcher-ng2` + `.exe` (à mettre à jour en 6.9.0).
  - CA interne cert-manager `claude-launcher-ca` (nameConstraints `launcher.local` / `launcher-qualif.local`).
  - Kubeconfig Rancher ADMIN dans `PRIVATE/{QUALIF,PROD}/kubeconfig` (risque accepté).
- **Travail en cours** : aucun (v6.9.0 livré ; reste PROD, puis merge `milestone/v6.9.0` → `main`).

## Decisions de la session (v6.9.0)

- **#250** : réglage modèle/effort des teammates depuis la carte TEAM (fenêtre native : clic → menu ; coque tmux :
  `display-menu` ; touches `d`/`o`/`s`/`h`/`f`, `0`/`1`–`5`, `t`). Écrit le compagnon `.claude/agents/<agent>.md`, effet au
  prochain spawn (`⟳`). Teamleader : compagnon + `.claude/settings.json`.
- **Décisions GATE 2** : P1 refus en accès distant (serveur, `teamAgentSet`) ; P2 efforts filtrés selon le modèle (table
  `core.ModelEfforts`, documentée dans le contrat) ; P3 nouvelle valeur + ⟳ ; P4 teamleader = compagnon + settings.json ;
  P5 compagnon vide supprimé ; P6 table dans la doc API + entrée « Défaut (template) » (clé retirée).
- **Revue** : 2 majeurs corrigés (échappement `#` tmux ; réglages concurrents sous verrou relu). CI Windows rouge corrigée
  (test web227 passé en `!windows`). Outil `seed-factice-team.sh` ajouté.
- **Leçon** : les tests de refus distant n'ont pas été rejoués sous Windows.
- **Découpage v7** (GitHub, sans commit) : milestones #27 v7.0.0 « Accès authentifié mono-utilisateur », #37 v7.1.0
  agent+stockage, #38 v7.2.0 comptes/limites, #39 v7.3.0 partage ; issues N1–N16 = #251–#266. Décisions D1–D7 du
  cadrage : D1 = C plan de contrôle + agent ; D2 oauth2-proxy sidecar ; D3 GitHub OAuth ; D4 jeton supprimé derrière le
  proxy ; D5 v7.0.0 = auth seule ; D6 ingress externe activable via Helm ; D7 #249 avant multi-utilisateur. v6.9.0
  passe avant v7.0.0. Cadrage : `_work/reports/planner-v7-cadrage.md` (voir « A trancher »).
- **Pistes non ouvertes** : suites ui/desktop ≈199 s et core ≈72 s, hors budget 60 s. Modèle du teamleader passé en
  sonnet (`teamleader.md`, 9610849). `xhigh`/`max` dans `effortLevel` à vérifier en QUALIF.

## Decisions de la session (v6.8.2)

- **#242** : correctif minimal côté interface (pas de refonte du redimensionnement) ; test de reproduction headless
  `tests/integration/ui-web/clic-bandeau-242/` + procédure QUALIF `tests/procedures/ui-web/clic-bandeau-242/`.

## Decisions de la session (v6.8.1)

- **#237** : le launcher est la **seule source** du calcul d'épuisement ; marge de l'orange par fenêtre (session 1 h,
  hebdo 5 h) ; `usage-levels.json` (schéma 1, `contracts/usage-levels.md`) écrit à côté de `usage.json` seulement quand
  son `source_mtime` change ; consommateur = panneau milestone du template (template#160).
- **#238** : cause côté Go (cycle 10 min + TTL partagé 10 min qui neutralisait le cycle accéléré). **D1 = C** : 2/1 min avec
  jeton, 5/2 min sans ; **risque 429 anonyme accepté** (60 req/h par IP partagée hub/pod/postes) et documenté
  (`ng2/docs/versions.md`) ; remède `GITHUB_TOKEN`. Résultat partagé réutilisé s'il a moins que cycle − 10 s.

## Decisions de la session (v6.8.0)

- **#234** : cause = `<input list>`+`<datalist>` pré-rempli (le navigateur filtre sur la valeur), pas le Go ; option A
  retenue (liste complète : `images:` de backends.yaml ∪ défauts ∪ image courante, « Autre… » en saisie libre, « (aucune) »
  toujours proposé) ; images des pods k8s (option B) non faite, sans issue de suivi.
- **#235** : projection d'épuisement calculée une fois dans `core.ProjectExhaustion` (identique à `lib.ts` du template 0.6.0) ;
  `UsageRow.resetLevel` / `alert` ; pas de couleur si données périmées ou « passée ». Procédure HUMAIN #235 à jouer.
- Build image QUALIF : 33 min avec les miroirs Ubuntu officiels (lents depuis le réseau ; `fr.archive.ubuntu.com` rapide).
- Deux agents sur le même checkout : un `git commit` de l'un embarque les modifications non commitées de l'autre (ca18e29).

## Decisions de la session (v6.7.0)

- **#220** : `SHARE_WRITE_TIMEOUT` (défaut 10 s, bornes 2–120) ; au-delà du délai, retour à l'organisation précédente,
  écriture abandonnée poursuivie en tâche de fond, reprise automatique.
- **#221** : arrêt de l'interface borné à 25 s ; terminaux détachés puis attendus, arrêt forcé du client au bout de 5 s
  (écart contrat accepté : `detachWait` et non échéance − 1 s).
- **#222** : message de repli « leader à gauche refusé » affiché une fois par projet et par cause.
- **#231** : `/init-project` proposé d'abord ; `/start-session` proposé si projet géré ET `teams/<team>/config.json` absent.
  Limite : constat dans le `CLAUDE_CONFIG_DIR` du launcher seulement.
- **#223** : question + permission = attente ; notifications activées par défaut (`ATTENTION_NOTIFY`) ; état dans
  `<projet>/.claude-launcher/attention/<pane>.json` (`CL_ATTENTION_DIR`) ; `attentionFocus` émis par l'émetteur natif
  seul ; id inconnu = aucun évènement (contrat aligné). Maquette A retenue (badge « ❓ en attente »).

## Decisions de la session (v6.6.0)

- **#215 trois sources** : « Projet brut » (git init + remote si fourni), « Template projet » (git init + /init-project
  proposé), « Dépôt Git » ; option « géré » supprimée.
- **Déplacer / renommer, backend / image et env.yaml** refusés si le projet est ouvert ou son conteneur actif ; backend
  injoignable : env.yaml autorisé avec avertissement, backend / image / déplacement fail-closed.
- **#225** : config.conf relu à chaud ; Roots et CLAUDE_CONFIG_DIR exigent toujours une relance.
- **#226** : pastille d'effet par option + bulle ; note conservée pour backends.yaml / hub / env.yaml.
- **#227** : œil commun à tous les éléments cachés : révélation refusée en accès distant, 30 s, 10/min, journal sans valeur
  ni empreinte ; `kubectl port-forward` = accès local.
- **Tests** : garde d'isolation (`CL_TEST_ISOLATION`, écritures limitées à `os.TempDir`), jetons `ghp_FAKE_TEST_…`,
  faux podman/docker par défaut dans ui/desktop et ui/tmux.

## Decisions techniques de la session (v6.5.0)

- 5 lots : LOT-1 CI/tests fiables (#204 contrôle Chart.yaml = version du milestone dans `scripts/ci.sh` ; #203/#207/#208 ;
  `stopNative` attend `App.bg`/`waitBackground`) ; LOT-2 #201 ; LOT-3 #205 (balayage `^[0-9a-f]{16}\.json\.[0-9]+\.tmp$`
  > 24 h) et #206 (drainClose : goroutine de fond jusqu'au retour de Close) ; LOT-4/LOT-5 #209 ; LOT-5 #210.
- **#201 coffre des secrets PARTAGÉ** (exigence utilisateur, BREAKING) : un seul coffre en clair `0700/0600` sous
  `<CL_ROOT_BASE>/.config/claude-launcher-ng2/secrets/` pour hub, exe et pod (risque du stockage en clair accepté) ;
  secrets propres à un projet → `<projet>/PRIVATE/` (jamais écrits par le launcher) ; migration auto des coffres v6.4.0
  avec conservation de l'ancien (`MigrateLegacySecrets`) ; verrou `.vault.lock` ; liens pendants résolus ; `/files`
  n'expose ni le coffre ni `PRIVATE/` ; chart : plus de `CL_SECRETS_DIR`, ancien PVC monté en lecture seule
  (`secrets.legacy.enabled`, `CL_SECRETS_LEGACY_DIR`). Audit security reporté à v7.0.0 (risque accepté). En PROD et
  QUALIF l'ancien PVC était vide (0 backend migré).
- **#209** : réglage par projet `PROJECT_HIDE_IDLE` (auto/none/default) ; carte TEAM = œil à 3 états aligné à droite du
  titre (plus de puce « masquage N s »), fond plein (« bouton enfoncé ») = choix propre au projet, gris nu = default ;
  clic = cycle, clic droit = menu ; coque Linux « default (masqués 30 s) » / « auto hide ◆ » / « no hide ◆ » ;
  `projectApply` invalide `teamCache`, `applyNow`, `pushTeam`. Maquette
  `docs/mockup/v6.5.0/ui/team_card__masquage_par_projet_v2.html` (v1 obsolète).
- **#210** : émetteur de cartes à drapeau « relire encore » par App (plus de `cardsMu.TryLock` global).
- Tests : garde-fou `testguard` (HOME/APPDATA/LOCALAPPDATA temporaires, échec si `~/.config/claude-launcher*` change).
- Outil : `netavark` + `aardvark-dns` ajoutés à `.claude-launcher/env.yaml` (build d'image podman de `ci.sh`).
- Site : installation en sous-onglets Linux/Windows/Docker/k8s, « Nouveautés » = version courante seule, anciennes
  nouveautés dans Fonctionnalités avec chips Linux/Windows/Web/k8s (62/62 vérifiées dans le code), matrice
  fonctionnalités × plateformes (#matrix).
- **Leçon release** : `Chart.yaml` doit suivre la version du milestone (contrôle automatisé par #204).

### Décisions antérieures (condensées)

- **v6.4.0** « Backends distants et confort » : #199 version template (⚠ si plus récente) ; #194/#193 clés ssh et
  kubeconfig par backend (+ `rbac.extraNamespaces`) ; #189–191 onglets ; #198 bandeau MAJ client web ; #174
  `/files/<catégorie>-<projet>/…` ; #200 NetworkPolicy par défaut. Pas d'authentification (OAuth #172 → v7.0.0),
  ingress interne seul. Arrêt du launcher borné à 25 s. `ng2/README.md` découpé en guides `ng2/docs/`.
- **v6.3.0** : accès web sans jeton en cluster (`web.access: none`) ; contexte k8s ignoré en cluster ; variables `CL_*`
  sur le StatefulSet ; #188 masquage désactivé = tout réapparaît.
- **v6.2.0** : Ctrl+molette par zone ; `scripts/ci.sh` isole tmux sous `/tmp` ; sécurité `--web` (jeton d'URL puis
  cookie, contrôle Host) ; `config.conf` (jeton GitHub jamais affiché) ; copier/coller Windows.
- **Interfaces** : fenêtre native et `--web` ne tournent pas dans le même processus ; macOS = coque tmux + navigateur.
- **Outil** `chrome-headless-shell` dans `.claude-launcher/env.yaml` (PR #159).

## Points pour la prochaine session

- v6.9.0 : PROD à lancer (`/deploy prod`), puis merge `milestone/v6.9.0` → `main`. QUALIF 6.9.0.1 à jouer : scénarios 2 à 5
  du QA (humains), fenêtre Windows native non testée, `xhigh`/`max` dans `effortLevel`.
- Cadrage v7 : décisions D1–D7 consignées ci-dessus ; PLAN v7.0.0 et choix de démarrage du milestone #27 après v6.9.0.

- template#160 (panneau consomme `usage-levels.json`) traité côté template ; un `usage-levels.json` présent dans le vrai
  cache avant v6.8.1 → possible fuite de test côté template (à signaler sur #160 si l'utilisateur le souhaite).
- BuzzMaster (`other-buzzmaster-0`) : bootstrap lent car `archive.ubuntu.com` / `security.ubuntu.com` quasi inaccessibles
  depuis le réseau (`fr.archive.ubuntu.com` rapide) ; correctif miroir FR à reposer dans le pod après réouverture (perdu à
  chaque recréation) ; pas d'issue (décision utilisateur).
- Hub à relancer (`Esc` puis `l`) ; `launcher.local` et `/healthz` QUALIF non vérifiés depuis les conteneurs.
- Le test Ingress QUALIF n'est pas faisable depuis le conteneur (pas de mDNS ; poste en 192.168.1.0/24).
- Validations humaines à jouer : #223, #234, #235 (et #242 sur QUALIF).
- Procédure HUMAIN #223 jamais jouée (lot validé sans test Windows) : rejouable via
  `tests/procedures/ui-desktop/attente-teamleader-223/simulate-attention.sh` ; relancer les sessions claude pour charger les
  hooks #223 (lus au démarrage de claude).
- Pistes d'issues non ouvertes : suite ui/desktop ~190 s (hors budget 60 s, à optimiser), test Windows instable
  `TestFilesSansSessionEnModeNone174`, `_work/test-plan/` non servi, binaires QUALIF 6.6.0.x résiduels.
- Limites connues : badge conservé jusqu'à 24 h si claude est mort mais son pane resté ; un outil parallèle terminé
  pendant une permission l'efface.
- Réservé pour plus tard : PSA point 11 ; supprimer trust.json lève toutes les approbations.

## Regles critiques

- Règles permanentes du projet (tests isolés, milestone non démarré, maquette, README, mesure Windows réelle, issue rouverte, concision) : `.claude/agents/context/COMMON.md`.
- Branches : `CLAUDE.md` « Conventions Git » ; fusion vers `main` par PR (`project-config.json` → `release.merge_mode`, `.claude/agents/context/COMMON.md` « Release »).

## Corrections de comportement

- Réseaux sociaux : aucun post (`marketing.social` absent de `project-config.json`, opt-in du template).
