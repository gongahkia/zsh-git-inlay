//go:build unix

package daemon

import "syscall"

func syscallUmask077() int   { return syscall.Umask(0o077) }
func syscallUmask(value int) { syscall.Umask(value) }
