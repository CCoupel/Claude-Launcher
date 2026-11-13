package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ── Démarrage de claude dans un projet ────────────────────────────────────
//
// Commandes tapées dans le shell du projet (hôte) ou script exécuté par le bash
// du tmux de son conteneur : questions /init-project (template en retard ou
// absent) et reprise de la dernière conversation, posées dans le terminal du
// projet, puis claude et le marqueur de sortie propre. Communes à la coque tmux
// (ui/tmux/launch.go) et à l'interface native (ui/desktop).

// LaunchMode : ouverture d'un projet, ou juste après sa création depuis le
// launcher — géré (/init-project d'office) ou brut (ni git ni init-project).
type LaunchMode int

const (
	LaunchNormal LaunchMode = iota
	LaunchNewManaged
	LaunchNewRaw
)

// ClaudeBin : commande lancée dans chaque projet (CL_CLAUDE_BIN : remplaçant de test).
func ClaudeBin() string {
	if b := os.Getenv("CL_CLAUDE_BIN"); b != "" {
		return Shq(b)
	}
	return "claude"
}

// StartCommands : démarrage de claude (start) et copie d'init-project.md dans
// le projet (copyInit, vide pour un projet brut tout juste créé). Dépose aussi
// /end-session dans un projet non géré.
func StartCommands(cfg Config, path, latest string, mode LaunchMode) (start, copyInit string) {
	initCache := InitProjectPath()
	tpl := ReadTemplate(path, latest)

	var initPre, initArg string
	switch {
	case mode == LaunchNewManaged:
		initArg = " /init-project" // projet tout juste créé : pas de question
	case mode == LaunchNewRaw: // ni question ni /init-project
	case tpl == TplOutdated:
		q := "↑ template en retard : projet " + ProjectTemplateRef(path) + " → disponible " + TemplateRef(ReadTrim(TplVersionPath()), latest) +
			" — lancer /init-project ? [o/N] "
		initPre = `_ip_arg=''; read -rp ` + Shq(q) + ` _r; [[ "$_r" == [oOyY]* ]] && _ip_arg='/init-project'; `
		initArg = " $_ip_arg"
	case tpl == TplMissing:
		q := "🗑 fichiers template manquants (clone frais) : projet " + ProjectTemplateRef(path) + ", disponible " + TemplateRef(ReadTrim(TplVersionPath()), latest) +
			" — lancer /init-project pour restaurer ? [O/n] "
		initPre = `_ip_arg='/init-project'; read -rp ` + Shq(q) + ` _r; [[ "$_r" == [nN]* ]] && _ip_arg=''; `
		initArg = " $_ip_arg"
	}

	// init-project.md est déposé dans le projet, sauf pour un projet brut tout juste créé
	copyInit = fmt.Sprintf("mkdir -p .claude/commands\n_ip=.claude/commands/init-project.md\n"+
		"[[ -s %s ]] && cp %s \"$_ip\" || echo '⚠  init-project.md non disponible — relancer le launcher connecté'\n",
		Shq(initCache), Shq(initCache))
	if mode == LaunchNewRaw {
		copyInit = ""
	}
	start = initPre + ClaudeBin() + " " + cfg.ClaudeOptions + initArg

	// Reprise de la dernière conversation (resume.go) : si elle est choisie, ni
	// question ni /init-project, qui viendraient perturber la conversation reprise.
	if mode == LaunchNormal {
		if pre, r, ok := cfg.ResumeShell(path); ok {
			resume := ClaudeBin() + " " + cfg.ClaudeOptions + " --resume " + Shq(r.ID)
			if p := ResumePrompt(r); p != "" {
				resume += " " + Shq(p)
			}
			start = pre + "if [[ -n $_rs ]]; then " + resume + "; else " + start + "; fi"
		}
	}
	// sortie propre : marqueur posé seulement si claude rend la main au shell
	start += "; " + CleanExitShell(path)
	// projet non géré : /end-session minimal, pour pouvoir le clore comme les autres
	if mode == LaunchNewRaw || mode == LaunchNormal && tpl == TplNone {
		EnsureEndSession(path)
	}
	return start, copyInit
}

// HostStartKeys : ce qui est tapé dans le shell du projet sur l'hôte.
func HostStartKeys(cfg Config, path, start, copyInit string, synced bool) string {
	return fmt.Sprintf("cd %s%s\n%s%s", Shq(path), cfg.Exports(!synced), copyInit, start)
}

// CtrStartScript : même démarrage qu'en local, exécuté par le bash du tmux du
// conteneur ; init-project.md est copié depuis l'hôte (le cache n'est pas monté).
func CtrStartScript(cfg Config, path, start string, mode LaunchMode, synced bool) string {
	if mode != LaunchNewRaw {
		if b, err := os.ReadFile(InitProjectPath()); err == nil && len(b) > 0 {
			d := filepath.Join(path, ".claude", "commands")
			if os.MkdirAll(d, 0o755) == nil {
				_ = os.WriteFile(filepath.Join(d, "init-project.md"), b, 0o644)
			}
		}
	}
	ctrStart := strings.ReplaceAll(start, ClaudeBin()+" ", "claude ")
	if cfg.CtrStopOnExit {
		// sortie propre : la session du conteneur se termine, et ctr-attach
		// (resté parent de l'attache) arrête le conteneur
		ctrStart += " && tmux kill-session"
	}
	return fmt.Sprintf("cd %s; export PATH=\"$HOME/.local/bin:$PATH\"%s\n%s", Shq(RemotePath(path)), cfg.Exports(!synced), ctrStart)
}

// EndSessionCmd : /end-session déposé dans un projet non géré (le template, lui,
// apporte le sien). Le taper laisse dans la conversation la marque que le
// navigateur reconnaît (resume.go) : la session n'est plus proposée à la reprise.
const EndSessionCmd = `---
description: Clore la session (déposé par Claude-Launcher dans un projet non géré)
---

Fin de session. Sans modifier aucun fichier ni lancer de commande, résume en
quelques lignes ce qui a été fait pendant la session et ce qui reste à faire,
puis indique que la session peut être quittée (/exit).
`

// EnsureEndSession : dépose .claude/commands/end-session.md s'il n'existe pas.
func EnsureEndSession(path string) {
	f := filepath.Join(path, ".claude", "commands", "end-session.md")
	if _, err := os.Stat(f); err == nil {
		return
	}
	if os.MkdirAll(filepath.Dir(f), 0o755) == nil {
		_ = os.WriteFile(f, []byte(EndSessionCmd), 0o644)
	}
}

// SyncLauncherEnv : recopie des variables du launcher dans settings.json ;
// false si elle a échoué (les secrets sont alors tapés comme avant).
func SyncLauncherEnv(cfg Config) bool {
	_ = SyncBrowserHook(ClaudeDir()) // consigne navigateur à chaque session (#106)
	return SyncSettingsEnv(ClaudeDir(), cfg.EnvVars()) == nil
}
