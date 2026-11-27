package desktop

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"claude-launcher-ng2/core"
	"claude-launcher-ng2/internal/tun"
)

// Routage par identité (contracts/user-backends.md §9, #255).
//
// Une identité = un espace. L'administrateur (compte local compris) garde l'App actuelle (la racine). Chaque autre
// utilisateur a SA propre instance d'état d'interface (userSpace), créée à sa première requête et libérée 10 minutes après
// son dernier client : ses évènements ne partent qu'à ses flux (emitTo), ses appels n'atteignent que son espace.
//
// L'instance d'un utilisateur n'a PAS de projets en v7.1.0 LOT-2 (ils viendront de ses backends, LOT-3.2) et n'appelle jamais
// les fonctions de core qui lisent l'espace de l'admin (backends.yaml, coffre, projects.json de la racine) : seules les
// méthodes de la liste userMethods existent, toutes limitées à users/<uid>/. Toute autre méthode est refusée.

// spaceIdle : durée sans client au-delà de laquelle l'instance d'un utilisateur est libérée.
var spaceIdle = tun.Of(10 * time.Minute)

// msgNoSpaceMethod : méthode absente de l'espace d'un utilisateur.
const msgNoSpaceMethod = "indisponible dans votre espace"

// userSpace : l'état d'interface d'un utilisateur.
type userSpace struct {
	sp    core.Space
	login string
	// clients : flux d'évènements ouverts ; last : dernière activité (ns Unix) — la libération compte depuis le dernier de l'un ou de l'autre
	clients atomic.Int32
	last    atomic.Int64
	prefsMu sync.Mutex
	// backendTest : 6 par minute (tmu, tests) et un seul à la fois (testing)
	tmu     sync.Mutex
	tests   []time.Time
	testing atomic.Bool
	secrets []time.Time                 // backendSecretSet : même plafond par minute que backendTest
	proj    userProjects                // découverte des projets par backend (cache 10 s)
	emit    func(name string, data any) // évènements de CETTE identité seulement (jamais une diffusion commune)
	cfgs    []time.Time                 // configOpen / Model / Validate / Save / Apply (analyse YAML et DNS) : plafond généreux (audit m9)
}

func (u *userSpace) touch() { u.last.Store(time.Now().UnixNano()) }

func (u *userSpace) idleSince(now time.Time) time.Duration {
	if u.clients.Load() > 0 {
		return 0
	}
	return now.Sub(time.Unix(0, u.last.Load()))
}

// spaceHub : instances des utilisateurs, par espace.
type spaceHub struct {
	mu     sync.Mutex
	m      map[string]*userSpace
	emitTo func(uid, name string, data any) // évènements d'une identité (web.go)
}

// get : l'instance de l'espace sp (créée à la première requête).
func (h *spaceHub) get(sp core.Space, login string) *userSpace {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.m == nil {
		h.m = map[string]*userSpace{}
	}
	u := h.m[sp.UID]
	if u == nil {
		u = &userSpace{sp: sp, login: login}
		if h.emitTo != nil {
			emitTo, uid := h.emitTo, sp.UID
			u.emit = func(name string, data any) { emitTo(uid, name, data) }
		}
		h.m[sp.UID] = u
	}
	u.login = login // un login renommé garde son espace (même identifiant numérique)
	u.touch()
	return u
}

// reap : libère les instances sans client depuis plus de spaceIdle ; rend le nombre libéré.
func (h *spaceHub) reap(now time.Time) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for uid, u := range h.m {
		if u.idleSince(now) > spaceIdle.Get() {
			delete(h.m, uid)
			n++
		}
	}
	return n
}

// count : instances vivantes (tests).
func (h *spaceHub) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.m)
}

// reapEvery : cadence du balayage des instances inactives.
var reapEvery = tun.Of(time.Minute)

// runReaper : balaie jusqu'à ce que stop soit fermé.
func (h *spaceHub) runReaper(stop <-chan struct{}) {
	t := time.NewTicker(reapEvery.Get())
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case now := <-t.C:
			h.reap(now)
		}
	}
}

