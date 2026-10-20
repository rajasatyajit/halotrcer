package install

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// release is a fake GitHub release server for one or more tools.
type release struct {
	srv      *httptest.Server
	files    map[string][]byte // path -> body
	requests []string
}

func tarGz(t *testing.T, name string, body []byte) []byte {
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	for _, f := range []struct {
		n string
		b []byte
	}{{"LICENSE", []byte("license")}, {name, body}} {
		if err := tw.WriteHeader(&tar.Header{Name: f.n, Mode: 0o755, Size: int64(len(f.b)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(f.b); err != nil {
			t.Fatal(err)
		}
	}
	_ = tw.Close()
	_ = gz.Close()
	return b.Bytes()
}

func zipOf(t *testing.T, name string, body []byte) []byte {
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write(body)
	_ = zw.Close()
	return b.Bytes()
}

func fakeBinary(name, version string) []byte {
	return []byte("#!/bin/sh\necho '" + name + " " + version + " (commit abc1234)'\n")
}

func newRelease(t *testing.T, tool Tool, tag string, p Platform) *release {
	r := &release{files: map[string][]byte{}}
	archive := ArchiveName(tool, tag, p)
	var data []byte
	if p.OS == "windows" {
		data = zipOf(t, tool.Name+".exe", fakeBinary(tool.Name, tag))
	} else {
		data = tarGz(t, tool.Name, fakeBinary(tool.Name, tag))
	}
	sum := sha256.Sum256(data)
	base := "/" + tool.Repo + "/releases/download/" + tag + "/"
	r.files[base+archive] = data
	r.files[base+"checksums.txt"] = []byte(fmt.Sprintf("%s  other_file.tar.gz\n%s  %s\n", strings.Repeat("0", 64), hex.EncodeToString(sum[:]), archive))
	r.files[base+"checksums.txt.sigstore.json"] = []byte(`{"bundle":"fake"}`)
	r.files["/api/"+tool.Repo+"/releases/latest"] = []byte(`{"tag_name":"` + tag + `"}`)
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.requests = append(r.requests, req.URL.Path)
		b, ok := r.files[req.URL.Path]
		if !ok {
			http.NotFound(w, req)
			return
		}
		_, _ = w.Write(b)
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *release) downloader(t *testing.T) *Downloader {
	d, err := NewDownloader(func(k string) string {
		switch k {
		case "HALOTRCER_INSTALL_DOWNLOAD_BASE":
			return r.srv.URL
		case "HALOTRCER_INSTALL_API_BASE":
			return r.srv.URL + "/api"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func testSystem(home string, have ...string) System {
	return System{
		OS: runtime.GOOS, Arch: runtime.GOARCH, Home: home,
		Getenv: func(k string) string {
			if k == "PATH" {
				return filepath.Join(home, ".local", "bin")
			}
			return ""
		},
		LookPath: func(n string) (string, error) {
			for _, h := range have {
				if h == n {
					return "/usr/bin/" + n, nil
				}
			}
			return "", errors.New("not found")
		},
		Run: func(context.Context, string, ...string) (int, string) { return 1, "" },
	}
}

func newInstaller(t *testing.T, sys System, dl *Downloader) (*Installer, *bytes.Buffer, *[]string) {
	var out bytes.Buffer
	var ran []string
	in := &Installer{
		Sys: sys, DL: dl, Out: &out, Err: &out,
		Exec: func(_ context.Context, argv []string) error { ran = append(ran, strings.Join(argv, " ")); return nil },
		Find: func(string) (string, error) { return "", errors.New("not installed") },
		Version: func(ctx context.Context, tool Tool, path string) string {
			b, err := os.ReadFile(path)
			if err != nil {
				return ""
			}
			// The fake binaries are shell scripts echoing their version.
			return ParseVersionOutput(tool.Name, strings.TrimSuffix(strings.SplitN(string(b), "'", 3)[1], ""))
		},
		StatePath: filepath.Join(sys.Home, "state", "installed.json"),
	}
	return in, &out, &ran
}

func TestDirectInstallVerifies(t *testing.T) {
	home := t.TempDir()
	sys := testSystem(home)
	sys.OS, sys.Arch = "linux", "amd64"
	rel := newRelease(t, Tokvera, "v0.2.0", Platform{"linux", "amd64"})
	in, out, _ := newInstaller(t, sys, rel.downloader(t))
	var cosignArgs []string
	in.Cosign = func(_ context.Context, args []string) error { cosignArgs = args; return nil }
	o, err := in.Install(context.Background(), Tokvera, Options{})
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	want := filepath.Join(home, ".local", "bin", "tokvera")
	if o.Action != ActionInstalled || o.Path != want || o.Version != "v0.2.0" || o.Signature != "verified" {
		t.Errorf("%+v", o)
	}
	joined := strings.Join(cosignArgs, " ")
	if !strings.Contains(joined, `--certificate-identity-regexp ^https://github\.com/rajasatyajit/tokvera/\.github/workflows/release\.yml@refs/tags/v`) ||
		!strings.Contains(joined, "--certificate-oidc-issuer https://token.actions.githubusercontent.com") {
		t.Errorf("cosign args: %s", joined)
	}
	st, err := LoadState(in.StatePath)
	if err != nil || st.Tools["tokvera"].Channel != ChannelDirect || st.Tools["tokvera"].Path != want {
		t.Errorf("state %+v %v", st, err)
	}
	// Signature fetched before the archive.
	var sigAt, archiveAt int
	for i, p := range rel.requests {
		if strings.HasSuffix(p, ".sigstore.json") {
			sigAt = i
		}
		if strings.HasSuffix(p, ".tar.gz") {
			archiveAt = i
		}
	}
	if sigAt > archiveAt {
		t.Errorf("archive fetched before the signature: %v", rel.requests)
	}
}

func TestDirectRefusals(t *testing.T) {
	home := t.TempDir()
	sys := testSystem(home)
	sys.OS, sys.Arch = "linux", "amd64"
	p := Platform{"linux", "amd64"}

	rel := newRelease(t, Sievant, "v0.2.0", p)
	in, _, _ := newInstaller(t, sys, rel.downloader(t))
	if _, err := in.Install(context.Background(), Sievant, Options{RequireCosign: true}); err == nil || !strings.Contains(err.Error(), "cosign is not installed") {
		t.Errorf("require-cosign without cosign: %v", err)
	}
	in.Cosign = func(context.Context, []string) error { return errors.New("no matching signatures") }
	if _, err := in.Install(context.Background(), Sievant, Options{}); err == nil || !strings.Contains(err.Error(), "did not verify") {
		t.Errorf("bad signature: %v", err)
	}
	in.Cosign = nil
	// Tamper with the archive after checksums.txt was made.
	for k, v := range rel.files {
		if strings.HasSuffix(k, ".tar.gz") {
			rel.files[k] = append(append([]byte{}, v...), 0)
		}
	}
	if _, err := in.Install(context.Background(), Sievant, Options{}); err == nil || !strings.Contains(err.Error(), "SHA-256 does not match") {
		t.Errorf("tampered archive: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "bin", "sievant")); err == nil {
		t.Error("something was installed despite a refusal")
	}
	if _, err := in.Install(context.Background(), Sievant, Options{Version: "latest-ish"}); err == nil {
		t.Error("bad version accepted")
	}
	if _, err := NewDownloader(func(string) string { return "http://mirror.example.com" }); err == nil {
		t.Error("plain http mirror accepted")
	}
}

func TestWindowsZip(t *testing.T) {
	home := t.TempDir()
	sys := testSystem(home)
	sys.OS, sys.Arch = "windows", "arm64"
	rel := newRelease(t, Tokvera, "v0.2.1", Platform{"windows", "arm64"})
	d := rel.downloader(t)
	v, err := d.Fetch(context.Background(), Tokvera, "v0.2.1", Platform{"windows", "arm64"}, nil, false, t.TempDir())
	if err != nil || !bytes.Contains(v.Binary, []byte("tokvera v0.2.1")) {
		t.Fatalf("%v", err)
	}
	if got := sys.DefaultDir(Tokvera); !strings.HasSuffix(filepath.ToSlash(got), "AppData/Local/Programs/tokvera") {
		t.Errorf("windows dir %q", got)
	}
}

func TestUpToDateAndUpgrade(t *testing.T) {
	home := t.TempDir()
	sys := testSystem(home)
	sys.OS, sys.Arch = "linux", "amd64"
	rel := newRelease(t, Tokvera, "v0.2.0", Platform{"linux", "amd64"})
	in, _, _ := newInstaller(t, sys, rel.downloader(t))
	bin := filepath.Join(home, ".local", "bin")
	existing, err := Place(bin, "tokvera", fakeBinary("tokvera", "v0.2.0"))
	if err != nil {
		t.Fatal(err)
	}
	in.Find = func(string) (string, error) { return existing, nil }
	o, err := in.Install(context.Background(), Tokvera, Options{})
	if err != nil || o.Action != ActionUpToDate {
		t.Errorf("%+v %v", o, err)
	}
	if _, err := Place(bin, "tokvera", fakeBinary("tokvera", "v0.1.9")); err != nil {
		t.Fatal(err)
	}
	o, err = in.Install(context.Background(), Tokvera, Options{})
	if err != nil || o.Action != ActionUpgraded {
		t.Fatalf("%+v %v", o, err)
	}
	st, _ := LoadState(in.StatePath)
	if _, ok := st.Tools["tokvera"]; ok {
		t.Error("upgrading a tool the user installed recorded it as Halotrcer's")
	}
	if _, err := in.Install(context.Background(), Tokvera, Options{Via: ChannelBrew}); err == nil || !strings.Contains(err.Error(), "already installed by direct") {
		t.Errorf("switching channel: %v", err)
	}
}

func TestPackageManagers(t *testing.T) {
	home := t.TempDir()
	sys := testSystem(home, "brew", "scoop", "winget", "go")
	cases := []struct {
		os, via string
		want    []string
	}{
		{"darwin", "", []string{"brew install --cask rajasatyajit/tap/tokvera"}},
		{"windows", "", []string{"scoop bucket add rajasatyajit https://github.com/rajasatyajit/scoop-bucket", "scoop install rajasatyajit/tokvera"}},
		{"windows", "winget", []string{"winget install -e --id Halotrcer.Tokvera"}},
		{"linux", "go", []string{"go install github.com/rajasatyajit/tokvera/cmd/tokvera@latest"}},
	}
	for _, c := range cases {
		s := sys
		s.OS = c.os
		in, _, ran := newInstaller(t, s, nil)
		o, err := in.Install(context.Background(), Tokvera, Options{Via: c.via})
		if err != nil {
			t.Fatalf("%s/%s: %v", c.os, c.via, err)
		}
		if strings.Join(*ran, "|") != strings.Join(c.want, "|") {
			t.Errorf("%s/%s ran %q, want %q", c.os, c.via, *ran, c.want)
		}
		if o.Action != ActionInstalled {
			t.Errorf("%+v", o)
		}
	}
	s := testSystem(home)
	s.OS = "darwin"
	in, _, _ := newInstaller(t, s, nil)
	if _, err := in.Install(context.Background(), Tokvera, Options{Via: ChannelBrew}); err == nil || !strings.Contains(err.Error(), "brew is not installed") {
		t.Errorf("missing brew: %v", err)
	}
	if got := s.DefaultChannel(); got != ChannelDirect {
		t.Errorf("darwin without brew: %s", got)
	}
	s.OS = "linux"
	s.LookPath = testSystem(home, "apt", "dnf").LookPath
	if got := s.DefaultChannel(); got != ChannelDirect {
		t.Errorf("linux default %s: root-only channels must never be the default", got)
	}
}

func TestManualNeverRuns(t *testing.T) {
	home := t.TempDir()
	sys := testSystem(home, "apt", "dnf", "apk")
	sys.OS, sys.Arch = "linux", "arm64"
	rel := newRelease(t, Sievant, "v0.2.0", Platform{"linux", "arm64"})
	for _, ch := range []string{ChannelApt, ChannelDnf, ChannelApk} {
		in, out, ran := newInstaller(t, sys, rel.downloader(t))
		o, err := in.Install(context.Background(), Sievant, Options{Via: ch})
		if err != nil || o.Action != ActionManual || len(*ran) != 0 {
			t.Fatalf("%s: %+v %v ran %v", ch, o, err, *ran)
		}
		if !strings.Contains(out.String(), "never runs sudo") {
			t.Errorf("%s: %s", ch, out)
		}
	}
	// The exact documented commands (tokvera and sievant docs/install.md).
	apt := ManualCommands(ChannelApt, Sievant, false, false, "", "")
	wantApt := []string{
		"sudo install -d -m 0755 /etc/apt/keyrings",
		"sudo curl -fsSLo /etc/apt/keyrings/halotrcer-packages.asc https://rajasatyajit.github.io/packages/halotrcer-packages.asc",
		`echo "deb [signed-by=/etc/apt/keyrings/halotrcer-packages.asc] https://rajasatyajit.github.io/packages/apt stable main" | sudo tee /etc/apt/sources.list.d/sievant.list`,
		"sudo apt update && sudo apt install sievant",
	}
	if strings.Join(apt, "\n") != strings.Join(wantApt, "\n") {
		t.Errorf("apt:\n%s", strings.Join(apt, "\n"))
	}
	if got := ManualCommands(ChannelApt, Tokvera, false, true, "", ""); len(got) != 1 || got[0] != "sudo apt update && sudo apt install tokvera" {
		t.Errorf("configured apt: %q", got)
	}
	dnf := ManualCommands(ChannelDnf, Tokvera, false, false, "", "")
	if dnf[0] != "sudo curl -fsSLo /etc/yum.repos.d/tokvera.repo https://rajasatyajit.github.io/packages/rpm/tokvera.repo" || dnf[1] != "sudo dnf install tokvera" {
		t.Errorf("dnf: %q", dnf)
	}
	apk := ManualCommands(ChannelApk, Sievant, false, false, "v0.2.0", "aarch64")
	if apk[0] != "wget -O /etc/apk/keys/satyajit.rajaraman@halotrcer.com-sievant.rsa.pub https://raw.githubusercontent.com/rajasatyajit/sievant/main/packaging/keys/satyajit.rajaraman@halotrcer.com-sievant.rsa.pub" ||
		apk[2] != "apk add ./sievant_0.2.0_aarch64.apk" {
		t.Errorf("apk: %q", apk)
	}
}

func TestOwner(t *testing.T) {
	ctx := context.Background()
	home := "/home/u"
	s := testSystem(home, "dpkg")
	s.OS = "linux"
	s.Run = func(_ context.Context, name string, args ...string) (int, string) {
		if name == "dpkg" && args[1] == "/usr/bin/tokvera" {
			return 0, "tokvera: /usr/bin/tokvera"
		}
		return 1, ""
	}
	cases := map[string]string{
		"/opt/homebrew/Caskroom/tokvera/0.2.0/tokvera": ChannelBrew,
		"/usr/bin/tokvera":                                            ChannelApt,
		"/home/u/go/bin/tokvera":                                      ChannelGo,
		"/home/u/.local/bin/tokvera":                                  ChannelDirect,
		`C:/Users/u/scoop/shims/tokvera.exe`:                          ChannelScoop,
		`C:/Users/u/AppData/Local/Microsoft/WinGet/Links/tokvera.exe`: ChannelWinget,
	}
	for p, want := range cases {
		if got := s.Owner(ctx, Tokvera, p); got != want {
			t.Errorf("Owner(%s) = %s, want %s", p, got, want)
		}
	}
}

func TestUninstallOnlyWhatHalotrcerInstalled(t *testing.T) {
	home := t.TempDir()
	sys := testSystem(home)
	in, out, ran := newInstaller(t, sys, nil)
	mine, _ := Place(filepath.Join(home, "bin"), "sievant", []byte("x"))
	theirs, _ := Place(filepath.Join(home, "other"), "tokvera", []byte("y"))
	st := &State{Version: 1, Tools: map[string]Record{
		"sievant":                  {Channel: ChannelDirect, Path: mine},
		"tokvera-brew-placeholder": {Channel: ChannelBrew},
	}}
	if err := st.Save(in.StatePath); err != nil {
		t.Fatal(err)
	}
	in.Find = func(name string) (string, error) {
		if name == "tokvera" {
			return theirs, nil
		}
		return "", errors.New("x")
	}
	removed, err := in.Uninstall(context.Background(), Tokvera, false)
	if err != nil || removed {
		t.Errorf("tokvera: removed=%v %v", removed, err)
	}
	if _, err := os.Stat(theirs); err != nil {
		t.Error("a separately installed tool was removed")
	}
	if !strings.Contains(out.String(), "not installed by Halotrcer: left alone") {
		t.Errorf("%s", out)
	}
	if removed, err := in.Uninstall(context.Background(), Sievant, false); err != nil || !removed {
		t.Errorf("sievant: %v %v", removed, err)
	}
	if _, err := os.Stat(mine); err == nil {
		t.Error("Halotrcer's install was not removed")
	}
	st, _ = LoadState(in.StatePath)
	if _, ok := st.Tools["sievant"]; ok {
		t.Error("record kept")
	}
	_ = st.Save(in.StatePath)
	st.Tools["sievant"] = Record{Channel: ChannelBrew}
	_ = st.Save(in.StatePath)
	if _, err := in.Uninstall(context.Background(), Sievant, false); err != nil || len(*ran) != 1 || (*ran)[0] != "brew uninstall --cask sievant" {
		t.Errorf("brew uninstall: %v %v", *ran, err)
	}
}

func TestRosetta(t *testing.T) {
	s := testSystem("/h", "sysctl")
	s.OS, s.Arch = "darwin", "amd64"
	s.Run = func(_ context.Context, name string, args ...string) (int, string) { return 0, "1\n" }
	if p := s.Platform(context.Background()); p.Arch != "arm64" {
		t.Errorf("rosetta: %+v", p)
	}
	s.Run = func(context.Context, string, ...string) (int, string) { return 0, "0\n" }
	if p := s.Platform(context.Background()); p.Arch != "amd64" {
		t.Errorf("native intel: %+v", p)
	}
}

func TestStateRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s", "installed.json")
	st, err := LoadState(p)
	if err != nil || len(st.Tools) != 0 {
		t.Fatal(err)
	}
	st.Tools["tokvera"] = Record{Channel: ChannelBrew, Version: "v0.2.0"}
	if err := st.Save(p); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(p)
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode())
	}
	got, err := LoadState(p)
	if err != nil || got.Tools["tokvera"].Version != "v0.2.0" {
		t.Errorf("%+v %v", got, err)
	}
	if err := os.WriteFile(p, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadState(p); err == nil {
		t.Error("corrupt state accepted")
	}
}

func TestNoRelease(t *testing.T) {
	rel := newRelease(t, Tokvera, "v0.2.0", Platform{"linux", "amd64"})
	delete(rel.files, "/api/tokvera/releases/latest")
	_, err := rel.downloader(t).Latest(context.Background(), Tokvera)
	if err == nil || !strings.Contains(err.Error(), "no published release yet") {
		t.Errorf("%v", err)
	}
}
