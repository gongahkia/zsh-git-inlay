package activity

import (
	"encoding/json"
	"testing"
)

func FuzzEventWireFormat(f *testing.F) {
	f.Add([]byte(`{"schema_version":1,"repository_id":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","worktree_id":"fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210","source":"shell","kind":"git.index_changed","timestamp":"2026-09-05T00:00:00Z","sensitivity":"private"}`))
	f.Add([]byte(`{"schema_version":2,"unknown":true}`))
	store := New(DefaultSettings(), func() (Permissions, error) { return Permissions{Activity: true}, nil })
	f.Fuzz(func(t *testing.T, content []byte) {
		if len(content) > 16*1024 {
			return
		}
		var event Event
		if json.Unmarshal(content, &event) == nil {
			_ = store.Ingest(event)
		}
	})
}
