package desktop

// Correctif cycle 1 du LOT-2 (audit security KO + revue) — tests écrits avant le code (RED attendu, voir handoff) :
// CRIT-1 contournement de R1 par la casse / les doublons des clés JSON, backendTest (champs R1 fournis par le client),
// flux SSE revalidé, audit R1 écrit avant l'exécution, flood de refus, queue de X-Forwarded-For.
// Règles : path (jamais filepath) pour les chemins de pod et d'URL ; aucun vrai projet ; HOME factice.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// ── CRIT-1 : la décision R1 se prend sur la structure EXÉCUTÉE ────────────────────────────────────────────────────

// snapshot : contenu (ou absence) des trois fichiers de configuration globaux ; rien ne doit y être écrit par un user.
func cfgSnapshot(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, f := range []string{"config.conf", "env.yaml", "backends.yaml"} {
		b, err := os.ReadFile(filepath.Join(core.Ng2ConfigDir(), f))
		if err != nil {
			out[f] = "<absent>"
			continue
		}
		out[f] = string(b)
	}
	return out
}

func sameSnapshot(t *testing.T, before, after map[string]string, what string) {
	t.Helper()
	for f, b := range before {
		if after[f] != b {
			t.Errorf("%s : %s modifié par un user (R1 contournée)", what, f)
		}
	}
}

// crit1Payloads : la charge de l'audit (clés en casse mixte, doublons exacts ou à la casse près) pour configSave et configApply.
// conf, env, bk : contenus VALIDES pour config.conf, env.yaml du launcher et backends.yaml (un contenu invalide ne serait pas
// écrit même si la décision R1 était contournée : la charge ne prouverait rien).
func crit1Payloads(conf, env, bk string) map[string]string {
	q := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	c, e, k, benign := q(conf), q(env), q(bk), `"x"`
	return map[string]string{
		"configSave Kind":                   `{"method":"configSave","args":{"kind":"project-env","Kind":"config.conf","path":"/x","content":"x","CONTENT":` + c + `}}`,
		"configSave KIND env.yaml":          `{"method":"configSave","args":{"kind":"project-env","KIND":"env.yaml","path":"/x","content":` + e + `}}`,
		"configSave kind en double":         `{"method":"configSave","args":{"kind":"project-env","kind":"config.conf","path":"/x","content":` + c + `}}`,
		"configSave kind inversé":           `{"method":"configSave","args":{"kind":"config.conf","kind":"project-env","path":"/x","content":` + c + `}}`,
		"configSave content en double":      `{"method":"configSave","args":{"kind":"backends.yaml","content":` + benign + `,"content":` + k + `}}`,
		"configSave CONTENT backends.yaml":  `{"method":"configSave","args":{"kind":"backends.yaml","content":` + q("backends:\n  s: {type: ssh, host: h}\n") + `,"CONTENT":` + k + `}}`,
		"configSave Content backends.yaml":  `{"method":"configSave","args":{"kind":"backends.yaml","content":` + q("backends:\n  s: {type: ssh, host: h}\n") + `,"Content":` + k + `}}`,
		"configSave Path":                   `{"method":"configSave","args":{"kind":"project-env","path":"/x","Path":"/y","content":` + c + `}}`,
		"configSave kind valeur non chaîne": `{"method":"configSave","args":{"kind":["config.conf"],"Kind":"project-env","content":` + c + `}}`,
		"configSave kind avec espace":       `{"method":"configSave","args":{"kind":" config.conf","content":` + c + `}}`,
		"configSave kind en majuscules":     `{"method":"configSave","args":{"kind":"CONFIG.CONF","content":` + c + `}}`,
		"configSave args dans un tableau":   `{"method":"configSave","args":[{"kind":"config.conf","content":` + c + `}]}`,
		"configSave Method":                 `{"method":"configSave","Method":"refresh","args":{"kind":"config.conf","content":` + c + `}}`,
		"configSave args en double":         `{"method":"configSave","args":{"kind":"project-env","path":"/x","content":"x"},"args":{"kind":"config.conf","content":` + c + `}}`,
		"configApply Kind":                  `{"method":"configApply","args":{"kind":"project-env","Kind":"config.conf","path":"/x","changes":[{"path":"launcher_poc","value":true}]}}`,
		"configApply kind en double":        `{"method":"configApply","args":{"kind":"project-env","kind":"hub","changes":[{"path":"backends.k.type","value":"host"}]}}`,
		"configApply Changes backends.yaml": `{"method":"configApply","args":{"kind":"backends.yaml","changes":[],"Changes":[{"path":"backends.s.pod.serviceAccountName","value":"admin"}]}}`,
		"configApply CHANGES en double":     `{"method":"configApply","args":{"kind":"backends.yaml","changes":[],"changes":[{"path":"backends.s.type","value":"host"}]}}`,
	}
}

