package main

import (
	"os/exec"
	"strings"
)

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

// projectOf : cible des panes de la fenêtre hubWin du hub. Une fenêtre de projet
// en conteneur porte les options @cl_backend et @cl_ctr (posées par
// launchProject) : ses panes sont alors dans le tmux du conteneur, piloté par son
// canal de contrôle ; sinon ils sont dans la fenêtre du hub elle-même.
func projectOf(hubWin string) projWin {
	direct := projWin{t: hub, win: hubWin, hubWin: hubWin}
	// display-message répond pour une autre fenêtre si hubWin a disparu : on vérifie l'id
	s, err := tout(hub, "display-message", "-p", "-t", hubWin, "#{window_id}|#{@cl_backend}|#{@cl_ctr}")
	if err != nil {
		return direct
	}
	f := strings.SplitN(s, "|", 3)
	if len(f) != 3 || strings.HasPrefix(hubWin, "@") && f[0] != hubWin {
		return direct
	}
	be, ctr := f[1], f[2]
	if be == "" || ctr == "" {
		return direct
	}
	b, ok := findBackend(be)
	if !ok {
		return direct
	}
	e, ok := engineOf(b)
	if !ok {
		return direct
	}
	return projWin{t: ctlFor(e, ctr), win: "=" + ctrSession + ":", hubWin: hubWin}
}

// v4Server : serveur tmux par défaut, celui du launcher v4 (claude-hub). NG2 n'y
// écrit jamais ; il le lit pour ne pas croire mortes les teams d'une session v4.
type v4Server struct{}

func (v4Server) Run(args ...string) error { return v4Server{}.cmd(args...).Run() }
func (v4Server) Output(args ...string) ([]byte, error) {
	return v4Server{}.cmd(args...).Output()
}
func (v4Server) cmd(args ...string) *exec.Cmd {
	c := exec.Command("tmux", args...)
	c.Env = envWithout("TMUX", "TMUX_PANE")
	return c
}

// targets : tmux de l'hôte portant des panes de teams (nettoyage des teams) :
// le hub NG2 et le serveur par défaut (v4). Les teams des conteneurs ne sont
// jamais jugées ici (pidSpace étranger, cf. teams.go).
func targets() []Target { return []Target{hub, v4Server{}} }

// tout : Output d'une cible, espaces de fin retirés.
func tout(t Target, args ...string) (string, error) {
	b, err := t.Output(args...)
	return strings.TrimSpace(string(b)), err
}
