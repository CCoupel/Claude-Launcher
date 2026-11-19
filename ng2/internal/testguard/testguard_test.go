package testguard

import (
	"os"
	"path/filepath"
	"testing"
)

// La garde voit une écriture, une modification et une suppression dans le dossier surveillé.
func TestTakeDetectsChanges(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, ".template-version")
	a := take(d)
	if err := os.WriteFile(p, []byte("v5.9.2"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := take(d)
	if a[".template-version"] == b[".template-version"] {
		t.Error("création non détectée")
	}
	_ = os.WriteFile(p, []byte("v5.9.3"), 0o644)
	if c := take(d); c[".template-version"] == b[".template-version"] {
		t.Error("modification non détectée")
	}
	_ = os.Remove(p)
	if c := take(d); c[".template-version"] != a[".template-version"] {
		t.Error("suppression : retour à l'état initial attendu")
	}
}