// crit1Contents : contenus valides et reconnaissables (marqueur « POC-CRIT-1 ») pour les trois fichiers R1.
func crit1Contents() (conf, env, bk string) {
	return core.ConfigTemplate(core.ConfConf) + "\n# POC-CRIT-1\n", core.ConfigTemplate(core.ConfLauncherEnv) + "\n# POC-CRIT-1\n",
		"backends:\n  k: {type: k8s, pod: {serviceAccountName: admin}, volumes: {/a: b}}\n# POC-CRIT-1\n"
}

// App : aucun des chemins du dispatch ne laisse un user écrire un fichier R1 (config.conf, env.yaml, champs R1 de backends.yaml),
// quelle que soit la casse ou la répétition des clés ; aucune entrée de la charge n'est décidée sur un second décodage.
func TestCrit1172UserCannotWriteR1ThroughKeyCase(t *testing.T) {
	a := r1App(t)
	before := cfgSnapshot(t)
	for name, body := range crit1Payloads(crit1Contents()) {
		var env struct {
			Method string          `json:"method"`
			Args   json.RawMessage `json:"args"`
		}
		_ = json.Unmarshal([]byte(body), &env)
		res, err := r1Call(a, core.AccessProxy, r1User, env.Method, string(env.Args))
		if err == nil && res != nil && !forbidden(res) && res["ok"] == true {
			t.Errorf("%s : accepté pour un user : %v", name, res)
		}
		sameSnapshot(t, before, cfgSnapshot(t), name)
	}
	// le mode « none » (sans authentification) est lecture seule pour R1 : mêmes garanties
	for name, body := range crit1Payloads(crit1Contents()) {
		var env struct {
			Method string          `json:"method"`
			Args   json.RawMessage `json:"args"`
		}
		_ = json.Unmarshal([]byte(body), &env)
		_, _ = r1Call(a, core.AccessNone, nil, env.Method, string(env.Args))
		sameSnapshot(t, before, cfgSnapshot(t), "none / "+name)
	}
}

// HTTP de bout en bout : la charge de l'audit est refusée (R1 ou 400) et rien n'est écrit ; l'entrée d'audit porte un refus.
func TestCrit1172PoCOverHTTP(t *testing.T) {
	px172Env(t)
	s := px172Start(t, false)
	before := cfgSnapshot(t)
	for name, body := range crit1Payloads(crit1Contents()) {
		resp, b := s.do("POST", "/api/call", px172Ext, map[string]string{"X-Forwarded-User": "alice", "Content-Type": "application/json"}, body)
		if resp == nil {
			t.Errorf("%s : pas de réponse", name)
			continue
		}
		var v map[string]any
		_ = json.Unmarshal([]byte(b), &v)
		if resp.StatusCode == 200 && d183Find(v, "ok") == true && d183Find(v, "error") == nil {
			t.Errorf("%s : accepté pour un user : %.150s", name, b)
		}
		sameSnapshot(t, before, cfgSnapshot(t), name)
	}
	// l'administrateur, lui, n'est pas refusé par R1 sur une charge non ambiguë
	if v, _ := s.call("ccoupel", px172Ext, `{"method":"configSave","args":{"kind":"config.conf","content":"x"}}`); d183Find(v, "forbidden") != nil {
		t.Errorf("admin refusé par R1 sur une charge non ambiguë : %v", v)
	}
}

