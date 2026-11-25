package core

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
)

// #250 (LOT-1) — modèle et effort des teammates depuis la carte TEAM : écriture du compagnon.
// Écrit depuis contracts/desktop-dispatch.md « v6.9.0 » (teamAgentSet) et le plan, AVANT le code final.
// API attendue (plan, tâche 2) : SetAgentOverride(proj, agent string, model, effort *string) (AgentOverrideResult, error).
// Le résultat est lu par son JSON (noms du contrat : ok, model, effort, modelSrc, effortSrc, file, removed,
// effortAdjusted, nextSpawn, note, settings, partial) : les tests ne dépendent pas des noms de champs Go.
// Projets factices uniquement (t.TempDir) ; jamais de vrai projet.

func s250(v string) *string { return &v }

// c250Proj : projet factice avec registre. Template des agents : model sonnet / effort medium (tplModel/tplEffort réglables).
func c250Proj(t *testing.T) string { return c250ProjTpl(t, "sonnet", "medium") }

func c250ProjTpl(t *testing.T, tplModel, tplEffort string) string {
	t.Helper()
	proj := t.TempDir()
	agents := filepath.Join(proj, ".claude", "agents")
	if err := os.MkdirAll(agents, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := `{"agents":{"registry":[
 {"name":"implementation-planner","tag":"v5.16.0"},{"name":"dev-backend","tag":"v5.16.0"},
 {"name":"deploy","tag":"v5.16.0"},{"name":"teamleader","tag":"v5.16.0"},{"name":"cdp","tag":"v5.16.0"},
 {"name":"generic","tag":"v5.16.0"}],
 "generic":[{"name":"redacteur"}]}}`
	c250Write(t, filepath.Join(proj, ".claude", "project-config.json"), cfg)
	tpl := func(name string) {
		c250Write(t, filepath.Join(agents, name+".template.md"),
			"---\nname: "+name+"\ndescription: gabarit de "+name+"\nmodel: "+tplModel+"\neffort: "+tplEffort+"\n---\n\n# Template "+name+"\n")
	}
	for _, n := range []string{"implementation-planner", "dev-backend", "deploy", "teamleader", "generic"} {
		tpl(n)
	}
	// cdp : pas un teammate, ni model: ni effort: dans son template → non réglable (editable faux)
	c250Write(t, filepath.Join(agents, "cdp.template.md"), "# CDP\nRègles d'orchestration.\n")
	return proj
}

func c250Write(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func c250Read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("lecture %s : %v", p, err)
	}
	return string(b)
}

func c250Exists(p string) bool { _, err := os.Lstat(p); return err == nil }

func c250Companion(proj, agent string) string {
	return filepath.Join(proj, ".claude", "agents", agent+".md")
}

func c250Hash(s string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(s))) }

// c250Set : appelle SetAgentOverride et rend le résultat décodé (JSON du contrat).
func c250Set(t *testing.T, proj, agent string, model, effort *string) (map[string]any, error) {
	t.Helper()
	res, err := SetAgentOverride(proj, agent, model, effort)
	b, _ := json.Marshal(res)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if m == nil {
		m = map[string]any{}
	}
	return m, err
}

func c250MustSet(t *testing.T, proj, agent string, model, effort *string) map[string]any {
	t.Helper()
	m, err := c250Set(t, proj, agent, model, effort)
	if err != nil {
		t.Fatalf("SetAgentOverride(%s, model=%v, effort=%v) : %v", agent, deref250(model), deref250(effort), err)
	}
	return m
}

func deref250(p *string) string {
	if p == nil {
		return "<non modifié>"
	}
	return *p
}

// snapshot250 : contenu de tous les fichiers du projet (chemin → hash), pour prouver « rien n'est écrit ».
func snapshot250(t *testing.T, proj string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.Walk(proj, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(proj, p)
		if info.IsDir() {
			out[rel+"/"] = "dir"
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			out[rel] = "symlink"
			return nil
		}
		b, _ := os.ReadFile(p)
		out[rel] = c250Hash(string(b))
		return nil
	})
	return out
}

