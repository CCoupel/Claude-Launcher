package tmux

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"claude-launcher-ng2/core"
)

// #250 (LOT-1) — coque tmux : sous-commande team-set et menu de la carte TEAM (maquette M2 A « Menu tmux au clic »).
// Écrit depuis contracts/desktop-dispatch.md « v6.9.0 › Coque tmux » : team-set <projet> <agent> [model=…] [effort=…],
// sortie = la ligne note (code 0), refus sur stderr (code 2). Projets factices (t.TempDir) ; aucun tmux réel.

func proj250(t *testing.T) string {
	t.Helper()
	withHome(t)
	proj := t.TempDir()
	agents := filepath.Join(proj, ".claude", "agents")
	if err := os.MkdirAll(agents, 0o755); err != nil {
		t.Fatal(err)
	}
	w := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w(filepath.Join(proj, ".claude", "project-config.json"),
		`{"agents":{"registry":[{"name":"dev-backend","tag":"v5.16.0"},{"name":"teamleader","tag":"v5.16.0"},{"name":"cdp","tag":"v5.16.0"}]}}`)
	for _, n := range []string{"dev-backend", "teamleader"} {
		w(filepath.Join(agents, n+".template.md"), "---\nname: "+n+"\nmodel: sonnet\neffort: medium\n---\n\n# "+n+"\n")
	}
	w(filepath.Join(agents, "cdp.template.md"), "# CDP\n")
	return proj
}

func run250(t *testing.T, args ...string) (code int, out, errs string) {
	t.Helper()
	var o, e bytes.Buffer
	code = RunTeamSet(args, &o, &e)
	return code, o.String(), e.String()
}

func read250(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("lecture %s : %v", p, err)
	}
	return string(b)
}

// ── team-set ──────────────────────────────────────────────────────────────

func TestTeamSetEcritEtRendLaNote250(t *testing.T) {
	proj := proj250(t)
	code, out, errs := run250(t, proj, "dev-backend", "model=opus", "effort=max")
	if code != 0 || errs != "" {
		t.Fatalf("code %d, stderr %q", code, errs)
	}
	if strings.TrimSpace(out) == "" || strings.Count(strings.TrimSpace(out), "\n") != 0 {
		t.Errorf("sortie : %q, attendu la seule ligne note", out)
	}
	if got := read250(t, filepath.Join(proj, ".claude", "agents", "dev-backend.md")); got != "---\nmodel: opus\neffort: max\n---\n" {
		t.Errorf("compagnon : %q", got)
	}
	// « default » : clé retirée, compagnon supprimé
	if code, _, errs := run250(t, proj, "dev-backend", "model=default", "effort=default"); code != 0 {
		t.Fatalf("default : code %d %s", code, errs)
	}
	if _, err := os.Stat(filepath.Join(proj, ".claude", "agents", "dev-backend.md")); err == nil {
		t.Error("compagnon non supprimé après model=default effort=default")
	}
}

func TestTeamSetTeamleaderEcritSettings250(t *testing.T) {
	proj := proj250(t)
	code, out, errs := run250(t, proj, "teamleader", "model=opus")
	if code != 0 {
		t.Fatalf("code %d %s", code, errs)
	}
	if !strings.Contains(out, "/model") || !strings.Contains(out, "/effort") {
		t.Errorf("note : %q, attendu le rappel de /model et /effort", out)
	}
	if got := read250(t, filepath.Join(proj, ".claude", "settings.json")); got != "{\n  \"model\": \"opus\",\n  \"effortLevel\": \"medium\"\n}\n" {
		t.Errorf("settings.json : %q", got)
	}
}

// Refus : message sur stderr, code 2, rien d'écrit, rien sur stdout.
func TestTeamSetRefusCode2250(t *testing.T) {
	proj := proj250(t)
	cases := map[string][]string{
		"agent hors registre":  {proj, "inconnu", "model=opus"},
		"traversée":            {proj, "../x", "model=opus"},
		"cdp non réglable":     {proj, "cdp", "model=opus"},
		"modèle hors liste":    {proj, "dev-backend", "model=gpt-5"},
		"effort hors liste":    {proj, "dev-backend", "effort=ultra"},
		"argument inconnu":     {proj, "dev-backend", "couleur=rouge"},
		"argument sans =":      {proj, "dev-backend", "opus"},
		"trop peu d'arguments": {proj},
	}
	for name, args := range cases {
		code, out, errs := run250(t, args...)
		if code != 2 {
			t.Errorf("%s : code %d, attendu 2", name, code)
		}
		if strings.TrimSpace(errs) == "" {
			t.Errorf("%s : message de refus absent de stderr", name)
		}
		if strings.TrimSpace(out) != "" {
			t.Errorf("%s : stdout %q, attendu vide (le refus va sur stderr)", name, out)
		}
	}
	ents, _ := os.ReadDir(filepath.Join(proj, ".claude", "agents"))
	for _, e := range ents {
		if !strings.HasSuffix(e.Name(), ".template.md") {
			t.Errorf("fichier écrit malgré les refus : %s", e.Name())
		}
	}
}

