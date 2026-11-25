package desktop

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// #250 (LOT-1) — réglage du modèle et de l'effort d'un teammate depuis la carte TEAM : méthodes clientInfo et
// teamAgentSet, TeamRow, teamHead.modelEfforts. Écrit depuis contracts/desktop-dispatch.md « v6.9.0 » (P1 : refus en
// accès distant, côté serveur). Projet factice (sandbox) ; faux jetons seulement ; jamais de vrai projet.
// Réutilise web227/sess227/post227/result227 (reveal227_test.go) pour l'accès web local et distant.

const refusDistant250 = "réglage réservé à la fenêtre native"

func fix250(t *testing.T) (*App, *recorder, string) {
	t.Helper()
	ps := sandbox(t, "", "alpha")
	proj := ps[0]
	agents := filepath.Join(proj, ".claude", "agents")
	if err := os.MkdirAll(agents, 0o755); err != nil {
		t.Fatal(err)
	}
	w := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w(filepath.Join(proj, ".claude", "project-config.json"),
		`{"agents":{"registry":[{"name":"implementation-planner","tag":"v5.16.0"},{"name":"dev-backend","tag":"v5.16.0"},{"name":"teamleader","tag":"v5.16.0"},{"name":"cdp","tag":"v5.16.0"}]}}`)
	for _, n := range []string{"implementation-planner", "dev-backend", "teamleader"} {
		w(filepath.Join(agents, n+".template.md"), "---\nname: "+n+"\nmodel: sonnet\neffort: medium\n---\n\n# "+n+"\n")
	}
	w(filepath.Join(agents, "cdp.template.md"), "# CDP\n")
	r := &recorder{}
	return newApp(t, r.emit), r, proj
}

func snap250(t *testing.T, root string) string {
	t.Helper()
	var names []string
	h := sha256.New()
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		b, _ := os.ReadFile(p)
		rel, _ := filepath.Rel(root, p)
		names = append(names, rel)
		fmt.Fprintf(h, "%s\x00%x\n", rel, sha256.Sum256(b))
		return nil
	})
	sort.Strings(names)
	return fmt.Sprintf("%x|%s", h.Sum(nil), strings.Join(names, ","))
}

func companion250(proj, agent string) string {
	return filepath.Join(proj, ".claude", "agents", agent+".md")
}

func read250(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("lecture %s : %v", p, err)
	}
	return string(b)
}

// native250 : appel par la fenêtre native (Dispatch) ; une erreur Go de refus est rendue comme {ok:false, msg}.
func native250(t *testing.T, a *App, method string, args map[string]any) map[string]any {
	t.Helper()
	b, _ := json.Marshal(args)
	res, err := a.Dispatch(method, b)
	if err != nil {
		return map[string]any{"ok": false, "msg": err.Error()}
	}
	out, _ := json.Marshal(res)
	var v map[string]any
	_ = json.Unmarshal(out, &v)
	return v
}

func set250(proj, agent string, kv ...string) map[string]any {
	m := map[string]any{"path": proj, "agent": agent}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return m
}

// cardsWhen250 : sélectionne le projet puis attend un évènement cards (complété en tâche de fond) qui satisfait cond.
func cardsWhen250(t *testing.T, a *App, r *recorder, proj string, cond func(map[string]any) bool) map[string]any {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"path": proj})
	_, _ = a.Dispatch("select", b)
	return r.wait(t, 20*time.Second, "cards du projet", func(e map[string]any) bool {
		return e["_name"] == "cards" && e["path"] == proj && cond(e)
	})
}

func rowIn250(t *testing.T, v any, name string) map[string]any {
	t.Helper()
	out, _ := json.Marshal(v)
	var c map[string]any
	_ = json.Unmarshal(out, &c)
	rows, _ := c["team"].([]any)
	for _, r := range rows {
		if m, ok := r.(map[string]any); ok && m["name"] == name {
			return m
		}
	}
	return nil
}

// ── clientInfo ────────────────────────────────────────────────────────────

