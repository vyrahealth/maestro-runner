package cli

import (
	"archive/zip"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/devicelab-dev/maestro-runner/pkg/logger"
)

// insecureTransport skips TLS verification, for a self-signed --app-file host
// when --insecure is set.
func insecureTransport() *http.Transport {
	return &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // opt-in via --insecure
	}
}

// appDownloadTimeout bounds a single --app-file download. App binaries can be
// hundreds of MB, so it is generous.
const appDownloadTimeout = 15 * time.Minute

// isRemoteAppFile reports whether s is an http(s) URL rather than a local path.
func isRemoteAppFile(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// redactURL returns a URL safe to print: the scheme, host and path only, with
// the query string and any userinfo removed. Presigned URLs (S3, GCS, Azure
// SAS) carry credentials in the query, so the raw URL must never reach a log,
// report or error message. An unparseable URL is reduced to a fixed placeholder
// rather than risk leaking it.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<redacted url>"
	}
	u.RawQuery = ""
	u.Fragment = ""
	u.User = nil
	if u.RawQuery == "" && strings.Contains(raw, "?") {
		// Signal that parameters were dropped so a reader isn't misled into
		// thinking the printed URL is the exact one used.
		return u.String() + "?<redacted>"
	}
	return u.String()
}

// redactErr replaces every occurrence of the raw URL in an error's text with
// its redacted form, so a wrapped net/http error (which embeds the full URL,
// query and all) cannot leak credentials.
func redactErr(err error, raw string) error {
	if err == nil {
		return nil
	}
	msg := strings.ReplaceAll(err.Error(), raw, redactURL(raw))
	// Guard against a query string surfacing on its own (some errors quote just
	// the RawQuery). Drop anything after the first '?' of the raw URL if it
	// still appears verbatim.
	if i := strings.Index(raw, "?"); i >= 0 {
		if q := raw[i+1:]; q != "" {
			msg = strings.ReplaceAll(msg, q, "<redacted>")
		}
	}
	return fmt.Errorf("%s", msg)
}

// appCacheDir returns (creating it) the directory downloaded app binaries are
// cached in.
func appCacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".maestro-runner", "app-cache")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// canonicalAppURL is the URL without its query string — the object's identity,
// used as the cache key when no checksum is supplied so a re-signed URL for the
// same object still hits the cache.
func canonicalAppURL(raw string) string {
	if i := strings.Index(raw, "?"); i >= 0 {
		return raw[:i]
	}
	return raw
}

// resolveRemoteAppFile downloads cfg.AppFile when it is an http(s) URL, verifies
// an optional SHA-256, unpacks a .zip to the .app or .ipa inside, and rewrites
// cfg.AppFile to the local cached path. A local path is left untouched. The raw
// URL is never logged — only its redacted form.
func resolveRemoteAppFile(cfg *RunConfig) error {
	raw := cfg.AppFile
	if raw == "" || !isRemoteAppFile(raw) {
		return nil
	}
	redacted := redactURL(raw)

	cacheDir, err := appCacheDir()
	if err != nil {
		return fmt.Errorf("app-file cache: %w", err)
	}

	// Cache key: content-addressed by the supplied checksum when given (a
	// changed artifact then lands in a different slot), else by the object URL
	// without its query so re-signed URLs reuse the cache.
	wantSHA := strings.ToLower(strings.TrimSpace(cfg.AppFileSHA256))
	var key string
	if wantSHA != "" {
		key = "sha-" + wantSHA
	} else {
		sum := sha256.Sum256([]byte(canonicalAppURL(raw)))
		key = "url-" + hex.EncodeToString(sum[:])
	}

	// Preserve the extension so downstream install/unzip logic keys off it.
	ext := strings.ToLower(filepath.Ext(canonicalAppURL(raw)))
	cachedPath := filepath.Join(cacheDir, key+ext)

	// Reuse a cached download when it is present and (if a checksum was given)
	// still matches. A checksum mismatch means the file changed under a reused
	// key, so refetch.
	if fi, statErr := os.Stat(cachedPath); statErr == nil && fi.Size() > 0 {
		if wantSHA == "" {
			logger.Info("Using cached app-file for %s", redacted)
			return finishRemoteAppFile(cfg, cachedPath, ext)
		}
		got, sumErr := sha256File(cachedPath)
		if sumErr == nil && got == wantSHA {
			logger.Info("Using cached app-file for %s (sha256 verified)", redacted)
			return finishRemoteAppFile(cfg, cachedPath, ext)
		}
		_ = os.Remove(cachedPath)
	}

	logger.Info("Downloading app-file from %s", redacted)
	printSetupStep(fmt.Sprintf("Downloading app: %s", redacted))
	if err := downloadTo(raw, cachedPath, cfg.Insecure); err != nil {
		return redactErr(err, raw)
	}

	if wantSHA != "" {
		got, sumErr := sha256File(cachedPath)
		if sumErr != nil {
			_ = os.Remove(cachedPath)
			return fmt.Errorf("app-file checksum: %w", sumErr)
		}
		if got != wantSHA {
			_ = os.Remove(cachedPath)
			return fmt.Errorf("app-file from %s failed SHA-256 check: expected %s, got %s", redacted, wantSHA, got)
		}
		logger.Info("app-file sha256 verified")
	}

	return finishRemoteAppFile(cfg, cachedPath, ext)
}

