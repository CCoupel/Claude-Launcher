package desktop

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"claude-launcher-ng2/core"
)

// Projets d'un UTILISATEUR (LOT-3.2, contrat §5, §5 bis, §6) : la liste vient du BACKEND (découverte par labels) complétée par la trace ; un projet est
// identifié par « <backend>/<catégorie>/<nom> » (jamais un chemin du serveur). Aucune lecture du stockage de l'utilisateur par le launcher.

// rowGone : projet dont l'objet a disparu du backend (trace sans objet).
type rowGone struct {
	Backend string `json:"backend"`
	Seen    string `json:"seen"`
}

// rowUnreach : projet d'un backend injoignable (trace seulement).
type rowUnreach struct {
	Backend string `json:"backend"`
	Reason  string `json:"reason"`
}

// userRow : ligne de l'arbre d'un utilisateur. « missing » (objet) masque le booléen de Row du même nom.
type userRow struct {
	Row
	Missing     *rowGone    `json:"missing,omitempty"`
	Stopped     bool        `json:"stopped,omitempty"`
	StateAt     string      `json:"stateAt,omitempty"`
	Unreachable *rowUnreach `json:"unreachable,omitempty"`
	Backend     string      `json:"backend,omitempty"`
}

// userSnapshot : état d'interface d'un utilisateur (rows de projets).
type userSnapshot struct {
	Snapshot
	Rows []userRow `json:"rows"`
}

// découverte : cache de 10 s par backend (la cadence du sondage), jamais plusieurs lectures simultanées d'un même backend.
type userDisc struct {
	at       time.Time
	projects []core.UserProject
	err      error
	loaded   bool
}

type userProjects struct {
	pr   *core.Prober
	mu   sync.Mutex
	disc map[string]*userDisc
}

const userDiscTTL = 10 * time.Second

func (u *userSpace) engines() (map[string]core.Engine, []core.BackendConf, error) {
	bs, err := u.sp.LoadBackends()
	if err != nil {
		return nil, nil, err
	}
	m := map[string]core.Engine{}
	for _, b := range bs {
		if e, ok := core.EngineOf(b); ok {
			m[b.Name] = e
		}
	}
	return m, bs, nil
}

// discover : projets découverts par backend (cache), trace mise à jour pour chaque découverte réussie. force : relecture immédiate.
func (u *userSpace) discover(force bool) (map[string]*userDisc, []core.BackendConf) {
	es, bs, err := u.engines()
	if err != nil {
		return map[string]*userDisc{}, nil
	}
	u.proj.mu.Lock()
	defer u.proj.mu.Unlock()
	if u.proj.disc == nil {
		u.proj.disc = map[string]*userDisc{}
	}
	out := map[string]*userDisc{}
	for _, b := range bs {
		d := u.proj.disc[b.Name]
		if d == nil || force { // première lecture ou demande explicite : synchrone
			d = u.probeOnce(b, es[b.Name])
			u.proj.disc[b.Name] = d
		} else if time.Since(d.at) > userDiscTTL { // rafraîchissement : sondeur (10 s → 5 min d'attente en cas d'échec, 16 en vol au plus), hors de l'appel
			b, e := b, es[b.Name]
			u.proj.prober().Run(b.Name, func() error {
				nd := u.probeOnce(b, e)
				u.proj.mu.Lock()
				if u.proj.disc != nil {
					u.proj.disc[b.Name] = nd
				}
				u.proj.mu.Unlock()
				u.pushState()
				return nd.err
			})
		}
		out[b.Name] = d
	}
	return out, bs
}

// probeOnce : une lecture du backend (découverte) et mise à jour de la trace.
func (u *userSpace) probeOnce(b core.BackendConf, e core.Engine) *userDisc {
	ps, derr := e.DiscoverUserProjects()
	if derr == nil {
		for _, p := range ps {
			_ = u.sp.RecordTrace(core.Trace{Backend: b.Name, Kind: b.Type, Object: p.Object, Project: p.Project, Category: p.Category})
		}
	}
	return &userDisc{at: time.Now(), projects: ps, err: derr, loaded: true}
}

