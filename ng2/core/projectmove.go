package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"claude-launcher-ng2/platform"
)

// ── Déplacer / renommer un projet (#216, contracts/desktop-dispatch.md « projectMove ») ──
//
// Étapes, dans l'ordre, consignées dans le journal moves.json (dossier de config, sous verrou) :
//   dir → assign → conf → history → trust → ctr.
// Échec à l'étape N : les étapes déjà faites sont annulées dans l'ordre inverse (« ctr », non réversible, est la dernière).
// Un retour arrière en échec laisse l'entrée du journal (état « partial ») : le projet porte le blocker « déplacement
// inachevé » et l'interface propose « Reprendre » (rejoue depuis l'étape en échec) ou « Annuler » (retour arrière).

// Étapes d'un déplacement.
const (
	MoveStepDir     = "dir"
	MoveStepAssign  = "assign"
	MoveStepConf    = "conf"
	MoveStepHistory = "history"
	MoveStepTrust   = "trust"
	MoveStepCtr     = "ctr"
)

var moveStepOrder = []string{MoveStepDir, MoveStepAssign, MoveStepConf, MoveStepHistory, MoveStepTrust, MoveStepCtr}

// MoveTarget : destination demandée (mêmes règles que la création, #214).
type MoveTarget struct {
	Base        string `json:"base"`
	Category    string `json:"category"`
	NewCategory bool   `json:"newCategory"`
	Name        string `json:"name"`
}

// MoveImpact : une ligne du tableau « ce qui change » du dialogue (étapes qui changent quelque chose seulement).
type MoveImpact struct {
	What  string   `json:"what"` // dir, ctr, assign, conf, history, trust
	From  string   `json:"from,omitempty"`
	To    string   `json:"to,omitempty"`
	Value string   `json:"value,omitempty"`
	Keys  []string `json:"keys,omitempty"`
	Label string   `json:"label,omitempty"` // texte affichable tel quel (trust ; conf : clé partagée avec d'autres projets)
}

// MoveDest : destination envisagée (projectMoveInfo avec destination), sans rien écrire.
type MoveDest struct {
	OK          bool         `json:"ok"`
	Path        string       `json:"path,omitempty"`
	Key         string       `json:"key,omitempty"` // clé recalculée pour la destination
	KeyChanged  bool         `json:"keyChanged"`
	CrossDevice bool         `json:"crossDevice"` // autre système de fichiers : copie puis suppression
	Exists      bool         `json:"exists"`
	Field       string       `json:"field"`
	Msg         string       `json:"msg"`
	Impacts     []MoveImpact `json:"impacts"`
}

// MovePending : entrée du journal telle que l'interface la voit.
type MovePending struct {
	ID      string   `json:"id"`
	From    string   `json:"from"`
	To      string   `json:"to"`
	FromKey string   `json:"fromKey"`
	ToKey   string   `json:"toKey"`
	Step    string   `json:"step"` // première étape non faite : celle d'où « Reprendre » rejoue
	Done    []string `json:"done"`
	Msg     string   `json:"msg"`
	At      string   `json:"at"`
}

func pendingOf(e MoveEntry) *MovePending {
	step := "" // première étape non faite : « Reprendre » rejoue à partir d'elle
	for _, s := range moveStepOrder {
		if !has(e.Done, s) {
			step = s
			break
		}
	}
	done := e.Done
	if done == nil {
		done = []string{}
	}
	return &MovePending{ID: e.ID, From: e.From, To: e.To, FromKey: CtrName(e.From), ToKey: CtrName(e.To), Step: step, Done: done, Msg: e.Msg, At: e.At}
}

// MoveInfo : état d'un projet pour le dialogue (projectMoveInfo).
type MoveInfo struct {
	Base     string       `json:"base"`
	Category string       `json:"category"`
	Name     string       `json:"name"`
	Key      string       `json:"key"`
	Open     bool         `json:"open"`
	Blockers []string     `json:"blockers"`
	Dest     *MoveDest    `json:"dest,omitempty"`    // seulement si une destination est donnée
	Pending  *MovePending `json:"pending,omitempty"` // déplacement inachevé dont from ou to est path
}

// MoveResult : résultat de projectMove.
type MoveResult struct {
	OK         bool     `json:"ok"`
	Path       string   `json:"path,omitempty"`
	Key        string   `json:"key,omitempty"`
	Step       string   `json:"step,omitempty"`
	Msg        string   `json:"msg,omitempty"`
	RolledBack bool     `json:"rolledBack"`
	State      string   `json:"state,omitempty"` // « partial » : retour arrière en échec
	Done       []string `json:"done,omitempty"`
}

// MoveEntry : une ligne du journal moves.json.
type MoveEntry struct {
	ID      string            `json:"id"`
	From    string            `json:"from"`
	To      string            `json:"to"`
	Backend string            `json:"backend,omitempty"` // backend du projet AVANT le déplacement (étape ctr)
	Done    []string          `json:"done"`
	Failed  string            `json:"failed,omitempty"`
	State   string            `json:"state"` // running | partial
	Detail  map[string]string `json:"detail,omitempty"`
	Msg     string            `json:"msg,omitempty"`
	At      string            `json:"at"`
}

// MoveEnv : dépendances et points d'injection (tests : échec injecté à chaque étape, EXDEV simulé, faux conteneurs).
type MoveEnv struct {
	Roots     []string
	OpenHere  func(path string) bool             // projet ouvert dans ce launcher (fenêtre tmux, onglet) ; nil : non
	Foreign   func(path string) string           // propriétaire d'un autre launcher ; nil : ForeignOwner
	Running   func(path string) string           // conteneur / pod en cours d'exécution ; nil : listing des backends
	Rename    func(src, dst string) error        // nil : os.Rename
	RemoveCtr func(path, backend string) error   // nil : suppression du conteneur / StatefulSet
	Fail      func(step string, undo bool) error // échec injecté (tests) ; nil : aucun
	ClaudeDir string                             // dossier de config de claude ; vide : ClaudeDir()
}

