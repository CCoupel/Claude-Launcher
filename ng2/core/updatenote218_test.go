package core

import "testing"

func TestNoteDeMiseAJourReprise218(t *testing.T) {
	t.Setenv(CacheDirVar, t.TempDir())
	if n := TakeUpdateNote(); n != "" {
		t.Errorf("note initiale %q", n)
	}
	SaveUpdateNote("sans sauvegarde : disque plein")
	if n := TakeUpdateNote(); n != "sans sauvegarde : disque plein" {
		t.Errorf("note = %q", n)
	}
	if n := TakeUpdateNote(); n != "" {
		t.Errorf("note non vidée : %q", n)
	}
}
