package core

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ── Isolation des tests : jamais d'écriture dans l'environnement réel (#216 enquête GH_TOKEN) ──
//
// Un faux jeton de test s'est retrouvé dans le settings.json réel (CLAUDE_CONFIG_DIR visait le dossier de la session).
// Sous test, toute écriture d'un fichier de configuration ou de secret (settings.json, trust.json, moves.json, config.conf,
// projects.json…) hors d'un dossier temporaire est REFUSÉE et consignée ; le TestMain des paquets échoue si une violation
// a eu lieu (IsolationViolations), même quand l'appelant ignore l'erreur. La variable d'environnement se transmet aux
// sous-processus lancés par les tests (binaire compilé, serveurs).

// TestIsolationVar : « 1 » active la garde dans ce processus et dans ses descendants.
const TestIsolationVar = "CL_TEST_ISOLATION"

// TestIsolation : activée par le TestMain d'un paquet (équivalent de la variable d'environnement, sans l'hériter).
var TestIsolation bool

func isolationOn() bool { return TestIsolation || os.Getenv(TestIsolationVar) == "1" }

var (
	violMu     sync.Mutex
	violations []string
)

// IsolationViolations : écritures refusées par la garde dans ce processus (vide : aucune).
func IsolationViolations() []string {
	violMu.Lock()
	defer violMu.Unlock()
	return append([]string(nil), violations...)
}

// ResetIsolationViolations : vide la liste (tests de la garde elle-même).
func ResetIsolationViolations() {
	violMu.Lock()
	violations = nil
	violMu.Unlock()
}

// guardWrite : nil si l'écriture de path est permise ; sous test, refus si path n'est pas sous le dossier temporaire.
func guardWrite(path string) error {
	if !isolationOn() {
		return nil
	}
	if underTemp(path) {
		return nil
	}
	msg := fmt.Sprintf("écriture refusée sous test hors d'un dossier temporaire : %s", path)
	violMu.Lock()
	seen := false
	for _, v := range violations {
		seen = seen || v == msg
	}
	if !seen {
		violations = append(violations, msg)
	}
	violMu.Unlock()
	sort.Strings(violations)
	return fmt.Errorf("%s", msg)
}

// underTemp : path (son plus proche dossier existant, liens résolus) est sous os.TempDir() résolu.
func underTemp(path string) bool {
	tmp, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		tmp = filepath.Clean(os.TempDir())
	}
	p := filepath.Clean(path)
	if !filepath.IsAbs(p) {
		if a, err := filepath.Abs(p); err == nil {
			p = a
		}
	}
	d := filepath.Dir(p)
	for {
		if _, err := os.Stat(d); err == nil {
			break
		}
		up := filepath.Dir(d)
		if up == d {
			break
		}
		d = up
	}
	if r, err := filepath.EvalSymlinks(d); err == nil {
		d = r
	}
	rel, err := filepath.Rel(tmp, d)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