// Un objet `args` dont deux clés ne diffèrent que par la casse est ambigu : refusé pour TOUS les rôles (400), jamais décodé
// deux fois différemment (correction (a) de l'audit).
func TestCrit1172AmbiguousArgsRefusedForEveryRole(t *testing.T) {
	px172Env(t)
	s := px172Start(t, false)
	body := `{"method":"refresh","args":{"path":"/a","Path":"/b"}}`
	for _, who := range [][2]string{{"ccoupel", px172Ext}, {"alice", px172Ext}, {"admin@local", px172Int}} {
		h := map[string]string{"X-Forwarded-User": who[0], "Content-Type": "application/json"}
		if resp, b := s.do("POST", "/api/call", who[1], h, body); resp == nil || resp.StatusCode != 400 {
			t.Errorf("%s : args à clés ambiguës : %v %.100s, attendu 400", who[0], resp, b)
		}
	}
	// une charge sans doublon passe toujours
	if v, st := s.call("alice", px172Ext, `{"method":"refresh","args":{"path":"/a"}}`); st != 200 {
		t.Errorf("charge normale refusée : %d %v", st, v)
	}
}

// ── backendTest : un user teste un backend DÉCLARÉ ; les champs R1 fournis par le client sont refusés ─────────────────

func backendTestSetup(t *testing.T) (a *App, calls string) {
	t.Helper()
	unixOnlyDesktop(t)
	a = r1App(t)
	dir := t.TempDir()
	calls = filepath.Join(dir, "calls.log")
	script := "#!/bin/sh\necho \"$@\" >> " + calls + "\ncase \"$*\" in *'auth can-i'*) echo yes;; *) echo '{}';; esac\n"
	for _, n := range []string{"kubectl", "ssh"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	y := "defaults:\n  browser_server: { listen: off }\nbackends:\n  k: {type: k8s, context: ctx-a, namespace: ns-a, volumes: {/data: pvc}}\n  s: {type: ssh, host: hôte-a, shared: [/data]}\n"
	if err := os.WriteFile(core.BackendsPath(), []byte(y), 0o644); err != nil {
		t.Fatal(err)
	}
	return a, calls
}

func unixOnlyDesktop(t *testing.T) {
	t.Helper()
	if os.PathSeparator != '/' {
		t.Skip("faux kubectl / ssh en scripts sh : Linux/macOS")
	}
}

func TestBackendTest172UserRefusedR1FieldsFromClient(t *testing.T) {
	a, calls := backendTestSetup(t)
	for name, body := range map[string]string{
		"context différent du déclaré":        `{"backend":{"name":"k","type":"k8s","context":"admin-ctx","namespace":"ns-a"}}`,
		"namespace différent":                 `{"backend":{"name":"k","type":"k8s","context":"ctx-a","namespace":"kube-system"}}`,
		"kubeconfig fourni":                   `{"backend":{"name":"k","type":"k8s","context":"ctx-a","namespace":"ns-a","kubeconfig":"/etc/rancher/k3s/k3s.yaml"}}`,
		"kubeconfig du coffre":                `{"backend":{"name":"k","type":"k8s","kubeconfig":true}}`,
		"backend non déclaré k8s":             `{"backend":{"name":"nouveau","type":"k8s","context":"admin-ctx","namespace":"kube-system"}}`,
		"type host":                           `{"backend":{"name":"h","type":"host"}}`,
		"volumes fournis":                     `{"backend":{"name":"k","type":"k8s","context":"ctx-a","namespace":"ns-a","volumes":{"/etc":"x"}}}`,
		"hôte ssh substitué (clé du déclaré)": `{"backend":{"name":"s","type":"ssh","host":"hôte-b","shared":["/data"]}}`,
		"nouveau backend ssh avec context":    `{"backend":{"name":"n","type":"ssh","host":"h","context":"admin-ctx"}}`,
		"clés ambiguës (casse)":               `{"backend":{"name":"k","type":"k8s","context":"ctx-a","Context":"admin-ctx","namespace":"ns-a"}}`,
	} {
		res, err := r1Call(a, core.AccessProxy, r1User, "backendTest", body)
		if strings.Contains(name, "(casse)") && err != nil { // refus à l'analyse (clé en double) : accepté aussi
			continue
		}
		if err != nil || !forbidden(res) {
			t.Errorf("user / backendTest %s : %v %v, attendu refusé par R1", name, res, err)
		}
	}
	if b, _ := os.ReadFile(calls); len(b) != 0 {
		t.Errorf("kubectl/ssh lancés pour un backendTest refusé :\n%s", b)
	}
}

// Un user teste un backend déclaré tel quel (champs de connexion et R1 identiques à la déclaration) ou un NOUVEAU backend sans
// champ R1 ; un admin teste n'importe quelle valeur.
func TestBackendTest172UserAllowedOnDeclaredBackend(t *testing.T) {
	a, _ := backendTestSetup(t)
	for name, body := range map[string]string{
		"k8s déclaré tel quel":      `{"backend":{"name":"k","type":"k8s","context":"ctx-a","namespace":"ns-a","volumes":{"/data":"pvc"}}}`,
		"ssh déclaré":               `{"backend":{"name":"s","type":"ssh","host":"hôte-a","shared":["/data"]}}`,
		"nouveau ssh sans champ R1": `{"backend":{"name":"nouveau","type":"ssh","host":"hôte-c","shared":["/data"]}}`,
	} {
		if res, _ := r1Call(a, core.AccessProxy, r1User, "backendTest", body); forbidden(res) {
			t.Errorf("user / backendTest %s : refusé à tort (backend déclaré, champs libres)", name)
		}
	}
	for _, who := range []struct {
		n  string
		id *core.Identity
	}{{"admin", r1Admin}, {"admin@local", r1Local}} {
		body := `{"backend":{"name":"k","type":"k8s","context":"autre-ctx","namespace":"autre-ns"}}`
		if res, _ := r1Call(a, core.AccessProxy, who.id, "backendTest", body); forbidden(res) {
			t.Errorf("%s / backendTest : refusé par R1", who.n)
		}
	}
	// fenêtre native et mode jeton : inchangés
	body := `{"backend":{"name":"k","type":"k8s","context":"autre-ctx"}}`
	for _, access := range []string{core.AccessToken, ""} {
		if res, _ := r1Call(a, access, nil, "backendTest", body); forbidden(res) {
			t.Errorf("mode %q : backendTest refusé", access)
		}
	}
}

// ── flux SSE : l'identité est revalidée (durée de vie bornée par CL_AUTH_SESSION_REFRESH) ─────────────────────────────

func TestSSE172StreamClosedAfterRefresh(t *testing.T) {
	px172Env(t)
	t.Setenv("CL_AUTH_SESSION_REFRESH", "2s")
	t.Setenv("CL_AUTH_SESSION_DURATION", "1h")
	s := px172Start(t, false)
	open := func() (time.Duration, int) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:"+strconv.Itoa(s.port)+"/api/events", nil)
		req.Host = px172Ext
		req.Header.Set("X-Forwarded-User", "ccoupel")
		t0 := time.Now()
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return 0, resp.StatusCode
		}
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() { // jusqu'à la fermeture par le serveur
		}
		return time.Since(t0), 200
	}
	d, st := open()
	if st != 200 {
		t.Fatalf("flux : HTTP %d", st)
	}
	if d > 10*time.Second {
		t.Errorf("flux resté ouvert %s : durée de vie bornée par CL_AUTH_SESSION_REFRESH (2 s) attendue, le client se reconnecte", d)
	}
	// la reconnexion fonctionne (identité revérifiée à l'ouverture) ; un login retiré des listes est refusé
	if d2, st2 := open(); st2 != 200 || d2 > 10*time.Second {
		t.Errorf("reconnexion : HTTP %d, durée %s", st2, d2)
	}
	if resp, _ := s.do("GET", "/api/events", px172Ext, map[string]string{"X-Forwarded-User": "mallory"}, ""); resp == nil || resp.StatusCode != 403 {
		t.Errorf("flux d'un login hors listes : %v, attendu 403", resp)
	}
}

