//go:build windows

package core

import "os"

func sigOf(fi os.FileInfo) FileSig { return FileSig{Size: fi.Size(), MTime: fi.ModTime().UnixNano()} }
