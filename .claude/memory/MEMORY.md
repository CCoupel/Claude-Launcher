# Memoire projet — Claude-Launcher

> Source de verite unique au demarrage d'une session (/start-session). Mise a jour par /end-session.

## Etat courant

- **Version** : **6.8.1 livrée en QUALIF (6.8.1.1), PROD en cours** (branche `milestone/v6.8.1`, PR vers main + tag
  `v6.8.1`). Précédente : 6.8.0 en PROD (PR #236 mergée). Milestone #33 « v6.8.1 » (#237, #238), workflow `completed`.
  Site gh-pages à jour jusqu'à v6.7.0 (PR #233).
- **Branche** : `milestone/v6.8.1`.
- **Environnements** :
  - PROD : namespace `claude-ng2` (v6.8.0, v6.8.1 en cours de déploiement).
  - QUALIF : v6.8.1.1.
  - Hub : `/share/MFS/ROOT/CLAUDE_PROJETS/claude-launcher-ng2` + `.exe` (à relancer après la PROD).
  - CA interne cert-manager `claude-launcher-ca` (nameConstraints `launcher.local` / `launcher-qualif.local`).
  - Kubeconfig Rancher ADMIN dans `PRIVATE/{QUALIF,PROD}/kubeconfig` (risque accepté).
- **Travail en cours** : aucun (reste : déploiement PROD v6.8.1).

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

- Procédure HUMAIN #223 jamais jouée (lot validé sans test Windows) : rejouable via
  `tests/procedures/ui-desktop/attente-teamleader-223/simulate-attention.sh`.
- Relancer le hub et les sessions claude pour charger les hooks #223 (lus au démarrage de claude).
- Pistes d'issues non ouvertes : suite ui/desktop lente (166–230 s), test Windows instable
  `TestFilesSansSessionEnModeNone174`, `_work/test-plan/` non servi, QUALIF 6.7.0.4 et binaires QUALIF 6.6.0.x résiduels.
- Limites connues : badge conservé jusqu'à 24 h si claude est mort mais son pane resté ; un outil parallèle terminé
  pendant une permission l'efface.
- Relancer les sessions Claude ouvertes avant le 2026-11-21 ~10:00 (GH_TOKEN factice hérité d'un test).
- Suite ui/desktop plus lente avec les faux moteurs : à optimiser.
- Réservé pour plus tard : PSA point 11 ; supprimer trust.json lève toutes les approbations.

## Regles critiques

- Règles permanentes du projet (tests isolés, milestone non démarré, maquette, README, mesure Windows réelle, issue rouverte, concision) : `.claude/agents/context/COMMON.md`.
- Branches : `CLAUDE.md` « Conventions Git » ; fusion vers `main` par PR (`project-config.json` → `release.merge_mode`, `.claude/agents/context/COMMON.md` « Release »).

## Corrections de comportement

- Réseaux sociaux : aucun post (`marketing.social` absent de `project-config.json`, opt-in du template).