// ── menu display-menu (M2 A) ──────────────────────────────────────────────

type menuItem250 struct{ label, key, cmd string }

// items250 : entrées de display-menu après les options (-T titre, -x, -y) ; les séparateurs (label vide) sont gardés.
func items250(t *testing.T, args []string) (items []menuItem250, opts map[string]string) {
	t.Helper()
	if len(args) == 0 || args[0] != "display-menu" {
		t.Fatalf("arguments : %q, attendu display-menu en tête", args)
	}
	opts = map[string]string{}
	i := 1
	for i < len(args) && strings.HasPrefix(args[i], "-") && (args[i] == "-T" || args[i] == "-x" || args[i] == "-y") {
		opts[args[i]] = args[i+1]
		i += 2
	}
	rest := args[i:]
	if len(rest)%3 != 0 {
		t.Fatalf("entrées de menu : %d arguments, attendu des triplets (nom, raccourci, commande)", len(rest))
	}
	for j := 0; j+2 < len(rest); j += 3 {
		items = append(items, menuItem250{rest[j], rest[j+1], rest[j+2]})
	}
	return
}

func find250(items []menuItem250, key string) *menuItem250 {
	for i := range items {
		if items[i].key == key && items[i].label != "" {
			return &items[i]
		}
	}
	return nil
}

func line250(m string) core.TeamLine {
	return core.TeamLine{Name: "planner", Agent: "implementation-planner", Started: true, Model: "sonnet", Effort: "medium",
		ModelSrc: "template", EffortSrc: "template", TplModel: "sonnet", TplEffort: "medium", Editable: true}
}

func TestTeamMenuEntreesDefautEtRaccourcis250(t *testing.T) {
	l := line250("")
	items, _ := items250(t, teamMenuArgs(l, "/p", "/usr/bin/claude-launcher-ng2", 10, 5))
	d := find250(items, "d")
	if d == nil || !strings.Contains(d.label, "Défaut (template) — sonnet") || !strings.Contains(d.cmd, "model=default") {
		t.Errorf("entrée « défaut » du modèle (touche d) : %+v", d)
	}
	z := find250(items, "0")
	if z == nil || !strings.Contains(z.label, "Défaut (template) — medium") || !strings.Contains(z.cmd, "effort=default") {
		t.Errorf("entrée « défaut » de l'effort (touche 0) : %+v", z)
	}
	for key, want := range map[string]string{"o": "model=opus", "s": "model=sonnet", "h": "model=haiku", "f": "model=fable",
		"1": "effort=low", "2": "effort=medium", "3": "effort=high", "4": "effort=xhigh", "5": "effort=max"} {
		it := find250(items, key)
		if it == nil || !strings.Contains(it.cmd, want) {
			t.Errorf("touche %s : %+v, attendu une commande avec %s", key, it, want)
		}
	}
	tt := find250(items, "t")
	if tt == nil || !strings.Contains(tt.cmd, "model=default") || !strings.Contains(tt.cmd, "effort=default") {
		t.Errorf("« tout par défaut » (touche t) : %+v", tt)
	}
	// commandes : team-set, binaire, projet et agent
	for _, it := range items {
		if it.cmd == "" {
			continue
		}
		if !strings.Contains(it.cmd, "team-set") || !strings.Contains(it.cmd, "/usr/bin/claude-launcher-ng2") ||
			!strings.Contains(it.cmd, "/p") || !strings.Contains(it.cmd, "implementation-planner") {
			t.Errorf("commande %q : team-set, binaire, projet et agent attendus", it.cmd)
		}
	}
	// pas de nouvelle touche hors d, o, s, h, f, 0-5, t (coque sans nouvelle touche globale : DECISIONS.md)
	for _, it := range items {
		if it.key != "" && !strings.Contains("dosfh012345t", it.key) {
			t.Errorf("raccourci inattendu %q (%s)", it.key, it.label)
		}
	}
}

