package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// LOT-3.2 Batch 2 (#275, tâches 13–16) — projets sur les backends de l'utilisateur : découverte et isolement par cl.user, limite de projets,
// cycle de vie sur kubectl/docker factices, état d'un projet arrêté (cl.meta, state.json), F5 / ouverture / sondage (P20). Écrits AVANT le code
// (sans lire celui des tâches 13–16). Étiquette « s275proj » (le paquet compile sans elle) ; API PROPOSÉE, à adopter ou renommer :
//
//	type ObjMeta struct{ Name string; Labels, Annotations map[string]string; Running bool }
//	func OwnedProjects(uid string, objs []ObjMeta) []ObjMeta                        // cl.launcher=ng2 ET cl.user==uid ; hors cl.state=old / cl.recreate-of
//	func (sp Space) CreateProject(e Engine, cat, name, repo string) error           // objet labellisé (+ clone dans le conteneur si repo), limite maxProjects
//	func (sp Space) DeleteProject(e Engine, cat, name string) error                 // dossier puis objet ; refuse un objet dont cl.user ≠ uid
//	func (sp Space) MoveProject(e Engine, cat, name, newCat, newName string) error  // même backend seulement
//	type ProjectMeta struct{ Branch string; Dirty bool; Ahead, Behind int; Head, Tpl string; Team []string; Seen time.Time }
//	func BuildClMeta(m ProjectMeta) (string, error)                                 // JSON ≤ 4 Kio, schéma fixe, marshalé par Go
//	func ParseProjectState(raw []byte) (ProjectMeta, error)                         // cl.meta ou state.json : ≤ 4 Kio, schéma fixe, bornes
//	type StartPlan struct{ Start, Session, Reread bool }
//	func PlanStart(trigger string, stoppedByUser bool) StartPlan                    // trigger : « open » | « f5 » | « probe »
//
// Faux outils : rcTools (userrecreate_e1c_test.go) — « ps.out » liste les objets (« nom label=valeur … »), « owner » donne cl.user.

func ptools(t *testing.T) (rc, Space, Engine) {
	t.Helper()
	r := rcTools(t, true)
	b := rcBackend(t)
	sp, _ := UserSpace("github-9")
	return r, sp, Engine{b}
}

func obj(name, uid string, extra ...string) ObjMeta {
	l := map[string]string{"cl.launcher": "ng2", "cl.user": uid, "cl.project": name, "cl.category": "cat", "cl.backend": "d"}
	for i := 0; i+1 < len(extra); i += 2 {
		l[extra[i]] = extra[i+1]
	}
	return ObjMeta{Name: "cl-" + name, Labels: l}
}

// ── découverte et isolement (T10, T13) ────────────────────────────────────────────────────────────────

func TestProj275DecouverteParLabelEtIsolement(t *testing.T) {
	objs := []ObjMeta{
		obj("mien", "github-9"), obj("tiers", "github-10"), obj("sans-label", ""), obj("usurpe", "github-9x"), obj("casse", "GITHUB-9"),
		obj("ancien", "github-9", "cl.state", "old"), obj("sauvegarde", "github-9", "cl.recreate-of", "cl-mien"),
		{Name: "autre-outil", Labels: map[string]string{"app": "x"}},
		{Name: "pas-ng2", Labels: map[string]string{"cl.user": "github-9"}}, // cl.launcher manquant
	}
	got := OwnedProjects("github-9", objs)
	if len(got) != 1 || got[0].Name != "cl-mien" {
		var names []string
		for _, g := range got {
			names = append(names, g.Name)
		}
		t.Errorf("projets découverts : %v (seulement cl-mien : jamais un objet d'un autre uid, sans label, usurpé, ancien ou sauvegarde)", names)
	}
	if len(OwnedProjects("github-10", objs)) != 1 || len(OwnedProjects("", objs)) != 0 || len(OwnedProjects("github-", objs)) != 0 {
		t.Error("isolement par uid")
	}
}

