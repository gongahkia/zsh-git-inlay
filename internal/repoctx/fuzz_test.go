package repoctx

import "testing"

func FuzzRedactionAndBounds(f *testing.F) {
	f.Add("token=secret\nAuthorization: Bearer hidden\n")
	f.Add("-----BEGIN PRIVATE KEY-----\nsecret\n")
	f.Fuzz(func(t *testing.T, value string) {
		if len(value) > 32*1024 {
			return
		}
		redacted, _ := redact(value)
		repeated, _ := redact(redacted)
		if repeated != redacted {
			t.Fatalf("redaction is not idempotent: first=%q repeated=%q", redacted, repeated)
		}
		for _, limit := range []int{0, 1, 16, 256, 4096} {
			if trimmed := trimBytes(value, limit); len(trimmed) > limit {
				t.Fatalf("trimmed length %d exceeds limit %d", len(trimmed), limit)
			}
		}
	})
}
