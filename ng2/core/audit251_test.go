package core

// Tests de spécification #251 (v7.0.0) — contracts/auth.md §5 : journal d'audit audit.jsonl (une entrée par appel, agrégation
// des méthodes fréquentes par minute, rotation à 10 Mio, échec d'écriture). Journal dans un dossier temporaire ; horloge
// injectée (champ now). Écrits depuis le contrat avant le code.

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

var t251 = time.Date(2026, 12, 1, 10, 0, 30, 123_000_000, time.UTC)

func audit251(t *testing.T) (*Audit, string, *time.Time) {
	t.Helper()
	dir := t.TempDir()
	a := NewAudit(dir)
	clock := t251
	a.now = func() time.Time { return clock }
	return a, filepath.Join(dir, "audit.jsonl"), &clock
}

func lines251(t *testing.T, p string) []map[string]any {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	defer f.Close()
	var out []map[string]any
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("ligne non JSON : %q (%v)", sc.Text(), err)
		}
		out = append(out, m)
	}
	return out
}

// Une entrée par appel : ts, user, role, src, method, project, ok ; ni argument, ni autre champ.
func TestAudit251OneEntryPerCall(t *testing.T) {
	a, p, _ := audit251(t)
	if err := a.Record(AuditEntry{User: "ccoupel", Role: "admin", Src: "10.42.0.7 (X-Forwarded-For: 192.168.1.20)",
		Method: "configSave", Project: "cl-claude-launcher", OK: true}); err != nil {
		t.Fatal(err)
	}
	if err := a.Record(AuditEntry{User: "alice", Role: "user", Src: "10.42.0.7", Method: "relaunch", OK: false, Err: "forbidden"}); err != nil {
		t.Fatal(err)
	}
	ls := lines251(t, p)
	if len(ls) != 2 {
		t.Fatalf("%d lignes, attendu 2 : %v", len(ls), ls)
	}
	e := ls[0]
	if e["ts"] != "2026-12-01T10:00:30.123Z" || e["user"] != "ccoupel" || e["role"] != "admin" || e["method"] != "configSave" ||
		e["project"] != "cl-claude-launcher" || e["ok"] != true || e["src"] != "10.42.0.7 (X-Forwarded-For: 192.168.1.20)" {
		t.Errorf("entrée = %v", e)
	}
	if _, has := e["err"]; has {
		t.Errorf("err présent sur un succès : %v", e)
	}
	allowed := map[string]bool{"ts": true, "user": true, "role": true, "src": true, "method": true, "project": true, "ok": true, "err": true}
	for _, l := range ls {
		for k := range l {
			if !allowed[k] {
				t.Errorf("champ %q hors contrat : %v", k, l)
			}
		}
	}
	f := ls[1]
	if f["ok"] != false || f["err"] != "forbidden" || f["user"] != "alice" {
		t.Errorf("refus = %v", f)
	}
	if _, has := f["project"]; has {
		t.Errorf("project présent alors que l'appel n'en porte pas : %v", f)
	}
}

// Fichier 0600, ajout (les entrées précédentes restent).
func TestAudit251FileModeAndAppend(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("droits Unix")
	}
	a, p, _ := audit251(t)
	_ = a.Record(AuditEntry{User: "u", Method: "configSave", OK: true})
	a2 := NewAudit(filepath.Dir(p)) // nouveau processus : le journal existant est prolongé
	_ = a2.Record(AuditEntry{User: "u", Method: "configApply", OK: true})
	if st, err := os.Stat(p); err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v (%v), attendu 0600", st.Mode().Perm(), err)
	}
	if n := len(lines251(t, p)); n != 2 {
		t.Errorf("%d lignes après deux écrivains, attendu 2", n)
	}
}

