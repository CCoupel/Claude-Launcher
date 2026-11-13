package desktop

import (
	"strconv"
	"strings"
)

// ── Taille de la fenêtre tmux quand plusieurs clients sont attachés (#126) ─
//
// Un projet peut être affiché à la fois par l'interface native (client de
// contrôle) et par un autre client tmux (le hub Linux, un terminal), d'une
// autre taille. Avec window-size « latest » (défaut de tmux), la fenêtre prend
// la taille du dernier client actif : elle passerait d'une taille à l'autre à
// chaque frappe ou rafraîchissement d'un côté, et l'affichage des deux serait
// cassé. Règle : la taille est celle du client qui pilote.
//   - L'interface pilote (propriétaire, ou projet sur l'hôte) : les autres
//     clients reçoivent le drapeau ignore-size (tmux ≥ 3.2) ; ils voient la
//     fenêtre à la taille de l'interface, sans la changer.
//   - Un autre launcher a la main (lecture seule) : l'interface prend
//     ignore-size pour elle-même et rend ce drapeau aux clients qu'elle avait
//     marqués ; la fenêtre garde la taille du pilote, l'interface l'affiche
//     réduite si besoin.
// Jamais de redimensionnement en réponse à %layout-change : seule la taille de
// la zone d'affichage (anti-rebond) envoie refresh-client -C.

// tmuxAtLeast : version de tmux (« 3.3a », « next-3.4 ») ≥ maj.min.
func tmuxAtLeast(v string, maj, min int) bool {
	v = strings.TrimPrefix(strings.TrimSpace(v), "next-")
	a, b, _ := strings.Cut(v, ".")
	ma, err := strconv.Atoi(a)
	if err != nil {
		return false
	}
	end := 0
	for end < len(b) && b[end] >= '0' && b[end] <= '9' {
		end++
	}
	mi, _ := strconv.Atoi(b[:end])
	return ma > maj || ma == maj && mi >= min
}

// otherClient : un client tmux de la session autre que celui de l'interface.
type otherClient struct {
	name       string
	ignoreSize bool
}

// parseClients : sortie de list-clients -F "#{client_name}|#{client_control_mode}|#{client_flags}|#{client_pid}" ;
// clients normaux seulement (les canaux de contrôle — le nôtre, celui de core
// en ignore-size — ne sont pas concernés).
func parseClients(out string) []otherClient {
	var cs []otherClient
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.SplitN(l, "|", 4)
		if len(f) < 3 || f[0] == "" || f[1] == "1" {
			continue
		}
		cs = append(cs, otherClient{name: f[0], ignoreSize: hasFlag(f[2], "ignore-size")})
	}
	return cs
}

func hasFlag(flags, f string) bool {
	for _, x := range strings.Split(flags, ",") {
		if strings.TrimSpace(x) == f {
			return true
		}
	}
	return false
}

// govern : applique la règle de taille ; appelé à l'attache, à chaque
// battement (5 s) et au passage en lecture seule. Sans effet avant tmux 3.2.
func (t *projTerm) govern() {
	c := t.cl()
	if c == nil {
		return
	}
	t.mu.Lock()
	ver, ro := t.tmuxVer, t.readOnly
	t.mu.Unlock()
	if !tmuxAtLeast(ver, 3, 2) {
		return
	}
	if ro {
		// un autre launcher pilote : l'interface ne compte plus pour la taille
		_ = c.Send("refresh-client", "-f", "ignore-size")
		t.releaseOthers()
		return
	}
	_ = c.Send("refresh-client", "-f", "!ignore-size")
	b, err := c.Output("list-clients", "-t", "="+t.sess, "-F", "#{client_name}|#{client_control_mode}|#{client_flags}|#{client_pid}")
	if err != nil {
		return
	}
	marked := false
	for _, o := range parseClients(string(b)) {
		if o.ignoreSize {
			continue
		}
		if c.Send("refresh-client", "-t", o.name, "-f", "ignore-size") == nil {
			marked = true
			t.mu.Lock()
			if t.flagged == nil {
				t.flagged = map[string]bool{}
			}
			t.flagged[o.name] = true
			t.mu.Unlock()
		}
	}
	if marked {
		// taille recalculée tout de suite : celle de l'interface
		t.mu.Lock()
		w, h := t.cols, t.rows
		t.mu.Unlock()
		if w > 0 && h > 0 {
			_ = c.Send("refresh-client", "-C", clientSize(w, h))
		}
	}
}

// releaseOthers : rend leur rôle dans la taille aux clients que l'interface
// avait marqués (lecture seule, détachement).
func (t *projTerm) releaseOthers() {
	c := t.cl()
	t.mu.Lock()
	names := make([]string, 0, len(t.flagged))
	for n := range t.flagged {
		names = append(names, n)
	}
	t.flagged = nil
	t.mu.Unlock()
	if c == nil {
		return
	}
	for _, n := range names {
		_ = c.Send("refresh-client", "-t", n, "-f", "!ignore-size")
	}
}
