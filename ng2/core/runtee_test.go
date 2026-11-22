//go:build !windows

package core

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// stdout et stderr écrits en rafale par deux descripteurs : pas de course (go test -race), sortie complète rendue.
func TestRunTeeSansCourse(t *testing.T) {
	script := `i=0; while [ $i -lt 300 ]; do echo "out $i"; echo "err $i" >&2; i=$((i+1)); done; exit 3`
	for n := 0; n < 5; n++ {
		var log bytes.Buffer
		c := exec.Command("sh", "-c", script)
		out, err := runTee(c, &log)
		if err == nil {
			t.Fatal("code de sortie 3 attendu")
		}
		for _, want := range []string{"out 0", "out 299", "err 0", "err 299"} {
			if !strings.Contains(out, want) || !strings.Contains(log.String(), want) {
				t.Fatalf("sortie incomplète (manque %q)", want)
			}
		}
		if c.Stdout != c.Stderr {
			t.Error("stdout et stderr doivent partager UN écrivain")
		}
	}
}
