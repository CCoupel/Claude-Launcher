# Index des tests

> Tests de specification ecrits par le test-writer, ranges en lots `<famille>/<theme>/<lot>/` ; statuts tenus par le CDP.
> Le chemin porte l'identite ; cet index ne porte que l'etat : une ligne par lot (chemin termine par `/`),
> plus des lignes fichier pour les exceptions (quarantaine). Lot sans ligne = `feature`.
> Convention : `context/COMMON.md` section 15. Statuts : `feature` | `regression` | `quarantaine`.
> Tags : `smoke`, `critical`, `slow`.

| Chemin | Niveau | Composant | Feature | Statut | Tags |
|--------|--------|-----------|---------|--------|------|
| tests/procedures/ui-desktop/taille-caracteres/ | procedure | ui-desktop | Taille des caracteres des terminaux, Ctrl+molette (#140) | feature | |
| tests/procedures/ui-desktop/panes-caches/ | procedure | ui-desktop | Teammates inactifs caches, carte TEAM (#136) | feature | critical |
| tests/integration/ui-tmux/panes-caches/ | integration | ui-tmux | Technique des panes caches : break-pane/join-pane, %id conserves, reserve (#136) | feature | critical |
| tests/procedures/ui-desktop/config-backends/ | procedure | ui-desktop | Reglages : Backends, Pools, Defauts (#118) | feature | |
| tests/procedures/ui-desktop/reglages-projet/ | procedure | ui-desktop | Menu contextuel et reglages d'un projet (#148) | feature | |
| tests/procedures/ui-desktop/config-poste-launcher/ | procedure | ui-desktop | Reglages : Ce poste, Launcher, Conteneurs (#149) | feature | |
| tests/procedures/ui-desktop/copier-coller/ | procedure | ui-desktop | Selection, copier / coller dans les terminaux (#155) | feature | |
| tests/procedures/ui-desktop/taille-zones-150/ | procedure | ui-desktop | Taille des caracteres par zone : arbre, cartes, terminaux (#150) | feature | |
| tests/procedures/ui-desktop/rafraichir-forcer-158/ | procedure | ui-desktop | Rafraichir un projet, Forcer la fermeture : menu, bandeau, pod supprime (#158) | feature | critical |
| tests/procedures/ui-tmux/rafraichir-forcer-158/ | procedure | ui-tmux | Coque Linux : F5 relit le projet, Suppr propose la fermeture forcee (#158) | feature | |
| tests/procedures/ui-desktop/team-lisible-151-152-162/ | procedure | ui-desktop | Carte TEAM (versions en en-tete), en-tetes de panes par etat, onglet Teamleaders isole (#151, #152, #162) | feature | |
| tests/procedures/ui-tmux/team-lisible-151-152/ | procedure | ui-tmux | Coque Linux : carte TEAM, ligne de titre par pane, deux launchers (#151, #152) | feature | |
| tests/procedures/ui-desktop/interface-web-163/ | procedure | ui-desktop | Interface web servie par la fenetre native : reglage a chaud, jeton, hors boucle locale, presse-papiers (#163) | feature | critical |
| tests/procedures/ui-desktop/organisations-153/ | procedure | ui-desktop | Organisations de la fenetre : leader a gauche, exception par projet, panes caches (#153) | feature | |
| tests/procedures/ui-tmux/organisations-153/ | procedure | ui-tmux | Coque Linux : leader a gauche, PROJECT_LAYOUTS, Ctrl+B R (#153) | feature | |
| tests/procedures/ci/ci-60s-179/ | procedure | main | CI sous 60 s : trois passes de scripts/ci.sh, nombre de tests et couverture (#179) | feature | slow |
| tests/procedures/ui-desktop/serveurs-web-183/ | procedure | ui-desktop | Reglages : deux serveurs web separes, commutateur de l'interface web (#183) | feature | |