func (e MoveEnv) rename(a, b string) error {
	if e.Rename != nil {
		return e.Rename(a, b)
	}
	return platform.ReplaceFile(a, b) // Windows : retry court (fichier tenu par un antivirus ou un indexeur)
}

// removeAllRetry : os.RemoveAll retenté brièvement (Windows : fichier verrouillé passagèrement).
func removeAllRetry(p string) error {
	var err error
	for i, d := 0, 50*time.Millisecond; i < 5; i, d = i+1, d*2 {
		if err = os.RemoveAll(p); err == nil {
			return nil
		}
		time.Sleep(d)
	}
	return err
}

func (e MoveEnv) fail(step string, undo bool) error {
	if e.Fail != nil {
		return e.Fail(step, undo)
	}
	return nil
}

func (e MoveEnv) claudeDir() string {
	if e.ClaudeDir != "" {
		return e.ClaudeDir
	}
	return ClaudeDir()
}

// ── Journal ───────────────────────────────────────────────────────────────

func movesPath() string { return filepath.Join(Ng2ConfigDir(), "moves.json") }

// movesBrokenError : moves.json présent mais illisible (ou verrou du journal inaccessible) — fail-closed, comme trust.json :
// tout déplacement est refusé, le journal n'est JAMAIS réécrit.
type movesBrokenError struct{ cause string }

func (e movesBrokenError) Error() string {
	return fmt.Sprintf("journal des déplacements illisible (%s%s) : restaurez moves.json.bak ou supprimez moves.json", movesPath(), e.cause)
}

// readMovesLocked : journal lu (verrou tenu) ; illisible : movesBrokenError.
func readMovesLocked() ([]MoveEntry, []byte, error) {
	raw, err := os.ReadFile(movesPath())
	if os.IsNotExist(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, movesBrokenError{" : " + err.Error()}
	}
	var out []MoveEntry
	if len(strings.TrimSpace(string(raw))) == 0 || json.Unmarshal(raw, &out) != nil {
		return nil, nil, movesBrokenError{}
	}
	return out, raw, nil
}

