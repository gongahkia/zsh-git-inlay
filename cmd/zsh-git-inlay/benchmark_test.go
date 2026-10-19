package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// BenchmarkSuggestHelper includes a new helper process, exact index snapshot,
// Unix-socket lookup and shell composition against small and large real indexes.
func BenchmarkSuggestHelper(b *testing.B) {
	for _, files := range []int{10, 10000} {
		b.Run(fmt.Sprintf("index_%d", files), func(b *testing.B) {
			root, err := os.MkdirTemp("/tmp", "zgi-bench-")
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = os.RemoveAll(root) })
			binary := filepath.Join(root, "zsh-git-inlay")
			cmd := exec.Command("go", "build", "-o", binary, ".")
			if output, err := cmd.CombinedOutput(); err != nil {
				b.Fatalf("build helper: %s %v", output, err)
			}
			b.Setenv("ZSH_GIT_INLAY_RUNTIME_DIR", filepath.Join(root, "run"))
			b.Setenv("ZSH_GIT_INLAY_CACHE_DIR", filepath.Join(root, "cache"))
			b.Setenv("ZSH_GIT_INLAY_DATA_DIR", filepath.Join(root, "data"))
			b.Setenv("ZSH_GIT_INLAY_CONFIG", filepath.Join(root, "absent.toml"))
			git := func(args ...string) {
				b.Helper()
				cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
				if output, err := cmd.CombinedOutput(); err != nil {
					b.Fatalf("git: %s %v", output, err)
				}
			}
			git("init", "-q", "-b", "main")
			git("config", "user.name", "Benchmark")
			git("config", "user.email", "benchmark@example.invalid")
			repo := filepath.Join(root, "files")
			if err := os.Mkdir(repo, 0700); err != nil {
				b.Fatal(err)
			}
			for index := 0; index < files; index++ {
				if err := os.WriteFile(filepath.Join(repo, fmt.Sprintf("file-%05d.txt", index)), []byte("baseline\n"), 0600); err != nil {
					b.Fatal(err)
				}
			}
			git("add", "files")
			git("commit", "-qm", "baseline")
			if err := os.WriteFile(filepath.Join(repo, "file-00000.txt"), []byte("staged update\n"), 0600); err != nil {
				b.Fatal(err)
			}
			git("add", "files/file-00000.txt")
			helper := func(args ...string) ([]byte, error) { return exec.Command(binary, args...).CombinedOutput() }
			b.Cleanup(func() { _, _ = helper("daemon", "stop") })
			if output, err := helper("observe", "--cwd", root); err != nil {
				b.Fatalf("observe: %s %v", output, err)
			}
			deadline := time.Now().Add(15 * time.Second)
			for {
				if _, err := helper("candidates", "--cwd", root); err == nil {
					break
				}
				if time.Now().After(deadline) {
					b.Fatal("preparation timed out")
				}
				time.Sleep(20 * time.Millisecond)
			}
			info, err := os.Stat(filepath.Join(root, ".git", "index"))
			if err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			for index := 0; index < b.N; index++ {
				output, err := helper("suggest", "--no-start", "--cwd", root, "--buffer", "git commit -m ")
				if err != nil || !strings.HasPrefix(string(output), "git commit -m '") {
					b.Fatalf("suggest: %s %v", output, err)
				}
			}
			b.ReportMetric(float64(info.Size()), "index_bytes")
		})
	}
}
