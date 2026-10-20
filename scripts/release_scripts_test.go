package scripts

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// script runs scripts/release/NAME with sh in dir, with env added to a
// minimal environment, and returns its combined output and exit code.
func script(t *testing.T, dir string, env []string, name string, args ...string) (string, int) {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("release", name))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "sh", append([]string{path}, args...)...)
	cmd.Dir = dir
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "LC_ALL=C"}, env...)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("%s: %v", name, err)
		}
		code = ee.ExitCode()
	}
	return string(out), code
}

func need(t *testing.T, tools ...string) {
	t.Helper()
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			if os.Getenv("CI") != "" {
				t.Fatalf("%s is required in CI", tool)
			}
			t.Skipf("%s not on PATH", tool)
		}
	}
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@invalid", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@invalid")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

const digestA = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func pins(tokvera, sievant string) string {
	return "# pinned\nTOKVERA_VERSION=" + tokvera + "\nTOKVERA_IMAGE_DIGEST=" + digestA + "\nTOKVERA_CHART_DIGEST=" + digestA +
		"\n\nSIEVANT_VERSION=" + sievant + "\nSIEVANT_IMAGE_DIGEST=" + digestA + "\nSIEVANT_CHART_DIGEST=" + digestA + "\n"
}

func TestBundledTools(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.env")
	writeFile(t, good, pins("v0.2.0", "v0.2.1-rc.1"), 0o644)
	out, code := script(t, dir, nil, "bundled-tools.sh", good)
	if code != 0 {
		t.Fatalf("valid pins refused (%d):\n%s", code, out)
	}
	for _, line := range []string{
		"TOKVERA_IMAGE=ghcr.io/rajasatyajit/tokvera@" + digestA,
		"TOKVERA_CHART=ghcr.io/rajasatyajit/charts/tokvera@" + digestA,
		"TOKVERA_CHART_VERSION=0.2.0",
		"TOKVERA_IDENTITY=https://github.com/rajasatyajit/tokvera/.github/workflows/release.yml@refs/tags/v0.2.0",
		"SIEVANT_IMAGE=ghcr.io/rajasatyajit/sievant@" + digestA,
		"SIEVANT_CHART_VERSION=0.2.1-rc.1",
		"SIEVANT_IDENTITY=https://github.com/rajasatyajit/sievant/.github/workflows/release.yml@refs/tags/v0.2.1-rc.1",
	} {
		if !strings.Contains(out, line+"\n") {
			t.Errorf("output lacks %q:\n%s", line, out)
		}
	}

	// The committed file is empty until both tools release: refused.
	committed, err := filepath.Abs(filepath.Join("..", "packaging", "bundled-tools.env"))
	if err != nil {
		t.Fatal(err)
	}
	if out, code := script(t, dir, nil, "bundled-tools.sh", committed); code == 0 || !strings.Contains(out, "TOKVERA_VERSION is empty") {
		t.Errorf("the empty committed file was accepted (%d):\n%s", code, out)
	}

	canary := filepath.Join(dir, "ran")
	for name, content := range map[string]string{
		"substitution": strings.Replace(pins("v0.2.0", "v0.2.0"), "TOKVERA_VERSION=v0.2.0", "TOKVERA_VERSION=$(touch "+canary+")", 1),
		"backticks":    strings.Replace(pins("v0.2.0", "v0.2.0"), "SIEVANT_VERSION=v0.2.0", "SIEVANT_VERSION=`touch "+canary+"`", 1),
		"unknown key":  pins("v0.2.0", "v0.2.0") + "PATH=/tmp\n",
		"set twice":    pins("v0.2.0", "v0.2.0") + "TOKVERA_VERSION=v0.2.1\n",
		"missing":      strings.Replace(pins("v0.2.0", "v0.2.0"), "SIEVANT_CHART_DIGEST="+digestA+"\n", "", 1),
		"short digest": strings.Replace(pins("v0.2.0", "v0.2.0"), "TOKVERA_IMAGE_DIGEST="+digestA, "TOKVERA_IMAGE_DIGEST=sha256:abc", 1),
		"tag not v":    pins("0.2.0", "v0.2.0"),
		"not k=v":      pins("v0.2.0", "v0.2.0") + "TOKVERA\n",
	} {
		f := filepath.Join(dir, strings.ReplaceAll(name, " ", "-")+".env")
		writeFile(t, f, content, 0o644)
		if out, code := script(t, dir, nil, "bundled-tools.sh", f); code != 1 || strings.Contains(out, "_IMAGE=") {
			t.Errorf("%s: exit %d, want 1 and no output:\n%s", name, code, out)
		}
	}
	if _, err := os.Stat(canary); err == nil {
		t.Error("a value was executed")
	}
}

