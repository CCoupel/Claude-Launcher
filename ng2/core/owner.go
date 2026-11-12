package core

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"claude-launcher-ng2/platform"
)

// ── Propriétaire d'un projet en conteneur (#48) ───────────────────────────
//
// Plusieurs launchers (machines) peuvent atteindre le même conteneur. Un seul
// le pilote : son propriétaire, noté dans la session tmux du conteneur
// (@cl_owner = identifiant du launcher, @cl_owner_at = battement de cœur,
// entretenu par son layout-watch). Un autre launcher peut rejoindre en lecture
// seule (attach -r, sans mise en page) ou prendre la main (l'ancien
// propriétaire est détaché et son watcher s'arrête).

const ownerTTL = 20 * time.Second // au-delà, le propriétaire est tenu pour parti

// LauncherID : identité de ce launcher (machine, utilisateur, serveur du hub).
func LauncherID() string {
	return platform.HostName() + ":" + strconv.Itoa(os.Getuid()) + ":" + InnerSocket()
}

type OwnerInfo struct {
	Id string
	At time.Time
}

func (o OwnerInfo) Alive() bool { return o.Id != "" && time.Since(o.At) < ownerTTL }

func ParseOwner(s string) OwnerInfo {
	id, at, _ := strings.Cut(strings.TrimSpace(s), "|")
	n, _ := strconv.ParseInt(at, 10, 64)
	return OwnerInfo{id, time.Unix(n, 0)}
}

const OwnerFormat = "#{@cl_owner}|#{@cl_owner_at}"

func (e Engine) Owner(name string) OwnerInfo {
	s, _ := e.Output(name, TmuxIn("display-message", "-p", "-t", "="+CtrSession+":", OwnerFormat)...)
	return ParseOwner(s)
}

// claimOwner : ce launcher devient propriétaire de la session.
func (e Engine) claimOwner(name string) {
	_ = e.Exec(name, false, nil, nil, TmuxIn("set-option", "-t", "="+CtrSession+":", "@cl_owner", LauncherID(), ";",
		"set-option", "-t", "="+CtrSession+":", "@cl_owner_at", strconv.FormatInt(time.Now().Unix(), 10))...)
}

// TakeOver : prise de main — les clients en écriture (l'ancien propriétaire)
// passent en lecture seule (switch-client -r bascule le drapeau) : ils voient
// toujours la session ; son watcher, voyant le nouveau propriétaire, le signale.
func (e Engine) TakeOver(name string) {
	for _, c := range e.rwClients(name) {
		_ = e.Exec(name, false, nil, nil, TmuxIn("switch-client", "-c", c, "-r")...)
	}
	e.claimOwner(name)
}

// ClaimFree : prise de propriété sans autre propriétaire vivant (c'était nous,
// ou il est parti) : un client en écriture encore là est un fantôme — fenêtre du
// hub tuée, le pty restant ouvert côté conteneur — et il est détaché.
func (e Engine) ClaimFree(name string) {
	for _, c := range e.rwClients(name) {
		_ = e.Exec(name, false, nil, nil, TmuxIn("detach-client", "-t", c)...)
	}
	e.claimOwner(name)
}

// rwClients : clients en écriture de la session (hors canaux de contrôle).
func (e Engine) rwClients(name string) []string {
	s, _ := e.Output(name, TmuxIn("list-clients", "-t", "="+CtrSession+":", "-F", "#{client_name}|#{client_readonly}|#{client_control_mode}")...)
	var out []string
	for _, l := range strings.Split(s, "\n") {
		f := strings.Split(l, "|")
		if len(f) == 3 && f[0] != "" && f[1] != "1" && f[2] != "1" {
			out = append(out, f[0])
		}
	}
	return out
}

// OwnerChoice : la session a un autre propriétaire vivant — demander quoi faire
// (dans la fenêtre du projet). Renvoie "l" (lecture seule), "p" (prendre la
// main) ou "a" (annuler).
func OwnerChoice(o OwnerInfo) string {
	fmt.Printf("\n\033[1;33m⚠ projet ouvert par un autre launcher : %s (actif il y a %ds)\033[0m\n",
		o.Id, int(time.Since(o.At).Seconds()))
	fmt.Println("  l  rejoindre en lecture seule (partage d'écran, sans mise en page)")
	fmt.Println("  p  prendre la main (l'autre launcher est détaché)")
	fmt.Println("  a  annuler")
	rd := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("choix [l/p/a] : ")
		l, err := rd.ReadString('\n')
		if err != nil {
			return "a"
		}
		switch c := strings.ToLower(strings.TrimSpace(l)); c {
		case "l", "p", "a":
			return c
		}
	}
}

// ForeignOwner : le projet tourne dans un conteneur (sur un backend connu) dont
// la session a un autre propriétaire vivant ; "" sinon.
func ForeignOwner(proj string) string {
	if !HasEnv(proj) {
		return ""
	}
	for _, b := range CtrBackends() {
		e, _ := EngineOf(b)
		if len(DiagnoseCached(b)) > 0 {
			continue
		}
		fs, err := e.List()
		if err != nil {
			continue
		}
		for _, f := range fs {
			if f.Project != proj || !f.Running || !e.Of(f).HasSession(f.Name) {
				continue
			}
			if o := e.Of(f).Owner(f.Name); o.Alive() && o.Id != LauncherID() {
				return o.Id + " (" + b.Name + ")"
			}
		}
	}
	return ""
}