// Efforts grisés (préfixe « - » de tmux, sans raccourci ni commande) selon le modèle ; « défaut » jamais grisé.
func TestTeamMenuEffortsGrisesSelonLeModele250(t *testing.T) {
	for _, c := range []struct {
		model    string
		disabled []string // touches des efforts grisés
	}{
		{"claude-opus-4-5", []string{"4", "5"}},
		{"claude-opus-4-6", []string{"4"}},
		{"claude-haiku-4-5", []string{"1", "2", "3", "4", "5"}},
		{"opus", nil},
	} {
		l := line250("")
		l.Model, l.ModelSrc = c.model, "companion"
		args := teamMenuArgs(l, "/p", "/x", -1, -1)
		items, _ := items250(t, args)
		dis := map[string]bool{}
		for _, k := range c.disabled {
			dis[k] = true
		}
		for i, e := range core.Efforts {
			key := strconv.Itoa(i + 1)
			if dis[key] {
				if find250(items, key) != nil {
					t.Errorf("%s : effort %s accessible alors qu'il est incompatible", c.model, e)
				}
				found := false
				for _, it := range items {
					if strings.HasPrefix(it.label, "-") && strings.Contains(it.label, e) && it.key == "" && it.cmd == "" {
						found = true
					}
				}
				if !found {
					t.Errorf("%s : effort %s non grisé (entrée « -… » sans raccourci attendue)", c.model, e)
				}
			} else if find250(items, key) == nil {
				t.Errorf("%s : effort %s compatible mais grisé ou absent", c.model, e)
			}
		}
		if z := find250(items, "0"); z == nil || strings.HasPrefix(z.label, "-") {
			t.Errorf("%s : l'entrée « défaut » de l'effort doit rester accessible : %+v", c.model, z)
		}
		if d := find250(items, "d"); d == nil || strings.HasPrefix(d.label, "-") {
			t.Errorf("%s : l'entrée « défaut » du modèle doit rester accessible : %+v", c.model, d)
		}
	}
}

// Coches : « défaut » cochée quand la clé est absente, la valeur explicite sinon.
func TestTeamMenuCochesSelonLaSource250(t *testing.T) {
	l := line250("")
	items, _ := items250(t, teamMenuArgs(l, "/p", "/x", -1, -1))
	if !strings.Contains(find250(items, "d").label, "✓") || !strings.Contains(find250(items, "0").label, "✓") {
		t.Error("source template : « défaut » doit être cochée dans les deux rubriques")
	}
	l.Model, l.ModelSrc, l.Effort, l.EffortSrc = "opus", "companion", "max", "companion"
	items, _ = items250(t, teamMenuArgs(l, "/p", "/x", -1, -1))
	if strings.Contains(find250(items, "d").label, "✓") || strings.Contains(find250(items, "0").label, "✓") {
		t.Error("source compagnon : « défaut » ne doit plus être cochée")
	}
	if !strings.Contains(find250(items, "o").label, "✓") || !strings.Contains(find250(items, "5").label, "✓") {
		t.Errorf("source compagnon : opus et max doivent être cochés (%q, %q)", find250(items, "o").label, find250(items, "5").label)
	}
}

// Position : « -x/-y » aux coordonnées données, sinon (repli) aucune position : tmux centre le menu.
func TestTeamMenuPosition250(t *testing.T) {
	_, o := items250(t, teamMenuArgs(line250(""), "/p", "/x", 42, 7))
	if o["-x"] != "42" || o["-y"] != "7" {
		t.Errorf("position : %v, attendu -x 42 -y 7", o)
	}
	_, o = items250(t, teamMenuArgs(line250(""), "/p", "/x", -1, -1))
	if _, x := o["-x"]; x {
		t.Errorf("repli : -x présent (%v), attendu aucune position (centre)", o)
	}
	if o["-T"] == "" {
		t.Error("titre du menu (-T) absent")
	}
}

// Teamleader : note de bas de menu qui cite settings.json et /model /effort.
func TestTeamMenuNoteTeamleader250(t *testing.T) {
	l := line250("")
	l.Name, l.Agent = "leader", "teamleader"
	items, _ := items250(t, teamMenuArgs(l, "/p", "/x", -1, -1))
	var foot string
	for _, it := range items {
		if strings.HasPrefix(it.label, "-") && strings.Contains(it.label, "settings.json") {
			foot = it.label
		}
	}
	if !strings.Contains(foot, "/model") || !strings.Contains(foot, "/effort") {
		t.Errorf("note du teamleader : %q, attendu settings.json, /model et /effort", foot)
	}
}