// userMethods : méthodes de l'espace d'un utilisateur (toutes limitées à son espace). Les autres sont refusées.
var userMethods = map[string]bool{
	"clientInfo": true, "init": true, "setPrefs": true, "setTheme": true,
	"configFiles": true, "configOpen": true, "configValidate": true, "configSave": true,
	"configModel": true, "configApply": true, "backendStates": true,
	"backendSecretSet": true, "backendSecretGet": true, "backendSecretDelete": true, "backendTest": true,
	"userBackendAdd": true, "userBackendRemove": true,
	"select": true, "cardsFor": true, "refresh": true, "refreshProject": true, "projectPurge": true, "projectMove": true, "projectDelete": true, "newInfo": true, "create": true,
	"projectSettings": true, "projectApply": true,
}

// dispatch : un appel de l'utilisateur. L'identité (c.User) n'est jamais celle d'une autre instance : l'instance est choisie
// par le serveur d'après l'en-tête de la requête, jamais d'après les arguments.
func (u *userSpace) dispatch(c webCall, method string, raw json.RawMessage) (any, error) {
	u.touch()
	// règle R1 inchangée (v7.0.0) : une écriture réservée à l'admin répond « forbidden », jamais « indisponible ». Seule
	// exception : configSave et configApply du backends.yaml de SON espace
	forbidden := map[string]any{"ok": false, "forbidden": core.RoleAdmin, "msg": msgR1}
	if cls, _ := core.ClassOf(method); cls == core.ClassR1 {
		if method == "configSave" || method == "configApply" {
			var in struct{ Kind, Content string }
			switch _ = json.Unmarshal(raw, &in); core.ConfigKind(in.Kind) {
			case core.ConfBackends: // son propre fichier : libre, sauf les champs R1 (configApply : vérifiés sur le contenu produit)
				if method == "configSave" && core.UserSSHRefused([]byte(in.Content)) { // ssh : refus clair (P13), pas un refus R1
					return map[string]any{"invalid": true, "problems": u.sp.ValidateBackends([]byte(in.Content))}, nil
				}
				if method == "configSave" && core.UserAdminFields([]byte(in.Content)) {
					return forbidden, nil
				}
			case core.ConfProjectEnv: // pas de projet dans son espace (LOT-3.2) : ni refus R1 ni écriture
				return nil, fmt.Errorf("%s : %s", method, msgNoSpaceMethod)
			default:
				return forbidden, nil
			}
		} else if !secretUserOK(method, raw) { // kubeconfig / TLS de SON backend dans SON coffre : libre (clés ssh, known_hosts : admin)
			return forbidden, nil
		}
	}
	if !userMethods[method] {
		return nil, fmt.Errorf("%s : %s", method, msgNoSpaceMethod)
	}
	switch method {
	case "backendSecretSet", "backendSecretGet", "backendSecretDelete":
		return u.secretCall(c, method, raw)
	case "backendTest":
		return u.backendTest(c, raw)
	case "select", "cardsFor", "refresh", "refreshProject", "projectPurge", "projectMove", "projectDelete", "newInfo", "create", "projectSettings", "projectApply":
		return u.projectCall(c, method, raw)
	case "userBackendAdd":
		return u.backendAdd(c, raw)
	case "userBackendRemove":
		return u.backendRemove(c, raw)
	case "backendStates": // aucun état sondé en LOT-2 (le sondage des backends de l'utilisateur vient avec LOT-4) : listes vides
		return map[string]any{"states": map[string]any{}, "load": map[string]any{}}, nil
	case "clientInfo":
		out := map[string]any{"remote": c.Remote, "access": userAccess(c), "space": u.info()}
		if ui := c.userInfo(); ui != nil {
			out["user"] = ui
		}
		return out, nil
	case "init":
		out := map[string]any{"state": u.snapshot(), "cards": nil, "help": helpData(), "prefs": u.prefs(), "server": serverInfo(),
			"access": userAccess(c), "space": u.info()}
		if ui := c.userInfo(); ui != nil {
			out["user"] = ui
		}
		return out, nil
	case "setPrefs":
		var patch map[string]any
		if err := json.Unmarshal(raw, &patch); err != nil {
			return nil, fmt.Errorf("setPrefs : %v", err)
		}
		return u.setPrefs(patch)
	case "setTheme":
		return nil, nil // le thème d'une fenêtre native n'existe pas ici ; la préférence passe par setPrefs
	}
	var in struct {
		Kind    string              `json:"kind"`
		Path    string              `json:"path"`
		Content string              `json:"content"`
		MTime   string              `json:"mtime"`
		Changes []core.ConfigChange `json:"changes"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, fmt.Errorf("%s : arguments invalides : %v", method, err)
		}
	}
	if method != "configFiles" {
		if msg := u.admitCfg(time.Now()); msg != "" {
			return map[string]any{"ok": false, "code": "rate", "status": 429, "msg": msg}, nil
		}
	}
	// éditeur de configuration : un seul fichier, le backends.yaml de l'espace ; aucun chemin venu du client
	if method != "configFiles" && core.ConfigKind(in.Kind) != core.ConfBackends {
		return nil, fmt.Errorf("%s : rubrique %s réservée à l'administrateur", method, shortKind(in.Kind))
	}
	if in.Path != "" {
		return nil, fmt.Errorf("%s : %s", method, msgNoSpaceMethod)
	}
	switch method {
	case "configFiles":
		return []core.ConfigFile{u.backendsFile()}, nil
	case "configOpen":
		return u.configOpen()
	case "configValidate":
		ps := u.sp.ValidateBackends([]byte(in.Content))
		c.noteRefusal(core.FirstProblemCat(ps))
		return map[string]any{"problems": ps, "merged": ""}, nil
	case "configSave":
		return u.configSave(c, in.Content, in.MTime)
	case "configModel":
		return u.configModel()
	case "configApply":
		return u.configApply(c, in.Changes, in.MTime)
	}
	return nil, fmt.Errorf("%s : %s", method, msgNoSpaceMethod)
}

// backendsFile : fichier de l'espace, sans chemin du serveur (le client ne voit jamais l'identifiant de son espace).
func (u *userSpace) backendsFile() core.ConfigFile {
	f := u.sp.BackendsFile()
	f.Path = "backends.yaml"
	return f
}

func (u *userSpace) configOpen() (ConfigDoc, error) {
	f := u.backendsFile()
	d := ConfigDoc{ConfigFile: f, Problems: []core.ConfigProblem{}}
	if f.Exists {
		b, err := os.ReadFile(u.sp.BackendsPath())
		if err != nil {
			return d, err
		}
		d.Content, d.MTime = string(b), f.ModTime.Format(time.RFC3339Nano)
	} else {
		d.Content, d.FromTemp = core.ConfigTemplate(core.ConfBackends), true
	}
	if ps := u.sp.ValidateBackends([]byte(d.Content)); ps != nil {
		d.Problems = ps
	}
	return d, nil
}

// configModel : le modèle structuré de SON backends.yaml (éditeur de backends actuel) ; jamais le fichier de l'admin.
func (u *userSpace) configModel() (any, error) {
	d, err := u.configOpen()
	if err != nil {
		return nil, err
	}
	content := d.Content
	if d.FromTemp {
		content = "" // fichier absent : l'exemple commenté n'est pas une configuration
	}
	// problèmes de LECTURE du modèle : les erreurs seulement ; les avertissements (docker sans TLS vérifié…) restent affichés en permanence
	// sur la carte du backend (d'après `tls` du modèle), pas comme un défaut de lecture
	errs := []core.ConfigProblem{}
	for _, p := range d.Problems {
		if p.Severity == core.SevError {
			errs = append(errs, p)
		}
	}
	d.Problems = errs
	model, merr := core.ReadBackendsModel([]byte(content))
	if merr != nil && len(d.Problems) == 0 {
		d.Problems = core.ScrubProblems([]core.ConfigProblem{{Severity: core.SevError, Message: merr.Error()}})
	}
	return map[string]any{"path": d.Path, "mtime": d.MTime, "exists": d.Exists, "problems": d.Problems, "model": model}, nil
}

// configApply : changements structurés appliqués à SON backends.yaml, puis enregistrés comme configSave. Un contenu qui touche un
// champ R1 est refusé (forbidden) ; un changement inapplicable donne {invalid, problems}.
func (u *userSpace) configApply(c webCall, changes []core.ConfigChange, mtime string) (any, error) {
	var cur []byte
	if b, err := os.ReadFile(u.sp.BackendsPath()); err == nil {
		cur = b
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	out, err := core.ApplyConfigChanges(core.ConfBackends, cur, changes)
	if err != nil {
		return map[string]any{"invalid": true, "problems": core.ScrubProblems([]core.ConfigProblem{{Severity: core.SevError, Message: err.Error()}})}, nil
	}
	if core.UserSSHRefused(out) {
		return map[string]any{"invalid": true, "problems": u.sp.ValidateBackends(out)}, nil
	}
	if core.UserAdminFields(out) {
		return map[string]any{"ok": false, "forbidden": core.RoleAdmin, "msg": msgR1}, nil
	}
	return u.configSave(c, string(out), mtime)
}

func (u *userSpace) configSave(c webCall, content, mtime string) (any, error) {
	var expected time.Time
	if mtime != "" {
		var err error
		if expected, err = time.Parse(time.RFC3339Nano, mtime); err != nil {
			return nil, fmt.Errorf("date d'ouverture invalide : %v", err)
		}
	}
	ps, err := u.sp.SaveBackends([]byte(content), expected)
	switch {
	case errors.Is(err, core.ErrConfigConflict):
		cur := ""
		if st, e := os.Stat(u.sp.BackendsPath()); e == nil {
			cur = st.ModTime().Format(time.RFC3339Nano)
		}
		return map[string]any{"conflict": true, "mtime": cur, "problems": ps}, nil
	case errors.Is(err, core.ErrConfigInvalid):
		c.noteRefusal(core.FirstProblemCat(ps))
		// un refus ne modifie RIEN (ni fichier, ni .bak) ; la date actuelle est rendue pour que l'éditeur reste synchronisé
		return map[string]any{"invalid": true, "problems": ps, "mtime": u.currentMTime()}, nil
	case err != nil:
		return nil, err
	}
	out := map[string]any{"saved": true, "problems": ps, "note": core.ConfigReloadNote(core.ConfBackends)}
	if st, e := os.Stat(u.sp.BackendsPath()); e == nil {
		out["mtime"] = st.ModTime().Format(time.RFC3339Nano)
	}
	return out, nil
}

// info : ce que l'interface sait de l'espace — jamais l'identifiant numérique ni un chemin du serveur.
func (u *userSpace) info() map[string]any {
	bs, err := u.sp.LoadBackends()
	rows := []map[string]any{}
	for _, b := range bs {
		rows = append(rows, map[string]any{"name": b.Name, "type": b.Type})
	}
	out := map[string]any{"admin": false, "login": u.login, "backends": rows, "empty": len(rows) == 0}
	if err != nil {
		out["error"] = "backends.yaml : " + err.Error()
	}
	return out
}

// snapshot : état d'interface d'un utilisateur — pas de projets (LOT-3.2), aucune donnée de la racine.
func (u *userSpace) snapshot() userSnapshot {
	return userSnapshot{Snapshot: Snapshot{Version: core.NgName + " " + core.Version(), Tpl: "—", Alerts: []Alert{}, Usage: []UsageRow{}, Rows: []Row{},
		Tabs: []TabInfo{}, Roots: []string{}, OS: goos, Server: serverInfo(), WebUI: WebUIState{State: "on", Server: true, Enabled: true}}, Rows: u.rows()}
}

func (u *userSpace) prefs() map[string]any {
	st, err := u.sp.LoadSettings()
	if err != nil || st.Display == nil {
		return map[string]any{}
	}
	return st.Display
}

func (u *userSpace) setPrefs(patch map[string]any) (any, error) {
	u.prefsMu.Lock()
	defer u.prefsMu.Unlock()
	st, err := u.sp.LoadSettings()
	if err != nil {
		return nil, err
	}
	if st.Display == nil {
		st.Display = map[string]any{}
	}
	for k, v := range patch {
		if v == nil {
			delete(st.Display, k)
		} else {
			st.Display[k] = v
		}
	}
	if err := u.sp.SaveSettings(st); err != nil {
		return nil, err
	}
	return st.Display, nil
}

// userList : méthode de l'admin — utilisateurs connus, nombre de backends, état ; jamais d'identifiant.
func userList() any { return map[string]any{"users": core.ListUsers()} }

// userTarget : identifiant court d'un appel R1 refusé pour le journal (kind de configuration ou nom de backend), jamais une valeur.
func userTarget(method string, raw json.RawMessage) string {
	var in struct {
		Kind    string
		Backend json.RawMessage
	}
	_ = json.Unmarshal(raw, &in)
	if method == "configSave" || method == "configApply" {
		return in.Kind
	}
	var name string
	_ = json.Unmarshal(in.Backend, &name)
	return name
}

// shortKind : kind d'une rubrique pour un message — jamais la valeur brute du client (tronquée, entre guillemets échappés).
func shortKind(k string) string {
	if r := []rune(k); len(r) > 24 {
		k = string(r[:24]) + "…"
	}
	return strconv.Quote(k)
}

// secretUserOK : l'appel de coffre vise un secret que l'utilisateur dépose pour son backend : kubeconfig (k8s) ou CA / certificat / clé
// TLS (docker, podman). Clés ssh et known_hosts : admin seulement.
func secretUserOK(method string, raw json.RawMessage) bool {
	var in struct {
		Kubeconfig, CA, Cert, Key, What string
	}
	_ = json.Unmarshal(raw, &in)
	switch method {
	case "backendSecretSet":
		return in.Kubeconfig != "" || in.CA != "" || in.Cert != "" || in.Key != ""
	case "backendSecretDelete":
		return in.What == "kubeconfig" || in.What == "tls"
	}
	return false
}

// secretCall : coffre de l'utilisateur — SON espace seulement (users/<uid>/vault/<backend>/), backend déclaré dans SON backends.yaml.
// Réponses sans contenu, sans chemin, sans identifiant.
func (u *userSpace) secretCall(c webCall, method string, raw json.RawMessage) (any, error) {
	var in struct {
		Backend    string `json:"backend"`
		Kubeconfig string `json:"kubeconfig"`
		CA         string `json:"ca"`
		Cert       string `json:"cert"`
		Key        string `json:"key"`
		What       string `json:"what"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("%s : arguments invalides", method)
	}
	fail := func(msg string) (any, error) { return map[string]any{"ok": false, "msg": core.ScrubMessage(msg)}, nil }
	if err := core.CheckUserBackendName(in.Backend); err != nil {
		return fail(err.Error())
	}
	bs, err := u.sp.LoadBackends()
	if err != nil {
		return fail("backends.yaml illisible : corriger la déclaration d'abord")
	}
	kind := ""
	for _, b := range bs {
		if b.Name == in.Backend {
			kind = b.Type
		}
	}
	tlsCall := in.CA != "" || in.Cert != "" || in.Key != "" || in.What == "tls"
	switch {
	case kind == "":
		return fail("backend inconnu dans vos réglages : l'enregistrer avant d'y déposer un secret")
	case tlsCall && kind != "docker" && kind != "podman":
		return fail("les certificats TLS ne concernent que les backends docker")
	case !tlsCall && method != "backendSecretGet" && kind != "k8s":
		return fail("le kubeconfig ne concerne que les backends k8s")
	}
	w := core.SecretWrite{Source: "navigateur", User: c.userLabel()}
	if method == "backendSecretSet" { // analyse YAML 64 Kio et DNS : même plafond que backendTest (audit m4)
		if msg := u.admitSecret(time.Now()); msg != "" {
			return map[string]any{"ok": false, "code": "rate", "status": 429, "msg": msg}, nil
		}
	}
	switch method {
	case "backendSecretGet":
		return u.sp.SecretInfo(in.Backend)
	case "backendSecretDelete":
		var err error
		if tlsCall {
			err = u.sp.SecretDeleteTLS(in.Backend, w)
		} else {
			err = u.sp.SecretDeleteKubeconfig(in.Backend, w)
		}
		if err != nil {
			return fail(err.Error())
		}
		return map[string]any{"ok": true}, nil
	}
	if tlsCall {
		infos, err := u.sp.SecretSetTLS(in.Backend, in.CA, in.Cert, in.Key, w)
		if err != nil {
			return fail(err.Error())
		}
		warns := core.TLSExpiryWarnings(infos)
		if warns == nil {
			warns = []string{}
		}
		return map[string]any{"ok": true, "tls": infos, "warnings": warns, "msg": "certificats enregistrés"}, nil
	}
	ctxs, fp, warns, err := u.sp.SecretSetKubeconfig(in.Backend, in.Kubeconfig, w)
	if err != nil {
		return fail(err.Error())
	}
	if warns == nil {
		warns = []string{}
	}
	return map[string]any{"ok": true, "fingerprint": fp, "contexts": ctxs, "warnings": warns, "msg": "kubeconfig enregistré"}, nil
}

