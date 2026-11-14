package core

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// #148 (LOT-3.2) — compléments à projectsettings_test.go : couleur (clés voisines,
// métacaractères, commentaires et guillemets piégeux, CRLF, idempotence), écriture
// structurée de env.yaml (commentaires des éléments gardés, clés inconnues, mounts
// sans `ro`, listes vides) et fusion sans template ni launcher.

func colorOf(t *testing.T, src, name string) string {
	t.Helper()
	conf := filepath.Join(t.TempDir(), "config.conf")
	psWrite(t, conf, src)
	return loadConfigAt(conf).ProjectColors[name]
}

// Un nom qui en préfixe un autre, ou qui porte des métacaractères d'expression
// régulière, ne touche que sa propre entrée.
func TestSetProjectColorNeighbourKeys(t *testing.T) {
	unixOnly(t)
	src := "declare -A PROJECT_COLORS=(\n  [\"alpha2\"]=\"1\"\n  [\"axb\"]=\"2\"\n  [\"Projet é\"]=\"3\"\n  [\"x\"]=\"4\"\n)\n"
	for _, tt := range []struct{ name, color string }{{"alpha", "9"}, {"a.b", "9"}, {"Projet é", "9"}, {"a*", "9"}, {"x", "9"}} {
		got, err := SetProjectColor(src, tt.name, tt.color)
		if err != nil {
			t.Fatalf("%s : %v", tt.name, err)
		}
		if HasErrors(ValidateConfig(ConfConf, []byte(got))) {
			t.Errorf("%s : invalide : %s", tt.name, got)
		}
		for other, want := range map[string]string{"alpha2": "1", "axb": "2", "Projet é": "3", "x": "4"} {
			if other == tt.name {
				continue
			}
			if c := colorOf(t, got, other); c != want {
				t.Errorf("%s a modifié %q : %q, attendu %q\n%s", tt.name, other, c, want, got)
			}
		}
		if c := colorOf(t, got, tt.name); c != "9" {
			t.Errorf("%s : couleur écrite %q\n%s", tt.name, c, got)
		}
	}
}

// « ) » et « # » dans un commentaire ou entre guillemets ne terminent pas le bloc.
func TestSetProjectColorTrickyBlock(t *testing.T) {
	unixOnly(t)
	src := "declare -A PROJECT_COLORS=(\n  # voir (la doc) ) ici\n  [\"a#b\"]=\"1\"   # note ) finale\n  [\"c)d\"]=\"2\"\n)\nAFTER=1\n"
	got, err := SetProjectColor(src, "neuf", "7")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "AFTER=1") || !strings.Contains(got, "# voir (la doc) ) ici") || !strings.Contains(got, "# note ) finale") {
		t.Errorf("fichier abîmé :\n%s", got)
	}
	if HasErrors(ValidateConfig(ConfConf, []byte(got))) {
		t.Errorf("invalide :\n%s", got)
	}
	for name, want := range map[string]string{"a#b": "1", "c)d": "2", "neuf": "7"} {
		if c := colorOf(t, got, name); c != want {
			t.Errorf("%q = %q, attendu %q\n%s", name, c, want, got)
		}
	}
}

// Fixer deux fois la même couleur = une fois ; retirer puis refixer rend la
// même couleur ; bornes 0 et 255 permises.
func TestSetProjectColorIdempotentAndBounds(t *testing.T) {
	unixOnly(t)
	src := "# c\ndeclare -A PROJECT_COLORS=(\n  [\"a\"]=\"1\"\n)\n"
	once, _ := SetProjectColor(src, "p", "52")
	twice, err := SetProjectColor(once, "p", "52")
	if err != nil || twice != once {
		t.Errorf("non idempotent :\n%s\n--\n%s (%v)", once, twice, err)
	}
	back, _ := SetProjectColor(once, "p", "")
	if back != src {
		t.Errorf("retrait ne ramène pas l'original :\n%q\n%q", back, src)
	}
	for _, c := range []string{"0", "255"} {
		got, err := SetProjectColor(src, "p", c)
		if err != nil || colorOf(t, got, "p") != c {
			t.Errorf("borne %s : %v\n%s", c, err, got)
		}
	}
}

// config.conf aux fins de ligne Windows : les lignes d'origine restent en CRLF,
// aucune ligne d'origine n'est perdue, la couleur est relue.
func TestSetProjectColorCRLF(t *testing.T) {
	src := "# ma config\r\nX=1\r\ndeclare -A PROJECT_COLORS=(\r\n  [\"a\"]=\"1\"\r\n)\r\nY=2\r\n"
	got, err := SetProjectColor(src, "p", "52")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range []string{"# ma config", "X=1", "[\"a\"]=\"1\"", "Y=2"} {
		if !strings.Contains(got, l+"\r\n") {
			t.Errorf("ligne %q perdue ou sans CRLF :\n%q", l, got)
		}
	}
	if !strings.Contains(got, `["p"]="52"`) {
		t.Errorf("couleur absente : %q", got)
	}
	if strings.Count(got, "\r\n") < strings.Count(src, "\r\n") {
		t.Errorf("fins de ligne perdues : %q", got)
	}
}