func TestClientInfoNativeEtWebLocal250(t *testing.T) {
	a, _, _ := fix250(t)
	if res := native250(t, a, "clientInfo", map[string]any{}); res["remote"] != false {
		t.Errorf("clientInfo (fenêtre native) : %v, attendu {remote:false}", res)
	}
	srv, _ := web227(t, a, "", true)
	sess := sess227(t, srv, "")
	if sess == nil {
		t.Fatal("session web locale non obtenue")
	}
	resp, out := post227(t, srv, "", sess, "clientInfo", map[string]any{})
	if r := result227(out); resp.StatusCode != 200 || r["remote"] != false {
		t.Errorf("clientInfo (navigateur en boucle locale) : %d %s, attendu {remote:false}", resp.StatusCode, out)
	}
}

func TestClientInfoWebDistant250(t *testing.T) {
	a, _, _ := fix250(t)
	for name, c := range map[string]struct {
		public string
		strict bool
		host   string
	}{
		"adresse publique (ingress)": {"https://claude.example.org", true, "claude.example.org"},
		"hôte du réseau local":       {"", false, "192.168.1.50:8765"},
	} {
		srv, _ := web227(t, a, c.public, c.strict)
		sess := sess227(t, srv, c.host)
		if sess == nil {
			t.Fatalf("%s : session non obtenue", name)
		}
		resp, out := post227(t, srv, c.host, sess, "clientInfo", map[string]any{})
		if r := result227(out); resp.StatusCode != 200 || r["remote"] != true {
			t.Errorf("%s : clientInfo = %d %s, attendu {remote:true}", name, resp.StatusCode, out)
		}
	}
}

// ── teamAgentSet : fenêtre native et navigateur en boucle locale ──────────

func TestTeamAgentSetNativeEcritEtRelitLaCarte250(t *testing.T) {
	a, r, proj := fix250(t)
	n := r.count()
	res := native250(t, a, "teamAgentSet", set250(proj, "implementation-planner", "model", "opus", "effort", "max"))
	if res["ok"] != true || res["model"] != "opus" || res["effort"] != "max" || res["modelSrc"] != "companion" || res["effortSrc"] != "companion" ||
		res["file"] != "implementation-planner.md" || res["removed"] != false || res["effortAdjusted"] != false {
		t.Fatalf("résultat : %v", res)
	}
	if got := read250(t, companion250(proj, "implementation-planner")); got != "---\nmodel: opus\neffort: max\n---\n" {
		t.Errorf("compagnon : %q", got)
	}
	// la carte TEAM du projet est renvoyée tout de suite après l'écriture (évènement cards, comme teamToggle)
	ev := r.wait(t, 15*time.Second, "cards après teamAgentSet", func(e map[string]any) bool {
		if e["_name"] != "cards" || e["path"] != proj {
			return false
		}
		row := rowIn250(t, e, "implementation-planner")
		return row != nil && row["model"] == "opus"
	})
	_ = n
	row := rowIn250(t, ev, "implementation-planner")
	if row["effort"] != "max" || row["modelSrc"] != "companion" || row["effortSrc"] != "companion" ||
		row["tplModel"] != "sonnet" || row["tplEffort"] != "medium" || row["editable"] != true || row["agent"] != "implementation-planner" ||
		row["companion"] != true || row["companionFile"] != "implementation-planner.md" {
		t.Errorf("ligne de la carte : %v, attendu model/effort résolus, sources companion, références du template, editable, agent, compagnon présent (le « + » apparaît tout de suite : pas de cache de 60 s)", row)
	}
}

