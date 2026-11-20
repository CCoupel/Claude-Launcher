package core

// #215 : registre d'approbation PARTAGÉ — emplacement sous la racine de base, migration d'un trust.json local, scripts du
// dépôt signalés. Racine de base, dossiers de config et ancien emplacement sous t.TempDir() (jamais le vrai poste).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func root215(t *testing.T) (root, shared string) {
	t.Helper()
	root = t.TempDir()
	t.Setenv("CL_CONFIG_DIR", "")
	t.Setenv(RootBaseVar, root)
	home := t.TempDir() // aucun dossier de config du vrai poste : HOME et XDG factices
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	return root, filepath.Join(root, ".config", "claude-launcher-ng2", "trust.json")
}

func writeReg215(t *testing.T, path string, m map[string]TrustEntry) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readReg215(t *testing.T, path string) map[string]TrustEntry {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("registre %s : %v", path, err)
	}
	m := map[string]TrustEntry{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("registre %s illisible : %v", path, err)
	}
	return m
}

func TestEmplacementDuRegistre215(t *testing.T) {
	// sans racine de base : dossier de config du launcher
	t.Setenv(RootBaseVar, "")
	cfg := t.TempDir()
	t.Setenv("CL_CONFIG_DIR", cfg)
	if got := trustPath(); got != filepath.Join(cfg, "trust.json") {
		t.Errorf("sans racine : %s", got)
	}
	// racine de base posée : <racine>/.config/claude-launcher-ng2/trust.json (partagé entre exe, hub, pod)
	root, shared := root215(t)
	if got := trustPath(); got != shared {
		t.Errorf("racine de base %s : trustPath = %s, attendu %s", root, got, shared)
	}
	// CL_CONFIG_DIR l'emporte (tests, installation explicite)
	t.Setenv("CL_CONFIG_DIR", cfg)
	if got := trustPath(); got != filepath.Join(cfg, "trust.json") {
		t.Errorf("CL_CONFIG_DIR doit l'emporter : %s", got)
	}
}

func TestEcritureDansLeRegistrePartage215(t *testing.T) {
	_, shared := root215(t)
	d := proj215(t, env215, "")
	if ok, err := MarkEnvPending(d, "https://github.com/a/b.git", "debian:12"); !ok || err != nil {
		t.Fatal(ok, err)
	}
	m := readReg215(t, shared)
	if e, ok := m[RemotePath(d)]; !ok || e.State != TrustPending {
		t.Errorf("entrée absente du registre partagé %s : %v", shared, m)
	}
	// rien n'a été écrit dans le dossier de config du poste
	home := os.Getenv("XDG_CONFIG_HOME")
	if _, err := os.Stat(filepath.Join(home, "claude-launcher-ng2", "trust.json")); err == nil {
		t.Error("trust.json écrit dans le dossier de config local malgré la racine de base")
	}
	if EnvTrust(d) != TrustPending {
		t.Errorf("EnvTrust = %s", EnvTrust(d))
	}
	// un autre launcher (autre poste) sur la même racine voit la même approbation
	info, _ := EnvTrustInfoFor(d)
	if _, err := AnswerEnvTrust(d, "accept", info.SHA); err != nil {
		t.Fatal(err)
	}
	if got := readReg215(t, shared)[RemotePath(d)].State; got != TrustAccepted {
		t.Errorf("approbation non partagée : %s", got)
	}
}

