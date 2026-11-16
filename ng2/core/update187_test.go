package core

import "testing"

// #187 (a) : une adresse invalide pour le téléchargement d'init-project.md ne doit pas faire paniquer
// (http.NewRequest en erreur → requête nil) : syncInitProject rend false, le cache est conservé.
func TestSyncInitProjectInvalidURLDoesNotPanic(t *testing.T) {
	old := RawBase
	defer func() { RawBase = old }()
	for _, bad := range []string{"http://exemple\x7f.test", "http://[::1", "://", "http://a b"} {
		RawBase = bad
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("RawBase=%q : panique %v", bad, r)
				}
			}()
			if syncInitProject("abc123") {
				t.Errorf("RawBase=%q : synchronisation annoncée réussie", bad)
			}
		}()
	}
}