func (p *userProjects) prober() *core.Prober {
	if p.pr == nil {
		p.pr = core.NewProber(time.Now)
	}
	return p.pr
}

func (u *userSpace) invalidate() {
	u.proj.mu.Lock()
	u.proj.disc = nil
	u.proj.mu.Unlock()
}

func (u *userSpace) pushState() {
	if u.emit != nil {
		u.emit("state", u.snapshot())
	}
}

// rows : arbre « backend → catégorie → projet » (découverte + trace).
func (u *userSpace) rows() []userRow {
	disc, bs := u.discover(false)
	traces, _ := u.sp.LoadTraces()
	var rows []userRow
	names := make([]string, 0, len(bs))
	for _, b := range bs {
		names = append(names, b.Name)
	}
	sort.Strings(names)
	for _, bn := range names {
		d := disc[bn]
		rows = append(rows, userRow{Row: Row{Kind: "root", Name: bn, Path: bn, Depth: 0}, Backend: bn})
		type item struct {
			cat, name string
			row       userRow
		}
		var items []item
		seen := map[string]bool{}
		if d != nil && d.err == nil {
			for _, p := range d.projects {
				id := core.UserProjectID(bn, p.Category, p.Project)
				seen[id] = true
				r := userRow{Row: Row{Kind: "project", Name: p.Project, Path: id, Depth: 2, Open: u.term(id) != nil}, Backend: bn, Stopped: !p.Running}
				if p.Meta != nil {
					r.StateAt = p.Meta.Seen.UTC().Format(time.RFC3339)
				}
				if p.Running {
					r.StateAt = time.Now().UTC().Format(time.RFC3339)
				}
				items = append(items, item{p.Category, p.Project, r})
			}
		}
		for _, t := range traces {
			id := core.UserProjectID(bn, t.Category, t.Project)
			if t.Backend != bn || seen[id] {
				continue
			}
			r := userRow{Row: Row{Kind: "project", Name: t.Project, Path: id, Depth: 2}, Backend: bn}
			if d == nil || d.err != nil { // backend injoignable : la trace seule
				r.Unreachable = &rowUnreach{Backend: bn, Reason: "backend injoignable"}
			} else { // objet disparu d'un backend joignable
				r.Missing = &rowGone{Backend: bn, Seen: t.LastSeen.UTC().Format(time.RFC3339)}
			}
			items = append(items, item{t.Category, t.Project, r})
		}
		sort.Slice(items, func(i, j int) bool {
			if items[i].cat != items[j].cat {
				return items[i].cat < items[j].cat
			}
			return items[i].name < items[j].name
		})
		cat := ""
		for _, it := range items {
			if it.cat != cat {
				cat = it.cat
				rows = append(rows, userRow{Row: Row{Kind: "cat", Name: cat, Path: bn + "/" + cat, Depth: 1}, Backend: bn})
			}
			rows = append(rows, it.row)
		}
	}
	if rows == nil {
		rows = []userRow{}
	}
	return rows
}

// projectCount : projets de l'utilisateur, tous backends confondus (limite users.maxProjects).
func (u *userSpace) projectCount() int {
	disc, _ := u.discover(false)
	n := 0
	for _, d := range disc {
		if d != nil && d.err == nil {
			n += len(d.projects)
		}
	}
	return n
}

func (u *userSpace) findProject(id string) (core.Engine, core.UserProject, *core.BackendConf, bool, error) {
	bn, cat, name, err := core.ParseUserProjectID(id)
	if err != nil {
		return core.Engine{}, core.UserProject{}, nil, false, err
	}
	es, bs, lerr := u.engines()
	e, ok := es[bn]
	if lerr != nil || !ok {
		return core.Engine{}, core.UserProject{}, nil, false, fmt.Errorf("backend inconnu")
	}
	var b *core.BackendConf
	for i := range bs {
		if bs[i].Name == bn {
			b = &bs[i]
		}
	}
	disc, _ := u.discover(false)
	if d := disc[bn]; d != nil && d.err == nil {
		for _, p := range d.projects {
			if p.Category == cat && p.Project == name {
				return e, p, b, true, nil
			}
		}
	}
	return e, core.UserProject{Backend: bn, Category: cat, Project: name}, b, false, nil
}

