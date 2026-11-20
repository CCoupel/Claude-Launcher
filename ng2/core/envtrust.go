package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

// ── Approbation de l'environnement d'un projet cloné (#215, contracts/desktop-dispatch.md) ──
//
// Un dépôt cloné n'est pas de confiance : son .claude-launcher/env.yaml et env-template.yaml (image, paquets, setup,
// on_start, mounts…) ne sont jamais appliqués sans accord. Le registre trust.json vit dans le dossier de config du
// launcher — JAMAIS dans le projet, qu'un dépôt pourrait fournir : un marqueur posé par le dépôt est sans effet.
// L'empreinte (sha) approuvée est comparée à chaque ouverture : si env.yaml / env-template.yaml a changé depuis
// l'approbation (accepted, ignored ou none), la question revient (état pending), sans afficher de différences. Les
// écritures du launcher lui-même (F2, réglages) mettent l'empreinte à jour. Un registre illisible échoue FERMÉ.

// TrustState : état d'approbation. TrustTrusted = pas d'entrée (projets existants, vide, template).
type TrustState string

const (
	TrustTrusted  TrustState = "trusted"
	TrustPending  TrustState = "pending"
	TrustAccepted TrustState = "accepted"
	TrustIgnored  TrustState = "ignored"
	TrustNone     TrustState = "none" // projet cloné sans fichier d'environnement : un env.yaml qui apparaît ensuite → pending
)

// ErrEnvNotApproved : l'environnement du dépôt cloné n'est pas approuvé (jamais approuvé, changé depuis, ou registre
// illisible).
var ErrEnvNotApproved = errors.New("environnement du dépôt cloné non approuvé : ouvrir le projet pour l'examiner")

// ErrEnvChanged : l'environnement a changé depuis l'approbation (errors.Is ErrEnvNotApproved).
var ErrEnvChanged = fmt.Errorf("%w — l'environnement du dépôt a changé depuis l'approbation", ErrEnvNotApproved)

// ErrTrustBroken : trust.json présent mais illisible — jamais réécrit, tout projet portant un environnement est refusé.
// Le message dit comment s'en sortir (trustBrokenError) ; errors.Is(err, ErrTrustBroken) et ErrEnvNotApproved.
var ErrTrustBroken = errors.New("registre d'approbation illisible")

type trustBrokenError struct{}

func (trustBrokenError) Error() string {
	return fmt.Sprintf("registre d'approbation illisible (%s) : restaurez trust.json.bak ou supprimez trust.json pour repartir de zéro", trustPath())
}

func (trustBrokenError) Is(t error) bool { return t == ErrTrustBroken || t == ErrEnvNotApproved }

// TrustEntry : une ligne du registre.
type TrustEntry struct {
	State  TrustState `json:"state"`
	SHA    string     `json:"sha,omitempty"`
	Source string     `json:"source,omitempty"` // URL clonée
	Image  string     `json:"image,omitempty"`  // socle choisi à la création (gardé par « Ouvrir sans »)
	At     string     `json:"at,omitempty"`
}

// trustPath : le registre vit dans le dossier PARTAGÉ — <CL_ROOT_BASE>/.config/claude-launcher-ng2/ (comme le coffre des
// secrets #201), pour que l'exe Windows, le hub et le pod partagent les approbations. CL_CONFIG_DIR (tests, installation
// explicite) l'emporte ; sans racine de base : dossier de config du launcher (Ng2ConfigDir).
func trustPath() string {
	if os.Getenv("CL_CONFIG_DIR") == "" {
		if d := RootBaseConfigDir(); d != "" {
			return filepath.Join(d, "trust.json")
		}
	}
	return filepath.Join(Ng2ConfigDir(), "trust.json")
}

// TrustLegacyDirVar : ancien dossier contenant un trust.json local à fusionner (tests ; sinon le dossier de config local
// du poste quand une racine de base est posée).
const TrustLegacyDirVar = "CL_TRUST_LEGACY_DIR"

