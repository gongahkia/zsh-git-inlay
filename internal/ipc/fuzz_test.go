package ipc

import (
	"bytes"
	"testing"
)

func FuzzReadFrames(f *testing.F) {
	f.Add([]byte{0, 0, 0, 2, '{', '}'})
	f.Add([]byte{0, 0, 0, 10, '{', '}'})
	f.Add([]byte{255, 255, 255, 255})
	f.Fuzz(func(t *testing.T, frame []byte) {
		if len(frame) > MaxReplyBytes+4 {
			return
		}
		_, _ = ReadRequest(bytes.NewReader(frame))
		_, _ = ReadReply(bytes.NewReader(frame))
	})
}
