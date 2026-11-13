//go:build linux || darwin

package platform

import (
	"golang.org/x/sys/unix"
)

// RawMode : équivalent de « stty -echo -icanon -isig -ixon -ixoff -iexten » ;
// renvoie la fonction de restauration.
func RawMode(fd int) (func(), error) {
	old, err := unix.IoctlGetTermios(fd, ioctlGetTermios)
	if err != nil {
		return nil, err
	}
	t := *old
	t.Lflag &^= unix.ECHO | unix.ICANON | unix.ISIG | unix.IEXTEN
	t.Iflag &^= unix.IXON | unix.IXOFF | unix.ICRNL
	t.Cc[unix.VMIN], t.Cc[unix.VTIME] = 1, 0
	if err := unix.IoctlSetTermios(fd, ioctlSetTermios, &t); err != nil {
		return nil, err
	}
	return func() { _ = unix.IoctlSetTermios(fd, ioctlSetTermios, old) }, nil
}
