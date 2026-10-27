package main

import "golang.org/x/sys/unix"

// ioctls de lecture / écriture des attributs du terminal (cf. rawMode).
const (
	ioctlGetTermios = unix.TIOCGETA
	ioctlSetTermios = unix.TIOCSETA
)
