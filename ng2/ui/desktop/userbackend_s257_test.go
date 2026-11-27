package desktop

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// LOT-3.1 Batch 2 (#257) — par l'API web d'un UTILISATEUR (RunWeb, mode proxy) : kubeconfig durci (S10, S13, S17–S21), certificats TLS
// docker (S12), fuites par canari (S8, S25, S26), limites et audit (S31, D5), backendTest et avertissements (S22, P19). Écrits avant le
// code ; aucun secret réel (canaris CANARY-…), faux docker/kubectl, jamais de vrais projets. Formes de réponse :
// backendSecretSet → {ok, msg, warnings?} ; backendTest → {steps:[{label, ok, detail, fix}]} ; refus → {ok:false, msg}.

const (
	uCanary = "CANARY-7f3a9c1e"
	uRefus  = "adresse non autorisée pour un utilisateur"
)

type ub257 struct {
	s    *px172
	dir  string   // faux outils
	seen []string // toutes les réponses brutes (contrôle des fuites)
	mu   sync.Mutex
}

func ub257Start(t *testing.T) *ub257 {
	t.Helper()
	s, _ := ub16Start(t)
	dir := t.TempDir()
	script := func(body string) string { return "#!/bin/sh\necho \"ARGV: $*\" >> \"" + dir + "/calls.log\"\n" + body }
	docker := script(`case "$1" in
  volume) [ -f "` + dir + `/novolume" ] && { echo "Error: No such volume" >&2; exit 1; }; echo v ;;
  *) [ -f "` + dir + `/slow" ] && sleep 1; echo 27.0 ;;
esac
`)
	kubectl := script(`case "$*" in
  *"get pvc"*) [ -f "` + dir + `/nopvc" ] && { echo "Error from server (NotFound)" >&2; exit 1; }; printf 'Bound ["ReadWriteMany"]' ;;
  *"auth can-i"*) echo yes ;;
  *create*) echo '{"status":{"allowed":true}}' ;;
  *) [ -f "` + dir + `/slow" ] && sleep 1; echo '{}' ;;
esac
`)
	for n, b := range map[string]string{"docker": docker, "podman": docker, "kubectl": kubectl} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(b), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return &ub257{s: s, dir: dir}
}

func (u *ub257) call(t *testing.T, user, method string, args any) (raw string, res map[string]any, errMsg string) {
	t.Helper()
	raw, errMsg, res = ed255Do(t, u.s, user, method, args)
	u.mu.Lock()
	u.seen = append(u.seen, raw)
	u.mu.Unlock()
	return
}

// refus : réponse de refus lisible (error ou {ok:false,msg}) ; rend le message.
func ub257Refusal(res map[string]any, errMsg string) (string, bool) {
	if errMsg != "" {
		return errMsg, true
	}
	if res != nil && (res["ok"] == false || res["invalid"] == true) {
		if m, _ := res["msg"].(string); m != "" {
			return m, true
		}
		if ps, _ := res["problems"].([]any); len(ps) > 0 {
			return fmt.Sprint(ps), true
		}
		return "", res["ok"] == false
	}
	return "", false
}

func ub257DeclareDocker(t *testing.T, uid, name, host, extra string) {
	t.Helper()
	sp, err := core.UserSpace(uid)
	if err != nil {
		t.Fatal(err)
	}
	if err := sp.Ensure(); err != nil {
		t.Fatal(err)
	}
	y := "backends:\n  " + name + ":\n    type: docker\n    host: " + host + "\n    volume: v\n" + extra
	if err := os.WriteFile(sp.BackendsPath(), []byte(y), 0o600); err != nil {
		t.Fatal(err)
	}
}

