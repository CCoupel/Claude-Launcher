package tmux

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"claude-launcher-ng2/core"
)

// #158 (LOT-2), équivalent de la coque Linux (plan 2.3, validé au GATE 2) :
//   - F5 relit AUSSI le projet sélectionné (conteneurs, sessions, fenêtres, git, diagnostic forcé),
//     avec « ⟳ … » puis le résultat en pied de navigateur ;
//   - Suppr sur un projet proposable pose la question de fermeture forcée (o/N).
// Pas de nouvelle touche.

func navModel() (model, core.Node) {
	n := core.Node{Name: "alpha", Path: "/x/alpha", Key: "alpha", Project: true}
	m := model{windows: map[string]bool{}, git: map[string]core.GitInfo{}, tpl: map[string]core.TemplateState{},
		nodes: []core.Node{n}, visible: []int{0}, launch: true}
	return m, n
}

func press(m model, k tea.KeyType) (model, tea.Cmd) {
	r, cmd := m.Update(tea.KeyMsg{Type: k})
	return r.(model), cmd
}

// F5 sur un projet sélectionné : message « ⟳ … » en pied et une commande de relecture du projet
// (à la place du seul « recherche des mises à jour… »).
func TestF5RereadsSelectedProject(t *testing.T) {
	withHome(t)
	m, _ := navModel()
	m2, cmd := press(m, tea.KeyF5)
	if !strings.Contains(m2.msg, "⟳") {
		t.Errorf("pied après F5 : %q, attendu « ⟳ … » (relecture du projet sélectionné)", m2.msg)
	}
	if !strings.Contains(m2.msg, "alpha") {
		t.Errorf("pied après F5 : %q, attendu le nom du projet relu", m2.msg)
	}
	if cmd == nil {
		t.Error("F5 : aucune commande de relecture")
	}
	// l'effet historique de F5 est conservé : recherche de mises à jour, annonces masquées réapparaissent
	m.hideLauncher, m.hideTpl = true, true
	m3, _ := press(m, tea.KeyF5)
	if m3.hideLauncher || m3.hideTpl {
		t.Error("F5 ne fait plus réapparaître les annonces masquées")
	}
	if !m3.checking {
		t.Error("F5 ne lance plus la recherche de mises à jour")
	}
}

// F5 sans projet sélectionné (liste vide) : reste valide, sans plantage.
func TestF5NoSelection(t *testing.T) {
	withHome(t)
	m := model{windows: map[string]bool{}, git: map[string]core.GitInfo{}, tpl: map[string]core.TemplateState{}, launch: true}
	m2, _ := press(m, tea.KeyF5)
	if m2.msg == "" {
		t.Error("F5 sans sélection : aucun message")
	}
}

// Suppr sur un projet affiché ouvert dont le conteneur (ou pod) n'existe plus : la question de
// fermeture forcée est posée (o/N), au lieu de la fermeture normale (/exit, attente de 30 s).
func TestDeleteProposesForceCloseWhenContainerGone(t *testing.T) {
	withHome(t)
	m, n := navModel()
	m.windows[n.Key] = true                                                                   // fenêtre du hub ouverte…
	m.ctr = ctrView{assign: map[string]string{n.Path: "lab"}, found: map[string]core.Found{}, // … conteneur affecté mais disparu
		live: map[string]core.LiveSess{}, errs: map[string]string{}}
	m2, _ := press(m, tea.KeyDelete)
	if m2.closeAsk == nil {
		t.Fatalf("Suppr : aucune question posée (msg=%q)", m2.msg)
	}
	q := strings.ToLower(m2.closeAsk.question)
	if !strings.Contains(q, "forc") || !strings.Contains(q, "o/n") {
		t.Errorf("question = %q, attendu une question de fermeture forcée (o/N)", m2.closeAsk.question)
	}
	if strings.Contains(q, "/exit") {
		t.Errorf("question = %q : la fermeture forcée n'envoie pas /exit", m2.closeAsk.question)
	}
}

// Suppr refusé pour un projet piloté par un autre launcher (inchangé, aucune fermeture forcée).
func TestDeleteNeverForcesWhenOtherLauncherAlive(t *testing.T) {
	withHome(t)
	m, n := navModel()
	m.windows[n.Key] = true
	m.ctr = ctrView{assign: map[string]string{n.Path: "lab"}, found: map[string]core.Found{},
		live: map[string]core.LiveSess{n.Path: {Owner: core.OwnerInfo{Id: "srv1:1:x", At: time.Now()}, Panes: 3, Claude: true}}}
	m2, _ := press(m, tea.KeyDelete)
	if m2.closeAsk != nil {
		t.Errorf("question posée pour un projet piloté ailleurs : %q", m2.closeAsk.question)
	}
	if !strings.Contains(m2.msg, "prendre la main") {
		t.Errorf("msg = %q, attendu « … prendre la main … »", m2.msg)
	}
}
