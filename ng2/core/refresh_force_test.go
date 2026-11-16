package core

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// #158 (LOT-2, 2.1) — tests unitaires de core : lecture forcée, délai minimal de l'usage,
// conditions de la fermeture forcée, nettoyage. Complète refresh_test.go (cas de base).

func hashTree(t *testing.T, root string, skip ...string) map[string]string {
	t.Helper()
	sk := map[string]bool{}
	for _, s := range skip {
		sk[filepath.ToSlash(s)] = true
	}
	m := map[string]string{}
	_ = filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if sk[rel] {
			return nil
		}
		b, _ := os.ReadFile(p)
		h := sha256.Sum256(b)
		m[rel] = hex.EncodeToString(h[:])
		return nil
	})
	return m
}

// Le cache du registre d'agents est ignoré par la lecture forcée, et la valeur relue le remplace.
func TestReadTplInfoForcedIgnoresCache(t *testing.T) {
	proj := t.TempDir()
	tplCache.Lock()
	tplCache.m[proj] = tplEntry{at: time.Now(), files: map[string]string{"sentinelle": "périmé"}}
	tplCache.Unlock()
	t.Cleanup(func() { ForgetTplInfo(proj) })

	ReadTplInfo(proj) // cache frais : la valeur périmée est conservée
	tplCache.Lock()
	_, kept := tplCache.m[proj].files["sentinelle"]
	tplCache.Unlock()
	if !kept {
		t.Fatal("précondition : ReadTplInfo aurait dû garder le cache frais")
	}
	ReadTplInfoForced(proj)
	tplCache.Lock()
	e, ok := tplCache.m[proj]
	tplCache.Unlock()
	if !ok {
		t.Fatal("la lecture forcée n'a pas réécrit le cache")
	}
	if _, stale := e.files["sentinelle"]; stale {
		t.Error("la lecture forcée a gardé la valeur du cache")
	}
	if time.Since(e.at) > 5*time.Second {
		t.Error("cache non renouvelé par la lecture forcée")
	}
	ForgetTplInfo(proj)
	tplCache.Lock()
	_, still := tplCache.m[proj]
	tplCache.Unlock()
	if still {
		t.Error("ForgetTplInfo a laissé l'entrée")
	}
}

// CRITICAL — délai minimal de l'usage : après un échec récent de l'API (429, « trop d… »), la
// consommation forcée ne l'interroge pas (pas de nouveau 429) et ne touche pas au cache.
func TestFetchUsageForcedRespectsBackoff(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	cache := usageCachePath()
	if err := os.MkdirAll(filepath.Dir(cache), 0o755); err != nil {
		t.Fatal(err)
	}
	const body = `{"five_hour":{"utilization":12}}`
	_ = os.WriteFile(cache, []byte(body), 0o644)
	_ = os.WriteFile(cache+".err", []byte("trop de requêtes (429)"), 0o644) // échec à l'instant
	if FetchUsageForced(60) {
		t.Error("l'API a été interrogée dans le délai minimal qui suit un 429")
	}
	if b, _ := os.ReadFile(cache); string(b) != body {
		t.Errorf("cache de consommation modifié pendant le délai minimal : %q", b)
	}
	// l'échec est mémorisé : le délai tient aussi pour un rafraîchissement répété
	if FetchUsageForced(60) {
		t.Error("second appel : l'API a été interrogée dans le délai minimal")
	}
}

