package managed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallRollbackAndUninstallUseOnlyManagedAssets(t *testing.T) {
	runtimeBytes, modelBytes := []byte("runtime"), []byte("model")
	server := fixtureServer(t, runtimeBytes, modelBytes)
	defer server.Close()
	manager, err := New(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	manager.allowHTTP = true
	first := fixtureManifest(server.URL, "v1", runtimeBytes, modelBytes)
	if _, err := manager.Install(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	second := fixtureManifest(server.URL, "v2", runtimeBytes, modelBytes)
	if _, err := manager.Install(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	if err := manager.Rollback("v1"); err != nil {
		t.Fatal(err)
	}
	status, err := manager.Status()
	if err != nil || status.Current != "v1" || len(status.Installations) != 2 {
		t.Fatalf("status=%#v err=%v", status, err)
	}
	unrelated := filepath.Join(manager.dataDir, "unrelated")
	if err := os.WriteFile(unrelated, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := manager.Uninstall("v1"); err != nil {
		t.Fatal(err)
	}
	status, err = manager.Status()
	if err != nil || status.Current != "v2" || len(status.Installations) != 1 {
		t.Fatalf("status after uninstall=%#v err=%v", status, err)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("unrelated data removed: %v", err)
	}
}

func TestInstallRejectsChecksumMismatchAndHTTPInProduction(t *testing.T) {
	bytes := []byte("artifact")
	server := fixtureServer(t, bytes, bytes)
	defer server.Close()
	manager, err := New(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := fixtureManifest(server.URL, "v1", bytes, bytes)
	if _, err := manager.Install(context.Background(), manifest); err == nil {
		t.Fatal("HTTP manifest accepted outside test mode")
	}
	manager.allowHTTP = true
	manifest.Runtime.SHA256 = stringsOf('0', 64)
	if _, err := manager.Install(context.Background(), manifest); err == nil {
		t.Fatal("checksum mismatch accepted")
	}
	if _, err := os.Stat(filepath.Join(manager.installationsDir(), "v1")); !os.IsNotExist(err) {
		t.Fatalf("partial installation became active: %v", err)
	}
}

func TestInstallResumesVerifiedPartialArtifact(t *testing.T) {
	runtimeBytes, modelBytes := []byte("runtime-data"), []byte("model-data")
	server := fixtureServer(t, runtimeBytes, modelBytes)
	defer server.Close()
	manager, err := New(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	manager.allowHTTP = true
	manifest := fixtureManifest(server.URL, "v1", runtimeBytes, modelBytes)
	if err := os.MkdirAll(manager.downloadsDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	partial := filepath.Join(manager.downloadsDir(), manifest.Runtime.SHA256+".part")
	if err := os.WriteFile(partial, runtimeBytes[:3], 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Install(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	installed, err := os.ReadFile(filepath.Join(manager.installationsDir(), "v1", manifest.Runtime.Name))
	if err != nil || string(installed) != string(runtimeBytes) {
		t.Fatalf("resumed runtime=%q err=%v", installed, err)
	}
}

func TestManifestRejectsUnsafeInputs(t *testing.T) {
	manifest := Manifest{SchemaVersion: SchemaVersion, Version: "../bad", Platform: Platform(), ModelName: "model", ModelLicense: "license", Source: "source", Runtime: Artifact{Name: "runtime", URL: "https://example.invalid/runtime", SHA256: stringsOf('a', 64), Size: 1}, Model: Artifact{Name: "model", URL: "https://example.invalid/model", SHA256: stringsOf('b', 64), Size: 1}}
	if err := validateManifest(manifest, false); err == nil {
		t.Fatal("unsafe manifest accepted")
	}
}

func fixtureManifest(base, version string, runtimeBytes, modelBytes []byte) Manifest {
	return Manifest{SchemaVersion: SchemaVersion, Version: version, Platform: Platform(), ModelName: "fixture", ModelLicense: "test", Source: "fixture", Runtime: Artifact{Name: "runtime.bin", URL: base + "/runtime", SHA256: digest(runtimeBytes), Size: int64(len(runtimeBytes))}, Model: Artifact{Name: "model.gguf", URL: base + "/model", SHA256: digest(modelBytes), Size: int64(len(modelBytes))}}
}

func fixtureServer(t *testing.T, runtimeBytes, modelBytes []byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		content := runtimeBytes
		if request.URL.Path == "/model" {
			content = modelBytes
		}
		if rangeHeader := request.Header.Get("Range"); rangeHeader != "" {
			var offset int
			if _, err := fmt.Sscanf(rangeHeader, "bytes=%d-", &offset); err != nil {
				t.Errorf("range: %v", err)
				return
			}
			writer.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, len(content)-1, len(content)))
			writer.WriteHeader(http.StatusPartialContent)
			_, _ = writer.Write(content[offset:])
			return
		}
		_, _ = writer.Write(content)
	}))
}

func digest(value []byte) string                 { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }
func stringsOf(character rune, count int) string { return strings.Repeat(string(character), count) }
