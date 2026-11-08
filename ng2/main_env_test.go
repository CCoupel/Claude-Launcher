package main

import (
	"os"
	"testing"
)

// TestMain : la config claude de la session (CLAUDE_CONFIG_DIR, souvent sur le
// stockage partagé) n'est jamais celle des tests — un test qui ne la redéfinit
// pas écrirait dans le vrai cache de consommation commun (usage.go).
func TestMain(m *testing.M) {
	d, err := os.MkdirTemp("", "ng2-test-claude-")
	if err != nil {
		panic(err)
	}
	os.Setenv("CLAUDE_CONFIG_DIR", d)
	code := m.Run()
	os.RemoveAll(d)
	os.Exit(code)
}
