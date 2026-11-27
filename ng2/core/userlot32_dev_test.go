package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// LOT-3.2 (dev) : découverte par labels, état d'un projet arrêté, approbation scellée. Faux docker / kubectl, dossiers temporaires.

func TestDecouverteDockerParLabels275(t *testing.T) {
	r := rcTools(t, true)
	e := Engine{rcBackend(t)}
	good := UserObjName("github-9", "cat", "proj")
	other := UserObjName("github-10", "cat", "proj")
	lines := []string{
		good + "|running|debian:12|cl.launcher=ng2,cl.user=github-9,cl.backend=d,cl.category=cat,cl.project=proj,cl.image=debian:12",
		other + "|running|x|cl.launcher=ng2,cl.user=github-10,cl.backend=d,cl.category=cat,cl.project=proj",           // autre utilisateur
		good + "-old-1-abcd|exited|x|cl.launcher=ng2,cl.user=github-9,cl.backend=d,cl.category=cat,cl.project=proj",   // sauvegarde de recréation : nom ≠ nom canonique
		"cl-zzzzzzzz-cat-p|exited|x|cl.launcher=ng2,cl.user=github-9,cl.backend=d,cl.category=cat,cl.project=p",       // nom ne redonne pas les labels
		good + "|exited|x|cl.launcher=ng2,cl.user=github-9,cl.backend=d,cl.category=cat,cl.project=proj,cl.state=old", // étiquette de sauvegarde
		"cl-x|exited|x|cl.launcher=ng2", // sans label d'utilisateur
	}
	_ = os.WriteFile(filepath.Join(r.dir, "ps.out"), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	ps, err := e.DiscoverUserProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 1 || ps[0].Object != good || ps[0].Category != "cat" || ps[0].Project != "proj" || !ps[0].Running {
		t.Errorf("projets découverts : %+v (un seul attendu)", ps)
	}
	for _, c := range r.calls() {
		if !strings.HasPrefix(c, "ps ") {
			t.Errorf("commande autre que la lecture : %s", c)
		}
	}
}
