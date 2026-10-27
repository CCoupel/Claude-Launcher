package main

import "golang.org/x/sys/unix"

// ioctls de lecture / écriture des attributs du terminal (cf. rawMode).
const (
	ioctlGetTermios = unix.TCGETS
	ioctlSetTermios = unix.TCSETS
)
