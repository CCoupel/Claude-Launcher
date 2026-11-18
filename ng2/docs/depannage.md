# Dépannage

> Autres guides : [Installation et lancement](installation.md) · [Configuration et projets en conteneur](configuration.md) · [Launcher Windows natif](windows.md) · [Déploiement dans un cluster Kubernetes](kubernetes.md) · [Interface](interface.md) · [Versions, mises à jour et template](versions.md) · [Architecture](architecture.md) · [Tests](tests.md). Vue d'ensemble : [README](../README.md).

**Sommaire**

- [Dépannage](#dépannage)

---

## Dépannage

- **`Ctrl+←/→` ne fait rien** : votre terminal les intercepte. Glissez la bordure à la souris.
- **La bascule de mise en page est lente** : le détail est dans `desktop-perf.log`, dans le dossier de cache du launcher
  (`~/.cache/claude-launcher-ng2/` sous Linux, `%LocalAppData%\claude-launcher-ng2\` sous Windows). Les lignes
  `bascule : avant écriture`, `bascule : écriture config.conf` et `bascule : total` donnent la durée de chaque étape
  (fichier gardé à 200 lignes).
- **Ctrl+clic ne ferme pas le projet** : votre terminal le garde pour lui (ouverture de liens).
  Sélectionnez le projet et appuyez sur `Suppr`.
- **Un projet ne se ferme plus** (pod k8s disparu, backend injoignable, session déjà morte) : menu du projet →
  « Forcer la fermeture… » (ou `Suppr` dans la coque Linux). Elle ne touche ni à `projects.json` ni au backend
  quand celui-ci est injoignable ; voir [Rafraîchir et forcer la fermeture](configuration.md#rafraîchir-et-forcer-la-fermeture-v620-158).
- **Windows : « dubious ownership » sur un dépôt du partage** : l'étape git du rafraîchissement l'affiche ; voir la
  dernière puce de [Rafraîchir et forcer la fermeture](configuration.md#rafraîchir-et-forcer-la-fermeture-v620-158) (exception `safe.directory`
  limitée au dossier du projet, risque à accepter pour vos propres dépôts seulement).
- **L'état d'un projet est en retard** : recalcul d'au plus ~10 s pour le projet sélectionné ;
  `Ctrl+R` relance le scan des projets.
- **Panes d'une team en désordre** : `Ctrl+B R` dans l'onglet du projet, ou
  `claude-launcher-ng2 relayout <window_id>`.
- **Quitter** : `Esc` puis `l` arrête le launcher (serveur `claude-ui-ng2`) en laissant les projets
  tourner dans `claude-hub-ng2` ; `Esc`, `t` puis `o` ferme aussi `claude-hub-ng2` et ses sessions groupées.
- **Pas de zone tmux à droite** : relancer `claude-launcher-ng2`, le pane manquant est recréé.
- **Navigateur disparu** (plantage, panic, binaire illisible ou absent après un démontage du
  stockage) : depuis la v5.8.0, le navigateur et le pane des cartes tournent dans une boucle qui
  les relance en 2 s ; la cause s'affiche au redémarrage (`⚠ relancé après arrêt (2) (SIGBUS : …)`)
  et le détail (code de sortie, début de stderr) reste dans `~/.cache/claude-launcher-ng2/nav.err.last`.
  Si le pane du navigateur a quand même été fermé, le pane des cartes le recrée sous 3 s (ou dès
  qu'une commande y est tapée). Une coque plus ancienne sans navigateur est recréée à la relance
  (`⚠ navigateur absent de la coque : coque recréée`), projets conservés. Jusqu'à la v5.7.2, la
  relance échouait sur `coque sans navigateur` : `tmux -L claude-ui-ng2 kill-server` puis relancer.
- **Stockage des projets démonté** (MooseFS, NFS… tombé) : la racine reste dans le navigateur,
  marquée `⚠ inaccessible`, et elle est scrutée toutes les 3 s ; au remontage, ses projets
  reviennent seuls (`F5` force la relecture). Jusqu'à la v5.7.2, une racine perdue n'était plus
  surveillée du tout : navigateur vide jusqu'à la relance du launcher.
  Si le binaire lui-même est sur ce stockage (mode portable), un démontage peut aussi faire tomber
  le navigateur : la relance recrée alors la coque (voir ci-dessus).
- **« mise à jour : /CLAUDE_PROJETS/claude-launcher-ng2 : open … no such file or directory »**
  (jusqu'à la v5.7.3) : le stockage du binaire a été démonté puis remonté pendant que le launcher
  tournait ; le système ne donnait plus que le chemin interne au partage. Depuis la v5.7.4, le
  chemin vu au démarrage sert de repli. Contournement : `Esc` `l`, puis relancer le launcher.
- **Une maquette ne s'ouvre pas dans Chrome** : `claude-launcher-ng2 backends` doit afficher
  `actif` pour le serveur web. `écoute impossible … address already in use` : un autre service tient
  le port, changez `browser_server.listen` (hub Linux et exe Windows du même poste : un port
  par système, `hubs.<machine>.windows.browser_server.listen`). Le serveur est lancé par le pane des cartes : relancez le
  hub après une mise à jour du binaire. 404 : le fichier n'est pas sous une racine connue, ajoutez son
  dossier à `browser_server.allow`. Sous WSL, si `localhost` ne répond pas depuis Windows, vérifiez
  que `localhostForwarding` n'est pas désactivé dans `.wslconfig`.
- **Essayer sans toucher à `claude-hub-ng2`** :
  `CL_HUB=claude-hub-test CL_OUTER_SOCKET=claude-ui-test claude-launcher-ng2`.
- **Les annonces de version n'apparaissent pas** : GitHub est interrogé sans jeton ; en cas de
  limite de débit ou hors ligne, le NG réessaie 10 minutes plus tard.
