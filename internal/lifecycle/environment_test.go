package lifecycle

import (
	"github.com/gongahkia/zsh-git-inlay/internal/testenv"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	cleanup, err := testenv.Isolate()
	if err != nil {
		panic(err)
	}
	code := m.Run()
	cleanup()
	os.Exit(code)
}
