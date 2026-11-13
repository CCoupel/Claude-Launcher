package desktop

import (
	"errors"
	"fmt"
	"os"
	"time"

	"claude-launcher-ng2/core"
)

// ── Éditeur des fichiers de configuration (#117) ──────────────────────────
//
// Texte brut, validé par core (même code que le chargement réel) avant
// l'enregistrement ; .bak, écriture atomique et détection des modifications
// extérieures par core.SaveConfig ; relecture à chaud de ce qui peut l'être.

// ConfigDoc : un fichier ouvert dans l'éditeur.
type ConfigDoc struct {
	core.ConfigFile
	Content  string               `json:"content"`
	MTime    string               `json:"mtime"` // date à l'ouverture (RFC 3339, ns) ; vide : absent
	Problems []core.ConfigProblem `json:"problems"`
	Merged   string               `json:"merged,omitempty"`   // projet : env fusionné (lecture seule)
	TplText  string               `json:"tplText,omitempty"`  // projet : env-template.yaml (lecture seule)
	FromTemp bool                 `json:"fromTemp,omitempty"` // contenu proposé (fichier absent)
}

func (a *App) configFile(kind core.ConfigKind, path, project string) (core.ConfigFile, error) {
	for _, f := range core.ConfigFiles(project) {
		if f.Kind == kind && (path == "" || f.Path == path) {
			return f, nil
		}
	}
	return core.ConfigFile{}, fmt.Errorf("fichier de configuration inconnu : %s %s", kind, path)
}

// configOpen : contenu du fichier (modèle par défaut s'il est absent).
func (a *App) configOpen(kind core.ConfigKind, project string) (ConfigDoc, error) {
	f, err := a.configFile(kind, "", project)
	if err != nil {
		return ConfigDoc{}, err
	}
	d := ConfigDoc{ConfigFile: f}
	if f.Exists {
		b, err := os.ReadFile(f.Path)
		if err != nil {
			return d, err
		}
		d.Content, d.MTime = string(b), f.ModTime.Format(time.RFC3339Nano)
	} else {
		d.Content, d.FromTemp = core.ConfigTemplate(kind), true
		if kind == core.ConfConf {
			if b, err := os.ReadFile(core.ConfigPath()); err == nil { // apparu entre-temps
				d.Content = string(b)
			}
		}
	}
	d.Problems, d.Merged = a.configCheck(kind, project, d.Content)
	if f.Template != "" {
		if b, err := os.ReadFile(f.Template); err == nil {
			d.TplText = string(b)
		}
	}
	return d, nil
}

// configCheck : problèmes du contenu, et pour un projet l'environnement fusionné.
func (a *App) configCheck(kind core.ConfigKind, project, content string) ([]core.ConfigProblem, string) {
	if kind == core.ConfProjectEnv {
		ps := core.ValidateProjectEnv(project, []byte(content))
		m, err := core.MergedProjectEnv(project, []byte(content))
		if err != nil {
			m = ""
		}
		return ps, m
	}
	return core.ValidateConfig(kind, []byte(content)), ""
}

// configSave : enregistre ; conflit : rend {conflict, mtime actuel} pour
// proposer de recharger ou d'écraser.
func (a *App) configSave(kind core.ConfigKind, project, content, mtime string) (map[string]any, error) {
	f, err := a.configFile(kind, "", project)
	if err != nil {
		return nil, err
	}
	var expected time.Time
	if mtime != "" {
		if expected, err = time.Parse(time.RFC3339Nano, mtime); err != nil {
			return nil, fmt.Errorf("date d'ouverture invalide : %v", err)
		}
	}
	ps, err := core.SaveConfig(kind, f.Path, []byte(content), expected)
	switch {
	case errors.Is(err, core.ErrConfigConflict):
		cur := ""
		if st, e := os.Stat(f.Path); e == nil {
			cur = st.ModTime().Format(time.RFC3339Nano)
		}
		return map[string]any{"conflict": true, "mtime": cur, "problems": ps}, nil
	case errors.Is(err, core.ErrConfigInvalid):
		return map[string]any{"invalid": true, "problems": ps}, nil
	case err != nil:
		return nil, err
	}
	note := a.configReload(kind)
	st, _ := os.Stat(f.Path)
	out := map[string]any{"saved": true, "problems": ps, "note": note}
	if st != nil {
		out["mtime"] = st.ModTime().Format(time.RFC3339Nano)
	}
	a.setMsg("✓ " + f.Path + " enregistré (copie .bak)")
	return out, nil
}

// conf : configuration courante (relue à chaud après un enregistrement de
// config.conf).
func (a *App) conf() core.Config {
	a.confMu.RLock()
	defer a.confMu.RUnlock()
	return a.cfg
}

// configReload : ce qui est relu sans redémarrer.
func (a *App) configReload(kind core.ConfigKind) string {
	switch kind {
	case core.ConfConf:
		cfg := core.LoadConfig()
		a.confMu.Lock()
		cfg.Roots = a.cfg.Roots // la surveillance de l'arborescence garde ses racines
		a.cfg = cfg
		a.confMu.Unlock()
		go a.readUsage()
		a.changed()
		a.cardsChanged()
	case core.ConfBackends:
		go a.readCtr()
		a.changed()
	}
	return core.ConfigReloadNote(kind)
}