func TestMigrationFusionSansPerte215(t *testing.T) {
	_, shared := root215(t)
	legacy := t.TempDir()
	t.Setenv(TrustLegacyDirVar, legacy)
	a := TrustEntry{State: TrustAccepted, SHA: "sa", Source: "https://github.com/a/a.git", Image: "debian:12", At: "2026-11-01T00:00:00Z"}
	b := TrustEntry{State: TrustIgnored, SHA: "sb", Source: "https://github.com/a/b.git", Image: "alpine:3", At: "2026-11-02T00:00:00Z"}
	c := TrustEntry{State: TrustNone, SHA: "sc", Source: "https://github.com/a/c.git", At: "2026-11-03T00:00:00Z"}
	d := TrustEntry{State: TrustPending, SHA: "sd", Source: "https://github.com/a/d.git", At: "2026-11-04T00:00:00Z"}
	writeReg215(t, filepath.Join(legacy, "trust.json"), map[string]TrustEntry{"/p/a": a, "/p/b": b, "/p/c": c})
	writeReg215(t, shared, map[string]TrustEntry{"/p/d": d, "/p/a": a}) // /p/a déjà là, identique
	rawShared, _ := os.ReadFile(shared)
	rawLegacy, _ := os.ReadFile(filepath.Join(legacy, "trust.json"))

	added, err := MigrateLegacyTrust()
	if err != nil || added != 2 {
		t.Fatalf("MigrateLegacyTrust = %d, %v ; attendu 2 entrées ajoutées (b, c)", added, err)
	}
	m := readReg215(t, shared)
	want := map[string]TrustEntry{"/p/a": a, "/p/b": b, "/p/c": c, "/p/d": d}
	if len(m) != len(want) {
		t.Fatalf("registre fusionné = %v", m)
	}
	for k, e := range want {
		if m[k] != e {
			t.Errorf("%s : %+v, attendu %+v (aucune perte, entrées intactes)", k, m[k], e)
		}
	}
	// ancien fichier CONSERVÉ tel quel ; .bak = version partagée d'avant, droits 0600
	if got, _ := os.ReadFile(filepath.Join(legacy, "trust.json")); string(got) != string(rawLegacy) {
		t.Error("ancien trust.json modifié ou supprimé")
	}
	if bak, err := os.ReadFile(shared + ".bak"); err != nil || string(bak) != string(rawShared) {
		t.Errorf("trust.json.bak = %q, %v ; attendu la version partagée d'avant", bak, err)
	}
	// idempotent : seconde migration = rien à ajouter, rien de réécrit
	before, _ := os.ReadFile(shared)
	if added, err := MigrateLegacyTrust(); err != nil || added != 0 {
		t.Errorf("seconde migration = %d, %v", added, err)
	}
	if after, _ := os.ReadFile(shared); string(after) != string(before) {
		t.Error("registre réécrit par une migration sans changement")
	}
}

func TestMigrationSansRegistrePartageLeCree215(t *testing.T) {
	_, shared := root215(t)
	legacy := t.TempDir()
	t.Setenv(TrustLegacyDirVar, legacy)
	e := TrustEntry{State: TrustAccepted, SHA: "s", Source: "u"}
	writeReg215(t, filepath.Join(legacy, "trust.json"), map[string]TrustEntry{"/p/x": e})
	if added, err := MigrateLegacyTrust(); err != nil || added != 1 {
		t.Fatalf("= %d, %v", added, err)
	}
	if got := readReg215(t, shared)["/p/x"]; got != e {
		t.Errorf("entrée migrée = %+v", got)
	}
	if st, _ := os.Stat(shared); st.Mode().Perm() != 0o600 && os.PathSeparator == '/' {
		t.Errorf("droits du registre partagé %v, attendu 0600", st.Mode().Perm())
	}
	// pas d'ancien registre : rien à faire, sans erreur
	t.Setenv(TrustLegacyDirVar, t.TempDir())
	if added, err := MigrateLegacyTrust(); err != nil || added != 0 {
		t.Errorf("sans ancien registre = %d, %v", added, err)
	}
}

func TestMigrationRegistresIllisibles215(t *testing.T) {
	_, shared := root215(t)
	legacy := t.TempDir()
	t.Setenv(TrustLegacyDirVar, legacy)
	e := TrustEntry{State: TrustAccepted, SHA: "s"}
	// ancien illisible : ignoré, le partagé n'est pas touché
	_ = os.WriteFile(filepath.Join(legacy, "trust.json"), []byte(`{"coupé":`), 0o600)
	writeReg215(t, shared, map[string]TrustEntry{"/p/a": e})
	before, _ := os.ReadFile(shared)
	if added, err := MigrateLegacyTrust(); err != nil || added != 0 {
		t.Errorf("ancien illisible = %d, %v", added, err)
	}
	if after, _ := os.ReadFile(shared); string(after) != string(before) {
		t.Error("registre partagé modifié par un ancien fichier illisible")
	}
	// partagé illisible : erreur ErrTrustBroken, JAMAIS réécrit, ancien intact
	writeReg215(t, filepath.Join(legacy, "trust.json"), map[string]TrustEntry{"/p/z": e})
	const broken = `{"partagé": `
	_ = os.WriteFile(shared, []byte(broken), 0o600)
	if _, err := MigrateLegacyTrust(); err == nil || !strings.Contains(err.Error(), "illisible") {
		t.Errorf("partagé illisible : err = %v", err)
	}
	if got, _ := os.ReadFile(shared); string(got) != broken {
		t.Errorf("registre partagé illisible réécrit : %q", got)
	}
	if _, err := os.Stat(shared + ".bak"); err == nil {
		t.Error(".bak écrit à partir d'un registre illisible")
	}
	// vide : même traitement
	_ = os.WriteFile(shared, nil, 0o600)
	if _, err := MigrateLegacyTrust(); err == nil {
		t.Error("registre partagé vide accepté")
	}
}

