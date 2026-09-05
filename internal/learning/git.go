package learning

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

const maxGitMetadata = 32 * 1024

// Historical reads a bounded local commit-metadata sample and immediately
// reduces it to aggregate style statistics. No subject or body is persisted.
func Historical(ctx context.Context, cwd string) (Stats, error) {
	output, err := gitOutput(ctx, cwd, maxGitMetadata, "log", "-n", "64", "--format=%s%x00%b%x00")
	if err != nil {
		return Stats{}, err
	}
	parts := strings.Split(string(output), "\x00")
	stats := emptyStats()
	for index := 0; index+1 < len(parts); index += 2 {
		subject, body := parts[index], parts[index+1]
		if subject == "" || len(subject) > 200 || strings.ContainsAny(subject, "\r\n") {
			continue
		}
		add(&stats, Observation{Subject: subject, HasBody: strings.TrimSpace(body) != "", Origin: UserAuthored})
	}
	return stats, nil
}

// LatestCommit returns only the final local commit metadata needed to update a
// profile. Callers must reduce it immediately and must not persist the strings.
func LatestCommit(ctx context.Context, cwd string) (string, bool, error) {
	output, err := gitOutput(ctx, cwd, 4096, "log", "-1", "--format=%s%x00%b")
	if err != nil {
		return "", false, err
	}
	parts := strings.SplitN(string(output), "\x00", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "", false, fmt.Errorf("latest Git commit has no subject")
	}
	return parts[0], strings.TrimSpace(parts[1]) != "", nil
}

// RemoteHash derives a comparison value for explicit clone-sharing checks. It
// strips userinfo, queries, and fragments before hashing and never stores the
// remote value itself.
func RemoteHash(ctx context.Context, cwd string) (string, error) {
	output, err := gitOutput(ctx, cwd, 4096, "remote", "get-url", "origin")
	if err != nil {
		return "", err
	}
	remote := strings.TrimSpace(string(output))
	if remote == "" {
		return "", fmt.Errorf("origin remote is unavailable")
	}
	return RemoteDigest(remote), nil
}

func gitOutput(ctx context.Context, cwd string, maximum int64, arguments ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", cwd}, arguments...)...)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := command.Start(); err != nil {
		return nil, err
	}
	output, readErr := io.ReadAll(io.LimitReader(stdout, maximum+1))
	if len(output) > int(maximum) {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, fmt.Errorf("git metadata exceeds %d bytes", maximum)
	}
	waitErr := command.Wait()
	if readErr != nil {
		return nil, readErr
	}
	if waitErr != nil {
		return nil, fmt.Errorf("read Git metadata: %w", waitErr)
	}
	return output, nil
}
