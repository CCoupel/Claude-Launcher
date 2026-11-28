package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Ouverture d'un projet d'UTILISATEUR (LOT-3.2 jalon 4) : même parcours que OpenCtr (conteneur prêt, claude, session tmux, propriétaire) mais SANS rien
// de l'administrateur : backend fixé, env.yaml lu par exec dans le volume du projet puis vérifié par l'approbation scellée (§6 bis), chemin
// /space/PROJETS/<catégorie>/<nom>, ni Place ni table de montages, ni env.yaml du launcher, ni configuration de l'admin.

// UserTmuxConf : configuration tmux écrite dans le stockage de l'utilisateur (le conteneur ne s'installe pas en root).
const UserTmuxConf = "/space/.claude/cl-tmux.conf"

const userTmuxConfText = `set -g default-shell /bin/bash
set -g default-terminal tmux-256color
set -g prefix C-b
set -g status off
set -g mouse on
set -g escape-time 10
set -g history-limit 50000
bind-key -n DoubleClick1Pane select-pane -t = \; resize-pane -Z -t =
bind-key R set -wF @cl_relayout "#{e|+:#{?@cl_relayout,#{@cl_relayout},0},1}" \; display-message "relayout demandé"
`

// ErrUserEnvNotApproved : l'environnement du projet (env.yaml) n'est pas approuvé (jamais, modifié depuis, forgé, clé changée) : rien n'est démarré
// tant que l'utilisateur n'a pas répondu.
type ErrUserEnvNotApproved struct {
	Files   []string // fichiers à approuver (noms)
	Changed bool     // une approbation existait mais ne couvre plus ces fichiers
}

func (e ErrUserEnvNotApproved) Error() string {
	if e.Changed {
		return "l'environnement du projet a changé depuis son approbation"
	}
	return "l'environnement du projet n'est pas approuvé"
}

var userEnvFiles = []string{"env.yaml", "env-template.yaml"}

const maxUserEnvFile = 64 << 10

// readUserFile : contenu d'un fichier du projet lu PAR EXEC (borné), ok faux si absent.
func (e Engine) readUserFile(obj, abs string, max int) ([]byte, bool) {
	b, err := e.execOut(obj, userReadTimeout, int64(max)+1, nil, "sh", "-c", `test -f "$1" && head -c $2 -- "$1"`, "sh", abs, fmt.Sprint(max+1))
	if err != nil && !errors.Is(err, ErrOutputTooLarge) {
		return nil, false
	}
	return b, true
}

// UserEnvFiles : fichiers d'environnement présents dans le projet (nom → contenu).
func (e Engine) UserEnvFiles(cat, name string) (map[string][]byte, error) {
	obj, dir := e.ObjectName(cat, name), UserProjectDir(cat, name)
	files := map[string][]byte{}
	for _, f := range userEnvFiles {
		if b, ok := e.readUserFile(obj, dir+"/"+f, maxUserEnvFile); ok {
			if len(b) > maxUserEnvFile {
				return nil, fmt.Errorf("%s : fichier trop gros (64 Kio au plus)", f)
			}
			files[f] = b
		}
	}
	return files, nil
}

func userTrustPath(cat, name string) string {
	return UserProjectDir(cat, name) + "/.claude-launcher/trust.json"
}

