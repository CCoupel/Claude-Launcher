package core

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ── Rafraîchissement forcé et fermeture forcée d'un projet (#158) ─────────
// Commun à la coque tmux (ui/tmux) et à l'interface native (ui/desktop).

// ForceDiagnose : diagnostic du backend relu sans regarder le cache (une
// minute) ; la valeur relue le remplace.
func ForceDiagnose(b BackendConf) []Problem {
	ps := Diagnose(b)
	diagCache.Lock()
	diagCache.m[backendKey(b)] = diagEntry{time.Now(), ps}
	diagCache.Unlock()
	return ps
}

// ForgetTplInfo : oublie le registre d'agents et les compagnons gardés du
// projet (une minute) ; le prochain ReadTplInfo les relit.
func ForgetTplInfo(proj string) {
	tplCache.Lock()
	delete(tplCache.m, proj)
	tplCache.Unlock()
}

// ReadTplInfoForced : ReadTplInfo sans le cache du projet.
func ReadTplInfoForced(proj string) TplInfo {
	ForgetTplInfo(proj)
	return ReadTplInfo(proj)
}

// ReadCtrViewForced : ReadCtrView après un diagnostic forcé des backends
// affectés aux projets paths (ceux dont l'état de conteneur en dépend).
func ReadCtrViewForced(paths []string) CtrView {
	asg := ReadAssignments()
	seen := map[string]bool{}
	var todo []BackendConf
	add := func(name string) {
		if name == "" || name == "host" || seen[name] {
			return
		}
		seen[name] = true
		if b, ok := FindBackend(name); ok {
			todo = append(todo, b)
		}
	}
	for _, p := range paths {
		if name, ok := asg[p]; ok {
			add(name)
		} else if HasEnv(p) {
			add(ProjectBackend(p).Name)
		}
	}
	var wg sync.WaitGroup
	for _, b := range todo {
		wg.Add(1)
		go func(b BackendConf) { defer wg.Done(); ForceDiagnose(b) }(b)
	}
	wg.Wait()
	return ReadCtrView(paths)
}

// FetchUsageForced : relit la consommation sans attendre la péremption du
// cache. Le délai minimal après un échec de l'API (usageMinBackoff, 429
// compris) est conservé : dans ce délai, rien n'est demandé. Rend vrai si
// l'API a été interrogée.
func FetchUsageForced(refreshSec int) bool {
	if usageBackedOff(refreshSec) {
		return false
	}
	FetchUsage(refreshSec)
	return true
}

// usageBackedOff : un échec récent suspend les appels (même règle que
// NeedsFetch, sans le test de fraîcheur du cache).
func usageBackedOff(refreshSec int) bool {
	path := usageCachePath()
	interval := time.Duration(refreshSec) * time.Second
	backoff := 2 * interval
	if backoff < usageMinBackoff {
		backoff = usageMinBackoff
	}
	st, err := os.Stat(path + ".err")
	if err != nil || time.Since(st.ModTime()) >= backoff {
		return false
	}
	b, _ := os.ReadFile(path + ".err")
	cs, cerr := os.Stat(credentialsPath())
	return strings.HasPrefix(string(b), "trop d") || cerr != nil || !cs.ModTime().After(st.ModTime())
}

// ── Fermeture forcée proposable ───────────────────────────────────────────

// ForceInput : ce qu'on sait du projet pour décider d'une fermeture forcée.
type ForceInput struct {
	P        ProjInfo // état de la ligne (Dot calculé)
	Tab      bool     // un onglet (interface native) ou une fenêtre (coque) est ouvert
	K8s      bool     // backend du projet : k8s (« pod ») ou non (« conteneur »)
	HostGone bool     // projet sur l'hôte : sa session tmux locale n'existe plus ou ne répond pas
	// Unreliable : la lecture du backend n'a pas abouti sans erreur (CtrUnreliable) ;
	// l'état « conteneur absent » n'est alors qu'inconnu : rien n'est proposé
	Unreliable string
}

// CtrUnreliable : pourquoi l'état du conteneur du projet (backend be) n'est pas
// fiable — diagnostic en défaut, listing en erreur, serveurs du groupe
// injoignables — ou "" si la lecture a abouti sans erreur.
func CtrUnreliable(be string, errs, down map[string]string, diag map[string][]Problem) string {
	if be == "" || be == "host" {
		return ""
	}
	switch {
	case len(diag[be]) > 0:
		return "backend " + be + " en défaut : " + diag[be][0].What
	case errs[be] != "":
		return "backend " + be + " : " + errs[be]
	case down[be] != "":
		return "backend " + be + " : serveurs injoignables (" + down[be] + ")"
	}
	return ""
}

// ContainerProject : le projet s'exécute dans un conteneur ou un pod (env.yaml,
// ou backend affecté autre que l'hôte).
func ContainerProject(p ProjInfo) bool { return p.HasEnv || p.Backend != "" && p.Backend != "host" }

