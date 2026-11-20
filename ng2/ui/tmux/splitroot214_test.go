package tmux

import "testing"

func TestSplitUnderRootAccepteNomCommencantParPointPoint(t *testing.T) {
	roots := []string{"/r"}
	for dir, want := range map[string]string{"/r": "", "/r/INFRA": "INFRA", "/r/..foo": "..foo", "/r/A/..b": "A/..b"} {
		root, cat, ok := splitUnderRoot(roots, dir)
		if !ok || root != "/r" || cat != want {
			t.Errorf("%s → %q %q %v, attendu catégorie %q", dir, root, cat, ok, want)
		}
	}
	for _, dir := range []string{"/autre", "/", "/r/../x"} {
		if _, _, ok := splitUnderRoot(roots, dir); ok {
			t.Errorf("%s accepté hors racine", dir)
		}
	}
}
