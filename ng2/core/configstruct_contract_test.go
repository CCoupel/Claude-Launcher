package core

import (
	"reflect"
	"strings"
	"testing"
)

// #118 (LOT-3.1) — compléments aux tests de configmodel_test.go : stabilité de
// l'écriture structurée (idempotence, atomicité), types YAML piégeux, fins de
// ligne Windows, doublon / suppression d'un backend cité, renommage en place.
// Aucun accès réseau, fichiers temporaires seulement.

func modelOf(t *testing.T, s string) BackendsModel {
	t.Helper()
	m, err := ReadBackendsModel([]byte(s))
	if err != nil {
		t.Fatalf("modèle : %v\n%s", err, s)
	}
	return m
}

// critical : aucune modification = même modèle et mêmes commentaires ; une
// seconde application à la sortie de la première ne change plus rien.
func TestApplyNoChangeIsStable(t *testing.T) {
	once := apply(t, modelSample)
	if !reflect.DeepEqual(modelOf(t, once), modelOf(t, modelSample)) {
		t.Fatalf("modèle changé par une application vide :\n%s", once)
	}
	for _, c := range []string{"# Backends de la maison — ne pas toucher aux IP", "# machines du labo", "# le principal",
		"# docker distant", "# IP fixe", "# cluster", "# présélection"} {
		if !strings.Contains(once, c) {
			t.Errorf("commentaire %q perdu par une application vide", c)
		}
	}
	if twice := apply(t, once); twice != once {
		t.Errorf("seconde application instable :\n--- 1 ---\n%s\n--- 2 ---\n%s", once, twice)
	}
	// set d'une valeur identique : modèle identique
	same := apply(t, modelSample, ConfigChange{Op: "set", At: []string{"defaults", "backend"}, Value: "srv1-docker"})
	if !reflect.DeepEqual(modelOf(t, same), modelOf(t, modelSample)) || !strings.Contains(same, "# présélection") {
		t.Errorf("set identique :\n%s", same)
	}
}

// Une liste de changements est atomique : si l'un échoue, rien n'est rendu.
func TestApplyIsAtomic(t *testing.T) {
	out, err := ApplyConfigChanges(ConfBackends, []byte(modelSample), []ConfigChange{
		{Op: "set", At: []string{"defaults", "image"}, Value: "x:1"},
		{Op: "delete", At: []string{"backends", "inexistant"}},
	})
	if err == nil || len(out) != 0 {
		t.Fatalf("changement partiel rendu : err=%v, %d octets", err, len(out))
	}
}

// Les chaînes que YAML lirait comme un autre type restent des chaînes après
// écriture et relecture (namespace « 0123 », nom « yes », image « 1.10 »…).
func TestApplyKeepsStringsAsStrings(t *testing.T) {
	out := apply(t, modelSample,
		ConfigChange{Op: "set", At: []string{"backends", "yes"}, Value: map[string]any{
			"type": "k8s", "context": "k8s", "namespace": "0123",
			"volumes": map[string]any{"/share/MFS/ROOT": "pvc"}}},
		ConfigChange{Op: "set", At: []string{"backends", "12"}, Value: map[string]any{"type": "podman"}},
		ConfigChange{Op: "set", At: []string{"defaults", "image"}, Value: "1.10"},
	)
	m := modelOf(t, out)
	got := map[string]BackendEntry{}
	for _, b := range m.Backends {
		got[b.Name] = b
	}
	if b, ok := got["yes"]; !ok || b.Namespace != "0123" {
		t.Errorf("« yes » / namespace « 0123 » : %+v\n%s", got["yes"], out)
	}
	if _, ok := got["12"]; !ok {
		t.Errorf("backend « 12 » perdu :\n%s", out)
	}
	if m.Defaults.Image != "1.10" {
		t.Errorf("image « 1.10 » devenue %q\n%s", m.Defaults.Image, out)
	}
}

// spread: false est un choix explicite (≠ absent) : il survit ; chemins
// Windows (barres inverses) et accents aussi.
func TestApplyKeepsExplicitFalseAndSpecialChars(t *testing.T) {
	out := apply(t, modelSample, ConfigChange{Op: "set", At: []string{"backends", "w"}, Value: map[string]any{
		"type": "k8s", "context": "k8s", "namespace": "ns", "spread": false,
		"shared":  []any{`C:\Projets\é été`},
		"volumes": map[string]any{`C:\Projets`: "pvc-é"}}})
	m := modelOf(t, out)
	var w BackendEntry
	for _, b := range m.Backends {
		if b.Name == "w" {
			w = b
		}
	}
	if w.Spread == nil || *w.Spread {
		t.Errorf("spread:false perdu : %+v\n%s", w.Spread, out)
	}
	if len(w.Shared) != 1 || w.Shared[0] != `C:\Projets\é été` || w.Volumes[`C:\Projets`] != "pvc-é" {
		t.Errorf("caractères spéciaux : %+v\n%s", w, out)
	}
}