// Limite : les objets cl.user EXISTANTS sont comptés (jamais ceux d'un autre, ni les sauvegardes) ; le 31e est refusé AVANT toute commande.
func TestProj275LimiteDeProjets(t *testing.T) {
	r, sp, e := ptools(t)
	var ps strings.Builder
	for i := 0; i < 30; i++ {
		ps.WriteString("cl-p" + string(rune('a'+i/26)) + string(rune('a'+i%26)) + " cl.launcher=ng2 cl.user=github-9 cl.category=cat\n")
	}
	for i := 0; i < 5; i++ { // d'autres comptes et des sauvegardes : non comptés
		ps.WriteString("cl-x" + string(rune('a'+i)) + " cl.launcher=ng2 cl.user=github-10\n")
	}
	ps.WriteString("cl-vieux cl.launcher=ng2 cl.user=github-9 cl.state=old\n")
	_ = os.WriteFile(filepath.Join(r.dir, "ps.out"), []byte(ps.String()), 0o644)
	_ = os.Remove(r.log)
	err := sp.CreateProject(e, "cat", "trente-et-un", "")
	if err == nil || !strings.Contains(err.Error(), "nombre maximal de projets atteint (30)") {
		t.Fatalf("31e projet : %v", err)
	}
	for _, c := range r.calls() {
		if strings.HasPrefix(c, "create") || strings.HasPrefix(c, "start") || strings.HasPrefix(c, "run") {
			t.Errorf("commande de création lancée malgré le refus : %s", c)
		}
	}
	// 29 projets : le 30e passe
	_ = os.WriteFile(filepath.Join(r.dir, "ps.out"), []byte(strings.Join(strings.Split(ps.String(), "\n")[1:], "\n")), 0o644)
	if err := sp.CreateProject(e, "cat", "trentieme", ""); err != nil {
		t.Errorf("30e projet refusé : %v", err)
	}
}

// ── cycle de vie sur faux moteur : créer / cloner / déplacer / supprimer ───────────────────────────────

func TestProj275CreerLabelliseEtSansStockageCree(t *testing.T) {
	r, sp, e := ptools(t)
	if err := sp.CreateProject(e, "cat", "proj", ""); err != nil {
		t.Fatal(err)
	}
	calls := strings.Join(r.calls(), "\n")
	for _, want := range []string{"create", "cl.user=github-9", "cl.category=cat", "cl.project=proj", "cl.launcher=ng2", "cl.backend=d", "cl.created="} {
		if !strings.Contains(calls, want) {
			t.Errorf("« %s » absent des commandes :\n%s", want, calls)
		}
	}
	for _, bad := range []string{"volume create", "create pvc", " pvc ", "persistentvolumeclaim", "volume rm", "prune"} {
		if strings.Contains(strings.ToLower(calls), bad) {
			t.Errorf("le launcher ne crée ni ne supprime aucun stockage : « %s » dans\n%s", bad, calls)
		}
	}
}

func TestProj275NomsHostilesRefusesAvantToutAppel(t *testing.T) {
	r, sp, e := ptools(t)
	for _, bad := range [][2]string{{"cat", "foo-old-20261127"}, {"cat", "../x"}, {"cat", "A"}, {"cat", "a b"}, {"cat", "$(id)"}, {"cat", "a;touch x"}, {"cat", "--privileged"},
		{"../etc", "p"}, {"a/b", "p"}, {"Cat", "p"}, {"", "p"}, {"cat", ""}, {"cat", strings.Repeat("a", 64)}} {
		if err := sp.CreateProject(e, bad[0], bad[1], ""); err == nil {
			t.Errorf("création (%q, %q) acceptée", bad[0], bad[1])
		}
	}
	if len(r.calls()) != 0 {
		t.Errorf("commandes lancées pour des noms refusés : %v", r.calls())
	}
}

// Clone : l'URL est validée (ValidateCloneURL) ; le clone s'exécute DANS le conteneur sous l'uid du traitement, arguments positionnels après « -- ».
func TestProj275CloneDansLeConteneur(t *testing.T) {
	r, sp, e := ptools(t)
	for _, bad := range []string{"-upload-pack=touch /tmp/x", "ext::sh -c id", "https://u:p@h/x.git", "https://h/x.git\nevil", "file:///etc", "../x"} {
		if err := sp.CreateProject(e, "cat", "bad", bad); err == nil {
			t.Errorf("URL de clone %q acceptée", bad)
		}
	}
	_ = os.Remove(r.log)
	if err := sp.CreateProject(e, "cat", "clone", "https://github.com/CCoupel/Claude-Launcher.git"); err != nil {
		t.Fatal(err)
	}
	var clone string
	for _, c := range r.calls() {
		if strings.Contains(c, "git") && strings.Contains(c, "clone") {
			clone = c
		}
	}
	if clone == "" || !strings.Contains(clone, "exec") || !strings.Contains(clone, "--user=2000:2000") || !strings.Contains(clone, "-- https://github.com/CCoupel/Claude-Launcher.git") {
		t.Errorf("clone : %q (exec --user=2000:2000 … git clone -- <url> attendu)", clone)
	}
	if strings.Contains(clone, "sh -c") {
		t.Errorf("clone par un shell : %s", clone)
	}
}

