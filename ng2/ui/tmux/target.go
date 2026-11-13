package tmux

import (
	"os/exec"
	"strings"

	"claude-launcher-ng2/core"
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

// Target : un serveur tmux auquel on envoie des commandes (core.Target).
type Target = core.Target

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
	// conteneur : commande exécutée dans le conteneur du projet (nil en direct)
	ctrExec func(argv ...string) (string, error)
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
	s, err := tout(hub, "display-message", "-p", "-t", hubWin, "#{window_id}|#{@cl_backend}|#{@cl_ctr}|#{@cl_server}")
	if err != nil {
		return direct
	}
	f := strings.SplitN(s, "|", 4)
	if len(f) != 4 || strings.HasPrefix(hubWin, "@") && f[0] != hubWin {
		return direct
	}
	ctr := f[2]
	t, e, win, ok := core.CtrTarget(f[1], ctr, f[3]) // groupe de serveurs : celui du conteneur (#61)
	if !ok {
		return direct
	}
	return projWin{t: t, win: win, hubWin: hubWin,
		ctrExec: func(argv ...string) (string, error) { return e.Output(ctr, argv...) }}
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
	c.Env = core.EnvWithout("TMUX", "TMUX_PANE")
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