func ub257Cert(t *testing.T, ca bool) (certPEM, keyPEM string) {
	t.Helper()
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "test-257"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), IsCA: ca, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	kb, _ := x509.MarshalPKCS8PrivateKey(k)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kb}))
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// kubeconfig : serveur public par défaut ; extra : lignes ajoutées sous « user: » (indentées de 4) ou sous « cluster: » (cluster:).
func ub257Kube(server, userExtra, clusterExtra string) string {
	return "apiVersion: v1\nkind: Config\nclusters:\n- name: c\n  cluster:\n    server: " + server + "\n" + clusterExtra +
		"users:\n- name: u\n  user:\n    token: " + uCanary + "\n" + userExtra +
		"contexts:\n- name: ctx\n  context:\n    cluster: c\n    user: u\ncurrent-context: ctx\n"
}

func (u *ub257) scanLeaks(t *testing.T) {
	t.Helper()
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, r := range u.seen {
		if strings.Contains(r, uCanary) {
			t.Errorf("canari dans une réponse web : %.300s", r)
		}
	}
	for _, l := range u.s.auditLines(t) {
		if b, _ := json.Marshal(l); strings.Contains(string(b), uCanary) {
			t.Errorf("canari dans l'audit : %s", b)
		}
	}
	if strings.Contains(fmtJournal(t), uCanary) {
		t.Error("canari dans le journal des écritures")
	}
	for p, c := range ub16Tree(t, filepath.Dir(core.UsersDir())) {
		if strings.Contains(c, uCanary) && !strings.Contains(p, string(filepath.Separator)+"vault"+string(filepath.Separator)) {
			t.Errorf("canari écrit hors d'un coffre : %s", p)
		}
	}
}

// ── S10, S13, S17–S21 : kubeconfig durci ──────────────────────────────────────────────────────────────

func TestKubeconfig257Durci(t *testing.T) {
	u := ub257Start(t)
	ub16Declare(t, "github-1111", "mon-k8s")
	good := "https://93.184.216.34:6443"
	deep := "x: " + strings.Repeat("[", 25) + strings.Repeat("]", 25) + "\n"
	for name, kc := range map[string]string{
		"taille > 64 Kio":                 ub257Kube(good, "", "") + "# " + strings.Repeat("a", 70<<10) + "\n",
		"ancre et alias":                  strings.Replace(ub257Kube(good, "", ""), "- name: c\n", "- name: &a c\n", 1) + "extra: *a\n",
		"clés en double":                  ub257Kube(good, "    token: autre\n", ""),
		"profondeur > 20":                 ub257Kube(good, "", "") + deep,
		"exec":                            ub257Kube(good, "    exec:\n      command: sh\n      apiVersion: client.authentication.k8s.io/v1\n", ""),
		"exec (casse Exec)":               ub257Kube(good, "    Exec:\n      command: sh\n", ""),
		"exec (casse EXEC)":               ub257Kube(good, "    EXEC:\n      command: sh\n", ""),
		"auth-provider":                   ub257Kube(good, "    auth-provider:\n      name: gcp\n", ""),
		"auth-provider (casse)":           ub257Kube(good, "    Auth-Provider:\n      name: gcp\n", ""),
		"client-certificate (fichier)":    ub257Kube(good, "    client-certificate: /etc/ssl/c.pem\n", ""),
		"client-key (fichier)":            ub257Kube(good, "    client-key: /etc/ssl/k.pem\n", ""),
		"certificate-authority (fichier)": ub257Kube(good, "", "    certificate-authority: /etc/ssl/ca.pem\n"),
		"tokenFile":                       ub257Kube(good, "    tokenFile: /var/run/secrets/token\n", ""),
		"token-file":                      ub257Kube(good, "    token-file: /var/run/secrets/token\n", ""),
		"proxy-url":                       ub257Kube(good, "", "    proxy-url: http://127.0.0.1:3128\n"),
		"proxy-url (casse)":               ub257Kube(good, "", "    Proxy-URL: http://127.0.0.1:3128\n"),
		"YAML invalide":                   "clusters: [\n" + uCanary,
		"vide":                            "",
	} {
		_, res, e := u.call(t, "alice", "backendSecretSet", map[string]any{"backend": "mon-k8s", "kubeconfig": kc})
		msg, ok := ub257Refusal(res, e)
		if !ok || (res != nil && res["ok"] == true) {
			t.Errorf("%s : accepté ou sans refus clair : %v %q", name, res, e)
			continue
		}
		if msg == "" {
			t.Errorf("%s : refus sans message", name)
		}
	}
	if _, err := os.Stat(filepath.Join(core.UsersDir(), "github-1111", "vault", "mon-k8s", "kubeconfig")); err == nil {
		t.Error("un kubeconfig refusé a été écrit dans le coffre")
	}
	// insecure-skip-tls-verify : accepté AVEC avertissement
	_, res, e := u.call(t, "alice", "backendSecretSet", map[string]any{"backend": "mon-k8s", "kubeconfig": ub257Kube(good, "", "    insecure-skip-tls-verify: true\n")})
	if e != "" || res["ok"] != true || !strings.Contains(fmt.Sprint(res["warnings"]), "non vérifié") {
		t.Errorf("insecure-skip-tls-verify : %v %q (accepté avec l'avertissement « certificat de l'API non vérifié » attendu)", res, e)
	}
	// un kubeconfig sain est accepté
	if _, res, e = u.call(t, "alice", "backendSecretSet", map[string]any{"backend": "mon-k8s", "kubeconfig": ub257Kube(good, "", "")}); e != "" || res["ok"] != true {
		t.Errorf("kubeconfig valide refusé : %v %q", res, e)
	}
	u.scanLeaks(t)
}