func diff250(a, b map[string]string) string {
	var d []string
	for k, v := range a {
		if b[k] != v {
			d = append(d, k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			d = append(d, k)
		}
	}
	sort.Strings(d)
	return strings.Join(d, ", ")
}

// ── compagnon absent / sans frontmatter ──────────────────────────────────

// Compagnon absent : créé avec ce seul frontmatter ; modèle puis effort. Résultat du contrat.
func TestSetOverrideCompagnonAbsentCree250(t *testing.T) {
	proj := c250Proj(t)
	res := c250MustSet(t, proj, "implementation-planner", s250("opus"), nil)
	if got := c250Read(t, c250Companion(proj, "implementation-planner")); got != "---\nmodel: opus\n---\n" {
		t.Errorf("compagnon créé : %q, attendu le seul frontmatter « model: opus »", got)
	}
	if res["ok"] != true || res["model"] != "opus" || res["effort"] != "medium" || res["modelSrc"] != "companion" || res["effortSrc"] != "template" ||
		res["file"] != "implementation-planner.md" || res["removed"] != false || res["effortAdjusted"] != false {
		t.Errorf("résultat : %v, attendu ok, model opus (companion), effort medium (template), file implementation-planner.md", res)
	}
	// effort ajouté à la suite : les deux clés, model avant effort
	c250MustSet(t, proj, "implementation-planner", nil, s250("max"))
	if got := c250Read(t, c250Companion(proj, "implementation-planner")); got != "---\nmodel: opus\neffort: max\n---\n" {
		t.Errorf("deux clés : %q", got)
	}
}

// Sans frontmatter (cas réel de dev-backend.md de ce projet) : bloc ajouté en tête, reste inchangé octet pour octet.
func TestSetOverrideSansFrontmatterBlocEnTete250(t *testing.T) {
	proj := c250Proj(t)
	body := "# Companion dev-backend\n\nAdaptations projet : Go 1.27, Wails.\n\n- règle 1\n- règle 2\n"
	c250Write(t, c250Companion(proj, "dev-backend"), body)
	c250MustSet(t, proj, "dev-backend", s250("opus"), s250("high"))
	got := c250Read(t, c250Companion(proj, "dev-backend"))
	if got != "---\nmodel: opus\neffort: high\n---\n"+body {
		t.Errorf("bloc en tête : %q", got)
	}
	if !strings.HasSuffix(got, body) || c250Hash(got[len("---\nmodel: opus\neffort: high\n---\n"):]) != c250Hash(body) {
		t.Error("le corps du compagnon n'est pas conservé octet pour octet")
	}
}

// Autres clés du frontmatter : seules les lignes model:/effort: sont remplacées à leur place ; le reste intact.
func TestSetOverrideAutresClesEtCorpsConserves250(t *testing.T) {
	proj := c250Proj(t)
	orig := "---\nname: dev-backend\nmodel: haiku\ndescription: \"Développeur : backend\"\neffort: low\ntools: Read, Edit\n---\n\n# Corps\nTexte avec model: dans le corps (ne pas toucher)\n\n\tindenté\n"
	c250Write(t, c250Companion(proj, "dev-backend"), orig)
	c250MustSet(t, proj, "dev-backend", s250("opus"), s250("high"))
	want := strings.NewReplacer("model: haiku", "model: opus", "effort: low", "effort: high").Replace(orig)
	// seules les 2 lignes du frontmatter changent (le corps contient aussi « model: » : doit rester)
	if got := c250Read(t, c250Companion(proj, "dev-backend")); got != want {
		t.Errorf("réécriture :\n%q\nattendu :\n%q", got, want)
	}
}

// Fins de ligne CRLF : le corps est conservé octet pour octet, aucun LF nu introduit dans un fichier CRLF.
func TestSetOverrideCRLFConserve250(t *testing.T) {
	proj := c250Proj(t)
	body := "# Corps\r\nligne 2\r\n\r\ndernière sans fin"
	orig := "---\r\nname: dev-backend\r\nmodel: haiku\r\n---\r\n" + body
	c250Write(t, c250Companion(proj, "dev-backend"), orig)
	c250MustSet(t, proj, "dev-backend", s250("opus"), s250("high"))
	got := c250Read(t, c250Companion(proj, "dev-backend"))
	if !strings.HasSuffix(got, body) {
		t.Errorf("corps CRLF altéré : %q", got)
	}
	if strings.Count(got, "\n") != strings.Count(got, "\r\n") {
		t.Errorf("LF nu introduit dans un fichier CRLF : %q", got)
	}
	m, e := frontKeys(c250Companion(proj, "dev-backend"))
	if m != "opus" || e != "high" {
		t.Errorf("relecture du frontmatter CRLF : model=%q effort=%q", m, e)
	}
}

// BOM UTF-8 : comportement documenté par le test — soit refus sans écriture, soit écriture qui garde le BOM et le corps.
func TestSetOverrideBOM250(t *testing.T) {
	proj := c250Proj(t)
	bom := "\xef\xbb\xbf"
	body := "# Corps accentué : é à ü\n"
	orig := bom + "---\nmodel: haiku\n---\n" + body
	c250Write(t, c250Companion(proj, "dev-backend"), orig)
	_, err := c250Set(t, proj, "dev-backend", s250("opus"), nil)
	got := c250Read(t, c250Companion(proj, "dev-backend"))
	if err != nil {
		if got != orig {
			t.Errorf("refus pour BOM mais le fichier a été modifié : %q", got)
		}
		return
	}
	if !strings.HasPrefix(got, bom) || !strings.HasSuffix(got, body) {
		t.Errorf("écriture avec BOM : BOM ou corps perdu : %q", got)
	}
	if strings.Count(got, "model:") != 1 {
		t.Errorf("clé model: dupliquée avec BOM : %q", got)
	}
}

// ── défaut / retour au template ───────────────────────────────────────────

// « default » et « valeur égale au template » : même fichier résultant (clé retirée), jamais de valeur figée.
func TestSetOverrideDefautEtValeurDuTemplateIdentiques250(t *testing.T) {
	orig := "---\nname: dev-backend\nmodel: opus\neffort: high\n---\n\n# Corps\n"
	run := func(model, effort *string) (string, map[string]any) {
		proj := c250Proj(t) // template : sonnet / medium
		c250Write(t, c250Companion(proj, "dev-backend"), orig)
		res := c250MustSet(t, proj, "dev-backend", model, effort)
		return c250Read(t, c250Companion(proj, "dev-backend")), res
	}
	viaDefault, r1 := run(s250("default"), s250("default"))
	viaTpl, r2 := run(s250("sonnet"), s250("medium"))
	want := "---\nname: dev-backend\n---\n\n# Corps\n"
	if viaDefault != want || viaTpl != want {
		t.Errorf("default : %q ; valeur du template : %q ; attendu %q (clés retirées, rien d'autre ne change)", viaDefault, viaTpl, want)
	}
	for i, r := range []map[string]any{r1, r2} {
		if r["modelSrc"] != "template" || r["effortSrc"] != "template" || r["model"] != "sonnet" || r["effort"] != "medium" {
			t.Errorf("résultat %d : %v, attendu valeurs du template, sources « template »", i, r)
		}
	}
}

// Retour au template : clé retirée, puis bloc retiré, puis compagnon supprimé (P5, removed).
func TestSetOverrideRetourAuTemplateJusquASuppression250(t *testing.T) {
	proj := c250Proj(t)
	p := c250Companion(proj, "implementation-planner")
	c250MustSet(t, proj, "implementation-planner", s250("opus"), s250("high"))
	c250MustSet(t, proj, "implementation-planner", s250("default"), nil)
	if got := c250Read(t, p); got != "---\neffort: high\n---\n" {
		t.Fatalf("une clé retirée : %q", got)
	}
	res := c250MustSet(t, proj, "implementation-planner", nil, s250("default"))
	if c250Exists(p) {
		t.Fatalf("compagnon sans plus aucun contenu : doit être supprimé (P5), reste %q", c250Read(t, p))
	}
	if res["removed"] != true {
		t.Errorf("removed : %v, attendu true", res["removed"])
	}
	// idempotent : « default » sur un compagnon absent ne le crée pas
	res = c250MustSet(t, proj, "implementation-planner", s250("default"), s250("default"))
	if c250Exists(p) || res["ok"] != true {
		t.Errorf("default sur compagnon absent : fichier créé ou refus : %v", res)
	}
}

// Bloc retiré mais corps conservé : le compagnon reste (il a du contenu), pas de removed.
func TestSetOverrideBlocRetireCorpsGarde250(t *testing.T) {
	proj := c250Proj(t)
	body := "# Adaptations projet\n\nTexte.\n"
	c250Write(t, c250Companion(proj, "dev-backend"), "---\nmodel: opus\neffort: high\n---\n"+body)
	res := c250MustSet(t, proj, "dev-backend", s250("default"), s250("default")) // « ↺ Tout par défaut »
	if got := c250Read(t, c250Companion(proj, "dev-backend")); got != body {
		t.Errorf("après « Tout par défaut » : %q, attendu le corps seul %q", got, body)
	}
	if res["removed"] != false {
		t.Errorf("removed : %v, le compagnon a encore du contenu", res["removed"])
	}
}

// Clé absente de la requête : non modifiée.
func TestSetOverrideCleAbsenteNonModifiee250(t *testing.T) {
	proj := c250Proj(t)
	c250Write(t, c250Companion(proj, "dev-backend"), "---\nmodel: opus\neffort: high\n---\nX\n")
	c250MustSet(t, proj, "dev-backend", nil, s250("low"))
	if got := c250Read(t, c250Companion(proj, "dev-backend")); got != "---\nmodel: opus\neffort: low\n---\nX\n" {
		t.Errorf("seul effort change : %q", got)
	}
}

// Deux clés en un appel.
func TestSetOverrideDeuxClesEnUnAppel250(t *testing.T) {
	proj := c250Proj(t)
	c250MustSet(t, proj, "deploy", s250("haiku"), s250("xhigh"))
	m, e := frontKeys(c250Companion(proj, "deploy"))
	if m != "haiku" || e != "xhigh" {
		t.Errorf("deux clés : model=%q effort=%q", m, e)
	}
}

// ── instance générique ────────────────────────────────────────────────────

// Instance générique : écrite dans generic.<nom>.md ; référence = generic.md s'il fixe la clé, sinon generic.template.md.
func TestSetOverrideInstanceGenerique250(t *testing.T) {
	proj := c250Proj(t) // generic.template.md : sonnet / medium
	agents := filepath.Join(proj, ".claude", "agents")
	genericMd := "---\nmodel: haiku\n---\n# adaptations de toutes les instances\n"
	c250Write(t, filepath.Join(agents, "generic.md"), genericMd)
	tplHash := c250Hash(c250Read(t, filepath.Join(agents, "generic.template.md")))

	// valeur égale à la référence (generic.md : haiku) → clé retirée, aucun fichier d'instance créé
	res := c250MustSet(t, proj, "redacteur", s250("haiku"), nil)
	if c250Exists(filepath.Join(agents, "generic.redacteur.md")) {
		t.Errorf("model=haiku égal à generic.md : aucun compagnon d'instance attendu, trouvé %q", c250Read(t, filepath.Join(agents, "generic.redacteur.md")))
	}
	if res["tplModel"] != nil && res["tplModel"] != "haiku" {
		t.Errorf("référence de l'instance : %v, attendu haiku (generic.md)", res["tplModel"])
	}
	// autre valeur → generic.redacteur.md, generic.md et template intacts
	res = c250MustSet(t, proj, "redacteur", s250("opus"), s250("high"))
	if got := c250Read(t, filepath.Join(agents, "generic.redacteur.md")); got != "---\nmodel: opus\neffort: high\n---\n" {
		t.Errorf("generic.redacteur.md : %q", got)
	}
	if res["file"] != "generic.redacteur.md" {
		t.Errorf("file : %v, attendu generic.redacteur.md", res["file"])
	}
	if c250Read(t, filepath.Join(agents, "generic.md")) != genericMd {
		t.Error("generic.md modifié")
	}
	if c250Hash(c250Read(t, filepath.Join(agents, "generic.template.md"))) != tplHash {
		t.Error("generic.template.md modifié")
	}
}

// ReadTeammates : repli sur generic.md pour l'instance (correction de la résolution), puis generic.template.md.
func TestReadTeammatesRepliGenericMd250(t *testing.T) {
	proj := c250Proj(t)
	agents := filepath.Join(proj, ".claude", "agents")
	get := func() Teammate {
		for _, tm := range ReadTeammates(proj) {
			if tm.Name == "redacteur" {
				return tm
			}
		}
		t.Fatal("instance redacteur absente de ReadTeammates")
		return Teammate{}
	}
	if tm := get(); tm.Model != "sonnet" || tm.Effort != "medium" {
		t.Errorf("sans generic.md : %s/%s, attendu sonnet/medium (generic.template.md)", tm.Model, tm.Effort)
	}
	c250Write(t, filepath.Join(agents, "generic.md"), "---\nmodel: haiku\n---\ncorps\n")
	if tm := get(); tm.Model != "haiku" || tm.Effort != "medium" {
		t.Errorf("avec generic.md (model seul) : %s/%s, attendu haiku/medium", tm.Model, tm.Effort)
	}
	c250Write(t, filepath.Join(agents, "generic.redacteur.md"), "---\neffort: max\n---\n")
	if tm := get(); tm.Model != "haiku" || tm.Effort != "max" {
		t.Errorf("avec generic.redacteur.md : %s/%s, attendu haiku/max", tm.Model, tm.Effort)
	}
}

// ── refus ─────────────────────────────────────────────────────────────────

// Refus : agent hors registre, nom invalide, valeur hors liste, entrée non éditable. Rien n'est écrit, template intact.
func TestSetOverrideRefusSansEcriture250(t *testing.T) {
	cases := []struct {
		name          string
		agent         string
		model, effort *string
	}{
		{"hors registre", "inconnu", s250("opus"), nil},
		{"hors registre avec .template", "implementation-planner.template", s250("opus"), nil},
		{"traversée ../x", "../x", s250("opus"), nil},
		{"traversée ../../etc/passwd", "../../etc/passwd", s250("opus"), nil},
		{"séparateur /", "a/b", s250("opus"), nil},
		{"séparateur antislash", `a\b`, s250("opus"), nil},
		{"point seul", ".", s250("opus"), nil},
		{"deux points", "..", s250("opus"), nil},
		{"avec ..", "dev..backend", s250("opus"), nil},
		{"majuscules", "Dev-Backend", s250("opus"), nil},
		{"vide", "", s250("opus"), nil},
		{"trop long", strings.Repeat("a", 65), s250("opus"), nil},
		{"cdp non éditable", "cdp", s250("opus"), nil},
		{"modèle hors liste", "dev-backend", s250("gpt-5"), nil},
		{"modèle vide", "dev-backend", s250(""), nil},
		{"modèle majuscules", "dev-backend", s250("OPUS"), nil},
		{"modèle avec espace", "dev-backend", s250("opus "), nil},
		{"modèle injection de ligne", "dev-backend", s250("opus\nhooks: x"), nil},
		{"effort hors liste", "dev-backend", nil, s250("ultra")},
		{"effort vide", "dev-backend", nil, s250("")},
		{"effort injection de ligne", "dev-backend", nil, s250("high\nmodel: x")},
	}
	for _, c := range cases {
		proj := c250Proj(t)
		before := snapshot250(t, proj)
		if _, err := c250Set(t, proj, c.agent, c.model, c.effort); err == nil {
			t.Errorf("%s : refus attendu (erreur), l'appel a réussi", c.name)
		}
		if d := diff250(before, snapshot250(t, proj)); d != "" {
			t.Errorf("%s : fichiers modifiés malgré le refus : %s", c.name, d)
		}
	}
}

// Un nom hors du projet ne fait rien non plus à côté du projet (aucun fichier créé dans le dossier parent).
func TestSetOverrideTraverseeSansEffetHorsProjet250(t *testing.T) {
	parent := t.TempDir()
	proj := filepath.Join(parent, "proj")
	_ = os.MkdirAll(filepath.Join(proj, ".claude", "agents"), 0o755)
	c250Write(t, filepath.Join(proj, ".claude", "project-config.json"), `{"agents":{"registry":[{"name":"x"}]}}`)
	c250Write(t, filepath.Join(proj, ".claude", "agents", "x.template.md"), "---\nmodel: sonnet\neffort: medium\n---\n")
	before := snapshot250(t, parent)
	for _, a := range []string{"../../evil", "../evil", "..%2f..%2fevil"} {
		_, _ = c250Set(t, proj, a, s250("opus"), nil)
	}
	if d := diff250(before, snapshot250(t, parent)); d != "" {
		t.Errorf("écriture hors de .claude/agents : %s", d)
	}
}

// Compagnon qui est un lien symbolique : refus, la cible n'est pas modifiée.
func TestSetOverrideLienSymboliqueRefuse250(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("liens symboliques : Linux seulement")
	}
	proj := c250Proj(t)
	outside := filepath.Join(t.TempDir(), "cible.md")
	c250Write(t, outside, "---\nmodel: haiku\n---\nsecret\n")
	link := c250Companion(proj, "dev-backend")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip("lien symbolique impossible : " + err.Error())
	}
	before := c250Read(t, outside)
	if _, err := c250Set(t, proj, "dev-backend", s250("opus"), nil); err == nil {
		t.Error("compagnon lien symbolique : refus attendu")
	}
	if c250Read(t, outside) != before {
		t.Error("la cible du lien symbolique a été modifiée")
	}
	if st, err := os.Lstat(link); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Error("le lien symbolique a été remplacé")
	}
	// même chose pour « default » (qui aurait supprimé le fichier)
	if _, err := c250Set(t, proj, "dev-backend", s250("default"), s250("default")); err == nil {
		t.Error("compagnon lien symbolique + default : refus attendu")
	}
	if st, err := os.Lstat(link); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Error("le lien symbolique a été supprimé par « default »")
	}
}

