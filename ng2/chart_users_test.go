package main

import (
	"strconv"
	"strings"
	"testing"
)

// LOT-3.1 Batch 2 (#257, D5) — limites des backends d'utilisateur dans le chart (contracts/helm-values.md « users.* ») :
// users.maxBackends (entier 1–100, défaut 10) et users.backendTestsPerMinute (entier 1–60, défaut 6). Écrits avant le code ;
// aucun nom de variable d'environnement imposé : la valeur doit figurer dans l'environnement du conteneur launcher, sous un nom
// qui contient MAX_BACKENDS / TESTS (le lien chart → binaire est testé côté Go par le dev).

func u257Vals() m { return t172Ext() }

// u257Env : la valeur d'une variable du conteneur dont le nom contient tous les fragments.
func u257Env(t *testing.T, v m, frags ...string) (string, bool) {
	t.Helper()
	for name, val := range env(t, must(t, v)) {
		ok := true
		for _, f := range frags {
			ok = ok && strings.Contains(strings.ToUpper(name), f)
		}
		if ok {
			return val, true
		}
	}
	return "", false
}

func TestChartUsers257Defauts(t *testing.T) {
	needHelm(t)
	v := u257Vals()
	if got, ok := u257Env(t, v, "MAX", "BACKENDS"); !ok || got != "10" {
		t.Errorf("users.maxBackends par défaut : %q (présent=%v), 10 attendu", got, ok)
	}
	if got, ok := u257Env(t, v, "TEST", "MINUTE"); !ok || got != "6" {
		t.Errorf("users.backendTestsPerMinute par défaut : %q (présent=%v), 6 attendu", got, ok)
	}
}

func TestChartUsers257ValeursRendues(t *testing.T) {
	needHelm(t)
	for _, n := range []int{1, 25, 100} {
		v := u257Vals()
		sub(v, "users")["maxBackends"] = n
		if got, ok := u257Env(t, v, "MAX", "BACKENDS"); !ok || got != strconv.Itoa(n) {
			t.Errorf("maxBackends=%d : rendu %q", n, got)
		}
	}
	for _, n := range []int{1, 12, 60} {
		v := u257Vals()
		sub(v, "users")["backendTestsPerMinute"] = n
		if got, ok := u257Env(t, v, "TEST", "MINUTE"); !ok || got != strconv.Itoa(n) {
			t.Errorf("backendTestsPerMinute=%d : rendu %q", n, got)
		}
	}
}

func TestChartUsers257BornesDuSchema(t *testing.T) {
	needHelm(t)
	must(t, u257Vals())
	for name, tc := range map[string]struct {
		key string
		val any
	}{
		"maxBackends 0":                 {"maxBackends", 0},
		"maxBackends 101":               {"maxBackends", 101},
		"maxBackends négatif":           {"maxBackends", -3},
		"maxBackends texte":             {"maxBackends", "beaucoup"},
		"maxBackends décimal":           {"maxBackends", 2.5},
		"backendTestsPerMinute 0":       {"backendTestsPerMinute", 0},
		"backendTestsPerMinute 61":      {"backendTestsPerMinute", 61},
		"backendTestsPerMinute texte":   {"backendTestsPerMinute", "6/min"},
		"backendTestsPerMinute négatif": {"backendTestsPerMinute", -1},
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

// NR : users.enabled: false = comportement v7.0.0 ; les limites n'ont alors aucun effet requis mais ne cassent pas le rendu.
func TestChartUsers257EnabledFalseInchange(t *testing.T) {
	needHelm(t)
	v := u257Vals()
	sub(v, "users")["enabled"] = false
	sub(v, "users")["maxBackends"] = 5
	must(t, v)
}
