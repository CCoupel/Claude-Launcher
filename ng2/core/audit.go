package core

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"
)

// auditFrequent : méthodes agrégées par (utilisateur, méthode) et par minute (P3).
var auditFrequent = map[string]bool{
	"termInput": true, "termResize": true, "termFocus": true, "termSync": true, "termZoom": true, "resizePane": true,
	"launchInput": true, "cardsFor": true, "select": true, "init": true, "clientInfo": true, "backendStates": true,
	"setPrefs": true,
}

// auditBurst : au-delà, par minute et par utilisateur, toute méthode non R1 est agrégée.
const auditBurst = 60

// auditGenerations : fichiers de rotation gardés (audit.jsonl.1 … .5).
const auditGenerations = 5

// ── Journal d'audit (v7.0.0, #251, contracts/auth.md §5) ────────────────────
//
// <CacheDir>/audit.jsonl (0600, ajout, une ligne JSON par entrée), mode proxy seulement. Une entrée par appel ; les méthodes
// fréquentes sont agrégées par (utilisateur, méthode) et par minute. Jamais d'argument, de contenu, de valeur ni de chemin.

const (
	auditFile    = "audit.jsonl"
	auditMaxSize = 10 << 20 // au-delà : audit.jsonl → audit.jsonl.1 (une génération)
)

// AuditEntry : un appel (ou une ouverture de flux, ou une lecture /files) à journaliser.
type AuditEntry struct {
	User    string
	Role    string
	Src     string // adresse source (RemoteAddr, X-Forwarded-For en indication)
	Method  string // méthode d'appel, « events » (flux), « files » (lecture /files)
	Project string // nom du projet dans le launcher (core.CtrName) ; "" : l'appel n'en porte pas
	Phase   string // « intent » : intention d'une action R1, journalisée avant son exécution (le résultat suit)
	Target  string // méthodes R1 : identifiant court (kind de config, nom de backend), jamais une valeur
	OK      bool
	Err     string // court : forbidden, invalid, error
}

type auditAgg struct {
	count int
	since time.Time
}

type auditKey struct{ user, method string }

type denyKey struct {
	login string
	code  int
}

type auditBucket struct {
	minute time.Time
	n      int
}

// Audit : écrivain du journal.
type Audit struct {
	mu     sync.Mutex
	dir    string // "" : CacheDir() à chaque écriture
	agg    map[auditKey]*auditAgg
	deny   map[denyKey]*auditAgg
	perMin map[string]*auditBucket // appels par utilisateur dans la minute courante (seuil auditBurst)
	warned bool
	now    func() time.Time
	// out : copie de chaque ligne préfixée « audit » (sortie standard du conteneur, collectée par Kubernetes : non effaçable
	// par l'application) ; nil : aucune copie
	out io.Writer
}

// NewAudit : journal dans dir ("" : dossier de cache du launcher).
func NewAudit(dir string) *Audit {
	return &Audit{dir: dir, agg: map[auditKey]*auditAgg{}, deny: map[denyKey]*auditAgg{}, perMin: map[string]*auditBucket{}, now: time.Now, out: os.Stdout}
}

func (a *Audit) path() string {
	d := a.dir
	if d == "" {
		d = CacheDir()
	}
	return filepath.Join(d, auditFile)
}

func auditTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

// Check : le journal est inscriptible (sans rien écrire). Sert aux méthodes R1 : refusées si le journal l'est pas.
func (a *Audit) Check() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	f, err := os.OpenFile(a.path(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("journal d'audit inaccessible")
	}
	return f.Close()
}

// Record : consigne l'entrée (agrégée si la méthode est fréquente). Erreur : écriture impossible ; avertissement unique
// sur la sortie d'erreur (les appels non R1 continuent).
func (a *Audit) Record(e AuditEntry) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	err := a.flushLocked(now, false)
	e = auditClean(e)
	c, known := ClassOf(e.Method)
	r1 := known && c == ClassR1 // « events » et « files » ne sont pas des méthodes d'appel
	freq := auditFrequent[e.Method] || e.Method == "files"
	if !freq && !r1 && e.User != "" { // rafale : au-delà de auditBurst appels par minute, agrégé (jamais pour les appels R1)
		bk := a.perMin[e.User]
		if bk == nil || !bk.minute.Equal(now.Truncate(time.Minute)) {
			bk = &auditBucket{minute: now.Truncate(time.Minute)}
			a.perMin[e.User] = bk
		}
		if bk.n++; bk.n > auditBurst {
			freq = true
		}
	}
	if r1 && !e.OK && e.Phase == "" && e.User != "" { // refus R1 : peu coûteux pour un client, donc soumis à la rafale (jamais les succès)
		bk := a.perMin[e.User]
		if bk == nil || !bk.minute.Equal(now.Truncate(time.Minute)) {
			bk = &auditBucket{minute: now.Truncate(time.Minute)}
			a.perMin[e.User] = bk
		}
		if bk.n++; bk.n > auditBurst {
			k := auditKey{e.User, e.Method + " (refusé)"}
			g := a.agg[k]
			if g == nil {
				g = &auditAgg{since: now.Truncate(time.Minute)}
				a.agg[k] = g
			}
			g.count++
			return err
		}
	}
	if freq && !r1 {
		k := auditKey{e.User, e.Method}
		g := a.agg[k]
		if g == nil {
			g = &auditAgg{since: now.Truncate(time.Minute)}
			a.agg[k] = g
		}
		g.count++
		return err
	}
	rec := map[string]any{"ts": auditTime(now), "user": e.User, "role": e.Role, "src": e.Src, "method": e.Method, "ok": e.OK}
	if e.Project != "" {
		rec["project"] = e.Project
	}
	if e.Target != "" {
		rec["target"] = e.Target
	}
	if e.Err != "" {
		rec["err"] = e.Err
	}
	if e.Phase != "" {
		rec["phase"] = e.Phase
	}
	if werr := a.writeLocked(rec); werr != nil {
		return werr
	}
	return err
}

