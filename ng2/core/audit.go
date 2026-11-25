package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

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
	OK      bool
	Err     string // court : forbidden, invalid, error
}

type auditAgg struct {
	count int
	since time.Time
}

type auditKey struct{ user, method string }

// Audit : écrivain du journal.
type Audit struct {
	mu     sync.Mutex
	dir    string // "" : CacheDir() à chaque écriture
	agg    map[auditKey]*auditAgg
	warned bool
	now    func() time.Time
}

// NewAudit : journal dans dir ("" : dossier de cache du launcher).
func NewAudit(dir string) *Audit {
	return &Audit{dir: dir, agg: map[auditKey]*auditAgg{}, now: time.Now}
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
	if auditFrequent[e.Method] || e.Method == "files" {
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
	if e.Err != "" {
		rec["err"] = e.Err
	}
	if werr := a.writeLocked(rec); werr != nil {
		return werr
	}
	return err
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
	b, _ := json.Marshal(rec)
	p := a.path()
	if st, err := os.Stat(p); err == nil && st.Size() > auditMaxSize {
		_ = os.Rename(p, p+".1")
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err == nil {
		_, err = f.Write(append(b, '\n'))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
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
