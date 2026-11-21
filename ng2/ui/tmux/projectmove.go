package tmux

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"claude-launcher-ng2/core"
)

// ── Déplacer / renommer un projet depuis F2 (#216, H3 : entrée de F2, pas de nouvelle touche globale) ──

// moveEnv : environnement du déplacement pour la coque — « ouvert ici » = fenêtre tmux du projet.
func (m model) moveEnv() core.MoveEnv {
	return core.MoveEnv{
		Roots: m.cfg.Roots,
		OpenHere: func(path string) bool {
			for _, n := range m.nodes {
				if n.Project && n.Path == path {
					return m.windows[n.Key]
				}
			}
			return false
		},
	}
}

// startMove : écran de déplacement du projet path — blockers refusés d'emblée, déplacement inachevé : reprise ou annulation,
// sinon base → catégorie → nom (mêmes écrans que la création) puis confirmation.
func (m model) startMove(path, name string) model {
	info, err := core.MoveInfoFor(m.moveEnv(), path)
	if err != nil {
		m.form = newForm{}
		m.msg = "⚠ " + err.Error()
		return m
	}
	if info.Pending != nil {
		m.form = newForm{mode: formMovePending, moveFrom: path, moveInfo: &info, name: name}
		return m
	}
	if len(info.Blockers) > 0 {
		m.form.err = "déplacement impossible : " + strings.Join(info.Blockers, ", ")
		if m.form.mode == formNone {
			m.form = newForm{}
			m.msg = "⚠ " + m.form.err
		}
		return m
	}
	m.form = newForm{moveFrom: path, moveInfo: &info, name: info.Name, base: info.Base, category: info.Category, mode: formName}
	if len(m.cfg.Roots) > 1 {
		m.form.base, m.form.mode = "", formRoot
		return m
	}
	m.form.base = filepath.Clean(m.cfg.Roots[0])
	return m.enterCategory()
}

// moveTarget : destination saisie ; chemin contrôlé par la règle commune (#214), inexistant et différent de la source.
func (m model) moveTarget() (string, error) {
	f := m.form
	p, err := core.ResolveProjectPathIn(m.cfg.Roots, f.base, f.category, f.name)
	if err != nil {
		return "", err
	}
	if filepath.Clean(p) == filepath.Clean(f.moveFrom) {
		return "", fmt.Errorf("la destination est le projet lui-même")
	}
	if _, err := os.Lstat(p); err == nil {
		return "", fmt.Errorf("la destination existe déjà")
	}
	return p, nil
}

// onMoveKey : touches des écrans de confirmation et de reprise.
func (m model) onMoveKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := &m.form
	key := strings.ToLower(k.String())
	switch {
	case key == "esc" || key == "ctrl+c":
		m.form = newForm{}
	case f.mode == formMoveConfirm && (key == "o" || key == "y"):
		r := core.MoveProject(m.moveEnv(), f.moveFrom, core.MoveTarget{Base: f.base, Category: f.category, NewCategory: f.newCat, Name: f.name})
		return m.moveDone(r), nil
	case f.mode == formMovePending && key == "p":
		return m.moveDone(core.ResumeMove(m.moveEnv(), f.moveFrom)), nil
	case f.mode == formMovePending && key == "a":
		r := core.CancelMove(m.moveEnv(), f.moveFrom)
		if r.RolledBack {
			m.form = newForm{}
			m.msg = "↩ déplacement annulé : état initial rétabli"
			return m, nil
		}
		return m.moveDone(r), nil
	}
	return m, nil
}

// moveDone : suite d'un déplacement — succès : arbre relu et projet suivi ; échec : message, reprise proposée si partiel.
func (m model) moveDone(r core.MoveResult) model {
	from, name := m.form.moveFrom, m.form.name
	if r.OK {
		m.form = newForm{}
		m.filter = ""
		m.nodes = withSpecial(core.ScanTree(m.cfg.Roots))
		if m.mgr != nil {
			m.mgr.SetProjects(m.projectPaths())
		}
		m.refilter()
		for i, idx := range m.visible {
			if m.nodes[idx].Path == r.Path {
				m.cursor = i
			}
		}
		m.msg = "✓ projet déplacé : " + r.Path
		return m
	}
	if r.State == "partial" { // retour arrière en échec : l'entrée du journal reste — reprise ou annulation
		if info, err := core.MoveInfoFor(m.moveEnv(), from); err == nil {
			m.form = newForm{mode: formMovePending, moveFrom: from, moveInfo: &info, name: name, err: r.Msg}
			return m
		}
		if info, err := core.MoveInfoFor(m.moveEnv(), m.form.moveInfo.Base); err == nil {
			m.form.moveInfo = &info
		}
	}
	m.form.err = r.Msg
	return m
}

// renderMove : écrans de confirmation (effets listés) et de déplacement inachevé.
func (m model) renderMove(w int) string {
	f := m.form
	var sb strings.Builder
	line := func(label, text string) {
		sb.WriteString(" " + core.Dim.Render(label) + "\n")
		for _, l := range wrapText(core.SafeTerminalText(text), w-4) {
			sb.WriteString("   " + l + "\n")
		}
	}
	if f.mode == formMovePending {
		sb.WriteString(" " + core.Orange.Render("⚠ Déplacement inachevé : "+core.SafeTerminalText(f.name)) + "\n\n")
		if p := f.moveInfo.Pending; p != nil {
			line("de :", p.From)
			line("vers :", p.To)
			line("étapes faites :", strings.Join(p.Done, ", "))
			if p.Failed != "" {
				line("en échec :", p.Failed+" — "+p.Msg)
			}
		}
		if f.err != "" {
			sb.WriteString("\n " + core.Red.Render("✗ "+f.err) + "\n")
		}
		sb.WriteString("\n " + core.Dim.Render("p reprendre · a annuler (retour arrière) · Échap fermer"))
		return truncLines(sb.String(), w)
	}
	to, _ := m.moveTarget()
	sb.WriteString(" " + core.Cyan.Render("⇄ Déplacer / renommer "+core.SafeTerminalText(f.moveInfo.Name)) + "\n\n")
	line("de :", f.moveFrom)
	line("vers :", to)
	sb.WriteString("\n " + core.Dim.Render("effets :") + "\n")
	for _, e := range []string{
		"dossier du projet déplacé (copie contrôlée si autre système de fichiers)",
		"affectation (projects.json), couleur / organisation / masquage (config.conf) reportées",
		"historique des conversations de claude renommé",
		"approbation de l'environnement d'un dépôt cloné (trust.json) suit le projet",
		"conteneur / pod de l'ancienne clé supprimé : recréé à la prochaine ouverture",
	} {
		for i, l := range wrapWords(e, w-6) {
			p := "   • "
			if i > 0 {
				p = "     "
			}
			sb.WriteString(p + l + "\n")
		}
	}
	if f.err != "" {
		sb.WriteString("\n " + core.Red.Render("✗ "+f.err) + "\n")
	}
	sb.WriteString("\n " + core.Dim.Render("o confirmer · Échap annuler"))
	return truncLines(sb.String(), w)
}
