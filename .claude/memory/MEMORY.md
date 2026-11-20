# Memoire projet — Claude-Launcher

> Source de verite unique au demarrage d'une session (/start-session). Mise a jour par /end-session.

## Etat courant

- **Version** : **6.4.0 publiée le 2026-11-19** — PR #202 (merge 1fc88d7) + correctif direct sur `main` 5e0b6cf
  (Chart.yaml 6.4.0, oublié au cycle ; 1er run release 44487529105 KO sans artefact public). Tag `v6.4.0` re-posé sur
  5e0b6cf (choix utilisateur) ; run 44488567222. Milestone #28 clos (10 issues). Image `ghcr.io/ccoupel/claude-launcher:v6.4.0`.
- **Branche** : main (5e0b6cf).
- **Environnements** :
  - PROD cluster : namespace `claude-ng2`, helm rév. 2 (`-f values-prod` + `values-local`), pod 1/1 v6.4.0,
    NetworkPolicy + PVC coffre `claude-launcher-secrets`. `https://launcher.local` non vérifié depuis le conteneur.
  - QUALIF cluster : namespace `claude-qualif`, image `claude-launcher-qualif:v6.4.0.14`, `https://launcher-qualif.local`.
  - Hub : `/share/MFS/ROOT/CLAUDE_PROJETS/claude-launcher-ng2` (+ `.exe`) en v6.4.0 (ancien `.bak-v6.2.0`). Relancer le
    hub (Échap puis `l`).
  - CA interne cert-manager `claude-launcher-ca` (nameConstraints `launcher.local` / `launcher-qualif.local`).
  - Kubeconfig Rancher ADMIN dans `PRIVATE/{QUALIF,PROD}/kubeconfig` (risque accepté). Registre local abandonné
    (`registries.yaml` géré par Rancher).
- **Travail en cours** : aucun workflow actif.
- **Prochain** : milestone #29 « v6.5.0 — Fiabilisation » (non démarré) : #201 realPath/jonctions, #203 test SSE
  instable, #204 contrôle Chart.yaml en ci-branch, #205 .tmp orphelins, #206 drain fsnotify Windows, #207 test
  sse_flow, #208 test web184 lent. v7.0.0 (#27) SaaS = #172 OAuth, #184 étude.

## Decisions techniques de la session

- **v6.4.0 « Backends distants et confort »** en 5 lots :
  - #199 version template du projet (titre carte TEAM, ⚠ orange si plus récente publiée, adoption au clic seulement).
  - #194 clés ssh privée + publique et empreinte par backend saisies par l'interface (coffre PVC RWO hors racine
    partagée, jamais renvoyées).
  - #193 kubeconfig par backend + `rbac.extraNamespaces` (fichier illisible = erreur, jamais de repli ServiceAccount).
  - #189–191 onglets (ordre par poste). #198 bandeau de mise à jour serveur + Recharger dans le client web.
  - #174 `/files/<catégorie>-<projet>/<chemin relatif>` (ancienne forme chemin complet supprimée ; `CL_BROWSER_PATH_MAP`
    / `CL_BROWSER_HINT` par session). #200 NetworkPolicy par défaut (pods Traefik seuls). #178 guide k8s.
- **Pas d'authentification en v6.4.0** : OAuth #172 → v7.0.0 (SaaS). Ingress interne seul (liste LAN), ingress externe
  préparé mais refusé au rendu. Risques acceptés : écriture des secrets depuis le LAN, docker :2375 sans TLS.
- Arrêt du launcher borné à 25 s (ouvertures 20 s + détachements 5 s), `terminationGracePeriodSeconds` ≥ 30.
- Windows : correctifs fsnotify (Close avant Add, Add hors goroutine de lecture), CtrName slashPath, `/files` racine.
- `ng2/README.md` découpé en 9 guides `ng2/docs/` (installation, configuration, windows, kubernetes, interface,
  versions, architecture, depannage, tests) ; schéma `ng2/docs/k8s-zones.svg`. Hub maquettes 127.0.0.1:8765, exe 8766.
