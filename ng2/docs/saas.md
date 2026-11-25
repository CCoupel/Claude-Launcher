# Vers un launcher multi-utilisateur (étude SaaS, #184)

> Autres guides : [Déploiement dans un cluster Kubernetes](kubernetes.md) · [Coffre des secrets](secrets.md) · [Architecture](architecture.md) · [README](../README.md).

Ce document reprend l'étude d'architecture faite pour l'issue #184 : ce que le launcher doit changer pour servir plusieurs
personnes, les décisions prises, le découpage en milestones, et l'état après **v7.0.0**. Il remplace le cadrage de travail
qui servait à l'équipe d'agents.

**Sommaire**

- [1. Ce qui est mono-pilote aujourd'hui](#1-ce-qui-est-mono-pilote-aujourdhui)
- [2. Réponses aux questions de #184](#2-réponses-aux-questions-de-184)
- [3. Architecture retenue pour #249](#3-architecture-retenue-pour-249)
- [4. Découpage en milestones](#4-découpage-en-milestones)
- [5. Décisions D1 à D11](#5-décisions-d1-à-d11)
- [6. État après v7.0.0](#6-état-après-v700)

---

## Résumé

- **Verdict** : le multi-utilisateur est faisable sans toucher au mode poste, mais pas dans le launcher actuel tel quel.
  Aujourd'hui le launcher ne connaît qu'une **instance** (hub, machine, uid), jamais un **utilisateur** : une config claude,
  un jeton GitHub, un coffre et un `backends.yaml` servent tout le monde.
- **Recommandation (D1)** : un **plan de contrôle multi-locataire** unique (identité, comptes, limites, partage, interface)
  et un **agent par utilisateur** (plan de données), qui seul lit le contenu des projets de son utilisateur.
- **Constat bloquant (F1)** : les pods projets k8s montaient toute la racine. **Corrigé en v7.0.0** (LOT-1, #253).
- **Quatre milestones** : v7.0.0 accès authentifié mono-utilisateur ; v7.1.0 comptes et espaces ; v7.2.0 partage ;
  v7.3.0 stockage et backends fournis par l'utilisateur.

---

## 1. Ce qui est mono-pilote aujourd'hui

Le launcher identifie une instance par `hôte:uid:socket` (`core/owner.go`), et `CL_HUB_NAME` désigne le hub. Il n'a aucune
notion d'utilisateur humain, de rôle ni de quota (hors quota claude.ai du compte unique et limites techniques du web).

| Domaine | Aujourd'hui | Ce que le multi-utilisateur casse |
|---|---|---|
| tmux et hub | Sockets et sessions à nom fixe. En cluster, les sessions vivent dans les pods projets. | Qui peut s'attacher à quelle session (autorisation), mode lecture seule. |
| Pilote et présence | `@cl_owner` + TTL de 20 s désigne une instance, pas une personne. | Le partage « full » doit porter une identité utilisateur. |
| État du hub | `projects.json`, `trust.json`, `moves.json` et préférences partagés par tous les hubs. | À partitionner par utilisateur. |
| `config.conf` | Fichier bash sourcé : réglages plateforme et personnels mêlés. | Séparer réglages plateforme (admin) et utilisateur, en données (YAML ou JSON) : un script sourcé pour un compte tiers exécuterait du code. |
| Coffre | Un seul coffre en clair, par backend. | Secrets par utilisateur (clés ssh, kubeconfig, jeton git). |
| Jeton GitHub | `GITHUB_TOKEN` global, diffusé dans un unique `settings.json`. | Un jeton par utilisateur, injecté dans ses sessions seulement. |
| `CLAUDE_CONFIG_DIR` | Un dossier, une connexion claude.ai, des teams. | Un dossier par utilisateur. |
| Accès web | Un seul état d'interface (sélection, préférences) pour tous les clients. | Une identité par requête, une autorisation par méthode et par projet, un état par session. |
| Chart Helm | Un PVC RWX à la racine, un Role du namespace, pods projets sans label utilisateur. | Un volume par utilisateur, label `cl.user`, NetworkPolicy entre utilisateurs, aucun jeton de ServiceAccount dans les pods projets. |
| Backends | Un `backends.yaml` global, stockage supposé monté au même chemin partout. | Backends par utilisateur, avec ses identifiants. |
| Lectures directes | Le launcher lit lui-même `git status`, `.git/config`, milestones, `/files`. | Ces lectures se font dans l'agent de l'utilisateur, jamais dans le processus partagé. |

### Constats de sécurité

- **F1 — coffre visible depuis tout pod projet** (préexistant). Le manifeste montait chaque volume à sa racine. Les pods
  voyaient donc les clés ssh, les kubeconfigs, `backends.yaml` et la config claude de tous. **Corrigé en v7.0.0** : seuls le
  dossier du projet et `CLAUDE_CONFIG_DIR` sont montés en `subPath` (`kubernetes.md` §8b).
- **F2 — exécution de code via les dépôts.** `git status` dans un dépôt dont un tiers contrôle `.git/config`
  (`core.fsmonitor`, filtres) exécute une commande dans le processus du launcher, avec ses secrets. Acceptable en
  mono-utilisateur, inacceptable en multi-locataire. Règle n°1 : **le plan de contrôle n'exécute rien dans le contenu d'un
  utilisateur**.
- **F3 — `/files` et liens symboliques.** Un lien posé par A vers l'espace de B serait servi par un service de fichiers
  commun. Même règle : le service de fichiers doit vivre dans l'agent.
- **F4 — `config.conf` est sourcé par bash.** Pour des réglages utilisateur, il faut un format de données.

---

## 2. Réponses aux questions de #184

### Q1 — Modèle d'isolation

| Option | Principe | Verdict |
|---|---|---|
| A. Launcher par utilisateur provisionné | Un StatefulSet et un PVC par utilisateur ; portail et opérateur à construire | Isolation forte, mais partage difficile, #249 exige quand même un agent, ClusterRole nécessaire |
| B. Launcher unique multi-locataire, accès direct aux stockages | Un seul processus pour tous | Disqualifiée : F2 et F3 en plein, #249 impossible sans agent |
| **C. Plan de contrôle + agent par utilisateur** | Le plan de contrôle garde identité, comptes, limites et interface ; l'agent garde l'espace de l'utilisateur | **Recommandée** : isolation des données par construction, réponse native à #249 |
| D. Portail relais + launcher auto-hébergé | Chaque utilisateur héberge son launcher | Rien chez l'opérateur, mais pas de backends plateforme ; variante de C, ouverte plus tard |

Risques de C : un protocole plan de contrôle ↔ agent à versionner ; la latence des métadonnées (l'agent pousse ses
événements au lieu d'être interrogé) ; un composant de plus à déployer ; un jeton d'agent devenu secret critique.

### Q2 — Identité

- **Fournisseur** : GitHub OAuth d'abord (tous les utilisateurs ont un compte GitHub). L'OIDC générique reste possible en
  changeant la configuration du proxy.
- **Où** : oauth2-proxy **en sidecar** du pod, le launcher n'écoutant qu'en boucle locale : l'usurpation d'en-tête est
  impossible par construction.
- **Clé** : l'identifiant numérique GitHub (stable) ; le login sert à l'affichage. Pas d'utilisateur système par personne.
- **Rôles** : `admin` (réglages plateforme, comptes, limites) et `user`. Partage par projet (`owner`, `full`, `readonly`) en v7.2.0.

### Q3 — Secrets par utilisateur

- Les secrets d'un utilisateur vivent **dans son espace**, côté agent (jeton git, clés ssh, kubeconfigs, connexion claude.ai),
  chiffrés au repos. Le plan de contrôle ne garde que l'identité, le registre et le jeton d'agent.
- La saisie passe par l'interface puis part à l'agent, qui écrit. Aucune relecture.
- Pour v7.1.0, un Secret Kubernetes par utilisateur est une alternative acceptable (D8).

### Q4 — Montages

- Chaque utilisateur a un espace : `PROJETS/`, `.claude/`, `.config/` personnel. En v7.1.0 : `<racine>/users/<id>/`, monté en
  `subPath` dans ses seuls pods.
- Le quota de taille relève du stockage (quota par dossier), pas du launcher.
- Un projet commun n'est jamais monté deux fois : le partage passe par l'autorisation et le relais du plan de contrôle.

### Q5 — Paramètres

| Plateforme (admin) | Utilisateur (son espace, YAML ou JSON) |
|---|---|
| Fournisseur d'identité, admins, inscription | Couleurs, dispositions, thème, `CLAUDE_OPTIONS` |
| Backends plateforme, ressources par pod | Ses backends (v7.3.0), ses secrets |
| Limites par défaut et par compte | Organisations, template préféré, variables de ses projets |
| Exposition (ingress, URL publique), image | — |

`config.conf` reste la configuration du mode poste et de l'espace de l'admin.

### Q6 — Ressources et coûts

Les limites portent sur deux compteurs : projets créés (taille du registre) et projets ouverts en parallèle (pods en cours).
CPU et mémoire restent fixés par pod. Pas de ResourceQuota par utilisateur en namespace partagé (D9). L'agent hébergé peut
passer à zéro réplique sans session active.

### Q7 — Sécurité

- Rien ne répond sans identité. Autorisation sur chaque méthode et chaque projet. Règle R1 : les écritures sensibles sont
  réservées aux administrateurs authentifiés.
- Réseau : NetworkPolicy en refus entre pods d'utilisateurs différents ; agents joignant seulement le plan de contrôle
  (connexion sortante) ; pas de jeton de ServiceAccount dans les pods projets.
- Audit : journal nominatif (login GitHub) des connexions, écritures, ouvertures et partages.
- Règle n°2 : les pods projets ne voient que l'espace de leur propriétaire.

### Q8 — Compatibilité

Le mode poste (hub tmux Linux, exe Windows, fenêtre native) reste mono-utilisateur, sans identité, et inchangé. En cluster,
l'interface « espace de projets » a deux implémentations : locale (comportement actuel) et agent.

---

## 3. Architecture retenue pour #249

Le stockage fourni par l'utilisateur ne doit pas être monté chez le launcher. Quelqu'un côté stockage doit donc faire les
lectures : c'est un **agent d'espace**.

- **Quoi** : le binaire ng2 en sous-commande `agent`, qui réutilise `core` (projets, git, milestones, attention, fswatch,
  clone, fichiers bornés à l'espace, sessions sur les backends de l'utilisateur).
- **Où** : v7.1.0, un pod par utilisateur dans le namespace de la plateforme ; v7.3.0, sur le backend de l'utilisateur, à côté
  de son stockage.
- **Lien** : connexion **sortante** de l'agent vers le plan de contrôle (WebSocket sur HTTPS), authentifiée par un jeton
  d'agent révocable. Elle passe derrière NAT et pare-feu, sans port ouvert chez l'utilisateur.
- **Protocole** : requêtes (projets, fichiers, création, clone, ouverture de session), événements poussés (git, attention,
  état des sessions), flux de terminal (v7.3.0), négociation de version. Contrat à créer au plan de v7.1.0.
- **Terminaux** : backend joignable par la plateforme → attachement direct comme aujourd'hui ; backend privé → flux relayé
  par l'agent (v7.3.0).
- **Hors ligne** : l'agent absent donne des projets « injoignables », sans perte.

---

## 4. Découpage en milestones

| Milestone | Objectif | Issues et nouvelles issues |
|---|---|---|
| **v7.0.0** Accès authentifié | Le launcher de cluster n'accepte plus rien sans identité GitHub autorisée ; pods projets sans coffre ; ingress externe derrière l'authentification ; ce document | #172, #184, N1 (identité, audit), N2 (R1), N3 (pods projets, F1), N4 (ingress externe) |
| **v7.1.0** Comptes et espaces | Plusieurs comptes approuvés, chacun avec ses projets, sa connexion claude.ai, son jeton git, ses réglages, sur le backend k8s de la plateforme | N5 agent d'espace, N6 état d'interface par session, N7 registre des comptes, #247 demandes de compte, #244 liste par utilisateur, N8 cloisonnement des pods, #248 config par compte, N9 secrets par utilisateur, #246 limites, N10 mise en veille |
| **v7.2.0** Partage | A partage un projet avec B, en lecture seule ou en accès complet, révocable | #245, N11 lecture seule côté serveur, N12 pilote = utilisateur |
| **v7.3.0** Stockage utilisateur | L'utilisateur branche son stockage et ses backends ; la plateforme n'y accède jamais | #249, N13 agent auto-hébergé, N14 backends de l'utilisateur, N15 terminaux relayés, N16 agent hors ligne |

Dépendances : v7.1.0 dépend de v7.0.0 (identité, R1, pods sans coffre) ; v7.2.0 et v7.3.0 dépendent de v7.1.0. Ordre
v7.2.0 / v7.3.0 : à décider (D11).

Les estimations de chaque milestone (fichiers touchés, complexité) et les lots ont été faits au plan de chaque milestone,
pas ici.

---

## 5. Décisions

### 5.1 Arbitrées pour v7.0.0 (décidé)

Ces points sont tranchés par l'utilisateur. Ils priment sur les recommandations du cadrage. Référence : `plan-v7.0.0` (P1 à P11).

| Point | Arbitrage retenu pour v7.0.0 |
|---|---|
| **P1** Authentification | Activée par défaut (`auth.enabled: true`). `false` reste permis : lecture seule des réglages depuis l'interface, ingress externe refusé. |
| **P2** Rôles | Deux listes : `auth.admins` et `auth.users` (rôles `admin` et `user`). |
| **P3** Audit | Une entrée par appel ; les méthodes fréquentes sont agrégées par minute. |
| **P4** R1 (recentrée, addendum 2) | `admin` = serveur et options globales. `user` libre sur ses projets et ses backends. Coffre, clés et `known_hosts` réservés à l'admin en v7.0.0 ; « ses secrets » viendront en v7.1.0. |
| **P5** Montages des pods projets | Dossier du projet et `.claude` seulement (`CLAUDE_CONFIG_DIR`). |
| **P6** Pods en marche | Les pods en marche à l'ancien modèle sont conservés et signalés (carte, diagnostic). |
| **P7** Adresse annoncée | L'hôte externe, quand il est activé. |
| **P8** Application GitHub | Une GitHub App à deux URL de retour (interne et externe). Sous réserve que GitHub accepte une URL en `.local` (vérification en QUALIF). |
| **P9** Compte local `admin@local` | Activé par défaut, sur l'hôte interne seul. |
| **P10** Compte local sur l'externe | Jamais, ni par le proxy ni par le launcher. |
| **P11** Mot de passe local | Généré par le chart ; un `existingSecret` bcrypt reste possible en option. |
| **Clés GitHub** | `auth.existingSecret` (recommandé), et valeurs Helm `auth.github.clientId` / `clientSecret` ajoutées en LOT-3 (dans `PRIVATE/<ENV>/values-local.yaml`, jamais commitées). |

### 5.2 Recommandé ou à décider (v7.1 et au-delà)

Ce tableau donne la **recommandation** du cadrage pour les milestones suivants. Rien n'y est tranché.

| Décision | Recommandation | Alternatives écartées |
|---|---|---|
| **D1** Modèle d'isolation | C : plan de contrôle + agent par utilisateur | A (ClusterRole, partage difficile), B (F2, F3, #249 impossible), D (pas de backends plateforme) |
| **D2** Mise en œuvre de l'authentification | oauth2-proxy en sidecar, launcher en boucle locale | Proxy séparé + Traefik forwardAuth (identité par en-tête, NetworkPolicy à tenir), OIDC intégré (code à auditer) |
| **D3** Fournisseur d'identité en v7.0.0 | GitHub OAuth | OIDC générique (Keycloak, Dex), les deux |
| **D4** Jeton du launcher en mode serveur | Supprimé : identité vérifiée, `web.access` à `proxy` derrière le proxy | Second facteur par jeton (double saisie, gain faible derrière OAuth) |
| **D5** Contenu de v7.0.0 | Authentification mono-utilisateur seule | Authentification + comptes + espaces dans v7.0.0 (milestone de 70 à 100 fichiers) |
| **D6** Ingress externe en v7.0.0 | Oui, derrière l'authentification (N4) | Ingress interne seul jusqu'en v7.1.0 |
| **D7** Stockage des utilisateurs tiers | `<racine>/users/<id>`, servi par un agent hébergé | Pas d'utilisateur tiers avant le stockage fourni (#249 en v7.1.0) |
| **D8** Secrets des utilisateurs | Dans l'espace de l'utilisateur, côté agent, chiffrés au repos | Secret Kubernetes par utilisateur (à migrer en v7.3.0), coffre central (tous les secrets dans le processus partagé) |
| **D9** Cloisonnement k8s | Namespace partagé, label `cl.user`, subPath, NetworkPolicy, limites par pod | Namespace par utilisateur créé par le launcher (ClusterRole, escalade) ou pré-créé par l'admin (manuel) |
| **D10** Portée des limites #246 et inscription #247 | Compteurs sur les backends plateforme ; inscription ouverte, approbation par l'admin | Compteurs sur tous les backends ; inscription sur invitation ou organisation |
| **D11** Ordre après v7.1.0 | Partage (v7.2.0) puis stockage utilisateur (v7.3.0) | Stockage utilisateur d'abord |

Hypothèse **H1** : l'espace de l'admin reste la racine actuelle, et la cohabitation hub Linux / exe Windows / launcher de
cluster est conservée (voir [kubernetes.md](kubernetes.md) §7). Pas de migration de données.

---

## 6. État après v7.0.0

Livré : accès authentifié du launcher de cluster, **mono-utilisateur**. Les décisions arbitrées (P1 à P11) sont au § 5.1.
En résumé :

- **Identité** : oauth2-proxy en sidecar (P1 à P2) ; comptes `auth.admins` et `auth.users` ; identité par requête, pas de jeton.
- **Compte de secours** `admin@local` : mot de passe généré par le chart, hôte interne seulement, jamais sur l'hôte externe (P9 à P11).
- **Accès externe** : second sidecar sans compte local ; l'adresse annoncée aux sessions est l'hôte externe quand il est
  activé (P7) ; l'application GitHub accepte les deux URL de retour (P8).
- **Rôles et R1** : `admin` gère le serveur, les options globales, le coffre et les champs de `backends.yaml` qui touchent
  au pod ; `user` gère ses projets et ses backends (`contracts/auth.md` §4).
- **Journal** : `audit.jsonl` et sortie du conteneur, une ligne par appel (agrégation des méthodes fréquentes).
- **Pods projets** : dossier du projet et `CLAUDE_CONFIG_DIR` seulement, pas de jeton de ServiceAccount (`kubernetes.md` §8b).

### Risques acceptés en v7.0.0

- **Login GitHub renommable** : avant de renommer ou de supprimer un compte, retirer son login de `auth.admins` et
  `auth.users`, sinon un autre compte peut reprendre le login.
- **Pas de locataire** : tous les utilisateurs authentifiés voient les mêmes projets et backends.
- **Pods projets en root** : les pods tournent encore en root avec les capacités par défaut. Durcissement en v7.1.0.
- **Pouvoir d'un `user`** : il exécute du code dans les pods de projets et y lit `GH_TOKEN` (`secrets.md`). Borné par
  l'interdiction des secrets du coffre et de `backends.yaml` dans les pods, mais non supprimé.
- **Stockage en clair** : le coffre reste en clair sur la racine partagée (`secrets.md`).

### Suite

v7.1.0 apporte l'agent d'espace et les comptes utilisateur ; la première étape est le contrat `agent-protocol`, à écrire au
plan de v7.1.0.
