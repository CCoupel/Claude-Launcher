package core

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestValidateRemote(t *testing.T) {
	for _, ok := range []string{"", "  ", "git@github.com:moi/projet.git", "https://github.com/moi/p.git", "ssh://git@host:2222/a/b.git",
		"file:///srv/git/p.git", "/srv/git/p.git", "../p.git", "https://user@github.com/a/b"} {
		if why := ValidateRemote(ok); why != "" {
			t.Errorf("%q refusé : %s", ok, why)
		}
	}
	for _, bad := range []string{"-u", "--upload-pack=touch /tmp/x", "-oProxyCommand=x", "a b", "git@host:a\nb", "git@host:a\x00b",
		"ext::sh -c id", "ext::ssh x", "fd::3", "foo::bar", "https://user:pass@github.com/a/b", "ssh://u:p@host/a", "user:pass@host:a/b.git"} {
		if why := ValidateRemote(bad); why == "" {
			t.Errorf("%q accepté", bad)
		}
	}
}

// InitProject : un remote refusé n'ajoute rien ; un remote valide (file:// compris) est lié, après « -- ».
func TestInitProjectDurciLeRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git absent")
	}
	for _, bad := range []string{"--upload-pack=x", "ext::sh -c id", "https://u:p@github.com/a/b"} {
		d := t.TempDir()
		if err := InitProject(d, false, bad); err == nil || !strings.Contains(err.Error(), "git remote") {
			t.Errorf("remote %q : erreur = %v", bad, err)
		}
		if out, _ := exec.Command("git", "-C", d, "remote").Output(); strings.TrimSpace(string(out)) != "" {
			t.Errorf("remote %q : un remote a été lié (%q)", bad, out)
		}
	}
	for _, good := range []string{"file:///srv/git/p.git", "git@github.com:moi/p.git"} {
		d := t.TempDir()
		if err := InitProject(d, false, good); err != nil {
			t.Fatalf("remote %q : %v", good, err)
		}
		out, _ := exec.Command("git", "-C", d, "remote", "get-url", "origin").Output()
		if strings.TrimSpace(string(out)) != good {
			t.Errorf("origin = %q, attendu %q", out, good)
		}
	}
	// sans remote : aucun origin
	d := t.TempDir()
	if err := InitProject(d, false, ""); err != nil {
		t.Fatal(err)
	}
	if out, _ := exec.Command("git", "-C", d, "remote").Output(); strings.TrimSpace(string(out)) != "" {
		t.Errorf("remote lié sans URL : %q", out)
	}
	_ = os.Remove(d)
}