// critical — jamais d'écriture dans <agent>.template.md, quel que soit l'appel (hash avant/après de tous les templates).
func TestSetOverrideJamaisDEcritureDansTemplate250(t *testing.T) {
	proj := c250Proj(t)
	agents := filepath.Join(proj, ".claude", "agents")
	templates := func() map[string]string {
		out := map[string]string{}
		files, _ := filepath.Glob(filepath.Join(agents, "*.template.md"))
		for _, f := range files {
			out[filepath.Base(f)] = c250Hash(c250Read(t, f))
		}
		return out
	}
	before := templates()
	for _, a := range []string{"implementation-planner", "dev-backend", "deploy", "teamleader", "redacteur"} {
		c250MustSet(t, proj, a, s250("opus"), s250("max"))
		c250MustSet(t, proj, a, s250("default"), s250("default"))
		c250MustSet(t, proj, a, s250("sonnet"), s250("medium")) // = template
		for _, bad := range []string{"cdp", a + ".template", "../agents/" + a + ".template"} {
			_, _ = c250Set(t, proj, bad, s250("haiku"), nil)
		}
	}
	after := templates()
	if len(before) != len(after) {
		t.Fatalf("templates : %d avant, %d après", len(before), len(after))
	}
	for k, h := range before {
		if after[k] != h {
			t.Errorf("%s modifié", k)
		}
	}
}

