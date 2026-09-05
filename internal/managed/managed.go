// Package managed implements verified local runtime/model installation.
package managed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const SchemaVersion = "v1"

type Artifact struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type Manifest struct {
	SchemaVersion string   `json:"schema_version"`
	Version       string   `json:"version"`
	Platform      string   `json:"platform"`
	Runtime       Artifact `json:"runtime"`
	Model         Artifact `json:"model"`
	ModelName     string   `json:"model_name"`
	ModelLicense  string   `json:"model_license"`
	Source        string   `json:"source"`
}

type Installation struct {
	Manifest  Manifest  `json:"manifest"`
	Installed time.Time `json:"installed"`
}

type Status struct {
	DataDir       string         `json:"data_dir"`
	Current       string         `json:"current,omitempty"`
	Installations []Installation `json:"installations"`
	ManifestReady bool           `json:"manifest_ready"`
	Reason        string         `json:"reason,omitempty"`
}

type Manager struct {
	dataDir   string
	client    *http.Client
	allowHTTP bool // test-only; product manifests must use HTTPS.
}

func New(dataDir string) (*Manager, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dataDir, 0o700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dataDir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("managed data directory is unsafe: %s", dataDir)
	}
	return &Manager{dataDir: dataDir, client: &http.Client{Timeout: 2 * time.Minute}}, nil
}

func Platform() string { return runtime.GOOS + "-" + runtime.GOARCH }

func (manager *Manager) Install(ctx context.Context, manifest Manifest) (Installation, error) {
	if err := validateManifest(manifest, manager.allowHTTP); err != nil {
		return Installation{}, err
	}
	if manifest.Platform != Platform() {
		return Installation{}, fmt.Errorf("manifest platform %q does not match %q", manifest.Platform, Platform())
	}
	target := filepath.Join(manager.installationsDir(), manifest.Version)
	if info, err := os.Lstat(target); err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		return manager.readInstallation(target)
	}
	if err := os.MkdirAll(manager.downloadsDir(), 0o700); err != nil {
		return Installation{}, err
	}
	staging, err := os.MkdirTemp(manager.dataDir, ".install-")
	if err != nil {
		return Installation{}, err
	}
	defer os.RemoveAll(staging)
	if err := manager.fetch(ctx, manifest.Runtime, filepath.Join(staging, manifest.Runtime.Name)); err != nil {
		return Installation{}, err
	}
	if err := manager.fetch(ctx, manifest.Model, filepath.Join(staging, manifest.Model.Name)); err != nil {
		return Installation{}, err
	}
	installation := Installation{Manifest: manifest, Installed: time.Now().UTC()}
	encoded, err := json.Marshal(installation)
	if err != nil {
		return Installation{}, err
	}
	if err := writeAtomic(filepath.Join(staging, "installation.json"), encoded); err != nil {
		return Installation{}, err
	}
	if err := os.MkdirAll(manager.installationsDir(), 0o700); err != nil {
		return Installation{}, err
	}
	if err := os.Rename(staging, target); err != nil {
		return Installation{}, err
	}
	if err := manager.setCurrent(manifest.Version); err != nil {
		return Installation{}, err
	}
	return installation, nil
}

func (manager *Manager) Rollback(version string) error {
	if !safeVersion(version) {
		return errors.New("invalid managed version")
	}
	if _, err := manager.readInstallation(filepath.Join(manager.installationsDir(), version)); err != nil {
		return err
	}
	return manager.setCurrent(version)
}

func (manager *Manager) Uninstall(version string) error {
	if !safeVersion(version) {
		return errors.New("invalid managed version")
	}
	target := filepath.Join(manager.installationsDir(), version)
	info, err := os.Lstat(target)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("managed installation is unsafe")
	}
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	if current, _ := manager.current(); current == version {
		installations, _ := manager.installations()
		if len(installations) == 0 {
			return os.Remove(manager.currentPath())
		}
		return manager.setCurrent(installations[len(installations)-1].Manifest.Version)
	}
	return nil
}

func (manager *Manager) Status() (Status, error) {
	installations, err := manager.installations()
	if err != nil {
		return Status{}, err
	}
	current, _ := manager.current()
	return Status{DataDir: manager.dataDir, Current: current, Installations: installations, ManifestReady: false, Reason: "no authenticated runtime/model manifest is bundled"}, nil
}

