package main

import (
	"strconv"
	"strings"
	"testing"
)

// LOT-3.2 Batch 0 (#275, E1 / E1b) — limites et réseaux privés dans le chart (contracts/helm-values.md « users.* »,
// contracts/user-backends.md §3 et §4) : users.maxProjects (entier 1–1000, défaut 30) et users.allowPrivateNetworks (booléen, défaut
// false, transmis par CL_USERS_ALLOW_PRIVATE). Réutilise u257Vals / u257Env (chart_users_test.go). Écrits avant le code.

func TestChartProjects275Defauts(t *testing.T) {
	needHelm(t)
	v := u257Vals()
	if got, ok := u257Env(t, v, "MAX", "PROJECTS"); !ok || got != "30" {
		t.Errorf("users.maxProjects par défaut : %q (présent=%v), 30 attendu", got, ok)
	}
	e := env(t, must(t, v))
	if got, ok := e["CL_USERS_ALLOW_PRIVATE"]; !ok || got != "false" {
		t.Errorf("CL_USERS_ALLOW_PRIVATE par défaut : %q (présent=%v), « false » attendu", got, ok)
	}
}

func TestChartProjects275ValeursRendues(t *testing.T) {
	needHelm(t)
	for _, n := range []int{1, 50, 1000} {
		v := u257Vals()
		sub(v, "users")["maxProjects"] = n
		if got, ok := u257Env(t, v, "MAX", "PROJECTS"); !ok || got != strconv.Itoa(n) {
			t.Errorf("maxProjects=%d : rendu %q", n, got)
		}
	}
	v := u257Vals()
	sub(v, "users")["allowPrivateNetworks"] = true
	if got := env(t, must(t, v))["CL_USERS_ALLOW_PRIVATE"]; got != "true" {
		t.Errorf("allowPrivateNetworks=true : CL_USERS_ALLOW_PRIVATE = %q", got)
	}
}

func TestChartProjects275BornesDuSchema(t *testing.T) {
	needHelm(t)
	for name, tc := range map[string]struct {
		key string
		val any
	}{
		"maxProjects 0":               {"maxProjects", 0},
		"maxProjects 1001":            {"maxProjects", 1001},
		"maxProjects négatif":         {"maxProjects", -1},
		"maxProjects texte":           {"maxProjects", "beaucoup"},
		"maxProjects décimal":         {"maxProjects", 2.5},
		"allowPrivateNetworks texte":  {"allowPrivateNetworks", "oui"},
		"allowPrivateNetworks entier": {"allowPrivateNetworks", 1},
	} {
		v := u257Vals()
		sub(v, "users")[tc.key] = tc.val
		if _, out, err := render(t, v); err == nil {
			t.Errorf("%s : rendu accepté", name)
		} else if !strings.Contains(out, "users") && !strings.Contains(out, tc.key) {
			t.Errorf("%s : message sans « users » ni %s : %.200s", name, tc.key, out)
		}
	}
}

// NR : sans users.* le rendu reste celui de LOT-3.1 (maxBackends 10, tests 6) ; les nouveaux paramètres ne cassent rien.
func TestChartProjects275NonRegressionLot31(t *testing.T) {
	needHelm(t)
	v := u257Vals()
	if got, ok := u257Env(t, v, "MAX", "BACKENDS"); !ok || got != "10" {
		t.Errorf("maxBackends : %q", got)
	}
	sub(v, "users")["enabled"] = false
	sub(v, "users")["allowPrivateNetworks"] = true
	must(t, v)
}

// D3 — users.runAs.uid / users.runAs.gid : entiers ≥ 1, défaut 2000/2000 (jamais root) ; transmis au binaire (variable dont le nom contient
// RUN, AS et UID / GID : le dev choisit le nom exact).
func TestChartRunAs275(t *testing.T) {
	needHelm(t)
	v := u257Vals()
	if got, ok := u257Env(t, v, "RUN", "UID"); !ok || got != "2000" {
		t.Errorf("users.runAs.uid par défaut : %q (présent=%v), 2000 attendu", got, ok)
	}
	if got, ok := u257Env(t, v, "RUN", "GID"); !ok || got != "2000" {
		t.Errorf("users.runAs.gid par défaut : %q (présent=%v), 2000 attendu", got, ok)
	}
	sub(v, "users")["runAs"] = m{"uid": 1500, "gid": 1600}
	if got, _ := u257Env(t, v, "RUN", "UID"); got != "1500" {
		t.Errorf("runAs.uid=1500 : %q", got)
	}
	if got, _ := u257Env(t, v, "RUN", "GID"); got != "1600" {
		t.Errorf("runAs.gid=1600 : %q", got)
	}
	for name, ra := range map[string]m{
		"uid 0": {"uid": 0, "gid": 2000}, "gid 0": {"uid": 2000, "gid": 0}, "uid négatif": {"uid": -1, "gid": 2000}, "uid texte": {"uid": "root", "gid": 2000},
		"uid décimal": {"uid": 2.5, "gid": 2000}, "uid énorme": {"uid": 4294967296, "gid": 2000}, "clé inconnue": {"uid": 2000, "gid": 2000, "groups": []any{0}},
	} {
		w := u257Vals()
		sub(w, "users")["runAs"] = ra
		if _, out, err := render(t, w); err == nil {
			t.Errorf("runAs %s : rendu accepté", name)
		} else if !strings.Contains(out, "runAs") && !strings.Contains(out, "users") {
			t.Errorf("runAs %s : message sans « runAs » ni « users » : %.200s", name, out)
		}
	}
}