func writeMovesLocked(m []MoveEntry, prev []byte) error {
	if prev != nil {
		if err := WriteFileAtomic(movesPath()+".bak", prev, 0o600); err != nil {
			return err
		}
	}
	if len(m) == 0 {
		if err := os.Remove(movesPath()); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	return WriteFileAtomic(movesPath(), b, 0o600)
}

// editMoves : lire-modifier-écrire du journal sous verrou ; un journal illisible n'est jamais réécrit.
func editMoves(fn func(m []MoveEntry) []MoveEntry) error {
	return WithFileLock(movesPath(), func() error {
		m, prev, err := readMovesLocked()
		if err != nil {
			return err
		}
		return writeMovesLocked(fn(m), prev)
	})
}

func saveEntry(e *MoveEntry) error {
	return editMoves(func(m []MoveEntry) []MoveEntry {
		for i := range m {
			if m[i].ID == e.ID {
				m[i] = *e
				return m
			}
		}
		return append(m, *e)
	})
}

func dropEntry(id string) error {
	return editMoves(func(m []MoveEntry) []MoveEntry {
		out := m[:0]
		for _, x := range m {
			if x.ID != id {
				out = append(out, x)
			}
		}
		return out
	})
}

// readMoves : journal complet ; erreur si illisible ou si le verrou du journal est inaccessible.
func readMoves() ([]MoveEntry, error) {
	var m []MoveEntry
	err := WithFileLock(movesPath(), func() error {
		var rerr error
		m, _, rerr = readMovesLocked()
		return rerr
	})
	if err != nil {
		if _, ok := err.(movesBrokenError); ok {
			return nil, err
		}
		return nil, movesBrokenError{" : " + err.Error()}
	}
	return m, nil
}

// movesError : le journal est-il lisible ? nil si oui.
func movesError() error {
	_, err := readMoves()
	return err
}

// PendingMoves : déplacements inachevés (journal) qui concernent path (source ou destination).
func PendingMoves(path string) []MoveEntry {
	var out []MoveEntry
	m, _ := readMoves()
	for _, e := range m {
		if e.From == path || e.To == path {
			out = append(out, e)
		}
	}
	return out
}

func readMovesCached() []MoveEntry {
	m, _ := readMoves()
	return m
}

// ── Informations et blockers ──────────────────────────────────────────────

// SplitProjectPath : base (racine), catégorie (« A/B » relatif, vide = racine) et nom d'un chemin de projet sous roots.
func SplitProjectPath(roots []string, path string) (base, category, name string, ok bool) {
	p := filepath.Clean(path)
	for _, r := range roots {
		b := filepath.Clean(r)
		rel, err := filepath.Rel(b, p)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		rel = filepath.ToSlash(rel)
		if i := strings.LastIndex(rel, "/"); i >= 0 {
			return b, rel[:i], rel[i+1:], true
		}
		return b, "", rel, true
	}
	return "", "", "", false
}

// MoveBlockers : ce qui empêche de déplacer path (vide : déplaçable) — projet ouvert ici, ouvert par un autre
// launcher, conteneur ou pod en cours d'exécution, déplacement inachevé.
func MoveBlockers(env MoveEnv, path string) []string {
	out := []string{}
	if env.OpenHere != nil && env.OpenHere(path) {
		out = append(out, "projet ouvert dans ce launcher")
	}
	foreign := ForeignOwner
	if env.Foreign != nil {
		foreign = env.Foreign
	}
	if who := foreign(path); who != "" {
		out = append(out, "projet ouvert par un autre launcher ("+who+")")
	}
	run := runningCtr
	if env.Running != nil {
		run = env.Running
	}
	if what := run(path); what != "" {
		out = append(out, "conteneur ou pod en cours d'exécution ("+what+")")
	}
	if err := movesError(); err != nil {
		out = append(out, err.Error()) // journal illisible : aucun déplacement (fail-closed)
	} else if len(PendingMoves(path)) > 0 {
		out = append(out, "déplacement inachevé")
	}
	return out
}

// runningCtr : nom du backend dont un conteneur / pod du projet tourne ; "" sinon.
func runningCtr(path string) string {
	if !HasEnv(path) {
		return ""
	}
	for _, b := range CtrBackends() {
		e, _ := EngineOf(b)
		fs, err := e.List()
		if err != nil {
			continue
		}
		for _, f := range fs {
			if f.Project == path && f.Running {
				return f.Name + " sur " + b.Name
			}
		}
	}
	return ""
}

// MoveInfoFor : projectMoveInfo sans destination.
func MoveInfoFor(env MoveEnv, path string) (MoveInfo, error) { return MoveInfoDest(env, path, nil) }

// MoveInfoDest : projectMoveInfo ; avec une destination, dest : clé recalculée, système de fichiers, existence, impacts —
// sans rien écrire (appelé à la frappe par le dialogue).
func MoveInfoDest(env MoveEnv, path string, target *MoveTarget) (MoveInfo, error) {
	base, cat, name, ok := SplitProjectPath(env.Roots, path)
	if !ok {
		return MoveInfo{}, errors.New("projet hors des bases configurées")
	}
	info := MoveInfo{Base: base, Category: cat, Name: name, Key: CtrName(path), Blockers: MoveBlockers(env, path)}
	info.Open = env.OpenHere != nil && env.OpenHere(path)
	if p := PendingMoves(path); len(p) > 0 {
		info.Pending = pendingOf(p[0])
	}
	if target != nil {
		d := moveDest(env, path, *target)
		info.Dest = &d
	}
	return info, nil
}

var ctrExistsCache = struct {
	sync.Mutex
	m map[string]ctrExistsEntry
}{m: map[string]ctrExistsEntry{}}

type ctrExistsEntry struct {
	at time.Time
	ok bool
}

// ctrExists : un conteneur / StatefulSet du projet existe (mémorisé 30 s : le dialogue interroge à la frappe).
func ctrExists(path string) bool {
	ctrExistsCache.Lock()
	if e, ok := ctrExistsCache.m[path]; ok && time.Since(e.at) < 30*time.Second {
		ctrExistsCache.Unlock()
		return e.ok
	}
	ctrExistsCache.Unlock()
	found := false
	if HasEnv(path) {
		if b := ProjectBackend(path); b.Name != "host" {
			if e, ok := EngineOf(b); ok {
				if fs, err := e.List(); err == nil {
					for _, f := range fs {
						found = found || f.Project == path
					}
				}
			}
		}
	}
	ctrExistsCache.Lock()
	ctrExistsCache.m[path] = ctrExistsEntry{time.Now(), found}
	ctrExistsCache.Unlock()
	return found
}

func moveDest(env MoveEnv, path string, t MoveTarget) MoveDest {
	d := MoveDest{Impacts: []MoveImpact{}}
	fail := func(field, msg string) MoveDest { d.Field, d.Msg = field, msg; return d }
	dest, err := ResolveProjectPathIn(env.Roots, t.Base, t.Category, t.Name)
	if err != nil {
		if pe, ok := AsPathError(err); ok {
			return fail(pe.Field, pe.Msg)
		}
		return fail("", err.Error())
	}
	d.Path, d.Key = dest, CtrName(dest)
	d.KeyChanged = d.Key != CtrName(path)
	if filepath.Clean(dest) == filepath.Clean(path) {
		return fail("name", "la destination est le projet lui-même")
	}
	if strings.HasPrefix(filepath.Clean(dest)+string(filepath.Separator), filepath.Clean(path)+string(filepath.Separator)) {
		return fail("category", "la destination est dans le projet")
	}
	if _, err := os.Lstat(dest); err == nil {
		d.Exists = true
		return fail("name", "la destination existe déjà")
	}
	if !t.NewCategory && t.Category != "" {
		if cat, cerr := ResolveCategoryPathIn(env.Roots, t.Base, t.Category); cerr == nil {
			if _, serr := os.Stat(cat); serr != nil {
				return fail("category", "catégorie inexistante")
			}
		}
	}
	d.OK = true
	d.CrossDevice = !sameDevice(path, dest)
	d.Impacts = append(d.Impacts, MoveImpact{What: MoveStepDir, From: path, To: dest})
	if ctrExists(path) {
		d.Impacts = append(d.Impacts, MoveImpact{What: MoveStepCtr, From: CtrName(path)})
	}
	if v, ok := ReadAssignments()[path]; ok {
		d.Impacts = append(d.Impacts, MoveImpact{What: MoveStepAssign, Value: v})
	}
	if oldName, newName := filepath.Base(path), filepath.Base(dest); oldName != newName {
		cfg := LoadConfig()
		var keys []string
		if cfg.ProjectColors[oldName] != "" {
			keys = append(keys, "PROJECT_COLORS")
		}
		if cfg.ProjectLayouts[oldName] != "" {
			keys = append(keys, "PROJECT_LAYOUTS")
		}
		if cfg.ProjectHideIdle[oldName] != "" {
			keys = append(keys, "PROJECT_HIDE_IDLE")
		}
		if len(keys) > 0 {
			im := MoveImpact{What: MoveStepConf, Keys: keys}
			// la clé est le NOM du dossier : deux projets de même nom dans des catégories différentes la partagent
			var shared []string
			for _, n := range ScanTree(env.Roots) {
				if n.Project && filepath.Base(n.Path) == oldName && filepath.Clean(n.Path) != filepath.Clean(path) {
					shared = append(shared, n.Path)
				}
			}
			if len(shared) > 0 {
				sort.Strings(shared)
				im.Label = "clé « " + oldName + " » partagée avec " + strings.Join(shared, ", ") + " : leur couleur / organisation sera retirée avec ce déplacement"
			}
			d.Impacts = append(d.Impacts, im)
		}
	}
	hroot := filepath.Join(env.claudeDir(), "projects")
	if _, err := os.Stat(filepath.Join(hroot, ProjectDirName(path))); err == nil {
		d.Impacts = append(d.Impacts, MoveImpact{What: MoveStepHistory, From: ProjectDirName(path), To: ProjectDirName(dest)})
	}
	if _, ok := EnvTrustEntry(path); ok { // approbation d'un dépôt cloné : suit le projet
		d.Impacts = append(d.Impacts, MoveImpact{What: MoveStepTrust, Label: "Approbation de l'environnement : conservée"})
	}
	return d
}

// ── Déplacement ───────────────────────────────────────────────────────────

func newMoveID() string { return newLockToken() }

// ── Exclusion : un verrou par projet (source ET destination), tenu de la vérification des blockers à la fin ──

func moveLockPath(path string) string {
	h := sha256.Sum256([]byte(RemotePath(path)))
	return filepath.Join(Ng2ConfigDir(), "moves-locks", hex.EncodeToString(h[:8])+".lock")
}

// lockProjectsForMove : prend le verrou de chacun des chemins (triés : pas d'interblocage). Refus immédiat si un autre
// déplacement tient déjà l'un d'eux (« déjà en cours ») ; un verrou dont le détenteur ne bat plus (60 s) est repris.
// Le verrou est entretenu (date du fichier) pendant les copies longues.
func lockProjectsForMove(paths ...string) (unlock func(), err error) {
	seen := map[string]bool{}
	var list []string
	for _, p := range paths {
		if k := filepath.Clean(p); !seen[k] {
			seen[k] = true
			list = append(list, k)
		}
	}
	sort.Strings(list)
	token := newLockToken() + " " + LauncherID()
	var held []string
	release := func() {
		for _, f := range held {
			if lockOwnedBy(f, token) {
				_ = removeRetry(f)
			}
		}
	}
	for _, p := range list {
		f := moveLockPath(p)
		var created bool
		for try := 0; try < 2 && !created; try++ {
			var cerr error
			created, cerr = createExclusive(f, []byte(token+"\n"), 0o644)
			if cerr != nil {
				release()
				return nil, fmt.Errorf("verrou de déplacement : %w", cerr)
			}
			if !created {
				if st, serr := os.Stat(f); serr == nil && time.Since(st.ModTime()) > fileLockStale.Get() {
					_, _ = reclaimStale(f, fileLockStale.Get())
					continue
				}
				break
			}
		}
		if !created {
			release()
			return nil, errors.New("un déplacement est déjà en cours pour ce projet")
		}
		held = append(held, f)
	}
	stop := make(chan struct{})
	go func() {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				now := time.Now()
				for _, f := range held {
					_ = os.Chtimes(f, now, now)
				}
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(stop); release() }) }, nil
}

