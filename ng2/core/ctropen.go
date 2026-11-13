package core

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// ── Ouverture d'un projet en conteneur ────────────────────────────────────
//
// Commune à la coque tmux (ctr-attach, ui/tmux/ctr.go) et à l'interface native
// (ui/desktop) : la sortie va dans le terminal du projet (out), les questions
// (serveur d'un groupe, autre launcher propriétaire) y sont posées et la
// réponse lue dans in.
//  1. conteneur créé/démarré, bootstrap (idempotent : rapide s'il est à jour) ;
//  2. si le tmux du projet n'a pas de session : création, puis envoi du script
//     de démarrage (le même qu'en local : questions /init-project et reprise,
//     claude, marqueur de sortie propre) ;
//  3. propriétaire de la session (#48) : un seul launcher pilote.

// CtrOpen : projet en conteneur prêt, dans son tmux.
type CtrOpen struct {
	E        Engine // moteur (serveur du conteneur pour un groupe)
	Name     string // conteneur (pod sous k8s)
	ReadOnly bool   // un autre launcher a la main : lecture seule
	Canceled bool   // l'utilisateur a annulé (autre propriétaire)
}

// CtrOpenIO : terminal du projet et fonctions de l'interface.
type CtrOpenIO struct {
	Out   io.Writer
	In    *bufio.Reader
	Start func() []byte          // script de démarrage (vide : attendu jusqu'à 5 s)
	Size  func() (int, int)      // taille de la première fenêtre du tmux du projet
	Done  func()                 // appelée une fois la session prête (script consommé)
	Owner func(OwnerInfo) string // question du propriétaire (défaut : OwnerChoiceIO)
}

// OpenCtr : prépare le conteneur du projet proj et sa session tmux.
func OpenCtr(proj string, t CtrOpenIO) (CtrOpen, error) {
	out := t.Out
	b := ProjectBackend(proj)
	e, ok := EngineOf(b)
	if !ok {
		return CtrOpen{}, fmt.Errorf("%s : backend %s sans conteneur", proj, b.Name)
	}
	env, err := LoadEnvFull(proj)
	if err != nil {
		return CtrOpen{}, err
	}
	fmt.Fprintf(out, "\033[1m⬡ %s — %s (%s)\033[0m\n", baseOf(proj), b.Name, env.Image)
	if _, err := os.Stat(LauncherEnvPath()); err == nil {
		fmt.Fprintf(out, "  + bootstrap du launcher : %s\n", LauncherEnvPath())
	}
	if len(env.Local) > 0 {
		fmt.Fprintf(out, "  dossiers privés au conteneur : %s\n", strings.Join(env.Local, ", "))
	}
	if ps := Diagnose(b); len(ps) > 0 {
		var s strings.Builder
		for _, p := range ps {
			fmt.Fprintf(&s, "\n  ⚠ %s\n    → %s", p.What, p.Fix)
		}
		return CtrOpen{}, fmt.Errorf("backend %s inutilisable :%s", b.Name, s.String())
	}
	// groupe de serveurs (#61) : serveur du projet, choisi sous verrou s'il est nouveau
	e, unlock, err := e.Place(proj, Asker{Out: out, In: t.In})
	if err != nil {
		return CtrOpen{}, err
	}
	name, err := e.Ensure(proj, env, out)
	unlock()
	if err != nil {
		return CtrOpen{}, err
	}
	if err := e.Bootstrap(name, proj, env, out); err != nil {
		return CtrOpen{}, err
	}
	// cache de consommation commun (panneau milestone) : voir linkUsageCache
	_ = e.Exec(name, false, strings.NewReader(LinkUsageScript()), io.Discard, "sh", "-s")
	if l, ok := e.ReadLive(name); ok && !l.Claude {
		// tmux resté seul après un /exit (point bleu) : la session est recréée
		// pour que claude redémarre, au lieu de se rattacher à son shell
		fmt.Fprintln(out, "  claude n'est plus en marche dans la session : redémarrage")
		_ = e.Exec(name, false, nil, io.Discard, TmuxIn("kill-session", "-t", "="+CtrSession)...)
	}
	if !e.HasSession(name) {
		if err := e.OnStartTo(name, proj, env, out); err != nil {
			return CtrOpen{}, err
		}
		// le script de démarrage est écrit juste après la création de la fenêtre
		// (coque) : l'attendre (bootstrap déjà à jour = quelques dixièmes de s)
		var start []byte
		for i := 0; i < 50 && len(start) == 0; i++ {
			if start = t.Start(); len(start) == 0 {
				time.Sleep(100 * time.Millisecond)
			}
		}
		w, h := t.Size()
		if err := e.NewCtrSessionTo(name, proj, w, h, out); err != nil {
			return CtrOpen{}, err
		}
		if len(start) > 0 {
			_ = e.Exec(name, false, nil, out, TmuxIn("send-keys", "-t", "="+CtrSession+":", string(start), "Enter")...)
		}
	}
	if t.Done != nil {
		t.Done()
	}
	e.SetCtrBrowserPaths(name)
	// propriétaire (#48) : un seul launcher pilote la session
	res := CtrOpen{E: e, Name: name}
	if o := e.Owner(name); o.Alive() && o.Id != LauncherID() {
		ask := t.Owner
		if ask == nil {
			ask = func(o OwnerInfo) string { return OwnerChoiceIO(o, out, t.In) }
		}
		switch ask(o) {
		case "l":
			res.ReadOnly = true
		case "p":
			e.TakeOver(name)
		default:
			res.Canceled = true
		}
	} else {
		e.ClaimFree(name)
	}
	return res, nil
}

func baseOf(p string) string { return p[strings.LastIndexAny(p, `/\`)+1:] }
