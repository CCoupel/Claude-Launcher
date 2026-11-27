package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"claude-launcher-ng2/core"
)

// LOT-3.2 Batch 2 (#275, tâches 15–16) — projet arrêté d'un utilisateur par l'API web (RunWeb, mode proxy) : /files → 404 SANS démarrage ni
// lecture ; Purger = trace seulement, AUCUNE commande vers le backend (contracts/user-backends.md §5, §6, §11). Faux docker/kubectl qui
// journalisent (ub257Start) : tout appel apparaît dans calls.log. Écrits avant le code.

func (u *ub257) calls() string {
	b, _ := os.ReadFile(filepath.Join(u.dir, "calls.log"))
	return strings.TrimSpace(string(b))
}

func TestProj275FilesDUnProjetArreteSansDemarrage(t *testing.T) {
	u := ub257Start(t)
	ub257DeclareDocker(t, "github-1111", "d", "tcp://93.184.216.34:2376", "")
	sp, _ := core.UserSpace("github-1111")
	if err := sp.RecordTrace(core.Trace{Backend: "d", Kind: "docker", Object: "cl-proj", Project: "proj", Category: "cat"}); err != nil {
		t.Fatal(err)
	}
	hdr := map[string]string{"X-Forwarded-User": "alice", "X-Forwarded-For": "192.168.1.20"}
	for _, p := range []string{"/files/proj/readme.md", "/files/cat/proj/readme.md", "/files/proj/", "/files/proj/.git/config", "/files/proj/../../x", "/files/proj/_work/mockup/m.html"} {
		r, body := u.s.do("GET", p, px172Ext, hdr, "")
		if r == nil || r.StatusCode != 404 {
			code := 0
			if r != nil {
				code = r.StatusCode
			}
			t.Errorf("GET %s : %d (404 attendu pour un projet arrêté)", p, code)
		}
		if strings.Contains(strings.ToLower(body), "readme") && strings.Contains(body, "contenu") {
			t.Errorf("GET %s : contenu servi", p)
		}
	}
	if c := u.calls(); c != "" {
		t.Errorf("commande vers le backend pour un /files de projet arrêté (ni lecture ni démarrage) :\n%s", c)
	}
}

func TestProj275PurgerSansCommandeVersLeBackend(t *testing.T) {
	u := ub257Start(t)
	ub257DeclareDocker(t, "github-1111", "d", "tcp://93.184.216.34:2376", "")
	sp, _ := core.UserSpace("github-1111")
	for _, p := range []string{"proj", "autre"} {
		if err := sp.RecordTrace(core.Trace{Backend: "d", Kind: "docker", Object: "cl-" + p, Project: p, Category: "cat"}); err != nil {
			t.Fatal(err)
		}
	}
	// un autre utilisateur ne purge pas la trace d'alice
	_, res, e := u.call(t, "bob", "projectPurge", map[string]any{"backend": "d", "project": "proj"})
	if res != nil && res["ok"] == true && e == "" {
		if ts, _ := sp.LoadTraces(); len(ts) != 2 {
			t.Errorf("bob a purgé la trace d'alice : %d trace(s)", len(ts))
		}
	}
	raw, res, e := u.call(t, "alice", "projectPurge", map[string]any{"backend": "d", "project": "proj"})
	if e != "" || res["ok"] != true {
		t.Fatalf("projectPurge : %s", raw)
	}
	if !strings.Contains(strings.ToLower(raw), "fichiers") || !strings.Contains(strings.ToLower(raw), "restent") {
		t.Errorf("message « les fichiers éventuels restent dans <PVC/volume> » absent : %.300s", raw)
	}
	ts, _ := sp.LoadTraces()
	if len(ts) != 1 || ts[0].Project != "autre" {
		t.Errorf("traces après purge : %+v (seule « proj » effacée)", ts)
	}
	if c := u.calls(); c != "" {
		t.Errorf("Purger a lancé une commande vers le backend (trace seulement) :\n%s", c)
	}
	// noms hostiles / backend inconnu : refus, rien lancé
	for _, a := range []map[string]any{{"backend": "../x", "project": "p"}, {"backend": "d", "project": "../etc"}, {"backend": "", "project": ""}, {"backend": "inconnu", "project": "proj"}} {
		_, res, e := u.call(t, "alice", "projectPurge", a)
		if e == "" && res["ok"] == true && a["backend"] != "inconnu" {
			t.Errorf("projectPurge %v accepté", a)
		}
	}
	if c := u.calls(); c != "" {
		t.Errorf("commande lancée par un projectPurge refusé :\n%s", c)
	}
}
