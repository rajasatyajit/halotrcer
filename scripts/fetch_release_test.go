package scripts

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// The release a fake GitHub serves to scripts/smoke/fetch-release.sh: what
// Tokvera's v0.2.0 release, the shared tap and bucket, winget-pkgs and
// Tokvera's repository would hold.
const (
	frTag      = "v0.2.0"
	frIdentity = "https://github.com/rajasatyajit/tokvera/.github/workflows/release.yml@refs/tags/v0.2.0"
)

// frFiles are the twelve files the channels install.
var frFiles = []string{
	"tokvera_0.2.0_linux_amd64.tar.gz", "tokvera_0.2.0_linux_arm64.tar.gz",
	"tokvera_0.2.0_darwin_amd64.tar.gz", "tokvera_0.2.0_darwin_arm64.tar.gz",
	"tokvera_0.2.0_windows_amd64.zip", "tokvera_0.2.0_windows_arm64.zip",
	"tokvera_0.2.0_amd64.deb", "tokvera_0.2.0_arm64.deb",
	"tokvera-0.2.0-1.x86_64.rpm", "tokvera-0.2.0-1.aarch64.rpm",
	"tokvera_0.2.0_x86_64.apk", "tokvera_0.2.0_aarch64.apk",
}

type frRelease struct {
	mu    sync.Mutex
	files map[string][]byte // URL path -> body; absent is a 404
	asked []string
}

func frHex(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

const (
	frDL  = "/dl/rajasatyajit/tokvera/releases/download/v0.2.0/"
	frRaw = "/raw/"
)

// newFRRelease is a consistent release: every file listed and matching, the
// bundle binding checksums.txt to frIdentity, manifests at 0.2.0 naming only
// listed hashes, and a PEM apk key.
func newFRRelease() *frRelease {
	r := &frRelease{files: map[string][]byte{}}
	var sums strings.Builder
	for _, f := range frFiles {
		body := []byte("release file " + f)
		r.files[frDL+f] = body
		fmt.Fprintf(&sums, "%s  %s\n", frHex(body), f)
	}
	// A file the channels do not install is listed but never downloaded.
	fmt.Fprintf(&sums, "%s  %s\n", frHex([]byte("cache")), "tokvera-cache_0.2.0_linux_amd64.tar.gz")
	r.setChecksums(sums.String())
	a, b := frHex(r.files[frDL+frFiles[2]]), frHex(r.files[frDL+frFiles[4]])
	r.files[frRaw+"rajasatyajit/homebrew-tap/main/Casks/tokvera.rb"] = []byte("cask \"tokvera\" do\n  version \"0.2.0\"\n  sha256 \"" + a + "\"\nend\n")
	r.files[frRaw+"rajasatyajit/scoop-bucket/main/tokvera.json"] = []byte("{\n    \"version\": \"0.2.0\",\n    \"hash\": \"" + b + "\"\n}\n")
	w := frRaw + "microsoft/winget-pkgs/master/manifests/h/Halotrcer/Tokvera/0.2.0/"
	r.files[w+"Halotrcer.Tokvera.yaml"] = []byte("PackageIdentifier: Halotrcer.Tokvera\nPackageVersion: 0.2.0\n")
	r.files[w+"Halotrcer.Tokvera.locale.en-US.yaml"] = []byte("PackageIdentifier: Halotrcer.Tokvera\nPackageVersion: 0.2.0\n")
	r.files[w+"Halotrcer.Tokvera.installer.yaml"] = []byte("PackageIdentifier: Halotrcer.Tokvera\nPackageVersion: 0.2.0\nInstallers:\n  - InstallerSha256: " + strings.ToUpper(b) + "\n")
	r.files[frRaw+"rajasatyajit/tokvera/v0.2.0/packaging/keys/satyajit.rajaraman@halotrcer.com-tokvera.rsa.pub"] = []byte("-----BEGIN PUBLIC KEY-----\nAAAA\n-----END PUBLIC KEY-----\n")
	return r
}

// setChecksums serves sums as checksums.txt, with a bundle that vouches for
// exactly these bytes and frIdentity (what the fake cosign checks).
func (r *frRelease) setChecksums(sums string) {
	r.files[frDL+"checksums.txt"] = []byte(sums)
	r.files[frDL+"checksums.txt.sigstore.json"] = []byte("bundle " + frHex([]byte(sums)) + " " + frIdentity + "\n")
}

func (r *frRelease) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.asked = append(r.asked, req.URL.Path)
	b, ok := r.files[req.URL.Path]
	if !ok {
		http.NotFound(w, req)
		return
	}
	_, _ = w.Write(b)
}