// CheckUserEnvApproved : l'approbation scellée du projet couvre les fichiers ACTUELS ; sinon ErrUserEnvNotApproved. Sans fichier d'environnement : rien à approuver.
func (e Engine) CheckUserEnvApproved(key []byte, cat, name string, files map[string][]byte) error {
	if len(files) == 0 {
		return nil
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	raw, have := e.readUserFile(e.ObjectName(cat, name), userTrustPath(cat, name), 16<<10)
	if have && VerifyTrust(key, e.B.uid, e.B.Name, UserProjectDir(cat, name), files, raw) {
		return nil
	}
	return ErrUserEnvNotApproved{Files: names, Changed: have}
}

// ApproveUserEnv : l'utilisateur approuve les fichiers actuels : trust.json scellé (clé de l'utilisateur) écrit dans le volume du projet.
func (e Engine) ApproveUserEnv(key []byte, cat, name string, files map[string][]byte) error {
	tr, err := BuildTrust(key, e.B.uid, e.B.Name, UserProjectDir(cat, name), files)
	if err != nil {
		return err
	}
	obj := e.ObjectName(cat, name)
	if err := e.ownerOf(obj); err != nil {
		return err
	}
	if err := e.runIn(obj, "mkdir", "-p", "--", UserProjectDir(cat, name)+"/.claude-launcher"); err != nil {
		return err
	}
	return e.Exec(obj, false, bytes.NewReader(tr), io.Discard, "tee", "--", userTrustPath(cat, name))
}

// userEnvFromFiles : EnvSpec de l'utilisateur d'après ses fichiers (même fusion template + projet que l'admin, SANS le launcher).
func userEnvFromFiles(files map[string][]byte) (EnvSpec, error) {
	var p, t EnvSpec
	var err error
	pb, hasP := files["env.yaml"]
	tb, hasT := files["env-template.yaml"]
	if hasP {
		if p, err = parseEnvFile(pb, envFile); err != nil {
			return p, err
		}
	}
	e := p
	if hasT {
		var keys map[string]any
		if yaml.Unmarshal(tb, &keys) == nil {
			if _, ok := keys["prefer"]; ok {
				return e, fmt.Errorf("%s : « prefer » réservé à %s", envTemplateFile, envFile)
			}
		}
		if t, err = parseEnvFile(tb, envTemplateFile); err != nil {
			return t, err
		}
		e = t.withProject(p)
	}
	return e, nil
}

// waitUserReady : le démarrage du conteneur (bootstrap root puis setpriv) est terminé quand le marqueur existe.
func (e Engine) waitUserReady(obj string, out io.Writer, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	for {
		if e.ExecD(userReadTimeout, obj, false, nil, io.Discard, "test", "-f", "/space/.claude-launcher/ready") == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("le conteneur n'est pas prêt (démarrage trop long) : voir ses journaux sur ton backend")
		}
		time.Sleep(time.Second)
	}
}

// userReadyWait : délai d'attente du démarrage (variable : tests).
var userReadyWait = 180 * time.Second

// OpenUserProject : conteneur prêt, environnement approuvé, claude installé, session tmux créée (démarrage tapé). L'approbation est vérifiée à CHAQUE
// ouverture (key : clé d'approbation de l'utilisateur) ; non approuvé → ErrUserEnvNotApproved, rien d'autre n'est fait que le démarrage du conteneur.
func (e Engine) OpenUserProject(key []byte, cat, name string, t CtrOpenIO) (CtrOpen, error) {
	out := t.Out
	if out == nil {
		out = io.Discard
	}
	if !e.B.user {
		return CtrOpen{}, errors.New("backend d'utilisateur attendu")
	}
	obj, err := e.ensureUser(UserProjectDir(cat, name), EnvSpec{}, out)
	if err != nil {
		return CtrOpen{}, err
	}
	pod := strings.TrimSuffix(obj, "-0")
	if !e.B.K8s() {
		pod = obj
	}
	fmt.Fprintf(out, "\033[1m⬡ %s/%s — %s\033[0m\n", cat, name, e.B.Name)
	if err := e.waitUserReady(obj, out, userReadyWait); err != nil {
		return CtrOpen{}, err
	}
	files, err := e.UserEnvFiles(cat, name)
	if err != nil {
		return CtrOpen{}, err
	}
	if err := e.CheckUserEnvApproved(key, cat, name, files); err != nil {
		return CtrOpen{}, err
	}
	env, err := userEnvFromFiles(files)
	if err != nil {
		return CtrOpen{}, err
	}
	if ps := CheckUserEnv(env, UserDefaultImage()); len(ps) > 0 {
		return CtrOpen{}, errors.New(ps[0].Field + " : " + ps[0].Msg)
	}
	_ = pod
	if err := e.Exec(obj, false, strings.NewReader(userTmuxConfText), io.Discard, "tee", "--", UserTmuxConf); err != nil {
		return CtrOpen{}, errors.New("configuration tmux impossible à écrire dans ton stockage")
	}
	if err := e.installUserClaude(obj, out); err != nil {
		return CtrOpen{}, err
	}
	if !e.HasSession(obj) {
		var start []byte
		for i := 0; i < 50 && len(start) == 0 && t.Start != nil; i++ {
			if start = t.Start(); len(start) == 0 {
				time.Sleep(100 * time.Millisecond)
			}
		}
		w, h := 200, 50
		if t.Size != nil {
			w, h = t.Size()
		}
		dir := UserProjectDir(cat, name)
		if err := e.runIn(obj, "mkdir", "-p", "--", dir); err != nil {
			return CtrOpen{}, err
		}
		if err := e.Exec(obj, false, nil, out, TmuxIn("new-session", "-d", "-s", CtrSession, "-x", fmt.Sprint(w), "-y", fmt.Sprint(h), "-c", dir)...); err != nil {
			return CtrOpen{}, fmt.Errorf("tmux du projet : %w", err)
		}
		if len(start) > 0 {
			_ = e.Exec(obj, false, nil, out, TmuxIn("send-keys", "-t", "="+CtrSession+":", string(start), "Enter")...)
		}
	}
	if t.Done != nil {
		t.Done()
	}
	res := CtrOpen{E: e, Name: obj}
	if o := e.Owner(obj); o.Alive() && o.Id != LauncherID() && t.Owner != nil {
		switch t.Owner(o) {
		case "l":
			res.ReadOnly = true
		case "p":
			e.TakeOver(obj)
		default:
			res.Canceled = true
		}
	} else {
		e.ClaimFree(obj)
	}
	return res, nil
}

