package core

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"claude-launcher-ng2/platform"
)

// API des projets d'un utilisateur (LOT-3.2 tâches 13–16) : découverte par labels, création / clone / déplacement / suppression dans le conteneur,
// plan de démarrage (P20), plafonds par utilisateur (D5). Le git du launcher n'est JAMAIS lancé : tout passe par le conteneur.

// ObjMeta : un objet (StatefulSet, conteneur) vu par ses noms et labels.
type ObjMeta struct {
	Name        string
	Labels      map[string]string
	Annotations map[string]string
	Running     bool
}

// OwnedProjects : objets cl.launcher=ng2 ET cl.user=<uid> exactement, hors sauvegardes (cl.state=old, cl.recreate-of).
func OwnedProjects(uid string, objs []ObjMeta) []ObjMeta {
	var out []ObjMeta
	if uid == "" {
		return out
	}
	for _, o := range objs {
		l := o.Labels
		if l["cl.launcher"] != "ng2" || l["cl.user"] != uid || l["cl.state"] == "old" || l["cl.recreate-of"] != "" {
			continue
		}
		out = append(out, o)
	}
	return out
}

// listObjects : objets du backend portant cl.launcher=ng2 (docker/podman : « ps -a » ; k8s : StatefulSets).
func (e Engine) listObjects() ([]ObjMeta, error) {
	if e.B.K8s() {
		out, err := e.kcmd("get", "statefulsets", "-l", "cl.launcher=ng2", "-o", "json").Output()
		if err != nil {
			return nil, errors.New("lecture des StatefulSets impossible")
		}
		var l struct {
			Items []struct {
				Metadata struct {
					Name   string            `json:"name"`
					Labels map[string]string `json:"labels"`
				} `json:"metadata"`
			} `json:"items"`
		}
		if json.Unmarshal(out, &l) != nil {
			return nil, errors.New("réponse illisible")
		}
		var objs []ObjMeta
		for _, it := range l.Items {
			objs = append(objs, ObjMeta{Name: it.Metadata.Name, Labels: it.Metadata.Labels})
		}
		return objs, nil
	}
	out, err := e.cmd("ps", "-a", "--filter", "label=cl.launcher=ng2", "--format", "{{.Names}} {{.Labels}}").Output()
	if err != nil {
		return nil, errors.New("lecture des conteneurs impossible")
	}
	var objs []ObjMeta
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.FieldsFunc(line, func(r rune) bool { return r == ' ' || r == ',' || r == '\t' })
		if len(f) == 0 {
			continue
		}
		o := ObjMeta{Name: f[0], Labels: map[string]string{}}
		for _, kv := range f[1:] {
			if k, v, ok := strings.Cut(kv, "="); ok {
				o.Labels[k] = v
			}
		}
		objs = append(objs, o)
	}
	return objs, nil
}

// CreateProject : crée l'objet labellisé du projet (limite, noms et dépôt validés AVANT toute commande) ; avec un dépôt : démarre le conteneur puis
// clone DANS le conteneur, sous l'uid du traitement (arguments positionnels après « -- »).
func (sp Space) CreateProject(e Engine, cat, name, repo string) error {
	if msg := ValidUserProjectName(cat); msg != "" {
		return errors.New("catégorie : " + msg)
	}
	if msg := ValidUserProjectName(name); msg != "" {
		return errors.New("nom : " + msg)
	}
	if repo != "" {
		if _, err := ValidateCloneURL(repo); err != nil {
			return fmt.Errorf("dépôt : %w", err)
		}
	}
	objs, _ := e.listObjects() // injoignable : la création échouera d'elle-même
	owned := OwnedProjects(e.B.uid, objs)
	if p := CheckProjectLimit(len(owned)); p != nil {
		return errors.New(p.Msg)
	}
	obj := e.ObjectName(cat, name)
	for _, o := range objs {
		if o.Name == obj {
			return errors.New("un projet porte déjà ce nom")
		}
	}
	if err := e.CreateUserProject(cat, name, UserDefaultImage()); err != nil {
		return err
	}
	if repo == "" {
		return nil
	}
	if err := e.startRaw(cat, name); err != nil {
		return err
	}
	return e.runIn(obj, "git", "clone", "--", repo, UserProjectDir(cat, name))
}

