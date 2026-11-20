# Changelog des contrats

> Changements de contrats (API, schemas) : `BREAKING` | `CHANGED` | `ADDED`. Lu par le CDP au GATE 2.

## [20261120] — v6.6.0 (plan) : gestion des projets (#214 à #219)

- **[NEW]** `core.ResolveProjectPath` : base ∈ racines configurées, catégorie relative sans `..`, contrôle après liens symboliques — appliquée par `create`, `categoryCreate`, `projectMove` (et la coque tmux) — `contracts/desktop-dispatch.md` « v6.6.0 »
- **[NEW]** `projectBases`, `projectCategories`, `categoryCreate`, `createCancel`, instantané `newJob`, évènement `initProposal` / `initProposalAnswer`
- **[CHANGED]** `create` : `base` = racine configurée seulement (+ `category`, `newCategory`), `source` `empty|git|template`, `url`, asynchrone pour `git`, nettoyage complet sur échec — un appel v6.5.0 avec `base` = dossier de catégorie est refusé (interface et backend livrés ensemble, aucun client externe)
- **[CHANGED]** `newInfo` : `bases`, `default`, `auth` (présence du jeton / des clés, jamais leur valeur), `template` ; `roots` gardé (retrait en v7)
- **[NEW]** `projectMoveInfo`, `projectMove` (journal `moves.json`, retour arrière, état partiel repris)
- **[NEW]** `launcherInfo` (sans `stale` ni `backup`), `relaunch` ; coque : `ctrl+r` contextuel (relance si l'annonce est affichée, rescan sinon) ; `FindStaleProcs` filtré sur `argv[0]` = nom du binaire, `argv[1]` ∈ {mirror, layout-watch}, `CL_INNER_SOCKET` du launcher
- **[CHANGED]** sauvegarde `<exe>.bak-v<version>` avant mise à jour (une seule gardée) ; écriture atomique de `config.conf` (retentée sous Windows), balayage des `.config.conf.tmp-*`
- **[RETIRÉ]** (révision GATE 2, jamais publié) `rollback`, `update-skip`, champ `stale` de l'instantané : retour arrière = procédure manuelle documentée

## [20261119] — v6.5.0 (plan rev2) : coffre partagé (#201), masquage par projet (#209)

- **[BREAKING]** coffre des secrets **commun** au hub, à l'exe Windows et au pod, en clair (`0700`/`0600`) sous `<CL_ROOT_BASE>/.config/claude-launcher-ng2/secrets/` ; refus « sous la racine partagée » **supprimé** ; secrets de projet = `<projet>/PRIVATE/` (jamais écrits par le launcher) ; migration automatique des coffres v6.4.0, ancien conservé — `contracts/backends-remote.md` « Coffre des secrets »
- **[BREAKING]** chart : `CL_SECRETS_DIR` n'est plus posé ; PVC `<release>-secrets` monté seulement pour la migration (`secrets.legacy.enabled`, `CL_SECRETS_LEGACY_DIR`) ; init `secrets-perms` supprimé — `contracts/helm-values.md` « v6.5.0 »
- **[CHANGED]** `kubeconfig.path` admis dans le coffre global ou un `PRIVATE/` de projet
- **[NEW]** `PROJECT_HIDE_IDLE` (`config.conf`), `projectApply.hideIdle` / `projectSettings.hideIdle` (`auto|default|none`), `teamHead.hideIdle.mode` et `.global` — **[CHANGED]** `teamHead.hideIdle.enabled` = valeur effective du projet (identique pour `default`) — `contracts/desktop-dispatch.md` « v6.5.0 »

## [20261118] — v6.4.0, LOT-5 : budget d'arrêt (#198)

- **[CHANGED]** arrêt du launcher borné à **25 s au pire** (ouvertures 20 s + détachements des terminaux 5 s) ; schéma du chart : `terminationGracePeriodSeconds` `minimum: 30` — l'écart signalé le 2026-11-18 est résolu — `contracts/web-server.md` « Cohérence avec le délai de grâce du pod »

## [20261118] — v6.4.0, LOT-4 : `/files/<projet>/<chemin relatif>` (#174, décision utilisateur au GATE LOT-4)

- **[BREAKING]** (interne au milestone, aucune version publiée) : `/files/<chemin interne complet>` **supprimé** ; forme unique `/files/<projet>/<chemin relatif>`, `<projet>` = nom du projet dans le launcher, projets connus seulement (inconnu → `404`) ; `/files/` sans liste (`404`) ; premiers segments en `__` réservés (appariement `/files/__claude-launcher__/pair`) — `contracts/web-server.md` « Fichiers des projets par l'interface »
- **[CHANGED]** `CL_BROWSER_PATH_MAP` / `CL_BROWSER_HINT` : une correspondance par session, racine du projet → `<CL_WEB_URL>/files/<projet>` ; serveur local 8765 du hub et de l'exe inchangé

## [20261118] — v6.4.0, LOT-4 : CSP de `/files`, sources de la NetworkPolicy (#174, #200)

- **[CHANGED]** `/files` : `Content-Security-Policy: sandbox allow-scripts allow-forms allow-modals allow-popups; frame-ancestors 'none'` au lieu de `sandbox` seul (scripts des maquettes exécutés dans une origine opaque, jamais `allow-same-origin`) — `contracts/web-server.md` (commit ffcfc72, dev-backend ; avis security en cours)
- **[NEW]** chart : `networkPolicy.ingressCIDRs` (sources `ipBlock` en plus des namespaces ; rendu refusé sans namespace ni CIDR) — `contracts/helm-values.md` « v6.4.0 » (commit 441f093, infra)

## [20261118] — v6.4.0 : appel `reattach` (#198)

- **[NEW]** `reattach {path}` → `{ok, tab}` : rattache l'onglet à une session tmux survivante après un redémarrage du serveur, sans rien créer ; appelé par le client en repli au bout d'environ 5 s (abandon à 20 s) ; `ok: false` sans erreur quand la session manque, appartient à un autre launcher, ou que l'onglet est en lecture seule ou en erreur — `contracts/web-server.md` « Appel reattach » (commit 3c0b868)
- **[CHANGED]** arrêt du launcher et ouverture d'un projet (aucun changement d'API, commit cb07c97) : bootstrap du conteneur détaché du launcher (`setsid nohup`, `/var/lib/cl/boot.{log,pid,rc}`, un seul exec, jamais relancé s'il tourne), arrêt qui attend les ouvertures en cours 20 s au plus, conteneur à moitié installé réparé au démarrage (sans session) ou à l'ouverture — `contracts/web-server.md` « Arrêt du launcher et ouverture d'un projet » ; écart signalé : `terminationGracePeriodSeconds` du schéma sans minimum face à l'attente fixe de 20 s

## [20261117] — v6.4.0 (plan, rev2 GATE 2) : fichiers et version du serveur, backends distants, coffre, NetworkPolicy, version du template

- **Retiré (rev2)** : toute authentification de v6.4.0 (#172 déplacée en v7.0.0) : oauth2-proxy, `forwardAuth`, `CL_WEB_AUTH_*`, identité transmise, adresses publiques multiples. `contracts/web-auth.md` renommé `contracts/web-server.md` ; la piste v7.0.0 y est notée.
- **[NEW]** route `/files/<chemin>`, `CL_BROWSER_PATH_MAP` des sessions vers l'adresse de l'ingress interne, appariement par `/files/__claude-launcher__/pair` (#174) — `contracts/web-server.md`
- **[NEW]** `server = {version, boot}` dans `init` et l'instantané ; bandeaux « Serveur mis à jour » / reconnexion (#198)
- **[NEW]** chart : `networkPolicy` **activée par défaut**, coffre `secrets.persistence`, `rbac.extraNamespaces`, `backends.{docker,kubeconfigs}` ; `externalIngress.*` **préparé mais refusé au rendu** — `contracts/helm-values.md` « v6.4.0 »
- **[NEW]** coffre des secrets `CL_SECRETS_DIR` ; clés ssh et `known_hosts` par backend saisies dans l'interface ; appels `backendSecretGet/Set/Delete`, `backendHostScan`, `backendKnownHostsSet` ; **règle d'écriture provisoire** (ingress interne et fenêtre native) jusqu'à v7.0.0 — `contracts/backends-remote.md` (#194)
- **[NEW]** `kubeconfig` d'un backend k8s : coffre (`kubeconfig: true`) ou chemin ; priorité sur le ServiceAccount (#193). **[CHANGED]** par rapport au texte de #193 : le champ est modifiable depuis l'interface (règle provisoire), hors de l'empreinte d'exposition en v6.4.0
- **[CHANGED]** image : clients ssh, docker (binaire statique), podman (`podman-remote`) (#194)
- **[CHANGED]** version du template : comparaisons ordonnées, titre de la carte TEAM = version du projet, `teamHead.tplNewer`, vérification GitHub authentifiée, conditionnelle, partagée par poste, quota visible — `contracts/template-version.md` (#199)
- **Aucun BREAKING.**

## [20261117] — v6.3.0, LOT-5 : commutateur du masquage des teammates inactifs (#188)

- **[NEW]** clé `TEAM_HIDE_IDLE_ENABLED` de `config.conf` (`1` par défaut) : désactive le masquage sans perdre le délai `TEAM_HIDE_IDLE` — `contracts/desktop-dispatch.md`
- **[NEW]** `cards.teamHead.hideIdle` (`enabled`, `delay`) : état du masquage sur la carte TEAM
- **[CHANGED]** règle de visibilité quand le masquage est désactivé : aucun teammate caché, entrées `h` de `@cl_panevis` retirées, panes de la réserve ramenés — `contracts/tmux-options.md` « Masquage désactivé »
- **[CHANGED]** `teamToggle` : la bascule qui cacherait un teammate est refusée quand le masquage est désactivé ; `cards.team[].hidden` faux et `idleLeft` à 0 dans ce cas
- Aucun `BREAKING` : sans la nouvelle clé, comportement de la v6.2.0 (`TEAM_HIDE_IDLE=0` compris).

## [20261117] — v6.3.0, LOT-4.4 : interface web accessible sans jeton (décision utilisateur)

- **[NEW]** `desktop_web.access` (`token` | `none`, défaut `token`), variable `CL_WEB_ACCESS`, champ `webUI.access`, clé `desktop_web.access` de `configModel` / `configApply` — `contracts/desktop-web.md` « Mode d'accès »
- **[NEW]** mode `none` : ni jeton, ni cookie, ni session ; `Host`, origine, méthode et corps JSON toujours contrôlés ; `GET /?t=…` redirigé vers `/` ; accepté seulement en boucle locale ou avec une adresse publique en `https`
- **[NEW]** chart : valeur `web.access`, variable `CL_WEB_ACCESS` du pod — `contracts/helm-values.md`
- **[CHANGED]** chart : accès **sans jeton par défaut** (`web.access: none`) ; `NOTES.txt` sans lien d'amorçage ; `none` sans Ingress refusé au rendu
- **[CHANGED]** `/api/call`, `/api/events` : `Sec-Fetch-Site: cross-site` refusé (`403`, `origin`), dans les deux modes
- **[CHANGED]** `webUIRegen` et `webUIOpen` en mode `none` : refus de la régénération, ouverture sans `?t=`
- Aucun `BREAKING` pour le hub Linux et l'exe Windows : sans réglage, le mode reste `token`. Le serveur de maquettes n'est pas concerné.
- Risques acceptés jusqu'à v6.4.0 (#172) : tout poste de la liste d'adresses de l'Ingress pilote les projets ; tout pod du cluster atteint le Service sans passer par l'Ingress (pas de NetworkPolicy).

## [20261116] — v6.3.0 : launcher dans Kubernetes (#165 à #171, #173, #175 à #177), réglages des serveurs web (#183)

- **[NEW]** `CL_ROOT_BASE` et `--root-base` : racine de base ; ordre `CL_CONFIG_DIR` > racine de base > dossier du binaire > dossier personnel (#165) — `contracts/launcher-env.md`
- **[NEW]** `CL_HUB_NAME`, `CL_CACHE_DIR` : nom et dossier d'état du launcher (#169)
- **[NEW]** `CL_IN_CLUSTER`, `CL_ALLOW_HOST_BACKEND`, `CL_K8S_VOLUMES` : launcher dans un cluster — backend k8s par le ServiceAccount du pod, namespace du pod, backend k8s implicite, backend `host` refusé (#168, #169)
- **[CHANGED]** diagnostic et « Tester » d'un backend k8s **en cluster** : contrôle de chaque droit, contrôle du montage du PVC, plus d'étape `get namespace` (#168) — hors cluster, inchangé
- **[NEW]** `CL_UPDATE_MODE` (`auto` | `notify`) : annonce de version sans installation (#167)
- **[CHANGED]** `desktop --web` **en cluster** : jeton conservé dans le cache au lieu d'un jeton neuf à chaque démarrage (#167) — hors cluster, inchangé
- **[CHANGED]** `desktop --web` : arrêt propre sur `SIGTERM` / `SIGINT` (#167)
- **[NEW]** image `ghcr.io/ccoupel/claude-launcher` (tags `vX.Y.Z`, `vX.Y.Z.a`) (#167)
- **[NEW]** `desktop_web.enabled` (commutateur) et `desktop_web.url` (adresse publique) ; option `--url`, variable `CL_WEB_URL` (#183, #171) — `contracts/desktop-web.md` ; sans `enabled`, comportement de la v6.2.0
- **[NEW]** routes `GET /healthz` et `GET /readyz`, sans session ni contrôle de `Host`, sans information (#167)
- **[CHANGED]** `GET /api/events` : en-têtes `X-Accel-Buffering: no` et `no-transform` ; reconnexion de l'interface à délai croissant ; session perdue signalée (#171)
- **[CHANGED]** cookie de session : attribut `Secure` quand l'adresse publique est en `https` (#171) — sans adresse publique, inchangé
- **[NEW]** instantané `webUI` : champs `enabled`, `url`, `server` ; `configModel` / `configApply` `kind: "hub"` : clés `desktop_web.enabled`, `desktop_web.url` (#183, #171)
- **[NEW]** chart Helm `deploy/helm/claude-launcher` et ses valeurs (#170, #173) — `contracts/helm-values.md`
- **[NEW]** chart : `volume.create` — le chart crée le PVC de la racine (et, en option, le PV statique : droits cluster), conservés à la désinstallation (`helm.sh/resource-policy: keep`) ; défaut inchangé (PVC existant) (#170)
- **[CHANGED]** chart (revue et audit LOT-4.2) : Role réduit aux appels réels (`pods/exec` create seul, `statefulsets` sans update, `events` list, PVC get) ; liste d'adresses validée entrée par entrée (/8 IPv4 et /16 IPv6 au minimum) ; `rootBase` normalisé ; `extraEnv` et `ingress.annotations` ne peuvent pas remplacer les variables et annotations posées par le chart ; `clusterIssuer: a-renseigner` refusé ; racine du conteneur en lecture seule ; QUALIF dans le namespace `claude-qualif` sur la racine dédiée `/share/MFS/ROOT/CLAUDE-QUALIF` (#170, #173)
- Aucun `BREAKING` : sans les nouvelles variables ni les nouveaux réglages, hub Linux et exe Windows se comportent comme en v6.2.0.

## [20261115] — v6.2.0 : tailles par zone (#150), carte TEAM (#151), en-têtes (#152), rafraîchir (#158), interface web (#163)

- **[NEW]** préférences par poste `treeFontSize`, `cardsFontSize` (#150) — rendues aussi par `configModel` `kind: "hub"`
- **[NEW]** `cards.teamHead` (`tpl`, `claude`, `odd`) et `cards.team[].odd` (#151) — champs existants conservés
- **[NEW]** `layout.panes[].state` (`lead` / `active` / `idle` / vide) (#152) — `role` et `active` inchangés
- **[NEW]** options tmux de pane `@cl_state`, `@cl_name` et options de fenêtre `pane-border-*` posées par le launcher qui a la main (#152) — `contracts/tmux-options.md` ; un launcher antérieur les ignore
- **[NEW]** `refreshProject` + évènement `refresh`, `forceClose`, `forceCloseConfirm` ; lignes de l'instantané : `refreshing`, `canForce` (#158)
- **[NEW]** réglage `desktop_web` de `backends.yaml` (`defaults` / `hubs.<machine>[.<système>]`), champ `webUI` de l'instantané, méthode `webUIOpen` (#163)
- **[CHANGED]** `clipboardRead` / `clipboardWrite` : `unavailable` décidé **par appel** (client web) et non plus par processus ; `quit`, `setTheme` refusés à un client web (#163) — rétrocompatible pour `desktop --web` seul
- **[CHANGED]** `desktop --web` sans `--addr` : utilise `desktop_web.listen` s'il est fixé (sinon port libre, inchangé) (#163)
- **[NEW]** clés `LAYOUT_MODE` (`top` | `left`, défaut `top`), `LAYOUT_LEFT_LEADER_PCT`, `PROJECT_LAYOUTS` de `config.conf` (#153) — sans réglage, la mise en page est inchangée
- **[NEW]** `projectSettings` : `layout`, `layoutAuto`, `layouts` ; `projectApply` : argument `layout` (#153)
- **[CHANGED]** `projectApply` : `colorSaved` vrai dès que `config.conf` est écrite, pour la couleur ou pour l'organisation (#153) — rétrocompatible
- Coque Linux (#152) : forme arrêtée — ligne de titre par pane (`pane-border-status top`), maquette A retenue
- Aucun `BREAKING`.

## [20261113] — v6.1.0 : panes cachés (#136) et configuration structurée (#118)

- **[CHANGED]** `cards.team` (interne à l'exe : frontend embarqué, aucun consommateur externe) — une ligne par teammate, champ `count` retiré, champs `pane`, `started`, `active`, `hidden`, `pinned`, `idleLeft` ajoutés (`contracts/desktop-dispatch.md`)
- **[NEW]** `clipboardWrite`, `clipboardRead` (LOT-4, #155) — presse-papiers du poste par le runtime Wails, « indisponible » en `--web` ; **[CHANGED]** `termInput` rend un refus (`readonly`, `toobig`) au lieu d'ignorer la saisie
- **[NEW]** `projectSettings`, `projectApply` (LOT-3.2, #148) — réglages d'un projet : backend, image, couleur, `env.yaml` structuré, aperçu fusionné avec provenance (`contracts/desktop-dispatch.md`)
- **[ADDED]** `cards.team[]` (cycle 4) — `tplState` (`ok`/`late`/vide), `tplTarget`, `companionFile` : état du template par teammate (`contracts/desktop-dispatch.md`)
- **[NEW]** `teamToggle` — bascule caché / visible d'un teammate inactif
- **[NEW]** options tmux `@cl_panevis` (fenêtre), `@cl_idle_at`, `@cl_home` (pane) et session de réserve `cl-hidden` — `contracts/tmux-options.md` ; un launcher antérieur les ignore (panes cachés laissés dans la réserve)
- **[NEW]** `configModel`, `configApply`, `backendTest`, `backendStates` — configuration structurée
- **[NEW]** `projectSettings`, `projectApply` — réglages d'un projet
- **[NEW]** clé `TEAM_HIDE_IDLE` de `config.conf` (défaut 30, 0 = jamais) ; préférence par poste `termFontSize`