// S13 : le « server: » de TOUS les clusters est soumis au bornage ; https obligatoire.
func TestKubeconfig257ServerBorne(t *testing.T) {
	u := ub257Start(t)
	ub16Declare(t, "github-1111", "mon-k8s")
	second := func(srv string) string {
		return ub257Kube("https://93.184.216.34:6443", "", "") + "- name: c2\n  cluster:\n    server: " + srv + "\n"
	}
	for name, kc := range map[string]string{
		"http":                 ub257Kube("http://93.184.216.34:6443", "", ""),
		"loopback":             ub257Kube("https://127.0.0.1:6443", "", ""),
		"LAN 10/8":             ub257Kube("https://10.0.0.1:6443", "", ""),
		"LAN 192.168/16":       ub257Kube("https://192.168.1.50:6443", "", ""),
		"métadonnées":          ub257Kube("https://169.254.169.254:443", "", ""),
		"nom interne":          ub257Kube("https://kubernetes.default.svc.cluster.local:443", "", ""),
		"nom .lan":             ub257Kube("https://k3s.lan:6443", "", ""),
		"userinfo":             ub257Kube("https://u:p@93.184.216.34:6443", "", ""),
		"sans port":            ub257Kube("https://93.184.216.34", "", ""),
		"IPv4-mappée":          ub257Kube("https://[::ffff:10.0.0.1]:6443", "", ""),
		"décimal":              ub257Kube("https://3232235777:6443", "", ""),
		"second cluster privé": second("https://10.1.2.3:6443"),
	} {
		_, res, e := u.call(t, "alice", "backendSecretSet", map[string]any{"backend": "mon-k8s", "kubeconfig": kc})
		msg, ok := ub257Refusal(res, e)
		if !ok || (res != nil && res["ok"] == true) {
			t.Errorf("%s : accepté (%v %q)", name, res, e)
		} else if name != "http" && !strings.Contains(msg, uRefus) {
			t.Errorf("%s : message non uniforme %q (attendu « %s »)", name, msg, uRefus)
		}
	}
	// l'admin n'a aucune contrainte : même serveur LAN accepté
	raw, _ := u.s.call("admin@local", px172Int, `{"method":"backendSecretSet","args":`+ub257JSON(map[string]any{"backend": ed255Admin, "kubeconfig": ub257Kube("https://10.0.0.1:6443", "", "")})+`}`)
	if res, _ := raw["result"].(map[string]any); res == nil || res["ok"] != true {
		t.Errorf("admin : kubeconfig d'un serveur LAN refusé : %v", raw)
	}
	u.scanLeaks(t)
}