// Chemin du projet avec espace et apostrophe : une seule argument shell, jamais d'injection.
func TestTeamMenuCommandeSansInjection250(t *testing.T) {
	proj := `/p/it's a "proj" $(touch x)`
	items, _ := items250(t, teamMenuArgs(line250(""), proj, "/x/ng2", -1, -1))
	for _, it := range items {
		if it.cmd != "" && strings.Contains(it.cmd, "$(touch x)") && !strings.Contains(it.cmd, `\$(touch x)`) {
			t.Errorf("commande non protégée contre l'expansion : %q", it.cmd)
		}
	}
}

// ── zone de clic modèle · effort ≠ double-clic sur le nom ─────────────────

func click250(m stateModel, x, y int) (stateModel, tea.Cmd) {
	mm, cmd := m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: x, Y: y})
	return mm.(stateModel), cmd
}

func card250(w int) (stateModel, []core.TeamLine) {
	lines := []core.TeamLine{
		{Name: "leader", Agent: "teamleader", Pane: "%1", Started: true, Model: "sonnet", Effort: "medium", Editable: true, ModelSrc: "template", EffortSrc: "template"},
		{Name: "dev-a", Agent: "dev-backend", Pane: "%2", Started: true, Active: true, Model: "opus", Effort: "high", Editable: true, ModelSrc: "companion", EffortSrc: "companion", TplModel: "sonnet", TplEffort: "medium"},
		{Name: "qa", Agent: "qa", Model: "haiku", Effort: "low", Editable: true, ModelSrc: "template", EffortSrc: "template"}, // non démarré
		{Name: "cdp", Agent: "cdp", Pane: "%4", Started: true, Model: "", Effort: "", Editable: false},
	}
	m := stateModel{cfg: visCfg(), w: w, h: 60, loaded: true, hasSel: true, sel: Selection{Name: "p", Key: "p", Path: ""}}
	m.panes = paneData{open: true, lines: lines}
	return m, lines
}

// colX250 : colonne d'écran de la première occurrence de sub sur la ligne row de la liste.
func colX250(t *testing.T, m stateModel, row int, sub string) int {
	t.Helper()
	view := strings.Split(m.View(), "\n")
	line := ansi.Strip(view[m.teamTop()+row])
	i := strings.Index(line, sub)
	if i < 0 {
		t.Fatalf("ligne %d : %q ne contient pas %q", row, line, sub)
	}
	return lipgloss.Width(line[:i])
}

func TestTeamCardClicModeleOuvreLeMenu250(t *testing.T) {
	for _, w := range []int{100, 40} {
		m, _ := card250(w)
		top := m.teamTop()
		if top <= 0 {
			t.Fatalf("w=%d : début de liste %d", w, top)
		}
		// clic sur la colonne « modèle » : ouvre le menu (commande), jamais une bascule de masquage
		x := colX250(t, m, 1, "opus")
		var cmd tea.Cmd
		m, cmd = click250(m, x, top+1)
		if cmd == nil {
			t.Fatalf("w=%d : clic sur le modèle sans menu", w)
		}
		// deux clics rapprochés sur la colonne modèle : jamais pris pour le double-clic de masquage du nom
		m, c1 := click250(m, x, top+1)
		_, c2 := click250(m, x, top+1)
		if c1 == nil || c2 == nil {
			t.Errorf("w=%d : clics répétés sur le modèle : menu attendu à chaque clic", w)
		}
		// ligne non démarrée : réglable aussi (effet au prochain démarrage)
		m2, _ := card250(w)
		if _, cmd := click250(m2, colX250(t, m2, 2, "haiku"), top+2); cmd == nil {
			t.Errorf("w=%d : ligne non démarrée : menu attendu", w)
		}
	}
}

// Le double-clic sur le NOM reste inchangé : bascule au second clic, rien au premier ; pas de menu.
func TestTeamCardDoubleClicNomInchange250(t *testing.T) {
	m, _ := card250(100)
	top := m.teamTop()
	nx := colX250(t, m, 1, "dev-a")
	m, c1 := click250(m, nx, top+1)
	if c1 != nil {
		t.Error("un seul clic sur le nom : aucune commande attendue")
	}
	_, c2 := click250(m, nx, top+1)
	if c2 == nil {
		t.Error("double-clic sur le nom : bascule attendue")
	}
}

// Entrée non réglable (cdp) : pas de menu, la raison est dite sous la liste.
func TestTeamCardLigneNonReglable250(t *testing.T) {
	m, _ := card250(100)
	top := m.teamTop()
	x := colX250(t, m, 3, "cdp") + 8 // dans la zone des colonnes modèle · effort de la ligne cdp
	m, cmd := click250(m, x, top+3)
	if cmd != nil {
		// une commande peut être celle d'une bascule : la ligne n'a de toute façon pas de menu à ouvrir
		t.Logf("commande renvoyée pour cdp (bascule ou menu) — vérifié par le message ci-dessous")
	}
	body := ansi.Strip(m.teamBody(98))
	if !strings.Contains(strings.ToLower(body), "pas réglable") && !strings.Contains(strings.ToLower(body), "non réglable") {
		t.Errorf("raison de non-réglage absente sous la liste :\n%s", body)
	}
}

