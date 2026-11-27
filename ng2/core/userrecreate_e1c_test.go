package core

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// LOT-3.2 Batch 0 (#275, E1c / P27) — recréation d'un conteneur docker/podman après confirmation (contracts/user-backends.md §5 bis).
// Étiquette « s275api » ; API proposée (à adopter ou renommer) :
//
//	func (e Engine) RecreateToken(name string, createArgs []string) string                                   // jeton à usage unique, lié au projet et aux options
//	func (e Engine) RecreateContainer(name string, createArgs []string, token string, wasRunning bool, now time.Time) error
//
// + (sécurité T16–T18) : var ErrRecreateBusy, ErrRecreateNothing, ErrRecreateDuplicate error ; func (e Engine) RestoreInterrupted(name string) error
// createArgs : l'argv de « create » du NOUVEAU conteneur (sans « docker ») ; sans jeton valide : ErrRecreateUnconfirmed (D3/T17 : jeton lié
// au projet ET aux options demandées, à usage unique ; la sauvegarde est identifiée par ses labels cl.recreate-of / cl.state=old).
// Faux docker : journalise chaque commande ; « failcreate » dans son dossier fait échouer « create » ; l'état (existe / tourne) est simulé.

type rc struct{ dir, log string }

func rcTools(t *testing.T, running bool) rc {
	t.Helper()
	unixOnly(t)
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	state := filepath.Join(dir, "running")
	if running {
		_ = os.WriteFile(state, nil, 0o644)
	}
	script := `#!/bin/sh
[ "$1" = "-H" ] && shift 2 # le launcher joint le démon d'un utilisateur par « -H tcp://… » : le journal ne garde que la commande
echo "$*" >> "` + log + `"
case "$1" in
  ps) [ -f "` + dir + `/ps.out" ] && cat "` + dir + `/ps.out" ;;
  stop) [ -f "` + dir + `/slow" ] && sleep 1 ;;
  create) [ -f "` + dir + `/failcreate" ] && { echo "Error: create failed" >&2; exit 1; }; echo newid ;;
  inspect) if [ -f "` + dir + `/owner" ]; then cat "` + dir + `/owner"; elif [ -f "` + state + `" ]; then echo true; else echo false; fi ;;
  *) : ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return rc{dir: dir, log: log}
}

func (r rc) calls() []string {
	b, _ := os.ReadFile(r.log)
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// rcDo : recréation avec (confirmed) ou sans jeton de confirmation.
func rcDo(e Engine, name string, args []string, confirmed, running bool) error {
	token := ""
	if confirmed {
		token = e.RecreateToken(name, args)
	}
	return e.RecreateContainer(name, args, token, running, rcNow)
}

func rcBackend(t *testing.T) BackendConf {
	t.Helper()
	s15Setup(t)
	sp := s15Space(t, "github-9", "backends:\n  d:\n    type: docker\n    host: tcp://93.184.216.34:2376\n    volume: vol\n")
	bs, err := sp.LoadBackends()
	if err != nil || len(bs) == 0 {
		t.Fatalf("backend : %v", err)
	}
	return bs[0]
}

var rcNow = time.Date(2026, 11, 27, 15, 4, 5, 0, time.UTC)

func TestE1cSansConfirmationRienNeChange(t *testing.T) {
	r := rcTools(t, true)
	e := Engine{rcBackend(t)}
	err := rcDo(e, "cl-proj", []string{"create", "--name", "cl-proj", "--shm-size", "1g", "debian:12"}, false, true)
	if err == nil || err != ErrRecreateUnconfirmed {
		t.Errorf("sans confirmation : %v (ErrRecreateUnconfirmed attendu)", err)
	}
	for _, c := range r.calls() {
		if !strings.Contains(c, "inspect") { // seule la lecture est tolérée
			t.Errorf("commande lancée sans confirmation : %s", c)
		}
	}
}

func TestE1cDeroule(t *testing.T) {
	r := rcTools(t, true)
	e := Engine{rcBackend(t)}
	if err := rcDo(e, "cl-proj", []string{"create", "--name", "cl-proj", "--shm-size", "1g", "debian:12"}, true, true); err != nil {
		t.Fatal(err)
	}
	calls := strings.Join(r.calls(), "\n")
	order := []string{"stop", "rename cl-proj cl-proj-old-", "create", "start", "rm"}
	last := -1
	for _, want := range order {
		i := strings.Index(calls, want)
		if i < 0 || i < last {
			t.Errorf("étape « %s » absente ou dans le désordre :\n%s", want, calls)
		}
		last = i
	}
	// l'ancien est supprimé SANS toucher au volume : ni « rm -v », ni « volume … »
	for _, c := range r.calls() {
		if strings.HasPrefix(c, "volume") || strings.HasPrefix(c, "rm") && (strings.Contains(c, " -v") || strings.Contains(c, "--volumes")) {
			t.Errorf("le volume est touché : %s", c)
		}
	}
	if !strings.Contains(calls, "rm cl-proj-old-") {
		t.Errorf("l'ancien conteneur n'est pas supprimé :\n%s", calls)
	}
}

func TestE1cEchecDeCreationRetourArriere(t *testing.T) {
	for _, running := range []bool{true, false} {
		r := rcTools(t, running)
		_ = os.WriteFile(filepath.Join(r.dir, "failcreate"), nil, 0o644)
		e := Engine{rcBackend(t)}
		err := rcDo(e, "cl-proj", []string{"create", "--name", "cl-proj", "debian:12"}, true, running)
		if err == nil || strings.Contains(err.Error(), "ANCIEN-SECRET") {
			t.Errorf("running=%v : erreur nommée attendue : %v", running, err)
		}
		calls := strings.Join(r.calls(), "\n")
		if !strings.Contains(calls, "rename cl-proj-old-") || !strings.Contains(calls, " cl-proj\n") && !strings.HasSuffix(calls, " cl-proj") {
			t.Errorf("running=%v : l'ancien n'est pas renommé à son nom d'origine :\n%s", running, calls)
		}
		if strings.Contains(calls, "rm cl-proj-old-") {
			t.Errorf("running=%v : l'ancien conteneur supprimé malgré l'échec :\n%s", running, calls)
		}
		started := strings.Contains(calls, "start cl-proj")
		if running && !started {
			t.Errorf("l'ancien tournait : il doit être redémarré :\n%s", calls)
		}
		if !running && started {
			t.Errorf("l'ancien était arrêté : il ne doit pas être redémarré :\n%s", calls)
		}
	}
}

// Jamais d'option dangereuse dans la commande de recréation, même fournie en entrée.
func TestE1cArgumentsDangereuxRefuses(t *testing.T) {
	r := rcTools(t, true)
	e := Engine{rcBackend(t)}
	for _, bad := range [][]string{{"create", "--privileged", "debian:12"}, {"create", "-v", "/:/host", "debian:12"}, {"create", "--network", "host", "debian:12"},
		{"create", "-p", "22:22", "debian:12"}, {"run", "debian:12"}, {"rm", "-f", "cl-proj"}} {
		if err := e.RecreateContainer("cl-proj", bad, e.RecreateToken("cl-proj", bad), true, rcNow); err == nil {
			t.Errorf("argv %v accepté", bad)
		}
	}
	if len(r.calls()) != 0 {
		t.Errorf("commandes lancées pour des arguments refusés : %v", r.calls())
	}
}

// ── Exigences de sécurité T16–T19 (_work/reports/security-lot3.2.md) ───────────────────────────────────

// T16 — la sauvegarde est identifiée par LABEL (cl.recreate-of / cl.state=old), jamais par le motif du nom : un projet d'utilisateur nommé
// « x-old-<chiffres> » n'est jamais renommé, ni supprimé, ni « restauré » par la reprise après interruption.
// API proposée : func (e Engine) RestoreInterrupted(name string) error (reprise au démarrage / à l'ouverture).
func TestT16ProjetNommeXOldJamaisTouche(t *testing.T) {
	r := rcTools(t, true)
	e := Engine{rcBackend(t)}
	// le moteur ne connaît qu'un conteneur « x-old-20261127 » (projet de l'utilisateur, labels cl.user…), aucun « x »
	_ = os.WriteFile(filepath.Join(r.dir, "ps.out"), []byte("x-old-20261127 cl.user=github-9 cl.project=x-old-20261127\n"), 0o644)
	if err := e.RestoreInterrupted("x"); err != nil && err != ErrRecreateNothing {
		t.Logf("RestoreInterrupted : %v", err)
	}
	for _, c := range r.calls() {
		if strings.HasPrefix(c, "rename") || strings.HasPrefix(c, "rm") || strings.HasPrefix(c, "start") || strings.HasPrefix(c, "stop") {
			t.Errorf("le conteneur d'un projet « x-old-… » a été touché : %s", c)
		}
	}
}

// T17 — verrou par projet : deux recréations simultanées du même projet → une seule s'exécute, l'autre est refusée (ErrRecreateBusy).
func TestT17RecreationsConcurrentes(t *testing.T) {
	r := rcTools(t, true)
	_ = os.WriteFile(filepath.Join(r.dir, "slow"), nil, 0o644) // le faux docker ralentit « stop » (voir rcTools : variable slow)
	e := Engine{rcBackend(t)}
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			errs <- rcDo(e, "cl-proj", []string{"create", "--name", "cl-proj", "debian:12"}, true, true)
		}()
	}
	a, b := <-errs, <-errs
	busy := 0
	for _, err := range []error{a, b} {
		if err == ErrRecreateBusy {
			busy++
		}
	}
	if busy != 1 || (a != nil && b != nil && busy != 1) {
		t.Errorf("recréations simultanées : %v / %v (une seule exécutée, l'autre ErrRecreateBusy)", a, b)
	}
	creates := 0
	for _, c := range r.calls() {
		if strings.HasPrefix(c, "create") {
			creates++
		}
	}
	if creates != 1 {
		t.Errorf("%d création(s) de conteneur pour 2 demandes simultanées", creates)
	}
}

// T18 / T19 — l'ancien n'est supprimé qu'APRÈS le démarrage ET l'inspection du nouveau ; jamais « rm -v », « volume », « prune ».
func TestT18AncienSupprimeApresVerificationDuNouveau(t *testing.T) {
	r := rcTools(t, true)
	e := Engine{rcBackend(t)}
	if err := rcDo(e, "cl-proj", []string{"create", "--name", "cl-proj", "debian:12"}, true, true); err != nil {
		t.Fatal(err)
	}
	calls := r.calls()
	idx := func(prefix string, from int) int {
		for i := from; i < len(calls); i++ {
			if strings.HasPrefix(calls[i], prefix) {
				return i
			}
		}
		return -1
	}
	start := idx("start", 0)
	insp := idx("inspect", start+1) // inspection du NOUVEAU après son démarrage
	rm := idx("rm", 0)
	if start < 0 || insp < 0 || rm < 0 || !(start < insp && insp < rm) {
		t.Errorf("ordre attendu start → inspect → rm de l'ancien :\n%s", strings.Join(calls, "\n"))
	}
	for _, c := range calls {
		if strings.Contains(c, "prune") || strings.HasPrefix(c, "volume") || strings.HasPrefix(c, "rm") && (strings.Contains(c, " -v") || strings.Contains(c, "--volumes")) {
			t.Errorf("commande interdite : %s", c)
		}
	}
}

// D3/T16 — la sauvegarde porte cl.recreate-of=<nom> et cl.state=old (ou la découverte l'exclut par ces labels) ; jamais « -old-<horodatage> » seul.
func TestT16SauvegardeEtiquetee(t *testing.T) {
	r := rcTools(t, true)
	e := Engine{rcBackend(t)}
	if err := rcDo(e, "cl-proj", []string{"create", "--name", "cl-proj", "debian:12"}, true, true); err != nil {
		t.Fatal(err)
	}
	calls := strings.Join(r.calls(), "\n")
	// docker et podman ne permettent PAS d'ajouter une étiquette à un conteneur existant (ni « update --label-add », ni « rename » étiqueté) :
	// la sauvegarde garde ses étiquettes d'ORIGINE (cl.user, cl.project du projet) et son nom <nom>-old-<ns>-<aléa> ; elle est reconnue à
	// cette combinaison (RestoreInterrupted), jamais au motif du nom seul. cl.recreate-of / cl.state=old sont reconnus s'ils existent.
	_ = calls
	// nom de la sauvegarde : <nom>-old-<ns>-<aléa> (deux recréations successives → deux noms différents)
	first := ""
	for _, c := range r.calls() {
		if strings.HasPrefix(c, "rename cl-proj cl-proj-old-") {
			first = strings.Fields(c)[2]
		}
	}
	if first == "" || regexp.MustCompile(`^cl-proj-old-[0-9]+-[a-z0-9]{4,}$`).FindString(first) == "" {
		t.Errorf("nom de sauvegarde %q (<nom>-old-<ns>-<aléa> attendu)", first)
	}
}

// Jeton de confirmation : à usage unique, lié au projet et aux options ; un jeton rejoué, d'un autre projet ou d'autres options est refusé.
func TestT17JetonDeConfirmation(t *testing.T) {
	r := rcTools(t, true)
	e := Engine{rcBackend(t)}
	args := []string{"create", "--name", "cl-proj", "--shm-size=1g", "debian:12"}
	tok := e.RecreateToken("cl-proj", args)
	if tok == "" || tok == e.RecreateToken("cl-proj", args) {
		t.Errorf("le jeton doit être non vide et unique à chaque émission : %q", tok)
	}
	if err := e.RecreateContainer("cl-proj", []string{"create", "--name", "cl-proj", "--shm-size=8g", "debian:12"}, tok, true, rcNow); err != ErrRecreateUnconfirmed {
		t.Errorf("jeton émis pour d'autres options accepté : %v", err)
	}
	if err := e.RecreateContainer("cl-autre", args, tok, true, rcNow); err != ErrRecreateUnconfirmed {
		t.Errorf("jeton d'un autre projet accepté : %v", err)
	}
	if n := len(r.calls()); n > 2 { // inspect seulement
		t.Errorf("%d commande(s) lancée(s) avec un jeton invalide", n)
	}
	if err := e.RecreateContainer("cl-proj", args, tok, true, rcNow); err != nil {
		t.Fatalf("jeton valide refusé : %v", err)
	}
	if err := e.RecreateContainer("cl-proj", args, tok, true, rcNow); err != ErrRecreateUnconfirmed {
		t.Errorf("jeton rejoué accepté : %v", err)
	}
}

// T16 — reprise après arrêt brutal : sauvegarde présente (labels) et nominal absent → restauré ; les deux présents → RIEN n'est supprimé, doublon signalé.
func TestT16RepriseApresArretBrutal(t *testing.T) {
	r := rcTools(t, false)
	e := Engine{rcBackend(t)}
	_ = os.WriteFile(filepath.Join(r.dir, "ps.out"), []byte("cl-proj-old-1-abcd cl.recreate-of=cl-proj cl.state=old\n"), 0o644)
	if err := e.RestoreInterrupted("cl-proj"); err != nil {
		t.Fatalf("reprise : %v", err)
	}
	if !strings.Contains(strings.Join(r.calls(), "\n"), "rename cl-proj-old-1-abcd cl-proj") {
		t.Errorf("sauvegarde non restaurée :\n%s", strings.Join(r.calls(), "\n"))
	}
	_ = os.Remove(r.log)
	_ = os.WriteFile(filepath.Join(r.dir, "ps.out"), []byte("cl-proj cl.user=github-9\ncl-proj-old-1-abcd cl.recreate-of=cl-proj cl.state=old\n"), 0o644)
	err := e.RestoreInterrupted("cl-proj")
	if err != ErrRecreateDuplicate {
		t.Errorf("deux présents : %v (ErrRecreateDuplicate attendu : doublon signalé)", err)
	}
	for _, c := range r.calls() {
		if strings.HasPrefix(c, "rm") || strings.HasPrefix(c, "rename") {
			t.Errorf("doublon : commande destructrice lancée : %s", c)
		}
	}
}

// T10 — aucune opération sur un objet dont le label cl.user ≠ l'uid de l'appelant, ni sans label : vérifié AVANT usage (objet de même nom
// qu'un projet de l'utilisateur, créé par un autre compte). Le faux docker rend le label du fichier « owner ».
func TestT10ProprietaireVerifieAvantUsage(t *testing.T) {
	r := rcTools(t, true)
	e := Engine{rcBackend(t)}
	owner := filepath.Join(r.dir, "owner")
	for content, want := range map[string]bool{"github-9": true, "github-10": false, "": false, "github-9x": false, "GITHUB-9": false} {
		_ = os.WriteFile(owner, []byte(content), 0o644)
		err := e.VerifyOwner("github-9", "cl-proj")
		if (err == nil) != want {
			t.Errorf("cl.user=%q : propriétaire accepté=%v, attendu %v (%v)", content, err == nil, want, err)
		}
	}
	// recréation / suppression sur un objet d'un autre : refusées, rien lancé hors lecture
	_ = os.WriteFile(owner, []byte("github-10"), 0o644)
	_ = os.Remove(r.log)
	if err := rcDo(e, "cl-proj", []string{"create", "--name", "cl-proj", "debian:12"}, true, true); err == nil {
		t.Error("recréation d'un objet appartenant à un autre utilisateur acceptée")
	}
	for _, c := range r.calls() {
		if !strings.HasPrefix(c, "inspect") && !strings.HasPrefix(c, "ps") {
			t.Errorf("commande lancée sur l'objet d'un autre utilisateur : %s", c)
		}
	}
}