// ── S12 : certificats TLS docker collés ───────────────────────────────────────────────────────────────

func TestTLS257CertificatsColles(t *testing.T) {
	u := ub257Start(t)
	ub257DeclareDocker(t, "github-1111", "nas", "tcp://93.184.216.34:2376", "    tls:\n      enabled: true\n      verify: true\n")
	ca, _ := ub257Cert(t, true)
	cert, key := ub257Cert(t, false)
	_, key2 := ub257Cert(t, false)
	_, encKey := ub257Cert(t, false)
	encKey = strings.Replace(strings.Replace(encKey, "PRIVATE KEY", "ENCRYPTED PRIVATE KEY", 2), "\n", "\n", -1)
	set := func(args map[string]any) (map[string]any, string) {
		args["backend"] = "nas"
		_, res, e := u.call(t, "alice", "backendSecretSet", args)
		return res, e
	}
	if res, e := set(map[string]any{"ca": ca, "cert": cert, "key": key}); e != "" || res["ok"] != true {
		t.Fatalf("triplet valide refusé : %v %q", res, e)
	}
	for name, a := range map[string]map[string]any{
		"clé ≠ certificat":       {"ca": ca, "cert": cert, "key": key2},
		"clé chiffrée":           {"ca": ca, "cert": cert, "key": encKey},
		"clé absente":            {"ca": ca, "cert": cert},
		"PEM invalide (ca)":      {"ca": "pas un pem", "cert": cert, "key": key},
		"PEM invalide (cert)":    {"ca": ca, "cert": "-----BEGIN CERTIFICATE-----\n!!!\n-----END CERTIFICATE-----\n", "key": key},
		"deux blocs (ca)":        {"ca": ca + ca, "cert": cert, "key": key},
		"clé dans le champ cert": {"ca": ca, "cert": key, "key": key},
		"certificat dans la clé": {"ca": ca, "cert": cert, "key": cert},
		"> 64 Kio":               {"ca": ca + strings.Repeat("#", 70<<10), "cert": cert, "key": key},
	} {
		res, e := set(a)
		if _, ok := ub257Refusal(res, e); !ok || (res != nil && res["ok"] == true) {
			t.Errorf("%s : accepté (%v %q)", name, res, e)
		}
	}
	// le contenu n'est jamais réaffiché : ni PEM ni clé dans une réponse
	for _, r := range u.seen {
		if strings.Contains(r, "BEGIN") || strings.Contains(r, "PRIVATE KEY") {
			t.Errorf("matériel PEM dans une réponse : %.200s", r)
		}
	}
	if raw, _, _ := u.call(t, "alice", "backendSecretGet", map[string]any{"backend": "nas"}); strings.Contains(raw, "BEGIN") || strings.Contains(raw, "PRIVATE") {
		t.Errorf("backendSecretGet réaffiche du PEM : %.300s", raw)
	}
}

// ── S8, S25, S26 : fuites ──────────────────────────────────────────────────────────────────────────────

