package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Options par projet d'un backend d'UTILISATEUR (LOT-3.2 E1, contracts/user-backends.md §5 bis) : LISTE BLANCHE FERMÉE. Le launcher n'extrait
// que les champs listés et construit lui-même le manifeste ou l'argv ; aucun objet saisi n'est recopié. AUCUN plafond de CPU ni de
// mémoire (décision de l'utilisateur) : les quantités sont validées pour leur FORME, jamais bornées.

// OptionProblem : un refus nommé, avec le chemin JSON de l'élément fautif. Aucune valeur de l'utilisateur dans Msg.
type OptionProblem struct{ Field, Path, Code, Msg string }

func prob(field, path, code, msg string) OptionProblem {
	if len(path) > 120 {
		path = path[:120]
	}
	return OptionProblem{Field: field, Path: path, Code: code, Msg: msg}
}

// ── quantités k8s ─────────────────────────────────────────────────────────

var qtyRE = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)(n|u|m|k|M|G|T|P|E|Ki|Mi|Gi|Ti|Pi|Ei)?$`)

var qtyMul = map[string]float64{"": 1, "n": 1e-9, "u": 1e-6, "m": 1e-3, "k": 1e3, "M": 1e6, "G": 1e9, "T": 1e12, "P": 1e15, "E": 1e18,
	"Ki": 1 << 10, "Mi": 1 << 20, "Gi": 1 << 30, "Ti": 1 << 40, "Pi": 1 << 50, "Ei": 1 << 60}

// parseQty : quantité k8s (forme seulement : nombre, unité connue) ; faux pour toute autre chose (négatif, texte, espace).
func parseQty(s string) (float64, bool) {
	m := qtyRE.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	f, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	return f * qtyMul[m[2]], true
}

// PodSimple : champs simples d'un projet k8s (quantités k8s). Vide : non posé.
type PodSimple struct{ CPURequest, CPULimit, MemRequest, MemLimit, Ephemeral string }

// DefaultPodSimple : valeurs proposées (requests 100m / 256Mi, limits 2 / 4Gi).
func DefaultPodSimple() PodSimple {
	return PodSimple{CPURequest: "100m", CPULimit: "2", MemRequest: "256Mi", MemLimit: "4Gi"}
}

// PodOptions : options VALIDÉES d'un pod de projet (jamais un objet recopié du client).
type PodOptions struct {
	Requests, Limits map[string]string
	NodeSelector     map[string]string
	Tolerations      []map[string]any
	Affinity         map[string]any
}

// Resources : « resources » du conteneur construit à partir des champs validés.
func (po PodOptions) Resources() map[string]any {
	r := map[string]any{}
	if len(po.Requests) > 0 {
		r["requests"] = po.Requests
	}
	if len(po.Limits) > 0 {
		r["limits"] = po.Limits
	}
	return r
}

// Apply pose nodeSelector, tolerations et affinity dans le spec du pod m ; les ressources vont au conteneur (Resources).
func (po PodOptions) Apply(m map[string]any) {
	if len(po.NodeSelector) > 0 {
		m["nodeSelector"] = po.NodeSelector
	}
	if len(po.Tolerations) > 0 {
		m["tolerations"] = po.Tolerations
	}
	if len(po.Affinity) > 0 {
		m["affinity"] = po.Affinity
	}
}

// ParsePodOptions : champs simples + zone « Spécification du pod » (déjà décodée). Rend les options validées et les problèmes nommés.
func ParsePodOptions(simple PodSimple, spec map[string]any) (PodOptions, []OptionProblem) {
	po := PodOptions{Requests: map[string]string{}, Limits: map[string]string{}}
	var ps []OptionProblem
	type f struct {
		field, val string
		dst        map[string]string
		key        string
	}
	var vals = map[string]float64{}
	for _, x := range []f{
		{"pod.requests.cpu", simple.CPURequest, po.Requests, "cpu"}, {"pod.limits.cpu", simple.CPULimit, po.Limits, "cpu"},
		{"pod.requests.memory", simple.MemRequest, po.Requests, "memory"}, {"pod.limits.memory", simple.MemLimit, po.Limits, "memory"},
		{"pod.limits.ephemeral-storage", simple.Ephemeral, po.Limits, "ephemeral-storage"},
	} {
		if x.val == "" {
			continue
		}
		n, ok := parseQty(x.val)
		if !ok {
			ps = append(ps, prob(x.field, "", "invalid", "quantité invalide (nombre suivi d'une unité : 100m, 2, 256Mi, 4Gi)"))
			continue
		}
		x.dst[x.key] = x.val
		vals[x.field] = n
	}
	for _, r := range []string{"cpu", "memory"} {
		req, hasReq := vals["pod.requests."+r]
		lim, hasLim := vals["pod.limits."+r]
		if hasReq && hasLim && lim < req {
			ps = append(ps, prob("pod.limits."+r, "", "invalid", "la limite est inférieure à la demande"))
		}
	}
	zp := validatePodZone(spec, &po, false)
	return po, append(ps, zp...)
}

// ── zone JSON « Spécification du pod » ────────────────────────────────────

var (
	labelKeyRE = regexp.MustCompile(`^([a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?/)?[A-Za-z0-9]([-A-Za-z0-9_.]{0,61}[A-Za-z0-9])?$`)
	labelValRE = regexp.MustCompile(`^([A-Za-z0-9]([-A-Za-z0-9_.]{0,61}[A-Za-z0-9])?)?$`)
	extResRE   = regexp.MustCompile(`^([a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?/[A-Za-z0-9]([-A-Za-z0-9_.]{0,61}[A-Za-z0-9])?|hugepages-[0-9]+[KMGT]i)$`)
)

const forbiddenMsg = "champ non autorisé"

func validatePodZone(spec map[string]any, po *PodOptions, allowCPUMem bool) []OptionProblem {
	var ps []OptionProblem
	keys := make([]string, 0, len(spec))
	for k := range spec {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := spec[k]
		path := "$." + k
		switch k {
		case "resources":
			ps = append(ps, zoneResources(v, po, allowCPUMem)...)
		case "nodeSelector":
			ps = append(ps, zoneNodeSelector(v, po)...)
		case "tolerations":
			ps = append(ps, zoneTolerations(v, po)...)
		case "affinity":
			aff, aps := zoneAffinity(v)
			ps = append(ps, aps...)
			if len(aps) == 0 && po != nil {
				po.Affinity = aff
			}
		default:
			ps = append(ps, prob("pod.spec", path, "forbidden", forbiddenMsg))
		}
	}
	return ps
}

func zoneResources(v any, po *PodOptions, allowCPUMem bool) []OptionProblem {
	var ps []OptionProblem
	m, ok := v.(map[string]any)
	if !ok {
		return []OptionProblem{prob("pod.spec", "$.resources", "invalid", "un objet attendu")}
	}
	for side, sv := range m {
		path := "$.resources." + side
		if side == "claims" {
			ps = append(ps, prob("pod.spec", path, "forbidden", forbiddenMsg))
			continue
		}
		if side != "requests" && side != "limits" {
			ps = append(ps, prob("pod.spec", path, "forbidden", forbiddenMsg))
			continue
		}
		sm, ok := sv.(map[string]any)
		if !ok {
			ps = append(ps, prob("pod.spec", path, "invalid", "un objet attendu"))
			continue
		}
		for name, q := range sm {
			p := path + "." + name
			if name == "cpu" || name == "memory" || name == "ephemeral-storage" {
				if !allowCPUMem {
					ps = append(ps, prob("pod.spec", p, "forbidden", "utilise le champ CPU / mémoire (champs simples)"))
					continue
				}
			} else if !extResRE.MatchString(name) {
				ps = append(ps, prob("pod.spec", p, "invalid", "ressource étendue attendue (<domaine>/<nom> ou hugepages-<taille>)"))
				continue
			}
			qs := fmt.Sprint(q)
			if _, isStr := q.(string); !isStr {
				if f, isNum := q.(float64); isNum && f >= 0 {
					qs = strconv.FormatFloat(f, 'f', -1, 64)
				} else {
					ps = append(ps, prob("pod.spec", p, "invalid", "quantité invalide"))
					continue
				}
			}
			if _, ok := parseQty(qs); !ok {
				ps = append(ps, prob("pod.spec", p, "invalid", "quantité invalide"))
				continue
			}
			if po != nil {
				dst := po.Requests
				if side == "limits" {
					dst = po.Limits
				}
				dst[name] = qs
			}
		}
	}
	return ps
}

func zoneNodeSelector(v any, po *PodOptions) []OptionProblem {
	m, ok := v.(map[string]any)
	if !ok {
		return []OptionProblem{prob("pod.spec", "$.nodeSelector", "invalid", "un objet attendu")}
	}
	var ps []OptionProblem
	if len(m) > 16 {
		ps = append(ps, prob("pod.spec", "$.nodeSelector", "too_large", "16 paires au plus"))
	}
	out := map[string]string{}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		p := "$.nodeSelector." + k
		s, isStr := m[k].(string)
		switch {
		case !labelKeyRE.MatchString(k):
			ps = append(ps, prob("pod.spec", p, "invalid", "clé de label invalide"))
		case !isStr || !labelValRE.MatchString(s):
			ps = append(ps, prob("pod.spec", p, "invalid", "valeur de label invalide"))
		default:
			out[k] = s
		}
	}
	if po != nil && len(ps) == 0 {
		po.NodeSelector = out
	}
	return ps
}

func zoneTolerations(v any, po *PodOptions) []OptionProblem {
	l, ok := v.([]any)
	if !ok {
		return []OptionProblem{prob("pod.spec", "$.tolerations", "invalid", "une liste attendue")}
	}
	var ps []OptionProblem
	if len(l) > 16 {
		ps = append(ps, prob("pod.spec", "$.tolerations", "too_large", "16 tolérances au plus"))
	}
	var out []map[string]any
	for i, e := range l {
		base := fmt.Sprintf("$.tolerations[%d]", i)
		m, ok := e.(map[string]any)
		if !ok {
			ps = append(ps, prob("pod.spec", base, "invalid", "un objet attendu"))
			continue
		}
		t := map[string]any{}
		bad := false
		for k, val := range m {
			p := base + "." + k
			switch k {
			case "key":
				if s, ok := val.(string); ok && (s == "" || labelKeyRE.MatchString(s)) {
					t[k] = s
				} else {
					ps, bad = append(ps, prob("pod.spec", p, "invalid", "clé invalide")), true
				}
			case "operator":
				if s, ok := val.(string); ok && (s == "Equal" || s == "Exists") {
					t[k] = s
				} else {
					ps, bad = append(ps, prob("pod.spec", p, "invalid", "operator : Equal ou Exists")), true
				}
			case "value":
				if s, ok := val.(string); ok && labelValRE.MatchString(s) {
					t[k] = s
				} else {
					ps, bad = append(ps, prob("pod.spec", p, "invalid", "valeur invalide")), true
				}
			case "effect":
				if s, ok := val.(string); ok && (s == "NoSchedule" || s == "PreferNoSchedule" || s == "NoExecute") {
					t[k] = s
				} else {
					ps, bad = append(ps, prob("pod.spec", p, "invalid", "effect : NoSchedule, PreferNoSchedule ou NoExecute")), true
				}
			case "tolerationSeconds":
				if f, ok := val.(float64); ok && f >= 0 && f == float64(int64(f)) {
					t[k] = int64(f)
				} else {
					ps, bad = append(ps, prob("pod.spec", p, "invalid", "entier ≥ 0 attendu")), true
				}
			default:
				ps, bad = append(ps, prob("pod.spec", p, "forbidden", forbiddenMsg)), true
			}
		}
		if !bad {
			out = append(out, t)
		}
	}
	if po != nil && len(ps) == 0 {
		po.Tolerations = out
	}
	return ps
}

// schéma fermé de l'affinité (schéma k8s) : un nœud = objet à champs connus, liste d'éléments, ou feuille (scalaire, liste de scalaires,
// objet de scalaires).
type sch struct {
	fields map[string]*sch
	elem   *sch
	leaf   bool
}

var (
	schExpr = &sch{fields: map[string]*sch{"key": {leaf: true}, "operator": {leaf: true}, "values": {leaf: true}}}
	schTerm = &sch{fields: map[string]*sch{"matchExpressions": {elem: schExpr}, "matchFields": {elem: schExpr}}}
	schSel  = &sch{fields: map[string]*sch{"matchLabels": {leaf: true}, "matchExpressions": {elem: schExpr}}}
	schPAT  = &sch{fields: map[string]*sch{"labelSelector": schSel, "namespaceSelector": schSel, "namespaces": {leaf: true}, "topologyKey": {leaf: true},
		"matchLabelKeys": {leaf: true}, "mismatchLabelKeys": {leaf: true}}}
	schPod = &sch{fields: map[string]*sch{
		"requiredDuringSchedulingIgnoredDuringExecution":  {elem: schPAT},
		"preferredDuringSchedulingIgnoredDuringExecution": {elem: &sch{fields: map[string]*sch{"weight": {leaf: true}, "podAffinityTerm": schPAT}}},
	}}
	schAff = &sch{fields: map[string]*sch{
		"nodeAffinity": {fields: map[string]*sch{
			"requiredDuringSchedulingIgnoredDuringExecution":  {fields: map[string]*sch{"nodeSelectorTerms": {elem: schTerm}}},
			"preferredDuringSchedulingIgnoredDuringExecution": {elem: &sch{fields: map[string]*sch{"weight": {leaf: true}, "preference": schTerm}}},
		}},
		"podAffinity": schPod, "podAntiAffinity": schPod,
	}}
)

func isScalar(v any) bool {
	switch v.(type) {
	case string, float64, bool, int, int64:
		return true
	}
	return false
}

func checkLeaf(v any) bool {
	if isScalar(v) {
		return true
	}
	switch x := v.(type) {
	case []any:
		for _, e := range x {
			if !isScalar(e) {
				return false
			}
		}
		return true
	case map[string]any:
		for _, e := range x {
			if !isScalar(e) {
				return false
			}
		}
		return true
	}
	return false
}

func checkSch(path string, v any, n *sch, ps *[]OptionProblem) {
	switch {
	case n.leaf:
		if !checkLeaf(v) {
			*ps = append(*ps, prob("pod.spec", path, "invalid", "valeur invalide"))
		}
	case n.elem != nil:
		l, ok := v.([]any)
		if !ok {
			*ps = append(*ps, prob("pod.spec", path, "invalid", "une liste attendue"))
			return
		}
		if len(l) > 32 {
			*ps = append(*ps, prob("pod.spec", path, "too_large", "32 éléments au plus"))
			return
		}
		for i, e := range l {
			checkSch(fmt.Sprintf("%s[%d]", path, i), e, n.elem, ps)
		}
	default:
		m, ok := v.(map[string]any)
		if !ok {
			*ps = append(*ps, prob("pod.spec", path, "invalid", "un objet attendu"))
			return
		}
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			child, known := n.fields[k]
			if !known {
				*ps = append(*ps, prob("pod.spec", path+"."+k, "forbidden", forbiddenMsg))
				continue
			}
			checkSch(path+"."+k, m[k], child, ps)
		}
	}
}

func zoneAffinity(v any) (map[string]any, []OptionProblem) {
	var ps []OptionProblem
	checkSch("$.affinity", v, schAff, &ps)
	m, _ := v.(map[string]any)
	return m, ps
}

// ParsePodSpecJSON : la zone « Spécification du pod » lue depuis ses OCTETS (T8) : 16 Kio, profondeur 20, 256 nœuds, aucun NUL, aucune
// clé en double (casse et pliage Unicode compris), aucune valeur nulle ; puis la liste blanche (ParsePodOptions sans champs simples).
func ParsePodSpecJSON(raw []byte) (map[string]any, []OptionProblem) {
	bad := func(code, msg string) (map[string]any, []OptionProblem) {
		return nil, []OptionProblem{prob("pod.spec", "$", code, msg)}
	}
	if len(raw) > 16<<10 {
		return bad("too_large", "16 Kio au plus")
	}
	if bytes.IndexByte(raw, 0) >= 0 || !utf8.Valid(raw) {
		return bad("invalid", "caractère interdit")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	nodes := 0
	if err := scanJSON(dec, 0, &nodes); err != nil {
		return bad("invalid", err.Error())
	}
	if _, err := dec.Token(); err != io.EOF {
		return bad("invalid", "un seul objet JSON attendu")
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		return bad("invalid", "un objet JSON attendu")
	}
	var po PodOptions
	po.Requests, po.Limits = map[string]string{}, map[string]string{}
	return m, validatePodZone(m, &po, false)
}

func scanJSON(dec *json.Decoder, depth int, nodes *int) error {
	if depth > 20 {
		return fmt.Errorf("profondeur 20 au plus")
	}
	if *nodes++; *nodes > 256 {
		return fmt.Errorf("256 éléments au plus")
	}
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("JSON invalide")
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			seen := map[string]bool{}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return fmt.Errorf("JSON invalide")
				}
				k, _ := kt.(string)
				fk := strings.ToLower(strings.Map(foldRune, k))
				if seen[fk] {
					return fmt.Errorf("clé en double")
				}
				seen[fk] = true
				if err := scanJSON(dec, depth+1, nodes); err != nil {
					return err
				}
			}
			_, err = dec.Token()
		case '[':
			for dec.More() {
				if err := scanJSON(dec, depth+1, nodes); err != nil {
					return err
				}
			}
			_, err = dec.Token()
		}
		if err != nil {
			return fmt.Errorf("JSON invalide")
		}
	case nil:
		return fmt.Errorf("valeur nulle interdite")
	}
	return nil
}

// foldRune : pliage Unicode simple (« ſ » → « s », « K » kelvin → « k »).
func foldRune(r rune) rune {
	switch r {
	case 0x17F:
		return 's'
	case 0x212A:
		return 'k'
	}
	return r
}

// ── docker / podman ───────────────────────────────────────────────────────

// ContainerRaw : les cinq options plates saisies. Vide : non posée.
type ContainerRaw struct{ Cpus, Memory, PidsLimit, ShmSize, Gpus string }

// ContainerOptions : options VALIDÉES d'un conteneur de projet.
type ContainerOptions struct{ Cpus, Memory, PidsLimit, ShmSize, Gpus string }

// DefaultContainerRaw : valeurs proposées (cpus 2, memory 4g, pids_limit 4096, shm_size 256m).
func DefaultContainerRaw() ContainerRaw {
	return ContainerRaw{Cpus: "2", Memory: "4g", PidsLimit: "4096", ShmSize: "256m"}
}

// Args : options de « create » en --opt=valeur (jamais de second argument séparé).
func (c ContainerOptions) Args() []string {
	var a []string
	for _, kv := range [][2]string{{"--cpus=", c.Cpus}, {"--memory=", c.Memory}, {"--pids-limit=", c.PidsLimit}, {"--shm-size=", c.ShmSize}, {"--gpus=", c.Gpus}} {
		if kv[1] != "" {
			a = append(a, kv[0]+kv[1])
		}
	}
	return a
}

var (
	cpusRE  = regexp.MustCompile(`^[0-9]{1,4}(\.[0-9]{1,4})?$`)
	sizeRE  = regexp.MustCompile(`^[0-9]{1,15}[bkmg]$`)
	pidsRE  = regexp.MustCompile(`^(-1|[1-9][0-9]{0,6})$`)
	gpusRE  = regexp.MustCompile(`^(all|[1-9]|1[0-6])$`)
	sizeMul = map[byte]float64{'b': 1, 'k': 1 << 10, 'm': 1 << 20, 'g': 1 << 30}
)

func sizeBytes(s string) float64 {
	n, _ := strconv.ParseFloat(s[:len(s)-1], 64)
	return n * sizeMul[s[len(s)-1]]
}

// ParseContainerOptions : ParseContainerOptionsFor pour un backend docker.
func ParseContainerOptions(r ContainerRaw) (ContainerOptions, []OptionProblem) {
	return ParseContainerOptionsFor("docker", r)
}

// ParseContainerOptionsFor : valeurs strictes (forme exacte, jamais d'espace ni de second drapeau) ; podman : gpus refusé nommément.
func ParseContainerOptionsFor(backendType string, r ContainerRaw) (ContainerOptions, []OptionProblem) {
	var c ContainerOptions
	var ps []OptionProblem
	bad := func(f, msg string) { ps = append(ps, prob("container."+f, "", "invalid", msg)) }
	if r.Cpus != "" {
		if !cpusRE.MatchString(r.Cpus) {
			bad("cpus", "nombre décimal > 0 attendu (4 chiffres, 4 décimales au plus)")
		} else if f, _ := strconv.ParseFloat(r.Cpus, 64); f <= 0 {
			bad("cpus", "nombre décimal > 0 attendu")
		} else {
			c.Cpus = r.Cpus
		}
	}
	if r.Memory != "" {
		switch {
		case !sizeRE.MatchString(r.Memory):
			bad("memory", "quantité attendue (ex. 4g, 512m)")
		case sizeBytes(r.Memory) < 64<<20:
			bad("memory", "64m au moins")
		default:
			c.Memory = r.Memory
		}
	}
	if r.PidsLimit != "" {
		if n, err := strconv.Atoi(r.PidsLimit); !pidsRE.MatchString(r.PidsLimit) || err != nil || n > 4194304 {
			bad("pids_limit", "entier de 1 à 4194304, ou -1 (illimité)")
		} else {
			c.PidsLimit = r.PidsLimit
		}
	}
	if r.ShmSize != "" {
		if !sizeRE.MatchString(r.ShmSize) || sizeBytes(r.ShmSize) <= 0 {
			bad("shm_size", "quantité > 0 attendue (ex. 256m, 1g)")
		} else {
			c.ShmSize = r.ShmSize
		}
	}
	if r.Gpus != "" {
		switch {
		case backendType == "podman":
			ps = append(ps, prob("container.gpus", "", "forbidden", "gpus n'est pas disponible avec podman"))
		case !gpusRE.MatchString(r.Gpus):
			bad("gpus", "all ou un entier de 1 à 16")
		default:
			c.Gpus = r.Gpus
		}
	}
	return c, ps
}

// ParseContainerOptionsMap : options sous forme d'objet (« container: » du backend, client) — toute clé hors des cinq options est refusée.
func ParseContainerOptionsMap(backendType string, m map[string]any) (ContainerOptions, []OptionProblem) {
	var ps []OptionProblem
	var r ContainerRaw
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := fmt.Sprint(m[k])
		switch k {
		case "cpus":
			r.Cpus = v
		case "memory":
			r.Memory = v
		case "pids_limit":
			r.PidsLimit = v
		case "shm_size":
			r.ShmSize = v
		case "gpus":
			r.Gpus = v
		default:
			ps = append(ps, prob("container."+truncateKey(k), "", "forbidden", forbiddenMsg))
		}
	}
	c, more := ParseContainerOptionsFor(backendType, r)
	return c, append(ps, more...)
}

func truncateKey(k string) string {
	if len(k) > 40 {
		return k[:40]
	}
	return k
}

// ── limites, noms, identité, env.yaml ─────────────────────────────────────

// UsersMaxProjectsVar : users.maxProjects (chart) ; défaut 30, bornes 1 à 1000.
const UsersMaxProjectsVar = "CL_USERS_MAX_PROJECTS"

// UserMaxProjects : nombre maximal de projets d'un utilisateur, tous backends confondus.
func UserMaxProjects() int { return envInt(UsersMaxProjectsVar, 30, 1, 1000) }

// CheckProjectLimit : refus nommé quand l'utilisateur a déjà `existing` projets (la création du suivant dépasserait la limite).
func CheckProjectLimit(existing int) *OptionProblem {
	if max := UserMaxProjects(); existing >= max {
		p := prob("", "", "limit", fmt.Sprintf("nombre maximal de projets atteint (%d)", max))
		return &p
	}
	return nil
}

var (
	projNameRE = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	reservedRE = regexp.MustCompile(`-old-[0-9]+(-|$)`)
)

// ValidUserProjectName : « » si le nom est valide ; sinon la raison. DNS-1123, 63 au plus, sans le motif réservé « -old-<chiffres> »
// (collision avec les sauvegardes de recréation, T14).
func ValidUserProjectName(s string) string {
	switch {
	case !projNameRE.MatchString(s) || len(s) > 63:
		return "nom : minuscules, chiffres et tirets, 63 caractères au plus"
	case reservedRE.MatchString(s):
		return "nom : le motif -old-<chiffres> est réservé"
	}
	return ""
}

// RunAs : identité d'un projet (uid, gid non nuls).
type RunAs struct{ UID, GID int }

var idRE = regexp.MustCompile(`^[1-9][0-9]{0,9}$`)

// ParseRunAs : « Utilisateur du pod/conteneur » du projet — deux entiers de 1 à 4294967294 (root jamais).
func ParseRunAs(uid, gid string) (RunAs, []OptionProblem) {
	var ps []OptionProblem
	var r RunAs
	for _, x := range []struct {
		field, val string
		dst        *int
	}{{"pod.run_as.uid", uid, &r.UID}, {"pod.run_as.gid", gid, &r.GID}} {
		n, err := strconv.ParseInt(x.val, 10, 64)
		if !idRE.MatchString(x.val) || err != nil || !validID(n) {
			ps = append(ps, prob(x.field, "", "invalid", "entier de 1 à 4294967294 (jamais root)"))
			continue
		}
		*x.dst = int(n)
	}
	return r, ps
}

// ResolveRunAs : Helm → backend → projet, le plus précis l'emporte ; une valeur nulle ou invalide est ignorée (root jamais) ; défaut 2000:2000.
func ResolveRunAs(helm, backend, project *RunAs) RunAs {
	r := RunAs{UID: defaultRunAs, GID: defaultRunAs}
	for _, x := range []*RunAs{helm, backend, project} {
		if x != nil && validID(int64(x.UID)) && validID(int64(x.GID)) {
			r = *x
		}
	}
	return r
}

// CheckUserEnv : env.yaml d'un projet d'utilisateur — l'image est l'image par défaut du backend (LOT-3.3 ouvrira d'autres images) ;
// « mounts » et « local » refusés (aucun accès au système de fichiers du launcher, T5).
func CheckUserEnv(env EnvSpec, defaultImage string) []OptionProblem {
	var ps []OptionProblem
	if env.Image != "" {
		if err := CheckUserImage(env.Image); err != nil {
			ps = append(ps, prob("env.image", "", "invalid", err.Error()))
		} else if env.Image != defaultImage {
			ps = append(ps, prob("env.image", "", "forbidden", "image non autorisée : seule l'image par défaut est admise"))
		}
	}
	if len(env.Mounts) > 0 {
		ps = append(ps, prob("env.mounts", "", "forbidden", "« mounts » n'est pas admis sur un backend d'utilisateur"))
	}
	if len(env.Local) > 0 {
		ps = append(ps, prob("env.local", "", "forbidden", "« local » n'est pas admis sur un backend d'utilisateur"))
	}
	return ps
}
