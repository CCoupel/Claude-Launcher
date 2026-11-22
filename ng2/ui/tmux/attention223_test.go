package tmux

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"claude-launcher-ng2/core"
)

// #223 (LOT-3b v6.7.0) — coque tmux : pastille « ❓ en attente » sur la ligne du projet, relevé des dossiers d'état,
// option de fenêtre @cl_attn lue par les vignettes. Contrat : contracts/attention.md § 8. Serveur tmux intérieur
// isolé (isoInner / visProject), état écrit comme le ferait le hook d'un faux claude ; jamais de vrai projet.

const (
	attnQuestion = `{"hook_event_name":"PreToolUse","tool_name":"AskUserQuestion","tool_input":{"questions":[{"question":"Valider le plan ?"}]}}`
	attnPerm     = `{"hook_event_name":"Notification","message":"Claude a besoin de votre permission pour utiliser Bash"}`
)

func writeAttn(t *testing.T, proj, pane, payload string) {
	t.Helper()
	d := core.AttentionDir(proj)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, pane+".json"), []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Pastille sur la ligne du projet : présente seulement pour un projet dont l'attente est connue.
func TestBadgeShowsWaitingTeamleader223(t *testing.T) {
	n := core.Node{Project: true, Name: "alpha", Key: "alpha", Path: "/p/alpha"}
	other := core.Node{Project: true, Name: "beta", Key: "beta", Path: "/p/beta"}
	m := model{windows: map[string]bool{}, attn: map[string]core.Attention{"/p/alpha": {Kind: "question", Text: "Valider ?", ID: "4-1"}}}
	if b := ansi.Strip(m.badges(n)); !strings.Contains(b, "❓ en attente") {
		t.Errorf("ligne du projet en attente : %q, attendu la pastille « ❓ en attente »", b)
	}
	if b := ansi.Strip(m.badges(other)); strings.Contains(b, "en attente") {
		t.Errorf("projet sans attente : pastille présente %q", b)
	}
	if b := ansi.Strip((model{windows: map[string]bool{}}).badges(n)); strings.Contains(b, "en attente") {
		t.Errorf("aucune attente connue : pastille présente %q", b)
	}
}

// Le relevé lit l'état d'un projet OUVERT, publie « kind|texte » dans @cl_attn de sa fenêtre, retire l'option à la réponse ;
// un projet fermé (sans session) n'est pas lu.
func TestAttentionCmdPublishesWindowOption223(t *testing.T) {
	marker := isoInner(t)
	key, win, _, proj, leader, _ := visProject(t, marker)
	lp := strings.TrimPrefix(leader, "%") // seul le pane leader de la fenêtre compte
	closed := t.TempDir()
	writeAttn(t, closed, "5", attnQuestion) // projet fermé : ignoré
	writeAttn(t, proj, lp, attnQuestion)

	m := model{session: hubSession, windows: map[string]bool{key: true}, attn: map[string]core.Attention{},
		nodes: []core.Node{{Project: true, Name: key, Key: key, Path: proj}, {Project: true, Name: "ferme", Key: "ferme", Path: closed}}}
	opt := func() string { return dt(t, "display-message", "-p", "-t", win, "#{@cl_attn}") }

	msg, _ := m.attentionCmd()().(attnMsg)
	if a, ok := msg[proj]; !ok || a.Kind != "question" || a.Text != "Valider le plan ?" {
		t.Fatalf("relevé = %+v, attendu la question du projet ouvert", msg)
	}
	if _, ok := msg[closed]; ok {
		t.Errorf("projet fermé lu : %+v", msg[closed])
	}
	if got := opt(); got != "question|Valider le plan ?" {
		t.Errorf("@cl_attn = %q, attendu « question|Valider le plan ? »", got)
	}
	// nouvelle attente (permission) : l'option suit
	time.Sleep(10 * time.Millisecond)
	m.attn = msg
	writeAttn(t, proj, lp, attnPerm)
	_ = os.Chtimes(filepath.Join(core.AttentionDir(proj), lp+".json"), time.Now().Add(time.Second), time.Now().Add(time.Second))
	msg, _ = m.attentionCmd()().(attnMsg)
	if got := opt(); !strings.HasPrefix(got, "permission|") || !strings.Contains(got, "permission pour utiliser Bash") {
		t.Errorf("@cl_attn après permission = %q", got)
	}
	// réponse : l'attente disparaît, l'option est retirée
	m.attn = msg
	_ = os.Remove(filepath.Join(core.AttentionDir(proj), lp+".json"))
	msg, _ = m.attentionCmd()().(attnMsg)
	if len(msg) != 0 {
		t.Errorf("attente encore relevée après la réponse : %+v", msg)
	}
	if got := opt(); got != "" {
		t.Errorf("@cl_attn = %q après la réponse, attendu vide", got)
	}
}