func TestFuites257ErreursYAMLEtMessagesFiltres(t *testing.T) {
	u := ub257Start(t)
	ub16Declare(t, "github-1111", "mon-k8s")
	bad := "backends:\n  x:\n    type: k8s\n    namespace: [" + uCanary + "\n"
	for _, m := range []string{"configValidate", "configSave"} {
		raw, _, _ := u.call(t, "alice", m, map[string]any{"kind": "backends.yaml", "content": bad})
		if strings.Contains(raw, uCanary) {
			t.Errorf("%s : l'erreur YAML cite le fichier (canari) : %.300s", m, raw)
		}
	}
	for _, ch := range []map[string]any{{"op": "set", "at": []string{"backends", "x"}, "value": map[string]any{"type": "k8s", "namespace": uCanary + " bad ns"}}} {
		raw, _, _ := u.call(t, "alice", "configApply", map[string]any{"kind": "backends.yaml", "path": "", "mtime": "", "changes": []any{ch}})
		if strings.Contains(raw, uCanary) {
			t.Errorf("configApply : canari dans la réponse : %.300s", raw)
		}
	}
	// kubeconfig refusé dont le jeton est le canari : le message ne le reprend pas
	u.call(t, "alice", "backendSecretSet", map[string]any{"backend": "mon-k8s", "kubeconfig": ub257Kube("https://10.0.0.1:6443", "    exec:\n      command: sh\n", "")})
	// message d'erreur d'un appel sortant : première ligne seulement, sans jeton / Bearer / PEM / base64 long
	u.scanLeaks(t)
	for _, r := range u.seen {
		for _, bad := range []string{"Bearer ", "BEGIN CERTIFICATE", "BEGIN PRIVATE KEY", "client-key-data"} {
			if strings.Contains(r, bad) {
				t.Errorf("réponse contenant « %s » : %.200s", bad, r)
			}
		}
	}
}

// ── D5 : limites ───────────────────────────────────────────────────────────────────────────────────────

func ub257Many(n int) string {
	var b strings.Builder
	b.WriteString("backends:\n")
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "  d%02d:\n    type: docker\n    host: tcp://93.184.216.%d:2376\n    volume: v\n", i, 10+i)
	}
	return b.String()
}

func TestLimites257OnziemeBackendRefuse(t *testing.T) {
	u := ub257Start(t)
	_, res, e := u.call(t, "alice", "configSave", map[string]any{"kind": "backends.yaml", "content": ub257Many(10)})
	if e != "" || res["saved"] != true {
		t.Fatalf("10 backends refusés : %v %q", res, e)
	}
	_, res, e = u.call(t, "bob", "configSave", map[string]any{"kind": "backends.yaml", "content": ub257Many(11)})
	msg, ok := ub257Refusal(res, e)
	if !ok || res["saved"] == true {
		t.Fatalf("11 backends acceptés : %v %q", res, e)
	}
	if !strings.Contains(msg, "10") {
		t.Errorf("message sans la limite (10) : %q", msg)
	}
	if _, err := os.Stat(filepath.Join(core.UsersDir(), "github-2222", "backends.yaml")); err == nil {
		t.Error("11 backends écrits")
	}
	// la limite est par utilisateur : l'admin n'est pas limité
	if ms := core.ValidateConfig(core.ConfBackends, []byte(ub257Many(12))); len(errMsgsDesk(ms)) != 0 {
		t.Errorf("l'admin est limité : %v", ms)
	}
}

func errMsgsDesk(ps []core.ConfigProblem) []string {
	var out []string
	for _, p := range ps {
		if p.Severity == core.SevError {
			out = append(out, p.Message)
		}
	}
	return out
}

func ub257Test(u *ub257, t *testing.T, user string) (string, map[string]any, string) {
	be := map[string]any{"name": "d", "type": "docker", "host": "tcp://93.184.216.34:2376", "volume": "v"}
	return u.call(t, user, "backendTest", map[string]any{"backend": be})
}

