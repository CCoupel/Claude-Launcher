//go:build s275api

package core

import (
	"strings"
	"testing"
)

// LOT-3.2 Batch 0 (#275, E1) — options par projet en LISTE BLANCHE FERMÉE, défauts, limite de projets (contracts/user-backends.md §5 bis).
// Étiquette « s275api » : les tests supposent l'API ci-dessous, que dev-backend peut adopter ou renommer (il me prévient, j'adapte) ; sans
// elle le paquet compile. Proposition :
//
//	type OptionProblem struct{ Field, Path, Code, Msg string }
//	type PodSimple struct{ CPURequest, CPULimit, MemRequest, MemLimit, Ephemeral string }            // champs k8s, quantités k8s
//	func ParsePodOptions(s PodSimple, spec map[string]any) (PodOptions, []OptionProblem)             // zone JSON « Spécification du pod »
//	type ContainerRaw struct{ Cpus, Memory, PidsLimit, ShmSize, Gpus string }                         // docker / podman
//	func ParseContainerOptions(r ContainerRaw) (ContainerOptions, []OptionProblem)
//	func DefaultPodSimple() PodSimple ; func DefaultContainerRaw() ContainerRaw
//	func CheckProjectLimit(existing int) *OptionProblem                                                // users.maxProjects (CL_USERS_MAX_PROJECTS)
//	func (po PodOptions) Apply(m map[string]any)                                                       // construit dans le manifeste, jamais recopié

func ok275(t *testing.T, ps []OptionProblem) {
	t.Helper()
	if len(ps) != 0 {
		t.Errorf("refus inattendu : %+v", ps)
	}
}

func hasPath(ps []OptionProblem, path string) *OptionProblem {
	for i := range ps {
		if ps[i].Path == path {
			return &ps[i]
		}
	}
	return nil
}

// Zone JSON : tout champ hors liste blanche est refusé avec le CHEMIN exact.
func TestE1ZoneJSONChampsInterdits(t *testing.T) {
	for _, k := range []string{"privileged", "hostNetwork", "hostPID", "hostIPC", "securityContext", "serviceAccountName", "runtimeClassName", "hostPath",
		"volumes", "ports", "containers", "initContainers", "nodeName", "capabilities", "cap_add", "inconnu", "automountServiceAccountToken", "dnsPolicy", "hostAliases"} {
		_, ps := ParsePodOptions(DefaultPodSimple(), map[string]any{k: true})
		p := hasPath(ps, "$."+k)
		if p == nil || p.Field != "pod.spec" || p.Code == "" || p.Msg == "" {
			t.Errorf("%s : refus nommé avec le chemin $.%s attendu : %+v", k, k, ps)
		}
	}
	// imbriqué : le chemin est exact
	_, ps := ParsePodOptions(DefaultPodSimple(), map[string]any{"affinity": map[string]any{"nodeAffinity": map[string]any{"hostNetwork": true}}})
	if len(ps) == 0 {
		t.Error("champ interdit imbriqué dans affinity accepté")
	}
	_, ps = ParsePodOptions(DefaultPodSimple(), map[string]any{"tolerations": []any{map[string]any{"key": "a", "operator": "Exists", "effect": "NoSchedule"}, map[string]any{"key": "b", "operator": "Exists", "effect": "Nope"}}})
	if p := hasPath(ps, "$.tolerations[1].effect"); p == nil || p.Code != "invalid" {
		t.Errorf("toleration[1].effect invalide : %+v", ps)
	}
}

// CPU / mémoire saisis dans la zone : « utilise les champs CPU / mémoire ».
func TestE1ZoneJSONCPUMemoireRefuses(t *testing.T) {
	for _, path := range []string{"cpu", "memory", "ephemeral-storage"} {
		for _, side := range []string{"requests", "limits"} {
			spec := map[string]any{"resources": map[string]any{side: map[string]any{path: "1"}}}
			_, ps := ParsePodOptions(DefaultPodSimple(), spec)
			p := hasPath(ps, "$.resources."+side+"."+path)
			if p == nil || !strings.Contains(strings.ToLower(p.Msg), "utilise le") {
				t.Errorf("%s.%s dans la zone : %+v (« utilise les champs CPU / mémoire » attendu)", side, path, ps)
			}
		}
	}
	// ressources étendues : acceptées
	_, ps := ParsePodOptions(DefaultPodSimple(), map[string]any{"resources": map[string]any{"limits": map[string]any{"nvidia.com/gpu": "1", "hugepages-2Mi": "512Mi"}}})
	ok275(t, ps)
}

// AUCUN plafond de CPU / mémoire imposé par le launcher (décision utilisateur) ; quantités invalides refusées.
func TestE1QuantitesSansPlafond(t *testing.T) {
	_, ps := ParsePodOptions(PodSimple{CPURequest: "100m", CPULimit: "64", MemRequest: "256Mi", MemLimit: "1Ti", Ephemeral: "500Gi"}, nil)
	ok275(t, ps)
	_, ps = ParsePodOptions(PodSimple{CPURequest: "64", CPULimit: "128", MemRequest: "1Ti", MemLimit: "2Ti"}, nil)
	ok275(t, ps)
	for name, s := range map[string]PodSimple{
		"cpu texte":                {CPURequest: "abc", CPULimit: "2"},
		"cpu négatif":              {CPURequest: "-1", CPULimit: "2"},
		"cpu unité inconnue":       {CPURequest: "1x", CPULimit: "2"},
		"mémoire texte":            {MemRequest: "beaucoup", MemLimit: "4Gi"},
		"mémoire unité":            {MemRequest: "5 Go", MemLimit: "4Gi"},
		"mémoire négative":         {MemRequest: "-5Mi", MemLimit: "4Gi"},
		"éphémère invalide":        {Ephemeral: "zz"},
		"limite < demande":         {CPURequest: "4", CPULimit: "2"},
		"mémoire limite < demande": {MemRequest: "8Gi", MemLimit: "4Gi"},
	} {
		_, ps := ParsePodOptions(s, nil)
		if len(ps) == 0 || ps[0].Field == "" || ps[0].Msg == "" {
			t.Errorf("%s : refus nommé attendu : %+v", name, ps)
		}
	}
}

