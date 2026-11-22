//go:build !windows

package desktop

// #227 : secretReveal — révélation d'un élément caché (jeton GitHub) À LA DEMANDE. Liste fermée des clés `secret`, refus depuis
// un accès distant (adresse publique ou hôte hors boucle locale), `Cache-Control: no-store`, 10 révélations par minute et par
// session, valeur absente de configModel, des instantanés, des évènements, des messages d'erreur, des journaux et de la sortie
// de log. Jeton factice ghp_FAKE_TEST_… ; sandbox (HOME / CLAUDE_CONFIG_DIR temporaires). Écrit sur le contrat
// (contracts/desktop-dispatch.md « secretReveal »).

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"claude-launcher-ng2/core"
)

const token227 = "ghp_FAKE_TEST_227abcdefghijklmnopqrstuvwxyz0123456789"

func fix227(t *testing.T) (*App, *recorder) {
	t.Helper()
	sandbox(t, "", "alpha")
	if err := os.MkdirAll(filepath.Dir(core.ConfigPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	conf := "GITHUB_TOKEN=" + token227 + "\nCLAUDE_OPTIONS=\"--canary-227\"\nTEAM_HIDE_IDLE=30\n"
	if err := os.WriteFile(core.ConfigPath(), []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &recorder{}
	return newApp(t, r.emit), r
}

func reveal227(t *testing.T, a *App, key string) map[string]any {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"kind": "config.conf", "key": key})
	res, err := a.Dispatch("secretReveal", b)
	if err != nil {
		return map[string]any{"ok": false, "msg": err.Error()}
	}
	out, _ := json.Marshal(res)
	var v map[string]any
	_ = json.Unmarshal(out, &v)
	return v
}

// Fenêtre native : le jeton est rendu ; l'œil est actif (`reveal:true`) sur l'entrée secrète du modèle.
func TestRevelationNativeRendLeJeton227(t *testing.T) {
	a, _ := fix227(t)
	if r := reveal227(t, a, "GITHUB_TOKEN"); r["ok"] != true || r["value"] != token227 {
		t.Fatalf("secretReveal natif : %v", r)
	}
	b, _ := json.Marshal(map[string]any{"kind": "config.conf"})
	res, _ := a.Dispatch("configModel", b)
	out, _ := json.Marshal(res)
	var m map[string]any
	_ = json.Unmarshal(out, &m)
	found := false
	for _, it := range m["model"].(map[string]any)["settings"].([]any) {
		s := it.(map[string]any)
		if s["key"] == "GITHUB_TOKEN" {
			found = true
			if s["reveal"] != true {
				t.Errorf("GITHUB_TOKEN : reveal = %v, attendu true (fenêtre native)", s["reveal"])
			}
		} else if s["reveal"] == true {
			t.Errorf("%s : reveal vrai sur une entrée non secrète", s["key"])
		}
	}
	if !found {
		t.Error("GITHUB_TOKEN absent du modèle")
	}
}

// Liste fermée : toute clé qui n'est pas de type `secret` est refusée, sans valeur ni écho de la valeur d'une AUTRE clé.
func TestRevelationCleNonSecreteRefusee227(t *testing.T) {
	a, _ := fix227(t)
	for _, key := range []string{"CLAUDE_OPTIONS", "TEAM_HIDE_IDLE", "GITHUB_DIR", "NOPE", "", "github_token", "GITHUB_TOKEN ", "GITHUB_TOKEN\n", "../GITHUB_TOKEN", "GITHUB_TOKEN;ls", "*"} {
		r := reveal227(t, a, key)
		out, _ := json.Marshal(r)
		if r["ok"] == true || r["value"] != nil {
			t.Errorf("clé %q : %s, attendu un refus sans valeur", key, out)
		}
		if strings.Contains(string(out), token227) || strings.Contains(string(out), "canary-227") {
			t.Errorf("clé %q : la réponse contient une valeur : %s", key, out)
		}
	}
	// kind inconnu / autre fichier : refusé aussi
	b, _ := json.Marshal(map[string]any{"kind": "backends.yaml", "key": "GITHUB_TOKEN"})
	if res, _ := a.Dispatch("secretReveal", b); res != nil {
		out, _ := json.Marshal(res)
		if strings.Contains(string(out), token227) {
			t.Errorf("kind backends.yaml : %s", out)
		}
	}
}

// La valeur n'est JAMAIS dans configModel, l'instantané ni les évènements — avant comme après une révélation.
func TestJetonAbsentDesVuesEtEvenements227(t *testing.T) {
	a, r := fix227(t)
	a.rescan()
	bodies := func() string {
		var sb strings.Builder
		for _, m := range []string{"configModel", "configFiles", "launcherInfo", "snapshot"} {
			b, _ := json.Marshal(map[string]any{"kind": "config.conf"})
			res, err := a.Dispatch(m, b)
			if err == nil {
				out, _ := json.Marshal(res)
				sb.Write(out)
			}
		}
		out, _ := json.Marshal(a.Snapshot())
		sb.Write(out)
		return sb.String()
	}
	if strings.Contains(bodies(), token227) {
		t.Error("jeton présent dans une vue AVANT révélation")
	}
	if rr := reveal227(t, a, "GITHUB_TOKEN"); rr["ok"] != true {
		t.Fatalf("révélation : %v", rr)
	}
	if strings.Contains(bodies(), token227) {
		t.Error("jeton présent dans une vue APRÈS révélation (aucune persistance de la valeur côté serveur)")
	}
	r.mu.Lock()
	evs, _ := json.Marshal(r.evs)
	r.mu.Unlock()
	if strings.Contains(string(evs), token227) {
		t.Error("jeton présent dans un évènement émis")
	}
}

// Journal : « révélé : clé, interface, heure » — jamais la valeur, ni sa longueur, ni une empreinte ; sortie de log idem.
func TestJournalEtLogSansLaValeur227(t *testing.T) {
	a, _ := fix227(t)
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	if r := reveal227(t, a, "GITHUB_TOKEN"); r["ok"] != true {
		t.Fatalf("révélation : %v", r)
	}
	_ = reveal227(t, a, "CLAUDE_OPTIONS") // refusée : ne doit pas non plus fuiter
	jb, _ := os.ReadFile(filepath.Join(core.CacheDir(), "secrets-journal.jsonl"))
	j := string(jb)
	if !strings.Contains(j, "GITHUB_TOKEN") {
		t.Errorf("la révélation n'est pas journalisée (clé, interface, heure) : %q", j)
	}
	for _, line := range strings.Split(strings.TrimSpace(j), "\n") {
		var m map[string]string
		if json.Unmarshal([]byte(line), &m) != nil {
			continue
		}
		if m["fingerprint"] != "" {
			t.Errorf("journal : empreinte consignée (%q) : interdit", m["fingerprint"])
		}
	}
	for what, s := range map[string]string{"journal": j, "log": logs.String()} {
		for _, leak := range []string{token227, "FAKE_TEST_227", "canary-227"} {
			if strings.Contains(s, leak) {
				t.Errorf("%s : contient %q", what, leak)
			}
		}
	}
	if strings.Contains(j, "\"length\"") || strings.Contains(j, "\"len\"") {
		t.Errorf("journal : longueur consignée : %s", j)
	}
}

// ── accès web ─────────────────────────────────────────────────────────────

func web227(t *testing.T, a *App, public string, strict bool) (*httptest.Server, *webBridge) {
	t.Helper()
	w := &webBridge{app: a, token: "tok227", subs: map[chan []byte]bool{}, strictHost: strict}
	if public != "" {
		p, err := core.ParsePublicURL(public)
		if err != nil {
			t.Fatal(err)
		}
		w.public = &p
	}
	srv := httptest.NewServer(w.handler())
	t.Cleanup(srv.Close)
	return srv, w
}

// sess227 : session obtenue par la page d'amorçage sur l'hôte donné (sans suivre la redirection ; cookie « cl_session… » ou
// « __Host-cl_session… » en https public) ; nil si refusée.
func sess227(t *testing.T, srv *httptest.Server, host string) *http.Cookie {
	t.Helper()
	req, _ := http.NewRequest("GET", srv.URL+"/?t=tok227", nil)
	req.Host = host
	resp, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	for _, c := range resp.Cookies() {
		if strings.Contains(c.Name, "cl_session") {
			return c
		}
	}
	return nil
}

// post227 : appel /api/call avec la SESSION donnée (une même session sert à compter les révélations).
func post227(t *testing.T, srv *httptest.Server, host string, sess *http.Cookie, method string, args map[string]any) (*http.Response, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"method": method, "args": args})
	req, _ := http.NewRequest("POST", srv.URL+"/api/call", bytes.NewReader(body))
	if host != "" {
		req.Host = host
	}
	req.Header.Set("Content-Type", "application/json")
	if host == "launcher.test:8443" {
		req.Header.Set("Origin", "https://launcher.test:8443")
	}
	if sess != nil {
		req.AddCookie(sess)
	}
	resp, err := webClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func result227(out string) map[string]any {
	var v map[string]any
	_ = json.Unmarshal([]byte(out), &v)
	if r, ok := v["result"].(map[string]any); ok {
		return r
	}
	return v
}

