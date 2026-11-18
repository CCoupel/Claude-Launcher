package desktop

// Tests de spécification #194 (v6.4.0) — contracts/backends-remote.md : coffre des secrets, clés ssh d'un backend,
// known_hosts confirmé, règle d'écriture provisoire. Boîte noire : seuls les appels (Dispatch, comme /api/call et
// la liaison Wails) et les fichiers du coffre sont observés ; aucune fonction interne n'est nommée.
// Écrits avant l'implémentation (TDD) : RED attendu tant que backendSecret* n'existent pas.
// Aucun test ne touche le vrai ~ : sandbox() + CL_SECRETS_DIR temporaire ; ssh et ssh-keyscan sont de faux scripts.

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

type pair194 struct{ priv, pub, fp, privBody string }

func newPair194(t *testing.T) pair194 {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	blk, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	sp, _ := ssh.NewPublicKey(pub)
	pem := string(pemEncode194(blk))
	return pair194{priv: pem, pub: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sp))), fp: ssh.FingerprintSHA256(sp), privBody: bodyOf194(pem)}
}

func pemEncode194(b *pem.Block) []byte { return pem.EncodeToMemory(b) }

func marshalPEM194(b *pem.Block) string { return string(pem.EncodeToMemory(b)) }

// bodyOf194 : première ligne de base64 du corps de la clé (ce qui ne doit jamais fuiter).
func bodyOf194(pem string) string {
	ls := strings.Split(strings.TrimSpace(pem), "\n")
	if len(ls) < 3 {
		return pem
	}
	return strings.TrimSpace(ls[1])
}

