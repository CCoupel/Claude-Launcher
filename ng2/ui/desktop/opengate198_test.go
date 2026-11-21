//go:build !windows

package desktop

import (
	"claude-launcher-ng2/core"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// noContainerBackend198 : « aucun backend conteneur » POSÉ (backends.yaml vide), pas supposé — sans lui, un podman / docker du
// poste (runner de CI) fait répondre open() par le choix d'exécution (execChoice) : aucune ouverture, la barrière reste à 0.
func noContainerBackend198(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(core.BackendsPath(), []byte("backends: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if len(core.CtrBackends()) != 0 {
		t.Fatal("un backend conteneur est encore détecté : banc non isolé")
	}
}

func gateN(a *App) int {
	a.openGate.mu.Lock()
	defer a.openGate.mu.Unlock()
	return a.openGate.n
}

// L'ouverture est inscrite dans la barrière DÈS le retour d'open() (avant le démarrage de sa goroutine) : Stop et le délai
// de grâce du pod (#198) l'attendent même lancée à l'instant ; la barrière retombe à zéro quand elle finit, y compris en échec.
func TestOpenInscritLOuvertureAvantLaGoroutine198(t *testing.T) {
	ps := sandbox(t, "", "alpha")
	noContainerBackend198(t)
	fakeClaudeSession214(t)
	real, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux absent")
	}
	bin := t.TempDir()
	// has-session (1ʳᵉ commande de prepareHost) bloque 1 s : l'ouverture est en cours pendant que le test regarde
	script := "#!/bin/sh\nfor a in \"$@\"; do [ \"$a\" = has-session ] && sleep 1; done\nexec " + real + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "tmux"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	var r recorder
	a := newApp(t, r.emit)
	a.rescan()
	if res, err := a.open(ps[0], 0); err != nil || res.(map[string]any)["tab"] == nil {
		t.Fatalf("open : %v %v (une ouverture, pas un choix d'exécution)", res, err)
	}
	if n := gateN(a); n != 1 {
		t.Fatalf("barrière juste après open() = %d, attendu 1 (ouverture lancée, pas encore commencée)", n)
	}
	if a.waitOpenings(50 * time.Millisecond) {
		t.Error("waitOpenings a rendu la main alors que l'ouverture est en cours")
	}
	if !a.waitOpenings(20 * time.Second) {
		t.Fatal("l'ouverture ne finit pas")
	}
	if n := gateN(a); n != 0 {
		t.Errorf("barrière après l'ouverture = %d, attendu 0", n)
	}
}

// Ouverture refusée (onglet déjà ouvert) ou en échec : pas de begin sans end.
func TestOpenBarriereRetombeSurTousLesChemins198(t *testing.T) {
	ps := sandbox(t, "", "alpha")
	noContainerBackend198(t)
	fakeClaudeSession214(t)
	var r recorder
	a := newApp(t, r.emit)
	a.rescan()
	for i := 0; i < 2; i++ { // 2ᵉ appel : onglet déjà ouvert, rien n'est inscrit
		if _, err := a.open(ps[0], 0); err != nil {
			t.Fatal(err)
		}
	}
	if !a.waitOpenings(20 * time.Second) {
		t.Fatal("l'ouverture ne finit pas")
	}
	if n := gateN(a); n != 0 {
		t.Errorf("barrière = %d, attendu 0", n)
	}
}