// backendTest : test d'un backend de l'utilisateur — 6 par minute au plus et un seul à la fois (S31, D5) ; diagnostic grossier.
func (u *userSpace) backendTest(c webCall, raw json.RawMessage) (any, error) {
	var in struct {
		Backend core.BackendEntry `json:"backend"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("backendTest : arguments invalides")
	}
	if msg := u.admitTest(time.Now()); msg != "" {
		return map[string]any{"ok": false, "status": 429, "msg": msg}, nil
	}
	defer u.testing.Store(false)
	steps := u.sp.TestBackend(in.Backend)
	for _, st := range steps {
		c.noteRefusal(st.Cat)
	}
	return map[string]any{"steps": steps}, nil
}

// admitTest : "" si le test peut partir ; sinon le message du refus 429. Réservé tant que le test tourne (testing).
func (u *userSpace) admitTest(now time.Time) string {
	if !u.testing.CompareAndSwap(false, true) {
		return "429 : un seul test à la fois — un test est déjà en cours"
	}
	u.tmu.Lock()
	defer u.tmu.Unlock()
	keep := u.tests[:0]
	for _, t := range u.tests {
		if now.Sub(t) < time.Minute {
			keep = append(keep, t)
		}
	}
	u.tests = keep
	if max := core.UserTestsPerMinute(); len(u.tests) >= max {
		u.testing.Store(false)
		return fmt.Sprintf("429 : limite atteinte (%d tests par minute)", max)
	}
	u.tests = append(u.tests, now)
	return ""
}

// userSpaceServed : méthodes R1 par nature que l'instance d'un utilisateur sert pourtant dans SON espace (kubeconfig et certificats de
// ses backends) : retirées de access.deny (source unique du grisage de l'interface). backendKnownHostsSet (ssh) reste refusé.
var userSpaceServed = map[string]bool{"backendSecretSet": true, "backendSecretDelete": true}

// userAccess : access de l'utilisateur, sans les méthodes que son espace sert.
func userAccess(c webCall) core.AccessInfo {
	ai := c.accessInfo()
	deny := make([]string, 0, len(ai.Deny))
	for _, m := range ai.Deny {
		if !userSpaceServed[m] {
			deny = append(deny, m)
		}
	}
	ai.Deny = deny
	return ai
}

// admitSecret : "" si le dépôt de secret peut partir ; sinon le message 429 (10 fois le plafond de backendTest par minute : un
// collage s'essaie plusieurs fois, mais l'analyse YAML et le DNS ne sont pas illimités).
func (u *userSpace) admitSecret(now time.Time) string {
	u.tmu.Lock()
	defer u.tmu.Unlock()
	keep := u.secrets[:0]
	for _, t := range u.secrets {
		if now.Sub(t) < time.Minute {
			keep = append(keep, t)
		}
	}
	u.secrets = keep
	if max := 10 * core.UserTestsPerMinute(); len(u.secrets) >= max { // 60/min par défaut : les essais d'un collage restent possibles
		return fmt.Sprintf("429 : limite atteinte (%d dépôts de secret par minute)", max)
	}
	u.secrets = append(u.secrets, now)
	return ""
}

// noteRefusal : catégorie d'un refus d'hôte, pour l'audit serveur (jamais renvoyée à l'utilisateur).
func (c webCall) noteRefusal(cat string) {
	if c.Note != nil && cat != "" {
		c.Note(cat)
	}
}

// currentMTime : date actuelle du backends.yaml de l'espace ("" s'il n'existe pas).
func (u *userSpace) currentMTime() string {
	if st, err := os.Stat(u.sp.BackendsPath()); err == nil {
		return st.ModTime().Format(time.RFC3339Nano)
	}
	return ""
}

// refusalMap : refus de validation en {ok:false, field, code, msg} ; la raison interne d'un refus d'hôte part à l'audit, jamais à l'utilisateur.
func refusalMap(c webCall, e *core.AddError) map[string]any {
	c.noteRefusal(e.Reason)
	m := map[string]any{"ok": false, "code": e.Code, "msg": e.Msg}
	if e.Field != "" {
		m["field"] = e.Field
	}
	return m
}

// backendAdd : création atomique d'un backend avec ses secrets (contrat §3) ; compte dans le plafond de dépôts (60/min).
func (u *userSpace) backendAdd(c webCall, raw json.RawMessage) (any, error) {
	var in struct {
		Backend map[string]any `json:"backend"`
		Secrets struct {
			Kubeconfig string `json:"kubeconfig"`
			TLS        struct {
				CA   string `json:"ca"`
				Cert string `json:"cert"`
				Key  string `json:"key"`
			} `json:"tls"`
		} `json:"secrets"`
		MTime string `json:"mtime"`
	}
	if err := json.Unmarshal(raw, &in); err != nil || in.Backend == nil {
		return nil, fmt.Errorf("userBackendAdd : arguments invalides")
	}
	if msg := u.admitSecret(time.Now()); msg != "" {
		return map[string]any{"ok": false, "code": "rate", "status": 429, "msg": msg}, nil
	}
	var req core.AddRequest
	req.Backend, req.MTime = in.Backend, in.MTime
	req.Secrets.Kubeconfig = in.Secrets.Kubeconfig
	req.Secrets.TLS.CA, req.Secrets.TLS.Cert, req.Secrets.TLS.Key = in.Secrets.TLS.CA, in.Secrets.TLS.Cert, in.Secrets.TLS.Key
	res, e := u.sp.AddBackend(req, core.SecretWrite{Source: "navigateur", User: c.userLabel()})
	if e != nil {
		return refusalMap(c, e), nil
	}
	warns := res.Warnings
	if warns == nil {
		warns = []string{}
	}
	return map[string]any{"ok": true, "backend": map[string]any{"name": res.Name, "type": res.Type}, "warnings": warns}, nil
}

// backendRemove : retire un backend de l'utilisateur (déclaration, coffre, traces).
func (u *userSpace) backendRemove(c webCall, raw json.RawMessage) (any, error) {
	var in struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("userBackendRemove : arguments invalides")
	}
	if msg := u.admitSecret(time.Now()); msg != "" { // compté dans le plafond des dépôts (audit m10)
		return map[string]any{"ok": false, "code": "rate", "status": 429, "msg": msg}, nil
	}
	if e := u.sp.RemoveBackend(in.Name, core.SecretWrite{Source: "navigateur", User: c.userLabel()}); e != nil {
		return refusalMap(c, e), nil
	}
	return map[string]any{"ok": true}, nil
}

// admitCfg : "" si l'appel d'éditeur peut partir ; sinon le message 429 (10 fois le plafond de backendTest par minute : l'analyse YAML et la
// résolution DNS ne sont pas illimitées, mais l'éditeur n'est jamais gêné).
func (u *userSpace) admitCfg(now time.Time) string {
	u.tmu.Lock()
	defer u.tmu.Unlock()
	keep := u.cfgs[:0]
	for _, t := range u.cfgs {
		if now.Sub(t) < time.Minute {
			keep = append(keep, t)
		}
	}
	u.cfgs = keep
	if max := 10 * core.UserTestsPerMinute(); len(u.cfgs) >= max {
		return fmt.Sprintf("429 : limite atteinte (%d appels d'éditeur par minute)", max)
	}
	u.cfgs = append(u.cfgs, now)
	return ""
}
