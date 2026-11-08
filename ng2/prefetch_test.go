package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Faux GitHub pour v9.9.9 : lien direct du binaire en 504 tant que directDown ;
// l'API liste et sert les fichiers seulement si apiUp.
func fakePrefRelease(t *testing.T, bin []byte) (directDown, apiUp *bool) {
	t.Helper()
	d, a := true, false
	sums := sumOf(bin) + "  " + ngAsset() + "\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch p := r.URL.Path; {
		case p == "/repos/"+launcherRepo+"/releases/tags/v9.9.9":
			fmt.Fprintf(w, `{"assets":[{"id":1,"name":"SHA256SUMS","state":"uploaded"},{"id":2,"name":%q,"state":"uploaded"}]}`, ngAsset())
		case p == "/rel/v9.9.9/SHA256SUMS":
			fmt.Fprint(w, sums)
		case p == "/rel/v9.9.9/"+ngAsset() && !d:
			w.Write(bin)
		case p == "/rel/v9.9.9/"+ngAsset():
			w.WriteHeader(504)
		case p == "/repos/"+launcherRepo+"/releases/assets/2" && a:
			w.Header().Set("Content-Length", fmt.Sprint(len(bin)))
			w.Write(bin)
		default:
			w.WriteHeader(504)
		}
	}))
	t.Cleanup(srv.Close)
	oldAPI, oldRel, oldWait, oldVer := apiBase, releaseBase, releaseRetryWait, ngVersion
	apiBase, releaseBase, releaseRetryWait, ngVersion = srv.URL, srv.URL+"/rel", time.Millisecond, "v5.0.0"
	t.Cleanup(func() { apiBase, releaseBase, releaseRetryWait, ngVersion = oldAPI, oldRel, oldWait, oldVer })
	t.Setenv("TMPDIR", t.TempDir())
	return &d, &a
}

// drain : exécute la commande et repasse ses messages au modèle jusqu'à la fin
// de la préparation (prefDoneMsg).
func drainPref(t *testing.T, m model, cmd tea.Cmd) (model, []string) {
	t.Helper()
	var steps []string
	for cmd != nil {
		msg := cmd()
		nm, next := m.Update(msg)
		m = nm.(model)
		if s, ok := msg.(prefStepMsg); ok {
			steps = append(steps, s.step)
			cmd = next
			continue
		}
		if _, ok := msg.(prefDoneMsg); ok {
			return m, steps
		}
		cmd = next
	}
	return m, steps
}

func TestPrefetchStates(t *testing.T) {
	bin := []byte(strings.Repeat("nouveau binaire ", 20000))
	directDown, apiUp := fakePrefRelease(t, bin)
	m := model{newLauncher: "v9.9.9", w: 40}
	if a := m.alertText("launcher"); !strings.Contains(a, "nouvelle version en préparation") {
		t.Errorf("orange : %q", a)
	}
	// publiée, binaire inaccessible (direct et API en 504) : jaune, raison, réessayer
	if !releasePublished("v9.9.9") {
		t.Fatal("release non vue publiée")
	}
	m.launcherPub = true
	m, _ = drainPref(t, m, m.startPrefetch("v9.9.9"))
	if m.launcherOK || m.prefErr != "HTTP 504" || !strings.Contains(m.alertText("launcher"), "HTTP 504 · réessayer") {
		t.Fatalf("inaccessible : ok=%v err=%q %q", m.launcherOK, m.prefErr, m.alertText("launcher"))
	}
	if !strings.Contains(m.versionLines(), m.alertText("launcher")) {
		t.Error("annonce jaune absente")
	}
	// l'API répond : étapes version → SHA256SUMS → contrôle, puis vert
	*apiUp = true
	m, steps := drainPref(t, m, m.startPrefetch("v9.9.9"))
	j := strings.Join(steps, " ")
	if !strings.HasPrefix(j, "bin:0 api") || !strings.Contains(j, "bin:100") || !strings.HasSuffix(j, "sha check") {
		t.Errorf("étapes : %s", j)
	}
	if !m.launcherOK || !strings.Contains(m.alertText("launcher"), "cliquer pour installer") {
		t.Fatalf("préparée : ok=%v %q", m.launcherOK, m.alertText("launcher"))
	}
	if b, _ := os.ReadFile(m.prefFile); string(b) != string(bin) || !strings.HasPrefix(m.prefFile, os.TempDir()) {
		t.Errorf("binaire préparé : %s", m.prefFile)
	}
	// étapes lisibles, courtes en 40 colonnes
	for _, s := range []string{"bin:45", "sha", "check", "cached", "start", "api"} {
		if w := len([]rune("  ↑ NG v9.9.9 · " + stepText(s, false) + " ✕")); w > 40 || stepText(s, true) == "" {
			t.Errorf("étape %s : %d colonnes", s, w)
		}
	}
	// clic : copie à la place du binaire, après contrôle
	dst := t.TempDir() + "/ng2"
	_ = os.WriteFile(dst, []byte("ancien"), 0o755)
	if err := installPrepared(m.prefFile, m.prefSum, dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); string(b) != string(bin) {
		t.Error("binaire non installé")
	}
	// binaire préparé modifié : refusé
	_ = os.WriteFile(m.prefFile, []byte("altéré"), 0o755)
	if err := installPrepared(m.prefFile, m.prefSum, dst); err == nil || !strings.Contains(err.Error(), "modifié") {
		t.Errorf("altéré : %v", err)
	}
	// déjà téléchargé (relance) : pas de second téléchargement
	*directDown, *apiUp = true, false
	_ = os.WriteFile(prefPath("v9.9.9"), bin, 0o755)
	m2 := model{newLauncher: "v9.9.9", launcherPub: true}
	m2, steps = drainPref(t, m2, m2.startPrefetch("v9.9.9"))
	if !m2.launcherOK || strings.Join(steps, " ") != "cached sha check" {
		t.Errorf("en cache : ok=%v %v", m2.launcherOK, steps)
	}
}