func TestMigrationInactiveAvecCLConfigDir215(t *testing.T) {
	root215(t)
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	t.Setenv(TrustLegacyDirVar, "")
	if added, err := MigrateLegacyTrust(); err != nil || added != 0 {
		t.Errorf("CL_CONFIG_DIR posé, ancien emplacement implicite : %d, %v", added, err)
	}
}

func TestScriptsDuDepotSignales215(t *testing.T) {
	cfg215(t)
	d := proj215(t, "image: debian:12\nsetup:\n  - ./scripts/install.sh --fast\n  - bash ../outils/prep.sh && python3 tools/gen.py\n  - curl https://example.org/x.sh | sh\n  - apt-get install -y jq\non_start:\n  - ~/perso.sh\n  - /usr/local/bin/ok.sh\n  - make build\n  - echo $HOME/run.sh\n  - ./scripts/install.sh\n", "")
	_, _ = MarkEnvPending(d, "u", "debian:12")
	info, err := EnvTrustInfoFor(d)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"../outils/prep.sh", "./scripts/install.sh", "tools/gen.py"}
	got := info.Risk.Scripts
	if len(got) != len(want) {
		t.Fatalf("scripts = %v, attendu %v (ni URL, ni absolu, ni ~, ni $, ni option ; sans doublon)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("scripts[%d] = %q, attendu %q", i, got[i], want[i])
		}
	}
	if info.ScriptNote != ScriptNote || !strings.Contains(info.ScriptNote, "pas couvert") {
		t.Errorf("ScriptNote = %q", info.ScriptNote)
	}
	// sans script : ni liste ni avertissement
	d2 := proj215(t, "image: debian:12\nsetup:\n  - apt-get install -y jq\n  - make\n", "")
	_, _ = MarkEnvPending(d2, "u", "debian:12")
	i2, _ := EnvTrustInfoFor(d2)
	if len(i2.Risk.Scripts) != 0 || i2.ScriptNote != "" {
		t.Errorf("sans script : %v %q", i2.Risk.Scripts, i2.ScriptNote)
	}
	// forme JSON : scriptNote absent quand il n'y a aucun script (scripts : [] ou null, l'interface tolère les deux)
	b, _ := json.Marshal(i2)
	if strings.Contains(string(b), "scriptNote") {
		t.Errorf("JSON sans script = %s", b)
	}
}