// finishRemoteAppFile unpacks a downloaded .zip to the .app/.ipa inside (so a
// simulator gets its .app and a device its .ipa) and points cfg.AppFile at the
// resulting local path. A non-zip download is used as-is.
func finishRemoteAppFile(cfg *RunConfig, downloaded, ext string) error {
	if ext != ".zip" {
		cfg.AppFile = downloaded
		return nil
	}
	extractDir := strings.TrimSuffix(downloaded, ".zip") + "-extracted"
	bundle, err := extractAppBundle(downloaded, extractDir)
	if err != nil {
		return fmt.Errorf("unpacking downloaded app archive: %w", err)
	}
	cfg.AppFile = bundle
	logger.Info("Unpacked app archive to %s", bundle)
	return nil
}

// downloadTo streams url into a fresh file at dest via a temp file, so a killed
// download never leaves a truncated file in the cache under the final name.
func downloadTo(rawURL, dest string, insecure bool) error {
	client := &http.Client{Timeout: appDownloadTimeout}
	if insecure {
		client.Transport = insecureTransport()
	}

	resp, err := client.Get(rawURL)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download failed: HTTP %d for %s", resp.StatusCode, rawURL)
	}

	tmp, err := os.CreateTemp(filepath.Dir(dest), ".download-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName) // no-op once renamed
	}()

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, dest)
}

// sha256File returns the lowercase hex SHA-256 of a file's contents.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// extractAppBundle unzips src into dest (fresh) and returns the path of the
// single .app directory or .ipa file inside it, preferring a top-level .app.
func extractAppBundle(src, dest string) (string, error) {
	if err := os.RemoveAll(dest); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", err
	}
	if err := unzipInto(src, dest); err != nil {
		return "", err
	}

	// Prefer a top-level *.app; fall back to any *.app or *.ipa found by a walk.
	var appDir, ipaFile string
	entries, _ := os.ReadDir(dest)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() && strings.HasSuffix(strings.ToLower(name), ".app") {
			return filepath.Join(dest, name), nil
		}
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(name), ".ipa") {
			ipaFile = filepath.Join(dest, name)
		}
	}
	if ipaFile != "" {
		return ipaFile, nil
	}
	_ = filepath.Walk(dest, func(p string, info os.FileInfo, err error) error {
		if err != nil || appDir != "" {
			return nil
		}
		lower := strings.ToLower(info.Name())
		if info.IsDir() && strings.HasSuffix(lower, ".app") {
			appDir = p
		} else if !info.IsDir() && ipaFile == "" && strings.HasSuffix(lower, ".ipa") {
			ipaFile = p
		}
		return nil
	})
	if appDir != "" {
		return appDir, nil
	}
	if ipaFile != "" {
		return ipaFile, nil
	}
	return "", fmt.Errorf("no .app or .ipa found inside the archive")
}

// unzipInto extracts every entry of src under dest, guarding against zip-slip.
func unzipInto(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()

	destPrefix := filepath.Clean(dest) + string(os.PathSeparator)
	for _, f := range r.File {
		target := filepath.Join(dest, f.Name)
		if !strings.HasPrefix(filepath.Clean(target)+string(os.PathSeparator), destPrefix) {
			return fmt.Errorf("invalid path in archive: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := writeZipEntry(f, target); err != nil {
			return err
		}
	}
	return nil
}

func writeZipEntry(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	mode := f.Mode()
	if mode == 0 {
		mode = 0o644
	}
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	_, err = io.Copy(out, rc) //nolint:gosec // extracting a user-provided app archive; zip-slip guarded above
	return err
}
