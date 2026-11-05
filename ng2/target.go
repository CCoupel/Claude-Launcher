package main

import "strings"

// ── Cibles tmux ───────────────────────────────────────────────────────────
//
// Le launcher parle à deux sortes de tmux :
//   - le hub (serveur intérieur local, session claude-hub-ng2) : fenêtres et
//     onglets des projets, vignettes, bindings ;
//   - le tmux où vivent les panes d'un projet (leader + teammates).
//
// En exécution locale directe (seul mode aujourd'hui), les deux sont le même
// serveur et la fenêtre du projet est sa fenêtre du hub. Pour un projet en
// conteneur (#43), les panes vivent dans le tmux du conteneur, piloté par son
// canal de contrôle (tmux -C) : il suffira d'une seconde implémentation de
// Target et de la renvoyer depuis projectOf.

// Target : un serveur tmux auquel on envoie des commandes (même syntaxe que la
// ligne de commande tmux). Ne couvre pas ce qui demande un terminal (attach).
type Target interface {
	Run(args ...string) error
	Output(args ...string) ([]byte, error)
}

// directTarget : serveur intérieur local, via dtmux.
type directTarget struct{}

func (directTarget) Run(args ...string) error              { return dtmux(args...).Run() }
func (directTarget) Output(args ...string) ([]byte, error) { return dtmux(args...).Output() }

// hub : le serveur intérieur local.
var hub Target = directTarget{}

// projWin : où agir pour les panes d'un projet.
type projWin struct {
	t      Target // tmux qui porte les panes du projet
	win    string // fenêtre des panes, dans t
	hubWin string // fenêtre du projet dans le hub (onglet, messages)
}

// direct : panes dans le hub lui-même (exécution locale directe).
func (p projWin) direct() bool { return p.t == hub && p.win == p.hubWin }

// projectOf : cible des panes de la fenêtre hubWin du hub. Aujourd'hui toujours
// directe ; #43 y branchera les projets en conteneur.
func projectOf(hubWin string) projWin {
	return projWin{t: hub, win: hubWin, hubWin: hubWin}
}

// targets : tous les tmux portant des panes de projets (nettoyage des teams).
func targets() []Target { return []Target{hub} }

// tout : Output d'une cible, espaces de fin retirés.
func tout(t Target, args ...string) (string, error) {
	b, err := t.Output(args...)
	return strings.TrimSpace(string(b)), err
}