- `helm upgrade` : `--reset-then-reuse-values` ou `-f`, jamais `--reuse-values` seul.
- **Leçon release** : `Chart.yaml` doit suivre la version du milestone (#204).

### Décisions antérieures (condensées)

- **v6.3.0** « Kubernetisation du launcher » (#179 et lots) : accès web sans jeton en cluster (`web.access: none`,
  risque accepté jusqu'à l'auth v6.4.0) ; contexte k8s ignoré en cluster (ServiceAccount du pod) ; variables `CL_*`
  sur le StatefulSet (pas de ConfigMap) ; #188 masquage désactivé = tout réapparaît.
- **v6.2.0** (#150–#164) : Ctrl+molette par zone (#150) ; verrous git `describe --dirty` supprimé, `scripts/ci.sh`
  isole tmux sous `/tmp` (préfixes de test seulement), `go test -race` et `node --check` dans `env.yaml` (#154) ;
  Rafraîchir relit sans caches, backend injoignable = nettoyage local seul, `safe.directory` par appel (#158) ;
  interface web servie par la fenêtre native, `desktop_web` désactivé par défaut, hors boucle locale refusé sans
  `allow_lan: true`, cookie de session en mémoire (#163) ; organisations `top` / `left` (#153) ; versions dans le titre
  TEAM, en-tête des panes vert/gris (#151/#152/#162) ; sécurité `--web` (jeton d'URL puis cookie, `/api/call` JSON +
  même origine, contrôle Host, `backendTest` limité à 2) ; LOT-3.3 `config.conf` (jeton GitHub jamais affiché,
  `__GARDER__`, variables sensibles refusées en saisie structurée) ; copier/coller Windows (Ctrl+Maj+C, Ctrl+B puis
  Ctrl+V = ^V brut, collage ≤ 4 Mo).
- **Interfaces** : fenêtre native et `--web` ne tournent pas dans le même processus ; macOS = coque tmux + navigateur,
  pas de fenêtre native.
- **Outil** `chrome-headless-shell` ajouté à `.claude-launcher/env.yaml` (PR #159) pour les captures d'écran.

## Points pour la prochaine session

- Relancer le hub si pas fait (Échap puis `l`) ; `pkill -f 'claude-launcher-ng2 mirror'` si d'anciens mirror /
  layout-watch tournent.
- Supprimer la branche `milestone/v6.4.0` au `/end-session` (issue/PR #202 fusionnée).
- **Vérifications humaines restantes** : `https://launcher.local` depuis un poste (non vérifié depuis le conteneur) ;
  prise de main hub ↔ cluster sur la racine réelle ; PSA `baseline` sur `claude-ng2` non posée.
- **Triage en cours** : #201/#203/#204 → v6.5.0 (milestone #29), à confirmer.
- **À confirmer par l'utilisateur** : 3 StatefulSets de projets à 0/0 depuis 14:45 (2026-11-17).
- **Dossier du hub** : binaires QUALIF anciens et `.bak` — nettoyage à décider.
- **Suites mineures notées, non créées en issues** : verrou d'écriture de la bascule sans borne si le partage est
  mort ; message de repli `top` répété ; `config.conf` écrit non atomiquement ; l'exe Windows n'est pas sauvegardé
  avant remplacement.
- **Idées** : `App.Stop` n'attend pas les terminaux (socket tmux relancé dans les tests) ; opérateur k8s + CRD
  `ClaudeProject` (non retenu pour l'instant).
- **Consigne** : concision aux teammates (DONE ≤ 2 lignes, détail dans les rapports).
- **Maquettes** : proposer d'emblée les variantes (horizontale ET verticale) quand une mise en page est en jeu.

## Regles critiques

- Règles permanentes du projet (tests isolés, milestone non démarré, maquette, README) : `.claude/agents/context/COMMON.md`.
- Branches, PR et fusion vers `main` : `CLAUDE.md` « Conventions Git ».

## Corrections de comportement

- Les agents doivent vérifier sur Windows réel ce qu'ils mesurent sur Linux : trois refus du GATE LOT-5 viennent de
  mesures simulées (tour lent, FS local) qui ne reproduisaient pas wsl.exe / partage 9P.