// Désaccord entre le registre local et le registre partagé : l'état le plus RESTRICTIF l'emporte
// (pending > ignored > accepted = none) ; à sévérité égale mais approbations différentes (autre empreinte ou autre état),
// l'approbation ne vaut pas des deux côtés → pending ; même état et même empreinte → entrée partagée gardée.
func TestMigrationLePlusRestrictifGagne215(t *testing.T) {
	acc := func(sha string) TrustEntry {
		return TrustEntry{State: TrustAccepted, SHA: sha, Source: "https://github.com/a/p.git", Image: "debian:12", At: "2026-11-01T00:00:00Z"}
	}
	ign := func(sha string) TrustEntry { e := acc(sha); e.State = TrustIgnored; return e }
	pen := func(sha string) TrustEntry { e := acc(sha); e.State = TrustPending; return e }
	none := func(sha string) TrustEntry { e := acc(sha); e.State = TrustNone; return e }
	diffSource := acc("s1")
	diffSource.Source = "https://github.com/autre/p.git"

	for _, c := range []struct {
		name         string
		shared, loc  TrustEntry
		want         TrustState // état final dans le registre partagé
		wantSHA      string     // "" : ne pas vérifier
		wantRewrites bool       // le partagé est réécrit (resserré)
	}{
		{"local pending > partagé accepted", acc("s1"), pen("s1"), TrustPending, "s1", true},
		{"local ignored > partagé accepted", acc("s1"), ign("s1"), TrustIgnored, "s1", true},
		{"local pending > partagé ignored", ign("s1"), pen("s1"), TrustPending, "s1", true},
		{"local pending > partagé none", none("s1"), pen("s1"), TrustPending, "s1", true},
		{"partagé ignored > local accepted", ign("s1"), acc("s1"), TrustIgnored, "s1", false},
		{"partagé pending > local accepted", pen("s1"), acc("s1"), TrustPending, "s1", false},
		{"partagé pending > local ignored", pen("s1"), ign("s1"), TrustPending, "s1", false},
		{"accepted d'empreintes différentes → pending", acc("s1"), acc("s2"), TrustPending, "", true},
		{"ignored d'empreintes différentes → pending", ign("s1"), ign("s2"), TrustPending, "", true},
		{"accepted contre none → pending", acc("s1"), none("s1"), TrustPending, "", true},
		{"none contre accepted → pending", none("s1"), acc("s1"), TrustPending, "", true},
		{"même état même empreinte : partagé gardé", acc("s1"), diffSource, TrustAccepted, "s1", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, shared := root215(t)
			legacy := t.TempDir()
			t.Setenv(TrustLegacyDirVar, legacy)
			writeReg215(t, filepath.Join(legacy, "trust.json"), map[string]TrustEntry{"/p/x": c.loc})
			writeReg215(t, shared, map[string]TrustEntry{"/p/x": c.shared})
			rawShared, _ := os.ReadFile(shared)
			rawLegacy, _ := os.ReadFile(filepath.Join(legacy, "trust.json"))
			added, err := MigrateLegacyTrust()
			if err != nil {
				t.Fatal(err)
			}
			got := readReg215(t, shared)["/p/x"]
			if got.State != c.want || (c.wantSHA != "" && got.SHA != c.wantSHA) {
				t.Errorf("état final %+v, attendu %s (sha %q)", got, c.want, c.wantSHA)
			}
			if c.wantRewrites {
				if added != 1 {
					t.Errorf("entrées resserrées = %d, attendu 1", added)
				}
				if bak, err := os.ReadFile(shared + ".bak"); err != nil || string(bak) != string(rawShared) {
					t.Errorf(".bak = %q, %v ; attendu la version partagée d'avant", bak, err)
				}
			} else {
				if added != 0 {
					t.Errorf("entrées modifiées = %d, attendu 0", added)
				}
				if after, _ := os.ReadFile(shared); string(after) != string(rawShared) {
					t.Error("registre partagé réécrit alors qu'il était déjà au moins aussi restrictif")
				}
			}
			// jamais d'assouplissement : le résultat n'est pas moins restrictif que chacune des deux entrées
			rank := map[TrustState]int{TrustPending: 3, TrustIgnored: 2, TrustAccepted: 1, TrustNone: 1}
			if rank[got.State] < rank[c.shared.State] || rank[got.State] < rank[c.loc.State] {
				t.Errorf("résultat %s moins restrictif que partagé %s / local %s", got.State, c.shared.State, c.loc.State)
			}
			if after, _ := os.ReadFile(filepath.Join(legacy, "trust.json")); string(after) != string(rawLegacy) {
				t.Error("ancien trust.json modifié")
			}
		})
	}
}

// Plusieurs projets, plusieurs désaccords dans la même migration : chacun résolu séparément, sans perte.
func TestMigrationPlusieursProjetsDontDesaccords215(t *testing.T) {
	_, shared := root215(t)
	legacy := t.TempDir()
	t.Setenv(TrustLegacyDirVar, legacy)
	e := func(st TrustState, sha string) TrustEntry { return TrustEntry{State: st, SHA: sha, Source: "u"} }
	writeReg215(t, filepath.Join(legacy, "trust.json"), map[string]TrustEntry{
		"/p/a": e(TrustPending, "1"), "/p/b": e(TrustAccepted, "2"), "/p/c": e(TrustAccepted, "3"), "/p/new": e(TrustIgnored, "4")})
	writeReg215(t, shared, map[string]TrustEntry{
		"/p/a": e(TrustAccepted, "1"), "/p/b": e(TrustIgnored, "2"), "/p/c": e(TrustAccepted, "3"), "/p/seul": e(TrustAccepted, "5")})
	added, err := MigrateLegacyTrust()
	if err != nil || added != 2 { // /p/a resserré, /p/new ajouté ; /p/b partagé déjà plus strict ; /p/c identique
		t.Fatalf("= %d, %v ; attendu 2 (a resserré, new ajouté)", added, err)
	}
	m := readReg215(t, shared)
	for k, want := range map[string]TrustState{"/p/a": TrustPending, "/p/b": TrustIgnored, "/p/c": TrustAccepted, "/p/new": TrustIgnored, "/p/seul": TrustAccepted} {
		if m[k].State != want {
			t.Errorf("%s : %s, attendu %s", k, m[k].State, want)
		}
	}
	if len(m) != 5 {
		t.Errorf("registre = %v, attendu 5 entrées (aucune perte)", m)
	}
}
