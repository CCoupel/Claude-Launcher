package core

// #216 (complément) : garde d'écriture de l'environnement d'un projet (env.yaml, backend, image) — compléments boîte noire
// à envedit216_test.go : garde revérifiée à CHAQUE écriture, rien d'écrit sur un refus, couleur / organisation / masquage
// jamais gardés, « déplacement inachevé » écarté, approbation d'un dépôt cloné et création jamais bloquées.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const envGuardSample216 = "image: docker.io/library/debian:12\npackages: [jq]\n"

func guardProj216(t *testing.T) (proj string, change *EnvChange) {
	t.Helper()
	h := withHome(t)
	_ = os.WriteFile(BackendsPath(), nil, 0o644) // jamais de moteur détecté du PATH
	_ = os.MkdirAll(filepath.Dir(BackendsPath()), 0o755)
	_ = os.WriteFile(BackendsPath(), []byte("backends:\n  pod: {type: podman}\n"), 0o644)
	proj = filepath.Join(h, "PROJETS", "T", "alpha")
	_ = os.MkdirAll(filepath.Join(proj, ".claude-launcher"), 0o755)
	if err := os.WriteFile(EnvPath(proj), []byte(envGuardSample216), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(ConfigPath(), []byte(defaultConfigFor()), 0o600)
	st, err := os.Stat(EnvPath(proj))
	if err != nil {
		t.Fatal(err)
	}
	// date d'ouverture : sans elle, l'écriture d'un env.yaml existant est un conflit (le fichier a pu changer)
	return proj, &EnvChange{MTime: st.ModTime().Format(time.RFC3339Nano), Changes: []ConfigChange{{Op: "set", At: []string{"packages"}, Value: []any{"ripgrep"}}}}
}

func readF216(p string) string { b, _ := os.ReadFile(p); return string(b) }

var refus216 = []string{"projet ouvert dans ce launcher"}

// La garde est revérifiée à CHAQUE écriture : on la fait basculer en refus à chacun de ses appels successifs (le projet
// s'ouvre entre deux contrôles) ; quel que soit l'instant, un refus avant l'écriture de l'env.yaml n'écrit RIEN (ni env.yaml
// ni couleur), l'affectation n'est jamais changée, et un refus tardif (après l'env.yaml) le dit dans le résultat.
func TestGardeRevereifieeATousLesInstants216(t *testing.T) {
	be, img, color := "pod", "docker.io/library/alpine:3.20", "52"
	total := 0
	for k := 1; k <= 12; k++ {
		proj, env := guardProj216(t)
		_ = WriteAssignment(proj, "host")
		cfgBefore, envBefore := readF216(ConfigPath()), readF216(EnvPath(proj))
		calls, envAtFlip, flipped := 0, "", false
		r := ApplyProjectSettings(proj, false, ProjectChange{Env: env, Color: &color, Backend: &be, Image: &img, EnvGuard: func() []string {
			calls++
			if calls == k {
				flipped, envAtFlip = true, readF216(EnvPath(proj))
				return refus216
			}
			return nil
		}})
		if !flipped { // plus aucun appel d'indice k : le déplacement s'est déroulé sans basculer
			total = calls
			if !r.OK {
				t.Fatalf("sans basculement (%d appels) : %+v", calls, r)
			}
			break
		}
		if r.OK || r.Field != "env" {
			t.Errorf("bascule à l'appel %d : %+v, attendu un refus {field:env}", k, r)
			continue
		}
		if r.Msg != EnvEditMessage(refus216) || len(r.Blockers) != 1 || r.Blockers[0] != refus216[0] {
			t.Errorf("bascule à l'appel %d : msg=%q blockers=%v", k, r.Msg, r.Blockers)
		}
		if got := ProjectBackend(proj).Name; got != "host" {
			t.Errorf("bascule à l'appel %d : affectation changée en %q sous un projet ouvert", k, got)
		}
		if envAtFlip == envBefore { // refus avant toute écriture de l'env.yaml
			if readF216(EnvPath(proj)) != envBefore {
				t.Errorf("bascule à l'appel %d : env.yaml écrit APRÈS le refus", k)
			}
			if readF216(ConfigPath()) != cfgBefore {
				t.Errorf("bascule à l'appel %d : config.conf (couleur) écrit alors que l'env a été refusé", k)
			}
			if r.Env != nil && r.Env.Saved {
				t.Errorf("bascule à l'appel %d : EnvResult.Saved vrai sur un refus d'avant écriture", k)
			}
		} else if r.Env == nil || !r.Env.Saved { // refus tardif : l'env.yaml est déjà écrit, l'interface doit le savoir
			t.Errorf("bascule à l'appel %d (env.yaml déjà écrit) : résultat sans EnvResult.Saved : %+v", k, r)
		}
	}
	if total < 3 {
		t.Errorf("garde consultée %d fois seulement pour env.yaml + backend + image, attendu au moins 3 (avant toute écriture, avant l'env.yaml, avant le backend)", total)
	}
}

// Couleur, organisation, masquage : jamais gardés (à chaud), la garde n'est même pas consultée.
func TestCouleurOrganisationMasquageNonGardes216(t *testing.T) {
	proj, _ := guardProj216(t)
	calls := 0
	color, layout, hide := "52", "left", "none"
	r := ApplyProjectSettings(proj, true, ProjectChange{Color: &color, Layout: &layout, HideIdle: &hide, EnvGuard: func() []string {
		calls++
		return refus216
	}})
	if !r.OK || r.Field != "" || !r.Color {
		t.Fatalf("réglages à chaud sur projet ouvert : %+v", r)
	}
	if calls != 0 {
		t.Errorf("garde consultée %d fois pour des réglages hors environnement", calls)
	}
	cfg := LoadConfig()
	if cfg.ProjectColors["alpha"] != "52" || cfg.ProjectLayouts["alpha"] != "left" || cfg.ProjectHideIdle["alpha"] != "none" {
		t.Errorf("config relue : couleurs=%v organisations=%v masquage=%v", cfg.ProjectColors, cfg.ProjectLayouts, cfg.ProjectHideIdle)
	}
	if readF216(EnvPath(proj)) != envGuardSample216 {
		t.Error("env.yaml touché par des réglages hors environnement")
	}
}

// Backend et image sont de l'environnement : mêmes refus, rien n'est écrit (affectation, env.yaml).
func TestGardeBackendEtImage216(t *testing.T) {
	proj, _ := guardProj216(t)
	_ = WriteAssignment(proj, "host")
	envBefore := readF216(EnvPath(proj))
	be, img := "pod", "docker.io/library/alpine:3.20"
	for name, in := range map[string]ProjectChange{
		"backend + image": {Backend: &be, Image: &img},
	} {
		in.EnvGuard = func() []string { return refus216 }
		r := ApplyProjectSettings(proj, false, in)
		if r.OK || r.Field != "env" || !strings.Contains(r.Msg, "fermez le projet") || len(r.Blockers) != 1 {
			t.Errorf("%s : %+v, attendu un refus {field:env} avec la consigne", name, r)
		}
		if got := ProjectBackend(proj).Name; got != "host" {
			t.Errorf("%s : affectation changée en %q", name, got)
		}
		if got := readF216(EnvPath(proj)); got != envBefore {
			t.Errorf("%s : env.yaml écrit : %q", name, got)
		}
	}
	// image seule d'un projet déjà en conteneur
	_ = WriteAssignment(proj, "pod")
	r0 := ApplyProjectSettings(proj, false, ProjectChange{Image: &img, EnvGuard: func() []string { return refus216 }})
	if r0.OK || r0.Field != "env" || readF216(EnvPath(proj)) != envBefore {
		t.Errorf("image seule : %+v, env.yaml %q", r0, readF216(EnvPath(proj)))
	}
	_ = WriteAssignment(proj, "host")
	// projet ouvert SANS garde fournie : seul « ouvert ici » est connu, le refus est le même
	r := ApplyProjectSettings(proj, true, ProjectChange{Backend: &be, Image: &img})
	if r.OK || r.Field != "env" || r.Msg != EnvEditMessage([]string{"projet ouvert dans ce launcher"}) || len(r.Blockers) != 1 {
		t.Errorf("projet ouvert, sans garde : %+v, attendu la forme commune {field:env, msg, blockers[1]}", r)
	}
	if got := ProjectBackend(proj).Name; got != "host" {
		t.Errorf("affectation changée par un projet ouvert : %q", got)
	}
}

// Projet libre : tout passe, une seule écriture de l'env.yaml.
func TestGardeProjetLibreEcrit216(t *testing.T) {
	proj, env := guardProj216(t)
	r := ApplyProjectSettings(proj, false, ProjectChange{Env: env, EnvGuard: func() []string { return nil }})
	if !r.OK || r.Env == nil || !r.Env.Saved {
		t.Fatalf("projet libre : %+v", r)
	}
	if got := readF216(EnvPath(proj)); !strings.Contains(got, "ripgrep") || strings.Contains(got, "jq") {
		t.Errorf("env.yaml = %q", got)
	}
}

// « Déplacement inachevé » ne concerne pas l'environnement : il bloque le déplacement, pas l'édition de l'env.yaml.
func TestEnvEditIgnoreLeDeplacementInacheve216(t *testing.T) {
	proj, _ := guardProj216(t)
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	env := MoveEnv{OpenHere: func(string) bool { return false }, Foreign: func(string) string { return "" }, Running: func(string) string { return "" }}
	if err := saveEntry(&MoveEntry{ID: "x", From: proj, To: proj + "-dest", State: "partial", Done: []string{}}); err != nil {
		t.Fatal(err)
	}
	if bl := MoveBlockers(env, proj); !strings.Contains(strings.Join(bl, "|"), "déplacement inachevé") {
		t.Fatalf("préparation : MoveBlockers = %v", bl)
	}
	if bl := EnvEditBlockers(env, proj); len(bl) != 0 {
		t.Errorf("EnvEditBlockers = %v : un déplacement inachevé ne doit pas interdire l'édition de l'environnement", bl)
	}
	if m := EnvEditRefusal(env, proj); m != "" {
		t.Errorf("EnvEditRefusal = %q", m)
	}
}

// Flux jamais gardés : approbation d'un dépôt cloné, scellage par le launcher, création / clonage (ApplyExec) — même
// quand tous les blockers existent (la garde ne s'applique qu'aux entrées d'édition).
func TestFluxsExemptesDeLaGarde216(t *testing.T) {
	proj, _ := guardProj216(t)
	all := MoveEnv{OpenHere: func(string) bool { return true }, Foreign: func(string) string { return "pod-x" }, Running: func(string) string { return "c sur b" }}
	if len(EnvEditBlockers(all, proj)) != 3 {
		t.Fatal("préparation : 3 blockers attendus")
	}
	// approbation
	if ok, err := MarkEnvPending(proj, "https://github.com/a/b.git", "docker.io/library/debian:12"); !ok || err != nil {
		t.Fatalf("MarkEnvPending : %v %v", ok, err)
	}
	info, err := EnvTrustInfoFor(proj)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AnswerEnvTrust(proj, "accept", info.SHA); err != nil {
		t.Fatalf("AnswerEnvTrust : %v", err)
	}
	// écriture du launcher (création, F2 de première ouverture) : l'empreinte suit, aucune re-demande
	if err := ApplyExec(proj, "docker.io/library/alpine:3.20", "pod"); err != nil {
		t.Fatalf("ApplyExec : %v", err)
	}
	if EnvTrust(proj) != TrustAccepted || EnvTrustChanged(proj) {
		t.Errorf("après ApplyExec : %s changed=%v", EnvTrust(proj), EnvTrustChanged(proj))
	}
	// clonage : entrée posée avant, scellée après
	clone := filepath.Join(filepath.Dir(proj), "clone")
	_ = os.MkdirAll(clone, 0o755)
	if err := RegisterClone(clone, "https://github.com/a/c.git", "docker.io/library/debian:12"); err != nil {
		t.Fatalf("RegisterClone : %v", err)
	}
}
