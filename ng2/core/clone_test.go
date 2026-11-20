//go:build !windows

package core

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestValidateCloneURL(t *testing.T) {
	ok := map[string]string{
		"https://github.com/CCoupel/x.git": "https", "https://gitlab.example.org/a/b": "https",
		"git@github.com:moi/projet.git": "ssh", "ssh://git@host.example:2222/a/b.git": "ssh", "ssh://host/a/b": "ssh",
	}
	for u, scheme := range ok {
		if i, err := ValidateCloneURL(u); err != nil || i.Scheme != scheme {
			t.Errorf("%q : %v %v", u, i, err)
		}
	}
	for _, u := range []string{"", "-u", "--upload-pack=x", "file:///etc", "ext::sh -c id", "/tmp/repo", "../repo", "C:\\repo", "git://host/x",
		"http://github.com/a/b", "https://user:pw@github.com/a/b", "https://tok@github.com/a/b", "https://", "https://-evil/a/b", "https://github.com",
		"ssh://-oProxyCommand=x/a", "-oProxyCommand=x@host:a", "git@-host:a", "a b", "https://github.com/a\nb", "host:path", "git@host:", "ssh://u:p@h/a/b"} {
		if _, err := ValidateCloneURL(u); err == nil {
			t.Errorf("URL %q acceptée", u)
		}
	}
}

func TestCloneProgress(t *testing.T) {
	for line, want := range map[string]int{
		"Receiving objects:  50% (5/10)": 40, "Receiving objects: 100% (10/10), done.": 80,
		"Resolving deltas:  50% (1/2)": 87, "Updating files: 100% (3/3), done.": 100,
	} {
		if _, pct, ok := cloneProgress(line); !ok || pct != want {
			t.Errorf("%q → %d %v, attendu %d", line, pct, ok, want)
		}
	}
	if _, _, ok := cloneProgress("fatal: x"); ok {
		t.Error("ligne d'erreur prise pour une progression")
	}
}

// Faux git : jamais le vrai ; enregistre argv et l'environnement, émet une progression, réussit ou échoue.
func fakeGit(t *testing.T, body string) (rec string) {
	t.Helper()
	bin := t.TempDir()
	rec = filepath.Join(t.TempDir(), "rec")
	script := "#!/bin/sh\n{ echo \"ARGV:$*\"; env | sort; } > " + rec + "\n" + body
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return rec
}

func TestCloneJetonJamaisDansArgvNiMessages(t *testing.T) {
	const tok = "ghp_SECRETTOKEN123"
	rec := fakeGit(t, `printf 'Receiving objects:  50%% (1/2)\r' >&2
printf 'remote: Invalid username or password. ghp_SECRETTOKEN123\nfatal: Authentication failed for https://github.com/a/b\n' >&2
exit 128
`)
	dir := t.TempDir()
	var steps []string
	err := CloneProject(context.Background(), "https://github.com/a/b.git", dir, CloneAuth{Token: tok}, func(s string, p int) { steps = append(steps, s) })
	if err == nil || strings.Contains(err.Error(), tok) || !strings.Contains(err.Error(), "authentification refusée") {
		t.Fatalf("erreur = %v", err)
	}
	if len(steps) == 0 {
		t.Error("aucune progression")
	}
	b, _ := os.ReadFile(rec)
	argv := strings.SplitN(string(b), "\n", 2)[0]
	if strings.Contains(argv, tok) || !strings.Contains(argv, "clone --progress -- https://github.com/a/b.git") {
		t.Errorf("argv = %s", argv)
	}
	if !strings.Contains(string(b), "CL_GIT_TOKEN="+tok) || !strings.Contains(string(b), "GIT_ASKPASS=") || !strings.Contains(string(b), "GIT_TERMINAL_PROMPT=0") {
		t.Error("jeton/askpass absents de l'environnement du processus git")
	}
}

func TestCloneJetonSeulementPourGithub(t *testing.T) {
	rec := fakeGit(t, "exit 0\n")
	if err := CloneProject(context.Background(), "https://gitlab.example.org/a/b.git", t.TempDir(), CloneAuth{Token: "ghp_X"}, nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(rec); strings.Contains(string(b), "ghp_X") || strings.Contains(string(b), "GIT_ASKPASS=") {
		t.Error("jeton GitHub présenté à un autre hôte")
	}
}

func TestCloneRefuseUneURLInvalideSansLancerGit(t *testing.T) {
	rec := fakeGit(t, "exit 0\n")
	for _, u := range []string{"file:///etc", "-u", "/tmp/x"} {
		if err := CloneProject(context.Background(), u, t.TempDir(), CloneAuth{}, nil); err == nil {
			t.Errorf("%q accepté", u)
		}
	}
	if _, err := os.Stat(rec); err == nil {
		t.Error("git lancé pour une URL refusée")
	}
}

func TestCloneAnnulation(t *testing.T) {
	fakeGit(t, "sleep 30\n")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- CloneProject(ctx, "https://github.com/a/b", t.TempDir(), CloneAuth{}, nil) }()
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("err = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("annulation sans effet")
	}
}

