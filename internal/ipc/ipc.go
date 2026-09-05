// Package ipc implements the small versioned local socket protocol.
package ipc

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

const (
	Version         = 1
	MaxRequestBytes = 16 * 1024
	MaxReplyBytes   = 64 * 1024
)

type Request struct {
	Version     int    `json:"version"`
	Operation   string `json:"operation"`
	CWD         string `json:"cwd,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Repository  string `json:"repository_id,omitempty"`
	Worktree    string `json:"worktree_id,omitempty"`
}

type Reply struct {
	Version int    `json:"version"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
	Payload []byte `json:"payload,omitempty"`
}

func WriteRequest(writer io.Writer, request Request) error { return write(writer, request, MaxRequestBytes) }
func ReadRequest(reader io.Reader) (Request, error) { var request Request; return request, read(reader, &request, MaxRequestBytes) }
func WriteReply(writer io.Writer, reply Reply) error { return write(writer, reply, MaxReplyBytes) }
func ReadReply(reader io.Reader) (Reply, error) { var reply Reply; return reply, read(reader, &reply, MaxReplyBytes) }

func write(writer io.Writer, value any, maximum int) error {
	encoded, err := json.Marshal(value)
	if err != nil { return fmt.Errorf("encode IPC: %w", err) }
	if len(encoded) > maximum { return fmt.Errorf("IPC message exceeds %d bytes", maximum) }
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(encoded)))
	if _, err = writer.Write(length[:]); err != nil { return err }
	_, err = writer.Write(encoded)
	return err
}

func read(reader io.Reader, destination any, maximum int) error {
	var length [4]byte
	if _, err := io.ReadFull(reader, length[:]); err != nil { return err }
	size := binary.BigEndian.Uint32(length[:])
	if size == 0 || size > uint32(maximum) { return fmt.Errorf("invalid IPC message size %d", size) }
	encoded := make([]byte, size)
	if _, err := io.ReadFull(reader, encoded); err != nil { return err }
	if err := json.Unmarshal(encoded, destination); err != nil { return fmt.Errorf("decode IPC: %w", err) }
	return nil
}