// refusal : refus nommé {ok:false, field?, code, msg}.
func refusal(field, code, msg string) map[string]any {
	m := map[string]any{"ok": false, "code": code, "msg": core.ScrubMessage(msg)}
	if field != "" {
		m["field"] = field
	}
	return m
}

type userProjArgs struct {
	Path, Backend, Project, Category, Name, Image, Repo string
	Confirm                                             string
	Pane, Data, Kind, Choice, SHA                       string
	Binary                                              bool
	Cols, Rows                                          int
	DryRun                                              bool
	Options                                             json.RawMessage
}

// projectCall : méthodes projets de l'instance utilisateur.
func (u *userSpace) projectCall(c webCall, method string, raw json.RawMessage) (any, error) {
	var a userProjArgs
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, fmt.Errorf("%s : arguments invalides", method)
		}
	}
	switch method {
	case "select", "cardsFor":
		e, p, b, found, err := u.findProject(a.Path)
		if err != nil {
			return nil, nil
		}
		if found && !p.Running && p.Meta == nil && b != nil && !b.K8s() { // docker/podman arrêté : state.json lu par cp
			if m, merr := e.ReadStoppedState(p.Category, p.Project); merr == nil {
				p.Meta = m
			}
		}
		card := map[string]any{"path": a.Path, "name": p.Project, "backend": p.Backend, "stopped": found && !p.Running, "found": found}
		if b != nil && found {
			card["image"] = p.Image
		}
		if p.Meta != nil {
			card["stateAt"], card["git"] = p.Meta.Seen.UTC().Format(time.RFC3339), map[string]any{"branch": p.Meta.Branch, "dirty": p.Meta.Dirty, "ahead": p.Meta.Ahead, "behind": p.Meta.Behind, "head": p.Meta.Head}
			card["tpl"], card["team"] = p.Meta.Tpl, p.Meta.Team
		}
		return card, nil
	case "refresh", "refreshProject":
		return u.refreshProject(a.Path)
	case "projectPurge":
		return u.projectPurge(a)
	case "projectMove":
		return u.projectMove(a)
	case "projectDelete":
		return u.projectDelete(a)
	case "open":
		return u.openUserProject(a.Path)
	case "close", "forceClose", "closeConfirm", "forceCloseConfirm":
		return u.closeUserProject(a.Path)
	case "envTrustAnswer":
		return u.envTrustAnswer(a)
	case "termInput", "termResize", "termFocus", "termZoom", "resizePane", "launchInput", "termSync":
		return u.termCall(method, a)
	case "newInfo":
		return u.newInfo(), nil
	case "create":
		return u.createProject(c, a)
	case "projectSettings":
		return u.projectSettings(a)
	case "projectApply":
		return u.projectApply(a)
	}
	return nil, fmt.Errorf("%s : %s", method, msgNoSpaceMethod)
}

func (u *userSpace) newInfo() map[string]any {
	_, bs, _ := u.engines()
	img := core.UserDefaultImage()
	var list []map[string]any
	for _, b := range bs {
		list = append(list, map[string]any{"name": b.Name, "type": b.Type, "image": img})
	}
	if list == nil {
		list = []map[string]any{}
	}
	return map[string]any{"backends": list, "defaultImage": img, "maxProjects": core.UserMaxProjects(), "count": u.projectCount()}
}