const envComments = `# environnement de alpha — ne pas retirer
x_custom:            # clé inconnue du formulaire
  garde: [1, 2]
image: docker.io/library/debian:12   # socle
packages:
  - ripgrep   # recherche
  - fd        # fichiers
  - jq        # json
mounts:
  - {path: /data}
  - {path: /rw, ro: false}
`

// env.yaml : un élément retiré d'une liste n'emporte pas les commentaires des
// autres ; les clés inconnues et les commentaires de tête survivent ; un montage
// sans `ro` reste sans `ro` (lecture seule par défaut, pas un `ro` explicite).
func TestApplyProjectEnvKeepsCommentsAndUnknownKeys(t *testing.T) {
	out, err := ApplyConfigChanges(ConfProjectEnv, []byte(envComments), []ConfigChange{
		{Op: "set", At: []string{"packages"}, Value: []any{"ripgrep", "jq", "bat"}}})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{"# environnement de alpha — ne pas retirer", "# clé inconnue du formulaire", "garde:",
		"# socle", "# recherche", "# json", "bat"} {
		if !strings.Contains(s, want) {
			t.Errorf("%q perdu :\n%s", want, s)
		}
	}
	if strings.Contains(s, "# fichiers") || strings.Contains(s, "- fd") {
		t.Errorf("l'élément retiré (et son commentaire) est resté :\n%s", s)
	}
	m, err := ReadEnvModel(out)
	if err != nil || !reflect.DeepEqual(m.Packages, []string{"ripgrep", "jq", "bat"}) || m.Image != "docker.io/library/debian:12" {
		t.Fatalf("modèle relu : %+v %v", m, err)
	}
	if len(m.Mounts) != 2 || m.Mounts[0].RO != nil || m.Mounts[1].RO == nil || *m.Mounts[1].RO {
		t.Errorf("ro des montages changé : %+v", m.Mounts)
	}
	if ps := ValidateProjectEnv("", out); HasErrors(ps) {
		t.Errorf("invalide : %v\n%s", ps, s)
	}
}

// Une liste vidée donne un env.yaml valide et un modèle vide ; un changement
// invalide (mounts non liste) est refusé par la validation existante.
func TestApplyProjectEnvEmptyAndInvalid(t *testing.T) {
	out, err := ApplyConfigChanges(ConfProjectEnv, []byte(envComments), []ConfigChange{
		{Op: "set", At: []string{"packages"}, Value: []any{}}})
	if err != nil {
		t.Fatal(err)
	}
	if m, err := ReadEnvModel(out); err != nil || len(m.Packages) != 0 {
		t.Errorf("liste vidée : %+v %v\n%s", m, err, out)
	}
	if HasErrors(ValidateProjectEnv("", out)) {
		t.Errorf("liste vidée invalide : %v\n%s", ValidateProjectEnv("", out), out)
	}
	bad, err := ApplyConfigChanges(ConfProjectEnv, []byte(envComments), []ConfigChange{
		{Op: "set", At: []string{"mounts"}, Value: "pas une liste"}})
	if err == nil && !HasErrors(ValidateProjectEnv("", bad)) {
		t.Errorf("mounts non liste accepté :\n%s", bad)
	}
	// fichier absent : création
	created, err := ApplyConfigChanges(ConfProjectEnv, nil, []ConfigChange{
		{Op: "set", At: []string{"image"}, Value: "debian:12"}, {Op: "set", At: []string{"packages"}, Value: []any{"gh"}}})
	if err != nil {
		t.Fatal(err)
	}
	if m, _ := ReadEnvModel(created); m.Image != "debian:12" || len(m.Packages) != 1 || HasErrors(ValidateProjectEnv("", created)) {
		t.Errorf("création :\n%s", created)
	}
}

// Fusion sans template ni env.yaml du launcher : tout vient du projet ; un
// élément porté par les trois n'apparaît qu'une fois, revendiqué par le launcher.
func TestMergedEnvLinesOnlyProjectAndDedup(t *testing.T) {
	proj := psEnv(t)
	got, err := MergedEnvLines(proj, []byte("image: debian:12\npackages: [gh, jq]\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range got {
		if l.From != "" && l.From != "project" {
			t.Errorf("provenance inattendue sans template ni launcher : %+v", l)
		}
	}
	psWrite(t, LauncherEnvPath(), "packages: [gh]\n")
	psWrite(t, envTemplatePath(proj), "packages: [gh]\n")
	got, _ = MergedEnvLines(proj, []byte("packages: [gh]\n"))
	n, from := 0, ""
	for _, l := range got {
		if strings.TrimSpace(l.Text) == "- gh" {
			n++
			from = l.From
		}
	}
	if n != 1 || from != "launcher" {
		t.Errorf("« gh » : %d fois, de %q (attendu 1 fois, launcher)\n%+v", n, from, got)
	}
}
