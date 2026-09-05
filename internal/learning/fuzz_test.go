package learning

import (
	"encoding/json"
	"strings"
	"testing"
)

func FuzzRemoteNormalizationAndImport(f *testing.F) {
	f.Add("git@github.com:owner/repository.git", []byte(`{"schema_version":1,"enabled":true,"local_interactions":{"samples":0}}`))
	f.Add("https://user:secret@example.invalid/private.git?token=hidden#fragment", []byte(`{"schema_version":999}`))
	store, err := New(f.TempDir())
	if err != nil {
		f.Fatal(err)
	}
	repository := strings.Repeat("a", 64)
	f.Fuzz(func(t *testing.T, remote string, encoded []byte) {
		if len(remote) > 8192 || len(encoded) > 16*1024 {
			return
		}
		if normalized := NormalizeRemote("https://user:secret@example.invalid/repository.git?token=hidden#fragment"); normalized != "https://example.invalid/repository" {
			t.Fatalf("normalized remote retained credential material: %q", normalized)
		}
		_ = RemoteDigest(remote)
		var exported Export
		if json.Unmarshal(encoded, &exported) == nil {
			_, _ = store.Import(repository, exported)
		}
	})
}
