package clientapp

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// DefaultReleaseRepo is the GitHub repo used for client self-update.
const DefaultReleaseRepo = "leganck/wakehub"

// UpdateInfo describes a newer release asset.
type UpdateInfo struct {
	Current     string `json:"current"`
	Latest      string `json:"latest"`
	Tag         string `json:"tag"`
	AssetName   string `json:"assetName"`
	DownloadURL string `json:"downloadURL"`
	HTMLURL     string `json:"htmlURL"`
	UpToDate    bool   `json:"upToDate"`
}

type ghRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// CheckUpdate queries GitHub Releases for a newer wakehub-client asset.
// repo is "owner/name"; empty uses DefaultReleaseRepo.
func CheckUpdate(ctx context.Context, current, repo string) (*UpdateInfo, error) {
	if repo == "" {
		repo = DefaultReleaseRepo
	}
	if current == "" {
		current = "dev"
	}
	url := "https://api.github.com/repos/" + repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "wakehub-client/"+current)

	client := &http.Client{Timeout: 30 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return nil, fmt.Errorf("github releases: HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	var rel ghRelease
	if err := json.NewDecoder(res.Body).Decode(&rel); err != nil {
		return nil, err
	}
	latest := strings.TrimPrefix(rel.TagName, "v")
	info := &UpdateInfo{
		Current:  current,
		Latest:   latest,
		Tag:      rel.TagName,
		HTMLURL:  rel.HTMLURL,
		UpToDate: !isNewerVersion(latest, current),
	}
	want := clientAssetName()
	for _, a := range rel.Assets {
		if a.Name == want || strings.HasPrefix(a.Name, strings.TrimSuffix(want, filepath.Ext(want))) {
			info.AssetName = a.Name
			info.DownloadURL = a.BrowserDownloadURL
			break
		}
	}
	// Prefer exact name match; also accept contains binary + os + arch.
	if info.DownloadURL == "" {
		needleOS := titleOS()
		arch := releaseArch()
		for _, a := range rel.Assets {
			n := a.Name
			if strings.Contains(n, "wakehub-client") && strings.Contains(n, needleOS) && strings.Contains(n, arch) {
				info.AssetName = a.Name
				info.DownloadURL = a.BrowserDownloadURL
				break
			}
		}
	}
	if info.DownloadURL == "" && !info.UpToDate {
		return info, fmt.Errorf("no asset for %s/%s in %s (looked for %s)", runtime.GOOS, runtime.GOARCH, rel.TagName, want)
	}
	return info, nil
}

// ApplyUpdate downloads info.DownloadURL and replaces the current executable.
// On Windows the new binary is written beside the exe and a .bat swap may be needed;
// we write to exe+".new" then attempt rename after removing old to .old.
func ApplyUpdate(ctx context.Context, info *UpdateInfo) error {
	if info == nil || info.DownloadURL == "" {
		return fmt.Errorf("no download URL")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, info.DownloadURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "wakehub-client-update")
	client := &http.Client{Timeout: 5 * time.Minute}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("download: HTTP %d", res.StatusCode)
	}

	tmpDir, err := os.MkdirTemp("", "wakehub-update-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, info.AssetName)
	f, err := os.Create(archivePath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, res.Body); err != nil {
		f.Close()
		return err
	}
	f.Close()

	binPath, err := extractClientBinary(archivePath, tmpDir)
	if err != nil {
		return err
	}

	newPath := exe + ".new"
	if err := copyFile(binPath, newPath); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(newPath, 0o755)
	}

	oldPath := exe + ".old"
	_ = os.Remove(oldPath)
	if err := os.Rename(exe, oldPath); err != nil {
		// Windows may lock the running binary; keep .new and instruct user.
		return fmt.Errorf("could not replace running binary (stop the service first?): %w; new binary saved as %s", err, newPath)
	}
	if err := os.Rename(newPath, exe); err != nil {
		_ = os.Rename(oldPath, exe) // best-effort rollback
		return fmt.Errorf("install new binary: %w", err)
	}
	return nil
}

func clientAssetName() string {
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	return "wakehub-client_" + titleOS() + "_" + releaseArch() + ext
}

func titleOS() string {
	s := runtime.GOOS
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func releaseArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "386":
		return "i386"
	default:
		return runtime.GOARCH
	}
}

// isNewerVersion returns true if latest is greater than current (loose semver).
func isNewerVersion(latest, current string) bool {
	latest = strings.TrimPrefix(strings.TrimSpace(latest), "v")
	current = strings.TrimPrefix(strings.TrimSpace(current), "v")
	if current == "" || current == "dev" || current == "unknown" {
		return latest != "" && latest != current
	}
	if latest == current {
		return false
	}
	lp := splitVer(latest)
	cp := splitVer(current)
	for i := 0; i < 3; i++ {
		if lp[i] > cp[i] {
			return true
		}
		if lp[i] < cp[i] {
			return false
		}
	}
	return false
}

func splitVer(s string) [3]int {
	var out [3]int
	parts := strings.SplitN(s, ".", 3)
	for i := 0; i < len(parts) && i < 3; i++ {
		n := 0
		for _, r := range parts[i] {
			if r < '0' || r > '9' {
				break
			}
			n = n*10 + int(r-'0')
		}
		out[i] = n
	}
	return out
}

func extractClientBinary(archivePath, destDir string) (string, error) {
	name := strings.ToLower(archivePath)
	if strings.HasSuffix(name, ".zip") {
		return extractZipBinary(archivePath, destDir)
	}
	return extractTarGzBinary(archivePath, destDir)
}

func extractZipBinary(path, destDir string) (string, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer r.Close()
	var found string
	for _, f := range r.File {
		base := filepath.Base(f.Name)
		if base != "wakehub-client.exe" && base != "wakehub-client" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		outPath := filepath.Join(destDir, base)
		out, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			rc.Close()
			return "", err
		}
		_, err = io.Copy(out, rc)
		out.Close()
		rc.Close()
		if err != nil {
			return "", err
		}
		found = outPath
		break
	}
	if found == "" {
		return "", fmt.Errorf("wakehub-client binary not found in zip")
	}
	return found, nil
}

func extractTarGzBinary(path, destDir string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var found string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		base := filepath.Base(hdr.Name)
		if base != "wakehub-client" && base != "wakehub-client.exe" {
			continue
		}
		outPath := filepath.Join(destDir, base)
		out, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return "", err
		}
		_, err = io.Copy(out, tr)
		out.Close()
		if err != nil {
			return "", err
		}
		found = outPath
		break
	}
	if found == "" {
		return "", fmt.Errorf("wakehub-client binary not found in archive")
	}
	return found, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	cerr := out.Close()
	if err != nil {
		return err
	}
	return cerr
}
