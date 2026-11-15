package desktop

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Bouton de bascule de mise en page (#153) : les vraies fonctions de frontend/app.js (layoutFetch,
// paintLayoutButton, toggleLayout, layoutDrop, fitLeadersTab) sont exécutées par node dans un contexte vm minimal
// (testdata/tlayb_check.js) : lecture au changement d'onglet et après bascule, relecture espacée, frein croissant
// sur erreur, bouton grisé (aria-disabled + infobulle, clic ignoré) sur Teamleaders et pendant la lecture,
// bascule par projectApply, retour arrière sur refus, largeur du bouton comptée dans la réduction de l'onglet
// Teamleaders (#162). node est requis par scripts/ci.sh (node --check) : sans lui le test est sauté, explicitement.
func TestLayoutToggleButtonFrontend(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node absent : contrôle du bouton de mise en page non exécuté (scripts/ci.sh exige node)")
	}
	script, _ := filepath.Abs(filepath.Join("testdata", "tlayb_check.js"))
	app, _ := filepath.Abs(filepath.Join("frontend", "app.js"))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, node, script, app).CombinedOutput()
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.HasPrefix(l, "FAIL") {
			t.Error(l)
		}
	}
	if err != nil {
		t.Fatalf("tlayb_check.js : %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "PASS") || strings.Contains(string(out), "ÉCHEC") {
		t.Errorf("sortie inattendue :\n%s", out)
	}
	t.Log(lastLine(string(out)))
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	return s[strings.LastIndex(s, "\n")+1:]
}