// ── concurrence et atomicité ──────────────────────────────────────────────

// Deux écritures simultanées (et plus) : fichier valide, pas de temporaire ni de verrou laissé dans .claude/agents.
func TestSetOverrideConcurrenceFichierValide250(t *testing.T) {
	proj := c250Proj(t)
	body := "# Corps à conserver\n\nligne\n"
	c250Write(t, c250Companion(proj, "dev-backend"), "---\nname: dev-backend\nmodel: haiku\n---\n"+body)
	models := []string{"opus", "sonnet", "haiku", "fable"}
	efforts := []string{"low", "medium", "high", "xhigh", "max"}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []string
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := c250Set(t, proj, "dev-backend", s250(models[i%len(models)]), s250(efforts[i%len(efforts)]))
			if err != nil {
				mu.Lock()
				errs = append(errs, err.Error())
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	for _, e := range errs {
		t.Errorf("écriture concurrente refusée : %s", e)
	}
	got := c250Read(t, c250Companion(proj, "dev-backend"))
	if !strings.HasSuffix(got, "---\n"+body) || !strings.HasPrefix(got, "---\nname: dev-backend\n") {
		t.Errorf("fichier invalide après écritures concurrentes : %q", got)
	}
	if strings.Count(got, "model:") != 1 || strings.Count(got, "effort:") != 1 {
		t.Errorf("clés dupliquées : %q", got)
	}
	m, e := frontKeys(c250Companion(proj, "dev-backend"))
	okM, okE := false, false
	for _, v := range models {
		okM = okM || v == m
	}
	for _, v := range efforts {
		okE = okE || v == e
	}
	if !okM || !okE {
		t.Errorf("valeurs finales hors liste : model=%q effort=%q", m, e)
	}
	ents, _ := os.ReadDir(filepath.Join(proj, ".claude", "agents"))
	for _, en := range ents {
		n := en.Name()
		if strings.Contains(n, ".tmp") || strings.HasSuffix(n, ".lock") || strings.Contains(n, ".part") {
			t.Errorf("fichier temporaire ou verrou laissé : %s", n)
		}
	}
}

// Mode du compagnon conservé (0640 existant), défaut 0644 à la création.
func TestSetOverrideModeConserve250(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("modes POSIX")
	}
	proj := c250Proj(t)
	p := c250Companion(proj, "dev-backend")
	c250Write(t, p, "---\nmodel: haiku\n---\nX\n")
	_ = os.Chmod(p, 0o640)
	c250MustSet(t, proj, "dev-backend", s250("opus"), nil)
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o640 {
		t.Errorf("mode %v, attendu 0640 conservé", st.Mode().Perm())
	}
	c250MustSet(t, proj, "deploy", s250("opus"), nil)
	if st, _ := os.Stat(c250Companion(proj, "deploy")); st.Mode().Perm() != 0o644 {
		t.Errorf("mode à la création %v, attendu 0644", st.Mode().Perm())
	}
}

