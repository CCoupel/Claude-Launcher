package core

// Tests de spécification #251 (v7.0.0) — contracts/auth.md §5 : journal d'audit audit.jsonl (une entrée par appel, agrégation
// des méthodes fréquentes par minute, rotation à 10 Mio, échec d'écriture). Journal dans un dossier temporaire ; horloge
// injectée (champ now). Écrits depuis le contrat avant le code.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
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

// Rotation (addendum 2, MAJ-4) : au-delà de 10 Mio, audit.jsonl → audit.jsonl.1, … jusqu'à 5 générations (.5) ; aucune .6.
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
	methods := []string{"configSave", "configApply", "relaunch", "backendSecretSet", "backendSecretDelete", "backendKnownHostsSet", "login"}
	for i, m := range methods { // 7 rotations : les 5 dernières générations seulement
		fill()
		if err := a.Record(AuditEntry{User: "u", Method: m, OK: true}); err != nil {
			t.Fatal(err)
		}
		ls := lines251(t, p)
		if len(ls) != 1 || ls[0]["method"] != m {
			t.Fatalf("rotation %d : nouveau journal = %v", i+1, ls)
		}
		if st, _ := os.Stat(p); st.Size() > 1<<10 {
			t.Errorf("rotation %d : le nouveau journal contient l'ancien (%d octets)", i+1, st.Size())
		}
	}
	for g := 1; g <= 5; g++ {
		if st, err := os.Stat(p + "." + strconv.Itoa(g)); err != nil || st.Size() < 10<<20 {
			t.Errorf("audit.jsonl.%d absent ou trop petit : %v", g, err)
		}
	}
	if _, err := os.Stat(p + ".6"); err == nil {
		t.Error("audit.jsonl.6 créé : cinq générations gardées au plus")
	}
}

// Au-delà de 60 appels par minute et par utilisateur, toute méthode est agrégée (entrée agg) ; les appels R1 ne le sont JAMAIS.
func TestAudit251OverSixtyPerMinuteAggregated(t *testing.T) {
	a, p, clock := audit251(t)
	for i := 0; i < 100; i++ {
		_ = a.Record(AuditEntry{User: "bob", Role: "user", Method: "refresh", OK: true})
		_ = a.Record(AuditEntry{User: "alice", Role: "user", Method: "refresh", OK: true}) // autre utilisateur : son propre quota
	}
	for i := 0; i < 100; i++ { // succès R1 : jamais agrégés
		_ = a.Record(AuditEntry{User: "bob", Role: "admin", Method: "backendSecretSet", OK: true, Target: "backend-a"})
	}
	*clock = clock.Add(time.Minute)
	_ = a.Flush(false)
	indiv := map[string]int{}
	var aggBob float64
	for _, l := range lines251(t, p) {
		key := l["user"].(string) + "/" + l["method"].(string)
		if l["kind"] == "agg" {
			if key == "bob/refresh" {
				aggBob += l["count"].(float64)
			}
			continue
		}
		indiv[key]++
	}
	if indiv["bob/refresh"] != 60 || indiv["alice/refresh"] != 60 {
		t.Errorf("entrées individuelles refresh : bob %d, alice %d, attendu 60 chacun", indiv["bob/refresh"], indiv["alice/refresh"])
	}
	if aggBob != 40 {
		t.Errorf("agrégat bob/refresh : count %v, attendu 40 (100 − 60)", aggBob)
	}
	if indiv["bob/backendSecretSet"] != 100 {
		t.Errorf("succès R1 : %d entrées, attendu 100 (jamais agrégés)", indiv["bob/backendSecretSet"])
	}
}

// target (méthodes R1 et refus R1) : identifiant court écrit tel quel ; absent sinon.
func TestAudit251TargetField(t *testing.T) {
	a, p, _ := audit251(t)
	_ = a.Record(AuditEntry{User: "u", Role: "admin", Method: "configSave", Target: "backends.yaml", OK: true})
	_ = a.Record(AuditEntry{User: "u", Role: "admin", Method: "refresh", OK: true})
	ls := lines251(t, p)
	if len(ls) != 2 || ls[0]["target"] != "backends.yaml" {
		t.Fatalf("lignes = %v", ls)
	}
	if _, has := ls[1]["target"]; has {
		t.Errorf("target sur un appel qui n'en porte pas : %v", ls[1])
	}
}