func trustLegacyPaths(dest string) []string {
	var dirs []string
	if d := strings.TrimSpace(os.Getenv(TrustLegacyDirVar)); d != "" {
		dirs = append(dirs, d)
	} else if os.Getenv("CL_CONFIG_DIR") == "" && RootBaseConfigDir() != "" {
		home, _ := os.UserHomeDir()
		ucd := ""
		if runtime.GOOS == "windows" {
			ucd, _ = os.UserConfigDir()
		}
		dirs = append(dirs, defaultConfigDir(runtime.GOOS, home, ucd))
		if d := portableConfigDir(); d != "" {
			dirs = append(dirs, d)
		}
	}
	var out []string
	for _, d := range dirs {
		p := filepath.Join(d, "trust.json")
		if filepath.Clean(p) != filepath.Clean(dest) {
			if _, err := os.Stat(p); err == nil {
				out = append(out, p)
			}
		}
	}
	return out
}

// MigrateLegacyTrust : fusionne dans le registre partagé les entrées d'un trust.json local resté sur le poste (jamais de
// perte : une entrée absente du registre partagé est ajoutée ; en cas de désaccord le registre partagé l'emporte et
// l'écart est journalisé ; l'ancien fichier est conservé). Rend le nombre d'entrées ajoutées. Un registre partagé
// illisible n'est pas réécrit (erreur) ; un ancien fichier illisible est ignoré (journalisé).
func MigrateLegacyTrust() (added int, err error) {
	dest := trustPath()
	legacy := trustLegacyPaths(dest)
	if len(legacy) == 0 {
		return 0, nil
	}
	err = WithFileLock(dest, func() error {
		m := map[string]TrustEntry{}
		var rawDest []byte
		if raw, rerr := os.ReadFile(dest); rerr == nil {
			if len(strings.TrimSpace(string(raw))) == 0 || json.Unmarshal(raw, &m) != nil {
				return trustBrokenError{}
			}
			rawDest = raw
		}
		for _, lp := range legacy {
			raw, rerr := os.ReadFile(lp)
			l := map[string]TrustEntry{}
			if rerr != nil || json.Unmarshal(raw, &l) != nil {
				log.Printf("registre d'approbation : %s illisible, ignoré", lp)
				continue
			}
			for k, e := range l {
				cur, ok := m[k]
				switch {
				case !ok:
					m[k] = e
					added++
				case cur != e:
					log.Printf("registre d'approbation : %s : entrée locale différente de l'entrée partagée, entrée partagée conservée", k)
				}
			}
		}
		if added == 0 {
			return nil
		}
		if rawDest != nil {
			if werr := WriteFileAtomic(dest+".bak", rawDest, 0o600); werr != nil {
				return werr
			}
		}
		if merr := os.MkdirAll(filepath.Dir(dest), 0o755); merr != nil {
			return merr
		}
		b, _ := json.MarshalIndent(m, "", "  ")
		werr := WriteFileAtomic(dest, b, 0o600)
		trustMu.Lock()
		trustCache.valid = false
		trustMu.Unlock()
		return werr
	})
	if added > 0 && err == nil {
		log.Printf("registre d'approbation : %d entrée(s) fusionnée(s) depuis l'ancien emplacement local (conservé)", added)
	}
	return added, err
}

var (
	trustMu    sync.Mutex
	trustCache struct {
		path    string
		mtime   time.Time
		size    int64
		m       map[string]TrustEntry
		corrupt bool
		valid   bool
	}
)

// readTrust : registre complet et drapeau « illisible » (fichier présent mais vide ou non analysable). Mémorisé tant
// que le fichier ne change pas : LoadEnv est appelé souvent.
func readTrust() (map[string]TrustEntry, bool) {
	trustMu.Lock()
	defer trustMu.Unlock()
	p := trustPath()
	st, err := os.Stat(p)
	if err != nil {
		trustCache.path, trustCache.valid = p, false
		return nil, false
	}
	if trustCache.valid && trustCache.path == p && st.ModTime().Equal(trustCache.mtime) && st.Size() == trustCache.size {
		return trustCache.m, trustCache.corrupt
	}
	m := map[string]TrustEntry{}
	corrupt := false
	if b, err := os.ReadFile(p); err != nil || len(strings.TrimSpace(string(b))) == 0 || json.Unmarshal(b, &m) != nil {
		m, corrupt = nil, true
	}
	trustCache.path, trustCache.mtime, trustCache.size = p, st.ModTime(), st.Size()
	trustCache.m, trustCache.corrupt, trustCache.valid = m, corrupt, true
	return m, corrupt
}