// ── ReadTeammates / TeamLines (P3) ────────────────────────────────────────

// Modèle/effort résolus (compagnon > template) pour tous ; sources et références ; editable.
// Noms de champs du plan (tâche 1) : Teammate.ModelSrc, EffortSrc, TplModel, TplEffort, Editable.
func TestReadTeammatesResolutionSourcesEditable250(t *testing.T) {
	proj := c250Proj(t)
	c250Write(t, c250Companion(proj, "dev-backend"), "---\nmodel: opus\n---\nX\n")
	by := map[string]Teammate{}
	for _, tm := range ReadTeammates(proj) {
		by[tm.Name] = tm
	}
	d := by["dev-backend"]
	if d.Model != "opus" || d.Effort != "medium" {
		t.Errorf("dev-backend résolu %s/%s, attendu opus/medium", d.Model, d.Effort)
	}
	if d.ModelSrc != "companion" || d.EffortSrc != "template" || d.TplModel != "sonnet" || d.TplEffort != "medium" || !d.Editable {
		t.Errorf("dev-backend : sources %q/%q, références %q/%q, editable %v ; attendu companion/template, sonnet/medium, true",
			d.ModelSrc, d.EffortSrc, d.TplModel, d.TplEffort, d.Editable)
	}
	p := by["implementation-planner"]
	if p.ModelSrc != "template" || p.EffortSrc != "template" || !p.Editable {
		t.Errorf("planner sans compagnon : %q/%q editable %v", p.ModelSrc, p.EffortSrc, p.Editable)
	}
	if by["cdp"].Editable {
		t.Error("cdp (sans model:/effort: dans son template) ne doit pas être éditable")
	}
}

