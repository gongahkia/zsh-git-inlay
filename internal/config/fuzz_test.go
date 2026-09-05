package config

import "testing"

func FuzzDeclarativeValues(f *testing.F) {
	f.Add("[commit]\ntypes = [\"fix\"]\n")
	f.Add("[provider]\nendpoint = \"https://example.invalid\"\n")
	f.Add("[commit]\nscope_paths = [\"../secret=api\"]\n")
	f.Fuzz(func(t *testing.T, content string) {
		if len(content) > 8192 {
			return
		}
		_, _, _ = parse(content, map[string]bool{
			"commit.types":       true,
			"commit.scopes":      true,
			"commit.scope_paths": true,
			"provider.name":      true,
		})
		_, _ = policyArray(content, "commit.types")
		_ = safeProviderModel(content)
	})
}
