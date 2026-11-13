package core

// Target : un serveur tmux auquel on envoie des commandes (même syntaxe que la
// ligne de commande tmux). Ne couvre pas ce qui demande un terminal (attach).
// Implémentations : le tmux local du hub (ui/tmux), le tmux d'un conteneur par
// son canal de contrôle (ctl.go, CtrTarget).
type Target interface {
	Run(args ...string) error
	Output(args ...string) ([]byte, error)
}

// CtrTarget : tmux du conteneur ctr (StatefulSet sous k8s) d'un projet sur le
// backend be, sur le serveur server pour un groupe de serveurs (#61) : canal de
// contrôle, moteur et nom de la fenêtre des panes. ok faux si le backend est
// inconnu ou sans conteneur.
func CtrTarget(be, ctr, server string) (t Target, e Engine, win string, ok bool) {
	if be == "" || ctr == "" {
		return nil, Engine{}, "", false
	}
	b, ok := FindBackend(be)
	if !ok {
		return nil, Engine{}, "", false
	}
	e, ok = EngineOf(b.At(server)) // groupe de serveurs : celui du conteneur (#61)
	if !ok {
		return nil, Engine{}, "", false
	}
	return CtlFor(e, ctr), e, "=" + CtrSession + ":", true
}