// MoveProject : déplace / renomme le projet path vers target (règle commune de chemin, destination inexistante).
func MoveProject(env MoveEnv, path string, target MoveTarget) MoveResult {
	if _, _, _, ok := SplitProjectPath(env.Roots, path); !ok {
		return MoveResult{Msg: "projet hors des bases configurées"}
	}
	if st, err := os.Stat(path); err != nil || !st.IsDir() {
		return MoveResult{Msg: "projet introuvable"}
	}
	dest, err := ResolveProjectPathIn(env.Roots, target.Base, target.Category, target.Name)
	if err != nil {
		return MoveResult{Msg: err.Error()}
	}
	// exclusion : verrou de la source ET de la destination, de la vérification des blockers jusqu'à la fin
	unlock, lerr := lockProjectsForMove(path, dest)
	if lerr != nil {
		return MoveResult{Msg: "déplacement impossible : " + lerr.Error()}
	}
	defer unlock()
	if bl := MoveBlockers(env, path); len(bl) > 0 {
		return MoveResult{Msg: "déplacement impossible : " + strings.Join(bl, ", ")}
	}
	if filepath.Clean(dest) == filepath.Clean(path) {
		return MoveResult{Msg: "la destination est le projet lui-même"}
	}
	if _, err := os.Lstat(dest); err == nil {
		return MoveResult{Msg: "la destination existe déjà"}
	}
	if !target.NewCategory && target.Category != "" {
		if cat, cerr := ResolveCategoryPathIn(env.Roots, target.Base, target.Category); cerr != nil {
			return MoveResult{Msg: cerr.Error()}
		} else if _, serr := os.Stat(cat); serr != nil {
			return MoveResult{Msg: "catégorie inexistante"}
		}
	}
	// une destination dans le projet lui-même (ou le contenant) est refusée
	if strings.HasPrefix(filepath.Clean(dest)+string(filepath.Separator), filepath.Clean(path)+string(filepath.Separator)) {
		return MoveResult{Msg: "la destination est dans le projet"}
	}
	be := ""
	if HasEnv(path) {
		be = ProjectBackend(path).Name
	}
	e := &MoveEntry{ID: newMoveID(), From: path, To: dest, Backend: be, Done: []string{}, State: "running",
		Detail: map[string]string{"newCategory": fmt.Sprint(target.NewCategory), "base": filepath.Clean(target.Base)},
		At:     time.Now().UTC().Format(time.RFC3339)}
	if err := saveEntry(e); err != nil {
		return MoveResult{Msg: "journal des déplacements : " + err.Error()}
	}
	return runMove(env, e)
}

// ResumeMove : reprend un déplacement inachevé (rejoue depuis l'étape en échec).
func ResumeMove(env MoveEnv, path string) MoveResult {
	e := findEntry(path)
	if e == nil {
		return MoveResult{Msg: "aucun déplacement inachevé"}
	}
	unlock, lerr := lockProjectsForMove(e.From, e.To)
	if lerr != nil {
		return MoveResult{Msg: "reprise impossible : " + lerr.Error()}
	}
	defer unlock()
	e.State, e.Failed, e.Msg = "running", "", ""
	_ = saveEntry(e)
	return runMove(env, e)
}