func (manager *Manager) fetch(ctx context.Context, artifact Artifact, destination string) error {
	partial := filepath.Join(manager.downloadsDir(), artifact.SHA256+".part")
	offset := int64(0)
	if info, err := os.Stat(partial); err == nil {
		offset = info.Size()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, artifact.URL, nil)
	if err != nil {
		return err
	}
	if offset > 0 {
		request.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	response, err := manager.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if (offset == 0 && response.StatusCode != http.StatusOK) || (offset > 0 && response.StatusCode != http.StatusPartialContent) {
		return fmt.Errorf("download %s returned HTTP %d", artifact.Name, response.StatusCode)
	}
	file, err := os.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, io.LimitReader(response.Body, artifact.Size-offset+1))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if info, err := os.Stat(partial); err != nil || info.Size() != artifact.Size {
		return fmt.Errorf("downloaded %s has an unexpected size", artifact.Name)
	}
	if err := verify(partial, artifact.SHA256); err != nil {
		return err
	}
	if err := os.Rename(partial, destination); err != nil {
		return err
	}
	return os.Chmod(destination, 0o600)
}

func validateManifest(manifest Manifest, allowHTTP bool) error {
	if manifest.SchemaVersion != SchemaVersion || !safeVersion(manifest.Version) || manifest.Platform == "" || manifest.ModelName == "" || manifest.ModelLicense == "" || manifest.Source == "" {
		return errors.New("invalid managed manifest")
	}
	for _, artifact := range []Artifact{manifest.Runtime, manifest.Model} {
		parsed, err := url.Parse(artifact.URL)
		if err != nil || (parsed.Scheme != "https" && !(allowHTTP && parsed.Scheme == "http")) || parsed.Host == "" || !safeName(artifact.Name) || artifact.Size < 1 || len(artifact.SHA256) != 64 {
			return errors.New("invalid managed artifact")
		}
		if _, err := hex.DecodeString(artifact.SHA256); err != nil {
			return errors.New("invalid managed artifact checksum")
		}
	}
	return nil
}

func verify(path, expected string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return err
	}
	if hex.EncodeToString(sum.Sum(nil)) != expected {
		return errors.New("managed artifact checksum mismatch")
	}
	return nil
}

func (manager *Manager) installations() ([]Installation, error) {
	entries, err := os.ReadDir(manager.installationsDir())
	if os.IsNotExist(err) {
		return []Installation{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]Installation, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || !safeVersion(entry.Name()) {
			continue
		}
		installation, err := manager.readInstallation(filepath.Join(manager.installationsDir(), entry.Name()))
		if err == nil {
			result = append(result, installation)
		}
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Manifest.Version < result[right].Manifest.Version })
	return result, nil
}

func (manager *Manager) readInstallation(directory string) (Installation, error) {
	content, err := os.ReadFile(filepath.Join(directory, "installation.json"))
	if err != nil {
		return Installation{}, err
	}
	var installation Installation
	if err := json.Unmarshal(content, &installation); err != nil {
		return Installation{}, err
	}
	if err := validateManifest(installation.Manifest, manager.allowHTTP); err != nil {
		return Installation{}, err
	}
	return installation, nil
}

func (manager *Manager) setCurrent(version string) error {
	return writeAtomic(manager.currentPath(), []byte(version+"\n"))
}
func (manager *Manager) current() (string, error) {
	content, err := os.ReadFile(manager.currentPath())
	return strings.TrimSpace(string(content)), err
}
func (manager *Manager) installationsDir() string {
	return filepath.Join(manager.dataDir, "managed", "installations")
}
func (manager *Manager) downloadsDir() string {
	return filepath.Join(manager.dataDir, "managed", "downloads")
}
func (manager *Manager) currentPath() string {
	return filepath.Join(manager.dataDir, "managed", "current")
}

func writeAtomic(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".pending-")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err == nil {
		_, err = temporary.Write(content)
	}
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}

func safeName(value string) bool {
	return value != "" && value == filepath.Base(value) && !strings.Contains(value, "\x00")
}
func safeVersion(value string) bool {
	return safeName(value) && len(value) <= 80 && strings.Trim(value, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-") == ""
}
