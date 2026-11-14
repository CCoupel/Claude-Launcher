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
	go a.setMsg("✓ " + f.Path + " enregistré (copie .bak)") // message de pied : sans attendre a.mu
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
		// avertissements de hubs: relus : une faute corrigée (ou introduite) se voit
		// tout de suite, pas seulement au prochain démarrage
		// hors de la réponse : a.mu peut être tenu par une lecture lente, l'écran
		// Réglages n'a pas à l'attendre
		a.storeBackends()
		go func() {
			w := core.HubConfigWarnings()
			a.mu.Lock()
			a.hubWarns = w
			a.mu.Unlock()
			a.changed()
		}()
		go a.readCtr()
		a.changed()
	}
	return core.ConfigReloadNote(kind)
}

// ── Écriture structurée (#118, LOT-3.1 : contracts/desktop-dispatch.md) ───

// configModel : backends.yaml lu en modèle (backends et pools dans l'ordre du
// fichier, défauts, images) avec les problèmes de validation et la date
// d'ouverture, pour l'écran Réglages. Fichier absent : modèle vide.
func (a *App) configModel(kind core.ConfigKind, project string) (map[string]any, error) {
	if kind != core.ConfBackends {
		return nil, fmt.Errorf("pas de modèle structuré pour %s (voir « Éditer en YAML »)", kind)
	}
	d, err := a.configOpen(kind, project)
	if err != nil {
		return nil, err
	}
	content := d.Content
	if d.FromTemp {
		content = "" // fichier absent : l'exemple commenté n'est pas une configuration
	}
	model, merr := core.ReadBackendsModel([]byte(content))
	if merr != nil && len(d.Problems) == 0 {
		d.Problems = []core.ConfigProblem{{Severity: core.SevError, Message: merr.Error()}}
	}
	if d.Problems == nil {
		d.Problems = []core.ConfigProblem{}
	}
	return map[string]any{"path": d.Path, "mtime": d.MTime, "exists": d.Exists, "problems": d.Problems, "model": model}, nil
}

// configApply : applique des changements structurés au texte du fichier
// (commentaires et ordre conservés), puis le même enregistrement que
// l'éditeur de texte : validation de core, contrôle de conflit, copie .bak.
// Résultat : celui de configSave.
func (a *App) configApply(kind core.ConfigKind, project, mtime string, changes []core.ConfigChange) (map[string]any, error) {
	f, err := a.configFile(kind, "", project)
	if err != nil {
		return nil, err
	}
	content := ""
	if b, err := os.ReadFile(f.Path); err == nil {
		content = string(b)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	out, err := core.ApplyConfigChanges(kind, []byte(content), changes)
	if err != nil {
		return map[string]any{"invalid": true, "problems": []core.ConfigProblem{{Severity: core.SevError, Message: err.Error()}}}, nil
	}
	return a.configSave(kind, project, string(out), mtime)
}

// backendTest : étapes du test d'un backend du formulaire (pas forcément
// enregistré) ; ses pools sont ceux du fichier.
func (a *App) backendTest(be core.BackendEntry) map[string]any {
	var pools []core.PoolEntry
	if b, err := os.ReadFile(core.BackendsPath()); err == nil {
		if m, err := core.ReadBackendsModel(b); err == nil {
			pools = m.Pools
		}
	}
	conf, err := be.Conf(pools)
	if err != nil {
		return map[string]any{"steps": []core.TestStep{{Label: "configuration du backend", Detail: err.Error(),
			Fix: "corriger le formulaire (les mêmes règles s'appliquent à l'enregistrement)"}}}
	}
	return map[string]any{"steps": core.TestBackend(conf)}
}

// backendStates : état de chaque backend et charge des pools, depuis le cache
// du diagnostic et la dernière lecture des conteneurs ; ne bloque pas.
func (a *App) backendStates() map[string]any {
	// instantanés : ni a.mu ni lecture du fichier sur le chemin de la réponse
	var bs []core.BackendConf
	if p := a.bkSnap.Load(); p != nil {
		bs = *p
	} else {
		bs, _ = core.LoadBackends() // avant la première lecture des conteneurs
		a.bkSnap.Store(&bs)
	}
	var founds []core.Found
	if p := a.ctrFounds.Load(); p != nil {
		founds = *p
	}
	return map[string]any{"states": core.BackendStates(bs), "load": core.PoolLoads(bs, founds)}
}