// Méthodes fréquentes : agrégées par (utilisateur, méthode) et par minute — une seule ligne agg avec count et since,
// écrite quand la minute est révolue ; rien d'écrit dans la minute en cours.
func TestAudit251FrequentAggregated(t *testing.T) {
	a, p, clock := audit251(t)
	for i := 0; i < 312; i++ {
		_ = a.Record(AuditEntry{User: "ccoupel", Role: "admin", Method: "termInput", OK: true})
	}
	for i := 0; i < 5; i++ {
		_ = a.Record(AuditEntry{User: "alice", Role: "user", Method: "termInput", OK: true})
	}
	_ = a.Record(AuditEntry{User: "ccoupel", Role: "admin", Method: "termResize", OK: true})
	if ls := lines251(t, p); len(ls) != 0 {
		t.Fatalf("écriture dans la minute en cours : %v", ls)
	}
	*clock = clock.Add(time.Minute) // minute révolue
	if err := a.Flush(false); err != nil {
		t.Fatal(err)
	}
	agg := map[string]map[string]any{}
	for _, l := range lines251(t, p) {
		if l["kind"] != "agg" {
			t.Errorf("entrée non agrégée pour une méthode fréquente : %v", l)
			continue
		}
		agg[l["user"].(string)+"/"+l["method"].(string)] = l
	}
	if len(agg) != 3 {
		t.Fatalf("%d agrégats, attendu 3 (utilisateur × méthode) : %v", len(agg), agg)
	}
	if c := agg["ccoupel/termInput"]["count"]; c != float64(312) {
		t.Errorf("count = %v, attendu 312", c)
	}
	if c := agg["alice/termInput"]["count"]; c != float64(5) {
		t.Errorf("count alice = %v, attendu 5", c)
	}
	if s := agg["ccoupel/termInput"]["since"]; s != "2026-12-01T10:00:00Z" {
		t.Errorf("since = %v, attendu le début de la minute 2026-12-01T10:00:00Z", s)
	}
	// vidés : un second Flush n'écrit plus rien
	_ = a.Flush(true)
	if n := len(lines251(t, p)); n != 3 {
		t.Errorf("%d lignes après second Flush, attendu 3", n)
	}
}

// Une minute suivante : nouvel agrégat ; Flush(all) à l'arrêt écrit même la minute en cours.
func TestAudit251AggregateNextMinuteAndShutdown(t *testing.T) {
	a, p, clock := audit251(t)
	_ = a.Record(AuditEntry{User: "u", Method: "select", OK: true})
	*clock = clock.Add(time.Minute)
	_ = a.Record(AuditEntry{User: "u", Method: "select", OK: true}) // déclenche l'écriture de la minute révolue
	if ls := lines251(t, p); len(ls) != 1 || ls[0]["count"] != float64(1) {
		t.Fatalf("minute révolue non écrite à l'appel suivant : %v", ls)
	}
	if err := a.Flush(true); err != nil {
		t.Fatal(err)
	}
	if ls := lines251(t, p); len(ls) != 2 || ls[1]["since"] != "2026-12-01T10:01:00Z" {
		t.Errorf("Flush(all) : %v", ls)
	}
}

// Lectures /files : agrégées sous method « files » ; flux : une entrée à l'ouverture (method « events »), non agrégée.
func TestAudit251FilesAggregatedEventsSingle(t *testing.T) {
	a, p, clock := audit251(t)
	for i := 0; i < 40; i++ {
		_ = a.Record(AuditEntry{User: "u", Role: "user", Method: "files", OK: true})
	}
	_ = a.Record(AuditEntry{User: "u", Role: "user", Method: "events", OK: true})
	*clock = clock.Add(time.Minute)
	_ = a.Flush(false)
	var files, events int
	for _, l := range lines251(t, p) {
		switch {
		case l["method"] == "files" && l["kind"] == "agg" && l["count"] == float64(40):
			files++
		case l["method"] == "events" && l["kind"] == nil && l["ok"] == true:
			events++
		default:
			t.Errorf("ligne inattendue : %v", l)
		}
	}
	if files != 1 || events != 1 {
		t.Errorf("files=%d events=%d, attendu 1 et 1", files, events)
	}
}