// ── audit : écrit AVANT l'exécution d'une méthode R1 ───────────────────────────────────────────────────────────────

func TestAudit172WrittenBeforeR1Execution(t *testing.T) {
	px172Env(t)
	s := px172Start(t, false)
	for _, c := range []struct{ kind, file, content string }{
		{"config.conf", "config.conf", core.ConfigTemplate(core.ConfConf) + "\n# essai audit\n"},
		{"env.yaml", "env.yaml", core.ConfigTemplate(core.ConfLauncherEnv) + "\n# essai audit\n"},
		{"backends.yaml", "backends.yaml", "defaults:\n  browser_server: { listen: off }\nbackends:\n  k: {type: k8s, volumes: {/a: pvc}}\n"},
	} {
		before := len(s.auditLines(t))
		body, _ := json.Marshal(map[string]any{"method": "configSave", "args": map[string]any{"kind": c.kind, "content": c.content}})
		s.call("ccoupel", px172Ext, string(body))
		lines := s.auditLines(t)[before:]
		// ordre : la ligne d'intention (phase « intent ») précède la ligne de résultat du même appel
		var intent, result = -1, -1
		for i, l := range lines {
			if l["method"] == "configSave" && l["phase"] == "intent" {
				intent = i
			} else if l["method"] == "configSave" && l["kind"] != "agg" {
				result = i
			}
		}
		if intent < 0 || result < 0 || intent > result {
			t.Errorf("%s : lignes d'audit %v, attendu une ligne d'intention (phase intent) puis la ligne de résultat", c.kind, lines)
			continue
		}
		// l'horodatage de l'intention précède celui du résultat (l'horloge du système de fichiers, trop grossière, ne permet pas
		// de la comparer à la date d'écriture du fichier de configuration : l'ordre des lignes est le critère)
		ti, _ := time.Parse(time.RFC3339Nano, fmt.Sprint(lines[intent]["ts"]))
		tr, _ := time.Parse(time.RFC3339Nano, fmt.Sprint(lines[result]["ts"]))
		if ti.After(tr) {
			t.Errorf("%s : horodatage de l'intention %s postérieur à celui du résultat %s", c.kind, ti, tr)
		}
	}
}

