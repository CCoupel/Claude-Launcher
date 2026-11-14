# Lot ui-tmux/panes-caches (#136)

Les tests Go de ce lot vivent dans le package `ng2/ui/tmux` (un test Go d'un package ne peut pas être rangé hors de lui) :
`ng2/ui/tmux/panehide_tmux_test.go` (tmux isolé, faux claude = `sleep`, jamais `claude-hub`).
Lancer : `cd ng2 && go test -run 'Hide|Hidden' ./ui/tmux`.
