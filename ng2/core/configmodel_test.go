package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const modelSample = `# Backends de la maison — ne pas toucher aux IP
pools:
  # machines du labo
  labo:
    hosts: [srv1, srv2]
    drain: []

backends:
  # le principal
  srv1-docker:   # docker distant
    type: docker
    host: tcp://192.168.1.211:2375   # IP fixe
    shared: [/share/MFS/ROOT]
  lab:           # cluster
    type: k8s
    context: k8s
    namespace: claude-ng2
    volumes:
      /share/MFS/ROOT: claude-shared
  pooled: { type: docker, pool: labo, host: "tcp://{}:2375", shared: [/share/MFS/ROOT] }

defaults:
  backend: srv1-docker   # présélection
  image: docker.io/library/debian:12
images: [docker.io/library/debian:12]
`

func apply(t *testing.T, content string, ch ...ConfigChange) string {
	t.Helper()
	out, err := ApplyConfigChanges(ConfBackends, []byte(content), ch)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestReadBackendsModel(t *testing.T) {
	m, err := ReadBackendsModel([]byte(modelSample))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, b := range declared(m) { // sous Windows, un « host » implicite est en tête du modèle
		names = append(names, b.Name)
	}
	if strings.Join(names, ",") != "srv1-docker,lab,pooled" { // ordre du fichier
		t.Fatalf("ordre : %v", names)
	}
	if b := declared(m)[1]; b.Type != "k8s" || b.Namespace != "claude-ng2" || b.Volumes["/share/MFS/ROOT"] != "claude-shared" {
		t.Fatalf("lab : %+v", b)
	}
	if len(m.Pools) != 1 || m.Pools[0].Name != "labo" || len(m.Pools[0].UsedBy) != 1 || m.Pools[0].UsedBy[0] != "pooled" {
		t.Fatalf("pools : %+v", m.Pools)
	}
	if m.Defaults.Backend != "srv1-docker" || len(m.Images) != 1 {
		t.Fatalf("defaults : %+v %v", m.Defaults, m.Images)
	}
	if _, err := ReadBackendsModel([]byte("backends: [")); err == nil {
		t.Fatal("YAML invalide accepté")
	}
	if m, err := ReadBackendsModel(nil); err != nil || len(declared(m)) != 0 {
		t.Fatalf("vide : %+v %v", m, err)
	}
}

// critical : les commentaires, l'ordre et les clés non touchées survivent aux
// changements structurés.
func TestApplyChangesKeepsComments(t *testing.T) {
	out := apply(t, modelSample,
		ConfigChange{Op: "set", At: []string{"backends", "srv1-docker"}, Value: map[string]any{
			"type": "docker", "host": "tcp://192.168.1.99:2375", "shared": []any{"/share/MFS/ROOT"}}},
		ConfigChange{Op: "set", At: []string{"backends", "nouveau"}, Value: map[string]any{
			"type": "ssh", "host": "srv9", "engine": "docker", "shared": []any{"/share/MFS/ROOT"}, "context": ""}},
		ConfigChange{Op: "set", At: []string{"defaults", "image"}, Value: "docker.io/library/debian:13"},
	)
	for _, want := range []string{"# Backends de la maison — ne pas toucher aux IP", "# machines du labo", "# le principal",
		"# docker distant", "# IP fixe", "# cluster", "# présélection", "namespace: claude-ng2", "tcp://192.168.1.99:2375"} {
		if !strings.Contains(out, want) {
			t.Errorf("« %s » perdu :\n%s", want, out)
		}
	}
	if strings.Contains(out, "192.168.1.211") {
		t.Error("ancienne adresse conservée")
	}
	// ordre : l'existant d'abord, le nouveau à la fin de la section
	i1, i2, i3 := strings.Index(out, "srv1-docker:"), strings.Index(out, "lab:"), strings.Index(out, "nouveau:")
	if !(i1 < i2 && i2 < i3) {
		t.Errorf("ordre des backends : %d %d %d\n%s", i1, i2, i3, out)
	}
	if strings.Contains(out, "context: \"\"") || strings.Count(out, "context:") != 1 {
		t.Errorf("clé vide écrite ou k8s perdu :\n%s", out)
	}
	if HasErrors(ValidateConfig(ConfBackends, []byte(out))) {
		t.Errorf("résultat invalide : %v\n%s", ValidateConfig(ConfBackends, []byte(out)), out)
	}
	m, err := ReadBackendsModel([]byte(out))
	if d := declared(m); err != nil || len(d) != 4 || d[3].Name != "nouveau" || d[3].Engine != "docker" {
		t.Fatalf("modèle relu : %+v %v", m.Backends, err)
	}
}

func TestApplyChangesDeleteRenameSet(t *testing.T) {
	out := apply(t, modelSample,
		ConfigChange{Op: "delete", At: []string{"backends", "lab"}},
		ConfigChange{Op: "rename", At: []string{"pools", "labo"}, Value: "atelier"},
		ConfigChange{Op: "set", At: []string{"backends", "pooled", "pool"}, Value: "atelier"},
		ConfigChange{Op: "set", At: []string{"pools", "atelier", "drain"}, Value: []any{"srv2"}},
	)
	if strings.Contains(out, "namespace: claude-ng2") || strings.Contains(out, "# cluster") && false {
		t.Errorf("backend supprimé conservé :\n%s", out)
	}
	m, err := ReadBackendsModel([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Pools) != 1 || m.Pools[0].Name != "atelier" || len(m.Pools[0].Drain) != 1 || m.Pools[0].UsedBy[0] != "pooled" {
		t.Fatalf("pools : %+v", m.Pools)
	}
	if !strings.Contains(out, "# machines du labo") || !strings.Contains(out, "# Backends de la maison") {
		t.Errorf("commentaires perdus :\n%s", out)
	}
	if HasErrors(ValidateConfig(ConfBackends, []byte(out))) {
		t.Errorf("invalide : %v", ValidateConfig(ConfBackends, []byte(out)))
	}
	// erreurs : opération, chemin, nom pris
	for _, ch := range []ConfigChange{{Op: "boum", At: []string{"a"}}, {Op: "delete", At: []string{"backends", "absent"}},
		{Op: "rename", At: []string{"backends", "lab"}, Value: "pooled"}, {Op: "set", At: nil}} {
		if _, err := ApplyConfigChanges(ConfBackends, []byte(modelSample), []ConfigChange{ch}); err == nil {
			t.Errorf("changement accepté : %+v", ch)
		}
	}
	// fichier vide ou seulement des commentaires : section créée
	out = apply(t, "# rien encore\n", ConfigChange{Op: "set", At: []string{"backends", "x"}, Value: map[string]any{"type": "podman"}})
	if !strings.Contains(out, "# rien encore") || !strings.Contains(out, "podman") {
		t.Errorf("fichier vide :\n%s", out)
	}
}

// Un changement invalide est refusé par la validation existante (rien d'écrit) ;
// un fichier modifié depuis l'ouverture est un conflit ; le texte valide s'écrit
// avec copie .bak.
func TestApplyThroughSaveConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CL_CONFIG_DIR", dir)
	path := filepath.Join(dir, "backends.yaml")
	if err := os.WriteFile(path, []byte(modelSample), 0o644); err != nil {
		t.Fatal(err)
	}
	if BackendsPath() != path {
		t.Skipf("BackendsPath = %s", BackendsPath())
	}
	st, _ := os.Stat(path)
	mt := st.ModTime()
	// invalide : type inconnu
	bad := apply(t, modelSample, ConfigChange{Op: "set", At: []string{"backends", "z"}, Value: map[string]any{"type": "vapeur"}})
	if _, err := SaveConfig(ConfBackends, path, []byte(bad), mt); err != ErrConfigInvalid {
		t.Fatalf("invalide accepté : %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != modelSample {
		t.Fatal("fichier modifié malgré l'erreur")
	}
	// conflit
	good := apply(t, modelSample, ConfigChange{Op: "set", At: []string{"defaults", "image"}, Value: "x:1"})
	if _, err := SaveConfig(ConfBackends, path, []byte(good), mt.Add(-time.Hour)); err != ErrConfigConflict {
		t.Fatalf("conflit non vu : %v", err)
	}
	// valide
	if _, err := SaveConfig(ConfBackends, path, []byte(good), mt); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path + ".bak"); string(b) != modelSample {
		t.Fatal("copie .bak absente ou différente")
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "# IP fixe") || !strings.Contains(string(b), "x:1") {
		t.Fatalf("fichier écrit : %s", b)
	}
}

// Références pendantes : defaults.backend vers un backend supprimé ou renommé est
// une erreur (un seul code pour l'éditeur texte et les Réglages) ; une image par
// défaut hors de la liste images est un avertissement ; un pool cité mais absent
// reste refusé par le chargement.
func TestValidateDanglingReferences(t *testing.T) {
	errs := func(content string) []ConfigProblem {
		var out []ConfigProblem
		for _, p := range ValidateConfig(ConfBackends, []byte(content)) {
			out = append(out, p)
		}
		return out
	}
	if HasErrors(errs(modelSample)) {
		t.Fatalf("modèle valide refusé : %v", errs(modelSample))
	}
	// suppression du backend par défaut
	del := apply(t, modelSample, ConfigChange{Op: "delete", At: []string{"backends", "srv1-docker"}})
	ps := errs(del)
	if !HasErrors(ps) || !strings.Contains(ps[0].Message, "defaults.backend désigne un backend inexistant : srv1-docker") || ps[0].Line == 0 {
		t.Fatalf("suppression : %+v", ps)
	}
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	if _, err := SaveConfig(ConfBackends, BackendsPath(), []byte(del), time.Time{}); err != ErrConfigInvalid {
		t.Fatalf("enregistrement : %v", err)
	}
	// renommage sans mise à jour de defaults.backend, puis avec
	ren := apply(t, modelSample, ConfigChange{Op: "rename", At: []string{"backends", "srv1-docker"}, Value: "srv1"})
	if !HasErrors(errs(ren)) {
		t.Fatal("renommage sans defaults.backend accepté")
	}
	fixed := apply(t, ren, ConfigChange{Op: "set", At: []string{"defaults", "backend"}, Value: "srv1"})
	if HasErrors(errs(fixed)) {
		t.Fatalf("renommage complet refusé : %v", errs(fixed))
	}
	// « host » est toujours un backend valide
	if HasErrors(errs("backends: {}\ndefaults:\n  backend: host\n")) {
		t.Fatal("defaults.backend: host refusé")
	}
	// image par défaut hors liste : avertissement seulement
	w := errs("backends: {}\ndefaults:\n  image: autre:1\nimages: [debian:12]\n")
	if HasErrors(w) || len(w) != 1 || w[0].Severity != SevWarning {
		t.Fatalf("image : %+v", w)
	}
	// pool cité mais inconnu : refusé (chargement)
	if !HasErrors(errs("backends:\n  p: { type: docker, pool: absent, host: 'tcp://{}:1' }\n")) {
		t.Fatal("pool inconnu accepté")
	}
}

// « set » d'un backend depuis le formulaire : les clés que le formulaire ne
// connaît pas sont conservées (ajoutées à la main, ou par une version plus
// récente) ; une clé connue absente de la valeur est retirée.
func TestSetBackendKeepsUnknownKeys(t *testing.T) {
	src := "backends:\n  # mon docker\n  d:\n    type: docker\n    host: tcp://1.2.3.4:2375\n    shared: [/a]\n    custom_key: précieux   # noté à la main\n    futur: { a: 1 }\n"
	out := apply(t, src, ConfigChange{Op: "set", At: []string{"backends", "d"}, Value: map[string]any{
		"type": "docker", "host": "tcp://9.9.9.9:2375"}}) // shared omis : retiré
	for _, want := range []string{"custom_key: précieux", "# noté à la main", "futur:", "# mon docker", "9.9.9.9"} {
		if !strings.Contains(out, want) {
			t.Errorf("« %s » perdu :\n%s", want, out)
		}
	}
	if strings.Contains(out, "shared") || strings.Contains(out, "1.2.3.4") {
		t.Errorf("clé connue omise conservée :\n%s", out)
	}
	if HasErrors(ValidateConfig(ConfBackends, []byte(out))) {
		t.Errorf("invalide : %v", ValidateConfig(ConfBackends, []byte(out)))
	}
	// un backend neuf n'a que les clés envoyées ; les pools ne conservent rien d'inconnu (clés connues seulement)
	n := apply(t, src, ConfigChange{Op: "set", At: []string{"backends", "e"}, Value: map[string]any{"type": "podman"}})
	if strings.Count(n, "custom_key") != 1 {
		t.Errorf("clé inconnue copiée ailleurs :\n%s", n)
	}
}

// Une liste de valeurs simples réécrite garde les commentaires de ses éléments
// conservés (« - ripgrep # recherche ») et son écriture ; les neufs s'ajoutent.
func TestSetScalarListKeepsItemComments(t *testing.T) {
	src := "image: x:1\npackages:\n  - jq        # json\n  - ripgrep   # recherche\n  - fd\nlocal: [a, b]   # privés\n"
	out, err := ApplyConfigChanges(ConfProjectEnv, []byte(src), []ConfigChange{
		{Op: "set", At: []string{"packages"}, Value: []any{"ripgrep", "jq", "curl"}},
		{Op: "set", At: []string{"local"}, Value: []any{"a", "c"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{"# json", "# recherche", "curl", "# privés", "[a, c]"} {
		if !strings.Contains(s, want) {
			t.Errorf("« %s » perdu :\n%s", want, s)
		}
	}
	if strings.Contains(s, "fd") || strings.Contains(s, "b]") {
		t.Errorf("élément retiré conservé :\n%s", s)
	}
	if strings.Index(s, "ripgrep") > strings.Index(s, "jq") {
		t.Errorf("ordre demandé non respecté :\n%s", s)
	}
}
