package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Espace par utilisateur (contracts/user-backends.md §2, #255). Modèle sans agent : le launcher ne garde par utilisateur
// que ses backends et identifiants, ses réglages et une trace minimale des projets vus ; JAMAIS de donnée projet.
//
//	<config>/users/<uid>/        0700 ; <uid> = « github-<id numérique> »
//	├── backends.yaml            backends de l'utilisateur
//	├── vault/                   identifiants (en clair 0600 : décision P14, dette documentée)
//	├── config.json              réglages globaux (affichage, CLAUDE_OPTIONS, choix du jeton GitHub)
//	└── traces.json              projets vus (§5) : seul résidu lié aux projets
//
// L'espace de l'admin est l'espace actuel (config, backends.yaml, coffre de la racine) : aucune migration.

// AdminUID : nom de l'espace de l'admin (l'espace actuel) ; compte local admin@local compris.
const AdminUID = "admin"

var uidRE = regexp.MustCompile(`^github-[1-9][0-9]{0,18}$`)

// Space : l'espace d'une identité.
type Space struct {
	UID   string
	Admin bool
}

// AdminSpace : l'espace actuel (racine), celui de l'administrateur.
func AdminSpace() Space { return Space{UID: AdminUID, Admin: true} }

// UserSpace : l'espace du compte uid (« github-<id> ») ; tout autre nom est refusé (jamais de « .. » ni de séparateur).
func UserSpace(uid string) (Space, error) {
	if !uidRE.MatchString(uid) {
		return Space{}, fmt.Errorf("espace invalide : %q", uid)
	}
	return Space{UID: uid}, nil
}

// SpaceOf : espace d'une identité du mode proxy. Un administrateur (compte local compris) a l'espace actuel ; un utilisateur
// a le sien, d'après l'identifiant numérique de son login (ResolveUID). Erreur : ErrLoginReused, ErrLoginUnresolved.
func SpaceOf(id Identity) (Space, error) {
	if id.Role == RoleAdmin || !UsersEnabled() { // users.enabled: false : espace commun, comme en v7.0.0
		return AdminSpace(), nil
	}
	uid, err := ResolveUID(id.Login)
	if err != nil {
		return Space{}, err
	}
	return UserSpace(uid)
}

// Dir : dossier de l'espace (admin : la config actuelle).
func (s Space) Dir() string {
	if s.Admin {
		return Ng2ConfigDir()
	}
	return filepath.Join(UsersDir(), s.UID)
}

// BackendsPath : backends.yaml de l'espace.
func (s Space) BackendsPath() string {
	if s.Admin {
		return BackendsPath()
	}
	return filepath.Join(s.Dir(), "backends.yaml")
}

// VaultDir : coffre de l'espace (admin : le coffre de la racine).
func (s Space) VaultDir() string {
	if s.Admin {
		return secretsDirRaw()
	}
	return filepath.Join(s.Dir(), "vault")
}

// SettingsPath : config.json de l'espace d'un utilisateur ; vide pour l'admin (config.conf et préférences actuelles).
func (s Space) SettingsPath() string {
	if s.Admin {
		return ""
	}
	return filepath.Join(s.Dir(), "config.json")
}

// TracesPath : traces.json de l'espace d'un utilisateur ; vide pour l'admin (ses projets viennent de la racine).
func (s Space) TracesPath() string {
	if s.Admin {
		return ""
	}
	return filepath.Join(s.Dir(), "traces.json")
}

// Ensure crée l'espace d'un utilisateur (0700) et son coffre ; sans effet pour l'admin.
func (s Space) Ensure() error {
	if s.Admin {
		return nil
	}
	for _, d := range []string{UsersDir(), s.Dir(), s.VaultDir()} {
		if err := guardWrite(d); err != nil {
			return err
		}
		if err := os.MkdirAll(d, 0o700); err != nil {
			return fmt.Errorf("espace %s : %w", s.UID, err)
		}
		_ = os.Chmod(d, 0o700)
	}
	return nil
}

// LoadBackends : backends de l'espace, triés. Admin : LoadBackends actuel. Utilisateur : seulement ce que son
// backends.yaml déclare — ni le « host » implicite, ni les moteurs locaux détectés (calcul sur la plateforme : règle n°2).
func (s Space) LoadBackends() ([]BackendConf, error) {
	if s.Admin {
		return LoadBackends()
	}
	b, err := os.ReadFile(s.BackendsPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	bs, err := loadBackendsData(b, err)
	if err != nil {
		return nil, err
	}
	out := bs[:0:0]
	for _, c := range bs {
		if c.Type == "host" {
			continue
		}
		if err := CheckUserBackendName(c.Name); err != nil { // échec fermé : le fichier entier est refusé
			return nil, err
		}
		if err := userFieldsOK(c); err != nil { // défense en profondeur : le fichier a pu être posé hors de l'API (S14, S29)
			return nil, err
		}
		c.user, c.vault = true, s.VaultDir() // S15 / S16 : son coffre, jamais celui de l'admin ni le ServiceAccount
		out = append(out, c)
	}
	return out, nil
}

// WriteJSONAtomic : v en JSON dans path, sous verrou, écriture atomique 0600.
func writeSpaceJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return WithFileLock(path, func() error { return WriteFileAtomic(path, append(b, '\n'), 0o600) })
}

// ── réglages globaux (config.json) ────────────────────────────────────────

// GithubTokenPAT, GithubTokenOAuth : choix du jeton GitHub de l'utilisateur (§8, tâche LOT-3.2).
const (
	GithubTokenPAT   = "pat"
	GithubTokenOAuth = "oauth"
)

// maxSettingsBytes : taille d'un config.json lu ou écrit (jamais de donnée projet : des réglages).
const maxSettingsBytes = 64 << 10

// UserSettings : réglages globaux de l'utilisateur (config.json). Aucun secret : les jetons vont dans le coffre.
type UserSettings struct {
	Display       map[string]any `json:"display,omitempty"`        // préférences d'affichage (thème, tailles…)
	ClaudeOptions string         `json:"claude_options,omitempty"` // CLAUDE_OPTIONS
	GithubToken   string         `json:"github_token,omitempty"`   // « pat » | « oauth » : choix de la source du jeton
}

// LoadSettings : config.json de l'utilisateur (vide sans fichier). Admin : réglages vides (il garde config.conf).
func (s Space) LoadSettings() (UserSettings, error) {
	var u UserSettings
	p := s.SettingsPath()
	if p == "" {
		return u, nil
	}
	b, err := readFileRetry(p)
	if errors.Is(err, os.ErrNotExist) {
		return u, nil
	}
	if err != nil {
		return u, err
	}
	if len(b) > maxSettingsBytes {
		return u, fmt.Errorf("config.json trop gros (%d octets)", len(b))
	}
	if err := json.Unmarshal(b, &u); err != nil {
		return UserSettings{}, fmt.Errorf("config.json : %w", err)
	}
	return u, nil
}

// SaveSettings : écrit config.json (atomique, verrou, 0600). Refusé pour l'admin.
func (s Space) SaveSettings(u UserSettings) error {
	p := s.SettingsPath()
	if p == "" {
		return errors.New("réglages de l'admin : config.conf")
	}
	if u.GithubToken != "" && u.GithubToken != GithubTokenPAT && u.GithubToken != GithubTokenOAuth {
		return fmt.Errorf("choix du jeton GitHub invalide : %q", u.GithubToken)
	}
	if b, err := json.Marshal(u); err != nil || len(b) > maxSettingsBytes {
		return errors.New("réglages trop gros")
	}
	if err := s.Ensure(); err != nil {
		return err
	}
	return writeSpaceJSON(p, u)
}

// ── trace des projets vus (traces.json) ───────────────────────────────────

// Trace : un projet vu à la dernière découverte réussie. RIEN d'autre (contrat §2) : ni branche, ni état git, ni
// chemin de fichier, ni dépôt.
type Trace struct {
	Backend   string    `json:"backend"`
	Kind      string    `json:"kind"`     // k8s | docker | podman
	Object    string    `json:"object"`   // nom du StatefulSet ou du conteneur
	Project   string    `json:"project"`  // cl.project
	Category  string    `json:"category"` // cl.category
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
}

type tracesFile struct {
	Traces []Trace `json:"traces"`
}

func (s Space) loadTraces() (tracesFile, error) {
	var f tracesFile
	p := s.TracesPath()
	if p == "" {
		return f, nil
	}
	b, err := readFileRetry(p)
	if errors.Is(err, os.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return f, err
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return tracesFile{}, fmt.Errorf("traces.json : %w", err)
	}
	return f, nil
}

// LoadTraces : traces de l'espace, triées (backend, catégorie, projet). Admin : aucune.
func (s Space) LoadTraces() ([]Trace, error) {
	f, err := s.loadTraces()
	sort.Slice(f.Traces, func(i, j int) bool {
		a, b := f.Traces[i], f.Traces[j]
		if a.Backend != b.Backend {
			return a.Backend < b.Backend
		}
		if a.Category != b.Category {
			return a.Category < b.Category
		}
		return a.Project < b.Project
	})
	return f.Traces, err
}

func sameTrace(a, b Trace) bool {
	return a.Backend == b.Backend && a.Project == b.Project && a.Category == b.Category
}

// RecordTrace : trace d'un projet vu (création ou mise à jour de last_seen). Les champs sont réduits au schéma du contrat.
func (s Space) RecordTrace(t Trace) error {
	p := s.TracesPath()
	if p == "" {
		return errors.New("traces : espace d'un utilisateur seulement")
	}
	if err := s.Ensure(); err != nil {
		return err
	}
	now := time.Now().UTC()
	return WithFileLock(p, func() error {
		f, err := s.loadTraces()
		if err != nil {
			return err
		}
		t.LastSeen = now
		for i, o := range f.Traces {
			if sameTrace(o, t) {
				t.FirstSeen = o.FirstSeen
				f.Traces[i] = t
				return s.writeTraces(p, f)
			}
		}
		t.FirstSeen = now
		f.Traces = append(f.Traces, t)
		return s.writeTraces(p, f)
	})
}

func (s Space) writeTraces(p string, f tracesFile) error {
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(p, append(b, '\n'), 0o600)
}

// PurgeTrace : efface la trace d'un projet (action « Purger »). Aucune commande vers le backend, rien de supprimé chez
// l'utilisateur. Rend le nombre de traces effacées.
func (s Space) PurgeTrace(backend, project string) (int, error) {
	return s.purge(func(t Trace) bool { return t.Backend == backend && t.Project == project })
}

// PurgeBackendTraces : efface les traces d'un backend retiré de la déclaration (après confirmation de l'utilisateur).
func (s Space) PurgeBackendTraces(backend string) (int, error) {
	return s.purge(func(t Trace) bool { return t.Backend == backend })
}

func (s Space) purge(match func(Trace) bool) (int, error) {
	p := s.TracesPath()
	if p == "" {
		return 0, errors.New("traces : espace d'un utilisateur seulement")
	}
	n := 0
	err := WithFileLock(p, func() error {
		f, err := s.loadTraces()
		if err != nil {
			return err
		}
		keep := f.Traces[:0:0]
		for _, t := range f.Traces {
			if match(t) {
				n++
				continue
			}
			keep = append(keep, t)
		}
		if n == 0 {
			return nil
		}
		f.Traces = keep
		return s.writeTraces(p, f)
	})
	return n, err
}

// ── liste des utilisateurs (admin) ────────────────────────────────────────

// UserSummary : une ligne de userList — jamais d'identifiant numérique ni de contenu d'espace.
type UserSummary struct {
	Login    string `json:"login"`
	Backends int    `json:"backends"` // nombre de backends déclarés
	State    string `json:"state"`    // « ok » | « sans backend » | « illisible »
}

// ListUsers : les utilisateurs connus de users/index.json et leur état (pour la méthode admin userList).
func ListUsers() []UserSummary {
	out := []UserSummary{}
	for login, uid := range UserIndexLogins() {
		u := UserSummary{Login: login, State: "sans backend"}
		if sp, err := UserSpace(uid); err == nil {
			switch bs, err := sp.LoadBackends(); {
			case err != nil:
				u.State = "illisible"
			case len(bs) > 0:
				u.Backends, u.State = len(bs), "ok"
			}
		}
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return strings.Compare(out[i].Login, out[j].Login) < 0 })
	return out
}

