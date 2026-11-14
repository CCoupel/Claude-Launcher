package desktop

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	if kind == core.ConfConf {
		ps = maskProblems(ps, core.ConfTokenValue(content)) // le jeton n'apparaît dans aucun message
	}
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
		a.refreshHubWarns(core.HubConfigWarnings)
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
	switch kind {
	case core.ConfBackends, core.ConfConf, core.ConfLauncherEnv, kindHub:
	default:
		return nil, fmt.Errorf("pas de modèle structuré pour %s (voir « Éditer en YAML »)", kind)
	}
	if kind != core.ConfBackends {
		project = "" // fichiers du poste : jamais un chemin fourni par le client
	}
	fileKind := kind
	if kind == kindHub {
		fileKind = core.ConfBackends // « Ce poste » est une section de backends.yaml
	}
	d, err := a.configOpen(fileKind, project)
	if err != nil {
		return nil, err
	}
	content := d.Content
	if d.FromTemp {
		content = "" // fichier absent : l'exemple commenté n'est pas une configuration
	}
	var model any
	var merr error
	switch kind {
	case core.ConfBackends:
		model, merr = core.ReadBackendsModel([]byte(content))
	case core.ConfConf:
		model = core.ReadConfModel(content) // le jeton n'y figure jamais
		d.Problems = maskProblems(d.Problems, core.ConfTokenValue(content))
	case core.ConfLauncherEnv:
		model, merr = core.ReadLauncherEnvModel([]byte(content))
	case kindHub:
		var hm core.HubModel
		if hm, merr = core.ReadHubModel([]byte(content), core.HubHost(), core.CurrentGOOS()); merr == nil {
			size := 13
			if n, ok := loadPrefs()["termFontSize"].(float64); ok {
				size = int(n)
			}
			model = struct {
				core.HubModel
				TermFontSize int `json:"termFontSize"`
			}{hm, size}
		}
	}
	if merr != nil && len(d.Problems) == 0 {
		d.Problems = []core.ConfigProblem{{Severity: core.SevError, Message: merr.Error()}}
	}
	if d.Problems == nil {
		d.Problems = []core.ConfigProblem{}
	}
	return map[string]any{"path": d.Path, "mtime": d.MTime, "exists": d.Exists, "problems": d.Problems, "model": model}, nil
}

// kindHub : « Ce poste » — la section hubs.<machine> de backends.yaml.
const kindHub core.ConfigKind = "hub"

// maskProblems : messages sans la valeur du jeton GitHub (jamais montrée).
func maskProblems(ps []core.ConfigProblem, token string) []core.ConfigProblem {
	if token == "" {
		return ps
	}
	out := make([]core.ConfigProblem, len(ps))
	for i, p := range ps {
		p.Message = strings.ReplaceAll(p.Message, token, "***")
		out[i] = p
	}
	return out
}

