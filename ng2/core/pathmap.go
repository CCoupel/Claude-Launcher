package core

import (
	"fmt"
	"os"
	"os/user"
	"path"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// ── Correspondance de chemins poste ↔ backends (#110) ─────────────────────
//
// Sous Windows, le launcher voit le stockage partagé sous un chemin Windows
// (\\wsl.localhost\Ubuntu\share\MFS\…) ; les backends (conteneurs, pods,
// machines ssh) le voient sous son chemin Linux (/share/MFS/…). Déclaration
// par machine, dans backends.yaml (commun à tous les hubs en mode portable) :
//
//	hubs:
//	  MON-PC:
//	    paths:
//	      - { local: '\\wsl.localhost\Ubuntu\share\MFS', remote: /share/MFS }
//
// Le launcher lit et parcourt les projets par leur chemin local, et transmet
// TOUJOURS aux backends le chemin Linux (remote) : montages, workdir, labels
// cl.project, nom « <catégorie>-<projet> », env, CLAUDE_CONFIG_DIR, clés de
// projects.json, contrôle du stockage partagé. Un même projet porte ainsi les
// mêmes nom et labels qu'il soit ouvert de Windows ou d'un hub Linux.
//
// Sans déclaration (cas de tout hub Linux) : chemins inchangés dans les deux sens.

// PathMap : un préfixe local et son équivalent vu des backends.
type PathMap struct {
	Local  string `yaml:"local" json:"local"`
	Remote string `yaml:"remote" json:"remote"`
}

// PathMapper : correspondances normalisées d'une machine.
type PathMapper struct {
	byLocal  []PathMap // préfixe local le plus long d'abord
	byRemote []PathMap // préfixe remote le plus long d'abord
}

// winPathForm : chemin de forme Windows (C:\…, \\serveur\…) ; comparé sans
// tenir compte de la casse ni du séparateur.
func winPathForm(p string) bool { return winHostRe.MatchString(p) }

// NewPathMapper : entrées validées (remote absolu Linux, local absolu Windows
// ou Linux), séparateurs finaux ôtés ; les invalides sont ignorées (warns).
func NewPathMapper(ps []PathMap) (PathMapper, []string) {
	var m PathMapper
	var warns []string
	for _, p := range ps {
		l, r := strings.TrimSpace(p.Local), strings.TrimSpace(p.Remote)
		switch {
		case !strings.HasPrefix(r, "/"):
			warns = append(warns, fmt.Sprintf("paths : remote %q : chemin Linux absolu attendu (entrée ignorée)", p.Remote))
			continue
		case l == "" || !winPathForm(l) && !strings.HasPrefix(l, "/"):
			warns = append(warns, fmt.Sprintf("paths : local %q : forme attendue C:\\…, \\\\serveur\\… ou /… (entrée ignorée)", p.Local))
			continue
		}
		if t := strings.TrimRight(r, "/"); t != "" {
			r = t
		}
		if winPathForm(l) {
			if t := strings.TrimRight(l, `\/`); t != "" && !(len(t) == 2 && t[1] == ':') {
				l = t
			} else if t != "" {
				l = t + `\` // « C: » → « C:\ »
			}
		} else if t := strings.TrimRight(l, "/"); t != "" {
			l = t
		}
		m.byLocal = append(m.byLocal, PathMap{l, r})
	}
	m.byRemote = append([]PathMap(nil), m.byLocal...)
	sort.SliceStable(m.byLocal, func(i, j int) bool { return len(m.byLocal[i].Local) > len(m.byLocal[j].Local) })
	sort.SliceStable(m.byRemote, func(i, j int) bool { return len(m.byRemote[i].Remote) > len(m.byRemote[j].Remote) })
	return m, warns
}

// Empty : aucune correspondance déclarée.
func (m PathMapper) Empty() bool { return len(m.byLocal) == 0 }

// Entries : correspondances, préfixe local le plus long d'abord.
func (m PathMapper) Entries() []PathMap { return append([]PathMap(nil), m.byLocal...) }

// cutLocal : reste de p après le préfixe local base (séparateurs « / »), ok si
// p est base ou en dessous.
func cutLocal(p, base string) (string, bool) {
	if winPathForm(base) {
		pn := strings.ReplaceAll(p, "/", `\`)
		bn := strings.ReplaceAll(base, "/", `\`)
		bn = strings.TrimRight(bn, `\`)
		if len(pn) < len(bn) || !strings.EqualFold(pn[:len(bn)], bn) {
			return "", false
		}
		rest := pn[len(bn):]
		if rest != "" && rest[0] != '\\' {
			return "", false
		}
		return strings.ReplaceAll(strings.Trim(rest, `\`), `\`, "/"), true
	}
	if base == "/" {
		return strings.Trim(p, "/"), strings.HasPrefix(p, "/")
	}
	if p == base {
		return "", true
	}
	if strings.HasPrefix(p, base+"/") {
		return strings.Trim(p[len(base):], "/"), true
	}
	return "", false
}

func joinSlash(base, rest string) string {
	if rest == "" {
		return base
	}
	return strings.TrimRight(base, "/") + "/" + rest
}

// ToRemote : chemin vu des backends ; ok faux (p rendu tel quel) hors de
// toute correspondance.
func (m PathMapper) ToRemote(p string) (string, bool) {
	for _, e := range m.byLocal {
		if rest, ok := cutLocal(p, e.Local); ok {
			return joinSlash(e.Remote, rest), true
		}
	}
	return p, false
}

// ToLocal : chemin local d'un chemin vu des backends ; ok faux (p rendu tel
// quel) hors de toute correspondance.
func (m PathMapper) ToLocal(p string) (string, bool) {
	for _, e := range m.byRemote {
		rest, ok := cutLocal(p, e.Remote)
		if !ok {
			continue
		}
		if !winPathForm(e.Local) {
			return joinSlash(e.Local, rest), true
		}
		if rest == "" {
			return e.Local, true
		}
		return strings.TrimRight(e.Local, `\`) + `\` + strings.ReplaceAll(rest, "/", `\`), true
	}
	return p, false
}

// remoteChecked : chemin vu des backends, ou erreur s'il ne peut pas y avoir
// de sens. Sous Windows un chemin hors correspondance (C:\…, \\…) n'existe
// pas côté Linux ; ailleurs, un chemin non traduit est transmis tel quel.
func (m PathMapper) remoteChecked(p, goos, machine string) (string, error) {
	r, ok := m.ToRemote(p)
	if ok || goos != "windows" || strings.HasPrefix(p, "/") {
		return r, nil
	}
	return p, fmt.Errorf("%s : aucune correspondance Linux — déclarer hubs.%s.paths: [{ local: '…', remote: /… }] dans %s", p, machine, BackendsPath())
}

// ── Réglages de la machine (hubs.<machine>), relus si backends.yaml change ──

type hubCacheEntry struct {
	key     string // source : chemin de backends.yaml, machine, date et taille du fichier
	loaded  bool
	paths   PathMapper
	src     string
	warns   []string
	skipped int // entrées de paths d'un autre système (pathsForOS)
	user    *RemoteUserConf
	userSrc string // hubs.<machine>.remote_user
}

// hubCache : réglages lus ; cheap/checked : contrôle du fichier (stat) au plus
// une fois par hubRecheck tant que l'environnement qui désigne le dossier de
// config et la machine ne changent pas. Appelé sur des chemins chauds (CtrName,
// ReadAssignments, ExecArgv…), parfois sur un stockage réseau (MooseFS).
var hubCache struct {
	sync.Mutex
	e       hubCacheEntry
	cheap   string
	checked time.Time
}

var hubRecheck = time.Second

// pathsGOOS : système pour lequel les entrées de hubs.<machine>.paths sont
// retenues (pathsForOS) ; "" : toutes (tests qui simulent Windows sous Linux).
var pathsGOOS = runtime.GOOS

// hubLoads : lectures de backends.yaml par hubSettings (tests).
var hubLoads int

// hubCheapKey : ce qui désigne, sans accès disque, le backends.yaml lu.
func hubCheapKey() string {
	k := hubName() + "|" + pathsGOOS
	for _, v := range []string{"CL_CONFIG_DIR", "HOME", "USERPROFILE", "APPDATA"} {
		k += "|" + os.Getenv(v)
	}
	return k
}

// hubSettings : correspondance et utilisateur distant de cette machine. Le
// fichier n'est relu que si sa date, sa taille, son chemin ou la machine
// changent ; ceux-ci sont contrôlés au plus une fois par seconde.
func hubSettings() hubCacheEntry {
	cheap := hubCheapKey()
	hubCache.Lock()
	defer hubCache.Unlock()
	if hubCache.e.loaded && hubCache.cheap == cheap && time.Since(hubCache.checked) < hubRecheck {
		return hubCache.e
	}
	path, host := BackendsPath(), hubName()
	key := hubFileKey(path, host)
	hubCache.cheap, hubCache.checked = cheap, time.Now()
	if hubCache.e.loaded && hubCache.e.key == key {
		return hubCache.e
	}
	hubLoads++
	e := resolveHubSettingsFor(readBackendsFile(), host, pathsGOOS)
	e.key, e.loaded = key, true
	hubCache.e = e
	return e
}

// hubFileKey : identité de la source (chemin, machine, date, taille).
func hubFileKey(path, host string) string {
	key := path + "|" + host + "|" + pathsGOOS
	if st, err := os.Stat(path); err == nil {
		key += fmt.Sprintf("|%d|%d", st.ModTime().UnixNano(), st.Size())
	}
	return key
}

// resolveHubSettings : réglages de hubs.<machine> (logique pure), toutes les
// entrées de paths quelle que soit leur forme.
func resolveHubSettings(f backendsFile, host string) hubCacheEntry {
	return resolveHubSettingsFor(f, host, "")
}

// pathsForOS : entrées de paths qui s'appliquent sur goos ("" : toutes). Une
// configuration portable est partagée par un hub Linux (WSL) et l'exe Windows
// du même poste, qui portent le même nom de machine : une entrée dont local est
// de forme Windows (C:\…, \\serveur\…) ne vaut que sous Windows, une entrée
// dont local est Linux (/…) qu'ailleurs. Les entrées invalides sont gardées
// (NewPathMapper les signale).
func pathsForOS(ps []PathMap, goos string) (keep []PathMap, skipped int) {
	for _, p := range ps {
		l := strings.TrimSpace(p.Local)
		if goos != "" && (winPathForm(l) && goos != "windows" || strings.HasPrefix(l, "/") && goos == "windows") {
			skipped++
			continue
		}
		keep = append(keep, p)
	}
	return keep, skipped
}

// resolveHubSettingsFor : resolveHubSettings, entrées de paths filtrées pour
// goos (pathsForOS).
func resolveHubSettingsFor(f backendsFile, host, goos string) hubCacheEntry {
	return resolveHubSettingsOS(f, host, goos, goos)
}

// resolveHubSettingsOS : réglages de hubs.<machine> avec la surcharge
// hubs.<machine>.<ovGOOS> (hubos.go), entrées de paths filtrées pour filterGOOS.
func resolveHubSettingsOS(f backendsFile, host, ovGOOS, filterGOOS string) hubCacheEntry {
	var e hubCacheEntry
	if v := pickHubOS(f.Hubs, host, ovGOOS); v.ok {
		decl, src := v.paths()
		ps, skipped := pathsForOS(decl, filterGOOS)
		e.skipped = skipped
		if len(ps) > 0 {
			e.paths, e.warns = NewPathMapper(ps)
			e.src = src
		}
		e.user, e.userSrc = v.remoteUser()
	}
	return e
}

// HubPaths : correspondance de chemins de cette machine (vide : aucune), sa
// source et les entrées ignorées.
func HubPaths() (PathMapper, string, []string) {
	e := hubSettings()
	return e.paths, e.src, e.warns
}

// confLocalPath : chemin local d'un réglage-chemin de la configuration
// (config.conf : GITHUB_DIR(S), CLAUDE_CONFIG_DIR). Sous Windows, un chemin
// Linux absolu (/…) — configuration portable partagée avec un hub Linux — est
// un chemin vu des backends : « . » et « .. » résolus en style Linux, puis
// converti par la correspondance de la machine (il redevient le même chemin
// Linux quand il est transmis aux backends, cf. RemotePath). Sans
// correspondance : rendu tel quel en forme Linux, ok faux (à signaler, jamais
// « \share\… », relatif au lecteur courant). Ailleurs : p inchangé.
func confLocalPath(goos string, m PathMapper, p string) (string, bool) {
	if goos != "windows" || !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") {
		return p, true
	}
	c := path.Clean(p)
	if l, ok := m.ToLocal(c); ok {
		return l, true
	}
	return c, false
}

// RemotePath : chemin transmis aux backends pour le chemin local p.
func RemotePath(p string) string {
	r, _ := hubSettings().paths.ToRemote(p)
	return r
}

// LocalPath : chemin local d'un chemin reçu d'un backend (label cl.project…).
func LocalPath(p string) string {
	l, _ := hubSettings().paths.ToLocal(p)
	return l
}

// ── Chemin vu par claude ─────────────────────────────────────────────────
//
// claude range conversations (~/.claude/projects/<chemin encodé>), teams (cwd)
// et marqueur de sortie propre d'après SON répertoire de travail : chemin des
// backends pour un projet en conteneur, chemin local pour un projet de l'hôte
// (hub Linux à correspondance déclarée). Le launcher doit chercher au même nom.

// claudePathFor : logique pure.
func claudePathFor(m PathMapper, proj string, inCtr bool) string {
	if !inCtr {
		return proj
	}
	r, _ := m.ToRemote(proj)
	return r
}

// ctrMapped : correspondance déclarée et projet en conteneur (sinon : rien à
// traduire ; sans correspondance, aucune lecture de l'affectation).
func ctrMapped(proj string) bool {
	if hubSettings().paths.Empty() {
		return false
	}
	_, ok := EngineOf(ProjectBackend(proj))
	return ok
}

// ClaudePath : chemin du projet tel que le voit claude.
func ClaudePath(proj string) string {
	return claudePathFor(hubSettings().paths, proj, ctrMapped(proj))
}

// RemotePathChecked : RemotePath, ou erreur explicite sous Windows pour un
// chemin sans correspondance (cf. remoteChecked).
func RemotePathChecked(p string) (string, error) {
	return hubSettings().paths.remoteChecked(p, runtime.GOOS, hubName())
}

// ── Utilisateur vu des conteneurs ─────────────────────────────────────────
//
// Les conteneurs tournent sous l'uid:gid de l'utilisateur (droits sur le
// stockage partagé), avec son HOME. Linux : ceux du launcher, inchangés.
// Windows n'a pas d'uid : hubs.<machine>.remote_user les donne, sinon
// 1000:1000, /home/<nom> (premier utilisateur d'une distribution Linux).

// RemoteUserConf : hubs.<machine>.remote_user.
type RemoteUserConf struct {
	UID  *int   `yaml:"uid"`
	GID  *int   `yaml:"gid"`
	Name string `yaml:"name"`
	Home string `yaml:"home"`
}

// RemoteUser : identité de l'utilisateur dans les conteneurs.
type RemoteUser struct {
	UID, GID   int
	Name, Home string
	Source     string // d'où elle vient (diag)
}

// localUser : utilisateur du launcher (valeurs du système, comme avant #110).
func localUser() RemoteUser {
	home, _ := os.UserHomeDir()
	name := "cl"
	if u, err := user.Current(); err == nil && u.Username != "" {
		name = u.Username
	}
	return RemoteUser{UID: os.Getuid(), GID: os.Getgid(), Name: name, Home: home, Source: "système"}
}

// unixName : nom d'utilisateur Windows (« DOMAINE\Nom ») réduit à un nom Unix.
func unixName(n string) string {
	if i := strings.LastIndexAny(n, `\/`); i >= 0 {
		n = n[i+1:]
	}
	var b strings.Builder
	for _, r := range strings.ToLower(n) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "cl"
	}
	return b.String()
}

// resolveRemoteUser : logique pure. Hors Windows sans déclaration : local tel quel.
func resolveRemoteUser(goos string, local RemoteUser, c *RemoteUserConf, src string) RemoteUser {
	if c == nil && goos != "windows" {
		return local
	}
	u := local
	if goos == "windows" {
		name := unixName(local.Name)
		u = RemoteUser{UID: 1000, GID: 1000, Name: name, Home: "/home/" + name, Source: "défaut Windows (1000:1000)"}
	}
	if c != nil {
		if c.UID != nil {
			u.UID = *c.UID
		}
		if c.GID != nil {
			u.GID = *c.GID
		}
		if c.Name != "" {
			u.Name = c.Name
			if c.Home == "" && goos == "windows" {
				u.Home = "/home/" + c.Name
			}
		}
		if c.Home != "" {
			u.Home = c.Home
		}
		u.Source = src
	}
	return u
}

// CtrUser : identité de l'utilisateur dans les conteneurs de cette machine.
func CtrUser() RemoteUser {
	e := hubSettings()
	return resolveRemoteUser(runtime.GOOS, localUser(), e.user, e.userSrc)
}

// windowsUnmapped : chemins Linux de la configuration (config.conf, racines
// partagées des backends) sans correspondance sur ce poste.
func windowsUnmapped() []string {
	out := append([]string(nil), LoadConfig().Unmapped...)
	m := hubSettings().paths
	seen := map[string]bool{}
	bs, _ := LoadBackends()
	for _, b := range bs {
		for _, r := range b.Shared {
			if _, ok := confLocalPath("windows", m, r); !ok && !seen[r] {
				seen[r] = true
				out = append(out, "shared "+r)
			}
		}
	}
	return out
}

// unmappedLines : avertissement unique pour les chemins Linux sans
// correspondance (logique pure), avec l'entrée de paths à déclarer.
func unmappedLines(machine, backendsPath string, unmapped []string) []string {
	if len(unmapped) == 0 {
		return nil
	}
	out := []string{"⚠ chemins Linux de la configuration sans correspondance sur ce poste :"}
	remote := ""
	for _, u := range unmapped {
		out = append(out, "    "+u)
		if remote == "" {
			if i := strings.Index(u, " /"); i >= 0 {
				remote = linuxPrefix(u[i+1:], 2)
			}
		}
	}
	if remote == "" {
		remote = "/share/MFS"
	}
	out = append(out, fmt.Sprintf("  → les déclarer une fois dans %s :", backendsPath),
		fmt.Sprintf("      hubs: { %s: { paths: [{ local: '\\\\wsl.localhost\\<distribution>%s', remote: %s }] } }", machine, strings.ReplaceAll(remote, "/", `\`), remote),
		"    (une entrée de forme Windows ne s'applique que sous Windows : le hub Linux du même nom n'est pas touché)")
	return out
}

// linuxPrefix : n premiers éléments d'un chemin Linux absolu (« /a/b »).
func linuxPrefix(p string, n int) string {
	parts := strings.Split(strings.Trim(path.Clean(p), "/"), "/")
	if len(parts) > n {
		parts = parts[:n]
	}
	return "/" + strings.Join(parts, "/")
}

// PrintHubPaths : section de « claude-launcher-ng2 backends » (rien sans
// déclaration, hors Windows).
func PrintHubPaths() {
	m, src, warns := HubPaths()
	if m.Empty() && len(warns) == 0 && runtime.GOOS != "windows" {
		return
	}
	fmt.Printf("\nChemins poste → backends (machine %s)", hubName())
	if src != "" {
		fmt.Printf(" — %s", src)
	}
	fmt.Println(" :")
	if m.Empty() {
		fmt.Printf("  aucun : déclarer hubs.%s.paths dans %s (ex. { local: '\\\\wsl.localhost\\Ubuntu\\share\\MFS', remote: /share/MFS })\n", hubName(), BackendsPath())
	}
	for _, e := range m.Entries() {
		fmt.Printf("  %s → %s\n", e.Local, e.Remote)
	}
	for _, w := range warns {
		fmt.Println("  ⚠ " + w)
	}
	if runtime.GOOS == "windows" {
		if n := hubSettings().skipped; n > 0 {
			fmt.Printf("  (%d entrée(s) de forme Linux ignorée(s) ici : elles valent pour le hub Linux du même nom)\n", n)
		}
		for _, l := range unmappedLines(hubName(), BackendsPath(), windowsUnmapped()) {
			fmt.Println("  " + l)
		}
		u := CtrUser()
		fmt.Printf("  utilisateur des conteneurs : %s (%d:%d, HOME=%s) — %s\n", u.Name, u.UID, u.GID, u.Home, u.Source)
	}
}