// ── édition de backends.yaml de l'utilisateur ─────────────────────────────

// maxBackendsFileBytes : taille de backends.yaml d'un utilisateur (jamais d'identifiant dedans : coffre à part).
const maxBackendsFileBytes = 256 << 10

// BackendsFile : fichier de configuration backends.yaml de l'espace (modèle par défaut s'il est absent).
func (s Space) BackendsFile() ConfigFile {
	return configFile(ConfBackends, "backends.yaml — vos backends", s.BackendsPath())
}

// ValidateBackends : problèmes du contenu proposé pour backends.yaml (mêmes règles de forme que l'éditeur actuel, plus celles d'un
// backend d'utilisateur : userBackendProblems).
func (s Space) ValidateBackends(content []byte) []ConfigProblem {
	if len(content) > maxBackendsFileBytes {
		return []ConfigProblem{{Severity: SevError, Message: "backends.yaml trop gros"}}
	}
	ps := ValidateConfig(ConfBackends, content)
	return ScrubProblems(append(ps, userBackendProblems(content)...)) // jamais une valeur de l'utilisateur dans un message (S25)
}

// UserSSHRefused : le contenu déclare un backend ssh (type, host ou engine_host en ssh://) — refusé en mode serveur (P13).
func UserSSHRefused(content []byte) bool {
	var f struct {
		Backends map[string]map[string]any `yaml:"backends"`
	}
	if yaml.Unmarshal(content, &f) != nil {
		return false
	}
	for _, be := range f.Backends {
		if t, _ := be["type"].(string); t == "ssh" {
			return true
		}
		for _, k := range []string{"host", "engine_host"} {
			if h, _ := be[k].(string); strings.HasPrefix(strings.ToLower(strings.TrimSpace(h)), "ssh:") {
				return true
			}
		}
	}
	return false
}

