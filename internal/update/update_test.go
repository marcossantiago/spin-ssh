package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		current, latest string
		want            bool
	}{
		{"v0.1.0", "v0.2.0", true},
		{"v0.2.0", "v0.2.0", false},
		{"v0.3.0", "v0.2.0", false},
		{"v0.0.0-20261113124748-b7f628aa03fc", "v0.1.0", true}, // go build from a checkout
		{"dev", "v0.1.0", true},
		{"v0.1.0", "v0.2.0-rc1", false}, // prereleases aren't offered
	} {
		if got := Newer(c.current, c.latest); got != c.want {
			t.Errorf("Newer(%s, %s) = %v", c.current, c.latest, got)
		}
	}
}

func tarGz(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct {
		name string
		data []byte
	}{{"README.md", []byte("readme")}, {name, content}} {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o755, Size: int64(len(f.data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write(f.data)
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// fakeGitHub serves a v0.2.0 release whose checksum is right unless corrupt is set.
func fakeGitHub(t *testing.T, corrupt bool) *Updater {
	archive := fmt.Sprintf("spin-ssh_0.2.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	data := tarGz(t, "spin-ssh", []byte("new binary"))
	sum := sha256.Sum256(data)
	if corrupt {
		sum[0] ^= 0xff
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/"+Repo+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tag_name": "v0.2.0"}`)
	})
	mux.HandleFunc("/"+Repo+"/releases/download/v0.2.0/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  %s\n%s  other.tar.gz\n", hex.EncodeToString(sum[:]), archive, hex.EncodeToString(sum[:]))
	})
	mux.HandleFunc("/"+Repo+"/releases/download/v0.2.0/"+archive, func(w http.ResponseWriter, r *http.Request) {
		w.Write(data)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &Updater{APIURL: srv.URL, DownloadURL: srv.URL, HTTP: srv.Client()}
}

func TestLatestAndApply(t *testing.T) {
	u := fakeGitHub(t, false)
	tag, err := u.Latest(context.Background())
	if err != nil || tag != "v0.2.0" {
		t.Fatalf("Latest = %q, %v", tag, err)
	}

	exe := filepath.Join(t.TempDir(), "spin-ssh")
	os.WriteFile(exe, []byte("old binary"), 0o755)
	if err := u.Apply(context.Background(), tag, exe); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(exe)
	info, _ := os.Stat(exe)
	if string(got) != "new binary" || info.Mode().Perm() != 0o755 {
		t.Fatalf("exe = %q, mode %v", got, info.Mode())
	}
	if leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), ".spin-ssh-update-*")); len(leftovers) > 0 {
		t.Fatalf("temp files left: %v", leftovers)
	}
}

func TestApplyRefusesBadChecksum(t *testing.T) {
	u := fakeGitHub(t, true)
	exe := filepath.Join(t.TempDir(), "spin-ssh")
	os.WriteFile(exe, []byte("old binary"), 0o755)
	if err := u.Apply(context.Background(), "v0.2.0", exe); err == nil {
		t.Fatal("installed a binary with a bad checksum")
	}
	if got, _ := os.ReadFile(exe); string(got) != "old binary" {
		t.Fatalf("exe changed to %q", got)
	}
}

func TestDetect(t *testing.T) {
	gopath := t.TempDir()
	t.Setenv("GOPATH", gopath)
	t.Setenv("GOBIN", "")
	os.Mkdir(filepath.Join(gopath, "bin"), 0o755)
	bin, _ := filepath.EvalSymlinks(gopath)
	if got := Detect(filepath.Join(bin, "bin", "spin-ssh")); got != GoInstall {
		t.Errorf("GOPATH/bin = %v", got)
	}
	if got := Detect("/opt/homebrew/Cellar/spin-ssh/0.1.0/bin/spin-ssh"); got != Homebrew {
		t.Errorf("Cellar = %v", got)
	}
	home, _ := os.UserHomeDir()
	if got := Detect(filepath.Join(home, ".local", "bin", "spin-ssh")); got != Binary {
		t.Errorf("~/.local/bin = %v", got)
	}
}