// frFakeCosign is cosign verify-blob as far as this script can tell: it
// accepts only a bundle naming the SHA-256 of the blob and the identity
// asked for, with GitHub's issuer, and logs its arguments.
const frFakeCosign = `#!/bin/sh
echo "$*" >>"$FAKE_COSIGN_LOG"
[ "$1" = verify-blob ] || exit 2
shift
bundle="" identity="" issuer="" blob=""
while [ $# -gt 0 ]; do
	case "$1" in
	--bundle) bundle="$2"; shift 2 ;;
	--certificate-identity) identity="$2"; shift 2 ;;
	--certificate-oidc-issuer) issuer="$2"; shift 2 ;;
	--*) exit 2 ;;
	*) blob="$1"; shift ;;
	esac
done
[ "$issuer" = https://token.actions.githubusercontent.com ] || { echo "wrong issuer" >&2; exit 1; }
[ "$(cat "$bundle")" = "bundle $(sha256sum "$blob" | cut -d' ' -f1) $identity" ] || { echo "no matching signature" >&2; exit 1; }
`

type frResult struct {
	code      int
	out       string
	dir       string
	cosignLog string
}

// frRun runs fetch-release.sh against r with a PATH of only what it needs
// (and the fake cosign unless noCosign).
func frRun(t *testing.T, r *frRelease, identity string, noCosign bool) frResult {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fetch-release.sh runs on Linux")
	}
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"sh", "curl", "sha256sum", "cut", "grep", "awk", "sed", "tr", "sort", "mkdir", "rm", "cat"} {
		p, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("%s not on PATH", tool)
		}
		if err := os.Symlink(p, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	if !noCosign {
		writeFile(t, filepath.Join(bin, "cosign"), frFakeCosign, 0o755)
	}
	path, err := filepath.Abs("smoke/fetch-release.sh")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "out")
	log := filepath.Join(root, "cosign.log")
	cmd := exec.CommandContext(t.Context(), filepath.Join(bin, "sh"), path, "tokvera", frTag, identity, out)
	cmd.Env = []string{"PATH=" + bin, "HOME=" + root, "LC_ALL=C", "FAKE_COSIGN_LOG=" + log,
		"SMOKE_GITHUB_DOWNLOAD=" + srv.URL + "/dl", "SMOKE_GITHUB_RAW=" + srv.URL + "/raw"}
	b, err := cmd.CombinedOutput()
	res := frResult{out: string(b), dir: out}
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatal(err)
		}
		res.code = ee.ExitCode()
	}
	if l, err := os.ReadFile(log); err == nil {
		res.cosignLog = string(l)
	}
	return res
}