// Deny : refus d'identité du launcher (401, 403), agrégé par minute (login tenté tronqué à 64 caractères, code).
func (a *Audit) Deny(login string, code int) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	err := a.flushLocked(now, false)
	if r := []rune(login); len(r) > 64 {
		login = string(r[:64])
	}
	login = cleanCtl(login)
	k := denyKey{login, code}
	g := a.deny[k]
	if g == nil {
		g = &auditAgg{since: now.Truncate(time.Minute)}
		a.deny[k] = g
	}
	g.count++
	return err
}

// cleanCtl : caractères de contrôle retirés.
func cleanCtl(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r >= 0x20 && r != 0x7f {
			out = append(out, r)
		}
	}
	return string(out)
}

// Flush : écrit les agrégats d'une minute révolue (all : tous, à l'arrêt).
func (a *Audit) Flush(all bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.flushLocked(a.now(), all)
}

func (a *Audit) flushLocked(now time.Time, all bool) error {
	keys := make([]auditKey, 0, len(a.agg))
	for k, g := range a.agg {
		if all || g.since.Before(now.Truncate(time.Minute)) {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].user+keys[i].method < keys[j].user+keys[j].method })
	var first error
	var dkeys []denyKey
	for k, g := range a.deny {
		if all || g.since.Before(now.Truncate(time.Minute)) {
			dkeys = append(dkeys, k)
		}
	}
	sort.Slice(dkeys, func(i, j int) bool {
		return dkeys[i].login+strconv.Itoa(dkeys[i].code) < dkeys[j].login+strconv.Itoa(dkeys[j].code)
	})
	for _, k := range dkeys {
		g := a.deny[k]
		rec := map[string]any{"ts": auditTime(now), "kind": "deny", "code": k.code, "count": g.count, "since": g.since.UTC().Format(time.RFC3339)}
		if k.login != "" {
			rec["login"] = k.login
		}
		if err := a.writeLocked(rec); err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		delete(a.deny, k)
	}
	for _, k := range keys {
		g := a.agg[k]
		rec := map[string]any{"ts": auditTime(now), "user": k.user, "kind": "agg", "method": k.method, "count": g.count, "since": g.since.UTC().Format(time.RFC3339)}
		if err := a.writeLocked(rec); err != nil {
			if first == nil {
				first = err
			}
			continue // gardé : réessayé à la prochaine écriture
		}
		delete(a.agg, k)
	}
	return first
}

func (a *Audit) writeLocked(rec map[string]any) error {
	b, _ := json.Marshal(rec) // encoding/json uniquement : jamais de concaténation
	p := a.path()
	if st, err := os.Stat(p); err == nil && st.Size() > auditMaxSize {
		for i := auditGenerations - 1; i >= 1; i-- { // .4 → .5 … .1 → .2, puis le fichier courant → .1 (la plus ancienne est perdue)
			_ = os.Rename(p+"."+strconv.Itoa(i), p+"."+strconv.Itoa(i+1))
		}
		_ = os.Rename(p, p+".1")
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err == nil {
		_, err = f.Write(append(b, '\n'))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if a.out != nil { // même ligne, préfixée, sur la sortie standard (écrite même si le fichier est inaccessible)
		_, _ = a.out.Write(append(append([]byte("audit "), b...), '\n'))
	}
	if err != nil {
		if !a.warned {
			a.warned = true
			fmt.Fprintln(os.Stderr, "⚠ journal d'audit inaccessible : les écritures sensibles (R1) sont refusées")
		}
		return fmt.Errorf("journal d'audit inaccessible")
	}
	return nil
}

// auditClean : champs d'origine client bornés — method 64, target et project 128 caractères, caractères de contrôle retirés ;
// une méthode inconnue (hors méthodes classées, « events » et « files ») devient « unknown » (pas de clé arbitraire dans l'agrégat).
func auditClean(e AuditEntry) AuditEntry {
	cut := func(s string, n int) string {
		s = cleanCtl(s)
		if r := []rune(s); len(r) > n {
			s = string(r[:n])
		}
		return s
	}
	e.Method = cut(e.Method, 64)
	if _, known := ClassOf(e.Method); !known && e.Method != "events" && e.Method != "files" && e.Method != "backend.call" {
		e.Method = "unknown"
	}
	e.Target, e.Project = cut(e.Target, 128), cut(e.Project, 128)
	return e
}
