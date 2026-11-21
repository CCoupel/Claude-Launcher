//go:build !windows

package core

// #215 : clonage d'un dépôt (contracts/desktop-dispatch.md « create », source git) — compléments boîte noire à
// clone_test.go : le jeton ne laisse aucune trace, progression bornée, annulation de tout l'arbre de processus,
// marqueur /init-project. Faux `git` dans le PATH uniquement (jamais le vrai) ; PATH, TMPDIR et dossiers sous t.TempDir().

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const tok215 = "ghp_FAKE_TEST_215_core_not_a_token" // FACTICE : invalide par construction, reconnu par testguard

// git215 : faux git. Imite un clone réussi (dossier cible : .git/config avec l'URL seule + un fichier), journalise argv
// et environnement. extra est du sh exécuté après l'enregistrement, à la place du clone si exit est dedans.
func git215(t *testing.T, extra string) (rec string) {
	t.Helper()
	bin := t.TempDir()
	rec = filepath.Join(t.TempDir(), "rec")
	script := `#!/bin/sh
{ echo "ARGV:$*"; env | sort; } > ` + rec + `
for last; do :; done; dest="$last"
` + extra + `
mkdir -p "$dest/.git"
printf '[remote "origin"]\n\turl = %s\n' "$(printf '%s' "$*" | sed 's/.*-- \([^ ]*\) .*/\1/')" > "$dest/.git/config"
echo hello > "$dest/README.md"
exit 0
`
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return rec
}

func grepTree215(t *testing.T, root, needle string) []string {
	t.Helper()
	var hits []string
	_ = filepath.Walk(root, func(p string, st os.FileInfo, _ error) error {
		if st != nil && st.Mode().IsRegular() {
			if b, _ := os.ReadFile(p); strings.Contains(string(b), needle) {
				hits = append(hits, p)
			}
		}
		return nil
	})
	return hits
}

func TestCloneSuccesLeJetonNeLaisseAucuneTrace215(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp) // le script askpass vit dans ce dossier : vérifié supprimé après
	rec := git215(t, "")
	dir := t.TempDir()
	var pcts []int
	err := CloneProject(context.Background(), "https://github.com/CCoupel/x.git", dir, CloneAuth{Token: tok215},
		func(step string, pct int) { pcts = append(pcts, pct) })
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(rec)
	log := string(b)
	argv := strings.SplitN(log, "\n", 2)[0]
	if strings.Contains(argv, tok215) {
		t.Errorf("jeton dans argv : %s", argv)
	}
	for _, want := range []string{"credential.helper=", "protocol.file.allow=never", "protocol.ext.allow=never", "clone", "--progress", "-- https://github.com/CCoupel/x.git " + dir} {
		if !strings.Contains(argv, want) {
			t.Errorf("argv sans %q : %s", want, argv)
		}
	}
	// le jeton n'est que dans l'environnement du processus git, via GIT_ASKPASS ; jamais interactif ; protocoles limités
	if !strings.Contains(log, "CL_GIT_TOKEN="+tok215) || !strings.Contains(log, "GIT_ASKPASS=") ||
		!strings.Contains(log, "GIT_TERMINAL_PROMPT=0") || !strings.Contains(log, "GIT_ALLOW_PROTOCOL=https:ssh") {
		t.Errorf("environnement du processus git incomplet :\n%s", log)
	}
	// aucune trace dans le dossier cloné (.git/config compris), ni de script askpass restant
	if hits := grepTree215(t, dir, tok215); len(hits) != 0 {
		t.Errorf("jeton écrit dans le projet : %v", hits)
	}
	if cfg, _ := os.ReadFile(filepath.Join(dir, ".git", "config")); strings.Contains(string(cfg), "@") || strings.Contains(string(cfg), "token") {
		t.Errorf(".git/config suspect : %s", cfg)
	}
	if hits := grepTree215(t, tmp, "CL_GIT_TOKEN"); len(hits) != 0 {
		t.Errorf("script askpass laissé : %v", hits)
	}
	if ents, _ := os.ReadDir(tmp); len(ents) != 0 {
		t.Errorf("fichiers temporaires laissés : %v", ents)
	}
	_ = pcts
}

func TestCloneProgressionBorneeEtCroissante215(t *testing.T) {
	git215(t, `printf 'Enumerating objects: 5, done.\n' >&2
printf 'Receiving objects:  10%% (1/10)\rReceiving objects:  60%% (6/10)\rReceiving objects: 100%% (10/10), done.\n' >&2
printf 'Resolving deltas: 100%% (3/3), done.\n' >&2
printf 'Updating files: 100%% (4/4), done.\n' >&2
`)
	var pcts []int
	var steps []string
	if err := CloneProject(context.Background(), "https://github.com/a/b", t.TempDir(), CloneAuth{}, func(s string, p int) {
		pcts, steps = append(pcts, p), append(steps, s)
	}); err != nil {
		t.Fatal(err)
	}
	if len(pcts) < 4 {
		t.Fatalf("progression = %v %v", steps, pcts)
	}
	for i, p := range pcts {
		if p < 0 || p > 100 {
			t.Errorf("pct %d hors 0..100", p)
		}
		if i > 0 && p < pcts[i-1] {
			t.Errorf("progression décroissante : %v", pcts)
		}
	}
	if pcts[len(pcts)-1] != 100 {
		t.Errorf("dernier pct = %d, attendu 100", pcts[len(pcts)-1])
	}
}