func TestLimites257SeptiemeTestEnUneMinute429(t *testing.T) {
	u := ub257Start(t)
	ub257DeclareDocker(t, "github-1111", "d", "tcp://93.184.216.34:2376", "")
	limited := func(raw string, res map[string]any, e string) bool {
		return strings.Contains(raw, "429") || strings.Contains(strings.ToLower(raw), "trop de tests") || strings.Contains(strings.ToLower(raw), "limite")
	}
	for i := 1; i <= 6; i++ {
		raw, res, e := ub257Test(u, t, "alice")
		if limited(raw, res, e) {
			t.Fatalf("test %d/6 déjà limité : %.200s", i, raw)
		}
	}
	if raw, res, e := ub257Test(u, t, "alice"); !limited(raw, res, e) {
		t.Errorf("7e test en une minute accepté : %.300s", raw)
	}
	// par utilisateur : bob n'est pas touché
	ub257DeclareDocker(t, "github-2222", "d", "tcp://93.184.216.34:2376", "")
	if raw, res, e := ub257Test(u, t, "bob"); limited(raw, res, e) {
		t.Errorf("bob limité par les tests d'alice : %.200s", raw)
	}
}

func TestLimites257UnSeulTestALaFois(t *testing.T) {
	u := ub257Start(t)
	ub257DeclareDocker(t, "github-1111", "d", "tcp://93.184.216.34:2376", "")
	_ = os.WriteFile(filepath.Join(u.dir, "slow"), nil, 0o644)
	var wg sync.WaitGroup
	out := make([]string, 2)
	for i := range out {
		wg.Add(1)
		go func(i int) { defer wg.Done(); out[i], _, _ = ub257Test(u, t, "alice") }(i)
	}
	wg.Wait()
	n := 0
	for _, r := range out {
		if strings.Contains(r, "429") || strings.Contains(strings.ToLower(r), "déjà en cours") || strings.Contains(strings.ToLower(r), "un seul") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d refus parmi 2 tests simultanés (1 attendu) : %.200s | %.200s", n, out[0], out[1])
	}
}

// ── audit backend.call ─────────────────────────────────────────────────────────────────────────────────

func TestAudit257BackendCallSansSecret(t *testing.T) {
	u := ub257Start(t)
	ub257DeclareDocker(t, "github-1111", "d", "tcp://93.184.216.34:2376", "")
	u.call(t, "alice", "backendSecretSet", map[string]any{"backend": "mon-k8s", "kubeconfig": ub257Kube("https://93.184.216.34:6443", "", "")})
	ub257Test(u, t, "alice")
	time.Sleep(200 * time.Millisecond)
	found := false
	for _, l := range u.s.auditLines(t) {
		b, _ := json.Marshal(l)
		s := string(b)
		if strings.Contains(s, "backend.call") {
			found = true
			for _, need := range []string{"alice"} {
				if !strings.Contains(s, need) {
					t.Errorf("ligne backend.call sans « %s » : %s", need, s)
				}
			}
			for _, leak := range []string{uCanary, "Bearer", "BEGIN", "93.184.216.34:6443"} {
				if strings.Contains(s, leak) {
					t.Errorf("ligne backend.call contenant « %s » : %s", leak, s)
				}
			}
		}
	}
	if !found {
		t.Error("aucun audit « backend.call » pour un appel sortant d'un utilisateur")
	}
	u.scanLeaks(t)
}

// ── backendTest : avertissements et refus ──────────────────────────────────────────────────────────────

