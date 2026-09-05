//go:build !unix

package daemon

func syscallUmask077() int { return 0 }
func syscallUmask(value int) {}
