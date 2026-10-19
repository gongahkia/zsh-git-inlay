package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverInstalledPrefixAndDependencyOverride(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "bin", "zsh-git-inlay")
	plugin := filepath.Join(root, "share", "zsh-git-inlay", "zsh-git-inlay.plugin.zsh")
	dependency := filepath.Join(root, "dependency with spaces.zsh")
	for _, name := range []string{binary, plugin, dependency} {
		if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte("# fixture\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("ZSH_AUTOSUGGESTIONS_SOURCE", dependency)
	value := Discover(binary)
	canonicalPlugin, _ := filepath.EvalSymlinks(plugin)
	if value.Plugin != canonicalPlugin || value.Autosuggestions != dependency {
		t.Fatalf("installation=%#v", value)
	}
	if quote := Quote("a'b $(secret)"); quote != "'a'\\''b $(secret)'" || strings.Contains(quote, "\n") {
		t.Fatalf("unsafe quote %q", quote)
	}
}