// TestFetchReleaseVerifies: a consistent release is verified and laid out
// where snapshot.sh and the channels read it, cosign asked about the exact
// identity and GitHub's issuer, and the unlisted-for-channels file skipped.
func TestFetchReleaseVerifies(t *testing.T) {
	r := newFRRelease()
	res := frRun(t, r, frIdentity, false)
	if res.code != 0 {
		t.Fatalf("exit %d:\n%s", res.code, res.out)
	}
	want := "verify-blob --bundle " + filepath.Join(res.dir, "releases/tokvera/v0.2.0/checksums.txt.sigstore.json") +
		" --certificate-identity " + frIdentity + " --certificate-oidc-issuer https://token.actions.githubusercontent.com " +
		filepath.Join(res.dir, "releases/tokvera/v0.2.0/checksums.txt")
	if strings.TrimSpace(res.cosignLog) != want {
		t.Errorf("cosign was asked:\n%s\nwant:\n%s", res.cosignLog, want)
	}
	for _, f := range append(slicesOf(frFiles, "releases/tokvera/v0.2.0/"),
		"releases/tokvera/v0.2.0/checksums.txt", "casks/tokvera.rb", "scoop/tokvera.json",
		"winget/Halotrcer.Tokvera/Halotrcer.Tokvera.installer.yaml",
		"apk-keys/satyajit.rajaraman@halotrcer.com-tokvera.rsa.pub") {
		if _, err := os.Stat(filepath.Join(res.dir, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	for _, p := range r.asked {
		if strings.Contains(p, "tokvera-cache") {
			t.Errorf("downloaded %s, which no channel installs", p)
		}
	}
}

func slicesOf(names []string, prefix string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = prefix + n
	}
	return out
}

// TestFetchReleaseRefuses: every way the release can fail to be what cosign
// and checksums.txt vouch for stops the script, saying why; when the
// signature does not verify, no release file is downloaded at all.
func TestFetchReleaseRefuses(t *testing.T) {
	sums := func(r *frRelease) string { return string(r.files[frDL+"checksums.txt"]) }
	cask := frRaw + "rajasatyajit/homebrew-tap/main/Casks/tokvera.rb"
	installer := frRaw + "microsoft/winget-pkgs/master/manifests/h/Halotrcer/Tokvera/0.2.0/Halotrcer.Tokvera.installer.yaml"
	for name, c := range map[string]struct {
		change   func(r *frRelease)
		identity string
		noCosign bool
		want     string
		noFiles  bool // no release file may have been downloaded
	}{
		"no cosign": {noCosign: true, want: "needs cosign", noFiles: true},
		"another identity": {identity: "https://github.com/rajasatyajit/tokvera/.github/workflows/release.yml@refs/tags/v0.2.1",
			want: "cosign could not verify checksums.txt", noFiles: true},
		"tampered checksums.txt": {change: func(r *frRelease) {
			r.files[frDL+"checksums.txt"] = []byte(strings.Replace(sums(r), "tokvera_0.2.0_linux_amd64", "tokvera_0.2.0_linux_amd64x", 1))
		}, want: "cosign could not verify checksums.txt", noFiles: true},
		"no bundle": {change: func(r *frRelease) { delete(r.files, frDL+"checksums.txt.sigstore.json") },
			want: "download failed: http", noFiles: true},
		"a path in checksums.txt": {change: func(r *frRelease) {
			r.setChecksums(sums(r) + frHex([]byte("x")) + "  ../../x\n")
		}, want: "not '<sha256>  <file name>'", noFiles: true},
		"an archive that does not match": {change: func(r *frRelease) {
			r.files[frDL+"tokvera_0.2.0_linux_amd64.tar.gz"] = []byte("swapped")
		}, want: "tokvera_0.2.0_linux_amd64.tar.gz: SHA-256 " + frHex([]byte("swapped")) + " does not match checksums.txt"},
		"a file checksums.txt does not list": {change: func(r *frRelease) {
			var keep []string
			for _, l := range strings.SplitAfter(sums(r), "\n") {
				if !strings.Contains(l, "_x86_64.apk") {
					keep = append(keep, l)
				}
			}
			r.setChecksums(strings.Join(keep, ""))
		}, want: "checksums.txt does not list tokvera_0.2.0_x86_64.apk"},
		"the tap at another version": {change: func(r *frRelease) {
			r.files[cask] = []byte(strings.Replace(string(r.files[cask]), `"0.2.0"`, `"0.2.1"`, 1))
		}, want: "the tap's cask is not version 0.2.0"},
		"a cask pointing elsewhere": {change: func(r *frRelease) {
			r.files[cask] = []byte("cask \"tokvera\" do\n  version \"0.2.0\"\n  sha256 \"" + frHex([]byte("elsewhere")) + "\"\nend\n")
		}, want: "tokvera.rb names " + frHex([]byte("elsewhere")) + ", which checksums.txt does not list"},
		"a winget installer pointing elsewhere": {change: func(r *frRelease) {
			r.files[installer] = []byte("PackageVersion: 0.2.0\nInstallers:\n  - InstallerSha256: " + strings.ToUpper(frHex([]byte("elsewhere"))) + "\n")
		}, want: "Halotrcer.Tokvera.installer.yaml names " + frHex([]byte("elsewhere"))},
		"no winget manifest yet": {change: func(r *frRelease) { delete(r.files, installer) }, want: "download failed: http"},
		"an apk key that is not one": {change: func(r *frRelease) {
			r.files[frRaw+"rajasatyajit/tokvera/v0.2.0/packaging/keys/satyajit.rajaraman@halotrcer.com-tokvera.rsa.pub"] = []byte("<html>")
		}, want: "is not a PEM public key"},
	} {
		t.Run(name, func(t *testing.T) {
			r := newFRRelease()
			if c.change != nil {
				c.change(r)
			}
			id := c.identity
			if id == "" {
				id = frIdentity
			}
			res := frRun(t, r, id, c.noCosign)
			if res.code != 1 || !strings.Contains(res.out, c.want) {
				t.Fatalf("exit %d, output lacks %q:\n%s", res.code, c.want, res.out)
			}
			if c.noFiles {
				for _, p := range r.asked {
					if !strings.HasSuffix(p, "/checksums.txt") && !strings.HasSuffix(p, "/checksums.txt.sigstore.json") {
						t.Errorf("downloaded %s before the signature verified", p)
					}
				}
			}
			if strings.Contains(name, "does not match") {
				if _, err := os.Stat(filepath.Join(res.dir, "releases/tokvera/v0.2.0/tokvera_0.2.0_linux_amd64.tar.gz")); err == nil {
					t.Error("the mismatching archive was left behind")
				}
			}
		})
	}
}

// TestSnapshotRefusesPartialPins: release mode starts only from a complete,
// valid packaging/bundled-tools.env; a partial one stops snapshot.sh before
// anything is built, and it never falls back to the stand-ins.
func TestSnapshotRefusesPartialPins(t *testing.T) {
	dir := t.TempDir()
	env := filepath.Join(dir, "bundled-tools.env")
	writeFile(t, env, "TOKVERA_VERSION=v0.2.0\nTOKVERA_IMAGE_DIGEST=\nTOKVERA_CHART_DIGEST=\nSIEVANT_VERSION=\nSIEVANT_IMAGE_DIGEST=\nSIEVANT_CHART_DIGEST=\n", 0o644)
	path, err := filepath.Abs("smoke/snapshot.sh")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	cmd := exec.CommandContext(t.Context(), "sh", path, out)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "LC_ALL=C", "SMOKE_BUNDLED_TOOLS=" + env}
	b, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(b), "SIEVANT_VERSION is empty") {
		t.Fatalf("snapshot.sh with partial pins: %v\n%s", err, b)
	}
	if _, err := os.Stat(filepath.Join(out, "TOOLS")); err == nil {
		t.Error("snapshot.sh recorded a mode for a partial pin file")
	}
}
