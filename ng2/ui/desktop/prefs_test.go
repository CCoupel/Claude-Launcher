package desktop

import (
	"encoding/json"
	"os"
	"testing"
)

// #123, #124 : préférences d'affichage dans le cache du launcher (relues au
// lancement suivant), fusionnées clé par clé, clé à null supprimée.
func TestPrefsPersisted(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	if _, err := a.Dispatch("setPrefs", json.RawMessage(`{"collapsed":["/r/INFRA"],"navW":420}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Dispatch("setPrefs", json.RawMessage(`{"treeH":300,"navW":null}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(prefsPath()); err != nil {
		t.Fatalf("fichier : %v", err)
	}
	b := newApp(t, r.emit) // lancement suivant
	res, _ := b.Dispatch("init", nil)
	p := res.(map[string]any)["prefs"].(map[string]any)
	if c, _ := p["collapsed"].([]any); len(c) != 1 || c[0] != "/r/INFRA" || p["treeH"] != 300.0 || p["navW"] != nil {
		t.Errorf("préférences relues : %v", p)
	}
}