// CancelMove : annule un déplacement inachevé (retour arrière des étapes faites).
func CancelMove(env MoveEnv, path string) MoveResult {
	e := findEntry(path)
	if e == nil {
		return MoveResult{Msg: "aucun déplacement inachevé"}
	}
	unlock, lerr := lockProjectsForMove(e.From, e.To)
	if lerr != nil {
		return MoveResult{Msg: "annulation impossible : " + lerr.Error()}
	}
	defer unlock()
	return rollback(env, e, "", errors.New("annulé"))
}

// MoveBlockersExceptPending : blockers hors « déplacement inachevé » (reprise / abandon : seul le journal est attendu).
func MoveBlockersExceptPending(env MoveEnv, path string) []string {
	out := []string{}
	for _, b := range MoveBlockers(env, path) {
		if b != "déplacement inachevé" {
			out = append(out, b)
		}
	}
	return out
}

func findEntryByID(id string) *MoveEntry {
	for _, e := range readMovesCached() {
		if e.ID == id {
			c := e
			return &c
		}
	}
	return nil
}

// ResumeMoveID : « Reprendre » — rejoue les étapes restantes de l'entrée id (les étapes faites ne sont pas refaites).
func ResumeMoveID(env MoveEnv, id string) MoveResult {
	e := findEntryByID(id)
	if e == nil {
		return MoveResult{Msg: "aucun déplacement inachevé"}
	}
	unlock, lerr := lockProjectsForMove(e.From, e.To)
	if lerr != nil {
		return MoveResult{Msg: "reprise impossible : " + lerr.Error()}
	}
	defer unlock()
	if e = findEntryByID(id); e == nil { // relu sous verrou : une autre exécution a pu finir entre-temps
		return MoveResult{Msg: "aucun déplacement inachevé"}
	}
	if bl := MoveBlockersExceptPending(env, e.From); len(bl) > 0 {
		return MoveResult{Msg: "reprise impossible : " + strings.Join(bl, ", ")}
	}
	if bl := MoveBlockersExceptPending(env, e.To); len(bl) > 0 {
		return MoveResult{Msg: "reprise impossible : " + strings.Join(bl, ", ")}
	}
	e.State, e.Failed, e.Msg = "running", "", ""
	_ = saveEntry(e)
	return runMove(env, e)
}

// AbortMoveID : « Annuler » — retour arrière des étapes faites (l'étape ctr ne s'annule pas). Succès : chemin et clé
// d'origine, rolledBack.
func AbortMoveID(env MoveEnv, id string) MoveResult {
	e := findEntryByID(id)
	if e == nil {
		return MoveResult{Msg: "aucun déplacement inachevé"}
	}
	unlock, lerr := lockProjectsForMove(e.From, e.To)
	if lerr != nil {
		return MoveResult{Msg: "annulation impossible : " + lerr.Error()}
	}
	defer unlock()
	if e = findEntryByID(id); e == nil {
		return MoveResult{Msg: "aucun déplacement inachevé"}
	}
	for _, p := range []string{e.From, e.To} {
		if bl := MoveBlockersExceptPending(env, p); len(bl) > 0 {
			return MoveResult{Msg: "annulation impossible : " + strings.Join(bl, ", ")}
		}
	}
	r := rollback(env, e, "", errors.New("annulé"))
	if r.RolledBack {
		r.OK, r.Path, r.Key, r.Step, r.Msg = true, e.From, CtrName(e.From), "", ""
	}
	return r
}

