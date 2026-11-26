package desktop

import (
	"claude-launcher-ng2/internal/tun"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
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
	// env.yaml d'un projet ouvert / à conteneur actif : éditeur en lecture seule (le refus d'écriture est côté core)
	ReadOnly       bool   `json:"readOnly,omitempty"`
	ReadOnlyReason string `json:"readOnlyReason,omitempty"`
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
	if kind == core.ConfConf {
		d.Content = core.MaskConfToken(d.Content) // le jeton ne quitte jamais le poste : marqueur « __GARDER__ »
	}
	if kind == core.ConfProjectEnv {
		if why := a.envLockedCheap(project); why != "" {
			d.ReadOnly, d.ReadOnlyReason = true, why
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
	ps := core.ValidateConfig(kind, []byte(content))
	if kind == core.ConfConf {
		ps = maskProblems(ps, core.ConfTokenValue(content), a.currentToken())
	}
	return ps, ""
}

// currentToken : jeton GitHub actuellement écrit dans config.conf (jamais renvoyé :
// sert à masquer les messages).
func (a *App) currentToken() string {
	if b, err := os.ReadFile(core.ConfigPath()); err == nil {
		return core.ConfTokenValue(string(b))
	}
	return ""
}

// configSave : enregistre ; conflit : rend {conflict, mtime actuel} pour
// proposer de recharger ou d'écraser.
func (a *App) configSave(kind core.ConfigKind, project, content, mtime string) (map[string]any, error) {
	return a.configSaveAs(false, kind, project, content, mtime)
}

// errWebDesktopWeb : un client web ne modifie pas desktop_web (interface web, allow_lan).
const errWebDesktopWeb = "les réglages desktop_web (interface web, allow_lan) et browser_server (serveur de fichiers), ainsi que le kubeconfig des backends, ne se modifient que depuis la fenêtre native"

// configSaveAs : configSave ; web : appel d'un client web du processus de la fenêtre — il ne peut
// pas modifier les réglages desktop_web (un client web rouvrirait l'interface sur le réseau).
func (a *App) configSaveAs(web bool, kind core.ConfigKind, project, content, mtime string) (map[string]any, error) {
	f, err := a.configFile(kind, "", project)
	if err != nil {
		return nil, err
	}
	if web && kind == core.ConfBackends {
		old, _ := os.ReadFile(f.Path)
		if core.WebExposureChanged(old, []byte(content)) {
			return map[string]any{"invalid": true, "problems": []core.ConfigProblem{{Severity: core.SevError, Message: errWebDesktopWeb}}}, nil
		}
	}
	var expected time.Time
	if mtime != "" {
		if expected, err = time.Parse(time.RFC3339Nano, mtime); err != nil {
			return nil, fmt.Errorf("date d'ouverture invalide : %v", err)
		}
	}
	cur := ""
	before := ""
	if b, rerr := os.ReadFile(f.Path); rerr == nil {
		before = string(b) // pour `apply` : options dont la valeur effective change (#226)
	}
	if kind == core.ConfConf && before == "" && !f.Exists {
		before = core.ConfigTemplate(core.ConfConf) // fichier absent : créé depuis le modèle, valeurs de départ = celles du modèle
	}
	if kind == core.ConfConf {
		// l'éditeur de texte ne reçoit jamais le jeton : « __GARDER__ » le conserve
		cur = before
		var rerr error
		if content, rerr = core.RestoreConfToken(content, cur); rerr != nil {
			return map[string]any{"invalid": true, "problems": []core.ConfigProblem{{Severity: core.SevError, Message: rerr.Error()}}}, nil
		}
	}
	var guard, recheck func() []string // éditeur YAML de l'env.yaml : refusé projet ouvert / conteneur actif
	var envWarns []string
	if kind == core.ConfProjectEnv {
		guard = func() []string { // complet, hors verrou
			b, w := core.EnvEditCheck(a.moveEnv(), project)
			envWarns = w
			return b
		}
		recheck = func() []string { return a.envFastBlockers(project) } // rapide, sous verrou
	}
	ps, err := core.SaveConfigGuarded(kind, f.Path, []byte(content), expected, guard, recheck)
	var locked *core.EnvLockedError
	if errors.As(err, &locked) {
		return envRefused(locked.Blockers), nil
	}
	if kind == core.ConfConf {
		ps = maskProblems(ps, core.ConfTokenValue(content), core.ConfTokenValue(cur)) // le jeton n'apparaît dans aucun message
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
	a.configReload(kind)
	st, _ := os.Stat(f.Path)
	out := map[string]any{"saved": true, "problems": ps, "apply": a.applyInfo(kind, before, content)}
	if n := core.ConfigReloadNote(kind); n != "" { // backends.yaml, hub, env.yaml : pas encore de pastille d'effet → note (config.conf : `effect` / `apply`)
		out["note"] = n
	}
	if kind == core.ConfConf { // recopie dans settings.json avec la config relue : {ok} ou {ok:false, msg}
		out["sync"] = a.syncSettings()
	}
	if len(envWarns) > 0 {
		out["warn"] = envWarns[0] // état du conteneur inconnu : écrit quand même
	}
	if st != nil {
		out["mtime"] = st.ModTime().Format(time.RFC3339Nano)
	}
	go a.setMsg("✓ " + f.Path + " enregistré (copie .bak)") // message de pied : sans attendre a.mu
	return out, nil
}

// conf : configuration courante (relue à chaud après un enregistrement de
// config.conf).
func (a *App) conf() core.Config {
	a.confRefresh(false)
	return a.confCopy()
}

// confNow : conf() avec relecture IMMÉDIATE si config.conf a changé (pas la limite d'une par seconde) — avant une action qui en
// dépend et la fige : ouverture d'un projet, création, rafraîchissement, milestones, relance (#225).
func (a *App) confNow() core.Config {
	a.confRefresh(true)
	return a.confCopy()
}

// confRecheck : au plus une comparaison de config.conf (date, taille) par intervalle pour conf().
var confRecheck = tun.Of(time.Second)

// confRefresh : relit config.conf si sa date ou sa taille a changé depuis la copie en mémoire (édition extérieure : éditeur,
// autre launcher, git pull). Invalide : dernière copie valide gardée et dite (message de pied). Même effet à chaud que
// configReload (consommation, mise en page, cartes). Sans surveillance de fichier : 9P / MooseFS.
func (a *App) confRefresh(force bool) {
	if !force {
		now := time.Now().UnixNano()
		last := a.confCheck.Load()
		if now-last < int64(confRecheck.Get()) || !a.confCheck.CompareAndSwap(last, now) {
			return
		}
	}
	a.confMu.RLock()
	cur := a.cfg
	a.confMu.RUnlock()
	if !core.ConfigChanged(cur.Stamp) {
		return
	}
	fresh, warn := core.FreshConfig(cur)
	if warn == "" && fresh.Stamp == cur.Stamp { // rien à adopter (illisible, absence en délai de grâce, relecture à refaire) : aucun effet
		return
	}
	if warn != "" {
		a.confMu.Lock()
		if a.cfg.Stamp == cur.Stamp {
			a.cfg.Stamp = fresh.Stamp // vue : pas de revalidation ni de nouveau message avant la prochaine modification
		}
		a.confMu.Unlock()
		a.bg(func() { a.setMsg("⚠ " + warn) })
		return
	}
	a.confMu.Lock()
	if a.cfg.Stamp != cur.Stamp { // relue entre-temps par un autre appelant (watcher, enregistrement) : rien à refaire
		a.confMu.Unlock()
		return
	}
	a.cfg = fresh
	a.confMu.Unlock()
	a.bg(func() { core.SyncLauncherEnv(a.conf()) }) // recopie dans settings.json avec la config relue (#225)
	a.bg(a.readUsage)
	a.bg(a.relayoutOpen)
	a.changed()
	a.cardsChanged()
}

func (a *App) confCopy() core.Config {
	a.confMu.RLock()
	defer a.confMu.RUnlock()
	c := a.cfg
	if len(a.layoutOv) > 0 { // organisations demandées, pas encore écrites dans config.conf
		m := make(map[string]string, len(c.ProjectLayouts)+len(a.layoutOv))
		for k, v := range c.ProjectLayouts {
			m[k] = v
		}
		for k, v := range a.layoutOv {
			if v == "" {
				delete(m, k)
			} else {
				m[k] = v
			}
		}
		c.ProjectLayouts = m
	}
	return c
}

func (a *App) setLayoutOverride(key, layout string) {
	a.confMu.Lock()
	if a.layoutOv == nil {
		a.layoutOv = map[string]string{}
	}
	a.layoutOv[key] = layout
	a.confMu.Unlock()
}

func (a *App) clearLayoutOverride(key string) {
	a.confMu.Lock()
	delete(a.layoutOv, key)
	a.confMu.Unlock()
}

// persistDelay : délai avant l'écriture de config.conf (tests : un partage réseau lent) ; nul en production.
var persistDelay = &core.ShareWriteDelay

// layoutWrites : écritures de config.conf demandées par la bascule (tests : une rafale de clics en fait 2 au plus).
var layoutWrites atomic.Int64

// layoutJob : clics de bascule d'un projet, sérialisés — le dernier clic gagne.
type layoutJob struct {
	mu      sync.Mutex // seq et override
	seq     uint64     // numéro du dernier clic
	writeMu sync.Mutex // une seule écriture de config.conf à la fois
}

func (a *App) layoutJobFor(base string) *layoutJob {
	a.confMu.Lock()
	defer a.confMu.Unlock()
	if a.layoutJobs == nil {
		a.layoutJobs = map[string]*layoutJob{}
	}
	if a.layoutJobs[base] == nil {
		a.layoutJobs[base] = &layoutJob{}
	}
	return a.layoutJobs[base]
}

// applyLayoutFirst : bascule de l'organisation d'un projet — la mise en page part AVANT la persistance
// (organisation demandée connue côté Go : la présentation change tout de suite, sans attendre le partage
// réseau où config.conf est écrit), puis config.conf est écrit ; écriture refusée : retour à l'organisation
// précédente et {ok:false, msg}. Réponse comme avant : colorSaved vrai dès que config.conf est écrit.
//
// Clics rapides : l'organisation affichée est toujours la dernière demandée (la demande reste en mémoire
// jusqu'à ce qu'elle soit écrite) ; les écritures sont sérialisées par projet et un clic remplacé par un plus
// récent n'écrit pas — au plus l'écriture déjà en cours puis l'écriture finale de la dernière valeur.
func (a *App) applyLayoutFirst(path, layout string) (map[string]any, error) {
	base := filepath.Base(path)
	if _, err := core.SetProjectLayout("", base, layout); err != nil { // valeur inconnue : refusée avant toute écriture
		return map[string]any{"ok": false, "msg": err.Error()}, nil
	}
	t0 := time.Now()
	j := a.layoutJobFor(base)
	j.mu.Lock()
	j.seq++
	my := j.seq
	a.setLayoutOverride(base, layout)
	j.mu.Unlock()
	if t := a.term(path); t != nil {
		t.relayoutNow()
	}
	a.changed()
	a.cardsChanged()
	a.perf.trace("bascule : avant écriture", base, time.Since(t0))

	j.writeMu.Lock()
	defer j.writeMu.Unlock()
	j.mu.Lock()
	superseded := j.seq != my
	j.mu.Unlock()
	if superseded { // un clic plus récent écrira la valeur finale
		return map[string]any{"ok": true}, nil
	}
	t1 := time.Now()
	layoutWrites.Add(1)
	// écriture bornée (SHARE_WRITE_TIMEOUT, #220) : un partage injoignable rend la main au délai, sans verrou tenu
	_, err := core.ApplyProjectConf(base, nil, &layout)
	a.perf.trace("bascule : écriture config.conf", base, time.Since(t1))
	j.mu.Lock()
	stillLatest := j.seq == my
	j.mu.Unlock()
	if err != nil {
		if !stillLatest { // un clic plus récent est en attente : c'est lui qui écrira, rien à annuler
			return map[string]any{"ok": true}, nil
		}
		a.clearLayoutOverride(base)
		a.bg(a.relayoutOpen) // l'organisation précédente revient
		a.changed()
		if core.IsShareTimeout(err) { // le fichier fait foi si l'écriture abandonnée aboutit plus tard (relecture)
			return map[string]any{"ok": false, "timeout": true, "msg": err.Error() + " — retour à l'organisation précédente"}, nil
		}
		return map[string]any{"ok": false, "msg": "config.conf : " + err.Error() + " — organisation non enregistrée, retour à la précédente"}, nil
	}
	a.configReload(core.ConfConf)
	j.mu.Lock()
	if j.seq == my { // aucun clic plus récent : la demande est écrite, la configuration la porte
		a.clearLayoutOverride(base)
	}
	j.mu.Unlock()
	a.perf.trace("bascule : total", base, time.Since(t0))
	return map[string]any{"ok": true, "colorSaved": true}, nil
}

// syncSettings : recopie des variables du launcher (jeton, souris, teams, EXTRA_ENVS) dans le settings.json utilisateur avec la
// configuration relue (#225) — résultat pour l'interface : {ok:true} ou {ok:false, msg}. Garde « jamais plus ancien » : voir
// core.SyncLauncherEnv.
func (a *App) syncSettings() map[string]any {
	if core.SyncLauncherEnv(a.confNow()) {
		return map[string]any{"ok": true}
	}
	return map[string]any{"ok": false, "msg": "recopie dans settings.json impossible (fichier illisible ou non inscriptible) : les valeurs secrètes seront tapées dans le shell à l'ouverture"}
}

// applyInfo : champ `apply` de configSave / configApply (#226) — options dont la valeur EFFECTIVE a changé avec leur effet,
// relance du launcher nécessaire, projets OUVERTS concernés par une option claude / project. Aucune action automatique : des
// actions sont seulement proposées par l'interface (Rafraîchir, Relancer le launcher).
func (a *App) applyInfo(kind core.ConfigKind, oldSrc, newSrc string) map[string]any {
	changed := []core.ChangedOption{}
	if kind == core.ConfConf {
		changed = append(changed, core.ChangedOptions(oldSrc, newSrc, core.IfaceDesktop)...)
	}
	projects := []map[string]any{}
	forProj := map[string]bool{}
	for _, c := range changed {
		if c.Effect == core.EffectClaude || c.Effect == core.EffectProject {
			forProj[c.Effect] = true
		}
	}
	if len(forProj) > 0 {
		var effs []string
		for _, e := range []string{core.EffectClaude, core.EffectProject} {
			if forProj[e] {
				effs = append(effs, e)
			}
		}
		var paths []string
		a.openTabs.Range(func(k, _ any) bool {
			if p, ok := k.(string); ok && a.knownProject(p) {
				paths = append(paths, p)
			}
			return true
		})
		sort.Strings(paths)
		for _, p := range paths {
			projects = append(projects, map[string]any{"path": p, "name": filepath.Base(p), "for": effs})
		}
	}
	return map[string]any{"changed": changed, "relaunch": core.NeedsRelaunch(changed), "projects": projects}
}

// configReload : ce qui est relu sans redémarrer.
func (a *App) configReload(kind core.ConfigKind) {
	switch kind {
	case core.ConfConf:
		a.confRefresh(true) // UN seul chemin de relecture : validée, copie valide gardée, effets à chaud, recopie settings.json
	case core.ConfBackends:
		// avertissements de hubs: relus : une faute corrigée (ou introduite) se voit
		// tout de suite, pas seulement au prochain démarrage
		// hors de la réponse : a.mu peut être tenu par une lecture lente, l'écran
		// Réglages n'a pas à l'attendre
		a.storeBackends()
		a.bg(a.applyWebUI) // desktop_web relu à chaud (#163)
		a.refreshHubWarns(core.HubConfigWarnings)
		a.bg(a.readCtr)
		a.changed()
	}
}

// ── Écriture structurée (#118, LOT-3.1 : contracts/desktop-dispatch.md) ───

// configModel : backends.yaml lu en modèle (backends et pools dans l'ordre du
// fichier, défauts, images) avec les problèmes de validation et la date
// d'ouverture, pour l'écran Réglages. Fichier absent : modèle vide.
func (a *App) configModel(kind core.ConfigKind, project string) (map[string]any, error) {
	return a.configModelFor(true, kind, project)
}

// configModelFor : configModel ; reveal : l'œil des entrées secrètes est actif (fenêtre native, accès web local) — faux
// pour un accès web distant (#227).
func (a *App) configModelFor(reveal bool, kind core.ConfigKind, project string) (map[string]any, error) {
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
		model = core.ReadConfModel(content).WithEffects(core.IfaceDesktop, reveal) // le jeton n'y figure jamais
		d.Problems = maskProblems(d.Problems, a.currentToken())
	case core.ConfLauncherEnv:
		model, merr = core.ReadLauncherEnvModel([]byte(content))
	case kindHub:
		var hm core.HubModel
		if hm, merr = core.ReadHubModel([]byte(content), core.HubHost(), core.CurrentGOOS()); merr == nil {
			prefs := loadPrefs()
			size := func(k string, def int) int { // préférence absente : défaut de la zone
				if n, ok := prefs[k].(float64); ok {
					return int(n)
				}
				return def
			}
			model = struct {
				core.HubModel
				TermFontSize  int `json:"termFontSize"`
				TreeFontSize  int `json:"treeFontSize"`
				CardsFontSize int `json:"cardsFontSize"`
			}{hm, size("termFontSize", 13), size("treeFontSize", 13), size("cardsFontSize", 12)}
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

// maskProblems : messages sans la valeur du jeton GitHub (jamais montrée ; un
// jeton de moins de 8 caractères remplace tout le message : core.MaskSecret).
func maskProblems(ps []core.ConfigProblem, tokens ...string) []core.ConfigProblem {
	out := make([]core.ConfigProblem, len(ps))
	for i, p := range ps {
		for _, t := range tokens {
			p.Message = core.MaskSecret(p.Message, t)
		}
		out[i] = p
	}
	return out
}

// configApply : applique des changements structurés au texte du fichier
// (commentaires et ordre conservés), puis le même enregistrement que
// l'éditeur de texte : validation de core, contrôle de conflit, copie .bak.
// Résultat : celui de configSave.
func (a *App) configApply(kind core.ConfigKind, project, mtime string, changes []core.ConfigChange) (map[string]any, error) {
	return a.configApplyAs(false, kind, project, mtime, changes)
}

// configApplyAs : configApply ; web : voir configSaveAs.
func (a *App) configApplyAs(web bool, kind core.ConfigKind, project, mtime string, changes []core.ConfigChange) (map[string]any, error) {
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
		out, err = core.ApplyHubChangesFor([]byte(content), core.HubHost(), core.CurrentGOOS(), changes)
	default:
		out, err = core.ApplyConfigChanges(kind, []byte(content), changes)
	}
	if err != nil {
		msg := core.MaskSecret(err.Error(), token)
		for _, ch := range changes { // le jeton qu'on vient de saisir non plus
			if s, ok := ch.Value.(string); ok && s != "" && len(ch.At) == 1 && ch.At[0] == "GITHUB_TOKEN" {
				msg = core.MaskSecret(msg, s)
			}
		}
		return map[string]any{"invalid": true, "problems": []core.ConfigProblem{{Severity: core.SevError, Message: msg}}}, nil
	}
	return a.configSaveAs(web, fileKind, project, string(out), mtime)
}

// backendTest : étapes du test d'un backend du formulaire (pas forcément
// enregistré) ; ses pools sont ceux du fichier.
// backendTestSem : au plus deux tests de backend à la fois (chacun lance ssh,
// docker ou kubectl pendant jusqu'à 45 s).
var backendTestSem = make(chan struct{}, 2)

// backendTestWait : attente d'une place avant de refuser un test de backend (tests : réduite).
var backendTestWait = tun.Of(3 * time.Second)

func (a *App) backendTest(be core.BackendEntry) map[string]any {
	select {
	case backendTestSem <- struct{}{}:
		defer func() { <-backendTestSem }()
	case <-time.After(backendTestWait.Get()):
		return map[string]any{"steps": []core.TestStep{{Label: "test refusé", Detail: "trop de tests de backend en cours",
			Fix: "attendre la fin des tests en cours, puis recommencer"}}}
	}
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
var foreignOwner = tun.Of(core.ForeignOwnerChecked) // « backend injoignable : … » si un moteur ne répond pas (échec rapide compris)

// runningOwner : conteneur / pod du projet en cours d'exécution (listing des moteurs), remplacé dans les tests.
var runningOwner = tun.Of(core.RunningCtr)

// foreignOwnerTimeout : délai global de la vérification du propriétaire.
var foreignOwnerTimeout = tun.Of(5 * time.Second)

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
	why := a.envLockedCheap(path)
	out["envLocked"], out["envLockReason"] = why != "", why
	if e, ok := core.EnvTrustEntry(path); ok { // absent : projet de confiance
		out["envTrust"] = map[string]any{"state": e.State, "source": e.Source, "at": e.At}
	}
	return out, nil
}

// envEditRefusal : "" si le backend / l'image du projet (F2, execApply) peut être changé ; sinon la raison (ouvert ici, par un autre launcher,
// conteneur / pod actif) avec la consigne « fermez le projet pour modifier son environnement ».
func (a *App) envEditRefusal(path string) string { return core.EnvExecRefusal(a.moveEnv(), path) }

// envEditBlockers : les raisons (ouvert ici, par un autre launcher, conteneur actif) ; vide : modifiable.
func (a *App) envEditBlockers(path string) []string { return core.EnvEditBlockers(a.moveEnv(), path) }

// envLockedCheap : état « verrouillé » pour l'affichage (projectSettings, configOpen), SANS interroger les moteurs : projet
// ouvert ici, session vivante vue dans un conteneur (autre launcher compris) ou conteneur / pod actif (dernière lecture des
// conteneurs, sans verrou). Le contrôle complet (propriétaire vivant, conteneur actif) reste celui de
// l'écriture. Rend la raison, "" si libre.
func (a *App) envLockedCheap(path string) string {
	if b := a.envFastBlockers(path); len(b) > 0 {
		return core.EnvEditMessage(b)
	}
	return ""
}

// envFastBlockers : blockers lus dans l'état mémorisé (onglet, session vivante vue, conteneur actif à la dernière lecture),
// SANS interroger les moteurs — revérification rapide sous les verrous d'écriture, et affichage d'avance.
func (a *App) envFastBlockers(path string) []string {
	var out []string
	if _, open := a.openTabs.Load(path); open {
		out = append(out, "projet ouvert dans ce launcher")
	} else if p := a.liveSnap.Load(); p != nil && (*p)[path] { // session vivante dans un conteneur : ici ou chez un autre launcher
		out = append(out, "projet ouvert (session vivante dans son conteneur)")
	}
	if p := a.ctrFounds.Load(); p != nil {
		for _, f := range *p {
			if f.Project == path && f.Running {
				out = append(out, "conteneur ou pod en cours d'exécution ("+f.Name+")")
				break
			}
		}
	}
	return out
}

// unverifiableOwner216 : backend / image refusés (fail-closed) — le propriétaire du projet n'a pas pu être vérifié.
func unverifiableOwner216() map[string]any {
	return map[string]any{"ok": false, "field": "env", "blockers": []string{core.EnvBlockUnknown},
		"msg": "impossible de vérifier si le projet est ouvert ailleurs (" + core.EnvBlockUnknown + ")"}
}

// envRefused : réponse d'un refus d'écriture de l'environnement — {ok:false, saved:false, field:"env", msg, blockers}.
func envRefused(blockers []string) map[string]any {
	return map[string]any{"ok": false, "saved": false, "field": "env", "msg": core.EnvEditMessage(blockers), "blockers": blockers}
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
	if in.Layout != nil && in.Backend == nil && in.Image == nil && in.Color == nil && in.Env == nil && in.HideIdle == nil {
		return a.applyLayoutFirst(path, *in.Layout) // bouton de bascule : mise en page avant persistance
	}
	open := a.projectOpen(path)
	if !open && (in.Backend != nil || in.Image != nil) {
		// session d'un autre launcher, vérifiée à la source — bornée : un backend
		// injoignable ne doit pas bloquer l'écran ; au délai, on refuse plutôt que
		// de laisser passer un changement sur un projet peut-être ouvert ailleurs
		res := make(chan string, 1)
		go func() { res <- foreignOwner.Get()(path) }()
		select {
		case who := <-res:
			if strings.HasPrefix(who, core.UnverifiablePrefix) { // propriétaire non vérifiable (échec rapide) : refus, comme le délai
				return unverifiableOwner216(), nil
			}
			open = who != ""
		case <-time.After(foreignOwnerTimeout.Get()):
			return unverifiableOwner216(), nil
		}
	}
	var envWarns []string           // vérification impossible (backend injoignable) : écriture permise, avertie
	in.EnvGuard = func() []string { // contrôle complet (moteurs, propriétaire) : une seule fois, avant toute écriture
		b, w := core.EnvEditCheck(a.moveEnv(), path)
		envWarns = w
		if (in.Backend != nil || in.Image != nil) && len(w) > 0 { // backend / image : état inconnu = refus (fail-closed)
			b = append(b, core.EnvBlockUnknown)
		}
		return b
	}
	in.EnvRecheck = func() []string { return a.envFastBlockers(path) } // revérification rapide sous verrou // env.yaml / backend / image : ni ouvert ni conteneur actif
	r := core.ApplyProjectSettings(path, open, in)
	if r.Color {
		a.configReload(core.ConfConf)
		if in.HideIdle != nil {
			// appliqué sans relance, tout de suite (#209) : carte TEAM en cache invalidée, règle de visibilité rejouée
			// sur le projet ouvert (config relue par configReload ci-dessus), sans attendre le tour du watcher
			teamCache.Lock()
			delete(teamCache.m, path)
			teamCache.Unlock()
			if t := a.term(path); t != nil {
				t.applyNow()
			}
			// carte TEAM renvoyée à coup sûr, même quand l'état effectif ne change pas (default ↔ auto, global activé) : mode et
			// global changent seuls ; ne dépend ni d'un select ni de la relecture périodique (relecture unique, signal perdable)
			a.pushTeam(path)
			a.cardsChanged()
		}
	}
	if r.OK && (in.Backend != nil || in.Image != nil) {
		a.bg(a.readCtr)
		go a.setMsg("⬡ " + filepath.Base(path) + " : réglages enregistrés")
	}
	if r.Env != nil && r.Env.Saved {
		a.cardsChanged()
	}
	b, _ := json.Marshal(r)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	if len(envWarns) > 0 && r.OK {
		out["warn"] = envWarns[0]
	}
	return out, nil
}
