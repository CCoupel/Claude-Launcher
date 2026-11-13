# Adaptations projet — deploy PROD (Claude-Launcher)

La PROD est le binaire du hub, sur MooseFS : `/share/MFS/ROOT/CLAUDE_PROJETS/claude-launcher-ng2` (Linux)
et `claude-launcher-ng2.exe` (Windows), dans le même dossier.

1. Attendre la CI `release.yml` verte sur le tag, puis télécharger les binaires de la release GitHub
   (`gh release download vX.Y.Z`) et les vérifier contre `SHA256SUMS`.
2. Sauvegarder le binaire Linux en place : `claude-launcher-ng2.bak-v<version précédente>` (un seul `.bak` gardé).
3. Remplacer `claude-launcher-ng2` (Linux amd64, chmod +x) et `claude-launcher-ng2.exe`.
4. Les processus `mirror` / `layout-watch` gardent l'ancien binaire : `pkill -f 'claude-launcher-ng2 mirror'` après
   la mise à jour, puis demander à l'utilisateur de relancer le hub (`Esc` puis `l`).
5. Clôturer le milestone `vX.Y.Z` avec le tag ; proposer une maquette du site `gh-pages` si le site est en retard.
