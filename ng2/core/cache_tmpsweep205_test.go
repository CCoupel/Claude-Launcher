//go:build statetmp

package core

// #205 : balayage au démarrage des « *.json.*.tmp » orphelins de stateDir(). Build tag `statetmp` : le point d'entrée n'existait pas
// à l'écriture (dev-backend codait en parallèle). Retirer le tag une fois posé ; noms ATTENDUS (à ajuster au code final) :
//
//	func sweepStateTmp()                       // appelé une fois au démarrage du suivi d'état, erreurs ignorées
//	var stateTmpMaxAge = tun.Of(24 * time.Hour) // âge réglable pour les tests

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func touchAge205(t *testing.T, name string, age time.Duration) string {
	t.Helper()
	p := filepath.Join(stateDir(), name)
	if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	mt := time.Now().Add(-age)
	if err := os.Chtimes(p, mt, mt); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBalayageTmpVieuxSupprimeRecentGarde205(t *testing.T) {
	t.Setenv(CacheDirVar, t.TempDir())
	vieux := touchAge205(t, "abc123.json.111.tmp", 25*time.Hour)
	limite := touchAge205(t, "abc123.json.222.tmp", 23*time.Hour) // écriture concurrente d'un autre launcher : gardé
	recent := touchAge205(t, "abc123.json.333.tmp", time.Minute)
	etat := touchAge205(t, "abc123.json", 48*time.Hour) // état réel, même vieux : jamais touché
	autre := touchAge205(t, "notes.tmp", 48*time.Hour)  // pas un « .json.*.tmp » : jamais touché
	sweepStateTmp()
	for p, want := range map[string]bool{vieux: false, limite: true, recent: true, etat: true, autre: true} {
		if _, err := os.Stat(p); (err == nil) != want {
			t.Errorf("%s : présent=%v, attendu %v", filepath.Base(p), err == nil, want)
		}
	}
}

func TestBalayageTmpAgeReglableEtErreursIgnorees205(t *testing.T) {
	t.Setenv(CacheDirVar, t.TempDir())
	old := stateTmpMaxAge.Set(time.Hour)
	defer stateTmpMaxAge.Set(old)
	p := touchAge205(t, "x.json.1.tmp", 2*time.Hour)
	sweepStateTmp()
	if _, err := os.Stat(p); err == nil {
		t.Error("âge réglé à 1 h : fichier de 2 h conservé")
	}
	sweepStateTmp() // dossier déjà balayé : aucune erreur, aucun effet
	_ = os.RemoveAll(stateDir())
	sweepStateTmp() // dossier absent : sans panique
}