// rawEntry : entrée du registre pour proj ; registre illisible + projet portant un environnement → entrée pending
// synthétique (échec fermé) ; broken le dit.
func rawEntry(proj string) (e TrustEntry, ok, broken bool) {
	m, corrupt := readTrust()
	if corrupt {
		if HasEnv(proj) {
			return TrustEntry{State: TrustPending}, true, true
		}
		return TrustEntry{}, false, true
	}
	e, ok = m[RemotePath(proj)]
	return e, ok, false
}

// effectiveTrust : entrée et état EFFECTIF — pending aussi quand l'empreinte approuvée ne correspond plus aux
// fichiers (changed) ou que le registre est illisible (broken).
func effectiveTrust(proj string) (e TrustEntry, st TrustState, changed, broken bool) {
	e, ok, broken := rawEntry(proj)
	if !ok {
		return e, TrustTrusted, false, broken
	}
	switch e.State {
	case TrustAccepted, TrustIgnored, TrustNone:
		if e.SHA != EnvFilesSHA(proj) {
			return e, TrustPending, true, false
		}
		return e, e.State, false, false
	}
	return e, TrustPending, false, broken
}

func envTrustEntry(proj string) (TrustEntry, bool) {
	e, ok, _ := rawEntry(proj)
	return e, ok
}

// EnvTrustEntry : entrée du registre pour le projet (absent : de confiance).
func EnvTrustEntry(proj string) (TrustEntry, bool) { return envTrustEntry(proj) }

// EnvTrust : état d'approbation EFFECTIF du projet (TrustTrusted : pas d'entrée ; pending aussi si les fichiers ont
// changé depuis l'approbation, ou si le registre est illisible).
func EnvTrust(proj string) TrustState {
	_, st, _, _ := effectiveTrust(proj)
	return st
}

// EnvTrustChanged : la question revient parce que les fichiers ont changé depuis l'approbation.
func EnvTrustChanged(proj string) bool {
	_, _, ch, _ := effectiveTrust(proj)
	return ch
}

// setTrust : lire-modifier-écrire du registre sous verrou, écriture atomique, copie .bak avant chaque écriture ; un
// registre illisible n'est JAMAIS réécrit (ErrTrustBroken). e nil : entrée retirée.
func setTrust(proj string, e *TrustEntry) error {
	return WithFileLock(trustPath(), func() error {
		m := map[string]TrustEntry{}
		if raw, err := os.ReadFile(trustPath()); err == nil {
			if len(strings.TrimSpace(string(raw))) == 0 || json.Unmarshal(raw, &m) != nil {
				return trustBrokenError{}
			}
			if err := WriteFileAtomic(trustPath()+".bak", raw, 0o600); err != nil {
				return err
			}
		}
		if e == nil {
			delete(m, RemotePath(proj))
		} else {
			m[RemotePath(proj)] = *e
		}
		b, _ := json.MarshalIndent(m, "", "  ")
		err := WriteFileAtomic(trustPath(), b, 0o600)
		trustMu.Lock()
		trustCache.valid = false // relu au prochain accès (la date et la taille peuvent ne pas changer)
		trustMu.Unlock()
		return err
	})
}

