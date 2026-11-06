package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
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

// launcherID : identité de ce launcher (machine, utilisateur, serveur du hub).
func launcherID() string {
	return hostName() + ":" + strconv.Itoa(os.Getuid()) + ":" + innerSocket()
}

type ownerInfo struct {
	id string
	at time.Time
}

func (o ownerInfo) alive() bool { return o.id != "" && time.Since(o.at) < ownerTTL }

func parseOwner(s string) ownerInfo {
	id, at, _ := strings.Cut(strings.TrimSpace(s), "|")
	n, _ := strconv.ParseInt(at, 10, 64)
	return ownerInfo{id, time.Unix(n, 0)}
}

const ownerFormat = "#{@cl_owner}|#{@cl_owner_at}"

func (e Engine) owner(name string) ownerInfo {
	s, _ := e.output(name, tmuxIn("display-message", "-p", "-t", "="+ctrSession+":", ownerFormat)...)
	return parseOwner(s)
}

// claimOwner : ce launcher devient propriétaire de la session.
func (e Engine) claimOwner(name string) {
	_ = e.exec(name, false, nil, nil, tmuxIn("set-option", "-t", "="+ctrSession+":", "@cl_owner", launcherID(), ";",
		"set-option", "-t", "="+ctrSession+":", "@cl_owner_at", strconv.FormatInt(time.Now().Unix(), 10))...)
}

// takeOver : prise de main — les clients en écriture (l'ancien propriétaire)
// sont détachés ; les clients en lecture seule restent.
func (e Engine) takeOver(name string) {
	s, _ := e.output(name, tmuxIn("list-clients", "-t", "="+ctrSession+":", "-F", "#{client_name}|#{client_readonly}|#{client_control_mode}")...)
	for _, l := range strings.Split(s, "\n") {
		f := strings.Split(l, "|")
		if len(f) == 3 && f[0] != "" && f[1] != "1" && f[2] != "1" {
			_ = e.exec(name, false, nil, nil, tmuxIn("detach-client", "-t", f[0])...)
		}
	}
	e.claimOwner(name)
}

// ownerChoice : la session a un autre propriétaire vivant — demander quoi faire
// (dans la fenêtre du projet). Renvoie "l" (lecture seule), "p" (prendre la
// main) ou "a" (annuler).
func ownerChoice(o ownerInfo) string {
	fmt.Printf("\n\033[1;33m⚠ projet ouvert par un autre launcher : %s (actif il y a %ds)\033[0m\n",
		o.id, int(time.Since(o.at).Seconds()))
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

// heartbeat : appelé par layout-watch ; false si un autre launcher a pris la main.
func heartbeat(pw projWin) bool {
	s, err := tout(pw.t, "display-message", "-p", "-t", pw.win, ownerFormat)
	if err != nil {
		return true // canal momentanément indisponible : on ne conclut rien
	}
	if o := parseOwner(s); o.id != "" && o.id != launcherID() {
		return false
	}
	_ = pw.t.Run("set-option", "-t", "="+ctrSession+":", "@cl_owner_at", strconv.FormatInt(time.Now().Unix(), 10))
	return true
}

// foreignOwner : le projet tourne dans un conteneur (sur un backend connu) dont
// la session a un autre propriétaire vivant ; "" sinon.
func foreignOwner(proj string) string {
	if !hasEnv(proj) {
		return ""
	}
	for _, b := range ctrBackends() {
		e, _ := engineOf(b)
		if len(diagnoseCached(b)) > 0 {
			continue
		}
		fs, err := e.list()
		if err != nil {
			continue
		}
		for _, f := range fs {
			if f.Project != proj || !f.Running || !e.hasSession(f.Name) {
				continue
			}
			if o := e.owner(f.Name); o.alive() && o.id != launcherID() {
				return o.id + " (" + b.Name + ")"
			}
		}
	}
	return ""
}