// Variables d'environnement posées par le chart (users.maxBackends, users.backendTestsPerMinute).
const (
	UsersMaxBackendsVar = "CL_USERS_MAX_BACKENDS"
	UsersTestsPerMinVar = "CL_USERS_BACKEND_TESTS_PER_MINUTE"
)

func envInt(name string, def, lo, hi int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name))); err == nil && n >= lo && n <= hi {
		return n
	}
	return def
}

// UsersEnabledVar : « false » rend le comportement v7.0.0 (un espace commun : tout compte authentifié sert l'App de l'admin, avec ses
// règles R1) ; posée par le chart (users.enabled).
const UsersEnabledVar = "CL_USERS_ENABLED"

// UsersEnabled : le routage par utilisateur est actif (défaut : oui).
func UsersEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(UsersEnabledVar))) {
	case "false", "0", "no", "non":
		return false
	}
	return true
}

// UserMaxBackends : nombre maximal de backends d'un utilisateur (défaut 10, 1 à 100).
func UserMaxBackends() int { return envInt(UsersMaxBackendsVar, 10, 1, 100) }

// UserTestsPerMinute : nombre maximal de backendTest par minute et par utilisateur (défaut 6, 1 à 60).
func UserTestsPerMinute() int { return envInt(UsersTestsPerMinVar, 6, 1, 60) }

