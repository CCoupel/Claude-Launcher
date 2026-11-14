package core

import (
	"strconv"
	"strings"
)

// LiveSess : session tmux du conteneur d'un projet (#50) : propriétaire, nombre
// de panes (claude et ses teammates) et claude encore en marche dans le leader.
type LiveSess struct {
	Owner  OwnerInfo
	Panes  int  // fenêtre du projet et panes cachés (#136)
	Hidden int  // dont panes cachés, dans la réserve
	Claude bool // le leader (plus petit %id) n'est pas revenu à son shell
}

// ReadLive : session du conteneur name, si elle existe (une seule commande :
// propriétaire et panes, puis la liste des panes).
func (e Engine) ReadLive(name string) (LiveSess, bool) {
	s, err := e.Output(name, TmuxIn("display-message", "-p", "-t", "="+CtrSession+":", OwnerFormat+"|#{window_panes}", ";",
		"list-panes", "-a", "-F", "#{?#{@cl_home},H,#{?#{==:#{session_name},"+CtrSession+"},P,X}} #{pane_id} #{pane_current_command}")...)
	if err != nil {
		return LiveSess{}, false
	}
	return ParseLive(s)
}

func ParseLive(s string) (LiveSess, bool) {
	head, panes, _ := strings.Cut(s, "\n")
	i := strings.LastIndex(head, "|")
	if i < 0 {
		return LiveSess{}, false
	}
	n, _ := strconv.Atoi(head[i+1:])
	l := LiveSess{Owner: ParseOwner(head[:i])}
	// lignes « P|H|X %id commande » (fenêtre du projet, réserve, autre) ; sans
	// marqueur : tous les panes de la fenêtre
	var win []string
	for _, ln := range strings.Split(panes, "\n") {
		switch {
		case strings.HasPrefix(ln, "H "):
			l.Hidden++
		case strings.HasPrefix(ln, "P "):
			win = append(win, ln[2:])
		case strings.HasPrefix(ln, "X "):
		default:
			win = append(win, ln)
		}
	}
	l.Panes = n + l.Hidden
	if id, cmd := LeaderOf(strings.Join(win, "\n")); id != "" {
		l.Claude = !Shells[cmd]
	}
	return l, true
}