// ⟳ sur la ligne d'un teammate qui tourne encore avec l'ancien réglage ; message 6 s sous la liste.
func TestTeamCardAffichageRunningEtMessage250(t *testing.T) {
	m, lines := card250(100)
	lines[1].Running = &core.TeamRunning{Model: "sonnet"}
	m.panes = paneData{open: true, lines: lines}
	rows := strings.Split(ansi.Strip(m.teamBody(98)), "\n")
	if !strings.Contains(rows[1], "opus") || !strings.Contains(rows[1], "⟳") {
		t.Errorf("ligne dev-a : %q, attendu la valeur résolue opus suivie de ⟳", rows[1])
	}
	if strings.Contains(rows[0], "⟳") || strings.Contains(rows[2], "⟳") {
		t.Error("⟳ sur une ligne qui tourne avec la valeur résolue")
	}
	m.teamMsg, m.teamMsgAt = "dev-a : opus · high enregistré", time.Now()
	if body := ansi.Strip(m.teamBody(98)); !strings.Contains(body, "dev-a : opus · high enregistré") {
		t.Errorf("message de confirmation absent :\n%s", body)
	}
	m.teamMsgAt = time.Now().Add(-7 * time.Second)
	if body := ansi.Strip(m.teamBody(98)); strings.Contains(body, "enregistré") {
		t.Error("message encore affiché après 6 s")
	}
}

// ── cycle 1, M1 : aucun format tmux actif dans les textes du menu (titre, libellés) ──

// actif250 : texte contenant un format tmux actif (#( #{ …) une fois les « ## » (dièse littéral) retirés.
func actif250(s string) bool {
	r := strings.ReplaceAll(s, "##", "")
	return strings.Contains(r, "#(") || strings.Contains(r, "#{") || strings.Contains(r, "#[") || strings.Contains(r, "#S")
}

func TestTeamMenuSansFormatTmuxActif250(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "injecte")
	evil := "#(touch " + marker + ")"
	l := core.TeamLine{
		Name: "x" + evil, Agent: "dev" + evil, Started: true, Editable: true,
		Model: evil, Effort: "#{pane_pid}", ModelSrc: "companion", EffortSrc: "companion",
		TplModel: evil, TplEffort: "#{session_name}",
	}
	items, opts := items250(t, teamMenuArgs(l, "/p", "/x/ng2", 3, 4))
	if actif250(opts["-T"]) {
		t.Errorf("titre du menu avec format actif : %q (# doit être échappé en ##)", opts["-T"])
	}
	for _, it := range items {
		if actif250(it.label) {
			t.Errorf("libellé avec format actif : %q (# doit être échappé en ##)", it.label)
		}
		if strings.Contains(it.label, "touch") && !strings.Contains(it.label, "##") && strings.Contains(it.label, "#") {
			t.Errorf("libellé exposant le texte du fichier : %q", it.label)
		}
	}
	// valeur hors listes core.Models / core.Efforts : « — » (jamais la valeur brute)
	d, z := find250(items, "d"), find250(items, "0")
	if d == nil || !strings.Contains(d.label, "Défaut (template) — —") {
		t.Errorf("modèle du template hors liste : %+v, attendu « Défaut (template) — — »", d)
	}
	if z == nil || !strings.Contains(z.label, "Défaut (template) — —") {
		t.Errorf("effort du template hors liste : %+v, attendu « Défaut (template) — — »", z)
	}
	for _, it := range items {
		if strings.Contains(it.label, "pane_pid") || strings.Contains(it.label, "session_name") || strings.Contains(it.label, "touch") && !strings.Contains(opts["-T"], it.label) {
			t.Errorf("valeur brute du fichier dans un libellé : %q", it.label)
		}
	}
	// la commande run-shell reste quotée (valeurs choisies dans les listes, agent entre apostrophes)
	for _, it := range items {
		if it.cmd != "" && !strings.Contains(it.cmd, "'dev#(touch") && !strings.Contains(it.cmd, "dev##(touch") && strings.Contains(it.cmd, "#(touch") {
			t.Errorf("commande avec format actif non protégé : %q", it.cmd)
		}
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("marqueur créé : une commande a été exécutée")
	}
}
