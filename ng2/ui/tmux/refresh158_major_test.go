package tmux

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"claude-launcher-ng2/core"
)

// #158 (LOT-2), revue de code — deux MAJEURS, côté coque Linux (voir ui/desktop/refresh158_major_test.go).

// isoler : tmux et caches de test jamais ceux de la session (la fermeture lancée par un test rouge
// n'a alors aucun serveur à toucher).
func isolerCoque(t *testing.T) {
	t.Helper()
	withHome(t)
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	if d, err := os.MkdirTemp("", "cltmux"); err == nil {
		t.Setenv("TMUX_TMPDIR", d)
		t.Cleanup(func() { _ = os.RemoveAll(d) })
	}
	t.Setenv("TMUX", "")
}

// drain : attend la fin d'une fermeture éventuellement démarrée (avant le nettoyage des dossiers).
func drain(m model) {
	if m.closeCh == nil {
		return
	}
	end := time.After(5 * time.Second)
	for {
		select {
		case c := <-m.closeCh:
			if c.done {
				return
			}
		case <-end:
			return
		}
	}
}

func containerGoneModel() (model, core.Node) {
	m, n := navModel()
	m.windows[n.Key] = true
	m.ctr = ctrView{assign: map[string]string{n.Path: "lab"}, found: map[string]core.Found{}, live: map[string]core.LiveSess{},
		errs: map[string]string{}, diag: map[string][]core.Problem{}, down: map[string]string{}, env: map[string]bool{n.Path: true}}
	return m, n
}

// (1) Le projet revit entre la question et la confirmation (« o ») : refus, aucune fermeture démarrée,
// aucune marque de sortie propre.
func TestForceCloseConfirmRefusedWhenProjectRevives(t *testing.T) {
	isolerCoque(t)
	m, n := containerGoneModel()
	m, _ = press(m, tea.KeyDelete)
	if m.closeAsk == nil || !m.closeAsk.force {
		t.Fatalf("précondition : question de fermeture forcée attendue (msg=%q)", m.msg)
	}
	// la session du conteneur est recréée (HasLive) pendant que la question est affichée
	m.ctr.live[n.Path] = core.LiveSess{Owner: core.OwnerInfo{Id: core.LauncherID(), At: time.Now()}, Panes: 2, Claude: true}
	r, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	m2 := r.(model)
	t.Cleanup(func() { drain(m2) })
	if len(m2.closing) != 0 {
		t.Errorf("fermeture forcée démarrée sur un projet redevenu vivant : %v", m2.closing)
	}
	if _, err := os.Stat(core.CleanExitMarker(n.Path)); err == nil {
		t.Error("marque de sortie propre posée sur un projet vivant")
	}
	if m2.closeAsk != nil {
		t.Error("question toujours affichée après la réponse")
	}
	if !strings.Contains(strings.ToLower(m2.msg), "annul") && !strings.Contains(strings.ToLower(m2.msg), "vivant") && !strings.Contains(strings.ToLower(m2.msg), "revenu") {
		t.Errorf("pied = %q, attendu un message qui explique que la fermeture forcée est annulée", m2.msg)
	}
}

// (2) Lecture du backend non aboutie : Suppr ne pose pas la question de fermeture forcée, et F5 ne la
// recommande pas.
func TestForceCloseNotProposedWhenBackendReadFailed(t *testing.T) {
	cases := []struct {
		name string
		set  func(v *ctrView, path string)
	}{
		{"erreur de listing (errs)", func(v *ctrView, p string) { v.errs["lab"] = "docker : délai dépassé" }},
		{"serveur injoignable (down)", func(v *ctrView, p string) { v.down["lab"] = "srv1" }},
		{"backend inutilisable (diag)", func(v *ctrView, p string) { v.diag["lab"] = []core.Problem{{What: "daemon injoignable"}} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolerCoque(t)
			m, n := containerGoneModel()
			tc.set(&m.ctr, n.Path)
			m2, _ := press(m, tea.KeyDelete)
			if m2.closeAsk != nil && m2.closeAsk.force {
				t.Errorf("fermeture forcée proposée alors que la lecture du backend n'a pas abouti : %q", m2.closeAsk.question)
			}
			t.Cleanup(func() { drain(m2) })
			// F5 : le résultat de la relecture ne recommande pas non plus la fermeture forcée
			m3 := m.onRefreshed(refreshedMsg{node: n, ctr: m.ctr, has: false})
			if strings.Contains(m3.msg, "fermeture forcée") {
				t.Errorf("pied après F5 : %q, ne doit pas recommander la fermeture forcée", m3.msg)
			}
		})
	}
}
