package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ── Approbation de l'environnement d'un projet cloné (#215, contracts/desktop-dispatch.md) ──
//
// Un dépôt cloné n'est pas de confiance : son .claude-launcher/env.yaml et env-template.yaml (image, paquets, setup,
// on_start, mounts…) ne sont jamais appliqués sans accord. Le registre trust.json vit dans le dossier de config du
// launcher — JAMAIS dans le projet, qu'un dépôt pourrait fournir : un marqueur posé par le dépôt est sans effet.
// Choix définitif : accepted / ignored ne sont plus remis en question par une modification ultérieure des fichiers ;
// seule l'action « Revoir » remet l'entrée à pending.

// TrustState : état d'approbation. TrustTrusted = pas d'entrée (projets existants, vide, template).
type TrustState string

const (
	TrustTrusted  TrustState = "trusted"
	TrustPending  TrustState = "pending"
	TrustAccepted TrustState = "accepted"
	TrustIgnored  TrustState = "ignored"
)

// ErrEnvNotApproved : l'environnement du dépôt cloné n'est pas encore approuvé.
var ErrEnvNotApproved = errors.New("environnement du dépôt cloné non approuvé : ouvrir le projet pour l'examiner")

// TrustEntry : une ligne du registre.
type TrustEntry struct {
	State  TrustState `json:"state"`
	SHA    string     `json:"sha,omitempty"`
	Source string     `json:"source,omitempty"` // URL clonée
	Image  string     `json:"image,omitempty"`  // socle choisi à la création (gardé par « Ouvrir sans »)
	At     string     `json:"at,omitempty"`
}

func trustPath() string { return filepath.Join(Ng2ConfigDir(), "trust.json") }

var (
	trustMu    sync.Mutex
	trustCache struct {
		path  string
		mtime time.Time
		size  int64
		m     map[string]TrustEntry
	}
)

// readTrust : registre complet (mémorisé tant que le fichier ne change pas : LoadEnv est appelé souvent).
func readTrust() map[string]TrustEntry {
	trustMu.Lock()
	defer trustMu.Unlock()
	p := trustPath()
	st, err := os.Stat(p)
	if err != nil {
		trustCache.path, trustCache.m = p, nil
		return nil
	}
	if trustCache.path == p && trustCache.m != nil && st.ModTime().Equal(trustCache.mtime) && st.Size() == trustCache.size {
		return trustCache.m
	}
	m := map[string]TrustEntry{}
	if b, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	trustCache.path, trustCache.mtime, trustCache.size, trustCache.m = p, st.ModTime(), st.Size(), m
	return m
}

func envTrustEntry(proj string) (TrustEntry, bool) {
	e, ok := readTrust()[RemotePath(proj)]
	return e, ok
}

// EnvTrustEntry : entrée du registre pour le projet (absent : de confiance).
func EnvTrustEntry(proj string) (TrustEntry, bool) { return envTrustEntry(proj) }

// EnvTrust : état d'approbation du projet (TrustTrusted : pas d'entrée).
func EnvTrust(proj string) TrustState {
	if e, ok := envTrustEntry(proj); ok && e.State != "" {
		return e.State
	}
	return TrustTrusted
}

// setTrust : lire-modifier-écrire du registre sous verrou, écriture atomique ; e nil : entrée retirée.
func setTrust(proj string, e *TrustEntry) error {
	return WithFileLock(trustPath(), func() error {
		m := map[string]TrustEntry{}
		if b, err := os.ReadFile(trustPath()); err == nil {
			_ = json.Unmarshal(b, &m)
		}
		if e == nil {
			delete(m, RemotePath(proj))
		} else {
			m[RemotePath(proj)] = *e
		}
		b, _ := json.MarshalIndent(m, "", "  ")
		err := WriteFileAtomic(trustPath(), b, 0o600)
		trustMu.Lock()
		trustCache.m = nil // relu au prochain accès (la date et la taille peuvent ne pas changer)
		trustMu.Unlock()
		return err
	})
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

// MarkEnvPending : fin d'un clonage réussi — si le dépôt cloné porte env.yaml ou env-template.yaml, l'entrée
// « pending » est écrite (source = URL, image = socle choisi). Rend vrai si une entrée a été écrite.
func MarkEnvPending(proj, source, image string) (bool, error) {
	if !HasEnv(proj) {
		return false, nil
	}
	return true, setTrust(proj, &TrustEntry{State: TrustPending, SHA: EnvFilesSHA(proj), Source: source, Image: image,
		At: time.Now().UTC().Format(time.RFC3339)})
}

// ReviewEnvTrust : « Revoir » — remet à pending l'entrée d'un projet cloné (la question revient à la prochaine ouverture).
func ReviewEnvTrust(proj string) error {
	e, ok := envTrustEntry(proj)
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
}

// EnvTrustInfo : charge utile d'envTrustInfo et de l'évènement envTrust. Les textes sont rendus TELS QUELS : l'interface
// les échappe (jamais interprétés en HTML ni en séquences de terminal).
type EnvTrustInfo struct {
	Path        string            `json:"path"`
	State       TrustState        `json:"state"`
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
	ent, ok := envTrustEntry(proj)
	if !ok {
		return EnvTrustInfo{}, errors.New("aucune approbation en attente")
	}
	info := EnvTrustInfo{Path: proj, State: ent.State, Source: ent.Source, SHA: EnvFilesSHA(proj), ChosenImage: ent.Image,
		Files: map[string]string{
			"env.yaml":          readLimited(EnvPath(proj)),
			"env-template.yaml": readLimited(envTemplatePath(proj)),
		}}
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
	}
	return info, nil
}

// AnswerEnvTrust : réponse à la question. choice : accept (accepted, définitif), ignore (ignored : socle choisi seul),
// cancel (rien n'est écrit). sha : celui d'EnvTrustInfoFor ; fichier modifié pendant la question → retry.
func AnswerEnvTrust(proj, choice, sha string) (retry bool, err error) {
	ent, ok := envTrustEntry(proj)
	if !ok || ent.State != TrustPending {
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
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
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