// ── flood de refus R1 agrégé ; XFF : on garde la QUEUE (adresse ajoutée par Traefik) ───────────────────────────────

func TestAudit172ForbiddenFloodAggregated(t *testing.T) {
	px172Env(t)
	s := px172Start(t, false)
	minuteStart()
	for i := 0; i < 150; i++ {
		s.call("alice", px172Ext, `{"method":"configSave","args":{"kind":"config.conf","content":"x"}}`)
	}
	// un succès R1 de l'admin n'est jamais agrégé
	for i := 0; i < 3; i++ {
		s.call("ccoupel", px172Ext, `{"method":"backendSecretDelete","args":{"backend":"inconnu"}}`)
	}
	s.stop()
	var indiv, agg int
	var adminR1 int
	for _, l := range s.auditLines(t) {
		switch {
		case l["phase"] == "intent":
			// ligne d'intention (écrite avant l'exécution d'une méthode R1) : comptée à part
		case l["method"] == "configSave" && l["user"] == "alice" && l["kind"] == "agg":
			agg += int(l["count"].(float64))
		case l["method"] == "configSave" && l["user"] == "alice":
			indiv++
		case l["method"] == "backendSecretDelete" && l["user"] == "ccoupel" && l["kind"] != "agg":
			adminR1++
		}
	}
	if indiv > 60 || agg > 150-indiv || (agg > 0 && indiv+agg != 150) {
		t.Errorf("refus R1 de alice : %d individuelles + %d agrégées, attendu au plus 60 individuelles (le reste agrégé ou écarté, jamais écrit ligne à ligne)", indiv, agg)
	}
	if adminR1 != 3 {
		t.Errorf("appels R1 de l'admin : %d entrées individuelles, attendu 3 (jamais agrégés)", adminR1)
	}
}

