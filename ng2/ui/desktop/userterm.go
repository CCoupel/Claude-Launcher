package desktop

import (
	"bufio"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"claude-launcher-ng2/core"
)

// Terminal d'un projet d'UTILISATEUR (LOT-3.2 jalon 4) : le MÊME projTerm que celui de l'admin (tmux -C par exec, panes, saisie, redimensionnement) ;
// seul son hôte change — celui de l'instance de l'utilisateur : onglets, évènements et messages propres à son identité, aucune configuration ni
// fichier de l'administrateur, plafond de 40 exec par utilisateur.

// userOpen : ce qu'un terminal d'utilisateur sait de son projet.
type userOpen struct {
	u       *userSpace
	e       core.Engine
	backend string
	cat, nm string
	key     []byte
	release func() // plafond d'exec : libéré à la fermeture de l'onglet
	relOnce sync.Once
	gateUID string
}

func (o *userOpen) releaseGate() {
	o.relOnce.Do(func() {
		if o.release != nil {
			o.release()
		}
	})
}

// userTabs : onglets ouverts de l'instance.
type userTabs struct {
	mu    sync.Mutex
	terms map[string]*projTerm
	order []string
	msg   string
	push  atomic.Bool // un état est déjà programmé
	shut  atomic.Bool // instance libérée
}

// userHost : hôte des terminaux d'une instance utilisateur (termHost).
type userHost struct{ u *userSpace }

func (h userHost) Emit(name string, data any) {
	if h.u.emit != nil {
		h.u.emit(name, data)
	}
}

// changed : état renvoyé à l'interface, regroupé (au plus un envoi par 100 ms).
func (h userHost) changed() {
	if h.u.tabs.push.CompareAndSwap(false, true) {
		time.AfterFunc(100*time.Millisecond, func() {
			h.u.tabs.push.Store(false)
			h.u.pushState()
		})
	}
}
func (h userHost) proposeInit(string)                             {}
func (h userHost) endOpen()                                       {}
func (h userHost) confNow() core.Config                           { return core.Config{} } // jamais la configuration de l'administrateur
func (h userHost) conf() core.Config                              { return core.Config{} }
func (h userHost) confRefresh(bool)                               {}
func (h userHost) stopped() bool                                  { return h.u.tabs.shut.Load() }
func (h userHost) readSessions()                                  {}
func (h userHost) pushTeam(string)                                {}
func (h userHost) trace(string, string, time.Duration, ...string) {}
func (h userHost) setMsg(m string) {
	h.u.tabs.mu.Lock()
	h.u.tabs.msg = m
	h.u.tabs.mu.Unlock()
	h.changed()
}
func (h userHost) setMsgSoon(m string) { go h.setMsg(m) }
func (h userHost) clearMsgIf(m string) {
	h.u.tabs.mu.Lock()
	if h.u.tabs.msg == m {
		h.u.tabs.msg = ""
	}
	h.u.tabs.mu.Unlock()
}
func (h userHost) readCtr() { h.u.invalidate(); h.changed() }
func (h userHost) removeTab(path string, t *projTerm) {
	ts := &h.u.tabs
	ts.mu.Lock()
	if cur := ts.terms[path]; cur == nil || t != nil && cur != t {
		ts.mu.Unlock()
		return
	}
	delete(ts.terms, path)
	for i, p := range ts.order {
		if p == path {
			ts.order = append(ts.order[:i], ts.order[i+1:]...)
			break
		}
	}
	ts.mu.Unlock()
	if cur := cur0(t); cur != nil {
		cur.releaseGate()
	}
	h.changed()
}

func cur0(t *projTerm) *userOpen {
	if t == nil {
		return nil
	}
	return t.user
}

func (u *userSpace) term(path string) *projTerm {
	u.tabs.mu.Lock()
	defer u.tabs.mu.Unlock()
	return u.tabs.terms[path]
}

func (u *userSpace) tabInfos() []TabInfo {
	u.tabs.mu.Lock()
	var ts []*projTerm
	for _, p := range u.tabs.order {
		if t := u.tabs.terms[p]; t != nil {
			ts = append(ts, t)
		}
	}
	u.tabs.mu.Unlock()
	out := make([]TabInfo, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.info())
	}
	return out
}

