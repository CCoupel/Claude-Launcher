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