// withTrustRefresh : exécute write (écriture de env.yaml PAR LE LAUNCHER : F2, réglages) ; si l'empreinte approuvée
// correspondait avant l'écriture, elle suit l'écriture (pas de re-demande) — jamais si les fichiers avaient déjà été
// changés par le dépôt. Pour un projet ignoré, le socle gardé suit aussi.
func withTrustRefresh(proj string, write func() error) error {
	ent, st, _, broken := effectiveTrust(proj)
	track := !broken && (st == TrustAccepted || st == TrustIgnored || st == TrustNone)
	if !track {
		return write()
	}
	err := write()
	if err != nil {
		return err
	}
	ent.SHA = EnvFilesSHA(proj)
	if ent.State == TrustIgnored {
		if e, lerr := loadEnvRaw(proj); lerr == nil && e.Image != "" {
			ent.Image = e.Image
		}
	}
	_ = setTrust(proj, &ent)
	return nil
}

// EnvFilesSHA : SHA-256 du contenu de env-template.yaml puis env.yaml (absent = vide).
func EnvFilesSHA(proj string) string {
	t, _ := os.ReadFile(envTemplatePath(proj))
	p, _ := os.ReadFile(EnvPath(proj))
	h := sha256.New()
	h.Write([]byte("env-template.yaml\n"))
	h.Write(t)
	h.Write([]byte("\nenv.yaml\n"))
	h.Write(p)
	return hex.EncodeToString(h.Sum(nil))
}

// RegisterClone : AVANT le clonage — l'entrée pending du projet (un clone interrompu ne devient jamais de confiance).
// Retirée par l'undo de la création si elle échoue (CreateProjectDir).
func RegisterClone(proj, source, image string) error {
	return setTrust(proj, &TrustEntry{State: TrustPending, SHA: EnvFilesSHA(proj), Source: source, Image: image,
		At: time.Now().UTC().Format(time.RFC3339)})
}

// MarkEnvPending : fin d'un clonage réussi — l'entrée du projet cloné est écrite : « pending » si le dépôt porte
// env.yaml ou env-template.yaml (rend vrai), sinon « none » (aucun environnement ; un env.yaml qui apparaît ensuite
// → pending). source = URL, image = socle choisi.
func MarkEnvPending(proj, source, image string) (bool, error) {
	e := TrustEntry{State: TrustNone, SHA: EnvFilesSHA(proj), Source: source, Image: image, At: time.Now().UTC().Format(time.RFC3339)}
	if HasEnv(proj) {
		e.State = TrustPending
	}
	return e.State == TrustPending, setTrust(proj, &e)
}

// ReviewEnvTrust : « Revoir » — remet à pending l'entrée d'un projet cloné (la question revient à la prochaine ouverture).
func ReviewEnvTrust(proj string) error {
	e, ok, broken := rawEntry(proj)
	if broken {
		return trustBrokenError{}
	}
	if !ok {
		return errors.New("projet de confiance : rien à revoir")
	}
	e.State, e.SHA, e.At = TrustPending, EnvFilesSHA(proj), time.Now().UTC().Format(time.RFC3339)
	return setTrust(proj, &e)
}

// ── Question posée à l'utilisateur ────────────────────────────────────────

// EnvTrustMount : montage demandé par le dépôt.
type EnvTrustMount struct {
	Path string `json:"path"`
	RO   bool   `json:"ro"`
}

// EnvTrustSpec : ce que le dépôt demande, lu des fichiers bruts.
type EnvTrustSpec struct {
	Image             string              `json:"image"`
	Packages          []string            `json:"packages"`
	PackagesByManager map[string][]string `json:"packagesByManager"`
	Setup             []string            `json:"setup"`
	OnStart           []string            `json:"onStart"`
	Watch             []string            `json:"watch"`
	Mounts            []EnvTrustMount     `json:"mounts"`
	Local             []string            `json:"local"`
	Prefer            string              `json:"prefer"`
}

// EnvTrustRisk : nombre de commandes exécutées (setup + on_start) et de montages hors du projet.
type EnvTrustRisk struct {
	Exec       int `json:"exec"`
	HostMounts int `json:"hostMounts"`
	Packages   int `json:"packages"` // paquets installés (packages + packages_by_manager)
	Watch      int `json:"watch"`
	Local      int `json:"local"`
	// Scripts : scripts DU DÉPÔT que setup / on_start appellent (chemins relatifs, ./x, *.sh…) — leur version courante est
	// exécutée, elle n'est pas couverte par l'approbation (seuls env.yaml / env-template.yaml le sont)
	Scripts []string `json:"scripts"`
}