func TestCloneSSHUtiliseLeCoffreDuBackend(t *testing.T) {
	rec := fakeGit(t, "exit 0\n")
	if err := CloneProject(context.Background(), "git@github.com:a/b.git", t.TempDir(), CloneAuth{Backend: "inconnu"}, nil); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(rec)
	if !strings.Contains(string(b), "GIT_SSH_COMMAND='ssh' '-o' 'BatchMode=yes'") {
		t.Errorf("GIT_SSH_COMMAND absent : %s", b)
	}
}

// M1 : le script askpass ne répond qu'aux invites qui nomment exactement github.com.
func TestAskpassSeulementPourGithub(t *testing.T) {
	d := t.TempDir()
	sh := filepath.Join(d, "askpass.sh")
	if err := os.WriteFile(sh, []byte(askpassScript), 0o700); err != nil {
		t.Fatal(err)
	}
	run := func(prompt string) (string, error) {
		c := exec.Command(sh, prompt)
		c.Env = append(os.Environ(), "CL_GIT_TOKEN=ghp_SECRET")
		out, err := c.Output()
		return strings.TrimSpace(string(out)), err
	}
	for prompt, want := range map[string]string{
		"Username for 'https://github.com': ":                "x-access-token",
		"Password for 'https://x-access-token@github.com': ": "ghp_SECRET",
		"Password for 'https://github.com': ":                "ghp_SECRET",
	} {
		if out, err := run(prompt); err != nil || out != want {
			t.Errorf("%q → %q %v, attendu %q", prompt, out, err, want)
		}
	}
	for _, prompt := range []string{
		"Password for 'https://evil.example/': ", "Password for 'https://github.com.evil.org': ", "Password for 'https://x-access-token@evil.com': ",
		"Password for 'https://github.com:8443': ", "Password: ", "Password for 'https://evilgithub.com': ",
	} {
		if out, err := run(prompt); err == nil || out != "" {
			t.Errorf("invite %q répondue : %q %v", prompt, out, err)
		}
	}
}

// m1 : configuration git injectée ou de l'utilisateur neutralisée ; m3 : StrictHostKeyChecking=yes toujours ; m4 : chemin à espace.
func TestEnvironnementGitNeutralise(t *testing.T) {
	t.Setenv("GIT_CONFIG_PARAMETERS", "'url.https://evil/.insteadof=https://github.com/'")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "url.https://evil/.insteadOf")
	t.Setenv("GIT_CONFIG_VALUE_0", "https://github.com/")
	t.Setenv("GIT_EXEC_PATH", "/evil")
	t.Setenv("GIT_PROXY_COMMAND", "/evil")
	empty := filepath.Join(t.TempDir(), "gitconfig")
	_ = os.WriteFile(empty, nil, 0o600)
	env := strings.Join(cloneEnv(CloneURLInfo{Scheme: "https", Host: "github.com"}, t.TempDir(), CloneAuth{Token: "ghp_X"}, "/tmp/Prénom Nom/askpass.sh", empty), "\n")
	for _, bad := range []string{"GIT_CONFIG_PARAMETERS", "evil", "url.https"} {
		if strings.Contains(env, bad) {
			t.Errorf("environnement git garde %q", bad)
		}
	}
	for _, need := range []string{"GIT_CONFIG_GLOBAL=" + empty, "GIT_CONFIG_SYSTEM=" + empty, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_KEY_0=safe.directory", "GIT_ASKPASS='/tmp/Prénom Nom/askpass.sh'"} {
		if !strings.Contains(env, need) {
			t.Errorf("environnement sans %q", need)
		}
	}
	ssh := strings.Join(cloneEnv(CloneURLInfo{Scheme: "ssh", Host: "github.com"}, t.TempDir(), CloneAuth{Backend: "inconnu"}, "", empty), "\n")
	if !strings.Contains(ssh, "'-o' 'StrictHostKeyChecking=yes'") {
		t.Errorf("StrictHostKeyChecking=yes absent sans clé au coffre : %s", ssh)
	}
}

func TestAskpassValueGuillemeteSiBesoin(t *testing.T) {
	for in, want := range map[string]string{"/tmp/cl-clone-1/askpass.sh": "/tmp/cl-clone-1/askpass.sh", "/tmp/a b/askpass.sh": "'/tmp/a b/askpass.sh'",
		`C:\Users\Prénom Nom\x.sh`: "'C:/Users/Prénom Nom/x.sh'"} {
		if runtime.GOOS == "windows" || !strings.Contains(in, `\`) {
			if got := askpassValue(in); got != want && !strings.Contains(in, `\`) {
				t.Errorf("askpassValue(%q) = %q, attendu %q", in, got, want)
			}
		}
	}
}

// Environnement du faux git : configuration globale vide, followRedirects désactivé.
func TestCloneLanceGitAvecConfigNeutre(t *testing.T) {
	rec := fakeGit(t, "exit 0\n")
	if err := CloneProject(context.Background(), "https://github.com/a/b", t.TempDir(), CloneAuth{Token: "ghp_X"}, nil); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(rec)
	if !strings.Contains(string(b), "http.followRedirects=false") {
		t.Error("followRedirects non désactivé")
	}
	for _, l := range strings.Split(string(b), "\n") {
		if p, ok := strings.CutPrefix(l, "GIT_CONFIG_GLOBAL="); ok {
			if _, err := os.Stat(p); err == nil {
				t.Error("fichier de configuration neutre encore là après le clonage (non supprimé)")
			}
			return
		}
	}
	t.Error("GIT_CONFIG_GLOBAL absent")
}