// Conditions de la fermeture forcée : matrice complète des cas du contrat.
func TestForceCloseableMatrix(t *testing.T) {
	mine := LiveSess{Owner: OwnerInfo{Id: LauncherID(), At: time.Now()}, Claude: true}
	other := LiveSess{Owner: OwnerInfo{Id: "autre-hub:9:y", At: time.Now()}, Claude: true}
	stale := LiveSess{Owner: OwnerInfo{Id: "autre-hub:9:y", At: time.Now().Add(-24 * time.Hour)}, Claude: true}
	cases := []struct {
		name    string
		in      ForceInput
		ok      bool
		whyHas  string
		refused string // fragment attendu ; « - » : aucune raison de refus
	}{
		{"projet fermé sain", ForceInput{P: ProjInfo{Dot: SessNone}}, false, "", "-"},
		{"ouvert, backend en panne", ForceInput{P: ProjInfo{Open: true, HasEnv: true, Backend: "lab", Problems: []Problem{{}}, Dot: SessAnomaly}, Tab: true}, true, "backend injoignable", "-"},
		{"ouvert, conteneur arrêté", ForceInput{P: ProjInfo{Open: true, HasEnv: true, CtrKnown: true, Dot: SessHere}, Tab: true}, true, "conteneur est arrêté", "-"},
		{"ouvert, pod en échec", ForceInput{P: ProjInfo{HasEnv: true, CtrKnown: true, Ctr: Found{Failed: true}, Dot: SessAnomaly}, K8s: true}, true, "pod k8s est en échec", "-"},
		{"conteneur en marche, session perdue", ForceInput{P: ProjInfo{Open: true, HasEnv: true, CtrKnown: true, Ctr: Found{Running: true}, Dot: SessHere}, Tab: true}, true, "session tmux n'existe plus", "-"},
		{"reprise interrompue, hôte", ForceInput{P: ProjInfo{HasSess: true, Dot: SessAnomaly}}, true, "reprise est interrompue", "-"},
		{"launcher courant pilote la session vivante", ForceInput{P: ProjInfo{Open: true, HasEnv: true, HasLive: true, Live: mine, Dot: SessHere}, Tab: true}, false, "", "-"},
		// CRITICAL : jamais si un autre launcher vivant pilote
		{"autre launcher vivant (ouvert ailleurs)", ForceInput{P: ProjInfo{HasEnv: true, HasLive: true, Live: other, Dot: SessElse}}, false, "", "prendre la main"},
		{"autre launcher vivant (lecture seule ici)", ForceInput{P: ProjInfo{Open: true, HasEnv: true, HasLive: true, Live: other, Dot: SessReadOnly}, Tab: true}, false, "", "prendre la main"},
		{"autre launcher, pod disparu mais pilote vivant", ForceInput{P: ProjInfo{Open: true, HasEnv: true, HasLive: true, Live: other, Dot: SessReadOnly}, Tab: true, K8s: true}, false, "", "autre-hub"},
	}
	for _, c := range cases {
		ok, why, refused := ForceCloseable(c.in)
		if ok != c.ok {
			t.Errorf("%s : ok=%v (attendu %v), why=%q refused=%q", c.name, ok, c.ok, why, refused)
		}
		if c.whyHas != "" && !strings.Contains(why, c.whyHas) {
			t.Errorf("%s : why=%q, attendu %q", c.name, why, c.whyHas)
		}
		if c.ok && why == "" {
			t.Errorf("%s : proposée sans raison à afficher", c.name)
		}
		if c.refused == "-" && refused != "" {
			t.Errorf("%s : refus inattendu %q", c.name, refused)
		}
		if c.refused != "-" && !strings.Contains(refused, c.refused) {
			t.Errorf("%s : refused=%q, attendu %q", c.name, refused, c.refused)
		}
	}
	// un propriétaire périmé (launcher disparu) ne bloque pas
	if _, _, refused := ForceCloseable(ForceInput{P: ProjInfo{Open: true, HasEnv: true, HasLive: true, Live: stale, Dot: SessHere}, Tab: true}); refused != "" {
		t.Errorf("propriétaire périmé : refus %q", refused)
	}
}

// Ce que la fermeture forcée annonce : onglet seulement s'il y en a un, session tmux locale pour un
// projet sur l'hôte, pod/conteneur selon le backend ; toujours la marque de sortie propre.
func TestForceItems(t *testing.T) {
	has := func(items []string, frag string) bool {
		for _, i := range items {
			if strings.Contains(i, frag) {
				return true
			}
		}
		return false
	}
	host := ForceItems(ForceInput{P: ProjInfo{}, Tab: true})
	if !has(host, "onglet") || !has(host, "session tmux locale") || !has(host, "marque de sortie propre") || has(host, "conteneur") || has(host, "pod") {
		t.Errorf("hôte avec onglet : %v", host)
	}
	k8s := ForceItems(ForceInput{P: ProjInfo{HasEnv: true, CtrKnown: true}, K8s: true})
	if has(k8s, "onglet") || has(k8s, "session tmux locale") || !has(k8s, "pod k8s") {
		t.Errorf("pod sans onglet : %v", k8s)
	}
	ctr := ForceItems(ForceInput{P: ProjInfo{HasEnv: true, CtrKnown: true}, Tab: true})
	if !has(ctr, "conteneur") || has(ctr, "pod") {
		t.Errorf("conteneur : %v", ctr)
	}
}

// CRITICAL — le nettoyage ne supprime rien du projet (sauf le verrou périmé) et ne touche pas à
// projects.json : seuls la marque de sortie propre, le verrou périmé et les fichiers d'ordre changent.
func TestCleanupProjectDeletesNothingInProjectNorProjectsJSON(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CL_CONFIG_DIR", cfg)
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	proj := t.TempDir()
	for name, body := range map[string]string{
		"CLAUDE.md":                         "projet",
		"src/main.go":                       "package main",
		".claude-launcher/env.yaml":         "image: x\n",
		".claude-launcher/autre-fichier.md": "garde",
	} {
		p := filepath.Join(proj, filepath.FromSlash(name))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte(body), 0o644)
	}
	lock := lockPath(proj)
	_ = os.WriteFile(lock, []byte("autre:1:x 2020-01-01T00:00:00Z\n"), 0o644)
	old := time.Now().Add(-2 * lockStale)
	_ = os.Chtimes(lock, old, old)
	if err := WriteAssignment(proj, "host"); err != nil {
		t.Fatal(err)
	}
	assign := filepath.Join(Ng2ConfigDir(), "projects.json")
	ab, err := os.ReadFile(assign)
	if err != nil {
		t.Fatal(err)
	}
	before := hashTree(t, proj, ".claude-launcher/.lock")
	ord := filepath.Join(t.TempDir(), "ordre")
	_ = os.WriteFile(ord, []byte("%1"), 0o644)

	r := CleanupProject(proj, nil, ord)
	if !r.Marker || !r.Lock || r.Orders != 1 {
		t.Errorf("nettoyage : %+v, attendu marque, verrou périmé et un fichier d'ordre", r)
	}
	if _, err := os.Stat(lock); err == nil {
		t.Error("verrou périmé conservé")
	}
	if _, err := os.Stat(CleanExitMarker(proj)); err != nil {
		t.Errorf("marque de sortie propre absente : %v", err)
	}
	after := hashTree(t, proj, ".claude-launcher/.lock")
	for p, h := range before {
		if a, ok := after[p]; !ok {
			t.Errorf("fichier du projet supprimé : %s", p)
		} else if a != h {
			t.Errorf("fichier du projet modifié : %s", p)
		}
	}
	for p := range after {
		if _, ok := before[p]; !ok {
			t.Errorf("fichier ajouté dans le projet : %s", p)
		}
	}
	if ab2, _ := os.ReadFile(assign); string(ab2) != string(ab) {
		t.Error("projects.json modifié par le nettoyage")
	}
}