// Retour au template par « default » : clé retirée, compagnon supprimé (removed), ligne en source « template ».
func TestTeamAgentSetDefautSupprimeLeCompagnon250(t *testing.T) {
	a, r, proj := fix250(t)
	native250(t, a, "teamAgentSet", set250(proj, "dev-backend", "model", "opus"))
	res := native250(t, a, "teamAgentSet", set250(proj, "dev-backend", "model", "default", "effort", "default"))
	if res["ok"] != true || res["removed"] != true || res["modelSrc"] != "template" || res["effortSrc"] != "template" ||
		res["model"] != "sonnet" || res["effort"] != "medium" {
		t.Errorf("résultat : %v", res)
	}
	if _, err := os.Stat(companion250(proj, "dev-backend")); err == nil {
		t.Error("le compagnon aurait dû être supprimé (P5)")
	}
	ev := cardsWhen250(t, a, r, proj, func(e map[string]any) bool {
		row := rowIn250(t, e, "dev-backend")
		return row != nil && row["modelSrc"] == "template"
	})
	row := rowIn250(t, ev, "dev-backend")
	if row["effortSrc"] != "template" || row["companion"] == true || row["companionFile"] != nil && row["companionFile"] != "" {
		t.Errorf("ligne après retrait : %v, attendu sources « template », plus de compagnon (le « + » disparaît)", row)
	}
}

// Refus du serveur : agent hors registre, nom invalide, valeur hors liste, entrée non éditable, chemin hors projet.
func TestTeamAgentSetRefusSansEcriture250(t *testing.T) {
	a, _, proj := fix250(t)
	before := snap250(t, proj)
	cases := map[string]map[string]any{
		"hors registre":     set250(proj, "inconnu", "model", "opus"),
		"traversée":         set250(proj, "../x", "model", "opus"),
		"séparateur":        set250(proj, "a/b", "model", "opus"),
		"deux points":       set250(proj, "..", "model", "opus"),
		"cdp non éditable":  set250(proj, "cdp", "model", "opus"),
		"modèle hors liste": set250(proj, "dev-backend", "model", "gpt-5"),
		"effort hors liste": set250(proj, "dev-backend", "effort", "ultra"),
		"modèle vide":       set250(proj, "dev-backend", "model", ""),
		"modèle null":       {"path": proj, "agent": "dev-backend", "model": nil},
		"agent absent":      {"path": proj, "model": "opus"},
	}
	for name, args := range cases {
		res := native250(t, a, "teamAgentSet", args)
		if res["ok"] == true {
			t.Errorf("%s : refus attendu, obtenu %v", name, res)
		}
		if m, _ := res["msg"].(string); m == "" {
			t.Errorf("%s : message de refus absent : %v", name, res)
		}
	}
	// chemin hors des projets de la fenêtre
	other := t.TempDir()
	_ = os.MkdirAll(filepath.Join(other, ".claude", "agents"), 0o755)
	otherBefore := snap250(t, other)
	if res := native250(t, a, "teamAgentSet", set250(other, "dev-backend", "model", "opus")); res["ok"] == true {
		t.Errorf("projet hors liste : refus attendu, obtenu %v", res)
	}
	if snap250(t, other) != otherBefore {
		t.Error("écriture dans un dossier qui n'est pas un projet de la fenêtre")
	}
	if snap250(t, proj) != before {
		t.Error("fichiers modifiés malgré les refus")
	}
}

