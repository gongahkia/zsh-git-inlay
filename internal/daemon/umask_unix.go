//go:build unix

package daemon

import "syscall"

// A Unix socket starts at 0777; mask owner execute too so it is born 0600.
func syscallSocketUmask() int { return syscall.Umask(0o177) }
func syscallUmask(value int)  { syscall.Umask(value) }