// vault194 : poste isolé, coffre 0700 hors de la racine partagée, backend ssh « srv1 » déclaré.
func vault194(t *testing.T) (a *App, dir string) {
	t.Helper()
	root := t.TempDir()
	sandbox(t, root, "alpha")
	t.Setenv("CL_ROOT_BASE", filepath.Join(root, "partage"))
	_ = os.MkdirAll(filepath.Join(root, "partage"), 0o755)
	dir = filepath.Join(root, "coffre")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CL_SECRETS_DIR", dir)
	cfg := filepath.Join(root, "cl_config_dir")
	if err := os.WriteFile(filepath.Join(cfg, "backends.yaml"), []byte("backends:\n  srv1: { type: ssh, host: 192.168.1.210 }\n  srv2: { type: ssh, host: 192.168.1.211 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return newApp(t, (&recorder{}).emit), dir
}

// call194 : résultat décodé en map ; failed = erreur d'appel ou ok:false.
func call194(t *testing.T, a *App, method string, args any) (res map[string]any, raw string, failed bool) {
	t.Helper()
	b, _ := json.Marshal(args)
	out, err := a.Dispatch(method, b)
	if err != nil {
		return nil, err.Error(), true
	}
	rb, _ := json.Marshal(out)
	raw = string(rb)
	_ = json.Unmarshal(rb, &res)
	ok, has := res["ok"].(bool)
	return res, raw, has && !ok
}

func mustOK194(t *testing.T, a *App, method string, args any) (map[string]any, string) {
	t.Helper()
	res, raw, failed := call194(t, a, method, args)
	if failed {
		t.Fatalf("%s refusé : %s", method, raw)
	}
	return res, raw
}

// wantMotif194 : le refus porte le motif attendu (insensible à la casse) ; un refus pour une autre raison échoue.
func wantMotif194(t *testing.T, raw string, motifs ...string) {
	t.Helper()
	l := strings.ToLower(raw)
	for _, m := range motifs {
		if !strings.Contains(l, strings.ToLower(m)) {
			t.Errorf("le refus ne porte pas le motif %q : %s", m, raw)
		}
	}
}

func noFiles194(t *testing.T, dir, backend string) {
	t.Helper()
	for _, f := range []string{"id", "id.pub", "known_hosts"} {
		if _, err := os.Stat(filepath.Join(dir, backend, f)); err == nil {
			t.Errorf("%s/%s écrit alors que l'appel devait être refusé", backend, f)
		}
	}
}

func TestSecretSetPaireValide194(t *testing.T) {
	a, dir := vault194(t)
	p := newPair194(t)
	res, raw := mustOK194(t, a, "backendSecretSet", map[string]any{"backend": "srv1", "privateKey": p.priv, "publicKey": p.pub})
	if res["fingerprint"] != p.fp {
		t.Errorf("empreinte = %v, attendu %s", res["fingerprint"], p.fp)
	}
	if strings.Contains(raw, p.privBody) || strings.Contains(raw, "PRIVATE KEY") {
		t.Errorf("la réponse de Set contient la clé privée : %s", raw)
	}
	for f, want := range map[string]os.FileMode{"id": 0o600, "id.pub": 0o600} {
		st, err := os.Stat(filepath.Join(dir, "srv1", f))
		if err != nil {
			t.Fatalf("%s absent : %v", f, err)
		}
		if runtime.GOOS != "windows" && st.Mode().Perm() != want {
			t.Errorf("%s : droits %v, attendu %v", f, st.Mode().Perm(), want)
		}
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(filepath.Join(dir, "srv1")); st.Mode().Perm() != 0o700 {
			t.Errorf("dossier du backend : droits %v, attendu 0700", st.Mode().Perm())
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "srv1", "id")); !strings.Contains(string(b), p.privBody) {
		t.Error("la clé privée n'est pas enregistrée dans le coffre")
	}
}

func TestSecretGetJamaisLaClePrivee194(t *testing.T) {
	a, _ := vault194(t)
	p := newPair194(t)
	mustOK194(t, a, "backendSecretSet", map[string]any{"backend": "srv1", "privateKey": p.priv, "publicKey": p.pub})
	res, raw := mustOK194(t, a, "backendSecretGet", map[string]any{"backend": "srv1"})
	if res["hasKey"] != true || res["fingerprint"] != p.fp || res["publicKey"] != p.pub {
		t.Errorf("Get : %v", res)
	}
	if strings.Contains(raw, p.privBody) || strings.Contains(raw, "PRIVATE KEY") {
		t.Errorf("Get renvoie la clé privée : %s", raw)
	}
	if res2, _ := mustOK194(t, a, "backendSecretGet", map[string]any{"backend": "srv2"}); res2["hasKey"] != false {
		t.Errorf("srv2 sans clé : hasKey = %v", res2["hasKey"])
	}
}

func TestSecretPaireIncoherenteRefusee194(t *testing.T) {
	a, dir := vault194(t)
	p1, p2 := newPair194(t), newPair194(t)
	_, raw, failed := call194(t, a, "backendSecretSet", map[string]any{"backend": "srv1", "privateKey": p1.priv, "publicKey": p2.pub})
	if !failed {
		t.Errorf("clé publique d'une autre paire acceptée : %s", raw)
	}
	wantMotif194(t, raw, "ne correspond pas") // le motif : l'incohérence de la paire, pas une autre erreur
	noFiles194(t, dir, "srv1")
}

func TestSecretPhraseDePasseRefusee194(t *testing.T) {
	a, dir := vault194(t)
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	blk, err := ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	sp, _ := ssh.NewPublicKey(priv.Public())
	pub := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sp)))
	res, raw, failed := call194(t, a, "backendSecretSet", map[string]any{"backend": "srv1", "privateKey": marshalPEM194(blk), "publicKey": pub})
	if !failed {
		t.Errorf("clé protégée par une phrase de passe acceptée : %s", raw)
	}
	if msg := strings.ToLower(fmt.Sprint(res["msg"]) + raw); !strings.Contains(msg, "phrase") {
		t.Errorf("le refus doit nommer la phrase de passe : %s", raw)
	}
	noFiles194(t, dir, "srv1")
}

func TestSecretClePriveeIllisibleRefusee194(t *testing.T) {
	a, dir := vault194(t)
	p := newPair194(t)
	_, raw, failed := call194(t, a, "backendSecretSet", map[string]any{"backend": "srv1", "privateKey": "pas une clé", "publicKey": p.pub})
	if !failed {
		t.Errorf("texte quelconque accepté comme clé privée : %s", raw)
	}
	wantMotif194(t, raw, "illisible") // le motif : clé privée illisible (ni phrase de passe, ni paire incohérente)
	if strings.Contains(strings.ToLower(raw), "phrase") || strings.Contains(raw, "ne correspond pas") {
		t.Errorf("mauvais motif pour une clé illisible : %s", raw)
	}
	noFiles194(t, dir, "srv1")
}