// Rotation : au-delà de 10 Mio, audit.jsonl → audit.jsonl.1 (une seule génération gardée), nouveau fichier ensuite.
func TestAudit251Rotation(t *testing.T) {
	a, p, _ := audit251(t)
	big := strings.Repeat("x", 1<<20) + "\n"
	fill := func() {
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 11; i++ { // 11 Mio > 10 Mio
			_, _ = f.WriteString(big)
		}
		f.Close()
	}
	fill()
	if err := a.Record(AuditEntry{User: "u", Method: "configSave", OK: true}); err != nil {
		t.Fatal(err)
	}
	st1, err := os.Stat(p + ".1")
	if err != nil || st1.Size() < 10<<20 {
		t.Fatalf("audit.jsonl.1 absent ou trop petit : %v", err)
	}
	ls := lines251(t, p)
	if len(ls) != 1 || ls[0]["method"] != "configSave" {
		t.Errorf("nouveau journal : %v", ls)
	}
	if st, _ := os.Stat(p); st.Size() > 1<<10 {
		t.Errorf("le nouveau journal contient l'ancien (%d octets)", st.Size())
	}
	// seconde rotation : .1 est remplacé, aucune génération .2
	fill()
	_ = a.Record(AuditEntry{User: "u", Method: "configApply", OK: true})
	if _, err := os.Stat(p + ".2"); err == nil {
		t.Error("audit.jsonl.2 créé : une seule génération gardée")
	}
	if _, err := os.Stat(p + ".1"); err != nil {
		t.Error("audit.jsonl.1 perdu")
	}
}

// Échec d'écriture : Check et Record rendent « journal d'audit inaccessible » (R1 refusées par l'appelant) ; les écritures
// suivantes reprennent quand le journal redevient accessible ; un seul avertissement (pas de panique, pas de boucle).
func TestAudit251WriteFailure(t *testing.T) {
	a, p, _ := audit251(t)
	if err := os.Mkdir(p, 0o755); err != nil { // un dossier à la place du fichier : ouverture impossible
		t.Fatal(err)
	}
	if err := a.Check(); err == nil || !strings.Contains(err.Error(), "journal d'audit inaccessible") {
		t.Errorf("Check = %v, attendu « journal d'audit inaccessible »", err)
	}
	for i := 0; i < 3; i++ {
		if err := a.Record(AuditEntry{User: "u", Method: "configSave", OK: true}); err == nil || !strings.Contains(err.Error(), "journal d'audit inaccessible") {
			t.Errorf("Record = %v", err)
		}
	}
	// agrégat non écrit : conservé, écrit quand le journal revient
	_ = a.Record(AuditEntry{User: "u", Method: "termInput", OK: true})
	_ = os.Remove(p)
	if err := a.Check(); err != nil {
		t.Fatalf("Check après rétablissement : %v", err)
	}
	if err := a.Record(AuditEntry{User: "u", Method: "configSave", OK: true}); err != nil {
		t.Errorf("Record après rétablissement : %v", err)
	}
}

// Check n'écrit aucune entrée.
func TestAudit251CheckWritesNothing(t *testing.T) {
	a, p, _ := audit251(t)
	if err := a.Check(); err != nil {
		t.Fatal(err)
	}
	if n := len(lines251(t, p)); n != 0 {
		t.Errorf("Check a écrit %d lignes", n)
	}
}

// Jamais d'argument, de valeur, de chemin ni de mot de passe dans une ligne : seuls les champs du contrat.
func TestAudit251NoSecretLeak(t *testing.T) {
	a, p, _ := audit251(t)
	_ = a.Record(AuditEntry{User: "admin@local", Role: "admin", Src: "10.0.0.1", Method: "backendSecretSet", Project: "cl-x", OK: true})
	b, _ := os.ReadFile(p)
	for _, bad := range []string{"password", "passwd", "secret\":", "value", "args", "/data/", "token"} {
		if strings.Contains(strings.ToLower(string(b)), bad) {
			t.Errorf("« %s » dans le journal : %s", bad, b)
		}
	}
	if !strings.Contains(string(b), `"user":"admin@local"`) {
		t.Errorf("user admin@local absent : %s", b)
	}
}
