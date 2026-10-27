package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Aide (F1) : commandes et raccourcis. Dans la coque, affichée dans un
// display-popup tmux par-dessus tout le terminal (le navigateur est trop étroit
// pour un tableau) ; « claude-launcher-ng help » l'imprime dans le terminal.

type helpRow struct{ key, desc string }

type helpSection struct {
	title string
	rows  []helpRow
}

var helpSections = []helpSection{
	{"Navigateur", []helpRow{
		{"↑ ↓ PgUp PgDn, molette, clic", "sélectionner un projet (clic sur un projet ouvert : sa fenêtre)"},
		{"Entrée, double-clic", "ouvrir / rejoindre le projet"},
		{"frappe", "filtrer ; Esc efface le filtre"},
		{"clic hors projet", "onglet ⌗ Teamleaders"},
		{"double-clic catégorie", "créer un projet dans ce dossier"},
		{"Tab, Alt+← Alt+→", "passer du navigateur à tmux et retour"},
		{"Ctrl+← Ctrl+→, bordure", "largeur du navigateur (mémorisée)"},
		{"Ctrl+R", "rescanner les projets"},
		{"F5", "mises à jour et consommation maintenant"},
		{"Ctrl+U, clic sur ✕", "masquer les annonces de mise à jour"},
		{"clic sur l'annonce verte", "installer la nouvelle version (projets conservés)"},
		{"clic sur l'alerte connexion", "fenêtre login qui lance claude /login"},
		{"Esc", "quitter : l launcher seul · t puis o tout fermer"},
		{"F1", "cette aide"},
	}},
	{"Zone tmux", []helpRow{
		{"double-clic sur un pane", "plein écran, double-clic à nouveau pour revenir"},
		{"Ctrl+B z", "idem au clavier"},
		{"Ctrl+B R", "remettre en ordre les panes de la team"},
	}},
	{"Commandes", []helpRow{
		{"claude-launcher-ng", "lancer le hub (mise à jour auto au passage)"},
		{"claude-launcher-ng --no-update", "sans mise à jour (ou CL_NO_UPDATE=1)"},
		{"claude-launcher-ng configure", "créer / éditer la config ($EDITOR)"},
		{"claude-launcher-ng relayout WIN_ID", "remettre en ordre les panes d'une fenêtre"},
		{"claude-launcher-ng help", "cette aide dans le terminal"},
		{"claude-launcher-ng version", "version installée"},
		{"~/claude-launcher.sh", "ancienne commande : redirige vers le NG"},
	}},
	{"Fichiers", []helpRow{
		{"~/.config/claude-launcher.conf", "configuration (GITHUB_DIRS, CLAUDE_OPTIONS…)"},
	}},
}

// helpText : l'aide mise en forme, sur width colonnes au plus.
func helpText(width int) string {
	keyW := 0
	for _, s := range helpSections {
		for _, r := range s.rows {
			keyW = max(keyW, lipgloss.Width(r.key))
		}
	}
	keyW = min(keyW, width/2)
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	key := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	var b strings.Builder
	b.WriteString(bold.Render(ngName+" "+ngVersion) + dim.Render("  ·  aide") + "\n")
	for _, s := range helpSections {
		b.WriteString("\n" + title.Render(s.title) + "\n")
		for _, r := range s.rows {
			k := r.key
			if lipgloss.Width(k) > keyW {
				// clé trop longue : description sur la ligne suivante
				b.WriteString("  " + key.Render(k) + "\n")
				for _, l := range wrapWords(r.desc, max(width-keyW-4, 20)) {
					b.WriteString("  " + strings.Repeat(" ", keyW+2) + l + "\n")
				}
				continue
			}
			for i, l := range wrapWords(r.desc, max(width-keyW-4, 20)) {
				if i > 0 {
					k = ""
				}
				b.WriteString("  " + key.Render(padRight(k, keyW)) + "  " + l + "\n")
			}
		}
	}
	return b.String()
}

// wrapWords coupe s en lignes d'au plus w colonnes, entre les mots.
func wrapWords(s string, w int) []string {
	var lines []string
	cur := ""
	for _, word := range strings.Fields(s) {
		switch {
		case cur == "":
			cur = word
		case lipgloss.Width(cur)+1+lipgloss.Width(word) <= w:
			cur += " " + word
		default:
			lines = append(lines, cur)
			cur = word
		}
	}
	return append(lines, cur)
}

// runHelp imprime l'aide ; avec wait (popup), attend une touche avant de rendre la main.
func runHelp(wait bool) error {
	w := 100
	if s, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && s > 0 {
		w = s
	}
	fmt.Print(helpText(w))
	if !wait {
		return nil
	}
	fmt.Print("\n" + dim.Render("  une touche pour fermer"))
	if restore, err := rawMode(int(os.Stdin.Fd())); err == nil {
		defer restore()
	}
	buf := make([]byte, 16)
	_, _ = os.Stdin.Read(buf)
	return nil
}

// openHelp : popup d'aide sur le client attaché à la coque.
func openHelp() error {
	out, err := otmux("list-clients", "-t", outerSession, "-F", "#{client_name} #{client_width} #{client_height}").Output()
	f := strings.Fields(strings.SplitN(string(out), "\n", 2)[0])
	if err != nil || len(f) != 3 {
		return fmt.Errorf("aucun terminal attaché")
	}
	name := f[0]
	cw, _ := strconv.Atoi(f[1])
	ch, _ := strconv.Atoi(f[2])
	w := min(100, max(cw, 20))
	tw := w - 2 // intérieur du popup (bordure)
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// aide + ligne vide + « une touche pour fermer » + bordures
	h := min(strings.Count(helpText(tw), "\n")+4, max(ch, 5))
	cmd := exec.Command("tmux", "-L", outerSocket(), "display-popup", "-c", name, "-E",
		"-w", strconv.Itoa(w), "-h", strconv.Itoa(h), "-T", " Aide · F1 ",
		"COLUMNS="+strconv.Itoa(tw)+" "+shq(strings.TrimSuffix(exe, " (deleted)"))+" help --wait")
	return cmd.Start() // le popup vit sa vie ; le navigateur reste réactif
}
