# Versions, mises à jour et template

> Autres guides : [Installation et lancement](installation.md) · [Configuration et projets en conteneur](configuration.md) · [Launcher Windows natif](windows.md) · [Déploiement dans un cluster Kubernetes](kubernetes.md) · [Interface](interface.md) · [Architecture](architecture.md) · [Dépannage](depannage.md) · [Tests](tests.md). Vue d'ensemble : [README](../README.md).

**Sommaire**

- [Versions, mises à jour et template](#versions-mises-à-jour-et-template)

---

## Versions, mises à jour et template

**Au lancement**, la coque se met à jour si la dernière release GitHub est plus récente : le binaire
de la plateforme est téléchargé, vérifié contre `SHA256SUMS`, remplacé,
puis le NG se relance. L'interface native (`desktop`, défaut sous Windows) ne se met pas à jour
avant d'ouvrir sa fenêtre : l'annonce ci-dessous prépare la version, un clic l'installe. Un navigateur resté sur l'ancien binaire est redémarré ; les projets
ouverts ne sont pas touchés. Hors ligne ou en cas d'échec, il démarre avec la version en place.
Désactiver : `claude-launcher-ng2 --no-update` ou `CL_NO_UPDATE=1`. Un binaire compilé à la
main (version `dev` ou suffixée) ne se remplace jamais.

Si le tag est déjà poussé mais que sa release n'est pas encore publiée (CI en cours), le NG
démarre avec la version en place.

Ensuite, vérification GitHub 6 s après le démarrage, puis toutes les 10 minutes (2 minutes tant
qu'une release est attendue ou pas encore prête). Chaque nouvelle version du NG passe par trois
couleurs :

| Annonce | État | Clic |
|---|---|---|
| **orange** `↑ NG vX · nouvelle version en préparation` | tag poussé, release pas encore publiée (CI en cours) | sans effet |
| **jaune** `↑ NG vX · ↓ version 45 %` | release publiée (selon l'API GitHub) : le binaire est **téléchargé dans le dossier temporaire** (`$TMPDIR`, sinon `/tmp`) et contrôlé ; étapes affichées : `↓ version …%`, `↓ par l'API GitHub…` (lien direct en erreur), `↓ SHA256SUMS`, `contrôle SHA-256` | sans effet |
| **jaune** `↑ NG vX · HTTP 504 · réessayer` | publiée mais inaccessible : raison de l'échec (`HTTP 504`, `délai dépassé`, `somme incorrecte`, `réseau`) ; nouvel essai à la vérification suivante (2 min) | réessayer tout de suite |
| **vert** `↑ NG vX · cliquer pour installer` | binaire prêt et contrôlé | **copie** le binaire préparé à la place de l'actuel (après un dernier contrôle de sa somme) et redémarre le navigateur |

Sur un terminal large (≥ 70 colonnes), les étapes sont en toutes lettres
(`téléchargement de la version… 45 %`, `disponible mais inaccessible (HTTP 504) · cliquer pour
réessayer`…). Un binaire préparé déjà présent (relance du launcher) n'est pas retéléchargé,
seulement recontrôlé ; s'il a disparu ou changé au moment du clic, il est retéléchargé.
Le template garde ses deux temps : orange au tag, vert à la release.

Les fichiers sont pris par le lien direct de la release
(`github.com/…/releases/download/<tag>/…`) ; s'il répond en erreur 5xx (GitHub sert
parfois un 504 constant pour certains fichiers), un second essai est fait, puis le
fichier est pris par l'API GitHub (`releases/tags/<tag>` → `releases/assets/<id>`).
Chaque fichier reste vérifié contre `SHA256SUMS`.

- **NG** : un clic sur l'annonce verte installe la version préparée et redémarre le navigateur
  sur place (projets et zone tmux conservés) ; `✕` masque l'annonce (elle revient quand elle
  change de couleur).
- **Template** (`↑ tpl vX …`) : un clic sur l'annonce verte adopte la version : les caches
  `.template-commit` / `.template-version` (dans `~`) passent au
  tag, `init-project.md` est resynchronisé et les badges `✓`/`⚠` recalculés. Un projet en
  retard propose ensuite `/init-project` à son lancement, en affichant la version du projet et
  celle disponible (`↑ template en retard : projet v4.1.0 (e925929) → disponible v5.5.0 (1520308)`). Sans cache (première installation),
  le dernier tag est adopté d'office.
- **Version majeure commune** : à partir de la v5, launcher et template partagent leur version
  majeure (le template v5 suppose un launcher v5). NG2 ne retient que les tags du template dont
  la majeure ne dépasse pas la sienne : un template v6 ne sera jamais proposé à un NG2 v5.
- **`init-project.md`** : resynchronisé au démarrage (au commit adopté) puis à chaque adoption
  (`~/init-project.md`, copié dans `.claude/commands/` à l'ouverture d'un projet).
  Hors ligne, l'ancienne version est conservée.