// createProject : crée l'objet labellisé du projet (arrêté) sur le backend choisi ; limite de projets, noms, image par défaut.
func (u *userSpace) createProject(c webCall, a userProjArgs) (any, error) {
	if msg := u.admitCfg(time.Now()); msg != "" {
		return map[string]any{"ok": false, "code": "rate", "status": 429, "msg": msg}, nil
	}
	es, _, _ := u.engines()
	e, ok := es[a.Backend]
	if !ok {
		return refusal("backend", "invalid", "backend inconnu dans vos réglages"), nil
	}
	if msg := core.ValidUserProjectName(a.Category); msg != "" {
		return refusal("category", "invalid", msg), nil
	}
	if msg := core.ValidUserProjectName(a.Name); msg != "" {
		return refusal("name", "invalid", msg), nil
	}
	if a.Repo != "" {
		if _, err := core.ValidateCloneURL(a.Repo); err != nil {
			return refusal("repo", "invalid", err.Error()), nil
		}
	}
	if p := core.CheckProjectLimit(u.projectCount()); p != nil {
		return refusal("", p.Code, p.Msg), nil
	}
	image := a.Image
	if image == "" {
		image = core.UserDefaultImage()
	}
	if err := core.CheckUserImage(image); err != nil {
		return refusal("image", "invalid", err.Error()), nil
	}
	var err error
	if a.Repo != "" { // clone DANS le conteneur (image par défaut : la seule admise)
		err = u.sp.CreateProject(e, a.Category, a.Name, a.Repo)
	} else {
		err = e.CreateUserProject(a.Category, a.Name, image)
	}
	if err != nil {
		code := "invalid"
		if strings.Contains(err.Error(), "déjà ce nom") {
			return refusal("name", "conflict", err.Error()), nil
		}
		return refusal("", code, err.Error()), nil
	}
	u.invalidate()
	u.pushState()
	return map[string]any{"ok": true, "path": core.UserProjectID(a.Backend, a.Category, a.Name)}, nil
}

// refreshProject (F5) : un projet arrêté est démarré (sans session claude), puis l'état est relu ; jamais par le sondage.
func (u *userSpace) refreshProject(id string) (any, error) {
	if msg := u.admitCfg(time.Now()); msg != "" {
		return map[string]any{"ok": false, "code": "rate", "status": 429, "msg": msg}, nil
	}
	u.invalidate() // F5 : relecture immédiate du backend
	e, p, _, found, err := u.findProject(id)
	if err != nil {
		return refusal("path", "invalid", err.Error()), nil
	}
	if !found {
		u.invalidate()
		u.pushState()
		return refusal("", "invalid", "projet introuvable sur le backend"), nil
	}
	started := false
	if !p.Running {
		if err := e.StartUserProject(p.Category, p.Project); err != nil {
			return refusal("", "invalid", err.Error()), nil
		}
		started = true
	}
	_ = e.RefreshUserMeta(p.Category, p.Project, time.Now()) // état rangé sur le backend ; un échec n'empêche pas le F5
	u.invalidate()
	u.pushState()
	return map[string]any{"ok": true, "started": started, "steps": []string{"start"}}, nil
}

// projectPurge : efface la TRACE d'un projet dont l'objet a disparu ; jamais une commande vers le backend ni une suppression chez l'utilisateur.
func (u *userSpace) projectPurge(a userProjArgs) (any, error) {
	bn, cat, name := a.Backend, "", a.Project
	if a.Path != "" {
		var err error
		if bn, cat, name, err = core.ParseUserProjectID(a.Path); err != nil {
			return refusal("path", "invalid", err.Error()), nil
		}
	}
	if core.CheckUserBackendName(bn) != nil || core.ValidUserProjectName(name) != "" || (cat != "" && core.ValidUserProjectName(cat) != "") {
		return refusal("path", "invalid", "identifiant de projet invalide"), nil
	}
	// trace seulement : AUCUNE commande vers le backend (ni découverte ni suppression)
	var n int
	var perr error
	if cat != "" {
		n, perr = u.sp.PurgeProjectTrace(bn, cat, name)
	} else {
		n, perr = u.sp.PurgeTrace(bn, name)
	}
	if perr != nil || n == 0 {
		return refusal("", "invalid", "aucune trace à purger"), nil
	}
	where := "le stockage de ton backend"
	if bs, err := u.sp.LoadBackends(); err == nil {
		for _, b := range bs {
			if b.Name == bn && b.PVC != "" {
				where = "le PVC " + b.PVC
			} else if b.Name == bn && b.Volume != "" {
				where = "le volume " + b.Volume
			}
		}
	}
	return map[string]any{"ok": true, "msg": "trace purgée ; les fichiers éventuels restent dans " + where}, nil
}

