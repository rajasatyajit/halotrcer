package scripts

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMirror: testdata/mirror serves the snapshot's releases in GitHub's
// layout (download, the release list, latest = the highest stable tag) and
// its package repositories, and refuses paths outside them.
func TestMirror(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}
	bin := filepath.Join(t.TempDir(), "mirror")
	buildGo(t, bin, "../testdata/mirror", "")
	dist := t.TempDir()
	for _, f := range []string{
		"releases/tokvera/v0.2.0/checksums.txt", "releases/tokvera/v0.2.10/checksums.txt",
		"releases/tokvera/v0.3.0-rc.1/checksums.txt", "releases/halotrcer/v1.2.3/halotrcer_1.2.3_linux_amd64.tar.gz",
		"repo/apt/dists/stable/InRelease", "secret.txt",
	} {
		writeFile(t, filepath.Join(dist, f), "body of "+f, 0o644)
	}
	urlFile := filepath.Join(t.TempDir(), "url")
	cmd := exec.CommandContext(t.Context(), bin, "--dist", dist, "--url-file", urlFile, "--latest", "halotrcer=v1.2.3-snapshot-abc1234")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	var base string
	for range 100 {
		if b, err := os.ReadFile(urlFile); err == nil && len(b) > 0 {
			base = strings.TrimSpace(string(b))
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if base == "" {
		t.Fatal("the mirror did not start")
	}
	get := func(path string) (int, string) {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, base+path, http.NoBody)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	if code, body := get("/dl/halotrcer/releases/download/v1.2.3/halotrcer_1.2.3_linux_amd64.tar.gz"); code != 200 || body != "body of releases/halotrcer/v1.2.3/halotrcer_1.2.3_linux_amd64.tar.gz" {
		t.Errorf("download: %d %q", code, body)
	}
	if code, body := get("/api/tokvera/releases/latest"); code != 200 || !strings.Contains(body, `"tag_name":"v0.2.10"`) {
		t.Errorf("latest: %d %s", code, body)
	}
	code, body := get("/api/tokvera/releases")
	var rels []struct {
		Tag        string `json:"tag_name"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.Unmarshal([]byte(body), &rels); code != 200 || err != nil || len(rels) != 3 ||
		rels[0].Tag != "v0.3.0-rc.1" || !rels[0].Prerelease || rels[1].Tag != "v0.2.10" || rels[2].Tag != "v0.2.0" {
		t.Errorf("releases: %d %s", code, body)
	}
	if code, body := get("/api/halotrcer/releases/latest"); code != 200 || !strings.Contains(body, `"tag_name":"v1.2.3-snapshot-abc1234"`) {
		t.Errorf("--latest: %d %s", code, body)
	}
	if code, body := get("/repo/apt/dists/stable/InRelease"); code != 200 || !strings.HasPrefix(body, "body of repo/") {
		t.Errorf("repo: %d %q", code, body)
	}
	for _, p := range []string{
		"/api/sievant/releases/latest",
		"/dl/tokvera/releases/download/v0.2.0/missing",
		"/dl/tokvera/releases/download/latest/checksums.txt",
		"/dl/..%2f..%2fsecret.txt/releases/download/v0.2.0/x",
		"/dl/tokvera/releases/download/v0.2.0/..%2f..%2f..%2fsecret.txt",
		"/repo/../secret.txt",
	} {
		if code, body := get(p); code == 200 {
			t.Errorf("%s served: %q", p, body)
		}
	}
}
