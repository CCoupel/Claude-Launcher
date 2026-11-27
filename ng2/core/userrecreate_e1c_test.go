//go:build s275api

package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// LOT-3.2 Batch 0 (#275, E1c / P27) — recréation d'un conteneur docker/podman après confirmation (contracts/user-backends.md §5 bis).
// Étiquette « s275api » ; API proposée (à adopter ou renommer) :
//
//	func (e Engine) RecreateContainer(name string, createArgs []string, confirmed bool, wasRunning bool, now time.Time) error
//
// createArgs : l'argv de « create » du NOUVEAU conteneur (sans « docker ») ; l'erreur sans confirmation est ErrRecreateUnconfirmed.
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
echo "$*" >> "` + log + `"
case "$1" in
  create) [ -f "` + dir + `/failcreate" ] && { echo "Error: create failed" >&2; exit 1; }; echo newid ;;
  inspect) [ -f "` + state + `" ] && echo true || echo false ;;
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
	err := e.RecreateContainer("cl-proj", []string{"create", "--name", "cl-proj", "--shm-size", "1g", "debian:12"}, false, true, rcNow)
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
	if err := e.RecreateContainer("cl-proj", []string{"create", "--name", "cl-proj", "--shm-size", "1g", "debian:12"}, true, true, rcNow); err != nil {
		t.Fatal(err)
	}
	calls := strings.Join(r.calls(), "\n")
	order := []string{"stop", "rename cl-proj cl-proj-old-20261127", "create", "start", "rm"}
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
	if !strings.Contains(calls, "rm cl-proj-old-20261127") {
		t.Errorf("l'ancien conteneur n'est pas supprimé :\n%s", calls)
	}
}

func TestE1cEchecDeCreationRetourArriere(t *testing.T) {
	for _, running := range []bool{true, false} {
		r := rcTools(t, running)
		_ = os.WriteFile(filepath.Join(r.dir, "failcreate"), nil, 0o644)
		e := Engine{rcBackend(t)}
		err := e.RecreateContainer("cl-proj", []string{"create", "--name", "cl-proj", "debian:12"}, true, running, rcNow)
		if err == nil || strings.Contains(err.Error(), "ANCIEN-SECRET") {
			t.Errorf("running=%v : erreur nommée attendue : %v", running, err)
		}
		calls := strings.Join(r.calls(), "\n")
		if !strings.Contains(calls, "rename cl-proj-old-20261127") || !strings.Contains(calls, " cl-proj\n") && !strings.HasSuffix(calls, " cl-proj") {
			t.Errorf("running=%v : l'ancien n'est pas renommé à son nom d'origine :\n%s", running, calls)
		}
		if strings.Contains(calls, "rm cl-proj-old") {
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
		if err := e.RecreateContainer("cl-proj", bad, true, true, rcNow); err == nil {
			t.Errorf("argv %v accepté", bad)
		}
	}
	if len(r.calls()) != 0 {
		t.Errorf("commandes lancées pour des arguments refusés : %v", r.calls())
	}
}