func TestSecretGenerer194(t *testing.T) {
	a, dir := vault194(t)
	res, raw := mustOK194(t, a, "backendSecretSet", map[string]any{"backend": "srv1", "generate": true})
	pub, _ := res["publicKey"].(string)
	if !strings.HasPrefix(pub, "ssh-ed25519 ") {
		t.Errorf("clé publique générée = %q, attendu ssh-ed25519", pub)
	}
	if !strings.HasPrefix(fmt.Sprint(res["fingerprint"]), "SHA256:") {
		t.Errorf("empreinte = %v", res["fingerprint"])
	}
	if strings.Contains(raw, "PRIVATE KEY") {
		t.Errorf("seule la clé publique est montrée : %s", raw)
	}
	b, err := os.ReadFile(filepath.Join(dir, "srv1", "id"))
	if err != nil {
		t.Fatalf("clé privée non enregistrée : %v", err)
	}
	if _, err := ssh.ParsePrivateKey(b); err != nil {
		t.Errorf("la clé générée n'est pas lisible sans phrase de passe : %v", err)
	}
}

func TestSecretRemplacementEntierSansResidus194(t *testing.T) {
	a, dir := vault194(t)
	p1, p2 := newPair194(t), newPair194(t)
	mustOK194(t, a, "backendSecretSet", map[string]any{"backend": "srv1", "privateKey": p1.priv, "publicKey": p1.pub})
	mustOK194(t, a, "backendSecretSet", map[string]any{"backend": "srv1", "privateKey": p2.priv, "publicKey": p2.pub})
	res, _ := mustOK194(t, a, "backendSecretGet", map[string]any{"backend": "srv1"})
	if res["fingerprint"] != p2.fp {
		t.Errorf("après remplacement : %v, attendu %s", res["fingerprint"], p2.fp)
	}
	ents, _ := os.ReadDir(filepath.Join(dir, "srv1"))
	for _, e := range ents { // écriture atomique : aucun temporaire oublié
		if e.Name() != "id" && e.Name() != "id.pub" && e.Name() != "known_hosts" {
			t.Errorf("fichier résiduel dans le coffre : %s", e.Name())
		}
	}
}

func TestSecretSupprimer194(t *testing.T) {
	a, dir := vault194(t)
	p := newPair194(t)
	mustOK194(t, a, "backendSecretSet", map[string]any{"backend": "srv1", "privateKey": p.priv, "publicKey": p.pub})
	line := "192.168.1.210 " + string(ssh.MarshalAuthorizedKey(mustPub194(t)))
	mustOK194(t, a, "backendKnownHostsSet", map[string]any{"backend": "srv1", "lines": line})

	mustOK194(t, a, "backendSecretDelete", map[string]any{"backend": "srv1", "what": "known_hosts"})
	if _, err := os.Stat(filepath.Join(dir, "srv1", "known_hosts")); err == nil {
		t.Error("known_hosts non supprimé")
	}
	if _, err := os.Stat(filepath.Join(dir, "srv1", "id")); err != nil {
		t.Error("« known_hosts » ne doit pas effacer la clé")
	}
	mustOK194(t, a, "backendKnownHostsSet", map[string]any{"backend": "srv1", "lines": line})
	mustOK194(t, a, "backendSecretDelete", map[string]any{"backend": "srv1", "what": "all"})
	noFiles194(t, dir, "srv1")
	if _, _, failed := call194(t, a, "backendSecretDelete", map[string]any{"backend": "srv1", "what": "tout"}); !failed {
		t.Error("« what » inconnu accepté")
	}
}

func mustPub194(t *testing.T) ssh.PublicKey {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	k, _ := ssh.NewPublicKey(pub)
	return k
}

func TestKnownHostsConfirmeEtValide194(t *testing.T) {
	a, dir := vault194(t)
	k := mustPub194(t)
	line := "192.168.1.210 " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(k)))
	mustOK194(t, a, "backendKnownHostsSet", map[string]any{"backend": "srv1", "lines": line})
	res, _ := mustOK194(t, a, "backendSecretGet", map[string]any{"backend": "srv1"})
	kh, _ := res["knownHosts"].([]any)
	if len(kh) != 1 {
		t.Fatalf("knownHosts = %v", res["knownHosts"])
	}
	if e, _ := kh[0].(map[string]any); e["fingerprint"] != ssh.FingerprintSHA256(k) || e["type"] != "ssh-ed25519" {
		t.Errorf("entrée = %v", kh[0])
	}
	if _, raw, failed := call194(t, a, "backendKnownHostsSet", map[string]any{"backend": "srv2", "lines": "n'importe quoi"}); !failed {
		t.Errorf("ligne known_hosts invalide acceptée : %s", raw)
	}
	noFiles194(t, dir, "srv2")
}

