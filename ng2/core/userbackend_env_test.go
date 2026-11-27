package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// LOT-3.1 Batch 2 (#257) — environnement et certificats des appels d'un UTILISATEUR (S10, S29, S32 ; contrat §3 « Appels d'un
// utilisateur »). Faux docker / kubectl qui journalisent leur environnement et les fichiers de certificats qu'ils reçoivent.
// Écrits avant le code, par le comportement (Space.LoadBackends → Engine.cmd / kcmd).

var s257Polluted = []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "all_proxy", "no_proxy",
	"DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_CONFIG", "DOCKER_CERT_PATH", "DOCKER_TLS_VERIFY", "DOCKER_TLS", "CONTAINER_HOST",
	"CONTAINER_CONNECTION", "KUBECONFIG", "KUBECTL_PROXY"}

type s257Fake struct{ log, dir string }

// s257Tools : faux docker et kubectl ; chacun écrit « ENV: NOM=valeur » pour les variables suspectes, puis, pour chaque fichier
// existant reçu en argument ou dans DOCKER_CERT_PATH, « FILE: chemin mode=… » avec la 1re ligne.
func s257Tools(t *testing.T) s257Fake {
	t.Helper()
	unixOnly(t)
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	body := `#!/bin/sh
echo "ARGV: $*" >> "` + log + `"
env | grep -E '^(HTTP_PROXY|HTTPS_PROXY|ALL_PROXY|NO_PROXY|http_proxy|https_proxy|all_proxy|no_proxy|DOCKER_[A-Z_]*|CONTAINER_[A-Z_]*|KUBECONFIG|KUBECTL_[A-Z_]*)=' | sed 's/^/ENV: /' >> "` + log + `"
for a in "$@"; do
  v="${a#*=}"
  for p in "$a" "$v"; do
    if [ -f "$p" ]; then echo "FILE: $p mode=$(stat -c %a "$p") first=$(head -n1 "$p")" >> "` + log + `"; fi
  done
done
if [ -n "$DOCKER_CERT_PATH" ] && [ -d "$DOCKER_CERT_PATH" ]; then
  for f in "$DOCKER_CERT_PATH"/*; do [ -f "$f" ] && echo "FILE: $f mode=$(stat -c %a "$f") first=$(head -n1 "$f")" >> "` + log + `"; done
fi
echo "27.0"
`
	for _, n := range []string{"docker", "podman", "kubectl"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, v := range s257Polluted {
		t.Setenv(v, "POLLUE-"+v)
	}
	return s257Fake{log: log, dir: dir}
}

func (f s257Fake) text() string { b, _ := os.ReadFile(f.log); return string(b) }

func (f s257Fake) files() (paths []string) {
	for _, l := range strings.Split(f.text(), "\n") {
		if strings.HasPrefix(l, "FILE: ") {
			paths = append(paths, strings.Fields(strings.TrimPrefix(l, "FILE: "))[0])
		}
	}
	return paths
}

func s257Docker(t *testing.T, tls bool) (Space, BackendConf) {
	t.Helper()
	y := "backends:\n  d:\n    type: docker\n    host: tcp://93.184.216.34:2376\n    volume: v\n"
	if tls {
		y += "    tls:\n      enabled: true\n      verify: true\n"
	}
	sp := s15Space(t, "github-9", y)
	if tls {
		for _, f := range []string{"ca.pem", "cert.pem", "key.pem"} {
			d := filepath.Join(sp.VaultDir(), "d")
			_ = os.MkdirAll(d, 0o700)
			_ = os.WriteFile(filepath.Join(d, f), []byte("-----BEGIN CERTIFICATE-----\nCANARY-"+f+"\n-----END CERTIFICATE-----\n"), 0o600)
		}
	}
	bs, err := sp.LoadBackends()
	if err != nil || len(bs) == 0 {
		t.Skipf("backend docker de test non chargé (%v) : attendre le code de la déclaration", err)
	}
	return sp, bs[0]
}

func TestEnv257AppelsUtilisateurSansVariablesHeritees(t *testing.T) {
	f := s257Tools(t)
	s15Setup2 := func() { _ = os.Setenv("CL_CONFIG_DIR", t.TempDir()) }
	s15Setup2()
	_, b := s257Docker(t, false)
	if out, err := (Engine{b}).cmd("version").CombinedOutput(); err != nil {
		t.Logf("docker factice : %v %s", err, out)
	}
	log := f.text()
	if !strings.Contains(log, "ARGV:") {
		t.Fatalf("docker factice non lancé :\n%s", log)
	}
	if strings.Contains(log, "POLLUE-") {
		t.Errorf("variable d'environnement héritée transmise à l'appel d'un utilisateur :\n%s", log)
	}
}

func TestEnv257KubectlUtilisateurSansVariablesHeritees(t *testing.T) {
	f := s257Tools(t)
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	t.Setenv("KUBECONFIG", "POLLUE-KUBECONFIG")
	sp := s15Space(t, "github-9", s15Yaml("k"))
	s15Vault(t, sp, "k", s15Kubeconfig("CANARY-env"))
	bs, err := sp.LoadBackends()
	if err != nil || len(bs) == 0 {
		t.Skipf("backend k8s de test non chargé (%v)", err)
	}
	_ = Engine{bs[0]}.kcmd("get", "pods").Run()
	log := f.text()
	if !strings.Contains(log, "ARGV:") {
		t.Fatalf("kubectl factice non lancé :\n%s", log)
	}
	if strings.Contains(log, "POLLUE-") {
		t.Errorf("variable héritée transmise à kubectl d'un utilisateur :\n%s", log)
	}
}

// Certificats TLS d'un backend docker : fichiers temporaires 0600, supprimés APRÈS l'appel ; jamais dans le coffre du poste.
func TestEnv257CertificatsTLSTemporairesSupprimes(t *testing.T) {
	f := s257Tools(t)
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	_, b := s257Docker(t, true)
	if out, err := (Engine{b}).cmd("version").CombinedOutput(); err != nil {
		t.Logf("docker factice : %v %s", err, out)
	}
	log := f.text()
	files := f.files()
	if len(files) == 0 {
		t.Fatalf("aucun fichier de certificat transmis à docker (backend TLS) :\n%s", log)
	}
	for _, l := range strings.Split(log, "\n") {
		if strings.HasPrefix(l, "FILE: ") && !strings.Contains(l, "mode=600") && !strings.Contains(l, "mode=400") {
			t.Errorf("certificat avec des droits trop larges : %s", l)
		}
	}
	for _, p := range files {
		if strings.HasPrefix(p, f.dir) {
			continue // le faux outil lui-même
		}
		if _, err := os.Stat(p); err == nil && !strings.Contains(p, "/vault/") {
			t.Errorf("fichier temporaire de certificat encore présent après l'appel : %s", p)
		}
	}
	if strings.Contains(log, "POLLUE-") {
		t.Errorf("variable héritée transmise :\n%s", log)
	}
}

// Deux appels simultanés : chacun a ses propres fichiers (aucun nom partagé prévisible).
func TestEnv257CertificatsNomsDistincts(t *testing.T) {
	f := s257Tools(t)
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	_, b := s257Docker(t, true)
	_ = (Engine{b}).cmd("version").Run()
	_ = (Engine{b}).cmd("version").Run()
	seen := map[string]int{}
	if len(f.files()) < 2 {
		t.Fatalf("aucun certificat transmis aux deux appels :\n%s", f.text())
	}
	for _, p := range f.files() {
		seen[p]++
	}
	for p, n := range seen {
		if n > 1 && !strings.Contains(p, "/vault/") {
			t.Errorf("même fichier temporaire %s réutilisé par deux appels", p)
		}
	}
}

// Arguments jamais passés par un shell : un nom ou un volume piégé n'est pas interprété (le « volume » est un nom seulement).
func TestEnv257PasDeShell(t *testing.T) {
	s257Tools(t)
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	marker := filepath.Join(t.TempDir(), "pwned")
	for _, vol := range []string{"v; touch " + marker, "$(touch " + marker + ")", "`touch " + marker + "`", "../../etc", "/abs/chemin", "a b"} {
		sp := s15Space(t, "github-9", "backends:\n  d:\n    type: docker\n    host: tcp://93.184.216.34:2376\n    volume: "+yamlQuote(vol)+"\n")
		if ms := errMsgs(sp.ValidateBackends([]byte("backends:\n  d:\n    type: docker\n    host: tcp://93.184.216.34:2376\n    volume: " + yamlQuote(vol) + "\n"))); len(ms) == 0 {
			t.Errorf("volume %q accepté (un nom seulement)", vol)
		}
		if bs, err := sp.LoadBackends(); err == nil && len(bs) > 0 {
			_ = exec.Command("sh", "-c", "true").Run()
			_ = (Engine{bs[0]}).cmd("volume", "inspect", vol).Run()
		}
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("un argument a été interprété par un shell")
	}
}