// Vignette : le titre porte « ❓ en attente » et la question, sans dépasser la largeur ; permission aussi ; rien hors attente.
func TestMirrorFrameAttentionKindsAndWidth223(t *testing.T) {
	frame := []string{"a", "b", "c"}
	for _, tc := range []struct{ attn, want string }{
		{"permission|Claude a besoin de votre permission pour utiliser Bash", "❓ permission : Claude a besoin"},
		{"question|Valider le plan ?", "❓ question : Valider le plan ?"},
	} {
		out := mirrorFrameAttn("proj", "17", tc.attn, 8, 40, false, 0, 0, frame)
		if !strings.Contains(out, "❓ en attente") || !strings.Contains(ansi.Strip(out), tc.want) {
			t.Errorf("%q : vignette sans « ❓ en attente » / %q", tc.attn, tc.want)
		}
		for _, l := range frameLines(out) {
			if w := ansi.StringWidth(l); w > 40 {
				t.Errorf("ligne de %d colonnes (> 40) : %q", w, l)
			}
		}
	}
	long := "question|" + strings.Repeat("très long ", 40)
	for _, l := range frameLines(mirrorFrameAttn("proj", "17", long, 8, 30, false, 0, 0, frame)) {
		if w := ansi.StringWidth(l); w > 30 {
			t.Errorf("question longue : ligne de %d colonnes (> 30)", w)
		}
	}
}

var rowStart = regexp.MustCompile(`\x1b\[\d+;1H`)

// frameLines : lignes d'une image de vignette (chaque ligne est positionnée par un déplacement de curseur).
func frameLines(frame string) []string { return rowStart.Split(frame, -1) }

// Revue LOT-3 : seul le pane leader de la fenêtre compte (un teammate qui hériterait de CL_ATTENTION_DIR ne signale pas),
// et un état dont le pane n'est plus le leader (pane disparu, crash) est ignoré.
func TestAttentionCmdLeaderOnly223(t *testing.T) {
	marker := isoInner(t)
	key, _, _, proj, leader, mates := visProject(t, marker)
	if len(mates) == 0 {
		t.Skip("pas de teammate dans la fixture")
	}
	writeAttn(t, proj, strings.TrimPrefix(mates[0], "%"), attnQuestion) // teammate seul
	m := model{session: hubSession, windows: map[string]bool{key: true}, attn: map[string]core.Attention{},
		nodes: []core.Node{{Project: true, Name: key, Key: key, Path: proj}}}
	if msg, _ := m.attentionCmd()().(attnMsg); len(msg) != 0 {
		t.Errorf("attente d'un teammate signalée : %+v", msg)
	}
	writeAttn(t, proj, strings.TrimPrefix(leader, "%"), attnQuestion)
	if msg, _ := m.attentionCmd()().(attnMsg); len(msg) != 1 {
		t.Errorf("attente du leader non signalée : %+v", msg)
	}
}

// Revue LOT-3 : @cl_attn n'est compté publié que si l'écriture réussit — retentée au tour suivant sinon.
func TestAttentionCmdRetriesFailedPublish223(t *testing.T) {
	marker := isoInner(t)
	key, win, _, proj, leader, _ := visProject(t, marker)
	writeAttn(t, proj, strings.TrimPrefix(leader, "%"), attnQuestion)
	m := model{session: hubSession, windows: map[string]bool{key: true}, attn: map[string]core.Attention{},
		nodes: []core.Node{{Project: true, Name: key, Key: key, Path: proj}}}
	old := setWinOption
	defer func() { setWinOption = old }()
	fails := 0
	setWinOption = func(args ...string) error { fails++; return errors.New("tmux en échec") }
	_ = m.attentionCmd()()
	if got := dt(t, "display-message", "-p", "-t", win, "#{@cl_attn}"); got != "" {
		t.Fatalf("option posée malgré l'échec : %q", got)
	}
	setWinOption = old // tmux répond de nouveau : même attente (même id), republiée
	_ = m.attentionCmd()()
	if got := dt(t, "display-message", "-p", "-t", win, "#{@cl_attn}"); got != "question|Valider le plan ?" {
		t.Errorf("option non republiée après l'échec : %q (échecs simulés : %d)", got, fails)
	}
}