func TestAudit172SrcKeepsTailOfXFF(t *testing.T) {
	px172Env(t)
	s := px172Start(t, false)
	xff := strings.Repeat("9", 400) + ", 203.0.113.77" // la vraie source est ajoutée en FIN de chaîne par Traefik
	s.do("POST", "/api/call", px172Ext, map[string]string{"X-Forwarded-User": "ccoupel", "Content-Type": "application/json", "X-Forwarded-For": xff}, `{"method":"configSave","args":{"kind":"config.conf","content":"x"}}`)
	s.stop()
	found := false
	for _, l := range s.auditLines(t) {
		if l["method"] == "configSave" {
			found = true
			src := fmt.Sprint(l["src"])
			if !strings.Contains(src, "203.0.113.77") {
				t.Errorf("src = %.100q… : la queue de X-Forwarded-For (adresse ajoutée par le proxy) est perdue", src)
			}
			if i := strings.Index(src, "X-Forwarded-For: "); i >= 0 && len(strings.TrimSuffix(src[i+len("X-Forwarded-For: "):], ")")) > 256 {
				t.Errorf("X-Forwarded-For de %d octets, attendu au plus 256", len(src))
			}
		}
	}
	if !found {
		t.Error("aucune entrée configSave")
	}
}

// method inconnue : journalisée « unknown », bornes sur method / target / project ; une ligne reste petite.
func TestAudit172FieldsBoundedOverHTTP(t *testing.T) {
	px172Env(t)
	s := px172Start(t, false)
	big := strings.Repeat("M", 100000)
	s.call("ccoupel", px172Ext, `{"method":"`+big+`","args":{}}`)
	s.call("ccoupel", px172Ext, `{"method":"configSave","args":{"kind":"`+strings.Repeat("K", 50000)+`","content":"x"}}`)
	s.call("ccoupel", px172Ext, `{"method":"backendSecretDelete","args":{"backend":"`+strings.Repeat("B", 50000)+`"}}`)
	s.stop()
	lines := s.auditLines(t)
	if len(lines) == 0 {
		t.Fatal("aucune entrée")
	}
	for _, l := range lines {
		b, _ := json.Marshal(l)
		if len(b) > 2048 {
			t.Errorf("ligne d'audit de %d octets : %.120s…", len(b), b)
		}
		if m, _ := l["method"].(string); len(m) > 64 {
			t.Errorf("method de %d caractères, attendu au plus 64", len(m))
		}
		if tg, _ := l["target"].(string); len(tg) > 128 {
			t.Errorf("target de %d caractères, attendu au plus 128", len(tg))
		}
		if pr, _ := l["project"].(string); len(pr) > 128 {
			t.Errorf("project de %d caractères, attendu au plus 128", len(pr))
		}
	}
	unknown := false
	for _, l := range lines {
		if l["method"] == "unknown" {
			unknown = true
		}
	}
	if !unknown {
		t.Errorf("méthode inconnue non journalisée sous « unknown » : %v", lines)
	}
}

