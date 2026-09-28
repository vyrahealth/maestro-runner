package cli

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRedactURL(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"https://s3.example.com/builds/app.apk", "https://s3.example.com/builds/app.apk"},
		{"https://s3.example.com/builds/app.apk?X-Amz-Signature=deadbeef&X-Amz-Credential=AKIA", "https://s3.example.com/builds/app.apk?<redacted>"},
		{"https://user:secret@host.example.com/app.ipa", "https://host.example.com/app.ipa"},
		{"http://host/app.zip", "http://host/app.zip"},
	}
	for _, c := range cases {
		if got := redactURL(c.in); got != c.want {
			t.Errorf("redactURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRedactURL_NeverLeaksSignature(t *testing.T) {
	raw := "https://s3.example.com/app.apk?X-Amz-Signature=TOPSECRETSIG&X-Amz-Credential=AKIAEXAMPLE"
	if got := redactURL(raw); strings.Contains(got, "TOPSECRETSIG") || strings.Contains(got, "AKIAEXAMPLE") {
		t.Errorf("redactURL leaked credentials: %q", got)
	}
}

func TestRedactErr(t *testing.T) {
	raw := "https://s3.example.com/app.apk?X-Amz-Signature=TOPSECRETSIG"
	err := redactErr(fmt.Errorf("Get %q: connection refused", raw), raw)
	if err == nil || strings.Contains(err.Error(), "TOPSECRETSIG") {
		t.Errorf("redactErr leaked the signature: %v", err)
	}
	if redactErr(nil, raw) != nil {
		t.Error("redactErr(nil) should be nil")
	}
}

// sha256Hex is the expected checksum of b, for tests.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestResolveRemoteAppFile_DownloadAndCache(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // isolate ~/.maestro-runner/app-cache
	body := []byte("PK-fake-apk-bytes")
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	cfg := &RunConfig{AppFile: srv.URL + "/build/app.apk"}
	if err := resolveRemoteAppFile(cfg); err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if cfg.AppFile == "" || strings.HasPrefix(cfg.AppFile, "http") {
		t.Fatalf("AppFile not rewritten to a local path: %q", cfg.AppFile)
	}
	if filepath.Ext(cfg.AppFile) != ".apk" {
		t.Errorf("cached file lost its extension: %q", cfg.AppFile)
	}
	got, _ := os.ReadFile(cfg.AppFile)
	if string(got) != string(body) {
		t.Errorf("downloaded content mismatch")
	}

	// Second resolve with a fresh cfg must reuse the cache (no second hit).
	cfg2 := &RunConfig{AppFile: srv.URL + "/build/app.apk"}
	if err := resolveRemoteAppFile(cfg2); err != nil {
		t.Fatalf("second resolve failed: %v", err)
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Errorf("expected 1 server hit (cache reuse), got %d", n)
	}
}

func TestResolveRemoteAppFile_SHA256(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	body := []byte("checksummed-app")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	t.Run("matching checksum passes", func(t *testing.T) {
		cfg := &RunConfig{AppFile: srv.URL + "/a.apk", AppFileSHA256: sha256Hex(body)}
		if err := resolveRemoteAppFile(cfg); err != nil {
			t.Fatalf("resolve with matching sha failed: %v", err)
		}
	})

	t.Run("wrong checksum is rejected and file removed", func(t *testing.T) {
		wrongSHA := sha256Hex([]byte("something-else"))
		cfg := &RunConfig{AppFile: srv.URL + "/b.apk", AppFileSHA256: wrongSHA}
		err := resolveRemoteAppFile(cfg)
		if err == nil {
			t.Fatal("expected a checksum error")
		}
		// The bad download, cached under its expected-sha key, must be gone.
		home, _ := os.UserHomeDir()
		badPath := filepath.Join(home, ".maestro-runner", "app-cache", "sha-"+wrongSHA+".apk")
		if _, statErr := os.Stat(badPath); statErr == nil {
			t.Errorf("a checksum-failed download was left cached: %s", badPath)
		}
	})
}

func TestResolveRemoteAppFile_UnzipsToApp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// Build an in-memory zip containing MyApp.app/Info.plist.
	zipBytes := buildZip(t, map[string]string{
		"MyApp.app/Info.plist":            "<plist/>",
		"MyApp.app/MyApp":                 "binary",
		"MyApp.app/Frameworks/keep.dylib": "x",
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(zipBytes)
	}))
	defer srv.Close()

	cfg := &RunConfig{AppFile: srv.URL + "/sim/MyApp.zip"}
	if err := resolveRemoteAppFile(cfg); err != nil {
		t.Fatalf("resolve of zip failed: %v", err)
	}
	if !strings.HasSuffix(cfg.AppFile, "MyApp.app") {
		t.Fatalf("AppFile should point at the .app, got %q", cfg.AppFile)
	}
	fi, err := os.Stat(cfg.AppFile)
	if err != nil || !fi.IsDir() {
		t.Fatalf("extracted .app is not a directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.AppFile, "Info.plist")); err != nil {
		t.Errorf("expected Info.plist inside the extracted .app: %v", err)
	}
}

func TestResolveRemoteAppFile_LocalPathUntouched(t *testing.T) {
	cfg := &RunConfig{AppFile: "/local/path/app.apk"}
	if err := resolveRemoteAppFile(cfg); err != nil {
		t.Fatalf("local path should be a no-op, got: %v", err)
	}
	if cfg.AppFile != "/local/path/app.apk" {
		t.Errorf("local AppFile was modified: %q", cfg.AppFile)
	}
}

func TestResolveRemoteAppFile_HTTPErrorRedacted(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer srv.Close()

	sig := "X-Amz-Signature=TOPSECRETSIG"
	cfg := &RunConfig{AppFile: srv.URL + "/x.apk?" + sig}
	err := resolveRemoteAppFile(cfg)
	if err == nil {
		t.Fatal("expected an error on HTTP 403")
	}
	if strings.Contains(err.Error(), "TOPSECRETSIG") {
		t.Errorf("error leaked the presigned signature: %v", err)
	}
}

// buildZip returns a zip archive containing the given path→content entries.
func buildZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "a.zip")
	f, err := os.Create(tmp)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
