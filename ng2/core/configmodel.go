package core

import (
	"bytes"
	"fmt"
	"runtime"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ── Écriture structurée de backends.yaml (#118, LOT-3.1) ──────────────────
//
// L'interface de configuration lit le fichier en MODÈLE (ReadBackendsModel) et
// l'écrit par une liste de CHANGEMENTS (ApplyConfigChanges) appliqués au
// texte par yaml.v3 Node : les commentaires et l'ordre des clés sont conservés.
// Le texte obtenu passe ensuite par ValidateConfig puis SaveConfig, inchangés
// (mêmes règles que le chargement réel, copie .bak, contrôle de conflit).

// BackendEntry : un backend tel que l'interface le montre et le renvoie.
type BackendEntry struct {
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Context    string            `json:"context,omitempty"`
	Host       string            `json:"host,omitempty"`
	Hosts      []string          `json:"hosts,omitempty"`
	Pool       string            `json:"pool,omitempty"`
	Engine     string            `json:"engine,omitempty"`
	EngineHost string            `json:"engine_host,omitempty"`
	Shared     []string          `json:"shared,omitempty"`
	Namespace  string            `json:"namespace,omitempty"`
	Volumes    map[string]string `json:"volumes,omitempty"`
	Spread     *bool             `json:"spread,omitempty"`
	Pod        map[string]any    `json:"pod,omitempty"`
	Implicit   bool              `json:"implicit,omitempty"` // host ajouté sans déclaration (Windows)
}

// PoolEntry : un pool de machines nommé ; UsedBy : backends qui le citent.
type PoolEntry struct {
	Name   string   `json:"name"`
	Hosts  []string `json:"hosts"`
	Drain  []string `json:"drain,omitempty"`
	UsedBy []string `json:"usedBy"`
}

// DefaultsEntry : section « defaults ».
type DefaultsEntry struct {
	Backend       string         `json:"backend,omitempty"`
	Image         string         `json:"image,omitempty"`
	BrowserPaths  *[]BrowserPath `json:"browser_paths,omitempty"`
	BrowserServer *BrowserServer `json:"browser_server,omitempty"`
}

// BackendsModel : backends.yaml lu en modèle. Les backends et les pools sont
// dans l'ordre du fichier.
type BackendsModel struct {
	Backends []BackendEntry `json:"backends"`
	Pools    []PoolEntry    `json:"pools"`
	Defaults DefaultsEntry  `json:"defaults"`
	Images   []string       `json:"images"`
}

// ReadBackendsModel : modèle du contenu de backends.yaml (même décodage que le
// chargement : mêmes types, mêmes erreurs). Contenu vide : modèle vide.
func ReadBackendsModel(content []byte) (BackendsModel, error) {
	m := BackendsModel{Backends: []BackendEntry{}, Pools: []PoolEntry{}, Images: []string{}}
	var f backendsFile
	if err := yaml.Unmarshal(content, &f); err != nil {
		return m, fmt.Errorf("backends.yaml : %w", err)
	}
	var doc yaml.Node
	_ = yaml.Unmarshal(content, &doc)
	order := func(section string) []string {
		var names []string
		if root := docRoot(&doc); root != nil {
			if i := keyIndex(root, section); i >= 0 && root.Content[i+1].Kind == yaml.MappingNode {
				for j := 0; j < len(root.Content[i+1].Content); j += 2 {
					names = append(names, root.Content[i+1].Content[j].Value)
				}
			}
		}
		return names
	}
	for _, n := range order("backends") {
		c := f.Backends[n]
		m.Backends = append(m.Backends, BackendEntry{Name: n, Type: c.Type, Context: c.Context, Host: c.Host, Hosts: c.Hosts,
			Pool: c.Pool, Engine: c.Engine, EngineHost: c.EngHost, Shared: c.Shared, Namespace: c.Namespace,
			Volumes: c.Volumes, Spread: c.Spread, Pod: c.Pod})
	}
	if _, declared := f.Backends["host"]; !declared && runtime.GOOS == "windows" {
		m.Backends = append([]BackendEntry{{Name: "host", Type: "host", Implicit: true}}, m.Backends...)
	}
	for _, n := range order("pools") {
		p := f.Pools[n]
		e := PoolEntry{Name: n, Hosts: p.Hosts, Drain: p.Drain, UsedBy: []string{}}
		for _, b := range m.Backends {
			if b.Pool == n {
				e.UsedBy = append(e.UsedBy, b.Name)
			}
		}
		m.Pools = append(m.Pools, e)
	}
	m.Defaults = DefaultsEntry{Backend: f.Defaults.Backend, Image: f.Defaults.Image,
		BrowserPaths: f.Defaults.BrowserPaths, BrowserServer: f.Defaults.BrowserServer}
	if f.Images != nil {
		m.Images = f.Images
	}
	return m, nil
}

// ConfigChange : un changement du fichier. Op : « set » (crée ou remplace la
// valeur au chemin At ; un objet est fusionné clé à clé, les clés absentes sont
// retirées), « delete » (retire la clé), « rename » (Value : nouveau nom de la
// dernière clé de At).
type ConfigChange struct {
	Op    string   `json:"op"`
	At    []string `json:"at"`
	Value any      `json:"value,omitempty"`
}

// backendKeyOrder : ordre d'écriture des clés d'un backend ajouté.
var backendKeyOrder = []string{"type", "context", "host", "hosts", "pool", "engine", "engine_host", "shared",
	"namespace", "volumes", "spread", "pod"}

// ApplyConfigChanges applique les changements au texte de backends.yaml :
// commentaires, ordre et clés non touchées conservés. Le texte rendu n'est pas
// validé ici (ValidateConfig puis SaveConfig s'en chargent).
func ApplyConfigChanges(kind ConfigKind, content []byte, changes []ConfigChange) ([]byte, error) {
	if kind != ConfBackends && kind != ConfProjectEnv && kind != ConfLauncherEnv {
		return nil, fmt.Errorf("écriture structurée non prévue pour %s", kind)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return nil, fmt.Errorf("%s : %w", kind, err)
	}
	if doc.Kind == 0 { // fichier vide ou que des commentaires
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
		if len(bytes.TrimSpace(content)) > 0 {
			doc.HeadComment = strings.TrimRight(string(content), "\n")
		}
	}
	root := docRoot(&doc)
	if root == nil {
		return nil, fmt.Errorf("%s : la racine doit être un objet", kind)
	}
	for i, ch := range changes {
		if len(ch.At) == 0 {
			return nil, fmt.Errorf("changement %d : chemin vide", i+1)
		}
		var err error
		switch ch.Op {
		case "set":
			err = setAt(root, ch.At, ch.Value)
		case "delete":
			err = deleteAt(root, ch.At)
		case "rename":
			to, _ := ch.Value.(string)
			err = renameAt(root, ch.At, to)
		default:
			err = fmt.Errorf("opération %q inconnue (set, delete, rename)", ch.Op)
		}
		if err != nil {
			return nil, fmt.Errorf("changement %d (%s %s) : %w", i+1, ch.Op, strings.Join(ch.At, "."), err)
		}
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// docRoot : objet racine du document (nil s'il n'en est pas un).
func docRoot(doc *yaml.Node) *yaml.Node {
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	return doc.Content[0]
}

func keyIndex(m *yaml.Node, key string) int {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return i
		}
	}
	return -1
}

// parentOf : objet qui porte la dernière clé de at ; create : crée les objets
// intermédiaires manquants.
func parentOf(root *yaml.Node, at []string, create bool) (*yaml.Node, error) {
	cur := root
	for _, k := range at[:len(at)-1] {
		i := keyIndex(cur, k)
		if i < 0 {
			if !create {
				return nil, fmt.Errorf("%q introuvable", k)
			}
			cur.Content = append(cur.Content, scalarKey(k), &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"})
			i = len(cur.Content) - 2
		}
		next := cur.Content[i+1]
		if next.Kind == yaml.ScalarNode && next.Tag == "!!null" && create { // « pools: » vide
			*next = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", HeadComment: next.HeadComment, LineComment: next.LineComment}
		}
		if next.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("%q n'est pas un objet", k)
		}
		cur = next
	}
	return cur, nil
}

func scalarKey(k string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k}
}

func setAt(root *yaml.Node, at []string, value any) error {
	parent, err := parentOf(root, at, true)
	if err != nil {
		return err
	}
	key := at[len(at)-1]
	nv := toNode(value, at)
	if i := keyIndex(parent, key); i >= 0 {
		mergeNode(parent.Content[i+1], nv, unknownKeyKeeper(at))
		return nil
	}
	parent.Content = append(parent.Content, scalarKey(key), nv)
	return nil
}

func deleteAt(root *yaml.Node, at []string) error {
	parent, err := parentOf(root, at, false)
	if err != nil {
		return err
	}
	i := keyIndex(parent, at[len(at)-1])
	if i < 0 {
		return fmt.Errorf("%q introuvable", at[len(at)-1])
	}
	parent.Content = append(parent.Content[:i], parent.Content[i+2:]...)
	if len(parent.Content) == 0 && parent != root {
		parent.Style = yaml.FlowStyle // « pools: {} » plutôt qu'une clé sans valeur
	}
	return nil
}

func renameAt(root *yaml.Node, at []string, to string) error {
	if strings.TrimSpace(to) == "" {
		return fmt.Errorf("nouveau nom vide")
	}
	parent, err := parentOf(root, at, false)
	if err != nil {
		return err
	}
	i := keyIndex(parent, at[len(at)-1])
	if i < 0 {
		return fmt.Errorf("%q introuvable", at[len(at)-1])
	}
	if keyIndex(parent, to) >= 0 {
		return fmt.Errorf("%q existe déjà", to)
	}
	parent.Content[i].Value = to
	return nil
}

// toNode : valeur JSON → nœud YAML. Les objets sont écrits dans un ordre
// stable : backendKeyOrder pour un backend, sinon l'ordre alphabétique ; les
// valeurs vides (chaîne, liste, objet, null) sont omises.
func toNode(v any, at []string) *yaml.Node {
	switch x := v.(type) {
	case map[string]any:
		n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		keys := make([]string, 0, len(x))
		for k, e := range x {
			if !emptyValue(e) {
				keys = append(keys, k)
			}
		}
		rank := func(k string) int {
			if len(at) == 2 && at[0] == "backends" {
				for i, o := range backendKeyOrder {
					if o == k {
						return i
					}
				}
			}
			return 1 << 20
		}
		sort.Slice(keys, func(i, j int) bool {
			if ri, rj := rank(keys[i]), rank(keys[j]); ri != rj {
				return ri < rj
			}
			return keys[i] < keys[j]
		})
		for _, k := range keys {
			n.Content = append(n.Content, scalarKey(k), toNode(x[k], append(append([]string{}, at...), k)))
		}
		return n
	case []any:
		n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, e := range x {
			n.Content = append(n.Content, toNode(e, at))
		}
		if len(x) > 0 && allScalars(n) {
			n.Style = yaml.FlowStyle // « hosts: [srv1, srv2] » comme les exemples
		}
		return n
	case []string:
		a := make([]any, len(x))
		for i, s := range x {
			a[i] = s
		}
		return toNode(a, at)
	}
	var n yaml.Node
	if err := n.Encode(v); err != nil {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: fmt.Sprint(v)}
	}
	return &n
}