// options d'un projet : formes échangées avec l'interface (champs simples + zone JSON « Spécification du pod »).
type userOptionsIn struct {
	Values  map[string]string `json:"values"`
	PodJSON string            `json:"podJson"`
	RunAs   struct {
		UID string `json:"uid"`
		GID string `json:"gid"`
	} `json:"runAs"`
}

func problemMaps(ps []core.OptionProblem) []map[string]any {
	out := make([]map[string]any, 0, len(ps))
	for _, p := range ps {
		m := map[string]any{"field": p.Field, "code": p.Code, "msg": p.Msg}
		if p.Path != "" {
			m["path"] = p.Path
		}
		out = append(out, m)
	}
	return out
}

func (u *userSpace) projectSettings(a userProjArgs) (any, error) {
	e, p, b, found, err := u.findProject(a.Path)
	if err != nil || b == nil {
		return refusal("path", "invalid", "projet inconnu"), nil
	}
	if !found {
		return refusal("", "invalid", "projet introuvable sur le backend"), nil
	}
	values, verr := e.UserProjectValues(p.Category, p.Project)
	if verr != nil {
		values = map[string]string{}
	}
	uid, gid := b.RunAsValues()
	ra := map[string]int64{"uid": uid, "gid": gid}
	opts := map[string]any{"backend": p.Backend, "image": p.Image, "shared": true, "values": values, "runAs": ra, "runAsDefault": ra}
	if b.K8s() {
		d := core.DefaultPodSimple()
		opts["kind"] = "k8s"
		opts["defaults"] = map[string]string{"cpuRequest": d.CPURequest, "memRequest": d.MemRequest, "cpuLimit": d.CPULimit, "memLimit": d.MemLimit, "ephemeral": d.Ephemeral}
		opts["podJson"], opts["podJsonDefault"] = "{}", "{}"
	} else {
		d := core.DefaultContainerRaw()
		opts["kind"] = "container"
		opts["defaults"] = map[string]string{"cpus": d.Cpus, "memory": d.Memory, "pids_limit": d.PidsLimit, "shm_size": d.ShmSize, "gpus": d.Gpus}
	}
	return map[string]any{"options": opts, "warnings": []string{core.SharedBackendWarning}}, nil
}

