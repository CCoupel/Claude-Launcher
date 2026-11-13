package core

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Shells : commandes d'un pane revenu à son shell (claude terminé).
var Shells = map[string]bool{"bash": true, "sh": true, "zsh": true, "dash": true, "fish": true, "ash": true}

// LeaderOf : dans des lignes « %id commande », le plus petit %id et sa commande.
func LeaderOf(s string) (id, cmd string) {
	best := -1
	for _, l := range strings.Split(s, "\n") {
		f := strings.Fields(l)
		if len(f) < 1 || !strings.HasPrefix(f[0], "%") {
			continue
		}
		n, err := strconv.Atoi(f[0][1:])
		if err != nil || best >= 0 && n >= best {
			continue
		}
		best, id, cmd = n, f[0], ""
		if len(f) > 1 {
			cmd = f[1]
		}
	}
	return id, cmd
}

// Stop : arrêt du conteneur du projet (k8s : 0 réplique, le pod est libéré ;
// tout est sur le stockage partagé, l'ouverture suivante le remet à 1).
func (e Engine) Stop(proj, name string) error {
	var c *exec.Cmd
	if e.B.K8s() {
		// nom réel (pod « <statefulset>-0 ») : un StatefulSet d'avant v5.6 garde le sien
		c = e.kcmd("scale", "statefulset", strings.TrimSuffix(name, "-0"), "--replicas=0")
	} else {
		c = e.cmd("stop", "-t", "5", name)
	}
	if out, err := c.CombinedOutput(); err != nil {
		return fmt.Errorf("arrêt du conteneur : %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}
