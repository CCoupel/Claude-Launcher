//go:build linux || darwin

package platform

import "syscall"

// ExecBinary : remplace le processus courant par le binaire argv0 (execve).
func ExecBinary(argv0 string, argv []string, envv []string) error {
	return syscall.Exec(argv0, argv, envv)
}

// ReplaceExecutable : pose le nouveau binaire dst par write (rename atomique
// par-dessus l'ancien : possible même s'il tourne).
func ReplaceExecutable(dst string, write func(string) error) error { return write(dst) }

// CleanupOldExe : rien à nettoyer (pas de .old hors Windows).
func CleanupOldExe() {}

// ReplaceExecutableWithBackup : comme ReplaceExecutable, après avoir sauvegardé l'ancien binaire en
// <dst>.bak-v<version> (copie atomique ; version : celle du binaire qui tourne) ; une seule sauvegarde gardée (#218).
func ReplaceExecutableWithBackup(dst, version string, write func(string) error) error {
	return replaceWithCopyBackup(dst, version, write)
}