func TestPreflight(t *testing.T) {
	need(t, "git")
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(repo, "packaging", "bundled-tools.env"), pins("v0.2.0", "v0.2.0"), 0o644)
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-qm", "one")
	gitIn(t, repo, "tag", "v1.0.0")
	gitIn(t, repo, "tag", "v1.0.1-rc.1")
	gitIn(t, repo, "switch", "-qc", "side")
	writeFile(t, filepath.Join(repo, "packaging", "bundled-tools.env"), "TOKVERA_VERSION=\n", 0o644)
	gitIn(t, repo, "commit", "-qam", "side")
	gitIn(t, repo, "tag", "v1.0.2")
	gitIn(t, repo, "switch", "-q", "main")
	draft := []string{"MAIN_REF=main", "PKG_GPG_KEY=x", "PKG_GPG_PASSPHRASE=x", "PKG_APK_KEY=x"}

	for _, c := range []struct {
		name  string
		env   []string
		args  []string
		code  int
		wants string
	}{
		{"draft ok", draft, []string{"draft", "v1.0.0"}, 0, "preflight: draft v1.0.0 ok"},
		{"rc ok", draft, []string{"draft", "v1.0.1-rc.1"}, 0, "ok"},
		{"publish needs the token", draft, []string{"publish", "v1.0.0"}, 1, "secret PUBLISHER_TOKEN is not set"},
		{"publish ok", append(draft, "PUBLISHER_TOKEN=x"), []string{"publish", "v1.0.0"}, 0, "ok"},
		{"private", append(draft, "REPO_PRIVATE=true"), []string{"draft", "v1.0.0"}, 1, "the repository is private"},
		{"bad tag", draft, []string{"draft", "1.0.0"}, 1, "is not vMAJOR.MINOR.PATCH"},
		{"unknown tag", draft, []string{"draft", "v9.9.9"}, 1, "no tag v9.9.9"},
		{"off main, unpinned", draft, []string{"draft", "v1.0.2"}, 1, "is not on main"},
		{"missing secret", []string{"MAIN_REF=main"}, []string{"draft", "v1.0.0"}, 1, "secret PKG_APK_KEY is not set"},
		{"bad phase", draft, []string{"ship", "v1.0.0"}, 2, "is not draft or publish"},
	} {
		out, code := script(t, repo, c.env, "preflight.sh", c.args...)
		if code != c.code || !strings.Contains(out, c.wants) {
			t.Errorf("%s: exit %d, want %d with %q:\n%s", c.name, code, c.code, c.wants, out)
		}
	}
	// The side tag's pins are empty: refused for that reason too.
	if out, _ := script(t, repo, draft, "preflight.sh", "draft", "v1.0.2"); !strings.Contains(out, "does not pin released Tokvera and Sievant") {
		t.Errorf("unpinned tools at the tag were not refused:\n%s", out)
	}
	// A secret's value is never printed.
	if out, _ := script(t, repo, []string{"MAIN_REF=main", "PKG_GPG_KEY=s3cr3t-value", "PKG_GPG_PASSPHRASE=x"}, "preflight.sh", "draft", "v1.0.0"); strings.Contains(out, "s3cr3t-value") {
		t.Error("preflight printed a secret")
	}
}

func TestWriteKeys(t *testing.T) {
	need(t, "gpg", "openssl")
	work := t.TempDir()
	gnupg := filepath.Join(work, "gnupg")
	if err := os.Mkdir(gnupg, 0o700); err != nil {
		t.Fatal(err)
	}
	gpg := func(args ...string) []byte {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "gpg", append([]string{"--batch", "--quiet", "--pinentry-mode", "loopback", "--passphrase", ""}, args...)...)
		cmd.Env = append(os.Environ(), "GNUPGHOME="+gnupg)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("gpg %v: %v", args, err)
		}
		return out
	}
	for _, n := range []string{"a", "b"} {
		gpg("--quick-gen-key", n+" <"+n+"@invalid>", "rsa2048", "sign", "1d")
		writeFile(t, filepath.Join(work, "gpg-"+n+".asc"), string(gpg("--armor", "--export-secret-keys", n+"@invalid")), 0o600)
		writeFile(t, filepath.Join(work, "gpg-"+n+".pub"), string(gpg("--armor", "--export", n+"@invalid")), 0o644)
	}
	rsaPair := func(n string) {
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		priv, _ := x509.MarshalPKCS8PrivateKey(k)
		pub, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
		writeFile(t, filepath.Join(work, "apk-"+n+".rsa"), string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: priv})), 0o600)
		writeFile(t, filepath.Join(work, "apk-"+n+".pub"), string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub})), 0o644)
	}
	rsaPair("a")
	rsaPair("b")
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(work, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	env := func(gpgKey, apkKey string) []string {
		return []string{
			"GPG_PUB=" + filepath.Join(work, "gpg-a.pub"), "APK_PUB=" + filepath.Join(work, "apk-a.pub"),
			"PKG_GPG_KEY=" + read(gpgKey), "PKG_APK_KEY=" + read(apkKey),
		}
	}

	keys := filepath.Join(work, "keys")
	out, code := script(t, work, env("gpg-a.asc", "apk-a.rsa"), "write-keys.sh", keys)
	if code != 0 {
		t.Fatalf("matching keys refused (%d):\n%s", code, out)
	}
	if !strings.Contains(out, "PKG_GPG_KEY_FILE="+keys+"/pkg-gpg.asc\n") || !strings.Contains(out, "PKG_APK_KEY_FILE="+keys+"/pkg-apk.rsa\n") {
		t.Errorf("no key file lines:\n%s", out)
	}
	for _, f := range []string{"pkg-gpg.asc", "pkg-apk.rsa"} {
		st, err := os.Stat(filepath.Join(keys, f))
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Errorf("%s: %v mode %v, want 0600", f, err, st)
		}
	}
	if out, code := script(t, work, env("gpg-a.asc", "apk-a.rsa"), "write-keys.sh", "--check"); code != 0 || strings.Contains(out, "_FILE=") {
		t.Errorf("--check (%d) printed key files or failed:\n%s", code, out)
	}
	for name, e := range map[string][]string{
		"wrong gpg key": env("gpg-b.asc", "apk-a.rsa"),
		"wrong apk key": env("gpg-a.asc", "apk-b.rsa"),
		"public as gpg": env("gpg-a.pub", "apk-a.rsa"),
		"public as apk": env("gpg-a.asc", "apk-a.pub"),
	} {
		dir := filepath.Join(work, strings.ReplaceAll(name, " ", "-"))
		out, code := script(t, work, e, "write-keys.sh", dir)
		if code != 1 {
			t.Errorf("%s: exit %d, want 1:\n%s", name, code, out)
		}
		if strings.Contains(out, "PRIVATE KEY") || strings.Contains(out, "_FILE=") {
			t.Errorf("%s: printed key material or a key file:\n%s", name, out)
		}
		if left, _ := filepath.Glob(filepath.Join(dir, "*")); len(left) != 0 {
			t.Errorf("%s: left %v behind", name, left)
		}
	}
	// The defaults are this repository's public keys.
	src := string(root(t, "scripts/release/write-keys.sh"))
	for _, p := range []string{"packaging/keys/halotrcer-packages.asc", "packaging/keys/satyajit.rajaraman@halotrcer.com-halotrcer.rsa.pub"} {
		if !strings.Contains(src, "$repo/"+p) {
			t.Errorf("write-keys.sh does not default to %s", p)
		}
	}
}

