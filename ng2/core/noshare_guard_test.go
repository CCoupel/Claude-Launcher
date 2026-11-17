package core

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Garde #166 : aucun littéral de chaîne du code exécuté ne porte le chemin d'origine du projet (/share/MFS) —
// ni messages, ni exemples, ni valeurs de repli : ils se déduisent de la configuration réelle ou restent neutres.
// Les commentaires et les fichiers de test sont hors du contrôle.
func TestNoShareMFSInExecutedStrings(t *testing.T) {
	root := ".."
	fset := token.NewFileSet()
	var bad []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "node_modules" || d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if perr != nil {
			t.Errorf("%s : %v", p, perr)
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if s, e := strconv.Unquote(lit.Value); e == nil && (strings.Contains(s, "/share/MFS") || strings.Contains(s, `share\MFS`)) {
					bad = append(bad, fset.Position(lit.Pos()).String())
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bad {
		t.Errorf("chemin d'origine /share/MFS dans un littéral du code exécuté : %s", b)
	}
}
