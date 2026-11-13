package platform

import (
	"os"
	"os/exec"
	"time"
)

// ExecBinary : pas d'execve sous Windows — le nouveau binaire est lancé (mêmes
// arguments, environnement, entrées/sorties) puis le processus courant se
// termine. Ne revient qu'en cas d'échec du lancement.
func ExecBinary(argv0 string, argv []string, envv []string) error {
	name, args := relaunchArgs(argv0, argv)
	c := exec.Command(name, args...)
	c.Env = envv
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}

// ReplaceExecutable : l'exe en cours d'exécution est renommé en .old, le
// nouveau écrit à sa place (cf. selfreplace.go).
func ReplaceExecutable(dst string, write func(string) error) error {
	return swapRunningExe(dst, write)
}

// CleanupOldExe : supprime, en tâche de fond, les .old laissés par une mise à
// jour ; l'ancien processus peut mettre un instant à se terminer après avoir
// lancé le nouveau : nouvel essai pendant une dizaine de secondes.
func CleanupOldExe() {
	exe, err := SelfExe()
	if err != nil || len(oldExes(exe)) == 0 {
		return
	}
	go func() {
		for i := 0; i < 20 && len(removeOldExes(exe)) > 0; i++ {
			time.Sleep(500 * time.Millisecond)
		}
	}()
}
