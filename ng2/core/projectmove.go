package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
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

// MoveInfo : état d'un projet pour le dialogue (projectMoveInfo).
type MoveInfo struct {
	Base     string   `json:"base"`
	Category string   `json:"category"`
	Name     string   `json:"name"`
	Key      string   `json:"key"`
	Open     bool     `json:"open"`
	Blockers []string `json:"blockers"`
	// Pending : déplacement inachevé (reprise / annulation possibles)
	Pending *MoveEntry `json:"pending,omitempty"`
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
	return os.Rename(a, b)
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

func readMovesLocked() []MoveEntry {
	var out []MoveEntry
	if b, err := os.ReadFile(movesPath()); err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

func writeMovesLocked(m []MoveEntry) error {
	if len(m) == 0 {
		if err := os.Remove(movesPath()); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	return WriteFileAtomic(movesPath(), b, 0o600)
}

// editMoves : lire-modifier-écrire du journal sous verrou.
func editMoves(fn func(m []MoveEntry) []MoveEntry) error {
	return WithFileLock(movesPath(), func() error { return writeMovesLocked(fn(readMovesLocked())) })
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

// PendingMoves : déplacements inachevés (journal) qui concernent path (source ou destination).
func PendingMoves(path string) []MoveEntry {
	var out []MoveEntry
	for _, e := range readMovesCached() {
		if e.From == path || e.To == path {
			out = append(out, e)
		}
	}
	return out
}

func readMovesCached() []MoveEntry {
	var m []MoveEntry
	_ = WithFileLock(movesPath(), func() error { m = readMovesLocked(); return nil })
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
	if len(PendingMoves(path)) > 0 {
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

// MoveInfoFor : projectMoveInfo.
func MoveInfoFor(env MoveEnv, path string) (MoveInfo, error) {
	base, cat, name, ok := SplitProjectPath(env.Roots, path)
	if !ok {
		return MoveInfo{}, errors.New("projet hors des bases configurées")
	}
	info := MoveInfo{Base: base, Category: cat, Name: name, Key: CtrName(path), Blockers: MoveBlockers(env, path)}
	info.Open = env.OpenHere != nil && env.OpenHere(path)
	if p := PendingMoves(path); len(p) > 0 {
		info.Pending = &p[0]
	}
	return info, nil
}

// ── Déplacement ───────────────────────────────────────────────────────────

func newMoveID() string { return newLockToken() }

// MoveProject : déplace / renomme le projet path vers target (règle commune de chemin, destination inexistante).
func MoveProject(env MoveEnv, path string, target MoveTarget) MoveResult {
	if _, _, _, ok := SplitProjectPath(env.Roots, path); !ok {
		return MoveResult{Msg: "projet hors des bases configurées"}
	}
	if st, err := os.Stat(path); err != nil || !st.IsDir() {
		return MoveResult{Msg: "projet introuvable"}
	}
	if bl := MoveBlockers(env, path); len(bl) > 0 {
		return MoveResult{Msg: "déplacement impossible : " + strings.Join(bl, ", ")}
	}
	dest, err := ResolveProjectPathIn(env.Roots, target.Base, target.Category, target.Name)
	if err != nil {
		return MoveResult{Msg: err.Error()}
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
	return rollback(env, e, "", errors.New("annulé"))
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
		return err
	}
	return nil
}

// ── Étape dir ─────────────────────────────────────────────────────────────

// moveDir : catégories absentes créées (notées), puis os.Rename ; EXDEV → copie complète vers <dest>.moving-<id>,
// contrôle (nombre et taille des fichiers), rename en <dest>, suppression de la source.
func moveDir(env MoveEnv, e *MoveEntry) error {
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
	if err := sameTree(e.From, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("contrôle de la copie : %w", err)
	}
	if err := os.Rename(tmp, e.To); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if err := os.RemoveAll(e.From); err != nil {
		e.Detail["dir"] = "copy-source-kept"
		return fmt.Errorf("suppression de la source : %w", err)
	}
	return nil
}

// undoMoveDir : rename inverse ; après une copie, copie inverse (comble les fichiers de la source déjà supprimés),
// contrôle, puis suppression de la destination ; catégories créées par le déplacement retirées si vides.
func undoMoveDir(env MoveEnv, e *MoveEntry) error {
	switch e.Detail["dir"] {
	case "copy", "copy-source-kept":
		if err := copyTree(e.To, e.From); err != nil {
			return err
		}
		if err := sameTree(e.To, e.From); err != nil {
			return err
		}
		if err := os.RemoveAll(e.To); err != nil {
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
			if err := sameTree(e.To, e.From); err != nil {
				return err
			}
			if err := os.RemoveAll(e.To); err != nil {
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

// copyTree : copie récursive (modes, liens symboliques recréés) ; dst est créé.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
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
			return os.MkdirAll(t, info.Mode().Perm()|0o700)
		case info.Mode()&os.ModeSymlink != 0:
			l, err := os.Readlink(p)
			if err != nil {
				return err
			}
			_ = os.Remove(t)
			return os.Symlink(l, t)
		case info.Mode().IsRegular():
			return copyFile(p, t, info.Mode().Perm())
		}
		return nil // sockets, périphériques : sans objet dans un projet
	})
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
	return out.Close()
}

// treeSig : nombre d'entrées et taille totale des fichiers réguliers.
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

// sameTree : a et b ont le même nombre de fichiers et la même taille totale.
func sameTree(a, b string) error {
	na, sa, err := treeSig(a)
	if err != nil {
		return err
	}
	nb, sb, err := treeSig(b)
	if err != nil {
		return err
	}
	if na != nb || sa != sb {
		return fmt.Errorf("%d fichiers / %d octets d'un côté, %d / %d de l'autre", na, sa, nb, sb)
	}
	return nil
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