// EnvTrustInfo : charge utile d'envTrustInfo et de l'évènement envTrust. Les textes sont rendus TELS QUELS : l'interface
// les échappe (jamais interprétés en HTML ni en séquences de terminal).
type EnvTrustInfo struct {
	Path        string            `json:"path"`
	State       TrustState        `json:"state"`
	ScriptNote  string            `json:"scriptNote,omitempty"` // message si Risk.Scripts n'est pas vide
	Broken      string            `json:"broken,omitempty"`     // registre illisible : message de reprise (restaurer .bak ou supprimer)
	Changed     bool              `json:"changed"`              // la question revient : les fichiers ont changé depuis l'approbation (aucune différence affichée)
	Source      string            `json:"source"`
	SHA         string            `json:"sha"`
	ChosenImage string            `json:"chosenImage"`
	Spec        EnvTrustSpec      `json:"spec"`
	Files       map[string]string `json:"files"`
	Risk        EnvTrustRisk      `json:"risk"`
}

const trustFileMax = 64 * 1024

func readLimited(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	if len(b) > trustFileMax {
		b = append(b[:trustFileMax], []byte("\n… (tronqué)")...)
	}
	return string(b)
}

// EnvTrustInfoFor : la question pour proj (entrée du registre, fichiers bruts, spécification, risque).
func EnvTrustInfoFor(proj string) (EnvTrustInfo, error) {
	ent, st, changed, broken := effectiveTrust(proj)
	if st != TrustPending {
		return EnvTrustInfo{}, errors.New("aucune approbation en attente")
	}
	info := EnvTrustInfo{Path: proj, State: TrustPending, Changed: changed, Source: ent.Source, SHA: EnvFilesSHA(proj), ChosenImage: ent.Image,
		Files: map[string]string{
			"env.yaml":          readLimited(EnvPath(proj)),
			"env-template.yaml": readLimited(envTemplatePath(proj)),
		}}
	if broken {
		info.Broken = trustBrokenError{}.Error()
	}
	info.Spec = EnvTrustSpec{Packages: []string{}, PackagesByManager: map[string][]string{}, Setup: []string{}, OnStart: []string{},
		Watch: []string{}, Mounts: []EnvTrustMount{}, Local: []string{}}
	if e, err := loadEnvRaw(proj); err == nil || e.Image != "" || len(e.Setup) > 0 || len(e.Mounts) > 0 {
		nz := func(l []string) []string {
			if l == nil {
				return []string{}
			}
			return l
		}
		info.Spec.Image, info.Spec.Packages, info.Spec.Setup, info.Spec.OnStart = e.Image, nz(e.Packages), nz(e.Setup), nz(e.OnStart)
		info.Spec.Watch, info.Spec.Local, info.Spec.Prefer = nz(e.Watch), nz(e.Local), e.Prefer
		if e.PackagesByManager != nil {
			info.Spec.PackagesByManager = e.PackagesByManager
		}
		rp := RemotePath(proj)
		for _, m := range e.Mounts {
			ro := m.RO == nil || *m.RO
			info.Spec.Mounts = append(info.Spec.Mounts, EnvTrustMount{Path: m.Path, RO: ro})
			if !pathWithin(filepath.ToSlash(filepath.Clean(m.Path)), rp) {
				info.Risk.HostMounts++
			}
		}
		info.Risk.Exec = len(e.Setup) + len(e.OnStart)
		info.Risk.Packages = len(e.Packages)
		for _, l := range e.PackagesByManager {
			info.Risk.Packages += len(l)
		}
		info.Risk.Watch, info.Risk.Local = len(e.Watch), len(e.Local)
		info.Risk.Scripts = repoScripts(append(append([]string(nil), e.Setup...), e.OnStart...))
		if len(info.Risk.Scripts) > 0 {
			info.ScriptNote = ScriptNote
		}
	}
	return info, nil
}