// TeamLines : ligne d'un teammate démarré = valeur résolue (pas le modèle du spawn) ; running.model = modèle réel s'il diffère.
func TestTeamLinesValeurResolueEtRunning250(t *testing.T) {
	proj := c250Proj(t)
	c250Write(t, c250Companion(proj, "dev-backend"), "---\nmodel: opus\n---\nX\n")
	tm := ReadTeammates(proj)
	panes := []ProjPane{{PaneInfo: PaneInfo{ID: "%1"}}, {PaneInfo: PaneInfo{ID: "%2"}}, {PaneInfo: PaneInfo{ID: "%3"}}}
	members := map[string]TeamMember{
		"%2": {Name: "dev-backend", AgentType: "dev-backend", PaneID: "%2", Model: "sonnet"}, // tourne en sonnet, résolu opus
		"%3": {Name: "deploy", AgentType: "deploy", PaneID: "%3", Model: "sonnet"},           // tourne comme résolu
	}
	lines := TeamLinesFor(tm, members, panes, nil, 0, 1000)
	by := map[string]TeamLine{}
	for _, l := range lines {
		by[l.Name] = l
	}
	d := by["dev-backend"]
	if d.Model != "opus" || d.Effort != "medium" {
		t.Errorf("dev-backend démarré : %s/%s, attendu la valeur résolue opus/medium (P3)", d.Model, d.Effort)
	}
	if d.ModelSrc != "companion" || d.TplModel != "sonnet" || !d.Editable {
		t.Errorf("dev-backend : modelSrc %q tplModel %q editable %v", d.ModelSrc, d.TplModel, d.Editable)
	}
	if r := fmt.Sprintf("%v", d.Running); !strings.Contains(r, "sonnet") {
		t.Errorf("running de dev-backend : %q, attendu le modèle réel sonnet (≠ résolu opus)", r)
	}
	// modèle réel = résolu : pas de running (pas de ⟳)
	dep := by["deploy"]
	if r := fmt.Sprintf("%v", dep.Running); strings.Contains(r, "sonnet") {
		t.Errorf("running de deploy : %q, aucun écart avec la valeur résolue", r)
	}
	// non démarré : valeur résolue aussi
	if p := by["implementation-planner"]; p.Started || p.Model != "sonnet" || p.Effort != "medium" {
		t.Errorf("planner non démarré : started=%v %s/%s", p.Started, p.Model, p.Effort)
	}
}