// Accès web LOCAL (boucle locale, session valide) : admis, `Cache-Control: no-store`, sans session : refusé.
func TestRevelationWebLocaleNoStore227(t *testing.T) {
	a, _ := fix227(t)
	srv, _ := web227(t, a, "", true)
	args := map[string]any{"kind": "config.conf", "key": "GITHUB_TOKEN"}
	if resp, out := post227(t, srv, "", nil, "secretReveal", args); resp.StatusCode == 200 || strings.Contains(out, token227) {
		t.Errorf("sans session : %d %s", resp.StatusCode, out)
	}
	sess := sessionCookie(t, srv.URL, "", "tok227")
	resp, out := post227(t, srv, "", sess, "secretReveal", args)
	if r := result227(out); resp.StatusCode != 200 || r["ok"] != true || r["value"] != token227 {
		t.Fatalf("révélation web locale : %d %s", resp.StatusCode, out)
	}
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("Cache-Control = %q, attendu no-store", cc)
	}
}

// Accès DISTANT : adresse publique (desktop_web.url) ou hôte hors boucle locale → refus « indisponible depuis un accès
// distant », jamais la valeur ; configModel y rend `reveal:false` (œil grisé).
func TestRevelationWebDistanteRefusee227(t *testing.T) {
	a, _ := fix227(t)
	args := map[string]any{"kind": "config.conf", "key": "GITHUB_TOKEN"}
	for name, c := range map[string]struct {
		public string
		strict bool
		host   string
	}{
		"adresse publique (proxy inverse)":   {"https://claude.example.org", true, "claude.example.org"},
		"hôte du réseau local (écoute LAN)":  {"", false, "192.168.1.50:8765"},
		"nom d'hôte quelconque (écoute LAN)": {"", false, "launcher.interne:8765"},
	} {
		t.Run(name, func(t *testing.T) {
			srv, _ := web227(t, a, c.public, c.strict)
			sess := sess227(t, srv, c.host)
			if sess == nil {
				t.Fatalf("session non obtenue pour l'hôte %s : le banc ne teste plus l'accès distant", c.host)
			}
			resp, out := post227(t, srv, c.host, sess, "secretReveal", args)
			if strings.Contains(out, token227) || strings.Contains(out, "FAKE_TEST_227") {
				t.Fatalf("la valeur a quitté le serveur vers un accès distant : %s", out)
			}
			r := result227(out)
			if resp.StatusCode == 200 && (r["ok"] == true || !strings.Contains(strings.ToLower(out), "distant")) {
				t.Errorf("réponse = %d %s, attendu un refus « indisponible depuis un accès distant »", resp.StatusCode, out)
			}
			seenTok := false
			_, mo := post227(t, srv, c.host, sess, "configModel", map[string]any{"kind": "config.conf"})
			if strings.Contains(mo, token227) {
				t.Error("configModel distant : contient le jeton")
			}
			for _, it := range func() []any { s, _ := result227(mo)["model"].(map[string]any)["settings"].([]any); return s }() {
				if s := it.(map[string]any); s["key"] == "GITHUB_TOKEN" {
					seenTok = true
					if v, present := s["reveal"]; !present || v != false {
						t.Errorf("configModel distant : reveal = %v (présent : %v), attendu false PRÉSENT (l'œil grisé se décide sur reveal === false)", v, present)
					}
				}
			}
			if !seenTok {
				t.Error("GITHUB_TOKEN absent du modèle distant")
			}
		})
	}
}

// Débit : au plus 10 révélations par minute et par session ; la 11ᵉ est refusée sans valeur ; une AUTRE session a son compte.
func TestRevelationDebitParSession227(t *testing.T) {
	a, _ := fix227(t)
	srv, _ := web227(t, a, "", true)
	args := map[string]any{"kind": "config.conf", "key": "GITHUB_TOKEN"}
	s1 := sessionCookie(t, srv.URL, "", "tok227")
	for i := 1; i <= 10; i++ {
		_, out := post227(t, srv, "", s1, "secretReveal", args)
		if r := result227(out); r["ok"] != true {
			t.Fatalf("révélation %d/10 refusée : %s", i, out)
		}
	}
	_, out := post227(t, srv, "", s1, "secretReveal", args)
	if r := result227(out); r["ok"] == true || strings.Contains(out, token227) {
		t.Errorf("11ᵉ révélation dans la minute : %s, attendu un refus sans valeur", out)
	}
	s2 := sessionCookie(t, srv.URL, "", "tok227")
	if _, out := post227(t, srv, "", s2, "secretReveal", args); result227(out)["ok"] != true {
		t.Errorf("autre session : %s, attendu admise (le compte est par session)", out)
	}
}
