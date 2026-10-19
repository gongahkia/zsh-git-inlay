package main

import (
	"os"
	"runtime"
	"testing"

	"github.com/gongahkia/zsh-git-inlay/internal/testenv"
)

// Darwin's Unix socket path limit is 104 bytes. Go's long test names plus
// macOS's default /var/folders temporary root can exceed it before bind.
func TestMain(m *testing.M) {
	if runtime.GOOS == "darwin" {
		if err := os.Setenv("TMPDIR", "/tmp"); err != nil {
			panic(err)
		}
	}
	cleanup, err := testenv.Isolate()
	if err != nil {
		panic(err)
	}
	code := m.Run()
	cleanup()
	os.Exit(code)
}
