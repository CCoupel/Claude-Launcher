//go:build !windows

package core

import (
	"os"
	"syscall"
)

func sigOf(fi os.FileInfo) FileSig {
	s := FileSig{Size: fi.Size(), MTime: fi.ModTime().UnixNano()}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		s.Dev, s.Ino = uint64(st.Dev), uint64(st.Ino)
	}
	return s
}