// runIn : commande dans le conteneur du projet, sous l'uid du traitement.
func (e Engine) runIn(obj string, argv ...string) error {
	a := e.ExecArgv(obj, false, false, argv...)
	out, err := platform.NoWindow(exec.Command(a[0], a[1:]...)).CombinedOutput()
	if err != nil {
		return errors.New("commande dans le conteneur impossible : " + ScrubMessage(firstLine(string(out), err)))
	}
	return nil
}

// DeleteProject : supprime le dossier du projet (dans le conteneur) puis l'objet ; refuse un objet d'un autre uid. Jamais le volume ni le PVC.
func (sp Space) DeleteProject(e Engine, cat, name string) error {
	obj := e.ObjectName(cat, name)
	if err := e.ownerOf(obj); err != nil {
		return err
	}
	if err := e.StartUserProject(cat, name); err != nil {
		return err
	}
	if err := e.runIn(obj, "rm", "-rf", "--", UserProjectDir(cat, name)); err != nil {
		return err
	}
	return e.removeObject(obj)
}

func (e Engine) ownerOf(obj string) error {
	if e.B.K8s() {
		return e.VerifyOwnerK8s(obj)
	}
	return e.VerifyOwner(e.B.uid, obj)
}

func (e Engine) removeObject(obj string) error {
	if e.B.K8s() {
		return e.kcmd("delete", "statefulset", obj, "--wait=true").Run()
	}
	_ = e.cmd("stop", obj).Run()
	return e.cmd("rm", "-f", obj).Run()
}

// MoveProject : déplace/renomme un projet DANS LE MÊME backend : dossier déplacé dans le conteneur (mv --), puis objet recréé aux nouveaux labels.
func (sp Space) MoveProject(e Engine, cat, name, newCat, newName string) error {
	if ValidUserProjectName(newCat) != "" || ValidUserProjectName(newName) != "" {
		return errors.New("nouveau nom invalide")
	}
	obj := e.ObjectName(cat, name)
	if err := e.ownerOf(obj); err != nil {
		return err
	}
	if cat == newCat && name == newName {
		return errors.New("même emplacement")
	}
	if objs, _ := e.listObjects(); true {
		for _, o := range objs {
			if o.Name == e.ObjectName(newCat, newName) {
				return errors.New("un projet porte déjà ce nom")
			}
		}
	}
	if err := e.StartUserProject(cat, name); err != nil {
		return err
	}
	if err := e.runIn(obj, "mkdir", "-p", "--", "/space/PROJETS/"+newCat); err != nil {
		return err
	}
	if err := e.runIn(obj, "mv", "--", UserProjectDir(cat, name), UserProjectDir(newCat, newName)); err != nil {
		return err
	}
	if err := e.removeObject(obj); err != nil {
		return err
	}
	return e.CreateUserProject(newCat, newName, UserDefaultImage())
}

// StartPlan : ce qu'un déclencheur doit faire d'un projet (P20).
type StartPlan struct{ Start, Session, Reread bool }

// PlanStart : « open » démarre et ouvre une session ; « f5 » démarre SANS session puis relit l'état ; « probe » (sondage de 10 s) ne démarre JAMAIS ;
// tout autre déclencheur : rien (échec fermé).
func PlanStart(trigger string, stoppedByUser bool) StartPlan {
	switch trigger {
	case "open":
		return StartPlan{Start: true, Session: true}
	case "f5":
		return StartPlan{Start: true, Reread: true}
	}
	return StartPlan{}
}

// ── plafonds par utilisateur (D5) ─────────────────────────────────────────

var (
	ErrGateExec    = errors.New("trop de processus d'exec simultanés (40 au plus)")
	ErrGateStreams = errors.New("trop de flux de fichiers simultanés (4 au plus)")
)

const (
	gateExecMax   = 40
	gateStreamMax = 4
)

// UserGate : compteurs d'exec et de flux /files par utilisateur.
type UserGate struct {
	mu      sync.Mutex
	execs   map[string]int
	streams map[string]int
}