// backendHostScan relève les empreintes sans rien enregistrer : l'enregistrement exige la confirmation explicite
// (backendKnownHostsSet) — jamais d'acceptation silencieuse.
func TestHostScanNEnregistreRien194(t *testing.T) {
	unixOnly194(t)
	a, dir := vault194(t)
	k := mustPub194(t)
	bin := t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\necho '192.168.1.210 %s'\n", strings.TrimSpace(string(ssh.MarshalAuthorizedKey(k))))
	if err := os.WriteFile(filepath.Join(bin, "ssh-keyscan"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	res, raw := mustOK194(t, a, "backendHostScan", map[string]any{"backend": "srv1"})
	keys, _ := res["keys"].([]any)
	if len(keys) == 0 {
		t.Fatalf("aucune empreinte relevée : %s", raw)
	}
	if e, _ := keys[0].(map[string]any); e["fingerprint"] != ssh.FingerprintSHA256(k) || e["line"] == "" || e["type"] != "ssh-ed25519" {
		t.Errorf("clé relevée = %v", keys[0])
	}
	noFiles194(t, dir, "srv1")
}

func unixOnly194(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("faux scripts shell : Linux seulement")
	}
}

// ── Coffre : emplacements refusés ─────────────────────────────────────────

func TestCoffreEmplacementsRefuses194(t *testing.T) {
	p := newPair194(t)
	for _, c := range []struct {
		name  string
		motif string                                 // fragment du message de refus
		prep  func(t *testing.T, root string) string // renvoie CL_SECRETS_DIR
	}{
		{"sous la racine partagée CL_ROOT_BASE", "zone partagée", func(t *testing.T, root string) string {
			d := filepath.Join(root, "partage", "coffre")
			_ = os.MkdirAll(d, 0o700)
			return d
		}},
		{"sous un volume de CL_K8S_VOLUMES", "zone partagée", func(t *testing.T, root string) string {
			v := filepath.Join(root, "vol")
			_ = os.MkdirAll(filepath.Join(v, "coffre"), 0o700)
			t.Setenv("CL_K8S_VOLUMES", v+"=claude-projets")
			return filepath.Join(v, "coffre")
		}},
		{"droits ≠ 0700", "droits", func(t *testing.T, root string) string {
			if runtime.GOOS == "windows" {
				t.Skip("droits POSIX")
			}
			d := filepath.Join(root, "ouvert")
			_ = os.MkdirAll(d, 0o755)
			_ = os.Chmod(d, 0o755)
			return d
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, _ := vault194(t)
			root := filepath.Dir(os.Getenv("CL_SECRETS_DIR"))
			d := c.prep(t, root)
			t.Setenv("CL_SECRETS_DIR", d)
			_, raw, failed := call194(t, a, "backendSecretSet", map[string]any{"backend": "srv1", "privateKey": p.priv, "publicKey": p.pub})
			if !failed {
				t.Errorf("écriture acceptée dans un coffre interdit (%s) : %s", c.name, raw)
			}
			wantMotif194(t, raw, "coffre", c.motif)
			noFiles194(t, d, "srv1")
		})
	}
}

func TestCoffreNomDeBackendInvalide194(t *testing.T) {
	a, dir := vault194(t)
	p := newPair194(t)
	for _, name := range []string{"../evasion", "a/b", "", "..", "inconnu"} {
		if _, raw, failed := call194(t, a, "backendSecretSet", map[string]any{"backend": name, "privateKey": p.priv, "publicKey": p.pub}); !failed {
			t.Errorf("backend %q accepté : %s", name, raw)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "evasion")); err == nil {
		t.Error("écriture hors du coffre")
	}
}

// ── Journal sans contenu, aucune copie ailleurs ───────────────────────────

func TestJournalSansContenu194(t *testing.T) {
	a, dir := vault194(t)
	root := filepath.Dir(dir)
	p := newPair194(t)
	mustOK194(t, a, "backendSecretSet", map[string]any{"backend": "srv1", "privateKey": p.priv, "publicKey": p.pub})
	journal := false
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasPrefix(path, dir+string(filepath.Separator)) {
			return nil
		}
		b, _ := os.ReadFile(path)
		s := string(b)
		if strings.Contains(s, p.privBody) || strings.Contains(s, "PRIVATE KEY") {
			t.Errorf("la clé privée est copiée hors du coffre : %s", path)
		}
		if strings.Contains(s, p.fp) && strings.Contains(s, "srv1") {
			journal = true
		}
		return nil
	})
	if !journal {
		t.Error("aucune trace journalisée de l'écriture (backend + empreinte), hors du coffre")
	}
}