// Écriture par encoding/json : une valeur hostile (guillemets, retours à la ligne, caractères de contrôle) reste UNE ligne
// JSON valide, sans champ ajouté.
func TestAudit251JSONEscaping(t *testing.T) {
	a, p, _ := audit251(t)
	hostile := "x\"},{\"ok\":true,\"role\":\"admin\"\n{\"forged\":1}\r\x00\x1b[31m"
	_ = a.Record(AuditEntry{User: hostile, Role: "user", Src: hostile, Method: "configSave", Target: hostile, OK: false, Err: "forbidden"})
	b, _ := os.ReadFile(p)
	if n := strings.Count(strings.TrimRight(string(b), "\n"), "\n"); n != 0 {
		t.Fatalf("%d sauts de ligne dans l'entrée : %q", n, b)
	}
	ls := lines251(t, p)
	if len(ls) != 1 || ls[0]["role"] != "user" || ls[0]["ok"] != false || ls[0]["forged"] != nil {
		t.Errorf("entrée forgée : %v", ls)
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

// Sortie standard (MAJ-4) : chaque ligne écrite dans le fichier l'est aussi, préfixée « audit », sur la sortie du conteneur
// (collecte Kubernetes : non effaçable par l'application) — y compris quand le fichier est inaccessible.
func TestAudit251StdoutMirror(t *testing.T) {
	a, p, clock := audit251(t)
	var out bytes.Buffer
	a.out = &out
	_ = a.Record(AuditEntry{User: "ccoupel", Role: "admin", Method: "configSave", Target: "backends.yaml", OK: true})
	for i := 0; i < 3; i++ {
		_ = a.Record(AuditEntry{User: "ccoupel", Role: "admin", Method: "termInput", OK: true})
	}
	*clock = clock.Add(time.Minute)
	_ = a.Flush(false)
	var file []string
	b, _ := os.ReadFile(p)
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		file = append(file, l)
	}
	got := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(got) != len(file) || len(file) != 2 {
		t.Fatalf("sortie standard %d lignes, fichier %d (attendu 2 et 2) :\n%s", len(got), len(file), out.String())
	}
	for i, l := range got {
		if l != "audit "+file[i] {
			t.Errorf("ligne %d : %q, attendu « audit » + la ligne du fichier %q", i, l, file[i])
		}
	}
	// fichier inaccessible : la sortie standard reste écrite, les méthodes R1 sont refusées par l'appelant (Check)
	_ = os.Remove(p)
	if err := os.Mkdir(p, 0o755); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	_ = a.Record(AuditEntry{User: "u", Role: "user", Method: "refresh", OK: true})
	if !strings.Contains(out.String(), `"method":"refresh"`) {
		t.Errorf("fichier en échec : sortie standard non écrite : %q", out.String())
	}
	if err := a.Check(); err == nil {
		t.Error("Check sans erreur alors que le fichier est inaccessible")
	}
}

// Refus d'identité (401, 403 du launcher) : agrégat par minute kind « deny » (login tronqué à 64 caractères sans caractère
// de contrôle, code, count) ; rien d'autre ; jamais écrit tant que la minute n'est pas révolue.
func TestAudit251DenyAggregated(t *testing.T) {
	a, p, clock := audit251(t)
	long := strings.Repeat("é", 100) + "\x1b[2J"
	for i := 0; i < 4; i++ {
		_ = a.Deny("bob-test", 403)
	}
	_ = a.Deny("", 401)
	_ = a.Deny(long, 403)
	if ls := lines251(t, p); len(ls) != 0 {
		t.Fatalf("écrit dans la minute en cours : %v", ls)
	}
	*clock = clock.Add(time.Minute)
	_ = a.Flush(false)
	by := map[string]map[string]any{}
	for _, l := range lines251(t, p) {
		if l["kind"] != "deny" {
			t.Errorf("entrée inattendue : %v", l)
			continue
		}
		for k := range l {
			switch k {
			case "ts", "kind", "login", "code", "count", "since":
			default:
				t.Errorf("champ %q hors contrat : %v", k, l)
			}
		}
		l2, _ := l["login"].(string)
		by[l2+"/"+fmt.Sprint(l["code"])] = l
	}
	if d := by["bob-test/403"]; d == nil || d["count"] != float64(4) || d["since"] != "2026-12-01T10:00:00Z" {
		t.Errorf("bob-test/403 = %v, attendu count 4 depuis le début de la minute", d)
	}
	if d := by["/401"]; d == nil || d["count"] != float64(1) {
		t.Errorf("identité absente = %v", d)
	}
	for k := range by {
		if strings.Contains(k, "\x1b") {
			t.Errorf("caractère de contrôle dans le login : %q", k)
		}
		if r := []rune(strings.TrimSuffix(k, "/403")); len(r) > 64 && strings.HasSuffix(k, "/403") && k != "bob-test/403" {
			t.Errorf("login de %d caractères, attendu tronqué à 64", len(r))
		}
	}
}

// MAJ-A : les REFUS R1 (ok false, err forbidden) sont soumis au plafond de 60 par minute et par utilisateur — un user ne
// peut pas renouveler le journal à coups de refus quasi gratuits ; les succès R1 ne le sont jamais.
func TestAudit251ForbiddenFloodLimited(t *testing.T) {
	a, p, clock := audit251(t)
	for i := 0; i < 500; i++ {
		_ = a.Record(AuditEntry{User: "mallory", Role: "user", Method: "configSave", Target: "config.conf", OK: false, Err: "forbidden"})
	}
	for i := 0; i < 5; i++ {
		_ = a.Record(AuditEntry{User: "ccoupel", Role: "admin", Method: "configSave", Target: "config.conf", OK: true})
	}
	*clock = clock.Add(time.Minute)
	_ = a.Flush(false)
	var indiv, agg, admin int
	for _, l := range lines251(t, p) {
		switch {
		case l["user"] == "mallory" && l["kind"] == "agg":
			agg += int(l["count"].(float64))
		case l["user"] == "mallory":
			indiv++
		case l["user"] == "ccoupel":
			admin++
		}
	}
	if indiv > 60 {
		t.Errorf("%d refus R1 écrits ligne à ligne, attendu au plus 60 par minute", indiv)
	}
	if agg > 0 && indiv+agg != 500 {
		t.Errorf("agrégat %d + %d individuelles != 500 refus", agg, indiv)
	}
	if admin != 5 {
		t.Errorf("succès R1 de l'admin : %d entrées, attendu 5 (jamais limités)", admin)
	}
	// le plafond de l'un ne limite pas l'autre : un autre utilisateur garde son quota
	_ = a.Record(AuditEntry{User: "alice", Role: "user", Method: "configSave", OK: false, Err: "forbidden"})
	found := false
	for _, l := range lines251(t, p) {
		if l["user"] == "alice" {
			found = true
		}
	}
	if !found {
		t.Error("le refus d'un autre utilisateur n'est pas écrit (quota propre à chaque utilisateur)")
	}
}

// Bornes : method 64, target et project 128 caractères ; méthode inconnue → « unknown » (jamais une clé arbitraire) ; une
// ligne reste petite même avec des champs d'un mégaoctet ; le champ phase (ligne d'intention) est écrit tel quel.
func TestAudit251FieldsBounded(t *testing.T) {
	a, p, clock := audit251(t)
	big := strings.Repeat("é", 1<<20)
	_ = a.Record(AuditEntry{User: "u", Role: "admin", Method: big, OK: true})
	_ = a.Record(AuditEntry{User: "u", Role: "admin", Method: "configSave", Target: big, Project: big, OK: true})
	for i := 0; i < 1000; i++ { // méthodes inconnues distinctes : une seule clé d'agrégat (« unknown »), pas 1000
		_ = a.Record(AuditEntry{User: "u", Role: "user", Method: "zz" + strconv.Itoa(i), OK: false, Err: "forbidden"})
	}
	_ = a.Record(AuditEntry{User: "u", Role: "admin", Method: "configSave", Target: "config.conf", OK: true, Phase: "intent"})
	*clock = clock.Add(time.Minute)
	_ = a.Flush(true)
	st, _ := os.Stat(p)
	if st.Size() > 64<<10 {
		t.Errorf("journal de %d octets après des champs d'un mégaoctet et 1000 méthodes inconnues", st.Size())
	}
	unknown, intent := 0, 0
	for _, l := range lines251(t, p) {
		if m, _ := l["method"].(string); len([]rune(m)) > 64 {
			t.Errorf("method de %d caractères", len([]rune(m)))
		}
		if tg, _ := l["target"].(string); len([]rune(tg)) > 128 {
			t.Errorf("target de %d caractères", len([]rune(tg)))
		}
		if pr, _ := l["project"].(string); len([]rune(pr)) > 128 {
			t.Errorf("project de %d caractères", len([]rune(pr)))
		}
		if l["method"] == "unknown" {
			unknown++
		}
		if strings.HasPrefix(fmt.Sprint(l["method"]), "zz") {
			t.Errorf("méthode inconnue écrite sous son nom : %v", l["method"])
		}
		if l["phase"] == "intent" {
			intent++
		}
	}
	if unknown == 0 {
		t.Error("aucune entrée « unknown »")
	}
	if intent != 1 {
		t.Errorf("%d lignes phase intent, attendu 1", intent)
	}
}
