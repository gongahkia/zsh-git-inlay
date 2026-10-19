package daemon

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// InspectSocket distinguishes a refused private socket from a transient dial
// failure. A timeout or permissions error never authorizes unlinking a daemon.
func InspectSocket(path string) (string, error) {
	state, _, err := inspectSocket(path)
	return state, err
}

func inspectSocket(path string) (string, os.FileInfo, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "missing", nil, nil
	}
	if err != nil {
		return "invalid", nil, err
	}
	parent, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		return "invalid", info, err
	}
	stat, owned := info.Sys().(*syscall.Stat_t)
	parentStat, parentOwned := parent.Sys().(*syscall.Stat_t)
	if info.Mode()&os.ModeSocket == 0 || !owned || int(stat.Uid) != os.Getuid() || !parent.IsDir() || parent.Mode().Perm()&0o077 != 0 || !parentOwned || int(parentStat.Uid) != os.Getuid() {
		return "invalid", info, fmt.Errorf("socket must be user-owned inside a private user-owned directory: %s", path)
	}
	connection, err := net.DialTimeout("unix", path, 30*time.Millisecond)
	if err == nil {
		connection.Close()
		return "running", info, nil
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return "stale", info, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return "missing", info, nil
	}
	return "unreachable", info, err
}

// RepairStaleSocket removes only a refused socket with unchanged inode and
// verified ownership/private parent. It never stops or unlinks a live daemon.
func RepairStaleSocket(path string) (bool, error) {
	state, before, err := inspectSocket(path)
	if err != nil || state != "stale" {
		return false, err
	}
	current, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !os.SameFile(before, current) {
		return false, errors.New("socket changed during repair; no file removed")
	}
	if err := os.Remove(path); err != nil {
		return false, err
	}
	return true, nil
}
