package ipc

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestRoundTripAndVersionedPayload(t *testing.T) {
	var buffer bytes.Buffer
	want := Request{Version: Version, Operation: "lookup", Fingerprint: "abc"}
	if err := WriteRequest(&buffer, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRequest(&buffer)
	if err != nil || got != want {
		t.Fatalf("got %#v, err %v", got, err)
	}
}

func TestReadRejectsOversizedAndMalformedMessages(t *testing.T) {
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], MaxRequestBytes+1)
	if _, err := ReadRequest(bytes.NewReader(header[:])); err == nil {
		t.Fatal("oversized request accepted")
	}
	buffer := bytes.NewBuffer(nil)
	binary.BigEndian.PutUint32(header[:], 3)
	buffer.Write(header[:])
	buffer.WriteString("bad")
	if _, err := ReadRequest(buffer); err == nil {
		t.Fatal("malformed JSON accepted")
	}
}

func TestReadRejectsTruncatedFrame(t *testing.T) {
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], 10)
	buffer := bytes.NewBuffer(header[:])
	buffer.WriteString("{}")
	if _, err := ReadRequest(buffer); err == nil {
		t.Fatal("truncated request accepted")
	}
}
