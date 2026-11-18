package core

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func secretsEnv(t *testing.T) string {
	t.Helper()
	d := filepath.Join(t.TempDir(), "secrets")
	t.Setenv(SecretsDirVar, d)
	t.Setenv("CL_ROOT_BASE", "")
	return d
}

func TestSecretsPaireEtLecture(t *testing.T) {
	d := secretsEnv(t)
	priv, pub, err := generateKeyPair("test")
	if err != nil {
		t.Fatal(err)
	}
	// clé collée depuis Windows : CRLF
	crlf := strings.ReplaceAll(string(priv), "\n", "\r\n")
	fp, gotPub, err := SecretSetKey("srv1", crlf, string(pub), false, SecretWrite{Source: "test"})
	if err != nil || !strings.HasPrefix(fp, "SHA256:") || gotPub == "" {
		t.Fatalf("enregistrement : %v %q", err, fp)
	}
	if runtime.GOOS != "windows" {
		for p, m := range map[string]os.FileMode{d: 0o700, filepath.Join(d, "srv1"): 0o700, filepath.Join(d, "srv1", "id"): 0o600} {
			if st, err := os.Stat(p); err != nil || st.Mode().Perm() != m {
				t.Errorf("%s : droits %v (attendu %o)", p, st, m)
			}
		}
	}
	info, err := SecretGet("srv1")
	if err != nil || !info.HasKey || info.Fingerprint != fp || info.KeyModified == "" {
		t.Fatalf("SecretGet : %+v %v", info, err)
	}
	b, _ := json.Marshal(info)
	if strings.Contains(string(b), "PRIVATE KEY") || strings.Contains(string(b), strings.Split(strings.TrimSpace(string(priv)), "\n")[1]) {
		t.Error("la clé privée ne doit jamais être rendue")
	}
	// journal : l'empreinte, pas le contenu
	j, _ := os.ReadFile(filepath.Join(d, secretJournal))
	if !strings.Contains(string(j), fp) || strings.Contains(string(j), "PRIVATE KEY") || !strings.Contains(string(j), `"source":"test"`) {
		t.Errorf("journal : %s", j)
	}
	// suppression
	if err := SecretDelete("srv1", "all", SecretWrite{Source: "test"}); err != nil {
		t.Fatal(err)
	}
	if info, _ := SecretGet("srv1"); info.HasKey {
		t.Error("clé encore présente après suppression")
	}
}

func TestSecretsRefus(t *testing.T) {
	secretsEnv(t)
	priv, pub, _ := generateKeyPair("a")
	_, pub2, _ := generateKeyPair("b")
	if _, _, err := SecretSetKey("srv", string(priv), string(pub2), false, SecretWrite{}); err == nil || !strings.Contains(err.Error(), "ne correspond pas") {
		t.Errorf("paire discordante : %v", err)
	}
	// phrase de passe
	_, sk, _ := ed25519.GenerateKey(rand.Reader)
	blk, err := ssh.MarshalPrivateKeyWithPassphrase(sk, "", []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := SecretSetKey("srv", string(pem.EncodeToMemory(blk)), string(pub), false, SecretWrite{}); err == nil || !strings.Contains(err.Error(), "phrase de passe") {
		t.Errorf("phrase de passe : %v", err)
	}
	for _, n := range []string{"../x", "a/b", "", ".", "-x"} {
		if _, err := SecretGet(n); err == nil {
			t.Errorf("nom %q accepté", n)
		}
	}
}

func TestSecretsDossierRefuse(t *testing.T) {
	root := t.TempDir()
	if err := checkSecretsDir(filepath.Join(root, "sec"), []string{root}); err == nil {
		t.Error("coffre sous la racine partagée accepté")
	}
	if err := checkSecretsDir(filepath.Join(t.TempDir(), "sec"), []string{root}); err != nil {
		t.Errorf("coffre hors zone partagée refusé : %v", err)
	}
	if runtime.GOOS != "windows" {
		d := filepath.Join(t.TempDir(), "large")
		_ = os.Mkdir(d, 0o755)
		_ = os.Chmod(d, 0o755)
		if err := checkSecretsDir(d, nil); err == nil {
			t.Error("coffre en 0755 accepté")
		}
	}
	t.Setenv("CL_ROOT_BASE", root)
	t.Setenv(SecretsDirVar, filepath.Join(root, "sec"))
	if _, err := SecretsDir(); err == nil {
		t.Error("SecretsDir sous CL_ROOT_BASE accepté")
	}
	if _, err := os.Stat(filepath.Join(root, "sec")); err == nil {
		t.Error("rien ne doit être créé en cas de refus")
	}
}

func TestSecretsKnownHostsEtScan(t *testing.T) {
	secretsEnv(t)
	pk, _, _ := ed25519.GenerateKey(rand.Reader)
	spk, _ := ssh.NewPublicKey(pk)
	line := "host1 " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(spk)))
	if n, err := SecretSetKnownHosts("srv", line+"\n# c\n", SecretWrite{Source: "t"}); err != nil || n != 1 {
		t.Fatalf("known_hosts : %d %v", n, err)
	}
	if _, err := SecretSetKnownHosts("srv", "@cert-authority * "+strings.SplitN(line, " ", 2)[1], SecretWrite{}); err == nil {
		t.Error("marqueur @cert-authority accepté")
	}
	if _, err := SecretSetKnownHosts("srv", "n'importe quoi", SecretWrite{}); err == nil {
		t.Error("ligne illisible acceptée")
	}
	info, _ := SecretGet("srv")
	if len(info.KnownHosts) != 1 || !strings.HasPrefix(info.KnownHosts[0].Fingerprint, "SHA256:") {
		t.Errorf("empreintes : %+v", info.KnownHosts)
	}
	old := keyscan
	defer func() { keyscan = old }()
	var gh, gp string
	keyscan = func(h, p string) ([]byte, error) { gh, gp = h, p; return []byte(line + "\n"), nil }
	es, err := HostScan("user@192.168.1.210:2222")
	if err != nil || len(es) != 1 || gh != "192.168.1.210" || gp != "2222" {
		t.Errorf("HostScan : %v %v %q %q", es, err, gh, gp)
	}
	if _, err := HostScan("-oProxyCommand=x"); err == nil {
		t.Error("destination « -o… » acceptée")
	}
}

func TestSshArgsAvecCle(t *testing.T) {
	d := secretsEnv(t)
	if a := sshBaseArgs("srv", "10"); strings.Contains(strings.Join(a, " "), "IdentitiesOnly") {
		t.Errorf("sans clé enregistrée : options du coffre : %v", a)
	}
	priv, pub, _ := generateKeyPair("a")
	if _, _, err := SecretSetKey("srv", string(priv), string(pub), false, SecretWrite{}); err != nil {
		t.Fatal(err)
	}
	a := strings.Join(sshBaseArgs("srv", "10"), " ")
	for _, w := range []string{"-i " + filepath.Join(d, "srv", "id"), "IdentitiesOnly=yes", "UserKnownHostsFile=" + filepath.Join(d, "srv", "known_hosts"), "StrictHostKeyChecking=yes", "BatchMode=yes"} {
		if !strings.Contains(a, w) {
			t.Errorf("option %q absente : %s", w, a)
		}
	}
}
