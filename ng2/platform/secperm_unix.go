//go:build !windows

package platform

// RestrictToUser : droits réservés à l'utilisateur. Sous Unix, ce sont les modes 0700/0600 posés à la création.
func RestrictToUser(path string, dir bool) error { return nil }