func TestBackendTest257Avertissements(t *testing.T) {
	t.Run("docker sans TLS : root sur l'hôte", func(t *testing.T) {
		u := ub257Start(t)
		ub257DeclareDocker(t, "github-1111", "d", "tcp://93.184.216.34:2376", "")
		raw, _, _ := ub257Test(u, t, "alice")
		if !strings.Contains(raw, "root sur l'hôte") {
			t.Errorf("pas d'avertissement « root sur l'hôte » : %.400s", raw)
		}
	})
	t.Run("TLS vérification désactivée : avertissement", func(t *testing.T) {
		u := ub257Start(t)
		ub257DeclareDocker(t, "github-1111", "d", "tcp://93.184.216.34:2376", "    tls:\n      enabled: true\n      verify: false\n")
		raw, _, _ := ub257Test(u, t, "alice")
		if !strings.Contains(raw, "root sur l'hôte") && !strings.Contains(strings.ToLower(raw), "non vérifi") {
			t.Errorf("pas d'avertissement TLS non vérifié : %.400s", raw)
		}
	})
	t.Run("volume absent : refus", func(t *testing.T) {
		u := ub257Start(t)
		ub257DeclareDocker(t, "github-1111", "d", "tcp://93.184.216.34:2376", "")
		_ = os.WriteFile(filepath.Join(u.dir, "novolume"), nil, 0o644)
		raw, _, _ := ub257Test(u, t, "alice")
		if !strings.Contains(raw, `"ok":false`) || !strings.Contains(strings.ToLower(raw), "volume") {
			t.Errorf("volume absent non signalé : %.400s", raw)
		}
	})
	t.Run("PVC absent : refus", func(t *testing.T) {
		u := ub257Start(t)
		ub16Declare(t, "github-1111", "mon-k8s")
		sp, _ := core.UserSpace("github-1111")
		_ = os.WriteFile(sp.BackendsPath(), []byte("backends:\n  mon-k8s:\n    type: k8s\n    namespace: ns\n    pvc: projets\n    kubeconfig: true\n"), 0o600)
		_, res, e := u.call(t, "alice", "backendSecretSet", map[string]any{"backend": "mon-k8s", "kubeconfig": ub257Kube("https://93.184.216.34:6443", "", "")})
		if e != "" || res["ok"] != true {
			t.Fatalf("kubeconfig : %v %q", res, e)
		}
		_ = os.WriteFile(filepath.Join(u.dir, "nopvc"), nil, 0o644)
		raw, _, _ := u.call(t, "alice", "backendTest", map[string]any{"backend": map[string]any{"name": "mon-k8s", "type": "k8s", "namespace": "ns", "pvc": "projets", "kubeconfig": true}})
		if !strings.Contains(raw, `"ok":false`) || !strings.Contains(strings.ToLower(raw), "pvc") {
			t.Errorf("PVC absent non signalé : %.400s", raw)
		}
	})
	t.Run("compte k8s trop puissant : avertissement et Role minimale", func(t *testing.T) {
		u := ub257Start(t)
		ub16Declare(t, "github-1111", "mon-k8s")
		_, res, e := u.call(t, "alice", "backendSecretSet", map[string]any{"backend": "mon-k8s", "kubeconfig": ub257Kube("https://93.184.216.34:6443", "", "")})
		if e != "" || res["ok"] != true {
			t.Fatalf("kubeconfig : %v %q", res, e)
		}
		raw, _, _ := u.call(t, "alice", "backendTest", map[string]any{"backend": map[string]any{"name": "mon-k8s", "type": "k8s", "namespace": "ns", "kubeconfig": true}})
		if !strings.Contains(strings.ToLower(raw), "plus puissant") || !strings.Contains(raw, "Role") {
			t.Errorf("pas d'avertissement « plus puissant que nécessaire » avec la Role minimale : %.500s", raw)
		}
		if strings.Contains(raw, uCanary) {
			t.Error("canari dans le résultat de backendTest")
		}
	})
}

// ssh (P13) par l'API web : refusé à l'enregistrement d'un utilisateur, message clair.
func TestSSH257RefuseALEnregistrement(t *testing.T) {
	u := ub257Start(t)
	_, res, e := u.call(t, "alice", "configSave", map[string]any{"kind": "backends.yaml", "content": "backends:\n  s:\n    type: ssh\n    host: srv1\n    shared: [/data]\n"})
	if msg, ok := ub257Refusal(res, e); !ok || res["saved"] == true || !strings.Contains(strings.ToLower(msg), "ssh") {
		t.Errorf("ssh accepté ou refus peu clair : %v %q", res, e)
	}
}

func ub257JSON(v any) string { b, _ := json.Marshal(v); return string(b) }