// Effort incompatible avec le modèle résultant : refusé par le serveur (compagnon portant déjà un identifiant complet).
func TestTeamAgentSetEffortIncompatibleRefuse250(t *testing.T) {
	a, _, proj := fix250(t)
	if err := os.WriteFile(companion250(proj, "dev-backend"), []byte("---\nmodel: claude-opus-4-5\n---\ncorps\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := snap250(t, proj)
	res := native250(t, a, "teamAgentSet", set250(proj, "dev-backend", "effort", "xhigh"))
	if res["ok"] == true {
		t.Fatalf("effort xhigh sur claude-opus-4-5 : refus attendu, obtenu %v", res)
	}
	if m, _ := res["msg"].(string); !strings.Contains(m, "non pris en charge") {
		t.Errorf("message : %q, attendu « effort xhigh non pris en charge par claude-opus-4-5 »", m)
	}
	if snap250(t, proj) != before {
		t.Error("écriture malgré le refus")
	}
	if res := native250(t, a, "teamAgentSet", set250(proj, "dev-backend", "effort", "default")); res["ok"] != true {
		t.Errorf("default jamais refusé par la table : %v", res)
	}
}

// Teamleader : teamleader.md et settings.json (valeurs résolues), note qui rappelle /model et /effort.
func TestTeamAgentSetTeamleaderEcritSettings250(t *testing.T) {
	a, _, proj := fix250(t)
	res := native250(t, a, "teamAgentSet", set250(proj, "teamleader", "model", "opus"))
	if res["ok"] != true {
		t.Fatalf("résultat : %v", res)
	}
	st, _ := res["settings"].(map[string]any)
	if st["file"] != ".claude/settings.json" || st["model"] != "opus" || st["effortLevel"] != "medium" {
		t.Errorf("settings : %v", st)
	}
	if got := read250(t, filepath.Join(proj, ".claude", "settings.json")); got != "{\n  \"model\": \"opus\",\n  \"effortLevel\": \"medium\"\n}\n" {
		t.Errorf("settings.json : %q", got)
	}
	if n, _ := res["note"].(string); !strings.Contains(n, "/model") || !strings.Contains(n, "/effort") {
		t.Errorf("note : %q, attendu le rappel de /model et /effort", n)
	}
	// settings.json invalide : rien d'écrit
	bad := filepath.Join(proj, ".claude", "settings.json")
	_ = os.WriteFile(bad, []byte("{ pas du json"), 0o644)
	_ = os.Remove(companion250(proj, "teamleader"))
	before := snap250(t, proj)
	res = native250(t, a, "teamAgentSet", set250(proj, "teamleader", "model", "haiku"))
	if res["ok"] == true || !strings.Contains(fmt.Sprint(res["msg"]), "settings.json illisible") {
		t.Errorf("settings.json invalide : %v, attendu un refus « settings.json illisible : … »", res)
	}
	if snap250(t, proj) != before {
		t.Error("écriture malgré settings.json invalide")
	}
}

// Navigateur en boucle locale : autorisé, comme la fenêtre native.
func TestTeamAgentSetWebLocalAutorise250(t *testing.T) {
	a, _, proj := fix250(t)
	srv, _ := web227(t, a, "", true)
	sess := sess227(t, srv, "")
	if sess == nil {
		t.Fatal("session web locale non obtenue")
	}
	resp, out := post227(t, srv, "", sess, "teamAgentSet", set250(proj, "dev-backend", "model", "haiku"))
	if r := result227(out); resp.StatusCode != 200 || r["ok"] != true || r["model"] != "haiku" {
		t.Fatalf("teamAgentSet en boucle locale : %d %s", resp.StatusCode, out)
	}
	if got := read250(t, companion250(proj, "dev-backend")); got != "---\nmodel: haiku\n---\n" {
		t.Errorf("compagnon : %q", got)
	}
}

// ── teamAgentSet : accès distant refusé côté serveur (P1) — critical ──────

func TestTeamAgentSetWebDistantRefuse250(t *testing.T) {
	a, _, proj := fix250(t)
	before := snap250(t, proj)
	for name, c := range map[string]struct {
		public string
		strict bool
		host   string
	}{
		"adresse publique (proxy inverse)": {"https://claude.example.org", true, "claude.example.org"},
		"hôte du réseau local":             {"", false, "192.168.1.50:8765"},
		"nom d'hôte quelconque":            {"", false, "launcher.interne:8765"},
	} {
		srv, _ := web227(t, a, c.public, c.strict)
		sess := sess227(t, srv, c.host)
		if sess == nil {
			t.Fatalf("%s : session non obtenue", name)
		}
		for _, args := range []map[string]any{
			set250(proj, "dev-backend", "model", "opus"),
			set250(proj, "teamleader", "model", "opus", "effort", "max"),
			set250(proj, "dev-backend", "model", "default"),
		} {
			resp, out := post227(t, srv, c.host, sess, "teamAgentSet", args)
			r := result227(out)
			if r["remote"] != true || r["ok"] == true || !strings.Contains(out, refusDistant250) {
				t.Errorf("%s : %d %s, attendu {ok:false, remote:true, msg:« %s »}", name, resp.StatusCode, out, refusDistant250)
			}
		}
		if snap250(t, proj) != before {
			t.Fatalf("%s : fichiers écrits par un accès distant", name)
		}
	}
}

// Passage par un proxy depuis la boucle locale (X-Forwarded-*, Forwarded, X-Real-IP) : distant aussi ; rien n'est écrit.
func TestTeamAgentSetWebProxyRefuse250(t *testing.T) {
	a, _, proj := fix250(t)
	srv, _ := web227(t, a, "", true)
	sess := sess227(t, srv, "")
	if sess == nil {
		t.Fatal("session web locale non obtenue")
	}
	before := snap250(t, proj)
	for _, h := range [][2]string{
		{"X-Forwarded-For", "203.0.113.9"},
		{"X-Forwarded-Host", "claude.example.org"},
		{"X-Forwarded-Proto", "https"},
		{"Forwarded", "for=203.0.113.9;proto=https"},
		{"X-Real-IP", "203.0.113.9"},
	} {
		body, _ := json.Marshal(map[string]any{"method": "teamAgentSet", "args": set250(proj, "dev-backend", "model", "opus")})
		req, _ := http.NewRequest("POST", srv.URL+"/api/call", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(h[0], h[1])
		req.AddCookie(sess)
		resp, err := webClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if r := result227(string(b)); r["remote"] != true || r["ok"] == true {
			t.Errorf("%s : %d %s, attendu un refus {remote:true}", h[0], resp.StatusCode, b)
		}
		// clientInfo suit la même règle
		req2, _ := http.NewRequest("POST", srv.URL+"/api/call", strings.NewReader(`{"method":"clientInfo","args":{}}`))
		req2.Header.Set("Content-Type", "application/json")
		req2.Header.Set(h[0], h[1])
		req2.AddCookie(sess)
		resp2, err := webClient.Do(req2)
		if err != nil {
			t.Fatal(err)
		}
		b2, _ := io.ReadAll(resp2.Body)
		resp2.Body.Close()
		if r := result227(string(b2)); r["remote"] != true {
			t.Errorf("clientInfo avec %s : %s, attendu {remote:true}", h[0], b2)
		}
	}
	if snap250(t, proj) != before {
		t.Error("fichiers écrits malgré le passage par un proxy")
	}
}

// DispatchWeb(remote=true) : refus avant toute lecture ou écriture, même avec des arguments invalides (le refus prime).
func TestDispatchWebRemoteRefuseAvantTout250(t *testing.T) {
	a, _, proj := fix250(t)
	before := snap250(t, proj)
	for _, args := range []map[string]any{
		set250(proj, "dev-backend", "model", "opus"),
		set250(proj, "inconnu", "model", "gpt"),
		{"path": "/nulle/part", "agent": "x"},
	} {
		b, _ := json.Marshal(args)
		res, err := a.DispatchWeb("203.0.113.9:1234", true, "sid", "teamAgentSet", b)
		out, _ := json.Marshal(res)
		var m map[string]any
		_ = json.Unmarshal(out, &m)
		if err == nil && (m["remote"] != true || m["ok"] == true) {
			t.Errorf("DispatchWeb remote : %s, attendu {ok:false, remote:true}", out)
		}
	}
	if snap250(t, proj) != before {
		t.Error("fichiers écrits en accès distant")
	}
	// remote=false : écrit
	b, _ := json.Marshal(set250(proj, "dev-backend", "model", "opus"))
	res, err := a.DispatchWeb("127.0.0.1:1234", false, "sid", "teamAgentSet", b)
	out, _ := json.Marshal(res)
	if err != nil || !strings.Contains(string(out), `"ok":true`) {
		t.Errorf("DispatchWeb local : %v %s", err, out)
	}
}

// ── TeamRow et teamHead.modelEfforts ──────────────────────────────────────

func TestTeamRowChampsDuContrat250(t *testing.T) {
	a, r, proj := fix250(t)
	if err := os.WriteFile(companion250(proj, "dev-backend"), []byte("---\nmodel: opus\n---\nX\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ev := cardsWhen250(t, a, r, proj, func(e map[string]any) bool { return rowIn250(t, e, "dev-backend") != nil })
	row := rowIn250(t, ev, "dev-backend")
	for k, want := range map[string]any{"agent": "dev-backend", "model": "opus", "effort": "medium", "modelSrc": "companion", "effortSrc": "template",
		"tplModel": "sonnet", "tplEffort": "medium", "editable": true} {
		if row[k] != want {
			t.Errorf("TeamRow.%s = %v, attendu %v", k, row[k], want)
		}
	}
	if _, running := row["running"]; running {
		t.Errorf("running présent pour un teammate non démarré : %v", row["running"])
	}
	if p := rowIn250(t, ev, "implementation-planner"); p == nil || p["modelSrc"] != "template" || p["model"] != "sonnet" {
		t.Errorf("planner sans compagnon : %v, attendu modelSrc « template » (état « Défaut »)", p)
	}
	if c := rowIn250(t, ev, "cdp"); c != nil && c["editable"] == true {
		t.Errorf("cdp : editable doit être faux (%v)", c)
	}
}

// teamHead.modelEfforts : la table unique de core, avec « * » pour un modèle inconnu.
func TestTeamHeadModelEfforts250(t *testing.T) {
	a, r, proj := fix250(t)
	ev := cardsWhen250(t, a, r, proj, func(e map[string]any) bool {
		th, _ := e["teamHead"].(map[string]any)
		return th["modelEfforts"] != nil
	})
	out, _ := json.Marshal(ev)
	var c struct {
		TeamHead struct {
			ModelEfforts map[string][]string `json:"modelEfforts"`
		} `json:"teamHead"`
	}
	_ = json.Unmarshal(out, &c)
	me := c.TeamHead.ModelEfforts
	if len(me) == 0 {
		t.Fatalf("teamHead.modelEfforts absent : %s", out)
	}
	for _, m := range []string{"opus", "sonnet", "haiku", "fable"} {
		want, _ := core.ModelEfforts(m)
		if fmt.Sprint(me[m]) != fmt.Sprint(want) {
			t.Errorf("modelEfforts[%s] = %v, table de core : %v", m, me[m], want)
		}
	}
	if want, _ := core.ModelEfforts("modele-inconnu"); fmt.Sprint(me["*"]) != fmt.Sprint(want) {
		t.Errorf("modelEfforts[*] = %v, attendu le repli de core %v", me["*"], want)
	}
}

// ── frontend : simulation node du menu (fonctions pures de app.js), testdata/team_menu.js ──

func TestTeamMenuSimulation250(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node absent : simulation ignorée")
	}
	wd, _ := os.Getwd()
	out, err := exec.Command(node, filepath.Join(wd, "testdata", "team_menu.js"), filepath.Join(wd, "frontend", "app.js")).CombinedOutput()
	if err != nil {
		t.Fatalf("simulation du menu modèle · effort : %v\n%s", err, out)
	}
}

// Le client appelle clientInfo au démarrage et teamAgentSet ; le style du menu et de la cellule existe.
func TestTeamMenuBranchementEtStyle250(t *testing.T) {
	js := asset(t, "app.js")
	for _, want := range []string{"'clientInfo'", "'teamAgentSet'"} {
		if !strings.Contains(js, want) {
			t.Errorf("app.js n'appelle pas %s", want)
		}
	}
	css := asset(t, "style.css")
	for _, sel := range []string{".teammenu", ".nx"} {
		if !strings.Contains(css, sel) {
			t.Errorf("style.css : règle %s absente", sel)
		}
	}
}
