//go:build !windows

package core

// #217 : écriture atomique de config.conf (contracts/desktop-dispatch.md « Écriture de config.conf »).
// Tag `atomic217` : API posée à l'écriture (dev-backend en parallèle) ; retirer le tag une fois posée. Noms ATTENDUS :
//
//	func WriteFileAtomic(path string, content []byte, mode os.FileMode) error
//	var atomicBeforeReplace func(tmp, dst string) error   // après fsync, avant rename ; erreur => abandon
//	var configTmpMaxAge = tun.Of(10 * time.Minute)
//	func sweepConfigTmp() int                              // balaie .config.conf.tmp-* de Ng2ConfigDir()

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func dir217(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	t.Setenv("CL_CONFIG_DIR", d)
	t.Setenv(CacheDirVar, t.TempDir())
	return d
}

func lsTmp217(t *testing.T, d string) []string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(d, ".config.conf.tmp-*"))
	return m
}

func TestEcritureInterrompueAncienIntact217(t *testing.T) {
	d := dir217(t)
	p := filepath.Join(d, "config.conf")
	if err := os.WriteFile(p, []byte("ANCIEN\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	crash := errors.New("crash simulé")
	atomicBeforeReplace = func(tmp, dst string) error {
		if b, _ := os.ReadFile(tmp); string(b) != "NOUVEAU\n" {
			t.Errorf("temporaire incomplet avant rename : %q", b)
		}
		return crash
	}
	defer func() { atomicBeforeReplace = nil }()
	if err := WriteFileAtomic(p, []byte("NOUVEAU\n"), 0o600); !errors.Is(err, crash) {
		t.Fatalf("erreur = %v, attendu le crash simulé", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "ANCIEN\n" {
		t.Errorf("config.conf = %q : l'ancien contenu doit rester intact", b)
	}
	if l := lsTmp217(t, d); len(l) != 0 {
		t.Errorf("temporaires laissés : %v", l)
	}
}

func TestEcritureNominaleRemplaceEtPermissions0600217(t *testing.T) {
	d := dir217(t)
	p := filepath.Join(d, "config.conf")
	if err := WriteFileAtomic(p, []byte("A\n"), 0o600); err != nil { // fichier absent : créé
		t.Fatal(err)
	}
	if err := WriteFileAtomic(p, []byte("B\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "B\n" {
		t.Errorf("contenu = %q", b)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Errorf("droits = %v, attendu 0600", st.Mode().Perm())
	}
	if l := lsTmp217(t, d); len(l) != 0 {
		t.Errorf("temporaires laissés : %v", l)
	}
}

func TestTemporaireCree0600AvantRename217(t *testing.T) {
	d := dir217(t)
	p := filepath.Join(d, "config.conf")
	var perm os.FileMode
	atomicBeforeReplace = func(tmp, dst string) error {
		st, err := os.Stat(tmp)
		if err != nil {
			t.Error(err)
			return nil
		}
		perm = st.Mode().Perm()
		if filepath.Dir(tmp) != filepath.Dir(dst) {
			t.Errorf("temporaire %s hors du dossier de destination", tmp)
		}
		return nil
	}
	defer func() { atomicBeforeReplace = nil }()
	if err := WriteFileAtomic(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if perm != 0o600 {
		t.Errorf("droits du temporaire = %v, attendu 0600", perm)
	}
}

func TestEcritureInterrompueLaisseBakCoherent217(t *testing.T) {
	d := dir217(t)
	p := filepath.Join(d, "config.conf")
	_ = os.WriteFile(p, []byte("ANCIEN\n"), 0o600)
	calls := 0
	atomicBeforeReplace = func(tmp, dst string) error {
		calls++
		if strings.HasSuffix(dst, ".bak") {
			return nil // le .bak passe, c'est config.conf qui « plante »
		}
		return errors.New("crash")
	}
	defer func() { atomicBeforeReplace = nil }()
	_ = WriteFileAtomic(p+".bak", []byte("ANCIEN\n"), 0o600)
	if err := WriteFileAtomic(p, []byte("N\n"), 0o600); err == nil {
		t.Fatal("crash attendu")
	}
	if b, _ := os.ReadFile(p + ".bak"); string(b) != "ANCIEN\n" {
		t.Errorf(".bak = %q", b)
	}
	if b, _ := os.ReadFile(p); string(b) != "ANCIEN\n" {
		t.Errorf("config.conf = %q", b)
	}
	if calls != 2 {
		t.Errorf("hook appelé %d fois, attendu 2 (.bak puis config.conf)", calls)
	}
}

func touch217(t *testing.T, d, name string, age time.Duration) string {
	t.Helper()
	p := filepath.Join(d, name)
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	mt := time.Now().Add(-age)
	if err := os.Chtimes(p, mt, mt); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBalayageConfigTmpAgeEtMotifExact217(t *testing.T) {
	d := dir217(t)
	vieux := touch217(t, d, ".config.conf.tmp-123456", 11*time.Minute)
	vieux2 := touch217(t, d, ".config.conf.tmp-abc", time.Hour)
	limite := touch217(t, d, ".config.conf.tmp-999", 9*time.Minute) // peut-être une écriture en cours : gardé
	recent := touch217(t, d, ".config.conf.tmp-1", time.Second)
	conf := touch217(t, d, "config.conf", 48*time.Hour)
	bak := touch217(t, d, "config.conf.bak", 48*time.Hour)
	autre1 := touch217(t, d, ".backends.yaml.tmp-1", 48*time.Hour)
	autre2 := touch217(t, d, "config.conf.tmp-1", 48*time.Hour)   // sans point initial
	autre3 := touch217(t, d, ".config.conf.tmp", 48*time.Hour)    // sans tiret
	autre4 := touch217(t, d, ".config.conf.tmpx-1", 48*time.Hour) // motif voisin
	sousdir := filepath.Join(d, ".config.conf.tmp-dir")
	if err := os.Mkdir(sousdir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	_ = os.Chtimes(sousdir, old, old)

	if n := sweepConfigTmp(); n != 2 {
		t.Errorf("sweepConfigTmp = %d, attendu 2", n)
	}
	for p, want := range map[string]bool{vieux: false, vieux2: false, limite: true, recent: true, conf: true, bak: true,
		autre1: true, autre2: true, autre3: true, autre4: true, sousdir: true} {
		if _, err := os.Stat(p); (err == nil) != want {
			t.Errorf("%s : présent=%v, attendu %v", filepath.Base(p), err == nil, want)
		}
	}
}

func TestBalayageConfigTmpAgeReglableEtErreursIgnorees217(t *testing.T) {
	d := dir217(t)
	old := configTmpMaxAge.Set(time.Hour)
	defer configTmpMaxAge.Set(old)
	p := touch217(t, d, ".config.conf.tmp-7", 2*time.Hour)
	if n := sweepConfigTmp(); n != 1 {
		t.Errorf("n = %d, attendu 1", n)
	}
	if _, err := os.Stat(p); err == nil {
		t.Error("fichier de 2 h conservé avec un âge réglé à 1 h")
	}
	if n := sweepConfigTmp(); n != 0 {
		t.Errorf("second balayage : %d", n)
	}
	_ = os.RemoveAll(d)
	if n := sweepConfigTmp(); n != 0 { // dossier absent : sans panique
		t.Errorf("dossier absent : %d", n)
	}
}

func TestEnsureConfigCreationExclusiveSansTemporaire217(t *testing.T) {
	d := dir217(t)
	created, err := EnsureConfig()
	if err != nil || !created {
		t.Fatalf("EnsureConfig = %v, %v", created, err)
	}
	if st, err := os.Stat(ConfigPath()); err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("config.conf : %v, %v", st, err)
	}
	if l := lsTmp217(t, d); len(l) != 0 {
		t.Errorf("temporaires laissés : %v", l)
	}
	// existant : jamais écrasé
	if err := os.WriteFile(ConfigPath(), []byte("MIEN\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	created, err = EnsureConfig()
	if err != nil || created {
		t.Fatalf("2e EnsureConfig = %v, %v", created, err)
	}
	if b, _ := os.ReadFile(ConfigPath()); string(b) != "MIEN\n" {
		t.Errorf("config existante écrasée : %q", b)
	}
}

func TestEnsureConfigConcurrentUnSeulCreateur217(t *testing.T) {
	dir217(t)
	const n = 12
	res := make(chan bool, n)
	for i := 0; i < n; i++ {
		go func() {
			c, err := EnsureConfig()
			if err != nil {
				t.Error(err)
			}
			res <- c
		}()
	}
	created := 0
	for i := 0; i < n; i++ {
		if <-res {
			created++
		}
	}
	if created != 1 {
		t.Errorf("%d créateurs, attendu exactement 1", created)
	}
	if b, err := os.ReadFile(ConfigPath()); err != nil || len(b) == 0 {
		t.Errorf("config.conf vide ou illisible : %v", err)
	}
}

func TestSaveConfigBakResserre0600EtCrashAncienIntact217(t *testing.T) {
	dir217(t)
	p := ConfigPath()
	if err := os.WriteFile(p, []byte("# v1\n"), 0o644); err != nil { // config plus ouverte que 0600
		t.Fatal(err)
	}
	_ = os.WriteFile(p+".bak", []byte("vieux bak"), 0o644)
	st, _ := os.Stat(p)
	// crash au rename de config.conf (le .bak est déjà passé)
	atomicBeforeReplace = func(tmp, dst string) error {
		if dst == p {
			return errors.New("crash")
		}
		return nil
	}
	_, err := SaveConfig(ConfConf, p, []byte("# v2\n"), st.ModTime())
	atomicBeforeReplace = nil
	if err == nil {
		t.Fatal("crash attendu")
	}
	if b, _ := os.ReadFile(p); string(b) != "# v1\n" {
		t.Errorf("config.conf = %q, ancien attendu", b)
	}
	if b, _ := os.ReadFile(p + ".bak"); string(b) != "# v1\n" {
		t.Errorf(".bak = %q, copie de la version précédente attendue", b)
	}
	if bst, _ := os.Stat(p + ".bak"); bst.Mode().Perm() != 0o600 {
		t.Errorf(".bak droits %v, attendu 0600", bst.Mode().Perm())
	}
	// succès
	st, _ = os.Stat(p)
	if _, err := SaveConfig(ConfConf, p, []byte("# v3\n"), st.ModTime()); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "# v3\n" {
		t.Errorf("config.conf = %q", b)
	}
	if cst, _ := os.Stat(p); cst.Mode().Perm() != 0o600 {
		t.Errorf("config.conf droits %v, attendu 0600", cst.Mode().Perm())
	}
	if l := lsTmp217(t, filepath.Dir(p)); len(l) != 0 {
		t.Errorf("temporaires laissés : %v", l)
	}
}

func TestCreateExclusiveAtomicJamaisEcraseNiTemporaire217(t *testing.T) {
	d := dir217(t)
	p := filepath.Join(d, "sous", "config.conf") // dossier absent : créé
	ok, err := createExclusiveAtomic(p, []byte("COMPLET\n"), 0o600)
	if err != nil || !ok {
		t.Fatalf("1re création = %v, %v", ok, err)
	}
	if b, _ := os.ReadFile(p); string(b) != "COMPLET\n" {
		t.Errorf("contenu = %q", b)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Errorf("droits %v, attendu 0600", st.Mode().Perm())
	}
	ok, err = createExclusiveAtomic(p, []byte("AUTRE\n"), 0o600) // existe : ni erreur, ni écrasement
	if err != nil || ok {
		t.Fatalf("2e création = %v, %v, attendu (false, nil)", ok, err)
	}
	if b, _ := os.ReadFile(p); string(b) != "COMPLET\n" {
		t.Errorf("fichier existant écrasé : %q", b)
	}
	if l := lsTmp217(t, filepath.Dir(p)); len(l) != 0 {
		t.Errorf("temporaires laissés : %v", l)
	}
}
