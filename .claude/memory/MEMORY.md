# Memoire projet — Claude-Launcher

> Source de verite unique au demarrage d'une session (/start-session). Mise a jour par /end-session.

## Etat courant

- **Version** : **v7.0.0** en cours de livraison (milestone #27 « Accès authentifié »). Candidate **7.0.0.6** ; QUALIF
  helm rév. **56** ; **PROD à déployer** : rév. **10**, depuis la rév. 9 (v6.9.0).
- **Précédente** : v6.9.0 en PROD (milestone #36 fermé, tag `v6.9.0`, helm `claude-ng2` rév. 9).
- **Branche** : `milestone/v7.0.0` (`main` après la PROD).
- **Environnements** :
  - PROD : namespace `claude-ng2`, v6.9.0 rév. 9 ; v7.0.0 rév. 10 à déployer.
  - QUALIF : `claude-launcher-qualif`, helm rév. 56.
  - Accès public : **QUALIF** `https://coupel.net/claude-qualif`, **PROD** `https://coupel.net/claude` (sous-chemin, P12).
  - Hub : `/share/MFS/ROOT/CLAUDE_PROJETS/claude-launcher-ng2` + `.exe` (binaires à remplacer après la PROD, contrôle SHA256).
  - CA interne cert-manager `claude-launcher-ca` (nameConstraints `launcher.local` / `launcher-qualif.local`).
  - Kubeconfig Rancher ADMIN dans `PRIVATE/{QUALIF,PROD}/kubeconfig` (risque accepté).
- **Travail en cours** : aucun. Restent : PROD v7.0.0 (`/deploy prod`), merge `milestone/v7.0.0` → `main`.

## Décisions de la session (v7.0.0)

Arbitrées par l'utilisateur (`ng2/docs/saas.md` § 5.1 ; plan `plan-v7.0.0` P1–P13) :

- **P1** authentification **activée par défaut** (`auth.enabled: true`) ; `false` permis (réglages en lecture seule, ingress externe refusé).
- **P2** deux rôles : `auth.admins` (admin) et `auth.users` (user).
- **P3** audit : une entrée par appel, méthodes fréquentes agrégées par minute (`audit.jsonl` + sortie du conteneur).
- **P4** R1 recentrée : `admin` = serveur et options globales ; `user` libre sur ses projets et backends ; coffre, clés et
  `known_hosts` = admin en v7.0.0 (« ses secrets » en v7.1.0).
- **P5** pods projets : dossier du projet + `.claude` seulement (`subPath`), pas de coffre ni de jeton ServiceAccount.
- **P6** pods en marche à l'ancien modèle : conservés et signalés (carte, diagnostic) ; recréés à l'arrêt.
- **P7** adresse annoncée aux sessions = hôte externe quand il est activé.
- **P8** GitHub App à **deux URL de retour** (interne et externe), **même application pour QUALIF et PROD**, sous réserve
  que GitHub accepte le `.local` (vérifié en QUALIF).
- **P9–P11** compte local `admin@local` **activé par défaut, hôte interne seul**, jamais sur l'externe (P10) ; mot de passe
  **généré** par le chart (`existingSecret` bcrypt en option).
- **Clés GitHub** : `auth.existingSecret` (recommandé) ou valeurs Helm `auth.github.clientId` / `clientSecret` (dans
  `PRIVATE/<ENV>/values-local.yaml`, jamais commitées ; `helm get values` les affiche). Redémarrage : `rollout restart`
  avec `existingSecret`, automatique avec les valeurs (checksum).
- **Accès public** : par **sous-chemin** `coupel.net/claude-qualif` (QUALIF) et `coupel.net/claude` (PROD). **Risque
  d'origine partagée accepté** (P12) ; sous-domaine dédié recommandé, non retenu. **Pas de HSTS** avec un sous-chemin (P13).
  Cookie externe : `__Secure-cl_auth_ext_<préfixe en tirets>` (QUALIF `__Secure-cl_auth_ext_claude-qualif`, PROD
  `__Secure-cl_auth_ext_claude`), `Path` = le sous-chemin, sans `Domain` ; sans sous-chemin `__Host-_cl_auth_ext`.
- **Login GitHub** : copier **avec sa casse exacte** (`--github-user` sensible à la casse) ; login renommable → le retirer
  avant de renommer ou supprimer le compte.

## Decisions de la session (v6.9.0)

- **#250** : réglage modèle/effort des teammates depuis la carte TEAM (menu natif / `display-menu`), compagnon
  `.claude/agents/<agent>.md`, effet au prochain spawn ; teamleader : compagnon + `.claude/settings.json`.
- Décisions GATE 2 : refus en accès distant ; efforts filtrés par modèle (`core.ModelEfforts`) ; entrée « Défaut (template) ».
- Revue : échappement `#` tmux et verrou des réglages concurrents corrigés. Outil `seed-factice-team.sh`.

## Decisions antérieures (condensées)

- **v6.8.2** : clic souris décalé d'une ligne avec un bandeau (#242) ; `fixScaledMouse()` ; `x/net` v0.57.0 (GO-2026-5026).
- **v6.4.0** « Backends distants » : clés ssh et kubeconfig par backend (#193/#194), NetworkPolicy par défaut (#200).
  Pas d'authentification en cluster (levée en v7.0.0).
- **v6.3.0** : accès web sans jeton en cluster (`web.access: none`) ; contexte k8s ignoré en cluster.
- **v6.2.0** : Ctrl+molette par zone ; `scripts/ci.sh` isole tmux sous `/tmp` ; `config.conf` (jeton GitHub jamais affiché).
- **Interfaces** : fenêtre native et `--web` ne tournent pas dans le même processus ; macOS = coque tmux + navigateur.
- **Outil** `chrome-headless-shell` dans `.claude-launcher/env.yaml`.

## RETEX v7.0.0 — leçons

- **Casse des logins GitHub** : oauth2-proxy compare `--github-user` **en respectant la casse** ; le launcher, lui, ignore
  la casse. Le chart transmet les logins tels que saisis ; une même identité sous deux casses est refusée au rendu (408a7ab).
  Symptôme : « Erreur de connexion », `500`, `missing github user` dans le sidecar.
- **`filepath` vs `path`** sur les chemins de pod (Linux/Windows) : `path` pour les chemins POSIX dans le pod ; CI Windows
  rouge tant que ce n'était pas le cas.
- **Tests instables traités (#270)** : `create214`, clipboard, `move216` ; `TestProxy172StartRefusals` stabilisé.
- **Garde `scripts/ci.sh` fail-closed avec canari** : un contrôle qui ne voit rien doit échouer, pas passer. Limites : ne voit
  pas les formes YAML en flux, ni `.json` / `.md`, ni les fichiers non suivis par git (confort, pas garantie).
- **`git stash` interdit aux agents** (perte de travail des teammates).
- **Plan de test HTML du GATE** : le commander au `test-writer` et l'**ouvrir dans Chrome** avant le GATE (oubli corrigé).
- **Serveur de fichiers** : `_work/test-plan/` renvoie `404` (#271) ; utiliser `_work/mockup/7.0.0/`.
- **Vérifier la méthode de test avant d'accuser** : le `KO` du client-id venait d'un séparateur `&` mal passé, pas de GitHub.

## Points pour la prochaine session

- **PROD v7.0.0** : `/deploy prod` (rév. 10), contrôle SHA256 des binaires hub, puis merge `milestone/v7.0.0` → `main`.
- **Ouverts** :
  - #268 fermée (estimation des quotas) ; #269 « Fermer / Forcer fermer » sans effet (bug) ;
  - #270 tests instables (partiellement traité) ; #271 serveur de fichiers 404 sur `_work/test-plan/` ;
  - `tests/integration/ui-web/clic-bandeau-242/run.sh` non exécutable ; S10.3 renvoie 308 au lieu de 302 ;
  - extension Chrome derrière le proxy : à tester après la PROD ;
  - `gitleaks` absent (fiche outil manquante dans le handoff dev-backend) ;
  - formes YAML exotiques non couvertes par la garde `ci.sh`.
- **Validations humaines** à jouer en QUALIF : scénarios 2 à 5 et 7 à 12 (GitHub réel, page de connexion, déconnexion),
  accès `https://coupel.net/claude-qualif` (GitHub seul, `admin@local` interdit sur l'externe).
- **Hub** à relancer (`Esc` puis `l`) ; binaires `launcher` à remplacer après PROD.
- BuzzMaster (`other-buzzmaster-0`) : miroir apt FR à reposer après recréation (pas d'issue, décision utilisateur).
- Pistes non ouvertes : suites ui/desktop ≈199 s et core ≈72 s, hors budget 60 s ; `xhigh`/`max` dans `effortLevel` à vérifier
  en QUALIF ; test Windows instable `TestFilesSansSessionEnModeNone174`.
- Limites connues : badge conservé jusqu'à 24 h si claude est mort mais son pane resté ; un outil parallèle terminé pendant
  une permission l'efface ; supprimer `trust.json` lève toutes les approbations.
- Procédure HUMAIN #223 rejouable via `tests/procedures/ui-desktop/attente-teamleader-223/simulate-attention.sh`.
- Cadrage SaaS migré dans `ng2/docs/saas.md` (la copie de travail `.claude/workflow/planner-v7-cadrage.md` a été supprimée).

## Regles critiques

- `Chart.yaml` doit suivre la version du milestone (contrôle automatisé #204 dans `scripts/ci.sh`).
- Tests de refus distant non rejoués sous Windows (leçon v6.9.0) : à rejouer sur l'exe Windows avant de déclarer le refus validé.
- Règles permanentes du projet (tests isolés, milestone non démarré, maquette, README, mesure Windows réelle, issue rouverte,
  concision) : `.claude/agents/context/COMMON.md`.
- Branches : `CLAUDE.md` « Conventions Git » ; fusion vers `main` par PR (`project-config.json` → `release.merge_mode`).
- `git stash` interdit aux agents.

## Corrections de comportement

- Réseaux sociaux : aucun post (`marketing.social` absent de `project-config.json`, opt-in du template).