// closeTerms : détache tous les onglets (instance libérée ou arrêt) ; les sessions continuent dans les conteneurs.
func (u *userSpace) closeTerms() {
	u.tabs.shut.Store(true)
	u.tabs.mu.Lock()
	var ts []*projTerm
	for _, t := range u.tabs.terms {
		ts = append(ts, t)
	}
	u.tabs.mu.Unlock()
	for _, t := range ts {
		t.detach()
	}
}

// prepareUser : ouverture du projet d'un utilisateur dans son terminal (équivalent de prepare, sans rien de l'administrateur).
func (t *projTerm) prepareUser() error {
	uo := t.user
	op, err := uo.e.OpenUserProject(uo.key, uo.cat, uo.nm, core.CtrOpenIO{
		Out:   t,
		In:    bufio.NewReader(&lineReader{t: t}),
		Start: func() []byte { return []byte(core.UserStartCommand(uo.cat, uo.nm)) },
		Size:  t.size,
		Owner: func(o core.OwnerInfo) string { return core.OwnerChoiceIO(o, t, bufio.NewReader(&lineReader{t: t})) },
	})
	var na core.ErrUserEnvNotApproved
	if errors.As(err, &na) { // approbation demandée dans l'interface ; rien de plus n'est lancé
		if files, ferr := uo.e.UserEnvFiles(uo.cat, uo.nm); ferr == nil {
			t.hs().Emit("envTrust", core.UserEnvTrustInfo(t.path, files, na.Changed))
		}
		t.setState("closed", "environnement à approuver")
		t.hs().removeTab(t.path, t)
		return nil
	}
	if err != nil {
		return err
	}
	if op.Canceled {
		t.setState("closed", "ouverture annulée")
		t.hs().removeTab(t.path, t)
		return nil
	}
	t.mu.Lock()
	t.e, t.ctr, t.readOnly = op.E, op.Name, op.ReadOnly
	if op.ReadOnly {
		t.owner = op.E.Owner(op.Name).Id
	}
	t.sess, t.win = core.CtrSession, "="+core.CtrSession+":"
	t.mu.Unlock()
	return nil
}

// openUserProject : ouvre (ou retrouve) l'onglet d'un projet de l'utilisateur.
func (u *userSpace) openUserProject(id string) (any, error) {
	bn, cat, name, err := core.ParseUserProjectID(id)
	if err != nil {
		return refusal("path", "invalid", err.Error()), nil
	}
	es, _, _ := u.engines()
	e, ok := es[bn]
	if !ok {
		return refusal("backend", "invalid", "backend inconnu dans vos réglages"), nil
	}
	if t := u.term(id); t != nil {
		if !t.isReadOnly() && t.info().State != "error" {
			return map[string]any{"tab": id}, nil
		}
		t.detach()
	}
	key, err := u.sp.ApprovalKey()
	if err != nil {
		return refusal("", "invalid", "clé d'approbation indisponible"), nil
	}
	rel, gerr := userGate.AcquireExec(u.sp.UID)
	if gerr != nil {
		return map[string]any{"ok": false, "code": "rate", "status": 429, "msg": gerr.Error()}, nil
	}
	uo := &userOpen{u: u, e: e, backend: bn, cat: cat, nm: name, key: key, release: rel}
	t := &projTerm{h: userHost{u}, user: uo, path: id, key: name, name: name, color: "75", mode: core.LaunchNormal,
		lines: make(chan string, 16), state: "opening", seen: map[string]bool{}, seeding: map[string]bool{},
		carry: map[string][]byte{}, flushCh: make(chan struct{}, 1), stop: make(chan struct{})}
	u.tabs.mu.Lock()
	if u.tabs.terms == nil {
		u.tabs.terms = map[string]*projTerm{}
	}
	if u.tabs.terms[id] != nil {
		u.tabs.mu.Unlock()
		uo.releaseGate()
		return map[string]any{"tab": id}, nil
	}
	u.tabs.terms[id] = t
	u.tabs.order = append(u.tabs.order, id)
	u.tabs.shut.Store(false)
	u.tabs.mu.Unlock()
	userHost{u}.changed()
	go func() {
		t.run()
		u.invalidate()
	}()
	return map[string]any{"tab": id}, nil
}