// CRITICAL — le verrou d'un autre launcher vivant (frais) n'est jamais supprimé, même si le nettoyage
// est demandé (la décision de refus appartient à ForceCloseable, ce garde-fou protège en dernier recours).
func TestCleanupProjectKeepsFreshForeignLock(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	proj := t.TempDir()
	lock := lockPath(proj)
	_ = os.MkdirAll(filepath.Dir(lock), 0o755)
	_ = os.WriteFile(lock, []byte("autre:1:x 2026-01-01T00:00:00Z\n"), 0o644)
	r := CleanupProject(proj, nil)
	if r.Lock {
		t.Error("verrou frais d'un autre launcher supprimé")
	}
	if _, err := os.Stat(lock); err != nil {
		t.Error("verrou frais d'un autre launcher absent après le nettoyage")
	}
}

// Marque de sortie propre : posée (même fichier que celle de la fermeture normale), idempotente.
func TestMarkCleanExitIdempotent(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	proj := t.TempDir()
	for i := 0; i < 2; i++ {
		if err := MarkCleanExit(proj); err != nil {
			t.Fatalf("appel %d : %v", i+1, err)
		}
	}
	st, err := os.Stat(CleanExitMarker(proj))
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(st.ModTime()) > time.Minute {
		t.Error("date de la marque non renouvelée")
	}
}

// Le cache du diagnostic d'un backend (une minute) est ignoré par ForceDiagnose, et la valeur relue
// le remplace : DiagnoseCached la rend ensuite sans relire. Moteur absent (PATH vide) : aucun appel réel.
func TestForceDiagnoseReplacesCache(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	b := BackendConf{Name: "diag158", Type: "podman"}
	key := backendKey(b)
	diagCache.Lock()
	diagCache.m[key] = diagEntry{time.Now(), []Problem{{What: "sentinelle périmée"}}}
	diagCache.Unlock()
	t.Cleanup(func() { diagCache.Lock(); delete(diagCache.m, key); diagCache.Unlock() })

	if ps := DiagnoseCached(b); len(ps) != 1 || ps[0].What != "sentinelle périmée" {
		t.Fatalf("précondition : le cache frais doit être servi, obtenu %v", ps)
	}
	ps := ForceDiagnose(b)
	for _, p := range ps {
		if strings.Contains(p.What, "sentinelle") {
			t.Fatalf("ForceDiagnose a rendu la valeur du cache : %v", ps)
		}
	}
	if len(ps) == 0 {
		t.Error("moteur absent : un problème était attendu")
	}
	cached := DiagnoseCached(b)
	if len(cached) != len(ps) || (len(ps) > 0 && cached[0].What != ps[0].What) {
		t.Errorf("la valeur relue n'a pas remplacé le cache : %v / %v", cached, ps)
	}
}

// Backend qui ne répond pas (Problems) : fermeture proposée AVEC avertissement ; rien n'est annoncé
// comme arrêté côté conteneur ou pod (l'état réel est inconnu).
func TestForceCloseableWarnsWhenBackendDown(t *testing.T) {
	in := ForceInput{P: ProjInfo{Open: true, HasEnv: true, Backend: "lab", CtrKnown: true, Ctr: Found{Running: true},
		Problems: []Problem{{What: "daemon injoignable"}}, Dot: SessAnomaly}, Tab: true}
	ok, why, refused := ForceCloseable(in)
	if !ok || refused != "" {
		t.Fatalf("ok=%v refused=%q : proposée attendue", ok, refused)
	}
	if !strings.Contains(why, "backend injoignable") || !strings.Contains(why, "tourne peut-être encore") {
		t.Errorf("why = %q, attendu « backend injoignable : le projet tourne peut-être encore »", why)
	}
	for _, it := range ForceItems(in) {
		l := strings.ToLower(it)
		if strings.Contains(l, "conteneur") || strings.Contains(l, "pod") || strings.Contains(l, "arrêt") {
			t.Errorf("ForceItems annonce un arrêt côté backend : %q", it)
		}
	}
}