func findEntry(path string) *MoveEntry {
	for _, e := range readMovesCached() {
		if e.From == path || e.To == path {
			c := e
			return &c
		}
	}
	return nil
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// runMove : joue les étapes non faites, dans l'ordre.
func runMove(env MoveEnv, e *MoveEntry) MoveResult {
	for _, step := range moveStepOrder {
		if has(e.Done, step) {
			continue
		}
		err := env.fail(step, false)
		if err == nil {
			err = preCheck(env, e, step) // blockers revérifiés juste avant l'étape longue (dir) et l'étape irréversible (ctr)
		}
		if err == nil {
			err = doStep(env, e, step)
		}
		if err != nil {
			return rollback(env, e, step, err)
		}
		e.Done = append(e.Done, step)
		_ = saveEntry(e)
	}
	_ = dropEntry(e.ID)
	return MoveResult{OK: true, Path: e.To, Key: CtrName(e.To)}
}

// rollback : étapes faites annulées dans l'ordre inverse (« ctr » n'est jamais annulée : elle est la dernière).
func rollback(env MoveEnv, e *MoveEntry, failed string, cause error) MoveResult {
	if failed == MoveStepDir { // l'étape dir n'est pas « faite » : ses restes (catégories créées, copie sans source) sont nettoyés
		if e.Detail["dir"] == "copy-source-kept" {
			if err := undoMoveDir(env, e); err != nil {
				e.State, e.Failed, e.Msg = "partial", MoveStepDir, err.Error()
				_ = saveEntry(e)
				return MoveResult{Step: failed, Msg: cause.Error() + " ; retour arrière impossible à l'étape dir : " + err.Error(), State: "partial", Done: []string{}}
			}
		} else {
			cleanupMadeCats(e)
		}
	}
	for i := len(moveStepOrder) - 1; i >= 0; i-- {
		step := moveStepOrder[i]
		if !has(e.Done, step) || step == MoveStepCtr {
			continue
		}
		err := env.fail(step, true)
		if err == nil {
			err = undoStep(env, e, step)
		}
		if err != nil { // retour arrière en échec : l'entrée reste, « partial »
			e.State, e.Failed, e.Msg = "partial", step, err.Error()
			_ = saveEntry(e)
			return MoveResult{Step: failed, Msg: cause.Error() + " ; retour arrière impossible à l'étape " + step + " : " + err.Error(),
				State: "partial", Done: append([]string(nil), e.Done...)}
		}
		e.Done = removeStr(e.Done, step)
		_ = saveEntry(e)
	}
	_ = dropEntry(e.ID)
	return MoveResult{Step: failed, Msg: cause.Error(), RolledBack: true}
}

func removeStr(l []string, s string) []string {
	out := l[:0:0]
	for _, x := range l {
		if x != s {
			out = append(out, x)
		}
	}
	return out
}

// preCheck : avant « dir » (copie de plusieurs minutes) et avant « ctr » (rm -f d'un conteneur), les blockers sont
// revérifiés — un projet ouvert, un conteneur démarré ou une session d'un autre launcher depuis le premier contrôle
// font échouer proprement l'étape (retour arrière), jamais un `rm -f` d'une session vivante.
func preCheck(env MoveEnv, e *MoveEntry, step string) error {
	switch step {
	case MoveStepDir:
		if bl := MoveBlockersExceptPending(env, e.From); len(bl) > 0 {
			return errors.New("le projet n'est plus déplaçable : " + strings.Join(bl, ", "))
		}
	case MoveStepCtr:
		if bl := ctrBlockers(env, e); len(bl) > 0 {
			return errors.New("conteneur non supprimé, le projet est utilisé : " + strings.Join(bl, ", "))
		}
	}
	return nil
}

// ctrBlockers : sessions ou conteneurs actifs de l'ANCIENNE clé, revus avant l'étape ctr. Le projet est déjà au nouveau
// chemin (HasEnv(From) est faux) : les contrôles par défaut partent du backend noté dans le journal.
func ctrBlockers(env MoveEnv, e *MoveEntry) []string {
	var out []string
	if env.OpenHere != nil && (env.OpenHere(e.From) || env.OpenHere(e.To)) {
		out = append(out, "projet ouvert dans ce launcher")
	}
	foreign := func(p string) string { return foreignOwnerIn(e.Backend, p) }
	if env.Foreign != nil {
		foreign = env.Foreign
	}
	if who := foreign(e.From); who != "" {
		out = append(out, "projet ouvert par un autre launcher ("+who+")")
	}
	running := func(p string) string { return runningCtrIn(e.Backend, p) }
	if env.Running != nil {
		running = env.Running
	}
	if what := running(e.From); what != "" {
		out = append(out, "conteneur ou pod en cours d'exécution ("+what+")")
	}
	return out
}

// runningCtrIn : conteneur du backend (nom) dont le projet from tourne ; "" sinon. Sans condition sur env.yaml.
func runningCtrIn(backend, from string) string {
	b, ok := FindBackend(backend)
	if backend == "" || backend == "host" || !ok {
		return ""
	}
	e, ok := EngineOf(b)
	if !ok {
		return ""
	}
	fs, err := e.List()
	if err != nil {
		return ""
	}
	for _, f := range fs {
		if f.Project == from && f.Running {
			return f.Name + " sur " + b.Name
		}
	}
	return ""
}

// foreignOwnerIn : propriétaire vivant, d'un autre launcher, d'une session du projet from sur ce backend.
func foreignOwnerIn(backend, from string) string {
	b, ok := FindBackend(backend)
	if backend == "" || backend == "host" || !ok {
		return ""
	}
	e, ok := EngineOf(b)
	if !ok {
		return ""
	}
	fs, err := e.List()
	if err != nil {
		return ""
	}
	for _, f := range fs {
		if f.Project != from || !f.Running || !e.Of(f).HasSession(f.Name) {
			continue
		}
		if o := e.Of(f).Owner(f.Name); o.Alive() && o.Id != LauncherID() {
			return o.Id + " (" + b.Name + ")"
		}
	}
	return ""
}

func doStep(env MoveEnv, e *MoveEntry, step string) error {
	switch step {
	case MoveStepDir:
		return moveDir(env, e)
	case MoveStepAssign:
		return moveAssignment(e.From, e.To)
	case MoveStepConf:
		return moveConf(e.From, e.To)
	case MoveStepHistory:
		return moveHistory(env, e.From, e.To)
	case MoveStepTrust:
		_, err := MoveTrustEntry(e.From, e.To)
		if errors.Is(err, ErrTrustBroken) && !HasEnv(e.To) { // registre illisible : ne bloque que les projets à environnement
			return nil
		}
		return err
	case MoveStepCtr:
		rm := env.RemoveCtr
		if rm == nil {
			rm = removeProjectCtr
		}
		return rm(e.From, e.Backend)
	}
	return fmt.Errorf("étape inconnue : %s", step)
}

func undoStep(env MoveEnv, e *MoveEntry, step string) error {
	switch step {
	case MoveStepDir:
		return undoMoveDir(env, e)
	case MoveStepAssign:
		return moveAssignment(e.To, e.From)
	case MoveStepConf:
		return moveConf(e.To, e.From)
	case MoveStepHistory:
		return moveHistory(env, e.To, e.From)
	case MoveStepTrust:
		_, err := MoveTrustEntry(e.To, e.From)
		if errors.Is(err, ErrTrustBroken) && !HasEnv(e.From) {
			return nil
		}
		return err
	}
	return nil
}

// ── Étape dir ─────────────────────────────────────────────────────────────

// moveDir : catégories absentes créées (notées), puis os.Rename ; EXDEV → copie complète vers <dest>.moving-<id>,
// contrôle (nombre et taille des fichiers), rename en <dest>, suppression de la source.
func moveDir(env MoveEnv, e *MoveEntry) error {
	// reprise après un arrêt entre le rename de la copie et la suppression de la source : la destination est complète
	// (contrôle de contenu) ⇒ le déplacement se termine, sans retour arrière par copie inverse
	if d := e.Detail["dir"]; d == "copy-done" || d == "copy" || d == "copy-source-kept" {
		if _, serr := os.Lstat(e.From); serr == nil {
			if _, derr := os.Lstat(e.To); derr == nil && sameTreeContent(e.From, e.To) == nil {
				if err := removeAllRetry(e.From); err != nil {
					e.Detail["dir"] = "copy-source-kept"
					return fmt.Errorf("suppression de la source : %w", err)
				}
				return nil
			}
		}
	}
	// idempotent : déjà à destination (reprise après un arrêt entre le rename et la mise à jour du journal)
	if _, serr := os.Lstat(e.From); os.IsNotExist(serr) {
		if _, derr := os.Lstat(e.To); derr == nil {
			e.Detail["dir"] = "rename"
			return nil
		}
	}
	if _, err := os.Lstat(e.To); err == nil && !has(e.Done, MoveStepDir) && e.Detail["dir"] == "" {
		return errors.New("la destination existe déjà")
	}
	parent := filepath.Dir(e.To)
	if e.Detail["newCategory"] == "true" {
		base := e.Detail["base"]
		var chain []string
		for p := parent; p != base && p != filepath.Dir(p); p = filepath.Dir(p) {
			if _, err := os.Stat(p); err != nil {
				chain = append([]string{p}, chain...)
			}
		}
		var made []string
		for _, p := range chain {
			switch merr := os.Mkdir(p, 0o755); {
			case merr == nil:
				made = append(made, p)
			case errors.Is(merr, os.ErrExist):
			default:
				return merr
			}
		}
		if len(made) > 0 {
			b, _ := json.Marshal(made)
			e.Detail["madeCats"] = string(b)
			_ = saveEntry(e)
		}
	}
	err := env.rename(e.From, e.To)
	if err == nil {
		e.Detail["dir"] = "rename"
		return nil
	}
	if !isCrossDevice(err) {
		return err
	}
	tmp := e.To + ".moving-" + e.ID
	e.Detail["dir"], e.Detail["tmp"] = "copy", tmp
	_ = saveEntry(e)
	if err := copyTree(e.From, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("copie vers un autre système de fichiers : %w", err)
	}
	// la copie est durable (fsync des fichiers et des dossiers) ET identique (contenu, pas seulement nombre et taille)
	// AVANT que la source ne soit touchée
	if err := sameTreeContent(e.From, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("contrôle de la copie : %w", err)
	}
	if err := os.Rename(tmp, e.To); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	_ = platform.SyncDir(filepath.Dir(e.To))
	e.Detail["dir"] = "copy-done" // destination complète : la reprise termine par la suppression de la source
	_ = saveEntry(e)
	if err := removeAllRetry(e.From); err != nil {
		e.Detail["dir"] = "copy-source-kept"
		return fmt.Errorf("suppression de la source : %w", err)
	}
	return nil
}

// undoMoveDir : rename inverse ; après une copie, copie inverse (comble les fichiers de la source déjà supprimés),
// contrôle, puis suppression de la destination ; catégories créées par le déplacement retirées si vides.
func undoMoveDir(env MoveEnv, e *MoveEntry) error {
	switch e.Detail["dir"] {
	case "copy", "copy-source-kept", "copy-done":
		if err := copyTree(e.To, e.From); err != nil {
			return err
		}
		if err := sameTreeContent(e.To, e.From); err != nil {
			return err
		}
		if err := removeAllRetry(e.To); err != nil {
			return err
		}
	default:
		if err := env.rename(e.To, e.From); err != nil {
			if !isCrossDevice(err) {
				return err
			}
			if err := copyTree(e.To, e.From); err != nil {
				return err
			}
			if err := sameTreeContent(e.To, e.From); err != nil {
				return err
			}
			if err := removeAllRetry(e.To); err != nil {
				return err
			}
		}
	}
	cleanupMadeCats(e)
	delete(e.Detail, "dir")
	return nil
}

// cleanupMadeCats : catégories créées par le déplacement, retirées si vides (du bas vers le haut).
func cleanupMadeCats(e *MoveEntry) {
	var made []string
	_ = json.Unmarshal([]byte(e.Detail["madeCats"]), &made)
	for i := len(made) - 1; i >= 0; i-- {
		_ = os.Remove(made[i]) // vide seulement
	}
	delete(e.Detail, "madeCats")
}

// copyTree : copie récursive (modes, dates de modification, liens symboliques recréés tels quels — relatifs ou absolus) ;
// dst est créé. Chaque fichier est fsync, puis chaque dossier : la copie est durable avant toute suppression de la source.
func copyTree(src, dst string) error {
	var dirs [][2]string // dossier copié, date à lui rendre (après ses enfants)
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		t := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			dirs = append(dirs, [2]string{t, info.ModTime().Format(time.RFC3339Nano)})
			return os.MkdirAll(t, info.Mode().Perm()|0o700)
		case info.Mode()&os.ModeSymlink != 0:
			l, err := os.Readlink(p)
			if err != nil {
				return err
			}
			_ = os.Remove(t)
			return os.Symlink(l, t)
		case info.Mode().IsRegular():
			if err := copyFile(p, t, info.Mode().Perm()); err != nil {
				return err
			}
			return os.Chtimes(t, info.ModTime(), info.ModTime())
		}
		return nil // sockets, périphériques : sans objet dans un projet
	})
	if err != nil {
		return err
	}
	for i := len(dirs) - 1; i >= 0; i-- { // du bas vers le haut : fsync du dossier puis date de modification
		_ = platform.SyncDir(dirs[i][0])
		if mt, perr := time.Parse(time.RFC3339Nano, dirs[i][1]); perr == nil {
			_ = os.Chtimes(dirs[i][0], mt, mt)
		}
	}
	return nil
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil { // le contenu est sur le support avant que la source ne soit supprimée
		out.Close()
		return err
	}
	return out.Close()
}