// N1 (ré-audit) : encoding/json plie la casse au-delà de l'ASCII — « ſ » (U+017F, s long) égale « s » et « K » (U+212A,
// signe Kelvin) égale « k ». Deux clés qui ne diffèrent que par ce pliage sont AMBIGUËS comme celles qui diffèrent par la
// casse : refusées (400) pour tous les rôles, jamais décodées deux fois différemment, aucune écriture R1 possible.
func unicodeFoldPayloads(conf, bk string) map[string]string {
	q := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	c, k := q(conf), q(bk)
	return map[string]string{
		"Kelvin \\u212A contre k (kind)":      `{"method":"configSave","args":{"kind":"project-env","Kind":"config.conf","path":"/x","content":` + c + `}}`,
		"Kelvin littéral contre k (kind)":     `{"method":"configSave","args":{"kind":"project-env","Kind":"config.conf","path":"/x","content":` + c + `}}`,
		"s long \\u017F contre s (changes)":   `{"method":"configApply","args":{"kind":"backends.yaml","changes":[],"changeſ":[{"path":"backends.s.pod.serviceAccountName","value":"admin"}]}}`,
		"s long littéral contre s (changes)":  `{"method":"configApply","args":{"kind":"backends.yaml","changes":[],"changeſ":[{"path":"backends.s.type","value":"host"}]}}`,
		"s long dans content":                 `{"method":"configSave","args":{"kind":"backends.yaml","content":"backends:\n  s: {type: ssh, host: h}\n","content":` + k + `}}`,
		"Kelvin dans un champ de backendTest": `{"method":"backendTest","args":{"backend":{"name":"k","type":"k8s","context":"ctx-a","namespace":"ns-a","kubeconfig":"/a","Kubeconfig":"/b"}}}`,
		"s long dans un champ de backendTest": `{"method":"backendTest","args":{"backend":{"name":"k","type":"k8s","namespace":"ns-a","nameſ":"x","namespace":"kube-system"}}}`,
		"Kelvin majuscule + s long":           `{"method":"configSave","args":{"KIND":"config.conf","kind":"project-env","content":` + c + `,"CONTENT":` + c + `}}`,
	}
}

func TestCrit1172UnicodeFoldKeysRefusedForEveryRole(t *testing.T) {
	px172Env(t)
	s := px172Start(t, false)
	conf, _, bk := crit1Contents()
	before := cfgSnapshot(t)
	for name, body := range unicodeFoldPayloads(conf, bk) {
		for _, who := range [][2]string{{"alice", px172Ext}, {"ccoupel", px172Ext}, {"admin@local", px172Int}} {
			h := map[string]string{"X-Forwarded-User": who[0], "Content-Type": "application/json"}
			resp, b := s.do("POST", "/api/call", who[1], h, body)
			if resp == nil || resp.StatusCode != 400 {
				code := 0
				if resp != nil {
					code = resp.StatusCode
				}
				t.Errorf("%s / %s : HTTP %d %.100s, attendu 400 (clés ambiguës après pliage de casse)", who[0], name, code, b)
			}
		}
	}
	sameSnapshot(t, before, cfgSnapshot(t), "pliage de casse unicode")
	// une clé qui ne diffère QUE par un autre caractère n'est pas un doublon : la charge normale passe
	if v, st := s.call("alice", px172Ext, `{"method":"refresh","args":{"path":"/a","paths":"/b","ſ":1}}`); st != 200 {
		t.Errorf("clés distinctes refusées : %d %v", st, v)
	}
}

// Même décision au niveau de l'application (sans passer par HTTP) : aucune charge n'écrit un fichier R1 pour un user.
func TestCrit1172UnicodeFoldNoR1WriteByUser(t *testing.T) {
	a := r1App(t)
	conf, _, bk := crit1Contents()
	before := cfgSnapshot(t)
	for name, body := range unicodeFoldPayloads(conf, bk) {
		var env struct {
			Method string          `json:"method"`
			Args   json.RawMessage `json:"args"`
		}
		_ = json.Unmarshal([]byte(body), &env)
		res, err := r1Call(a, core.AccessProxy, r1User, env.Method, string(env.Args))
		if err == nil && res != nil && res["ok"] == true && !forbidden(res) {
			t.Errorf("%s : accepté pour un user : %v", name, res)
		}
		sameSnapshot(t, before, cfgSnapshot(t), name)
	}
}