// closeUserProject : Fermer / Forcer fermer — l'onglet est détaché, la session terminée et le conteneur ARRÊTÉ ; le sondage ne le relance jamais (seul Ouvrir ou F5 démarre).
func (u *userSpace) closeUserProject(id string) (any, error) {
	bn, cat, name, err := core.ParseUserProjectID(id)
	if err != nil {
		return refusal("path", "invalid", err.Error()), nil
	}
	if t := u.term(id); t != nil {
		t.detach()
		userHost{u}.removeTab(id, t)
	}
	es, _, _ := u.engines()
	e, ok := es[bn]
	if !ok {
		return refusal("backend", "invalid", "backend inconnu"), nil
	}
	if err := e.StopUserProject(cat, name); err != nil {
		return refusal("", "invalid", "arrêt impossible : "+err.Error()), nil
	}
	u.invalidate()
	u.pushState()
	return map[string]any{"ok": true}, nil
}

// envTrustAnswer : réponse à la question d'approbation (accept | cancel) ; « ignore » n'existe pas pour un projet d'utilisateur.
func (u *userSpace) envTrustAnswer(a userProjArgs) (any, error) {
	bn, cat, name, err := core.ParseUserProjectID(a.Path)
	if err != nil {
		return refusal("path", "invalid", err.Error()), nil
	}
	switch a.Choice {
	case "cancel", "ignore":
		return map[string]any{"ok": true}, nil
	case "accept":
	default:
		return map[string]any{"ok": false, "msg": "choix inconnu : " + a.Choice}, nil
	}
	es, _, _ := u.engines()
	e, ok := es[bn]
	if !ok {
		return refusal("backend", "invalid", "backend inconnu"), nil
	}
	files, ferr := e.UserEnvFiles(cat, name)
	if ferr != nil {
		return map[string]any{"ok": false, "msg": ferr.Error()}, nil
	}
	if core.UserEnvSHA(files) != a.SHA {
		return map[string]any{"ok": false, "msg": "l'environnement a encore changé", "retry": true}, nil
	}
	key, kerr := u.sp.ApprovalKey()
	if kerr != nil {
		return map[string]any{"ok": false, "msg": "clé d'approbation indisponible"}, nil
	}
	if err := e.ApproveUserEnv(key, cat, name, files); err != nil {
		return map[string]any{"ok": false, "msg": core.ScrubMessage(err.Error())}, nil
	}
	return u.openUserProject(a.Path)
}

// termCall : routes term* de l'instance — mêmes effets que l'App commune, sur les onglets de l'utilisateur.
func (u *userSpace) termCall(method string, a userProjArgs) (any, error) {
	t := u.term(a.Path)
	switch method {
	case "termInput":
		if t != nil {
			if a.Binary {
				a.Data = binaryBytes(a.Data)
			}
			if why := t.input(a.Pane, a.Data); why != "" {
				msg := "saisie refusée"
				if why == "readonly" {
					msg = "lecture seule : un autre launcher a la main sur " + t.name
				} else {
					msg = fmt.Sprintf("collage trop volumineux (%d Mo au plus) : rien n'a été envoyé", maxPaste>>20)
				}
				return map[string]any{"ok": false, "reason": why, "msg": msg}, nil
			}
		}
	case "termResize":
		if t != nil {
			t.resize(a.Cols, a.Rows)
		}
	case "termFocus":
		if t != nil {
			t.focus(a.Pane)
		}
	case "termZoom":
		if t != nil {
			t.zoom(a.Pane)
		}
	case "resizePane":
		if t != nil {
			t.resizePane(a.Pane, a.Kind, a.Cols)
		}
	case "launchInput":
		if t != nil {
			t.launchInput(a.Data)
		}
	case "termSync":
		if t != nil {
			go t.resync()
		}
	}
	return nil, nil
}