func TestCloneEchecSansSecretDansLeMessage215(t *testing.T) {
	// sortie d'erreur non reconnue qui répète le jeton : le message rendu est masqué et court
	git215(t, `printf 'fatal: unable to access https://x-access-token:`+tok215+`@github.com/a/b: weird failure `+tok215+`\n' >&2
exit 128
`)
	err := CloneProject(context.Background(), "https://github.com/a/b", t.TempDir(), CloneAuth{Token: tok215}, nil)
	if err == nil {
		t.Fatal("erreur attendue")
	}
	if strings.Contains(err.Error(), tok215) {
		t.Errorf("jeton dans le message : %v", err)
	}
	if len(err.Error()) > 260 {
		t.Errorf("message trop long (%d)", len(err.Error()))
	}
	// échecs reconnus : raisons lisibles
	for stderr, want := range map[string]string{
		"fatal: Authentication failed for 'https://github.com/a/b/'":                        "authentification refusée",
		"fatal: repository 'https://github.com/a/zz/' not found":                            "dépôt introuvable",
		"fatal: unable to access 'https://h/': Could not resolve host: h":                   "hôte injoignable",
		"Host key verification failed.\nfatal: Could not read from remote repository.":      "hôte inconnu",
		"git@github.com: Permission denied (publickey).\nfatal: Could not read from remote": "authentification refusée",
	} {
		git215(t, "printf '%s\\n' '"+strings.ReplaceAll(stderr, "'", "")+"' >&2\nexit 128\n")
		e := CloneProject(context.Background(), "https://github.com/a/b", t.TempDir(), CloneAuth{}, nil)
		if e == nil || !strings.Contains(e.Error(), want) {
			t.Errorf("stderr %q → %v, attendu « %s »", stderr, e, want)
		}
	}
}

func TestCloneAnnulationTueToutLArbreDeProcessus215(t *testing.T) {
	pidf := filepath.Join(t.TempDir(), "enfant.pid")
	git215(t, "sleep 120 &\necho $! > "+pidf+"\nwait\n") // git a un enfant (comme remote-https / ssh)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- CloneProject(ctx, "https://github.com/a/b", t.TempDir(), CloneAuth{}, nil) }()
	var pid int
	for i := 0; i < 100 && pid == 0; i++ {
		if b, err := os.ReadFile(pidf); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		}
		time.Sleep(30 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("l'enfant du faux git n'a pas démarré")
	}
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("err = %v, attendu context.Canceled", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("annulation sans effet")
	}
	gone := false
	for i := 0; i < 100; i++ {
		if _, err := os.Stat("/proc/" + strconv.Itoa(pid)); err != nil {
			gone = true
			break
		}
		if b, _ := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat"); strings.Contains(string(b), ") Z ") { // zombie : mort
			gone = true
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if !gone {
		t.Errorf("l'enfant de git (pid %d) survit à l'annulation", pid)
		_ = os.Remove(pidf)
		_ = killPid215(pid)
	}
}

func killPid215(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}

func TestMarqueurInitProposed215(t *testing.T) {
	dir := t.TempDir()
	if InitProposalPending(dir) {
		t.Fatal("proposition en attente sur un dossier neuf")
	}
	if err := MarkInitProposed(dir); err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, ".claude-launcher", "init-proposed"); InitProposedPath(dir) != want {
		t.Errorf("chemin = %s, attendu %s", InitProposedPath(dir), want)
	}
	if !InitProposalPending(dir) {
		t.Error("marqueur posé mais non vu")
	}
	if err := MarkInitProposed(dir); err != nil { // idempotent
		t.Errorf("seconde pose : %v", err)
	}
	ClearInitProposal(dir)
	if InitProposalPending(dir) {
		t.Error("marqueur retiré mais encore vu")
	}
	ClearInitProposal(dir) // sans marqueur : sans effet ni panique
	ClearInitProposal(filepath.Join(dir, "absent"))
	if err := MarkInitProposed(filepath.Join(dir, "absent", "x")); err != nil { // dossier absent : créé
		t.Errorf("dossier absent : %v", err)
	}
}

func TestRedactSecret215(t *testing.T) {
	if got := RedactSecret("a "+tok215+" b "+tok215, tok215); strings.Contains(got, tok215) || strings.Count(got, "***") != 2 {
		t.Errorf("RedactSecret = %q", got)
	}
	if got := RedactSecret("rien", ""); got != "rien" {
		t.Errorf("secret vide : %q", got)
	}
}