func TestCompareArchives(t *testing.T) {
	dir := t.TempDir()
	a := strings.Repeat("a", 64)
	b := strings.Repeat("b", 64)
	c := strings.Repeat("c", 64)
	published := a + "  halotrcer_1.0.0_linux_amd64.tar.gz\n" + b + "  halotrcer_1.0.0_windows_arm64.zip\n" + c + "  halotrcer_1.0.0_amd64.deb\n"
	writeFile(t, filepath.Join(dir, "published"), published, 0o644)
	for _, x := range []struct {
		name, built string
		code        int
	}{
		{"same, packages ignored", b + "  halotrcer_1.0.0_windows_arm64.zip\n" + a + "  halotrcer_1.0.0_linux_amd64.tar.gz\n" + a + "  halotrcer_1.0.0_amd64.deb\n", 0},
		{"changed archive", c + "  halotrcer_1.0.0_linux_amd64.tar.gz\n" + b + "  halotrcer_1.0.0_windows_arm64.zip\n", 1},
		{"missing archive", a + "  halotrcer_1.0.0_linux_amd64.tar.gz\n", 1},
		{"extra archive", published + c + "  halotrcer_1.0.0_darwin_arm64.tar.gz\n", 1},
		{"no archives", c + "  halotrcer_1.0.0_amd64.deb\n", 1},
	} {
		writeFile(t, filepath.Join(dir, "built"), x.built, 0o644)
		if out, code := script(t, dir, nil, "compare-archives.sh", "built", "published"); code != x.code {
			t.Errorf("%s: exit %d, want %d:\n%s", x.name, code, x.code, out)
		}
	}
}

func TestImageSubject(t *testing.T) {
	need(t, "jq")
	dir := t.TempDir()
	img := "ghcr.io/rajasatyajit/halotrcer"
	art := func(name, digest string) string {
		return `{"type":"Docker Image","name":"` + name + `","extra":{"Digest":"` + digest + `"}}`
	}
	for _, x := range []struct {
		name, json string
		code       int
	}{
		{"two tags, one digest", "[" + art(img+":v1.0.0", digestA) + "," + art(img+":latest", digestA) + `,{"type":"Archive","name":"x"}]`, 0},
		{"two digests", "[" + art(img+":v1.0.0", digestA) + "," + art(img+":latest", "sha256:"+strings.Repeat("b", 64)) + "]", 1},
		{"other image only", "[" + art(img+"-other:v1", digestA) + "]", 1},
		{"not sha256", "[" + art(img+":v1.0.0", "md5:abc") + "]", 1},
	} {
		writeFile(t, filepath.Join(dir, "artifacts.json"), x.json, 0o644)
		out, code := script(t, dir, nil, "image-subject.sh", "artifacts.json", img)
		if code != x.code || (code == 0 && out != digestA+"\n") {
			t.Errorf("%s: exit %d output %q, want %d", x.name, code, out, x.code)
		}
	}
}
