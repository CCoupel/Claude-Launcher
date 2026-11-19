# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Les changements de contrats (API, schémas) sont suivis à part dans `contracts/CHANGELOG.md`.

## [Unreleased]

## [6.5.0] — en cours

### Added
- CI : `scripts/ci.sh` contrôle, sur une branche `milestone/vX.Y.Z`, que `Chart.yaml` porte déjà la version du milestone (même contrôle que « Version du chart = tag » de `release.yml`) ; le désaccord est détecté à la CI de branche, plus au tag (#204)

### Changed
- **[BREAKING]** Chart : `CL_SECRETS_DIR` n'est plus posé, le PVC `<release>-secrets` n'est plus créé et l'init `secrets-perms` est supprimé (coffre commun sous `<rootBase>/.config/claude-launcher-ng2/secrets`) ; `secrets.legacy.enabled` (défaut `true`) monte l'ancien PVC en lecture seule s'il existe (`lookup`) et pose `CL_SECRETS_LEGACY_DIR` pour la migration ; PVC conservé (`keep`) ; `secrets.persistence.*` obsolète, ignoré (#201)
- **Coffre des secrets commun** au hub, à l'exe Windows et au pod : en clair, `0700`/`0600`, sous `<CL_ROOT_BASE>/.config/claude-launcher-ng2/secrets/` ; le refus « sous la racine partagée » est supprimé ; refus d'un coffre sous un dossier de projet ou dont le chemin résolu (liens symboliques, liens pendants compris) sort de l'emplacement attendu ; écritures sous verrou fichier, journal avec l'instance (`hub`, `pod`, `windows`) ; sans ACL Windows sur un chemin UNC (#201)
- Migration au démarrage des coffres v6.4.0 (emplacement local et `CL_SECRETS_LEGACY_DIR`) vers le coffre commun ; ancien coffre conservé, écart de contenu journalisé et gardé côté coffre commun (#201)
- Secrets de projet : `<projet>/PRIVATE/`, jamais écrits par le launcher ; `kubeconfig:` par chemin admis dans le coffre ou dans un `PRIVATE/` ; `/files` ne sert pas `.config/claude-launcher*` (#201)
- Guide `ng2/docs/secrets.md` (emplacement, portée projet, migration, risque du stockage en clair) (#201)

### Fixed
- Arrêt de l'exe Windows : après l'échéance de fermeture d'un surveillant de fichiers, une goroutine de fond continue de vider ses évènements jusqu'au retour de `Close` (plus de lecteur bloqué ni de handles gardés) ; l'arrêt reste borné par `fsCloseLimit` (#206)
- Cache d'état : les `<hash>.json.<n>.tmp` de plus de 24 h (écritures interrompues) sont supprimés au démarrage du suivi d'état ; les plus récents sont gardés (#205)
- Tests : `TestSSEFrozenConnectionHasWriteDeadlineAndCountsInCap` attend l'inscription des deux gestionnaires de flux (condition bornée) au lieu d'une pause fixe de 200 ms ; échéance d'écriture portée à 5 s pour que les gestionnaires bloqués ne sortent pas avant la vérification du 503 sous `-race` (#203)
- Tests : simulation du flux SSE — le délai d'abandon d'init doit suivre 8000, 16000 puis 30000 répété (suite non décroissante, plafond 30 s) ; une suite non croissante ou un plafond dépassé échoue toujours (#207)
- Tests : `stopNative` attend la fin des tâches de fond de l'App (suivies par `App.bg`, `waitBackground`) au lieu d'une pause fixe de 300 ms ; suite `web184` nettement plus rapide et sans attente passive (#208)

## [6.4.0] — 2026-11-19

### Added
- Carte TEAM : le titre affiche la version du template du projet (« tpl vX ») et un `⚠` orange si une version publiée plus récente existe ; l'infobulle indique la version disponible et `/init-project` au lancement (#199)
- Vérification GitHub de la version du template : appels authentifiés quand un jeton est disponible (`GH_TOKEN`, `GITHUB_TOKEN`, ou `config.conf`), jamais journalisé ; requêtes conditionnelles par ETag (`304` réutilise la réponse mémorisée) ; résultat partagé par poste dans `update-check.json` (écriture atomique, réutilisé pendant 10 min) (#199)
- Alerte GitHub visible en continu, orange et non masquable : « vérification GitHub impossible (quota, reprise HH:MM) » sur quota dépassé (`403`/`429`), « GitHub injoignable » sur un autre échec, y compris en coque tmux (#199)
- Interface web : bandeau « Serveur mis à jour (vX) — Recharger » quand la version du serveur change ; après un simple redémarrage du serveur, les terminaux se rattachent automatiquement à leurs sessions tmux (appel `reattach {path}` en repli à 5 s, abandon à 20 s avec « N terminaux non rattachés — recharger ») (#198)
- Interface web : bandeau « connexion au serveur perdue » pendant une coupure ; l'état est redemandé à chaque reconnexion du flux, et l'évènement `hello` est traité à l'ouverture (#198)
- Interface web : « session introuvable — rouvrir le projet » quand la session d'un terminal n'existe plus ; le bandeau se ferme avec OK et disparaît à la réouverture du projet (#198)
- Onglets de l'interface native : ordre réglable par glisser-déposer, gardé par poste dans `cl.tabOrder` ; `Ctrl+Tab` suit l'ordre affiché (#191)
- Interface web en cluster : les fichiers des projets sont servis sous `/files` (CSP `sandbox`, origine opaque) ; les sessions utilisent `CL_WEB_URL/files` ; fichiers sensibles (`PRIVATE`, `.git`, `.claude`, `.env*`, clés, identifiants d'outils comme `.git-credentials`, `.npmrc`, `.docker`, `*.p12`, `*.pfx`, `*.jks`), dossiers d'état du launcher et `_work/tmp` jamais servis ni listés ; dans `_work`, seul `_work/mockup` est servi (#174)
- Chart Helm : NetworkPolicy activée par défaut, entrée limitée aux pods du contrôleur Traefik (`networkPolicy.ingressNamespaces`, `ingressPodSelector`, `ingressCIDRs`), sur le port nommé du conteneur ; l'ingress externe est préparé mais refusé au rendu tant qu'il n'y a pas d'authentification (v7.0.0) (#200)
- Backends ssh : clés ssh par backend saisies dans les Réglages (ou générées par le launcher, ed25519), rangées dans un coffre hors partage (`CL_SECRETS_DIR`, `0700`/`0600`) ; clé privée jamais relue ni affichée, phrase de passe refusée, empreinte de l'hôte relevée puis confirmée explicitement, journal des écritures sans contenu (`<CacheDir>/secrets-journal.jsonl`) (#194)
- Image du cluster : clients `ssh`, `docker` et `podman` distant (`openssh-client`, client docker statique, `podman-remote`) ; aucun démon ni `sshd` (#194)
- Backends k8s : kubeconfig par backend, déposé dans le coffre depuis les Réglages ou donné par un chemin ; contextes listés, contenu jamais affiché ; `--kubeconfig` prioritaire sur le ServiceAccount du pod (#193)
- Chart Helm : `rbac.extraNamespaces` (Role et RoleBinding dans d'autres namespaces du même cluster), `backends.kubeconfigs`, PVC du coffre `<release>-secrets` (`keep`), `backends.docker.secretName` pour un Secret TLS docker/podman (#193, #194)

### Changed
- **Changement hub et exe** : le serveur de maquettes local (hub `8765`, exe `8766`) ne sert plus `PRIVATE`, `.git`, `.claude`, les fichiers d'identifiants, ni `_work` hors `_work/mockup` ; `/files` refuse les requêtes cross-site, sauf les sous-ressources de maquette et la navigation de premier niveau (ouverture d'une maquette par l'extension ou un lien), acceptée de toute provenance (#174)
- La version du template est comparée dans l'ordre (`VersionNewer`) ; l'état « à mettre à jour » ne vaut que si la version adoptée est strictement plus récente que celle du projet (#199)
- La question au lancement ne propose plus de retour en arrière : une cible égale ou plus ancienne que la version du projet n'est jamais proposée (#199)
- La carte TEAM affiche la version du projet (et non plus la version adoptée) ; la version adoptée ne change qu'au clic sur l'annonce (#199)
- Kubeconfig contrôlé à chaque appel de kubectl : `exec` et `auth-provider` refusés (kubectl non lancé), fichiers lus sur le poste (`client-certificate`, `client-key`, `tokenFile`, `certificate-authority`) refusés, quelle que soit la casse des clés ; avertissements `insecure-skip-tls-verify` et `proxy-url` affichés en orange (#193)
- En cluster, l'annonce de release du launcher n'est plus cliquable : elle indique « mise à jour par helm upgrade » (#198)
- Durcissement de l'ouverture et de l'arrêt : l'installation du pod d'un projet (bootstrap du conteneur) est détachée du launcher ; à l'arrêt, le launcher attend les ouvertures en cours 20 s au plus ; un pod resté à moitié installé est réparé à l'ouverture suivante (#198)
- Chart : `terminationGracePeriodSeconds` vaut 30 s au minimum (schéma), pour laisser au launcher ses 20 s d'attente (#198)
- Onglets de l'interface native : les onglets non sélectionnés ont un liseré à la couleur de leur projet ; titre et largeur restent stables à la sélection, et « agent au travail » s'affiche dans un emplacement ⚙ fixe et dans l'infobulle (#189, #190)
- Un kubeconfig illisible met le backend en erreur, sans repli sur le ServiceAccount du pod (#193)

### Fixed
- Windows : interblocage du surveillant de fichiers corrigé (probablement présent en v6.3.0, non reproduit sur un exe publié) : la mise à jour d'état et de l'arbre, et l'arrêt, se figeaient lors de créations de dossiers en rafale
- Windows : nom de projet (`CtrName`) sans correspondance corrigé ; `/files` servi sous Windows
- Arrêt borné à 25 s au pire : 20 s d'attente des ouvertures en cours, puis détachement des terminaux, sous le délai de grâce de 30 s du pod (#198)
- Courses de données sous `-race` corrigées : variables globales de réglage (délais, crochets, URL de l'API, présence, verrous), `NgVersion` et `InitSynced` lues et écrites sous verrou (#174)
- Comparaisons de versions du template corrigées : seuls les 3 premiers champs `X.Y.Z` sont comparés (`5.11.1.3` → `5.11.1`) (#199)
- Les tests n'écrivent plus dans le vrai `~` : `CL_SCRIPT` pointe vers un dossier temporaire, et une garde fait échouer les tests si `~/.template-*` est touché (#199)

### Security
- Risques acceptés (v6.4.0) : une page tierce peut exécuter un fichier `.js` d'un projet servi sous `/files` et lire ses variables globales ; les scripts de maquette ne sont pas restreints en `connect-src` (CDN et API publiques) (#174)
- Risque accepté jusqu'à v7.0.0 : l'écriture des secrets (clés ssh, `known_hosts`, kubeconfig) est permise depuis le LAN par l'ingress interne sans authentification (`web.access: none`). Une machine admise peut redéclarer un backend ssh, y déposer sa clé et son empreinte, puis faire exécuter des commandes à un hôte qu'elle contrôle. Atténuations : liste d'adresses de l'ingress, NetworkPolicy, journal des écritures, relevé d'empreinte limité aux backends déclarés. Levée par la règle R1 de v7.0.0 (session authentifiée exigée) (#194, #193)

## [6.3.0] — 2026-11-17

### Added
- Commutateur « Cacher les teammates inactifs » (Réglages › Launcher, groupe « teams ») : clé `TEAM_HIDE_IDLE_ENABLED`, activée par défaut ; désactivée, tout teammate reste visible et le délai `TEAM_HIDE_IDLE` (en secondes) est conservé (#188)
- Guide « Installer le launcher dans Kubernetes par Helm (accès interne) » dans `ng2/docs/kubernetes.md` : prérequis, valeurs, installation par empreinte, accès sans jeton, vérifications de la liste d'adresses, mise à jour et retour arrière, limites de v6.3.0 (#185)
- Mode d'accès de l'interface web : `desktop_web.access` / `CL_WEB_ACCESS` (`token` par défaut, `none` sans jeton, réservé à la boucle locale ou à une adresse publique en https) ; chart `web.access: none` par défaut (#192)
- Release : l'image du launcher est publiée multi-architecture (`linux/amd64`, `linux/arm64`) sur ghcr, avec son empreinte jointe à la release, et le chart Helm est publié en OCI sur ghcr avec son archive. La release en brouillon n'est publiée qu'une fois l'image et le chart en place. La version candidate de QUALIF est poussée dans le registre interne (#177)
- `scripts/ci.sh` contrôle le chart Helm et construit l'image du launcher sous Linux ; `CL_CI_IMAGE=0` saute l'étape image (#177)
- Chart Helm `deploy/helm/claude-launcher` (#170, #173) : StatefulSet à une réplique (arrêt de l'ancien pod avant le démarrage du nouveau), Service `ClusterIP` seul, Role limité au namespace sans ClusterRole, Ingress interne Traefik ou nginx avec liste d'adresses admises obligatoire, non vide, et HTTPS obligatoire, racine du pod en lecture seule, valeurs réservées. Déploiement QUALIF dans le namespace `claude-qualif` sur la racine dédiée `/share/MFS/ROOT/CLAUDE-PROJECTS-QUALIF` ; PROD dans `claude-ng2`
- Image du launcher pour le cluster : `ghcr.io/ccoupel/claude-launcher` (uid 1000, `bash`, `git`, `gh`, `kubectl`, sans `claude` ni `tmux`), construite par `deploy/image/build.sh` ; `CL_ROOT_BASE` obligatoire au démarrage (#167)
- Mode serveur en cluster : ServiceAccount et namespace du pod, backend k8s implicite avec `CL_K8S_VOLUMES`, diagnostic des droits un par un, jeton conservé dans le cache et jamais imprimé, arrêt propre sur SIGTERM ; `CL_UPDATE_MODE=notify` (#168)
- Connexion claude.ai depuis un pod sans `claude` : elle se fait dans un projet ouvert (`/login` dans son leader) ; une sortie réseau bloquée est signalée dans l'erreur (#175)
- Identité et cache par launcher : `CL_HUB_NAME` (nom du launcher, clé `hubs.<nom>`, identité des sessions), `CL_CACHE_DIR` (dossier de cache utilisé tel quel), `CL_IN_CLUSTER` et `CL_ALLOW_HOST_BACKEND` (#169)
- Plusieurs launchers sur une même racine : garde de nom (`CL_HUB_NAME`), écritures partagées sous verrou puis écriture atomique (`projects.json`, `settings.json`, `env.yaml`), liste des clés de `settings.json` par launcher (#176)
- Racine de base : option `--root-base <dossier>` et variable `CL_ROOT_BASE` ; la configuration, la config de claude et les projets se placent sous cette racine (`CLAUDE_CONFIG_DIR` par défaut `<racine>/.claude`). Racine invalide ou option vide refusées sans rien créer ; section « Racine de base » dans `backends` (#165, #166)
- Adresse publique de l'interface web derrière un proxy inverse HTTPS : réglage `desktop_web.url`, option `--url`, variable `CL_WEB_URL` ; hôte public strict (punycode, port par défaut retiré) ; sondes `/healthz` et `/readyz` ; exemples nginx, Caddy et Traefik dans `ng2/README.md` (#171)
- Réglages › Ce poste : les rubriques « Serveur de maquettes » et « Interface web » sont séparées. Le commutateur `desktop_web.enabled` allume ou éteint l'interface web sans effacer son adresse, sans relance du launcher (#183)

### Changed
- En cluster, le `context` d'un backend k8s est ignoré : le pod agit sur le cluster de son ServiceAccount (#168)
- Interface web derrière un proxy : le flux se reconnecte avec un délai croissant (1,5 s à 15 s) ; une session expirée (`401`/`403` sur `/api/events` ou `/api/call`) est signalée comme telle, distincte d'un refus d'hôte ou d'origine (`X-Launcher-Refusal`). Après amorçage, la page est servie en `303` sans `t` dans l'URL ; le cookie prend le préfixe `__Host-` en https (#171)
- `desktop_web` et `browser_server` s'affichent et se modifient en valeur effective pour ce poste et ce système, avec la provenance de chaque réglage (`defaults`, `hubs.<machine>`, surcharge par système). « Retour au défaut » retire le champ de toutes les couches (#183)
- `scripts/ci.sh` tient en moins de 60 s hors étape image : 56 s en moyenne sur trois passes, contre 119 s. Les tests de `ui/desktop` attendent désormais des évènements au lieu de dormir ; la couverture et le nombre de tests sont inchangés (#179)

### Fixed
- Panique corrigée : une `NewRequest` en erreur ne fait plus paniquer le launcher ; `RawBase` est modifié sous verrou et le port d'écoute est repris correctement (#187)
- Tests : `TestRelayoutLeftZeroToTenTeammates` (ui/tmux) stabilisé en attendant le répertoire du leader ; `tmux -V` est affiché par `scripts/ci.sh`. Cause du flaky non établie, à confirmer par la CI ubuntu (#186)

## [6.2.0] — 2026-11-16

### Added
- Ctrl+molette sur l'arbre des projets et sur les cartes ◈ État : tailles de texte indépendantes par zone (8 à 28), mémorisées par poste et réglables dans Réglages › Ce poste › Affichage (#150)
- Menu du projet « ⟳ Rafraîchir » (`F5`) : relit tout le projet sans les caches (conteneur ou pod, session, git, GitHub, template, consommation), avec un bandeau d'étapes en tête des cartes ; une erreur reste affichée jusqu'à `×` (#158)
- Menu du projet « Forcer la fermeture… » : proposée quand un projet ouvert ou en erreur n'a plus de conteneur, de pod ni de session (pod k8s disparu) ; confirmation avec la raison et le détail du nettoyage local ; `projects.json` n'est jamais modifié (#158)
- Coque Linux : `F5` relit le projet sélectionné, `Suppr` propose la fermeture forcée (#158)
- Carte TEAM : titre « TEAM · N · TPL vX · CLAUDE Y » (versions une seule fois), `⚠` orange sur un teammate dont la version diffère (template ou claude ; détail en infobulle sous Windows) (#151)
- En-tête des panes de teammates : bandeau vert si actif, gris si inactif, neutre pour le leader ; suit l'état sans relance ; sous Linux, une ligne de titre tmux en tête de chaque pane (`pane-border-status top`) (#152)
- Onglet Teamleaders (interface native) isolé, épinglé à gauche, sans `×` ; réduit à son icône quand les onglets de projets débordent (#162)
- Interface web dans la fenêtre native : réglage `desktop_web` (désactivé par défaut, Réglages › Ce poste › Navigateur), écoute hors boucle locale refusée sans `allow_lan: true`, bannière, régénération du jeton, sessions par cookie (`cl_session_<port>`), lecture seule pour un client web (#163)
- Organisation « leader à gauche » des panes de projet (leader et `cdp` empilés à gauche, teammates à droite) : réglage global `LAYOUT_MODE` (`top` | `left`, défaut `top`), largeur `LAYOUT_LEFT_LEADER_PCT` (20–80), exception par projet `PROJECT_LAYOUTS` (champ « Mise en page » des Réglages du projet) ; bouton de bascule à droite de la barre d'onglets de l'interface native (enregistre l'exception du projet affiché) ; remise en page sans relance, repli sur `top` si tmux refuse (#153)

### Changed
- « Fermer » du menu projet est disponible dès que la ligne est ouverte ou que sa pastille n'est pas `○` (#158)
- `scripts/ci.sh` lance `go test -race ./...` sous Linux ; il exige gcc (cgo) et signale explicitement son absence (#164)
- Carte TEAM : les colonnes de version disparaissent des lignes, la carte est plus étroite (#151)

### Fixed
- Bascule de mise en page (#153) : la latence dépassait 15 s sur un partage réseau Windows ; elle est désormais immédiate dans l'interface native (`50101a7`, `d1bd4d2`). Sous Windows, la mise en page est appliquée avant la persistance de `config.conf`, les commandes tmux sont regroupées (deux allers-retours), et les clics rapides sont sérialisés par projet : le dernier clic gagne (`dd6642f`, `ac0e3a0`)
- Course de données sur `InitSynced` et `init-project.md` pendant les vérifications de mises à jour concurrentes : protégés par un verrou (`fa200eb`)
- Les lectures git du launcher ne prennent plus de verrou (`GIT_OPTIONAL_LOCKS=0`) et n'exécutent plus `git describe --dirty` : plus de `index.lock` laissé par le launcher (#154)
- Tests : les sockets tmux de test sont isolés dans un dossier dédié ; `scripts/ci.sh` échoue sur un socket de test résiduel ou sur un `index.lock` orphelin (#154)
- `scripts/ci.sh` échoue aussi sur un socket de test laissé dans `/tmp/tmux-<uid>` (préfixes des tests seulement) (#154)
- Backend injoignable : la fermeture forcée est proposée avec un avertissement, le nettoyage reste local et aucun arrêt du conteneur n'est demandé (#158)
- Rafraîchissement des conteneurs : plus de course entre la relecture et la lecture de l'état des conteneurs (#158)
- Rafraîchissement, étape GitHub : le milestone est relu sans attendre la relecture de masse ; `gh` absent, non connecté ou dépôt sans remote GitHub = étape verte avec la raison ; une panne réelle = étape rouge avec la cause renvoyée par `gh` (#158)
- Test `TestProjectApplyForeignOwnerTimeout` : race de test corrigée (la goroutine est attendue avant de restaurer `foreignOwner`) (#164)
- Windows, dépôt sur un partage : git refusait « dubious ownership » ; le launcher autorise désormais ce seul dossier de projet, pour ses appels `git` et `gh` (`safe.directory` par appel, configuration globale inchangée). Si git refuse encore, l'étape affiche la commande à exécuter pour vos propres dépôts (#158)

### Validation
Cinq lots (#150, #154 ; #158 ; #151, #152, #162 ; #163 ; #153) ont été validés en QUALIF, chacun après revue de code, QA et contrôle de sécurité quand le lot le demandait (audit de `--web`, 78/100 puis corrections M1 et M2). La CI passe avec `go test -race` sous Linux. La réserve restante est reportée à v6.3.0 : la durée de `scripts/ci.sh` (#179, au-dessus du budget indicatif de ~60 s).
