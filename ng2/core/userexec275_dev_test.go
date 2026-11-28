package core

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Audit LOT-3.2 B1/M2 (dev) : sorties bornées, délais, FIFO, chemin réel — faux docker à comportement hostile choisi par fichier « mode ».

func hostileFake(t *testing.T) (dir string, e Engine) {
	t.Helper()
	unixOnly(t)
	dir = t.TempDir()
	script := `#!/bin/sh
[ "$1" = "-H" ] && shift 2
M=$(cat "` + dir + `/mode" 2>/dev/null)
case "$1" in
  cp) [ "$M" = infinite ] && exec yes AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA ;;
  ps) [ "$M" = infinite ] && exec yes AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA; cat "` + dir + `/ps.out" 2>/dev/null ;;
  inspect) [ "$M" = hang ] && exec sleep 30; echo github-9 ;;
  exec)
    case "$*" in
      *realpath*) cat "` + dir + `/resolve.out" ;;
      *"cat --"*) [ "$M" = fifo ] && exec sleep 30; [ "$M" = infinite ] && exec yes AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA; echo contenu ;;
      *) : ;;
    esac ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir, Engine{rcBackend(t)}
}

func setMode(dir, m string) { _ = os.WriteFile(filepath.Join(dir, "mode"), []byte(m), 0o644) }

func TestSortiesBornees275(t *testing.T) {
	dir, e := hostileFake(t)
	setMode(dir, "infinite")
	t0 := time.Now()
	if _, err := e.ReadStoppedState("cat", "p"); err == nil {
		t.Error("état infini accepté")
	}
	if _, err := e.DiscoverUserProjects(); err == nil {
		t.Error("découverte à sortie infinie acceptée")
	}
	if d := time.Since(t0); d > 10*time.Second {
		t.Errorf("lecture de sortie infinie : %v (tuée dès le plafond attendu)", d)
	}
}

func TestDelaiCommandes275(t *testing.T) {
	dir, e := hostileFake(t)
	old := userReadTimeout
	userReadTimeout = 300 * time.Millisecond
	defer func() { userReadTimeout = old }()
	setMode(dir, "hang")
	t0 := time.Now()
	if err := e.VerifyOwner("github-9", "cl-x"); err == nil {
		t.Error("propriétaire accepté malgré le blocage")
	}
	if d := time.Since(t0); d > 5*time.Second {
		t.Errorf("commande bloquée non coupée : %v", d)
	}
}

func serveOnce(e Engine, g *UserGate, sub string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.ServeUserFile(rec, httptest.NewRequest("GET", "/files/cat/p/"+sub, nil), "cat", "p", sub, g)
	return rec
}

func TestFilesFifoEtCheminReel275(t *testing.T) {
	dir, e := hostileFake(t)
	g := NewUserGate()
	oldO := userOpenTimeout
	userOpenTimeout = 300 * time.Millisecond
	defer func() { userOpenTimeout = oldO }()
	res := func(kind, real string) {
		_ = os.WriteFile(filepath.Join(dir, "resolve.out"), []byte(kind+"\n"+real+"\n/space/PROJETS/cat/p\n"), 0o644)
	}
	// fichier ordinaire : servi
	setMode(dir, "")
	res("f", "/space/PROJETS/cat/p/a.txt")
	if rec := serveOnce(e, g, "a.txt"); rec.Code != 200 || !strings.Contains(rec.Body.String(), "contenu") {
		t.Fatalf("fichier : %d %q", rec.Code, rec.Body.String())
	}
	// FIFO : « cat » ne rend aucun octet → coupé au délai d'ouverture, 404, compteurs libérés
	setMode(dir, "fifo")
	t0 := time.Now()
	if rec := serveOnce(e, g, "a.txt"); rec.Code != http.StatusNotFound {
		t.Errorf("fifo : %d", rec.Code)
	}
	if d := time.Since(t0); d > 8*time.Second {
		t.Errorf("fifo non coupée : %v", d)
	}
	for i := 0; i < 4; i++ {
		rel, err := g.AcquireStream("github-9")
		if err != nil {
			t.Fatalf("flux non libéré (%d) : %v", i, err)
		}
		defer rel()
	}
	// chemin réel hors du projet, sensible ou autre genre : 404, rien lu
	setMode(dir, "")
	for name, r := range map[string][2]string{
		"hors projet":        {"f", "/etc/passwd"},
		"préfixe voisin":     {"f", "/space/PROJETS/cat/p2/x"},
		"sensible résolu":    {"f", "/space/PROJETS/cat/p/.env"},
		"claude résolu":      {"f", "/space/PROJETS/cat/p/_work/reports/x.md"},
		"dossier sans slash": {"d", "/space/PROJETS/cat/p/sub"},
	} {
		res(r[0], r[1])
		if rec := serveOnce(e, NewUserGate(), "a.txt"); rec.Code != http.StatusNotFound {
			t.Errorf("%s : %d servi", name, rec.Code)
		}
	}
	// sortie infinie : plafonnée (256 Mio) ou coupée à la fin de la requête — ici le client part tout de suite
}

func TestYamlDupKeyFoldRune275(t *testing.T) {
	for name, y := range map[string]string{"s/ſ": "a: 1\nA: 2\n", "ſ": "ſ: 1\ns: 2\n", "K": "K: 1\nk: 2\n"} {
		if !yamlDupKeyCI([]byte(y)) {
			t.Errorf("%s : clé en double non détectée", name)
		}
	}
	if yamlDupKeyCI([]byte("a: 1\nb: 2\n")) {
		t.Error("faux positif")
	}
}

// Revue LOT-3.2 (majeur) : un listage en erreur ne laisse JAMAIS créer sur k8s (le StatefulSet existant n'est ni mis à jour ni arrêté) ; la création
// emploie « create » et non « apply ».
func TestCreationK8sEchecFerme275(t *testing.T) {
	unixOnly(t)
	b := e2K8s(t)
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	script := `#!/bin/sh
echo "$*" >> "` + log + `"
case "$*" in
  *"get statefulsets"*) [ -f "` + dir + `/listfail" ] && { echo "timeout" >&2; exit 1; }; echo '{"items":[]}' ;;
  *) : ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	e := Engine{b}
	_ = os.WriteFile(filepath.Join(dir, "listfail"), nil, 0o644)
	if err := e.CreateUserProject("cat", "proj", UserDefaultImage()); err == nil {
		t.Error("création acceptée malgré l'échec du listage")
	}
	sp, _ := UserSpace(e2UID)
	if err := sp.MoveProject(e, "cat", "proj", "cat2", "proj2"); err == nil {
		t.Error("déplacement accepté malgré l'échec du listage")
	}
	if b, _ := os.ReadFile(log); strings.Contains(string(b), "create") || strings.Contains(string(b), "apply") || strings.Contains(string(b), "delete") {
		t.Errorf("commande d'écriture lancée malgré l'échec du listage :\n%s", b)
	}
	_ = os.Remove(filepath.Join(dir, "listfail"))
	if err := e.CreateUserProject("cat", "proj", UserDefaultImage()); err != nil {
		t.Fatalf("création : %v", err)
	}
	b2, _ := os.ReadFile(log)
	if !strings.Contains(string(b2), "create -f -") || strings.Contains(string(b2), "apply") {
		t.Errorf("« create -f - » attendu, jamais apply :\n%s", b2)
	}
}
