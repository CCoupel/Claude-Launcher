//go:build windows

package platform

// ProcessAlive : sous Windows l'existence d'un processus n'est pas sondée ici : toujours vrai. Après un plantage,
// le témoin de présence d'un launcher n'est donc repris qu'une fois périmé (10 s sans battement).
func ProcessAlive(pid int) bool { return true }
