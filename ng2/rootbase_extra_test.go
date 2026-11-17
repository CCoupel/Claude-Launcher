package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// #165 revue M1 : --root-base sans valeur, vide ou « = » : erreur explicite, code 1, rien créé, la variable n'est pas ignorée.
func TestRootBaseOptionWithoutValueIsAnError(t *testing.T) {
	rbNeedBinary(t)
	for _, args := range [][]string{{"configure", "--root-base"}, {"configure", "--root-base="}, {"configure", "--root-base", ""}} {
		home := t.TempDir()
		bin := rbCopyBin(t, filepath.Join(t.TempDir(), "bin"))
		out, errOut, code := rbRun(t, bin, home, []string{"CL_ROOT_BASE=" + t.TempDir()}, args...)
		if code == 0 || !strings.Contains(errOut+out, "--root-base") {
			t.Errorf("%q : code %d, message %q, attendu une erreur nommant --root-base", args, code, errOut+out)
		}
		if rbExists(filepath.Join(home, ".config")) || rbConfigCreated(out) != "" {
			t.Errorf("%q : une configuration a été créée", args)
		}
	}
}
