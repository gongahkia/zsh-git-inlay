package daemon

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestStaleSocketRepairPreservesLiveAndRegularFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "run")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "daemon.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	listener.SetUnlinkOnClose(false)
	defer listener.Close()
	if changed, err := RepairStaleSocket(path); err != nil || changed {
		t.Fatalf("live socket was removed: %t %v", changed, err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if state, err := InspectSocket(path); err != nil || state != "stale" {
		t.Fatalf("stale socket: %s %v", state, err)
	}
	if changed, err := RepairStaleSocket(path); err != nil || !changed {
		t.Fatalf("stale socket not repaired: %t %v", changed, err)
	}
	if err := os.WriteFile(path, []byte("user file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if changed, err := RepairStaleSocket(path); err == nil || changed {
		t.Fatal("repair replaced regular file")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "user file" {
		t.Fatal("repair changed user file")
	}
}
