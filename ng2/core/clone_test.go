//go:build !windows

package core

import (
	"context"
	"os"
	"path/filepath"
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
