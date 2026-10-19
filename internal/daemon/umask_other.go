//go:build !unix

package daemon

func syscallSocketUmask() int { return 0 }
func syscallUmask(value int)  {}
