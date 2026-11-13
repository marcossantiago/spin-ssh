// Package update checks GitHub for new spin-ssh releases and replaces the running
// binary with one, verified against the release's checksums.txt.
package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

// Repo is where releases are published.
const Repo = "marcossantiago/spin-ssh"

// Updater talks to GitHub. The zero value isn't usable; use New.
type Updater struct {
	APIURL      string // https://api.github.com
	DownloadURL string // https://github.com
	HTTP        *http.Client
}

// New returns an Updater for github.com.
func New() *Updater {
	return &Updater{
		APIURL:      "https://api.github.com",
		DownloadURL: "https://github.com",
		HTTP:        &http.Client{Timeout: 60 * time.Second},
	}
}

// IsRelease reports whether v is a tagged release, not a dev or pseudo-version build.
// Only releases are compared against the latest one.
func IsRelease(v string) bool {
	return semver.IsValid(v) && semver.Prerelease(v) == "" && semver.Build(v) == ""
}

// Newer reports whether latest is a newer release than current. Any release is newer
// than a dev build.
func Newer(current, latest string) bool {
	if !IsRelease(latest) {
		return false
	}
	if !semver.IsValid(current) {
		return true
	}
	return semver.Compare(latest, current) > 0
}

// Latest returns the tag of the latest release.
func (u *Updater) Latest(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.APIURL+"/repos/"+Repo+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := u.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("checking for updates: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("no releases published at https://github.com/%s/releases yet", Repo)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checking for updates: GitHub returned %s", resp.Status)
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", fmt.Errorf("checking for updates: %w", err)
	}
	return rel.TagName, nil
}

// Method is how the running binary was installed, which decides how to update it.
type Method int

const (
	// Binary is a downloaded binary (install.sh or Releases): spin-ssh replaces itself.
	Binary Method = iota
	// GoInstall is a binary in GOBIN or GOPATH/bin.
	GoInstall
	// Homebrew is a binary in Homebrew's prefix.
	Homebrew
)

// Executable is the running binary's real path, with symlinks resolved.
func Executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// Detect works out how the binary at exe was installed, from where it lives.
func Detect(exe string) Method {
	if strings.Contains(exe, "/Cellar/") || strings.HasPrefix(exe, "/opt/homebrew/") || strings.HasPrefix(exe, "/home/linuxbrew/") {
		return Homebrew
	}
	var dirs []string
	if gobin := os.Getenv("GOBIN"); gobin != "" {
		dirs = append(dirs, gobin)
	}
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		home, _ := os.UserHomeDir()
		gopath = filepath.Join(home, "go")
	}
	for _, p := range filepath.SplitList(gopath) {
		dirs = append(dirs, filepath.Join(p, "bin"))
	}
	for _, d := range dirs {
		real := filepath.Clean(d)
		if r, err := filepath.EvalSymlinks(d); err == nil {
			real = r
		}
		if filepath.Dir(exe) == real {
			return GoInstall
		}
	}
	return Binary
}

// Apply downloads release tag for this OS and architecture, checks it against
// checksums.txt, and atomically replaces the binary at exe.
func (u *Updater) Apply(ctx context.Context, tag, exe string) error {
	// Fail before downloading if exe's directory can't be written.
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".spin-ssh-update-*")
	if err != nil {
		return fmt.Errorf("can't write to %s: %w", dir, err)
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	archive := fmt.Sprintf("spin-ssh_%s_%s_%s.tar.gz", strings.TrimPrefix(tag, "v"), runtime.GOOS, runtime.GOARCH)
	base := u.DownloadURL + "/" + Repo + "/releases/download/" + tag + "/"
	sums, err := u.download(ctx, base+"checksums.txt")
	if err != nil {
		return err
	}
	want, err := checksumFor(sums, archive)
	if err != nil {
		return err
	}
	data, err := u.download(ctx, base+archive)
	if err != nil {
		return err
	}
	if got := sha256.Sum256(data); hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("%s doesn't match its checksum; not installing it", archive)
	}

	bin, err := extract(data, "spin-ssh")
	if err != nil {
		return fmt.Errorf("%s: %w", archive, err)
	}
	if _, err := tmp.Write(bin); err != nil {
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), exe)
}

func (u *Updater) download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := u.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 100<<20))
}

// checksumFor finds name's SHA256 in a checksums.txt ("<hex>  <name>" lines).
func checksumFor(sums []byte, name string) (string, error) {
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == name {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("no release build for %s/%s (%s isn't in checksums.txt)", runtime.GOOS, runtime.GOARCH, name)
}

// extract returns the file called name from a .tar.gz.
func extract(data []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("no %s in the archive", name)
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeReg && filepath.Base(h.Name) == name {
			return io.ReadAll(io.LimitReader(tr, 100<<20))
		}
	}
}