// Règle n°1 (F2) : le git DU LAUNCHER n'est jamais lancé pour un projet d'utilisateur (tout passe par le conteneur) — faux git en tête du PATH.
func TestProj275AucunGitDuLauncher(t *testing.T) {
	_, sp, e := ptools(t)
	gdir := t.TempDir()
	glog := filepath.Join(gdir, "git.log")
	if err := os.WriteFile(filepath.Join(gdir, "git"), []byte("#!/bin/sh\necho \"$*\" >> \""+glog+"\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", gdir+string(os.PathListSeparator)+os.Getenv("PATH"))
	_ = sp.CreateProject(e, "cat", "proj", "")
	_ = sp.CreateProject(e, "cat", "clone", "https://github.com/CCoupel/Claude-Launcher.git")
	_ = sp.MoveProject(e, "cat", "proj", "cat2", "proj2")
	_ = sp.DeleteProject(e, "cat2", "proj2")
	if b, _ := os.ReadFile(glog); len(b) != 0 {
		t.Errorf("git du launcher lancé pour un projet d'utilisateur : %s", b)
	}
}

// Suppression et déplacement : refusés pour un objet d'un autre uid ; déplacement dans le MÊME backend seulement.
func TestProj275SupprimerEtDeplacer(t *testing.T) {
	r, sp, e := ptools(t)
	owner := filepath.Join(r.dir, "owner")
	_ = os.WriteFile(owner, []byte("github-10"), 0o644)
	_ = os.Remove(r.log)
	if err := sp.DeleteProject(e, "cat", "proj"); err == nil {
		t.Error("suppression d'un objet d'un autre utilisateur acceptée")
	}
	if err := sp.MoveProject(e, "cat", "proj", "cat2", "proj2"); err == nil {
		t.Error("déplacement d'un objet d'un autre utilisateur accepté")
	}
	for _, c := range r.calls() {
		if !strings.HasPrefix(c, "inspect") && !strings.HasPrefix(c, "ps") {
			t.Errorf("commande lancée sur l'objet d'un autre : %s", c)
		}
	}
	_ = os.WriteFile(owner, []byte("github-9"), 0o644)
	_ = os.Remove(r.log)
	if err := sp.MoveProject(e, "cat", "proj", "cat2", "proj2"); err != nil {
		t.Fatalf("déplacement : %v", err)
	}
	j := strings.Join(r.calls(), "\n")
	if !strings.Contains(j, "mv") || !strings.Contains(j, "--") || !strings.Contains(j, "cl.category=cat2") || !strings.Contains(j, "cl.project=proj2") {
		t.Errorf("déplacement : dossier déplacé (mv -- …) puis objet recréé aux nouveaux labels attendus :\n%s", j)
	}
	_ = os.Remove(r.log)
	if err := sp.DeleteProject(e, "cat2", "proj2"); err != nil {
		t.Fatalf("suppression : %v", err)
	}
	j = strings.Join(r.calls(), "\n")
	if !strings.Contains(j, "rm") || strings.Contains(j, "rm -v") || strings.Contains(j, "volume") {
		t.Errorf("suppression : rm de l'objet sans -v ni commande volume attendu :\n%s", j)
	}
}

// Purger : trace seulement — AUCUNE commande vers le backend (déjà couvert côté web ; ici la couche Space).
func TestProj275PurgerSansCommande(t *testing.T) {
	r, sp, _ := ptools(t)
	_ = sp.RecordTrace(Trace{Backend: "d", Kind: "docker", Object: "cl-proj", Project: "proj", Category: "cat"})
	_ = os.Remove(r.log)
	if n, err := sp.PurgeTrace("d", "proj"); err != nil || n != 1 {
		t.Fatalf("purge : %d %v", n, err)
	}
	if len(r.calls()) != 0 {
		t.Errorf("commande vers le backend pour une purge : %v", r.calls())
	}
}

// ── état d'un projet arrêté (cl.meta k8s, state.json docker) : affiché seulement, jamais exécuté ──────────

func TestProj275ClMetaBorne(t *testing.T) {
	m := ProjectMeta{Branch: "main", Dirty: true, Ahead: 1, Behind: 2, Head: "abc1234", Tpl: "v5.16.1", Team: []string{"leader", "dev"}, Seen: time.Now()}
	s, err := BuildClMeta(m)
	if err != nil || len(s) > 4096 {
		t.Fatalf("cl.meta : %v (%d octets)", err, len(s))
	}
	back, err := ParseProjectState([]byte(s))
	if err != nil || back.Branch != "main" || !back.Dirty || back.Ahead != 1 || back.Behind != 2 {
		t.Errorf("aller-retour : %+v %v", back, err)
	}
	// trop gros : refusé (jamais tronqué en silence ni écrit)
	m.Team = make([]string, 2000)
	for i := range m.Team {
		m.Team[i] = strings.Repeat("x", 20)
	}
	if s, err := BuildClMeta(m); err == nil && len(s) > 4096 {
		t.Errorf("cl.meta de %d octets accepté", len(s))
	}
}

func TestProj275EtatNonFiable(t *testing.T) {
	const canary = "CANARY-meta-1"
	for name, raw := range map[string]string{
		"5 Kio":               `{"branch":"` + strings.Repeat("a", 5000) + `"}`,
		"clé inconnue":        `{"branch":"main","cmd":"rm -rf /"}`,
		"type inattendu":      `{"branch":["a"],"dirty":"oui"}`,
		"branch avec balise":  `{"branch":"<script>alert(1)</script>"}`,
		"branch avec retour":  `{"branch":"a\nb"}`,
		"branch avec NUL":     "{\"branch\":\"a\\u0000b\"}",
		"nombre énorme":       `{"ahead":99999999999999999999}`,
		"négatif":             `{"ahead":-5}`,
		"JSON dupliqué":       `{"branch":"a","branch":"b"}`,
		"JSON invalide":       `{"branch":`,
		"canari dans une clé": `{"` + canary + `":1}`,
	} {
		got, err := ParseProjectState([]byte(raw))
		if err == nil {
			// accepté seulement s'il est INERTE : aucune balise, saut de ligne ni NUL ne survit
			for _, v := range []string{got.Branch, got.Head, got.Tpl} {
				if strings.ContainsAny(v, "<>\n\r\x00") {
					t.Errorf("%s : valeur non assainie %q", name, v)
				}
			}
			if name == "5 Kio" || name == "clé inconnue" || name == "JSON dupliqué" || name == "canari dans une clé" || name == "nombre énorme" || name == "négatif" || name == "type inattendu" || name == "JSON invalide" {
				t.Errorf("%s : accepté", name)
			}
		} else if strings.Contains(err.Error(), canary) {
			t.Errorf("%s : valeur du fichier citée dans l'erreur : %v", name, err)
		}
	}
}

// ── F5, ouverture et sondage (P20) ────────────────────────────────────────────────────────────────────

func TestProj275F5OuvertureEtSondage(t *testing.T) {
	if p := PlanStart("open", false); !p.Start || !p.Session {
		t.Errorf("Ouvrir : %+v (démarre et ouvre une session claude)", p)
	}
	if p := PlanStart("f5", false); !p.Start || p.Session || !p.Reread {
		t.Errorf("F5 : %+v (démarre SANS session claude, puis relit l'état)", p)
	}
	for _, stopped := range []bool{true, false} { // le sondage de 10 s ne relance JAMAIS un projet arrêté (P20)
		if p := PlanStart("probe", stopped); p.Start || p.Session {
			t.Errorf("sondage (arrêt volontaire=%v) : %+v (aucun démarrage)", stopped, p)
		}
	}
	if p := PlanStart("inconnu", false); p.Start || p.Session {
		t.Errorf("déclencheur inconnu : %+v (échec fermé)", p)
	}
}
