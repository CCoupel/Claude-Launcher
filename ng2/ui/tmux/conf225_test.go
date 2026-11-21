package tmux

// #225 : la coque (nav du hub) relit config.conf par date à chaque tour, sans être relancée ; l'ouverture d'un projet (launch)
// recopie dans settings.json la NOUVELLE valeur ; config invalide : dernière copie valide gardée. CLAUDE_CONFIG_DIR temporaire,
// jamais le vrai settings.json ; aucun tmux lancé (le modèle est piloté par ses messages).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"claude-launcher-ng2/core"
)

const mouseVar225 = "CLAUDE_CODE_DISABLE_MOUSE"

func conf225(t *testing.T, content string, at time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(core.ConfigPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(core.ConfigPath(), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(core.ConfigPath(), at, at); err != nil {
		t.Fatal(err)
	}
}

func box225(t *testing.T) time.Time {
	t.Helper()
	withHome(t)
	t.Setenv("CL_SHELL", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	if !strings.HasPrefix(core.ClaudeDir(), os.TempDir()) {
		t.Fatalf("ClaudeDir hors dossier temporaire : %s", core.ClaudeDir())
	}
	return time.Now().Add(-time.Hour)
}

func mouse225(t *testing.T) (string, bool) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(core.ClaudeDir(), "settings.json"))
	if err != nil {
		return "", false
	}
	var doc struct {
		Env map[string]string `json:"env"`
	}
	_ = json.Unmarshal(b, &doc)
	return doc.Env[mouseVar225], true
}

// un « tour » du nav : le message de tic du hub.
func turn225(m model, msg tea.Msg) model {
	nm, _ := m.Update(msg)
	return nm.(model)
}

// Édition extérieure → relue au tour suivant, sans relancer le nav, dans les deux sens ; Roots (figées au démarrage) intactes.
func TestNavRelitConfigParDate225(t *testing.T) {
	t0 := box225(t)
	conf225(t, "CLAUDE_DISABLE_MOUSE=0\nTEAM_HIDE_IDLE=30\n", t0)
	m := NewModel(false)
	roots := append([]string(nil), m.cfg.Roots...)
	if m.cfg.DisableMouse || m.cfg.TeamHideIdle != 30 {
		t.Fatalf("départ : souris=%v hideidle=%d", m.cfg.DisableMouse, m.cfg.TeamHideIdle)
	}
	conf225(t, "CLAUDE_DISABLE_MOUSE=1\nTEAM_HIDE_IDLE=45\n", t0.Add(10*time.Second))
	for name, msg := range map[string]tea.Msg{"tic": tickMsg(time.Now())} { // un « tour » du nav = le tic du hub (2 s)
		t.Run(name, func(t *testing.T) {
			m2 := turn225(m, msg)
			if !m2.cfg.DisableMouse || m2.cfg.TeamHideIdle != 45 {
				t.Errorf("après un tour (%s) : souris=%v hideidle=%d, attendu 1 / 45 (config.conf relu sans relance)", name, m2.cfg.DisableMouse, m2.cfg.TeamHideIdle)
			}
			if strings.Join(m2.cfg.Roots, "|") != strings.Join(roots, "|") {
				t.Errorf("Roots changées à chaud : %v → %v (réglage lu seulement au démarrage)", roots, m2.cfg.Roots)
			}
		})
	}
	// fichier inchangé : même copie (pas de relecture inutile qui perdrait l'état)
	m2 := turn225(m, tickMsg(time.Now()))
	m3 := turn225(m2, tickMsg(time.Now()))
	if m3.cfg.Stamp != m2.cfg.Stamp {
		t.Errorf("config.conf inchangé : copie remplacée (%v → %v)", m2.cfg.Stamp, m3.cfg.Stamp)
	}
}

// Config invalide : dernière copie valide gardée (et dite en pied), corrigée : nouvelle valeur prise.
func TestNavConfigInvalideGardeLAncienneCopie225(t *testing.T) {
	t0 := box225(t)
	conf225(t, "CLAUDE_DISABLE_MOUSE=1\n", t0)
	m := NewModel(false)
	bad := "CLAUDE_DISABLE_MOUSE=0\nif then ((( fi\n"
	if p := core.ValidateConfig(core.ConfConf, []byte(bad)); !core.HasErrors(p) {
		t.Skip("contenu jugé valide : choisir une autre erreur de syntaxe")
	}
	conf225(t, bad, t0.Add(10*time.Second))
	m2 := turn225(m, tickMsg(time.Now()))
	if !m2.cfg.DisableMouse {
		t.Error("config invalide : la dernière copie valide (souris 1) n'est plus utilisée")
	}
	if !strings.Contains(m2.msg, "invalide") {
		t.Errorf("msg = %q, attendu la mention « invalide »", m2.msg)
	}
	conf225(t, "CLAUDE_DISABLE_MOUSE=0\n", t0.Add(20*time.Second))
	if m3 := turn225(m2, tickMsg(time.Now())); m3.cfg.DisableMouse {
		t.Error("config corrigée (souris 0) non relue")
	}
}

// Ouverture d'un projet (launch.go) avec une copie périmée : settings.json porte la NOUVELLE valeur (souris 0 → 1).
func TestLaunchRecopieLaNouvelleValeur225(t *testing.T) {
	for name, c := range map[string]struct{ avant, apres, want string }{"0 vers 1": {"0", "1", "1"}, "1 vers 0": {"1", "0", ""}} {
		t.Run(name, func(t *testing.T) {
			t0 := box225(t)
			conf225(t, "CLAUDE_DISABLE_MOUSE="+c.avant+"\n", t0)
			stale := core.LoadConfig()
			conf225(t, "CLAUDE_DISABLE_MOUSE="+c.apres+"\n", t0.Add(10*time.Second))
			_ = syncLauncherEnv(stale) // l'appel de launchProject, avec la copie qu'il a reçue
			if v, ok := mouse225(t); !ok || v != c.want {
				t.Errorf("settings.json : souris=%q (existe=%v), attendu %q (valeur du fichier)", v, ok, c.want)
			}
		})
	}
}