// AnswerEnvTrust : réponse à la question. choice : accept (accepted, définitif), ignore (ignored : socle choisi seul),
// cancel (rien n'est écrit). sha : celui d'EnvTrustInfoFor ; fichier modifié pendant la question → retry.
func AnswerEnvTrust(proj, choice, sha string) (retry bool, err error) {
	ent, st, _, broken := effectiveTrust(proj)
	if broken {
		return false, trustBrokenError{}
	}
	if st != TrustPending {
		return false, errors.New("aucune approbation en attente")
	}
	switch choice {
	case "cancel":
		return false, nil
	case "accept", "ignore":
	default:
		return false, errors.New("choix inconnu : " + choice)
	}
	if cur := EnvFilesSHA(proj); sha != cur {
		return true, errors.New("l'environnement a encore changé")
	}
	ent.State = TrustAccepted
	if choice == "ignore" {
		ent.State = TrustIgnored
	}
	ent.SHA, ent.At = sha, time.Now().UTC().Format(time.RFC3339)
	return false, setTrust(proj, &ent)
}

// invisibleRune : caractères invisibles ou de réorganisation bidirectionnelle (« Trojan Source ») : zéro largeur,
// marques et surcharges directionnelles, isolats, marque de lettre arabe, BOM.
func invisibleRune(r rune) bool {
	switch {
	case r >= 0x200B && r <= 0x200F, r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069:
		return true
	}
	return r == 0x061C || r == 0xFEFF
}

// SafeTerminalText : texte du dépôt rendu sans séquence de terminal ni caractère de contrôle (ESC, C0, C1, DEL) : les
// retours à la ligne et tabulations sont gardés, le reste devient « · ». À utiliser avant tout affichage en terminal.
func SafeTerminalText(s string) string {
	var b []rune
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t':
			b = append(b, r)
		case r == '\r':
			b = append(b, '\n')
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) || invisibleRune(r):
			b = append(b, '·')
		default:
			b = append(b, r)
		}
	}
	return string(b)
}

// EnvContainerProject : le projet tourne (ou va tourner) en conteneur : affecté à un backend à moteur.
func EnvContainerProject(proj string) bool {
	if !Assigned(proj) {
		return false
	}
	_, ok := EngineOf(ProjectBackend(proj))
	return ok
}

// ScriptNote : avertissement affiché quand setup / on_start appellent un script du dépôt.
const ScriptNote = "ce script n'est pas couvert par l'approbation : sa version courante sera exécutée"

var scriptExts = []string{".sh", ".bash", ".zsh", ".py", ".rb", ".pl", ".js", ".mjs", ".ps1", ".bat", ".cmd", ".mk"}

// repoScripts : références à des scripts du dépôt dans des commandes (setup / on_start) — jetons de chemin relatif
// (« ./x », « ../x », « scripts/x ») ou d'extension de script (.sh, .py…) ; ni URL, ni option, ni chemin absolu ou « ~ » ;
// sans doublon, triés. Heuristique : elle signale, elle ne prouve rien.
func repoScripts(cmds []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range cmds {
		for _, tok := range strings.FieldsFunc(c, func(r rune) bool {
			return unicode.IsSpace(r) || strings.ContainsRune(";&|()<>`'\"=,", r)
		}) {
			if isScriptToken(tok) && !seen[tok] {
				seen[tok] = true
				out = append(out, tok)
			}
		}
	}
	sort.Strings(out)
	return out
}

func isScriptToken(t string) bool {
	switch {
	case t == "" || strings.HasPrefix(t, "-") || strings.Contains(t, "://") || strings.HasPrefix(t, "/") ||
		strings.HasPrefix(t, "~") || strings.HasPrefix(t, "$"):
		return false
	case strings.HasPrefix(t, "./") || strings.HasPrefix(t, "../"):
		return true
	}
	low := strings.ToLower(t)
	for _, e := range scriptExts {
		if strings.HasSuffix(low, e) {
			return true
		}
	}
	return strings.Contains(t, "/")
}