// volumeNameRE : un NOM de volume seulement (jamais un chemin ni une liste d'options) ; dnsLabelRE : namespace, PVC.
var (
	volumeNameRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$`)
	dnsLabelRE   = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)
)

// RootOnHostWarning : avertissement d'un accès docker sans TLS vérifié.
const RootOnHostWarning = "Accès docker sans TLS vérifié : quiconque atteint ce port est root sur l'hôte. À réserver à un réseau de confiance."

// userBackendProblems : règles propres aux backends d'un utilisateur — nom DNS-1123, ssh refusé (P13), kubeconfig « true » obligatoire
// pour un k8s (S15), hôte borné (S1–S7, D1), namespace / PVC / volume = noms (S14, S29), limite de backends (D5).
func userBackendProblems(content []byte) []ConfigProblem {
	var f struct {
		Backends map[string]map[string]any `yaml:"backends"`
	}
	if yaml.Unmarshal(content, &f) != nil {
		return nil // déjà signalé par ValidateConfig
	}
	names := make([]string, 0, len(f.Backends))
	for n := range f.Backends {
		names = append(names, n)
	}
	sort.Strings(names)
	var ps []ConfigProblem
	if yamlDupKeyCI(content) {
		ps = append(ps, ConfigProblem{Severity: SevError, Message: "clé en double dans backends.yaml (la casse ne distingue pas deux clés)"})
	}
	if max := UserMaxBackends(); len(names) > max {
		ps = append(ps, ConfigProblem{Severity: SevError, Message: fmt.Sprintf("trop de backends : %d déclarés, la limite est de %d par utilisateur", len(names), max)})
	}
	// hôtes à résoudre : en parallèle et dans un délai total borné (un fichier de 10 backends ne bloque pas 40 s)
	var toResolve []string
	for _, n := range names {
		if t := f.Backends[n]["type"]; t == "docker" || t == "podman" {
			if h, _, err := parseUserEndpoint(str0(f.Backends[n], "host"), "tcp"); err == nil {
				toResolve = append(toResolve, h)
			}
		}
	}
	resolved := checkResolvedAll(toResolve)
	bad := func(n, msg string) {
		ps = append(ps, ConfigProblem{Severity: SevError, Line: keyLine(content, n), Message: "backend « " + shortName(n) + " » : " + msg})
	}
	str := func(be map[string]any, k string) string { v, _ := be[k].(string); return v }
	for _, n := range names {
		be := f.Backends[n]
		if err := CheckUserBackendName(n); err != nil {
			ps = append(ps, ConfigProblem{Severity: SevError, Line: keyLine(content, n), Message: err.Error()})
			continue
		}
		t := str(be, "type")
		for _, k := range []string{"host", "engine_host"} {
			if h := strings.ToLower(strings.TrimSpace(str(be, k))); strings.HasPrefix(h, "ssh:") {
				bad(n, "ssh est interdit en mode serveur ; utilise docker (TLS) ou k8s")
			}
		}
		switch t {
		case "ssh":
			bad(n, "ssh est interdit en mode serveur ; utilise docker (TLS) ou k8s")
		case "k8s":
			if v, ok := be["kubeconfig"].(bool); !ok || !v {
				bad(n, "« kubeconfig: true » obligatoire (le kubeconfig se colle dans les réglages du backend ; jamais le compte du launcher)")
			}
			ns := str(be, "namespace")
			if !dnsLabelRE.MatchString(ns) {
				bad(n, "« namespace » obligatoire : un nom DNS-1123 (minuscules, chiffres, tirets, 63 caractères au plus)")
			}
			if pvc := str(be, "pvc"); pvc != "" && !dnsLabelRE.MatchString(pvc) {
				bad(n, "« pvc » : un nom DNS-1123 (minuscules, chiffres, tirets, 63 caractères au plus)")
			}
		case "docker", "podman":
			if h, _, err := parseUserEndpoint(str(be, "host"), "tcp"); err != nil {
				bad(n, HostRefusal)
			} else if rerr := resolved[h]; rerr != nil {
				bad(n, HostRefusal)
			}
			if v, has := be["volume"]; has {
				if vs, ok := v.(string); !ok || !volumeNameRE.MatchString(vs) {
					bad(n, "« volume » : un nom de volume seulement (lettres, chiffres, « _ », « . », « - » ; jamais un chemin)")
				}
			}
			tlsOn, verify := false, true
			if tm, ok := be["tls"].(map[string]any); ok {
				tlsOn, _ = tm["enabled"].(bool)
				if v, has := tm["verify"].(bool); has {
					verify = v
				}
			}
			if t == "podman" && tlsOn {
				bad(n, "podman n'a pas de TLS : le placer derrière un proxy TLS et déclarer le backend docker")
			}
			if !tlsOn || !verify {
				ps = append(ps, ConfigProblem{Severity: SevWarning, Line: keyLine(content, n), Message: "backend « " + shortName(n) + " » : " + RootOnHostWarning})
			}
		}
	}
	return ps
}

// SaveBackends : enregistre backends.yaml de l'espace d'un utilisateur — validé d'abord (ErrConfigInvalid), refusé si le
// fichier a changé depuis l'ouverture (expectedMtime, ErrConfigConflict), copie .bak, écriture atomique 0600 sous verrou.
func (s Space) SaveBackends(content []byte, expectedMtime time.Time) ([]ConfigProblem, error) {
	if s.Admin {
		return nil, errors.New("espace de l'admin : éditeur actuel")
	}
	ps := s.ValidateBackends(content)
	if HasErrors(ps) {
		return ps, ErrConfigInvalid
	}
	if err := s.Ensure(); err != nil {
		return ps, err
	}
	path := s.BackendsPath()
	return ps, WithFileLock(path, func() error {
		st, err := os.Stat(path)
		switch {
		case err == nil && (expectedMtime.IsZero() || !st.ModTime().Equal(expectedMtime)):
			return ErrConfigConflict
		case err != nil && !os.IsNotExist(err):
			return err
		case err != nil && !expectedMtime.IsZero():
			return ErrConfigConflict
		}
		if err == nil {
			old, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			if berr := WriteFileAtomic(path+".bak", old, 0o600); berr != nil {
				return fmt.Errorf("copie .bak : %w", berr)
			}
		}
		return WriteFileAtomic(path, content, 0o600)
	})
}

// UserAdminFields : le backends.yaml proposé sort de ce qu'un utilisateur peut déclarer (règle R1, contracts/auth.md §4 et
// user-backends.md §3). LISTE BLANCHE, échec fermé : seule la rubrique « backends: » ; chaque backend est k8s, docker ou podman ;
//   - k8s : type, namespace, pvc, spread, kubeconfig (true seulement), pod (resources, nodeSelector, affinity seulement) ;
//   - docker / podman : type, host OBLIGATOIRE en tcp://, volume, tls (enabled, verify) ;
//
// tout autre champ (kubeconfig, context, volumes, pool, hosts, engine, engine_host, shared…) est refusé jusqu'à ce que le LOT-3.1
// les ouvre avec leurs contrôles, comme tout backend ssh, tout socket local et tout moteur local sans host. YAML illisible : vrai.
func UserAdminFields(content []byte) bool {
	if len(strings.TrimSpace(string(content))) == 0 {
		return false
	}
	var f map[string]any
	if yaml.Unmarshal(content, &f) != nil || yamlDupKeyCI(content) { // clé en double (casse comprise) : échec fermé
		return true
	}
	for k := range f {
		if k != "backends" {
			return true // pools, hubs, defaults, images… : rubriques de l'administrateur
		}
	}
	if f["backends"] == nil {
		return false
	}
	bs, ok := f["backends"].(map[string]any)
	if !ok {
		return true
	}
	for _, v := range bs {
		be, ok := v.(map[string]any)
		if !ok {
			return true
		}
		if !userBackendOK(be) {
			return true
		}
	}
	return false
}

// userBackendOK : un backend de l'utilisateur reste dans la liste blanche.
func userBackendOK(be map[string]any) bool {
	allowed := func(keys ...string) bool {
		for k := range be {
			ok := false
			for _, a := range keys {
				ok = ok || k == a
			}
			if !ok {
				return false
			}
		}
		return true
	}
	str := func(k string) string { s, _ := be[k].(string); return s }
	switch str("type") {
	case "k8s":
		if !allowed("type", "namespace", "pvc", "spread", "pod", "kubeconfig") {
			return false
		}
		if v, has := be["kubeconfig"]; has && v != true { // seul « true » (coffre de l'utilisateur) : jamais un chemin du poste
			return false
		}
		if _, has := be["spread"]; has {
			if _, isBool := be["spread"].(bool); !isBool {
				return false
			}
		}
		for _, k := range []string{"namespace", "pvc"} {
			if _, has := be[k]; has {
				if _, isStr := be[k].(string); !isStr {
					return false
				}
			}
		}
		if p, has := be["pod"]; has && p != nil {
			pod, isMap := p.(map[string]any)
			if !isMap {
				return false
			}
			for k := range pod {
				if k != "resources" && k != "nodeSelector" && k != "affinity" {
					return false
				}
			}
		}
		return true
	case "docker", "podman":
		if !allowed("type", "host", "volume", "tls") {
			return false
		}
		host := strings.TrimSpace(str("host"))
		if len(host) <= len("tcp://") || !strings.EqualFold(host[:len("tcp://")], "tcp://") || strings.Contains(host, "{}") {
			return false // host obligatoire, tcp:// seulement (ni unix, npipe, fd, ssh, https, chemin nu, modèle de pool)
		}
		if _, has := be["volume"]; has {
			if _, isStr := be["volume"].(string); !isStr {
				return false
			}
		}
		if t, has := be["tls"]; has && t != nil {
			tls, isMap := t.(map[string]any)
			if !isMap {
				return false
			}
			for k, v := range tls {
				if _, isBool := v.(bool); (k != "enabled" && k != "verify") || !isBool {
					return false
				}
			}
		}
		return true
	}
	return false // host, ssh, type absent ou inconnu
}

// userFieldsOK : namespace, PVC, volume et hôte d'un backend lu du fichier d'un utilisateur respectent les formats (comme à
// l'enregistrement) ; sans résolution DNS (revérifiée avant chaque appel).
func userFieldsOK(c BackendConf) error {
	bad := func(what string) error { return fmt.Errorf("backend « %s » : %s invalide", shortName(c.Name), what) }
	switch c.Type {
	case "k8s":
		if !dnsLabelRE.MatchString(c.Namespace) {
			return bad("namespace")
		}
		if c.PVC != "" && !dnsLabelRE.MatchString(c.PVC) {
			return bad("pvc")
		}
	case "docker", "podman":
		if _, _, err := parseUserEndpoint(c.Host, "tcp"); err != nil {
			return errors.New("backend « " + shortName(c.Name) + " » : " + HostRefusal)
		}
		if c.Volume != "" && !volumeNameRE.MatchString(c.Volume) {
			return bad("volume")
		}
	default:
		return bad("type")
	}
	return nil
}

func str0(be map[string]any, k string) string { v, _ := be[k].(string); return v }