func allScalars(n *yaml.Node) bool {
	for _, c := range n.Content {
		if c.Kind != yaml.ScalarNode {
			return false
		}
	}
	return true
}

func emptyValue(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case []any:
		return len(x) == 0
	case []string:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}

// unknownKeyKeeper : pour le « set » d'un backend (backends.<nom>), les clés que
// le formulaire ne connaît pas (hors backendKeyOrder) sont conservées : une clé
// ajoutée à la main ou par une version plus récente ne disparaît pas à
// l'enregistrement. Les clés connues absentes de la valeur sont retirées.
func unknownKeyKeeper(at []string) func(string) bool {
	if len(at) != 2 || at[0] != "backends" {
		return nil
	}
	return func(k string) bool {
		for _, o := range backendKeyOrder {
			if o == k {
				return false
			}
		}
		return true
	}
}

// mergeNode remplace old par nv en gardant les commentaires de old ; deux
// objets sont fusionnés clé à clé (commentaires des clés conservées gardés, clés
// absentes de nv retirées).
func mergeNode(old, nv *yaml.Node, keep func(string) bool) {
	if old.Kind == yaml.MappingNode && nv.Kind == yaml.MappingNode {
		var content []*yaml.Node
		for i := 0; i+1 < len(nv.Content); i += 2 {
			k, v := nv.Content[i], nv.Content[i+1]
			if j := keyIndex(old, k.Value); j >= 0 {
				mergeNode(old.Content[j+1], v, nil)
				content = append(content, old.Content[j], old.Content[j+1])
			} else {
				content = append(content, k, v)
			}
		}
		// l'ordre du fichier pour les clés conservées
		kept := map[string]bool{}
		for i := 0; i+1 < len(content); i += 2 {
			kept[content[i].Value] = true
		}
		for i := 0; keep != nil && i+1 < len(old.Content); i += 2 {
			if k := old.Content[i].Value; keep(k) {
				kept[k] = true // clé que l'appelant ne connaît pas : conservée
			}
		}
		var ordered, added []*yaml.Node
		for i := 0; i+1 < len(old.Content); i += 2 {
			if kept[old.Content[i].Value] {
				ordered = append(ordered, old.Content[i], old.Content[i+1])
			}
		}
		have := map[string]bool{}
		for i := 0; i+1 < len(ordered); i += 2 {
			have[ordered[i].Value] = true
		}
		for i := 0; i+1 < len(content); i += 2 {
			if !have[content[i].Value] {
				added = append(added, content[i], content[i+1])
			}
		}
		old.Content = append(ordered, added...)
		if len(old.Content) == 0 {
			old.Style = yaml.FlowStyle
		}
		return
	}
	if old.Kind == yaml.SequenceNode && nv.Kind == yaml.SequenceNode && allScalars(old) && allScalars(nv) {
		// liste de valeurs simples : les éléments conservés gardent leur nœud, donc
		// leurs commentaires (« - ripgrep   # recherche ») ; le style de la liste aussi
		prev := map[string]*yaml.Node{}
		for _, c := range old.Content {
			prev[c.Value] = c
		}
		content := make([]*yaml.Node, len(nv.Content))
		for i, c := range nv.Content {
			if o, ok := prev[c.Value]; ok && o.Tag == c.Tag {
				content[i] = o
			} else {
				content[i] = c
			}
		}
		old.Content = content
		if len(content) == 0 {
			old.Style = yaml.FlowStyle
		}
		return
	}
	head, line, foot := old.HeadComment, old.LineComment, old.FootComment
	style := old.Style
	*old = *nv
	if old.Kind == yaml.SequenceNode && style != 0 {
		old.Style = style // la liste garde son écriture (en ligne ou en bloc) quand le fichier l'avait
	}
	old.HeadComment, old.LineComment, old.FootComment = head, line, foot
}