// ForceCloseable dit si la fermeture forcée est proposable, et pourquoi.
// Proposée quand le projet est affiché ouvert ou en erreur (onglet, ✕, ▲ ou ●
// orange) alors que son conteneur, son pod ou sa session n'existe plus ou ne
// répond pas. Jamais quand un autre launcher vivant pilote le projet : refused
// dit alors quoi faire. why : la raison, pour la boîte de confirmation.
func ForceCloseable(in ForceInput) (ok bool, why, refused string) {
	p := in.P
	if o := p.Foreign(); o != "" || p.Dot == SessReadOnly || p.Dot == SessElse {
		if o == "" {
			o = p.Live.Owner.Id
		}
		return false, "", "piloté par " + o + " — prendre la main d'abord"
	}
	if in.Unreliable != "" {
		return false, "", "" // lecture non fiable : « inconnu », jamais « absent »
	}
	shown := in.Tab || p.Open
	switch p.Dot {
	case SessAnomaly, SessHere, SessOrphan:
		shown = true
	}
	if !shown || p.HasLive {
		return false, "", "" // affiché fermé, ou une session vit encore : rien à forcer
	}
	unit := "conteneur"
	if in.K8s {
		unit = "pod k8s"
	}
	ctr := ContainerProject(p)
	switch {
	case ctr && !p.CtrKnown && len(p.Problems) == 0:
		return true, "son " + unit + " n'existe plus", ""
	case len(p.Problems) > 0: // diagnostic en défaut : les appelants passent aussi Unreliable, qui prime
		return true, "son backend " + p.Backend + " ne répond pas", ""
	case p.CtrKnown && p.Ctr.Failed:
		return true, "son " + unit + " est en échec", ""
	case p.CtrKnown && !p.Ctr.Running:
		return true, "son " + unit + " est arrêté", ""
	case ctr && p.CtrKnown && p.Ctr.Running && !p.HasLive:
		return true, "sa session tmux n'existe plus dans le " + unit, ""
	case !ctr && in.HostGone:
		return true, "sa session tmux locale n'existe plus", ""
	case p.HasSess && !ctr:
		return true, "sa reprise est interrompue", ""
	}
	return false, "", ""
}

// ForceItems : ce que la fermeture forcée nettoiera, pour la confirmation.
func ForceItems(in ForceInput) []string {
	var it []string
	if in.Tab {
		it = append(it, "onglet")
	}
	if !ContainerProject(in.P) {
		it = append(it, "session tmux locale")
	}
	it = append(it, "panes cachés", "marque de sortie propre (reprise abandonnée)")
	if in.P.CtrKnown {
		if in.K8s {
			it = append(it, "pod k8s (arrêt demandé)")
		} else {
			it = append(it, "conteneur (arrêt demandé)")
		}
	}
	return it
}

// ── Nettoyage commun ──────────────────────────────────────────────────────

// CleanupResult : ce que le nettoyage a effectivement fait.
type CleanupResult struct {
	Hidden int  // panes cachés orphelins tués
	Marker bool // marque de sortie propre posée
	Lock   bool // verrou de création supprimé
	Orders int  // fichiers d'ordre supprimés
}

// MarkCleanExit pose le marqueur de sortie propre du projet : la reprise (↺)
// n'est plus proposée, la conversation reste lisible par /resume.
func MarkCleanExit(path string) error {
	m := CleanExitMarker(path)
	if err := os.MkdirAll(filepath.Dir(m), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(m, nil, 0o644); err != nil {
		return err
	}
	now := time.Now()
	return os.Chtimes(m, now, now)
}

// ReleaseStaleLock supprime le verrou de création du projet s'il est périmé ou
// posé par ce launcher ; celui d'un autre launcher vivant reste. Rend vrai si
// supprimé.
func ReleaseStaleLock(proj string) bool {
	p := lockPath(proj)
	st, err := os.Stat(p)
	if err != nil {
		return false
	}
	b, _ := os.ReadFile(p)
	ours := strings.HasPrefix(string(b), LauncherID()+" ")
	if !ours && time.Since(st.ModTime()) <= lockStale {
		return false
	}
	return os.Remove(p) == nil
}

// CleanupProject : état laissé par un projet dont la session ou le conteneur a
// disparu — panes cachés orphelins (t : tmux de l'hôte, nil si aucun), marque
// de sortie propre, verrou de création périmé ou à nous, fichiers d'ordre des
// panes. projects.json n'est jamais touché : il ne porte aucun état d'ouverture.
func CleanupProject(path string, t SessionTmux, orderFiles ...string) CleanupResult {
	var r CleanupResult
	if t != nil {
		r.Hidden = KillOrphanHidden(t)
	}
	r.Marker = MarkCleanExit(path) == nil
	r.Lock = ReleaseStaleLock(path)
	for _, f := range orderFiles {
		if f != "" && os.Remove(f) == nil {
			r.Orders++
		}
	}
	return r
}
