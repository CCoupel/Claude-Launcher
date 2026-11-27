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
	mu sync.Mutex
	m  map[string]*userSpace
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
				if method == "configSave" && core.UserAdminFields([]byte(in.Content)) {
					return forbidden, nil
				}
			case core.ConfProjectEnv: // pas de projet dans son espace (LOT-3.2) : ni refus R1 ni écriture
				return nil, fmt.Errorf("%s : %s", method, msgNoSpaceMethod)
			default:
				return forbidden, nil
			}
		} else {
			return forbidden, nil
		}
	}
	if !userMethods[method] {
		return nil, fmt.Errorf("%s : %s", method, msgNoSpaceMethod)
	}
	switch method {
	case "backendStates": // aucun état sondé en LOT-2 (le sondage des backends de l'utilisateur vient avec LOT-4) : listes vides
		return map[string]any{"states": map[string]any{}, "load": map[string]any{}}, nil
	case "clientInfo":
		out := map[string]any{"remote": c.Remote, "access": c.accessInfo(), "space": u.info()}
		if ui := c.userInfo(); ui != nil {
			out["user"] = ui
		}
		return out, nil
	case "init":
		out := map[string]any{"state": u.snapshot(), "cards": nil, "help": helpData(), "prefs": u.prefs(), "server": serverInfo(),
			"access": c.accessInfo(), "space": u.info()}
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
		return map[string]any{"problems": u.sp.ValidateBackends([]byte(in.Content)), "merged": ""}, nil
	case "configSave":
		return u.configSave(in.Content, in.MTime)
	case "configModel":
		return u.configModel()
	case "configApply":
		return u.configApply(in.Changes, in.MTime)
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
	model, merr := core.ReadBackendsModel([]byte(content))
	if merr != nil && len(d.Problems) == 0 {
		d.Problems = []core.ConfigProblem{{Severity: core.SevError, Message: merr.Error()}}
	}
	return map[string]any{"path": d.Path, "mtime": d.MTime, "exists": d.Exists, "problems": d.Problems, "model": model}, nil
}

// configApply : changements structurés appliqués à SON backends.yaml, puis enregistrés comme configSave. Un contenu qui touche un
// champ R1 est refusé (forbidden) ; un changement inapplicable donne {invalid, problems}.
func (u *userSpace) configApply(changes []core.ConfigChange, mtime string) (any, error) {
	var cur []byte
	if b, err := os.ReadFile(u.sp.BackendsPath()); err == nil {
		cur = b
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	out, err := core.ApplyConfigChanges(core.ConfBackends, cur, changes)
	if err != nil {
		return map[string]any{"invalid": true, "problems": []core.ConfigProblem{{Severity: core.SevError, Message: err.Error()}}}, nil
	}
	if core.UserAdminFields(out) {
		return map[string]any{"ok": false, "forbidden": core.RoleAdmin, "msg": msgR1}, nil
	}
	return u.configSave(string(out), mtime)
}

func (u *userSpace) configSave(content, mtime string) (any, error) {
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
		return map[string]any{"invalid": true, "problems": ps}, nil
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
func (u *userSpace) snapshot() Snapshot {
	return Snapshot{Version: core.NgName + " " + core.Version(), Tpl: "—", Alerts: []Alert{}, Usage: []UsageRow{}, Rows: []Row{},
		Tabs: []TabInfo{}, Roots: []string{}, OS: goos, Server: serverInfo(), WebUI: WebUIState{State: "on", Server: true, Enabled: true}}
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