// configApply : applique des changements structurés au texte du fichier
// (commentaires et ordre conservés), puis le même enregistrement que
// l'éditeur de texte : validation de core, contrôle de conflit, copie .bak.
// Résultat : celui de configSave.
func (a *App) configApply(kind core.ConfigKind, project, mtime string, changes []core.ConfigChange) (map[string]any, error) {
	fileKind := kind
	switch kind {
	case kindHub:
		fileKind = core.ConfBackends
		project = ""
	case core.ConfConf, core.ConfLauncherEnv:
		project = "" // fichiers du poste : jamais un chemin fourni par le client
	}
	f, err := a.configFile(fileKind, "", project)
	if err != nil {
		return nil, err
	}
	content := ""
	if b, err := os.ReadFile(f.Path); err == nil {
		content = string(b)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	var out []byte
	token := ""
	switch kind {
	case core.ConfConf:
		base := content
		if base == "" && !f.Exists {
			base = core.ConfigTemplate(core.ConfConf) // absent (Windows) : créé depuis le modèle
		}
		token = core.ConfTokenValue(base)
		var s string
		s, err = core.ApplyConfConfChanges(base, changes)
		out = []byte(s)
	case kindHub:
		out, err = core.ApplyHubChanges([]byte(content), core.HubHost(), changes)
	default:
		out, err = core.ApplyConfigChanges(kind, []byte(content), changes)
	}
	if err != nil {
		msg := err.Error()
		if token != "" {
			msg = strings.ReplaceAll(msg, token, "***")
		}
		for _, ch := range changes { // le jeton qu'on vient de saisir non plus
			if s, ok := ch.Value.(string); ok && s != "" && len(ch.At) == 1 && ch.At[0] == "GITHUB_TOKEN" {
				msg = strings.ReplaceAll(msg, s, "***")
			}
		}
		return map[string]any{"invalid": true, "problems": []core.ConfigProblem{{Severity: core.SevError, Message: msg}}}, nil
	}
	return a.configSave(fileKind, project, string(out), mtime)
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

// refreshHubWarns : relit les avertissements de hubs: en tâche de fond. Chaque
// relecture reçoit un numéro de génération à son lancement ; seule la plus
// récente s'applique, une relecture plus ancienne qui finit après elle (deux
// enregistrements rapprochés) ne l'écrase pas. Rend un canal fermé à la fin.
func (a *App) refreshHubWarns(read func() []string) <-chan struct{} {
	gen := a.hubGen.Add(1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		w := read()
		a.mu.Lock()
		if a.hubGen.Load() == gen {
			a.hubWarns = w
		}
		a.mu.Unlock()
		a.changed()
	}()
	return done
}

// ── Réglages d'un projet (#148, LOT-3.2 : contracts/desktop-dispatch.md) ──

// foreignOwner : propriétaire vivant d'une session d'un autre launcher (remplacé
// dans les tests : aucun accès aux moteurs ni au réseau).
var foreignOwner = core.ForeignOwner

// foreignOwnerTimeout : délai global de la vérification du propriétaire.
var foreignOwnerTimeout = 5 * time.Second

// projectOpen : le projet est ouvert, ici (onglet) ou par un autre launcher
// (session tmux vivante dans son conteneur, vue à la dernière lecture des
// conteneurs). Sans verrou.
func (a *App) projectOpen(path string) bool {
	if _, open := a.openTabs.Load(path); open {
		return true
	}
	p := a.liveSnap.Load()
	return p != nil && (*p)[path]
}

// projectSettings : backend, image, couleur, env.yaml du projet et aperçu
// fusionné avec provenance. Sans a.mu : l'état « ouvert » vient d'ensembles
// lisibles sans verrou, la configuration d'un instantané. Un chemin qui n'est
// pas un projet de l'arbre est refusé.
func (a *App) projectSettings(path string, changes []core.ConfigChange) (map[string]any, error) {
	if !a.knownProject(path) {
		return nil, fmt.Errorf("projet inconnu : %s", path)
	}
	s := core.ReadProjectSettings(path, a.conf(), changes)
	b, _ := json.Marshal(s)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	out["open"] = a.projectOpen(path)
	return out, nil
}

// projectApply : applique les parties demandées (core.ApplyProjectSettings) puis
// relit à chaud ce qui peut l'être : config.conf après une couleur (même si une
// étape suivante a échoué : elle est écrite), les conteneurs après un backend ou
// une image. Un projet ouvert par un autre launcher est détecté aussi
// (instantané des conteneurs, puis propriétaire vivant pour backend et image).
func (a *App) projectApply(path string, in core.ProjectChange) (map[string]any, error) {
	if !a.knownProject(path) {
		return nil, fmt.Errorf("projet inconnu : %s", path)
	}
	open := a.projectOpen(path)
	if !open && (in.Backend != nil || in.Image != nil) {
		// session d'un autre launcher, vérifiée à la source — bornée : un backend
		// injoignable ne doit pas bloquer l'écran ; au délai, on refuse plutôt que
		// de laisser passer un changement sur un projet peut-être ouvert ailleurs
		res := make(chan string, 1)
		go func() { res <- foreignOwner(path) }()
		select {
		case who := <-res:
			open = who != ""
		case <-time.After(foreignOwnerTimeout):
			return map[string]any{"ok": false, "msg": "impossible de vérifier si le projet est ouvert ailleurs (backend injoignable)"}, nil
		}
	}
	r := core.ApplyProjectSettings(path, open, in)
	if r.Color {
		a.configReload(core.ConfConf)
	}
	if r.OK && (in.Backend != nil || in.Image != nil) {
		go a.readCtr()
		go a.setMsg("⬡ " + filepath.Base(path) + " : réglages enregistrés")
	}
	if r.Env != nil && r.Env.Saved {
		a.cardsChanged()
	}
	b, _ := json.Marshal(r)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out, nil
}
