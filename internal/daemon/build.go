package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"

	"github.com/gongahkia/zsh-git-inlay/internal/ipc"
	"github.com/gongahkia/zsh-git-inlay/internal/setup"
)

type Build struct {
	Version        string `json:"version"`
	Commit         string `json:"commit"`
	Protocol       int    `json:"protocol"`
	PluginContract string `json:"plugin_contract"`
	Executable     string `json:"executable"`
	SHA256         string `json:"sha256"`
}

// CurrentBuild is captured once at daemon startup, so replacing the executable
// on disk cannot make an old running daemon report the new binary's identity.
func CurrentBuild(version, commit string) Build {
	result := Build{Version: version, Commit: commit, Protocol: ipc.Version, PluginContract: setup.PluginContract}
	path, err := os.Executable()
	if err != nil {
		return result
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	result.Executable = path
	file, err := os.Open(path)
	if err != nil {
		return result
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 64<<20 {
		return result
	}
	hash := sha256.New()
	if count, err := io.Copy(hash, io.LimitReader(file, (64<<20)+1)); err == nil && count == info.Size() {
		result.SHA256 = hex.EncodeToString(hash.Sum(nil))
	}
	return result
}