// projectApply : valide les options (même code que la saisie en direct : dryRun), puis les applique — k8s : patch du StatefulSet (au prochain
// démarrage) ; docker/podman : cpus, memory, pids_limit à chaud ; shm_size et gpus : recréation après confirmation (jeton lié au projet et aux options).
func (u *userSpace) projectApply(a userProjArgs) (any, error) {
	var in struct {
		Options userOptionsIn `json:"options"`
	}
	if len(a.Options) > 0 {
		var o userOptionsIn
		if err := json.Unmarshal(a.Options, &o); err != nil {
			return refusal("options", "invalid", "options illisibles"), nil
		}
		in.Options = o
	}
	e, p, b, found, err := u.findProject(a.Path)
	if err != nil || b == nil || !found {
		return refusal("path", "invalid", "projet introuvable sur le backend"), nil
	}
	o := in.Options
	var problems []core.OptionProblem
	if o.RunAs.UID != "" || o.RunAs.GID != "" {
		if ra, ps := core.ParseRunAs(o.RunAs.UID, o.RunAs.GID); len(ps) > 0 {
			problems = append(problems, ps...)
		} else if cu, cg := b.RunAsValues(); int64(ra.UID) != cu || int64(ra.GID) != cg {
			problems = append(problems, core.OptionProblem{Field: "pod.run_as", Code: "invalid", Msg: "changer l'utilisateur d'un projet existant n'est pas disponible dans cette version"})
		}
	}
	if b.K8s() {
		simple := core.PodSimple{CPURequest: o.Values["cpuRequest"], CPULimit: o.Values["cpuLimit"], MemRequest: o.Values["memRequest"], MemLimit: o.Values["memLimit"], Ephemeral: o.Values["ephemeral"]}
		var spec map[string]any
		if strings.TrimSpace(o.PodJSON) != "" {
			var sps []core.OptionProblem
			if spec, sps = core.ParsePodSpecJSON([]byte(o.PodJSON)); len(sps) > 0 {
				problems = append(problems, sps...)
				spec = nil
			}
		}
		po, ps := core.ParsePodOptions(simple, spec)
		problems = append(problems, ps...)
		if len(problems) > 0 {
			return map[string]any{"ok": false, "problems": problemMaps(problems)}, nil
		}
		if a.DryRun {
			return map[string]any{"ok": true, "dryRun": true}, nil
		}
		if err := e.ApplyPodOptions(p.Category, p.Project, po); err != nil {
			return refusal("", "invalid", err.Error()), nil
		}
		u.invalidate()
		return map[string]any{"ok": true, "applied": "next-start"}, nil
	}
	raw := core.ContainerRaw{Cpus: o.Values["cpus"], Memory: o.Values["memory"], PidsLimit: o.Values["pids_limit"], ShmSize: o.Values["shm_size"], Gpus: o.Values["gpus"]}
	co, ps := core.ParseContainerOptionsFor(b.Type, raw)
	problems = append(problems, ps...)
	if len(problems) > 0 {
		return map[string]any{"ok": false, "problems": problemMaps(problems)}, nil
	}
	if a.DryRun {
		return map[string]any{"ok": true, "dryRun": true}, nil
	}
	cur, _ := e.UserProjectValues(p.Category, p.Project)
	var changes []map[string]string
	for k, nv := range map[string]string{"shm_size": co.ShmSize, "gpus": co.Gpus} {
		if nv != "" && nv != cur[k] {
			changes = append(changes, map[string]string{"key": k, "from": cur[k], "to": nv})
		}
	}
	if len(changes) == 0 { // cpus, memory, pids_limit : à chaud, sans recréer
		if err := e.UpdateContainerHot(p.Category, p.Project, co); err != nil {
			return refusal("", "invalid", err.Error()), nil
		}
		u.invalidate()
		return map[string]any{"ok": true, "applied": "hot"}, nil
	}
	args, aerr := e.RecreateArgs(p.Category, p.Project, p.Image, co)
	if aerr != nil {
		return refusal("", "invalid", aerr.Error()), nil
	}
	if a.Confirm == "" {
		sort.Slice(changes, func(i, j int) bool { return changes[i]["key"] < changes[j]["key"] })
		return map[string]any{"ok": false, "recreate": map[string]any{"token": e.RecreateToken(e.ObjectName(p.Category, p.Project), args),
			"lost": []string{"les paquets et fichiers installés dans le conteneur hors du volume"}, "kept": []string{"le dossier du projet", ".claude", "l'état et l'approbation sous .claude-launcher/"},
			"sessionOpen": p.Running, "changes": changes}}, nil
	}
	if err := e.RecreateContainer(e.ObjectName(p.Category, p.Project), args, a.Confirm, p.Running, time.Now()); err != nil {
		return refusal("", "invalid", err.Error()), nil
	}
	u.invalidate()
	u.pushState()
	return map[string]any{"ok": true, "applied": "recreated"}, nil
}

// gate : plafonds par utilisateur (40 exec, 4 flux /files).
var userGate = core.NewUserGate()