// Fichier modifié sous Windows (CRLF) et/ou avec BOM : toujours lisible, valide,
// commentaires conservés.
func TestApplyWindowsLineEndings(t *testing.T) {
	for name, src := range map[string]string{
		"CRLF":     strings.ReplaceAll(modelSample, "\n", "\r\n"),
		"BOM+CRLF": "\ufeff" + strings.ReplaceAll(modelSample, "\n", "\r\n"),
	} {
		out, err := ApplyConfigChanges(ConfBackends, []byte(src), []ConfigChange{
			{Op: "set", At: []string{"defaults", "image"}, Value: "x:2"}})
		if err != nil {
			t.Errorf("%s : %v", name, err)
			continue
		}
		s := string(out)
		if !strings.Contains(s, "# IP fixe") || !strings.Contains(s, "# machines du labo") || !strings.Contains(s, "x:2") {
			t.Errorf("%s : commentaire ou valeur perdus :\n%s", name, s)
		}
		if HasErrors(ValidateConfig(ConfBackends, out)) {
			t.Errorf("%s : invalide : %v", name, ValidateConfig(ConfBackends, out))
		}
	}
}

// Dupliquer un backend (set sous un nouveau nom avec les champs du premier) :
// l'original reste intact, la copie est à la fin, les deux relus égaux.
func TestDuplicateBackend(t *testing.T) {
	m := modelOf(t, modelSample)
	src := m.Backends[0]
	out := apply(t, modelSample, ConfigChange{Op: "set", At: []string{"backends", src.Name + "-bis"}, Value: map[string]any{
		"type": src.Type, "host": src.Host, "shared": []any{src.Shared[0]}}})
	m2 := modelOf(t, out)
	if len(m2.Backends) != 4 || m2.Backends[3].Name != "srv1-docker-bis" {
		t.Fatalf("copie : %+v", m2.Backends)
	}
	a, b := m2.Backends[0], m2.Backends[3]
	b.Name = a.Name
	if !reflect.DeepEqual(a, b) || !reflect.DeepEqual(a, src) {
		t.Errorf("copie ≠ original : %+v / %+v / %+v", a, b, src)
	}
	if !strings.Contains(out, "# IP fixe") {
		t.Error("commentaire de l'original perdu")
	}
	if HasErrors(ValidateConfig(ConfBackends, []byte(out))) {
		t.Errorf("invalide : %v", ValidateConfig(ConfBackends, []byte(out)))
	}
}

// Suppression d'un élément encore cité : le fichier produit est refusé par la
// validation existante — pool utilisé par un backend, dernier hôte d'un pool,
// drain hors du pool. NB : supprimer le backend désigné par defaults.backend
// n'est PAS refusé par ValidateConfig : l'interface doit l'avertir (procédure
// config-backends, scénario 2).
func TestDeleteReferencedIsRefusedByValidation(t *testing.T) {
	cases := map[string][]ConfigChange{
		"pool cité par un backend":   {{Op: "delete", At: []string{"pools", "labo"}}},
		"dernière machine d'un pool": {{Op: "set", At: []string{"pools", "labo", "hosts"}, Value: []any{}}},
		"drain hors du pool":         {{Op: "set", At: []string{"pools", "labo", "drain"}, Value: []any{"inconnue"}}},
	}
	for name, ch := range cases {
		out, err := ApplyConfigChanges(ConfBackends, []byte(modelSample), ch)
		if err != nil {
			continue // refusé dès l'application : acceptable
		}
		if !HasErrors(ValidateConfig(ConfBackends, out)) {
			t.Errorf("%s : fichier accepté par la validation :\n%s", name, out)
		}
	}
}

// Renommer un backend : même place, commentaires accrochés conservés.
func TestRenameKeepsPositionAndComments(t *testing.T) {
	out := apply(t, modelSample, ConfigChange{Op: "rename", At: []string{"backends", "srv1-docker"}, Value: "principal"})
	m := modelOf(t, out)
	if m.Backends[0].Name != "principal" || m.Backends[1].Name != "lab" || len(m.Backends) != 3 {
		t.Fatalf("ordre après renommage : %+v", m.Backends)
	}
	if !strings.Contains(out, "# le principal") || !strings.Contains(out, "# IP fixe") {
		t.Errorf("commentaires perdus :\n%s", out)
	}
}
