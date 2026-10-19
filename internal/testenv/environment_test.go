package testenv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsolationRejectsInheritedRepositoryOverridesAndRestoresEnvironment(t *testing.T) {
	inherited := map[string]string{
		"GIT_DIR": "/untrusted/repository", "GIT_WORK_TREE": "/untrusted/worktree",
		"GIT_INDEX_FILE": "/untrusted/index", "GIT_OBJECT_DIRECTORY": "/untrusted/objects",
		"GIT_CONFIG_GLOBAL": "/untrusted/gitconfig", "GIT_CONFIG_COUNT": "1",
		"GIT_CONFIG_KEY_0": "core.hooksPath", "GIT_CONFIG_VALUE_0": "/untrusted/hooks",
		"ZSH_GIT_INLAY_CONFIG": "/untrusted/preferences", "OPENAI_API_KEY": "fixture-only",
	}
	for key, value := range inherited {
		t.Setenv(key, value)
	}
	cleanup, err := Isolate()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	root := filepath.Dir(os.Getenv("HOME"))
	for key := range inherited {
		if key == "GIT_CONFIG_GLOBAL" {
			if os.Getenv(key) != os.DevNull {
				t.Fatalf("global Git configuration was not isolated")
			}
			continue
		}
		if _, present := os.LookupEnv(key); present {
			t.Fatalf("inherited override %s survived isolation", key)
		}
	}
	cleanup()
	for key, value := range inherited {
		if os.Getenv(key) != value {
			t.Fatalf("%s was not restored", key)
		}
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("temporary home was not removed: %v", err)
	}
}
