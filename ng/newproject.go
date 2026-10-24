package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Création d'un projet, comme l'entrée __new__ du menu bash. Deux façons de la
// lancer :
//   - double-clic sur une catégorie (titre de groupe : EMBY, INFRA…) ou sur une
//     racine : le projet est créé dans ce dossier, le formulaire ne demande que
//     le nom et le remote ;
//   - l'entrée « ✦ Nouveau projet » : dans une racine (choix si plusieurs).
// Le dossier est créé, `git init` fait, le remote `origin` ajouté (facultatif),
// puis le projet est ouvert.

type formMode int

const (
	formNone formMode = iota
	formRoot
	formName
	formRemote
)

type newForm struct {
	mode   formMode
	root   int
	base   string // dossier fixé (catégorie ou racine) ; vide tant que la racine n'est pas choisie
	name   string
	remote string
	err    string
}

// withSpecial place l'entrée « Nouveau projet » en tête de la liste.
func withSpecial(nodes []Node) []Node {
	return append([]Node{{Name: "✦ Nouveau projet", Special: "new"}}, nodes...)
}

// startNewProject : base est le dossier choisi par double-clic sur une catégorie
// ou une racine ; vide pour l'entrée « Nouveau projet ».
func (m model) startNewProject(base string) model {
	m.form = newForm{mode: formName, base: base}
	if base == "" {
		if len(m.cfg.Roots) > 1 {
			m.form.mode = formRoot
		} else {
			m.form.base = filepath.Clean(m.cfg.Roots[0])
		}
	}
	return m
}

func (m model) onFormKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := &m.form
	switch k.String() {
	case "esc", "ctrl+c":
		m.form = newForm{}
	case "up":
		if f.mode == formRoot {
			f.root = max(0, f.root-1)
		}
	case "down":
		if f.mode == formRoot {
			f.root = min(len(m.cfg.Roots)-1, f.root+1)
		}
	case "enter":
		f.err = ""
		switch f.mode {
		case formRoot:
			f.base = filepath.Clean(m.cfg.Roots[f.root])
			f.mode = formName
		case formName:
			if err := validName(f.name); err != "" {
				f.err = err
			} else if _, err := os.Stat(m.projectDir()); err == nil {
				f.err = "ce dossier existe déjà"
			} else {
				f.mode = formRemote
			}
		case formRemote:
			return m.createProject(), nil
		}
	case "backspace":
		switch f.mode {
		case formName:
			f.name = dropLast(f.name)
		case formRemote:
			f.remote = dropLast(f.remote)
		}
	default:
		if k.Type != tea.KeyRunes {
			break
		}
		s := string(k.Runes)
		switch f.mode {
		case formRoot: // un chiffre choisit la racine
			if len(s) == 1 && s[0] >= '1' && int(s[0]-'1') < len(m.cfg.Roots) {
				f.root = int(s[0] - '1')
			}
		case formName:
			f.name += s
		case formRemote:
			f.remote += s
		}
	}
	return m, nil
}

func dropLast(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return string(r[:len(r)-1])
}

func validName(s string) string {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		return "nom requis"
	case strings.ContainsAny(s, `/\`) || strings.HasPrefix(s, "."):
		return "nom invalide (ni / ni . au début)"
	}
	return ""
}

func (m model) projectDir() string {
	return filepath.Join(m.form.base, strings.TrimSpace(m.form.name))
}

// where : « dans EMBY » — le dossier fixé, relatif à sa racine.
func (m model) where() string {
	base := m.form.base
	for _, r := range m.cfg.Roots {
		root := filepath.Clean(r)
		if rel, err := filepath.Rel(root, base); err == nil && !strings.HasPrefix(rel, "..") {
			if rel == "." {
				return filepath.Base(root)
			}
			return rel
		}
	}
	return base
}

func (m model) createProject() model {
	dir := m.projectDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		m.form.err = err.Error()
		return m
	}
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		m.form.err = "git init : " + strings.TrimSpace(string(out))
		return m
	}
	if r := strings.TrimSpace(m.form.remote); r != "" {
		if out, err := exec.Command("git", "-C", dir, "remote", "add", "origin", r).CombinedOutput(); err != nil {
			m.form.err = "git remote : " + strings.TrimSpace(string(out))
			return m
		}
	}
	m.form = newForm{}
	m.filter = ""
	m.nodes = withSpecial(scanTree(m.cfg.Roots))
	if m.mgr != nil {
		m.mgr.SetProjects(m.projectPaths())
	}
	m.refilter()
	for i, idx := range m.visible {
		if m.nodes[idx].Path == dir {
			m.cursor = i
		}
	}
	m = m.openSelected() // comme le bash : le projet créé est lancé aussitôt
	if m.msg == "" {
		m.msg = "✓ projet créé"
	}
	return m
}

func (m model) renderForm(w int) string {
	f := m.form
	var sb strings.Builder
	sb.WriteString(" " + cyan.Render("✦ Nouveau projet") + "\n\n")
	if f.mode == formRoot {
		sb.WriteString(" " + dim.Render("Dossier racine :") + "\n")
		for i, r := range m.cfg.Roots {
			mark, style := "  ", dim
			if i == f.root {
				mark, style = green.Render("▶ "), bold
			}
			sb.WriteString("  " + mark + style.Render(fmt.Sprintf("%d %s", i+1, shorten(r, w-9))) + "\n")
		}
	} else {
		sb.WriteString(" " + dim.Render("dans ") + cyan.Render(shorten(m.where(), w-8)) + "\n\n")
	}
	cur := dim.Render("▏")
	if f.mode >= formName {
		c := ""
		if f.mode == formName {
			c = cur
		}
		sb.WriteString(" " + dim.Render("Nom :") + " " + f.name + c + "\n")
	}
	if f.mode >= formRemote {
		sb.WriteString(" " + dim.Render("Remote git (facultatif) :") + "\n")
		sb.WriteString("  " + f.remote + cur + "\n")
	}
	if f.err != "" {
		sb.WriteString("\n " + red.Render("✗ "+f.err) + "\n")
	}
	hint := "↵ valider · esc annuler"
	if f.mode == formRoot {
		hint = "↑↓ ou 1-9 · ↵ valider · esc annuler"
	}
	sb.WriteString("\n " + dim.Render(hint))
	return truncLines(sb.String(), w)
}