func NewUserGate() *UserGate { return &UserGate{execs: map[string]int{}, streams: map[string]int{}} }

func (g *UserGate) acquire(m map[string]int, max int, uid string, errv error) (func(), error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if m[uid] >= max {
		return nil, errv
	}
	m[uid]++
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			if m[uid] > 0 {
				m[uid]--
			}
			g.mu.Unlock()
		})
	}, nil
}

// AcquireExec : un processus d'exec de plus pour l'utilisateur (libération idempotente).
func (g *UserGate) AcquireExec(uid string) (func(), error) {
	return g.acquire(g.execs, gateExecMax, uid, ErrGateExec)
}

// AcquireStream : un flux /files de plus pour l'utilisateur.
func (g *UserGate) AcquireStream(uid string) (func(), error) {
	return g.acquire(g.streams, gateStreamMax, uid, ErrGateStreams)
}

// RefreshUserMeta : relit l'état git du projet EN MARCHE (commandes git exécutées dans le conteneur, jamais le git du launcher) et le range sur le
// backend : annotation cl.meta (k8s) ou .claude-launcher/state.json dans le volume (docker/podman). Un projet sans dépôt : état vide valide.
func (e Engine) RefreshUserMeta(cat, name string, now time.Time) error {
	obj, dir := e.ObjectName(cat, name), UserProjectDir(cat, name)
	run := func(argv ...string) string {
		a := e.ExecArgv(obj, false, false, append([]string{"git", "-C", dir}, argv...)...)
		out, _ := platform.NoWindow(exec.Command(a[0], a[1:]...)).Output()
		return strings.TrimSpace(string(out))
	}
	m := ProjectMeta{Seen: now.UTC(), Team: []string{}}
	m.Branch, m.Head = run("rev-parse", "--abbrev-ref", "HEAD"), run("rev-parse", "--short", "HEAD")
	m.Dirty = run("status", "--porcelain") != ""
	if badMetaStr(m.Branch, 128) {
		m.Branch = ""
	}
	if badMetaStr(m.Head, 64) {
		m.Head = ""
	}
	if e.B.K8s() {
		return e.WriteUserMeta(obj, m)
	}
	b, err := m.JSON()
	if err != nil {
		return err
	}
	if err := e.ownerOf(obj); err != nil {
		return err
	}
	if err := e.runIn(obj, "mkdir", "-p", "--", dir+"/.claude-launcher"); err != nil {
		return err
	}
	a := e.ExecArgv(obj, false, false, "tee", "--", dir+"/.claude-launcher/state.json")
	c := platform.NoWindow(exec.Command(a[0], a[1:]...))
	c.Stdin = bytes.NewReader(b)
	return c.Run()
}

// ReadStoppedState : état (state.json) d'un projet docker/podman ARRÊTÉ, lu par « cp » (archive tar sur la sortie, 4 Kio au plus, une seule entrée) ;
// jamais exécuté ni démarré. k8s : l'état est dans l'annotation cl.meta (découverte).
func (e Engine) ReadStoppedState(cat, name string) (*ProjectMeta, error) {
	if e.B.K8s() {
		return nil, errors.New("k8s : annotation cl.meta")
	}
	obj := e.ObjectName(cat, name)
	if err := e.VerifyOwner(e.B.uid, obj); err != nil {
		return nil, err
	}
	out, err := platform.NoWindow(e.cmd("cp", obj+":"+UserProjectDir(cat, name)+"/.claude-launcher/state.json", "-")).Output()
	if err != nil || len(out) > 64<<10 {
		return nil, errors.New("état absent")
	}
	tr := tar.NewReader(bytes.NewReader(out))
	h, err := tr.Next()
	if err != nil || h.Typeflag != tar.TypeReg || h.Size > maxUserMeta {
		return nil, errors.New("état illisible")
	}
	raw, err := io.ReadAll(io.LimitReader(tr, maxUserMeta+1))
	if err != nil {
		return nil, errors.New("état illisible")
	}
	m, err := ParseProjectState(raw)
	if err != nil {
		return nil, err
	}
	return &m, nil
}