// UserStartCommand : ce qui est tapé dans la session du projet — claude, installé dans le $HOME de l'utilisateur.
func UserStartCommand(cat, name string) string {
	return "cd " + Shq(UserProjectDir(cat, name)) + " && export PATH=\"$HOME/.local/bin:$PATH\" && claude"
}

// installUserClaude : claude dans $HOME par l'uid du traitement (jamais en root) ; déjà installé : rien.
func (e Engine) installUserClaude(obj string, out io.Writer) error {
	if os.Getenv("CL_CTR_SKIP_CLAUDE") == "1" {
		return nil
	}
	script := "export PATH=\"$HOME/.local/bin:$PATH\"\ncommand -v claude >/dev/null 2>&1 && exit 0\necho '  installation de claude'\ncurl -fsSL https://claude.ai/install.sh | bash\n"
	if err := e.ExecD(userLongTimeout, obj, false, strings.NewReader(script), out, "bash", "-e", "-s"); err != nil {
		return fmt.Errorf("installation de claude : %w", err)
	}
	return nil
}

// UserEnvSHA : empreinte des fichiers d'environnement affichés à l'utilisateur ; la réponse « accepter » doit la rappeler (fichier modifié pendant la question → retry).
func UserEnvSHA(files map[string][]byte) string {
	d := digestsOf(files)
	names := make([]string, 0, len(d))
	for n := range d {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		b.WriteString(n + "=" + d[n] + "\n")
	}
	h := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(h[:])
}

// UserEnvTrustInfo : charge utile de la question d'approbation d'un projet d'utilisateur (même forme que celle de l'admin : l'interface la rend telle quelle).
func UserEnvTrustInfo(id string, files map[string][]byte, changed bool) EnvTrustInfo {
	info := EnvTrustInfo{Path: id, State: TrustPending, Changed: changed, SHA: UserEnvSHA(files), Source: "projet", Files: map[string]string{}}
	for n, b := range files {
		info.Files[n] = string(b)
	}
	nz := func(l []string) []string {
		if l == nil {
			return []string{}
		}
		return l
	}
	info.Spec = EnvTrustSpec{Packages: []string{}, PackagesByManager: map[string][]string{}, Setup: []string{}, OnStart: []string{}, Watch: []string{}, Mounts: []EnvTrustMount{}, Local: []string{}}
	info.Risk.Scripts = []string{}
	if e, err := userEnvFromFiles(files); err == nil {
		info.Spec.Image, info.Spec.Packages, info.Spec.Setup, info.Spec.OnStart = e.Image, nz(e.Packages), nz(e.Setup), nz(e.OnStart)
		info.Spec.Watch, info.Spec.Local = nz(e.Watch), nz(e.Local)
		if e.PackagesByManager != nil {
			info.Spec.PackagesByManager = e.PackagesByManager
		}
		for _, m := range e.Mounts {
			info.Spec.Mounts = append(info.Spec.Mounts, EnvTrustMount{Path: m.Path, RO: m.RO == nil || *m.RO})
			info.Risk.HostMounts++
		}
		info.Risk.Exec = len(e.Setup) + len(e.OnStart)
		info.Risk.Packages = len(e.Packages)
		info.Risk.Watch, info.Risk.Local = len(e.Watch), len(e.Local)
	}
	return info
}
