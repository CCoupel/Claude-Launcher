package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Question d'ouverture : versions du template du projet et disponible.
func TestProjectTemplateRef(t *testing.T) {
	d := t.TempDir()
	if got := ProjectTemplateRef(d); got != "version inconnue" {
		t.Fatalf("sans template : %q", got)
	}
	_ = os.MkdirAll(filepath.Join(d, ".claude"), 0o755)
	_ = os.WriteFile(filepath.Join(d, ".claude", "project-config.json"), []byte(`{"template_version":{"tag":"v4.1.0","commit":"e925929172577350"}}`), 0o644)
	if got := ProjectTemplateRef(d); got != "v4.1.0 (e925929)" {
		t.Fatalf("project-config : %q", got)
	}
	_ = os.MkdirAll(filepath.Join(d, "TEMPLATE_claude"), 0o755)
	_ = os.WriteFile(filepath.Join(d, "TEMPLATE_claude", ".template-source.json"), []byte(`{"tag":"v3.28.0","commit":"8f583bf7a54085e3"}`), 0o644)
	if got := ProjectTemplateRef(d); got != "v3.28.0 (8f583bf)" {
		t.Fatalf("template-source : %q", got)
	}
	if TemplateRef("", "abcdef0123") != "abcdef0" || TemplateRef("v5.5.0", "") != "v5.5.0" {
		t.Fatal("templateRef")
	}
}
