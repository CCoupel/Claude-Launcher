package desktop

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// #270 (Windows) — le nom du dossier du bac à sable ne dérive jamais tel quel du nom du test : les caractères interdits sous Windows
// (« <>:"/\|?* »), les noms réservés, les noms très longs ou vides ne doivent produire qu'un motif sûr. Vérifié ICI, sous Linux : un
// nom de sous-test pathologique aurait fait échouer la CI Windows sans que rien ne le signale à Linux.
func TestSandboxDirPatternIsSafeOnWindows270(t *testing.T) {
	safe := regexp.MustCompile(`^cl-sbx-[A-Za-z0-9_-]*-$`)
	for _, name := range []string{
		"", " ", ".", "..", "TestPublicURLCookieSecure/url=https://x:y/chemin_(a|b)", `<>:"/\|?*`, "CON", "NUL", "COM1", "aux.txt", "LPT9",
		"TestX/" + strings.Repeat("très-long ", 40), "é/ü/日本語", "a b\tc\nd", "nom.avec.points.", "fin avec espace ", "https://launcher.test/chemin",
		"url=https://launcher.test:8443", "Test/sous-test #01", "a%2Fb", "\x00\x01", strings.Repeat("a", 5000),
	} {
		p := sandboxDirPattern(name)
		if !safe.MatchString(p) {
			t.Errorf("nom %.40q : motif %q hors de ^cl-sbx-[A-Za-z0-9_-]*-$", name, p)
		}
		if len(p) > len("cl-sbx-")+24+1 {
			t.Errorf("nom %.40q : motif de %d caractères (trop long pour un chemin Windows court)", name, len(p))
		}
		if strings.ContainsAny(p, `<>:"/\|?* .`) {
			t.Errorf("nom %.40q : motif %q contient un caractère interdit ou final problématique", name, p)
		}
		d, err := os.MkdirTemp("", p)
		if err != nil {
			t.Errorf("nom %.40q : MkdirTemp(%q) : %v", name, p, err)
			continue
		}
		if strings.ContainsAny(filepath.Base(d), `<>:"/\|?*`) || strings.HasSuffix(filepath.Base(d), ".") || strings.HasSuffix(filepath.Base(d), " ") {
			t.Errorf("nom %.40q : dossier %q invalide sous Windows", name, d)
		}
		_ = os.RemoveAll(d)
	}
	// deux bacs à sable de même nom ne se heurtent pas
	a, b := sandboxDirPattern("x"), sandboxDirPattern("x")
	d1, _ := os.MkdirTemp("", a)
	d2, _ := os.MkdirTemp("", b)
	if d1 == d2 || d1 == "" {
		t.Errorf("dossiers identiques pour un même nom : %q %q", d1, d2)
	}
	_ = os.RemoveAll(d1)
	_ = os.RemoveAll(d2)
}

// Aucun autre chemin de fichier des tests ne dérive de t.Name() / du nom d'un sous-test (hors t.TempDir, qui assainit lui-même) : relecture
// du source — un nouveau motif os.MkdirTemp(…, t.Name()…) ou filepath.Join(…, t.Name()…) casserait Windows sans signal sous Linux.
func TestNoPathDerivedFromTestNameRaw270(t *testing.T) {
	files, _ := filepath.Glob("*_test.go")
	bad := regexp.MustCompile(`(MkdirTemp|Join|Mkdir|MkdirAll|WriteFile|Create)\(.*t\.Name\(\)`)
	for _, f := range files {
		if f == "sandboxname270_test.go" {
			continue
		}
		b, _ := os.ReadFile(f)
		for i, l := range strings.Split(string(b), "\n") {
			if bad.MatchString(l) && !strings.Contains(l, "sandboxDirPattern(t.Name())") {
				t.Errorf("%s:%d : chemin dérivé de t.Name() sans assainissement : %s", f, i+1, strings.TrimSpace(l))
			}
		}
	}
}
