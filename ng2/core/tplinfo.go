package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// État du template par teammate (carte TEAM, #136) : version enregistrée de
// l'agent (agents.registry[].tag de .claude/project-config.json) comparée à la
// version cible (tag du template adopté par le launcher, .template-version),
// et compagnon projet. Même règle pour la coque Linux et l'interface native.

// Tpl* : TeamLine.TplState.
const (
	TplLineNone = ""     // projet sans registre, version ou cible inconnue : indicateur neutre
	TplLineOK   = "ok"   // copie à jour (version enregistrée = cible)
	TplLineLate = "late" // template de l'agent en retard
)

// TplInfo : ce que la carte sait du template du projet.
type TplInfo struct {
	Managed bool              // le projet porte un registre d'agents (agents.registry)
	Target  string            // version cible ; "" si inconnue
	Files   map[string]string // nom d'agent → fichier compagnon existant (.claude/agents/…)
}

type tplEntry struct {
	at      time.Time
	managed bool
	files   map[string]string
}

var tplCache = struct {
	sync.Mutex
	m map[string]tplEntry
}{m: map[string]tplEntry{}}

// ReadTplInfo : état du template du projet. Les fichiers du projet (config,
// compagnons) sont relus au plus toutes les 60 s ; la version cible (petit
// fichier du cache du launcher) à chaque appel.
func ReadTplInfo(proj string) TplInfo {
	tplCache.Lock()
	e, ok := tplCache.m[proj]
	tplCache.Unlock()
	if !ok || time.Since(e.at) >= time.Minute {
		e = tplEntry{at: time.Now(), files: map[string]string{}}
		var cfg struct {
			Agents struct {
				Registry []struct{ Name string } `json:"registry"`
				Generic  []struct{ Name string } `json:"generic"`
			} `json:"agents"`
		}
		if b, err := os.ReadFile(filepath.Join(proj, ".claude", "project-config.json")); err == nil && json.Unmarshal(b, &cfg) == nil {
			e.managed = len(cfg.Agents.Registry) > 0
		}
		agents := filepath.Join(proj, ".claude", "agents")
		has := func(f string) bool {
			st, err := os.Stat(filepath.Join(agents, f))
			return err == nil && st.Size() > 0
		}
		for _, r := range cfg.Agents.Registry {
			if r.Name == "generic" && len(cfg.Agents.Generic) > 0 {
				for _, g := range cfg.Agents.Generic {
					if f := "generic." + g.Name + ".md"; has(f) {
						e.files[g.Name] = f
					}
				}
				continue
			}
			if f := r.Name + ".md"; has(f) {
				e.files[r.Name] = f
			}
		}
		tplCache.Lock()
		tplCache.m[proj] = e
		tplCache.Unlock()
	}
	return TplInfo{Managed: e.managed, Target: ReadTrim(TplVersionPath()), Files: e.files}
}

// Apply complète les lignes de la carte : état du template, version cible et
// fichier compagnon. Sans registre, les lignes restent neutres.
func (ti TplInfo) Apply(lines []TeamLine) []TeamLine {
	for i := range lines {
		l := &lines[i]
		name := l.Name
		if name == "leader" {
			name = "teamleader"
		}
		l.TplTarget = ti.Target
		l.CompanionFile = ti.Files[name]
		l.Companion = l.Companion || l.CompanionFile != ""
		switch {
		case !ti.Managed || l.Tag == "" || ti.Target == "":
			l.TplState = TplLineNone
		case l.Tag == ti.Target:
			l.TplState = TplLineOK
		default:
			l.TplState = TplLineLate
		}
	}
	return lines
}
