//go:build !windows

package desktop

// #250 : accès web local et distant (refus de teamAgentSet, clientInfo). Hors Windows : s'appuie sur web227, sess227,
// post227 et result227 (reveal227_test.go, //go:build !windows).

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

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