// sameTreeContent : b reproduit a — mêmes entrées (aucune en trop, aucune manquante), même taille ET même empreinte
// SHA-256 pour chaque fichier, mêmes cibles de liens symboliques.
func sameTreeContent(a, b string) error {
	na := 0
	err := filepath.WalkDir(a, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(a, p)
		q := filepath.Join(b, rel)
		ia, err := d.Info()
		if err != nil {
			return err
		}
		ib, err := os.Lstat(q)
		if err != nil {
			return fmt.Errorf("%s manquant dans la copie", rel)
		}
		switch {
		case ia.IsDir():
			if !ib.IsDir() {
				return fmt.Errorf("%s : dossier d'un côté seulement", rel)
			}
		case ia.Mode()&os.ModeSymlink != 0:
			na++
			la, _ := os.Readlink(p)
			lb, _ := os.Readlink(q)
			if ib.Mode()&os.ModeSymlink == 0 || la != lb {
				return fmt.Errorf("%s : lien symbolique différent", rel)
			}
		case ia.Mode().IsRegular():
			na++
			if !ib.Mode().IsRegular() || ia.Size() != ib.Size() {
				return fmt.Errorf("%s : taille différente", rel)
			}
			ha, err1 := fileSHA(p)
			hb, err2 := fileSHA(q)
			if err1 != nil || err2 != nil || ha != hb {
				return fmt.Errorf("%s : contenu différent", rel)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	nb, _, err := treeSig(b) // aucune entrée en trop dans la copie
	if err != nil {
		return err
	}
	if na != nb {
		return fmt.Errorf("%d fichiers d'un côté, %d de l'autre", na, nb)
	}
	return nil
}

func fileSHA(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// treeSig : nombre d'entrées (fichiers réguliers et liens) et taille totale des fichiers réguliers.
func treeSig(root string) (n int, size int64, err error) {
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			n++
			if info.Mode().IsRegular() {
				size += info.Size()
			}
		}
		return nil
	})
	return
}

// ── Étape assign ──────────────────────────────────────────────────────────

// moveAssignment : l'entrée de projects.json de from passe à to (une seule écriture, sous verrou).
func moveAssignment(from, to string) error {
	return WithFileLock(assignPath(), func() error {
		m := ReadAssignments()
		v, ok := m[from]
		if !ok {
			return nil
		}
		delete(m, from)
		m[to] = v
		return saveAssignments(m)
	})
}

// ── Étape conf ────────────────────────────────────────────────────────────

// moveConf : entrées PROJECT_COLORS / PROJECT_LAYOUTS / PROJECT_HIDE_IDLE (clé = nom du dossier) reportées si la clé
// change — UNE seule écriture de config.conf (SaveConfig : validation, conflit, copie .bak).
func moveConf(from, to string) error {
	oldName, newName := filepath.Base(from), filepath.Base(to)
	if oldName == newName {
		return nil
	}
	cfg := LoadConfig()
	vals := []struct {
		set   func(src, name, v string) (string, error)
		value string
	}{{SetProjectColor, cfg.ProjectColors[oldName]}, {SetProjectLayout, cfg.ProjectLayouts[oldName]}, {SetProjectHideIdle, cfg.ProjectHideIdle[oldName]}}
	anyVal := false
	for _, v := range vals {
		anyVal = anyVal || v.value != ""
	}
	if !anyVal {
		return nil
	}
	path := ConfigPath()
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	out := string(b)
	for _, v := range vals {
		if v.value == "" {
			continue
		}
		if out, err = v.set(out, oldName, ""); err != nil {
			return err
		}
		if out, err = v.set(out, newName, v.value); err != nil {
			return err
		}
	}
	_, err = SaveConfig(ConfConf, path, []byte(out), st.ModTime())
	return err
}

// ── Étape history ─────────────────────────────────────────────────────────

// moveHistory : dossiers <claude>/projects/<chemin encodé> renommés (chemin local et chemin vu des backends).
func moveHistory(env MoveEnv, from, to string) error {
	root := filepath.Join(env.claudeDir(), "projects")
	seen := map[string]bool{}
	for _, pair := range [][2]string{{from, to}, {RemotePath(from), RemotePath(to)}} {
		a, b := ProjectDirName(pair[0]), ProjectDirName(pair[1])
		if a == b || seen[a] {
			continue
		}
		seen[a] = true
		src := filepath.Join(root, a)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		dst := filepath.Join(root, b)
		if _, err := os.Stat(dst); err == nil {
			return fmt.Errorf("historique de conversations : %s existe déjà", dst)
		}
		if err := os.Rename(src, dst); err != nil {
			return err
		}
	}
	return nil
}

// ── Étape ctr ─────────────────────────────────────────────────────────────

// removeProjectCtr : conteneur (ou StatefulSet + pod) de l'ancienne clé supprimé, sur tous les serveurs du backend du
// projet ; il est recréé au nouveau nom à la prochaine ouverture. Rien à faire sans backend à moteur.
func removeProjectCtr(from, backend string) error {
	if backend == "" || backend == "host" {
		return nil
	}
	b, ok := FindBackend(backend)
	if !ok {
		return nil
	}
	e, ok := EngineOf(b)
	if !ok {
		return nil
	}
	found, err := e.List()
	if err != nil {
		return fmt.Errorf("conteneurs de %s illisibles : %w", backend, err)
	}
	var errs []string
	for _, f := range found {
		if f.Project != from {
			continue
		}
		if err := e.Of(f).RemoveProject(from, f.Name); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return errors.New(strings.Join(errs, " ; "))
	}
	return nil
}

// RemoveProject : supprime le conteneur name (volumes anonymes compris), ou le StatefulSet du projet sur k8s.
func (e Engine) RemoveProject(proj, name string) error {
	var out []byte
	var err error
	if e.B.K8s() {
		sts := strings.TrimSuffix(name, "-0")
		out, err = e.kcmd("delete", "statefulset", sts, "--wait=true", "--timeout=60s", "--ignore-not-found").CombinedOutput()
	} else {
		out, err = e.cmd("rm", "-f", "-v", name).CombinedOutput()
	}
	if err != nil {
		return fmt.Errorf("suppression de %s : %v (%s)", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}