// serveFiles : /files/<catégorie>/<projet>/… ou /files/<projet>/… d'un projet de l'utilisateur. Décision d'après la DERNIÈRE découverte connue (jamais de
// commande vers le backend pour décider) : projet inconnu ou arrêté → 404 « projet arrêté » sans lecture ni démarrage.
func (u *userSpace) serveFiles(rw http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, core.FilesPrefix), "/")
	segs := strings.Split(rest, "/")
	for _, s := range segs {
		if s == ".." || strings.HasPrefix(s, "__") {
			http.NotFound(rw, r)
			return
		}
	}
	u.proj.mu.Lock()
	var hit *core.UserProject
	var sub string
	var bn string
	for b, d := range u.proj.disc {
		if d == nil || d.err != nil {
			continue
		}
		for i := range d.projects {
			p := &d.projects[i]
			switch {
			case len(segs) >= 2 && segs[0] == p.Category && segs[1] == p.Project:
				hit, sub, bn = p, strings.Join(segs[2:], "/"), b
			case hit == nil && segs[0] == p.Project:
				hit, sub, bn = p, strings.Join(segs[1:], "/"), b
			}
		}
	}
	var running bool
	if hit != nil {
		running = hit.Running
	}
	var cat, name string
	if hit != nil {
		cat, name = hit.Category, hit.Project
	}
	u.proj.mu.Unlock()
	if hit == nil || !running {
		http.Error(rw, "projet arrêté", http.StatusNotFound)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/") && sub != "" {
		sub += "/"
	}
	es, _, _ := u.engines()
	e, ok := es[bn]
	if !ok {
		http.NotFound(rw, r)
		return
	}
	e.ServeUserFile(rw, r, cat, name, sub, userGate)
}

// projectMove : renomme / déplace un projet du MÊME backend (dossier déplacé dans le conteneur, objet recréé aux nouveaux labels).
func (u *userSpace) projectMove(a userProjArgs) (any, error) {
	bn, cat, name, err := core.ParseUserProjectID(a.Path)
	if err != nil {
		return refusal("path", "invalid", err.Error()), nil
	}
	if _, _, ok := u.owned(a.Path); !ok {
		return refusal("path", "invalid", "projet introuvable"), nil
	}
	es, _, _ := u.engines()
	e, ok := es[bn]
	if !ok {
		return refusal("backend", "invalid", "backend inconnu"), nil
	}
	nc, nn := a.Category, a.Name
	if nc == "" {
		nc = cat
	}
	if nn == "" {
		nn = name
	}
	if msg := core.ValidUserProjectName(nc); msg != "" {
		return refusal("category", "invalid", msg), nil
	}
	if msg := core.ValidUserProjectName(nn); msg != "" {
		return refusal("name", "invalid", msg), nil
	}
	if err := u.sp.MoveProject(e, cat, name, nc, nn); err != nil {
		return refusal("", "invalid", err.Error()), nil
	}
	u.invalidate()
	u.pushState()
	return map[string]any{"ok": true, "path": core.UserProjectID(bn, nc, nn)}, nil
}

// projectDelete : supprime le dossier du projet (dans le conteneur) puis l'objet ; jamais le volume ni le PVC.
func (u *userSpace) projectDelete(a userProjArgs) (any, error) {
	bn, cat, name, err := core.ParseUserProjectID(a.Path)
	if err != nil {
		return refusal("path", "invalid", err.Error()), nil
	}
	if _, _, ok := u.owned(a.Path); !ok {
		return refusal("path", "invalid", "projet introuvable"), nil
	}
	es, _, _ := u.engines()
	e, ok := es[bn]
	if !ok {
		return refusal("backend", "invalid", "backend inconnu"), nil
	}
	if err := u.sp.DeleteProject(e, cat, name); err != nil {
		return refusal("", "invalid", err.Error()), nil
	}
	_, _ = u.sp.PurgeProjectTrace(bn, cat, name)
	u.invalidate()
	u.pushState()
	return map[string]any{"ok": true}, nil
}

// owned : le projet figure dans la découverte (objets cl.user=<uid>) de CE utilisateur — relue si le cache ne le connaît pas. Contrôle d'appartenance
// commun à tout point d'entrée par chemin, AVANT toute commande de création, de démarrage, d'arrêt ou de lecture : jamais de création par un chemin
// qui n'est pas « create » (noms, motif réservé, limite et options y sont validés).
func (u *userSpace) owned(id string) (core.Engine, core.UserProject, bool) {
	e, p, _, found, err := u.findProject(id)
	if err == nil && !found {
		u.invalidate()
		e, p, _, found, err = u.findProject(id)
	}
	return e, p, err == nil && found
}