func TestE1ZoneJSONFormeEtTaille(t *testing.T) {
	big := map[string]any{"nodeSelector": map[string]any{}}
	for i := 0; i < 17; i++ {
		big["nodeSelector"].(map[string]any)["k"+string(rune('a'+i))] = "v"
	}
	if _, ps := ParsePodOptions(DefaultPodSimple(), big); len(ps) == 0 {
		t.Error("17 paires nodeSelector acceptées (16 au plus)")
	}
	if _, ps := ParsePodOptions(DefaultPodSimple(), map[string]any{"nodeSelector": map[string]any{"Bad Key!": "v"}}); hasPath(ps, "$.nodeSelector.Bad Key!") == nil {
		t.Errorf("clé de label invalide : %+v", ps)
	}
	if _, ps := ParsePodOptions(DefaultPodSimple(), map[string]any{"affinity": map[string]any{"podAffinity": map[string]any{"preferredDuringSchedulingIgnoredDuringExecution": []any{map[string]any{"weight": 1, "podAffinityTerm": map[string]any{"topologyKey": "kubernetes.io/hostname", "namespaceSelector": map[string]any{}}}}}}}); len(ps) != 0 {
		t.Errorf("namespaceSelector (cluster de l'utilisateur) refusé : %+v", ps)
	}
}

// docker / podman : cinq options plates.
func TestE1ConteneurOptions(t *testing.T) {
	_, ps := ParseContainerOptions(ContainerRaw{Cpus: "64", Memory: "1024g", PidsLimit: "100000", ShmSize: "16g", Gpus: "all"})
	ok275(t, ps) // aucun plafond
	_, ps = ParseContainerOptions(ContainerRaw{Gpus: "2"})
	ok275(t, ps)
	for name, r := range map[string]ContainerRaw{
		"cpus 0": {Cpus: "0"}, "cpus négatif": {Cpus: "-1"}, "cpus texte": {Cpus: "deux"},
		"memory < 64m": {Memory: "32m"}, "memory unité": {Memory: "4 Go"}, "memory texte": {Memory: "x"},
		"pids texte": {PidsLimit: "beaucoup"}, "pids décimal": {PidsLimit: "1.5"}, "pids 0": {PidsLimit: "0"},
		"shm invalide": {ShmSize: "gros"}, "gpus invalide": {Gpus: "device=0"}, "gpus injection": {Gpus: "all --privileged"},
	} {
		if _, ps := ParseContainerOptions(r); len(ps) == 0 || ps[0].Field == "" || !strings.HasPrefix(ps[0].Field, "container.") {
			t.Errorf("%s : refus nommé « container.<champ> » attendu : %+v", name, ps)
		}
	}
}

func TestE1Defauts(t *testing.T) {
	d := DefaultPodSimple()
	if d.CPURequest != "100m" || d.MemRequest != "256Mi" || d.CPULimit != "2" || d.MemLimit != "4Gi" {
		t.Errorf("défauts k8s : %+v (requests 100m/256Mi, limits 2/4Gi)", d)
	}
	c := DefaultContainerRaw()
	if c.Cpus != "2" || c.Memory != "4g" || c.PidsLimit != "4096" || c.ShmSize != "256m" || c.Gpus != "" {
		t.Errorf("défauts docker : %+v (cpus 2, memory 4g, pids_limit 4096, shm_size 256m)", c)
	}
	if _, ps := ParsePodOptions(d, nil); len(ps) != 0 {
		t.Errorf("les défauts doivent être valides : %+v", ps)
	}
	if _, ps := ParseContainerOptions(c); len(ps) != 0 {
		t.Errorf("les défauts docker doivent être valides : %+v", ps)
	}
}

// Limite de projets : 30 par défaut (Helm users.maxProjects, modifiable) ; refus nommé.
func TestE1LimiteDeProjets(t *testing.T) {
	t.Setenv("CL_USERS_MAX_PROJECTS", "")
	if p := CheckProjectLimit(29); p != nil {
		t.Errorf("30e projet refusé : %+v", p)
	}
	p := CheckProjectLimit(30)
	if p == nil || p.Code != "limit" || !strings.Contains(p.Msg, "nombre maximal de projets atteint (30)") {
		t.Errorf("31e projet : %+v", p)
	}
	t.Setenv("CL_USERS_MAX_PROJECTS", "5")
	if CheckProjectLimit(4) != nil || CheckProjectLimit(5) == nil || !strings.Contains(CheckProjectLimit(5).Msg, "(5)") {
		t.Error("limite modifiable (5)")
	}
	t.Setenv("CL_USERS_MAX_PROJECTS", "n'importe quoi")
	if CheckProjectLimit(30) == nil {
		t.Error("valeur illisible : retour au défaut (30)")
	}
}
