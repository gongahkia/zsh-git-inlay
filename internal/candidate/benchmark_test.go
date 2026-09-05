package candidate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func BenchmarkGenerate(b *testing.B) {
	repository := b.TempDir()
	benchmarkGit(b, repository, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repository, "file.txt"), []byte("staged\n"), 0o644); err != nil {
		b.Fatal(err)
	}
	benchmarkGit(b, repository, "add", "file.txt")
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := Generate(context.Background(), repository); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkGit(b *testing.B, cwd string, arguments ...string) {
	b.Helper()
	command := exec.Command("git", append([]string{"-C", cwd}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		b.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
}