// ── La règle d'écriture est la même pour tous les clients ─────────────────

func TestEcritureAutoriseeSansAuthentification194(t *testing.T) {
	// règle provisoire v6.4.0 : l'appel aboutit sans jeton ni session, quel que soit le client (fenêtre native,
	// client web du hub, ingress interne). Le même Dispatch sert ces trois voies.
	a, _ := vault194(t)
	p := newPair194(t)
	mustOK194(t, a, "backendSecretSet", map[string]any{"backend": "srv1", "privateKey": p.priv, "publicKey": p.pub})
}

// ── Utilisation par le backend ssh ────────────────────────────────────────

// fakeSSH194 : un faux ssh qui journalise ses arguments et réussit.
func fakeSSH194(t *testing.T) (log string) {
	unixOnly194(t)
	bin, log := t.TempDir(), filepath.Join(t.TempDir(), "argv.log")
	script := fmt.Sprintf("#!/bin/sh\necho \"$*\" >> %s\nexit 0\n", log)
	for _, n := range []string{"ssh"} {
		if err := os.WriteFile(filepath.Join(bin, n), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	return log
}

func TestSSHUtiliseLaCleDuCoffre194(t *testing.T) {
	log := fakeSSH194(t)
	a, dir := vault194(t)
	p := newPair194(t)
	mustOK194(t, a, "backendSecretSet", map[string]any{"backend": "srv1", "privateKey": p.priv, "publicKey": p.pub})
	k := mustPub194(t)
	mustOK194(t, a, "backendKnownHostsSet", map[string]any{"backend": "srv1", "lines": "192.168.1.210 " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(k)))})

	res, raw := mustOK194(t, a, "backendTest", map[string]any{"backend": map[string]any{"name": "srv1", "type": "ssh", "host": "192.168.1.210"}})
	b, _ := os.ReadFile(log)
	argv := string(b)
	if argv == "" {
		t.Fatalf("ssh jamais appelé : %s", raw)
	}
	for _, want := range []string{
		"-i " + filepath.Join(dir, "srv1", "id"), "IdentitiesOnly=yes", "StrictHostKeyChecking=yes",
		"UserKnownHostsFile=" + filepath.Join(dir, "srv1", "known_hosts"), "BatchMode=yes",
	} {
		if !strings.Contains(argv, want) {
			t.Errorf("argument ssh absent : %q\nargv : %s", want, argv)
		}
	}
	if strings.Contains(argv, "StrictHostKeyChecking=no") || strings.Contains(argv, "accept-new") {
		t.Errorf("acceptation silencieuse d'une empreinte : %s", argv)
	}
	labels := strings.ToLower(raw)
	for _, step := range []string{"clé ssh enregistrée", "empreinte"} {
		if !strings.Contains(labels, step) {
			t.Errorf("étape « %s » absente de « Tester » : %v", step, res["steps"])
		}
	}
}

func TestSSHSansCleComportementV630194(t *testing.T) {
	log := fakeSSH194(t)
	a, _ := vault194(t)
	mustOK194(t, a, "backendTest", map[string]any{"backend": map[string]any{"name": "srv2", "type": "ssh", "host": "192.168.1.211"}})
	b, _ := os.ReadFile(log)
	if strings.Contains(string(b), "-i ") || strings.Contains(string(b), "IdentitiesOnly") || strings.Contains(string(b), "UserKnownHostsFile") {
		t.Errorf("sans clé enregistrée, ssh doit rester comme en v6.3.0 (agent, ~/.ssh) : %s", b)
	}
}
