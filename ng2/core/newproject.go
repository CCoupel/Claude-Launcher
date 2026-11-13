package core

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ApplyImage : socle img pour le projet — env.yaml modifié (ou créé) seulement
// si l'environnement fusionné n'a pas déjà ce socle.
func ApplyImage(dir, img string) error {
	if e, err := LoadEnv(dir); err == nil && e.Image == img {
		return nil
	}
	return setEnvImage(dir, img) // absent, sans image valable, ou autre socle
}

// ApplyExec : affectation du projet — img vide : sur l'hôte ; sinon le socle
// img crée env.yaml, ou change sa ligne image: s'il existe déjà (hôte :
// env.yaml gardé), et le projet est affecté au backend conteneur be.
func ApplyExec(dir, img, be string) error {
	if img == "" {
		return WriteAssignment(dir, "host")
	}
	if err := ApplyImage(dir, img); err != nil {
		return err
	}
	return WriteAssignment(dir, be)
}

// ValidProjectName : "" si s est un nom de projet acceptable, sinon la raison.
func ValidProjectName(s string) string {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		return "nom requis"
	case strings.ContainsAny(s, `/\`) || strings.HasPrefix(s, "."):
		return "nom invalide (ni / ni . au début)"
	}
	return ""
}

// InitProject : contenu d'un projet tout juste créé (dossier dir existant) —
// brut : un CLAUDE.md minimal ; géré : git init et le remote origin s'il est
// donné. L'erreur porte l'étape (« git init : … »).
func InitProject(dir string, raw bool, remote string) error {
	if raw {
		if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(MinimalClaudeMD(filepath.Base(dir))), 0o644); err != nil {
			return errors.New("CLAUDE.md : " + err.Error())
		}
		return nil
	}
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		return errors.New("git init : " + strings.TrimSpace(string(out)))
	}
	if r := strings.TrimSpace(remote); r != "" {
		if out, err := exec.Command("git", "-C", dir, "remote", "add", "origin", r).CombinedOutput(); err != nil {
			return errors.New("git remote : " + strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// MinimalClaudeMD : contenu du CLAUDE.md d'un projet brut.
func MinimalClaudeMD(name string) string {
	return "# " + name + "\n\n" +
		"Projet créé le " + time.Now().Format("2006-01-02") + " par " + NgName + ".\n\n" +
		"## Objectif\n\n_À compléter._\n"
}
