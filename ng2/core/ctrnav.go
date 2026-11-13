package core

import (
	"strconv"
	"strings"
)

// LiveSess : session tmux du conteneur d'un projet (#50) : propriétaire, nombre
// de panes (claude et ses teammates) et claude encore en marche dans le leader.
type LiveSess struct {
	Owner  OwnerInfo
	Panes  int
	Claude bool // le leader (plus petit %id) n'est pas revenu à son shell
}

// ReadLive : session du conteneur name, si elle existe (une seule commande :
// propriétaire et panes, puis la liste des panes).
func (e Engine) ReadLive(name string) (LiveSess, bool) {
	s, err := e.Output(name, TmuxIn("display-message", "-p", "-t", "="+CtrSession+":", OwnerFormat+"|#{window_panes}", ";",
		"list-panes", "-s", "-t", "="+CtrSession, "-F", "#{pane_id} #{pane_current_command}")...)
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
	l := LiveSess{Owner: ParseOwner(head[:i]), Panes: n}
	if id, cmd := LeaderOf(panes); id != "" {
		l.Claude = !Shells[cmd]
	}
	return l, true
}
