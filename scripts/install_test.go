package scripts

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
)

// These tests run scripts/install.sh against a fake GitHub on loopback, with
// a hermetic PATH: a directory holding links to exactly the tools the script
// may use, plus fake uname/sysctl/ldd/cosign where a case needs them, as
// Tokvera's and Sievant's installer tests do. Nothing reaches the network.
//
// The release's halotrcer is a fake that logs how the script calls it and
// drops a fake tool into --dir; TestInstallEndToEnd uses the real halotrcer,
// built from this tree, to install the tools from their own fake releases.
//
// The script runs under every shell named in HALOTRCER_TEST_SHELLS (space
// separated), default "sh" plus dash and busybox when they are on PATH.

const instVersion = "v1.2.3"

// The real tools the script (and the fake halotrcer) may use.
var instBaseTools = []string{
	"sh", "cat", "awk", "sed", "grep", "head", "tar", "gzip", "mktemp", "rm",
	"mkdir", "cp", "mv", "chmod", "ls", "sha256sum", "curl", "printf", "env",
}

func instShells() []string {
	if v := os.Getenv("HALOTRCER_TEST_SHELLS"); v != "" {
		return strings.Fields(v)
	}
	out := []string{"sh"}
	for _, s := range []string{"dash", "busybox"} {
		if _, err := exec.LookPath(s); err == nil {
			out = append(out, s)
		}
	}
	return out
}

// instRelease is a fake GitHub for the owner's repositories:
// /api/<repo>/releases/latest and /api/<repo>/releases, and
// /dl/<repo>/releases/download/<tag>/<file>. Every request path is recorded.
type instRelease struct {
	srv      *httptest.Server
	mu       sync.Mutex
	requests []string
	files    map[string][]byte // "<repo>/<tag>/<file>"
	latest   map[string]string // repo -> tag
	list     map[string][]string
}

// fakeHalotrcer is the release's halotrcer: `version` prints the first line
// the real one prints; `install TOOL ... --dir DIR ...` appends its arguments
// to $INST_LOG, writes a fake TOOL into DIR and prints halotrcer's "next:"
// line; $INST_FAIL makes it fail.
func fakeHalotrcer(version string) string {
	return `#!/bin/sh
case "$1" in
version) echo 'halotrcer ` + version + ` (commit abc1234)' ;;
install)
	printf '%s\n' "$*" >>"$INST_LOG"
	tool="$2"
	dir=""
	shift 2
	while [ $# -gt 0 ]; do
		case "$1" in --dir) dir="$2"; shift 2 ;; *) shift ;; esac
	done
	[ -z "${INST_FAIL:-}" ] || { echo "halotrcer install $tool: boom" >&2; exit 1; }
	printf '#!/bin/sh\necho "%s v0.2.0 (commit fake)"\n' "$tool" >"$dir/$tool"
	chmod 0755 "$dir/$tool"
	echo "$tool: installed v0.2.0 at $dir/$tool"
	echo "next: halotrcer check --target <inference-url>"
	;;
esac
`
}

func instTarGz(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct {
		n    string
		mode int64
		b    []byte
	}{{name, 0o755, body}, {"LICENSE", 0o644, []byte("Apache\n")}} {
		if err := tw.WriteHeader(&tar.Header{Name: f.n, Mode: f.mode, Size: int64(len(f.b))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(f.b); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func newInstRelease(t *testing.T) *instRelease {
	t.Helper()
	r := &instRelease{files: map[string][]byte{}, latest: map[string]string{}, list: map[string][]string{}}
	r.add(t, "halotrcer", instVersion, "linux", "amd64", []byte(fakeHalotrcer(instVersion)))
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		r.requests = append(r.requests, req.URL.Path)
		r.mu.Unlock()
		p := req.URL.Path
		if rest, ok := strings.CutPrefix(p, "/api/"); ok {
			repo, what, _ := strings.Cut(rest, "/")
			r.mu.Lock()
			latest, list := r.latest[repo], r.list[repo]
			r.mu.Unlock()
			switch {
			case what == "releases/latest" && latest != "":
				_, _ = fmt.Fprintf(w, `{"url":"x","tag_name": "%s","name":"%s"}`, latest, latest)
				return
			case what == "releases" && list != nil:
				var rels []map[string]any
				for _, tag := range list {
					rels = append(rels, map[string]any{"tag_name": tag, "prerelease": strings.Contains(tag, "-")})
				}
				_ = json.NewEncoder(w).Encode(rels)
				return
			}
		}
		if rest, ok := strings.CutPrefix(p, "/dl/"); ok {
			repo, after, _ := strings.Cut(rest, "/releases/download/")
			r.mu.Lock()
			b, found := r.files[repo+"/"+after]
			r.mu.Unlock()
			if found {
				_, _ = w.Write(b)
				return
			}
		}
		http.NotFound(w, req)
	}))
	t.Cleanup(r.srv.Close)
	return r
}

// add publishes repo's tag for one platform: the archive with name=repo's
// binary, and checksums.txt and its (fake) signature bundle.
func (r *instRelease) add(t *testing.T, repo, tag, osName, arch string, bin []byte) {
	t.Helper()
	archive := fmt.Sprintf("%s_%s_%s_%s.tar.gz", repo, strings.TrimPrefix(tag, "v"), osName, arch)
	tgz := instTarGz(t, repo, bin)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.files[repo+"/"+tag+"/"+archive] = tgz
	r.files[repo+"/"+tag+"/checksums.txt"] = fmt.Appendf(nil, "%x  %s\n%x  other_file.tar.gz\n", sha256.Sum256(tgz), archive, sha256.Sum256([]byte("x")))
	r.files[repo+"/"+tag+"/checksums.txt.sigstore.json"] = []byte(`{"fake":"bundle"}`)
	r.latest[repo] = tag
}

func (r *instRelease) set(key string, b []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if b == nil {
		delete(r.files, key)
		return
	}
	r.files[key] = b
}

func (r *instRelease) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.requests)
}

// instEnv describes one run: the fake platform and the optional tools present.
type instEnv struct {
	unameS, unameM string
	rosetta        bool   // sysctl.proc_translated = 1
	musl           bool   // ldd reports musl
	cosign         string // "" absent, "ok" verifies, "bad" fails
	noCurl         bool   // curl absent (wget used)
	downloadBase   string // overrides the fake server's download base
	apiBase        string // overrides the fake server's API base
	failInstall    bool   // the fake halotrcer's install fails
	extraPath      string // a directory after the hermetic bin on PATH
}

type instResult struct {
	code      int
	stderr    string
	dir       string // default install dir ($HOME/.local/bin)
	home      string
	cosignLog string
	calls     []string // the fake halotrcer's install calls
}

func instRun(t *testing.T, shell string, r *instRelease, e instEnv, args ...string) instResult {
	t.Helper()
	if e.unameS == "" {
		e.unameS, e.unameM = "Linux", "x86_64"
	}
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	home := filepath.Join(root, "home")
	for _, d := range []string{bin, home} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	tools := slices.Clone(instBaseTools)
	if shell != "sh" {
		tools = append(tools, shell)
	}
	if _, err := exec.LookPath("curl"); err != nil {
		e.noCurl = true
	}
	if e.noCurl {
		tools = slices.DeleteFunc(tools, func(s string) bool { return s == "curl" })
		tools = append(tools, "wget")
	}
	for _, tool := range tools {
		p, err := exec.LookPath(tool)
		if err != nil {
			if tool == "printf" || tool == "env" {
				continue // shell builtins on every tested shell
			}
			t.Skipf("%s not on PATH", tool)
		}
		if err := os.Symlink(p, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("uname", fmt.Sprintf("case \"$1\" in -s) echo '%s';; -m) echo '%s';; *) echo '%s';; esac\n", e.unameS, e.unameM, e.unameS))
	if e.rosetta {
		write("sysctl", "[ \"$2\" = sysctl.proc_translated ] && echo 1\n")
	}
	if e.musl {
		write("ldd", "echo 'musl libc (x86_64)' >&2; exit 1\n")
	} else {
		write("ldd", "echo 'ldd (GNU libc) 2.41'\n")
	}
	// sudo must never run: a fake that records it.
	sudoLog := filepath.Join(root, "sudo.log")
	write("sudo", fmt.Sprintf("echo \"$*\" >> '%s'\nexit 1\n", sudoLog))
	cosignLog := filepath.Join(root, "cosign.log")
	switch e.cosign {
	case "ok":
		write("cosign", fmt.Sprintf("printf '%%s\\n' \"$@\" > '%s'\nexit 0\n", cosignLog))
	case "bad":
		write("cosign", fmt.Sprintf("printf '%%s\\n' \"$@\" > '%s'\necho 'error: no matching signatures' >&2\nexit 1\n", cosignLog))
	}

	script, err := filepath.Abs("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	argv := []string{script}
	if shell == "busybox" {
		argv = []string{"sh", script}
	}
	path := bin
	if e.extraPath != "" {
		path += ":" + e.extraPath
	}
	callLog := filepath.Join(root, "calls.log")
	cmd := exec.CommandContext(t.Context(), filepath.Join(bin, shell), append(argv, args...)...)
	cmd.Env = []string{"PATH=" + path, "HOME=" + home, "INST_LOG=" + callLog}
	if e.failInstall {
		cmd.Env = append(cmd.Env, "INST_FAIL=1")
	}
	if r != nil {
		dl, api := r.srv.URL+"/dl", r.srv.URL+"/api"
		if e.downloadBase != "" {
			dl = e.downloadBase
		}
		if e.apiBase != "" {
			api = e.apiBase
		}
		cmd.Env = append(cmd.Env, "HALOTRCER_INSTALL_DOWNLOAD_BASE="+dl, "HALOTRCER_INSTALL_API_BASE="+api)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &stderr
	err = cmd.Run()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(sudoLog); len(b) > 0 {
		t.Errorf("the script ran sudo: %s", b)
	}
	log, _ := os.ReadFile(cosignLog)
	calls, _ := os.ReadFile(callLog)
	var cl []string
	if s := strings.TrimSpace(string(calls)); s != "" {
		cl = strings.Split(s, "\n")
	}
	return instResult{code: code, stderr: stderr.String(), dir: filepath.Join(home, ".local", "bin"), home: home, cosignLog: string(log), calls: cl}
}

func eachInstShell(t *testing.T, f func(t *testing.T, shell string)) {
	if runtime.GOOS == "windows" {
		t.Skip("install.sh is the Linux/macOS installer")
	}
	shells := instShells()
	// CI tests every shell: a runner without one fails here rather than
	// passing on fewer (scripts/ci/runner-tools.sh checks for them too).
	if os.Getenv("CI") != "" && os.Getenv("HALOTRCER_TEST_SHELLS") == "" && len(shells) != 3 {
		t.Fatalf("CI runs install.sh under sh, dash and busybox; this runner has only %v", shells)
	}
	for _, sh := range shells {
		t.Run(sh, func(t *testing.T) { f(t, sh) })
	}
}

// firstLine runs dir/name version and returns its first line ("" when absent).
func firstLine(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	info, err := os.Stat(p)
	if err != nil {
		return ""
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("%s mode %v, want 0755", p, info.Mode().Perm())
	}
	out, err := exec.CommandContext(t.Context(), p, "version").CombinedOutput()
	if err != nil {
		t.Fatalf("%s does not run: %v", p, err)
	}
	return strings.SplitN(string(out), "\n", 2)[0]
}

func instNoLeftovers(t *testing.T, dir string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".halotrcer.new.") {
			t.Errorf("leftover temporary file %s in %s", e.Name(), dir)
		}
	}
}

const halArchive = "halotrcer_1.2.3_linux_amd64.tar.gz"

func TestInstallAllVerifiesHalotrcerThenDelegatesTheTools(t *testing.T) {
	eachInstShell(t, func(t *testing.T, shell string) {
		r := newInstRelease(t)
		res := instRun(t, shell, r, instEnv{})
		if res.code != 0 {
			t.Fatalf("exit %d:\n%s", res.code, res.stderr)
		}
		if got := firstLine(t, res.dir, "halotrcer"); got != "halotrcer v1.2.3 (commit abc1234)" {
			t.Fatalf("installed %q", got)
		}
		want := []string{"/api/halotrcer/releases/latest", "/dl/halotrcer/releases/download/v1.2.3/" + halArchive, "/dl/halotrcer/releases/download/v1.2.3/checksums.txt"}
		if got := r.seen(); !slices.Equal(got, want) {
			t.Errorf("requests %v, want %v", got, want)
		}
		wantCalls := []string{
			"install tokvera --via direct --dir " + res.dir,
			"install sievant --via direct --dir " + res.dir,
		}
		if !slices.Equal(res.calls, wantCalls) {
			t.Errorf("halotrcer calls %q, want %q", res.calls, wantCalls)
		}
		for _, tool := range []string{"tokvera", "sievant"} {
			if firstLine(t, res.dir, tool) == "" {
				t.Errorf("%s not installed", tool)
			}
		}
		for _, s := range []string{"SHA-256 verified", "cosign not found", "is not on your PATH", "tokvera: installed v0.2.0", "next: halotrcer check --target <inference-url>"} {
			if !strings.Contains(res.stderr, s) {
				t.Errorf("output lacks %q:\n%s", s, res.stderr)
			}
		}
		// halotrcer's own "next:" line is replaced by the script's.
		if n := strings.Count(res.stderr, "next: "); n != 1 {
			t.Errorf("%d next: lines:\n%s", n, res.stderr)
		}
		instNoLeftovers(t, res.dir)
	})
}

func TestInstallOnlyOneToolKeepsNoHalotrcer(t *testing.T) {
	eachInstShell(t, func(t *testing.T, shell string) {
		r := newInstRelease(t)
		res := instRun(t, shell, r, instEnv{}, "--only", "sievant")
		if res.code != 0 {
			t.Fatalf("exit %d:\n%s", res.code, res.stderr)
		}
		if got := firstLine(t, res.dir, "halotrcer"); got != "" {
			t.Errorf("--only left halotrcer installed: %q", got)
		}
		if want := []string{"install sievant --via direct --dir " + res.dir}; !slices.Equal(res.calls, want) {
			t.Errorf("calls %q, want %q", res.calls, want)
		}
		if !strings.Contains(res.stderr, "next: sievant scan") || strings.Contains(res.stderr, "halotrcer check") {
			t.Errorf("next step:\n%s", res.stderr)
		}
	})
}

func TestInstallIsIdempotent(t *testing.T) {
	eachInstShell(t, func(t *testing.T, shell string) {
		r := newInstRelease(t)
		dir := filepath.Join(t.TempDir(), "bin")
		if res := instRun(t, shell, r, instEnv{}, "--version", "1.2.3", "--dir", dir); res.code != 0 {
			t.Fatalf("first run exit %d:\n%s", res.code, res.stderr)
		}
		before := len(r.seen())
		res := instRun(t, shell, r, instEnv{}, "--version", "v1.2.3", "--dir", dir)
		if res.code != 0 || !strings.Contains(res.stderr, "halotrcer v1.2.3 is already installed") {
			t.Fatalf("second run exit %d:\n%s", res.code, res.stderr)
		}
		if after := r.seen(); len(after) != before {
			t.Errorf("second run downloaded halotrcer again: %v", after[before:])
		}
		// The tools are still handed to halotrcer, which upgrades them in place.
		if len(res.calls) != 2 {
			t.Errorf("calls %q", res.calls)
		}
	})
}

func TestInstallLeavesAToolInstalledElsewhere(t *testing.T) {
	eachInstShell(t, func(t *testing.T, shell string) {
		r := newInstRelease(t)
		other := t.TempDir()
		if err := os.WriteFile(filepath.Join(other, "tokvera"), []byte("#!/bin/sh\necho 'tokvera v0.2.1 (commit brew)'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		res := instRun(t, shell, r, instEnv{extraPath: other})
		if res.code != 0 {
			t.Fatalf("exit %d:\n%s", res.code, res.stderr)
		}
		if want := []string{"install sievant --via direct --dir " + res.dir}; !slices.Equal(res.calls, want) {
			t.Errorf("calls %q, want only sievant", res.calls)
		}
		if !strings.Contains(res.stderr, "tokvera is already installed at "+filepath.Join(other, "tokvera")+"; leaving it") {
			t.Errorf("output:\n%s", res.stderr)
		}
	})
}

func TestInstallRefusesABadChecksum(t *testing.T) {
	eachInstShell(t, func(t *testing.T, shell string) {
		key := "halotrcer/v1.2.3/"
		cases := map[string]struct {
			tamper func(r *instRelease)
			msg    string
		}{
			"mismatch": {func(r *instRelease) {
				r.set(key+halArchive, instTarGz(t, "halotrcer", []byte(fakeHalotrcer("v6.6.6"))))
			}, "SHA-256 mismatch for " + halArchive},
			"not listed": {func(r *instRelease) {
				r.set(key+"checksums.txt", []byte("0000  "+halArchive+".other\n"))
			}, halArchive + " is not listed in checksums.txt"},
			"no checksum file": {func(r *instRelease) { r.set(key+"checksums.txt", nil) },
				"download failed: <srv>/dl/halotrcer/releases/download/v1.2.3/checksums.txt"},
		}
		for name, c := range cases {
			t.Run(name, func(t *testing.T) {
				r := newInstRelease(t)
				c.tamper(r)
				dir := filepath.Join(t.TempDir(), "bin")
				res := instRun(t, shell, r, instEnv{}, "--version", "v1.2.3", "--dir", dir)
				if want := strings.ReplaceAll(c.msg, "<srv>", r.srv.URL); res.code != 1 || !strings.Contains(res.stderr, want) {
					t.Fatalf("exit %d (want 1), output lacks %q:\n%s", res.code, want, res.stderr)
				}
				if v := firstLine(t, dir, "halotrcer"); v != "" {
					t.Errorf("halotrcer was installed after a failed check: %q", v)
				}
				if len(res.calls) != 0 {
					t.Errorf("tools installed after a failed check: %q", res.calls)
				}
				instNoLeftovers(t, dir)
			})
		}
	})
}

func TestInstallNoVerifySkipsTheChecksAndPassesItOn(t *testing.T) {
	eachInstShell(t, func(t *testing.T, shell string) {
		r := newInstRelease(t)
		r.set("halotrcer/v1.2.3/checksums.txt", []byte("0000  "+halArchive+"\n"))
		dir := filepath.Join(t.TempDir(), "bin")
		res := instRun(t, shell, r, instEnv{cosign: "bad"}, "--version", "v1.2.3", "--dir", dir, "--no-verify")
		if res.code != 0 {
			t.Fatalf("exit %d:\n%s", res.code, res.stderr)
		}
		if !strings.Contains(res.stderr, "WARNING: --no-verify") {
			t.Errorf("no warning:\n%s", res.stderr)
		}
		if res.cosignLog != "" {
			t.Errorf("cosign ran under --no-verify: %s", res.cosignLog)
		}
		if slices.Contains(r.seen(), "/dl/halotrcer/releases/download/v1.2.3/checksums.txt") {
			t.Error("checksums.txt was downloaded under --no-verify")
		}
		for _, c := range res.calls {
			if !strings.HasSuffix(c, " --no-verify") {
				t.Errorf("halotrcer not told --no-verify: %q", c)
			}
		}
	})
}

func TestInstallVerifiesTheSignatureWhenCosignIsPresent(t *testing.T) {
	eachInstShell(t, func(t *testing.T, shell string) {
		r := newInstRelease(t)
		dir := filepath.Join(t.TempDir(), "bin")
		res := instRun(t, shell, r, instEnv{cosign: "ok"}, "--version", "v1.2.3", "--dir", dir, "--require-cosign")
		if res.code != 0 {
			t.Fatalf("exit %d:\n%s", res.code, res.stderr)
		}
		args := strings.Split(strings.TrimSpace(res.cosignLog), "\n")
		want := []string{
			"verify-blob", "--bundle", "<tmp>/checksums.txt.sigstore.json",
			"--certificate-identity-regexp", `^https://github\.com/rajasatyajit/halotrcer/\.github/workflows/release\.yml@refs/tags/v`,
			"--certificate-oidc-issuer", "https://token.actions.githubusercontent.com",
			"<tmp>/checksums.txt",
		}
		if len(args) != len(want) {
			t.Fatalf("cosign args %q", args)
		}
		for i := range want {
			if rest, ok := strings.CutPrefix(want[i], "<tmp>"); ok {
				if !strings.HasSuffix(args[i], rest) {
					t.Errorf("cosign arg %d = %q, want …%s", i, args[i], rest)
				}
			} else if args[i] != want[i] {
				t.Errorf("cosign arg %d = %q, want %q", i, args[i], want[i])
			}
		}
		// --require-cosign reaches halotrcer, which verifies each tool
		// against that tool's own release identity.
		for _, c := range res.calls {
			if !strings.HasSuffix(c, " --require-cosign") {
				t.Errorf("halotrcer not told --require-cosign: %q", c)
			}
		}

		bad := instRun(t, shell, r, instEnv{cosign: "bad"}, "--version", "v1.2.3", "--dir", filepath.Join(t.TempDir(), "bin2"))
		if bad.code != 1 || !strings.Contains(bad.stderr, "could not verify checksums.txt") || len(bad.calls) != 0 {
			t.Fatalf("rejected signature: exit %d, calls %q:\n%s", bad.code, bad.calls, bad.stderr)
		}
	})
}

func TestInstallRequireCosignWithoutCosignDownloadsNothing(t *testing.T) {
	eachInstShell(t, func(t *testing.T, shell string) {
		r := newInstRelease(t)
		res := instRun(t, shell, r, instEnv{}, "--version", "v1.2.3", "--require-cosign")
		if res.code != 1 || !strings.Contains(res.stderr, "cosign is not installed") {
			t.Fatalf("exit %d:\n%s", res.code, res.stderr)
		}
		if got := r.seen(); len(got) != 0 {
			t.Errorf("requests before refusing: %v", got)
		}
	})
}

func TestInstallToolFailureFails(t *testing.T) {
	eachInstShell(t, func(t *testing.T, shell string) {
		r := newInstRelease(t)
		res := instRun(t, shell, r, instEnv{failInstall: true})
		if res.code != 1 || !strings.Contains(res.stderr, "halotrcer install tokvera: boom") || !strings.Contains(res.stderr, "not everything was installed") {
			t.Fatalf("exit %d:\n%s", res.code, res.stderr)
		}
		// Both tools are tried; the failure is reported at the end.
		if len(res.calls) != 2 {
			t.Errorf("calls %q", res.calls)
		}
	})
}

func TestInstallGlobalNeverRunsSudo(t *testing.T) {
	if fi, err := os.Stat("/usr/local/bin"); err == nil && fi.IsDir() {
		if f, err := os.CreateTemp("/usr/local/bin", ".halotrcer-probe-*"); err == nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
			t.Skip("/usr/local/bin is writable here; this test needs it not to be")
		}
	}
	eachInstShell(t, func(t *testing.T, shell string) {
		r := newInstRelease(t)
		res := instRun(t, shell, r, instEnv{}, "--global")
		if res.code != 1 || !strings.Contains(res.stderr, "this script never runs sudo") || !strings.Contains(res.stderr, "| sudo sh -s -- --global") {
			t.Fatalf("exit %d:\n%s", res.code, res.stderr)
		}
		if got := r.seen(); len(got) != 0 {
			t.Errorf("requests before refusing: %v", got)
		}
	})
}

func TestInstallDryRunContactsNothing(t *testing.T) {
	eachInstShell(t, func(t *testing.T, shell string) {
		r := newInstRelease(t)
		res := instRun(t, shell, r, instEnv{}, "--dry-run", "--require-cosign")
		if res.code != 0 {
			t.Fatalf("exit %d:\n%s", res.code, res.stderr)
		}
		if got := r.seen(); len(got) != 0 {
			t.Errorf("dry run made requests: %v", got)
		}
		if _, err := os.Stat(res.dir); !os.IsNotExist(err) {
			t.Errorf("dry run created %s", res.dir)
		}
		for _, s := range []string{"dry run", "linux/amd64", "latest (resolved", "install to:   " + res.dir + "/halotrcer",
			"halotrcer install --via direct --dir " + res.dir + " --require-cosign <tool>, for each of: tokvera sievant"} {
			if !strings.Contains(res.stderr, s) {
				t.Errorf("plan lacks %q:\n%s", s, res.stderr)
			}
		}
	})
}

// fakeUnameWorks reports whether shell runs a `uname` found on PATH (a
// busybox built with FEATURE_SH_STANDALONE runs its own applet first).
func fakeUnameWorks(t *testing.T, shell string) bool {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "uname"), []byte("#!/bin/sh\necho FAKE\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	p, err := exec.LookPath(shell)
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-c", "uname -s"}
	if shell == "busybox" {
		args = append([]string{"sh"}, args...)
	}
	cmd := exec.CommandContext(t.Context(), p, args...)
	cmd.Env = []string{"PATH=" + dir}
	out, _ := cmd.Output()
	return strings.TrimSpace(string(out)) == "FAKE"
}

func TestInstallDetectsThePlatform(t *testing.T) {
	cases := []struct {
		name string
		env  instEnv
		want string
		code int
		note string
	}{
		{name: "linux amd64", env: instEnv{unameS: "Linux", unameM: "x86_64"}, want: "halotrcer_1.2.3_linux_amd64.tar.gz"},
		{name: "linux arm64", env: instEnv{unameS: "Linux", unameM: "aarch64"}, want: "halotrcer_1.2.3_linux_arm64.tar.gz"},
		{name: "linux musl", env: instEnv{unameS: "Linux", unameM: "x86_64", musl: true}, want: "halotrcer_1.2.3_linux_amd64.tar.gz", note: "musl libc detected"},
		{name: "macOS arm64", env: instEnv{unameS: "Darwin", unameM: "arm64"}, want: "halotrcer_1.2.3_darwin_arm64.tar.gz"},
		{name: "macOS intel", env: instEnv{unameS: "Darwin", unameM: "x86_64"}, want: "halotrcer_1.2.3_darwin_amd64.tar.gz"},
		{name: "macOS rosetta", env: instEnv{unameS: "Darwin", unameM: "x86_64", rosetta: true}, want: "halotrcer_1.2.3_darwin_arm64.tar.gz", note: "Rosetta 2 detected"},
		{name: "git bash", env: instEnv{unameS: "MINGW64_NT-10.0-26100", unameM: "x86_64"}, want: "use install.ps1", code: 1},
		{name: "freebsd", env: instEnv{unameS: "FreeBSD", unameM: "amd64"}, want: "unsupported OS: FreeBSD", code: 1},
		{name: "32-bit arm", env: instEnv{unameS: "Linux", unameM: "armv7l"}, want: "unsupported CPU: armv7l", code: 1},
	}
	ran := 0
	t.Cleanup(func() {
		if ran == 0 && runtime.GOOS != "windows" {
			t.Error("no shell ran the platform cases: the probe would hide every regression")
		}
	})
	eachInstShell(t, func(t *testing.T, shell string) {
		if !fakeUnameWorks(t, shell) {
			t.Skipf("%s runs its own uname applet before PATH (sh and dash cover these cases)", shell)
		}
		ran++
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				res := instRun(t, shell, nil, c.env, "--version", "v1.2.3", "--dry-run")
				if res.code != c.code || !strings.Contains(res.stderr, c.want) {
					t.Fatalf("exit %d (want %d), output lacks %q:\n%s", res.code, c.code, c.want, res.stderr)
				}
				if c.note != "" && !strings.Contains(res.stderr, c.note) {
					t.Errorf("output lacks %q:\n%s", c.note, res.stderr)
				}
			})
		}
	})
}

func TestInstallUsageErrors(t *testing.T) {
	cases := map[string]struct {
		args []string
		env  instEnv
		msg  string
	}{
		"unknown option":            {args: []string{"--frobnicate"}, msg: "unknown option: --frobnicate"},
		"unknown tool":              {args: []string{"--only", "halotrcer"}, msg: "--only must be tokvera or sievant"},
		"dir and global":            {args: []string{"--dir", "/tmp/x", "--global"}, msg: "mutually exclusive"},
		"no-verify and require":     {args: []string{"--no-verify", "--require-cosign"}, msg: "mutually exclusive"},
		"bad version":               {args: []string{"--version", "latest"}, msg: "--version must look like"},
		"version with a path":       {args: []string{"--version", "v1.2.3/../../x"}, msg: "--version must look like"},
		"missing value":             {args: []string{"--dir"}, msg: "--dir needs a value"},
		"remote http download base": {args: []string{"--dry-run"}, env: instEnv{downloadBase: "http://example.com/dl"}, msg: "HALOTRCER_INSTALL_DOWNLOAD_BASE must be an https URL"},
		"remote http api base":      {args: []string{"--dry-run"}, env: instEnv{apiBase: "http://example.com/api"}, msg: "HALOTRCER_INSTALL_API_BASE must be an https URL"},
		"lookalike loopback host":   {args: []string{"--dry-run"}, env: instEnv{downloadBase: "http://127.0.0.1.example.com/dl"}, msg: "HALOTRCER_INSTALL_DOWNLOAD_BASE must be an https URL"},
	}
	eachInstShell(t, func(t *testing.T, shell string) {
		for name, c := range cases {
			t.Run(name, func(t *testing.T) {
				r := newInstRelease(t)
				res := instRun(t, shell, r, c.env, c.args...)
				if !strings.Contains(res.stderr, c.msg) {
					t.Errorf("output lacks %q:\n%s", c.msg, res.stderr)
				}
				if res.code != 2 {
					t.Fatalf("exit %d, want 2:\n%s", res.code, res.stderr)
				}
				if got := r.seen(); len(got) != 0 {
					t.Errorf("requests on a usage error: %v", got)
				}
			})
		}
	})
}

func TestInstallLatestNotPublished(t *testing.T) {
	eachInstShell(t, func(t *testing.T, shell string) {
		r := newInstRelease(t)
		r.mu.Lock()
		delete(r.latest, "halotrcer")
		r.mu.Unlock()
		res := instRun(t, shell, r, instEnv{})
		if res.code != 1 || !strings.Contains(res.stderr, "pass --version") {
			t.Fatalf("exit %d:\n%s", res.code, res.stderr)
		}
	})
}

func TestInstallWithWget(t *testing.T) {
	if _, err := exec.LookPath("wget"); err != nil {
		t.Skip("wget not on PATH")
	}
	eachInstShell(t, func(t *testing.T, shell string) {
		r := newInstRelease(t)
		res := instRun(t, shell, r, instEnv{noCurl: true})
		if res.code != 0 {
			t.Fatalf("exit %d:\n%s", res.code, res.stderr)
		}
		if got := firstLine(t, res.dir, "halotrcer"); !strings.HasPrefix(got, "halotrcer v1.2.3 ") {
			t.Errorf("installed %q", got)
		}
	})
}

func TestInstallHelp(t *testing.T) {
	eachInstShell(t, func(t *testing.T, shell string) {
		res := instRun(t, shell, nil, instEnv{}, "--help")
		if res.code != 0 {
			t.Fatalf("exit %d", res.code)
		}
		for _, f := range []string{"--only", "--version", "--dir", "--global", "--dry-run", "--no-verify", "--require-cosign"} {
			if !strings.Contains(res.stderr, f) {
				t.Errorf("help lacks %s", f)
			}
		}
	})
}

// TestInstallEndToEnd runs the script with the real halotrcer, built from
// this tree and published on the fake server, which then installs Tokvera
// and Sievant from their own fake releases: GitHub's latest Tokvera is
// outside the compatibility matrix, so the newest in-range one is taken;
// both are recorded in halotrcer's state file.
func TestInstallEndToEnd(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("the fake release is linux/amd64")
	}
	bin := filepath.Join(t.TempDir(), "halotrcer")
	build := exec.CommandContext(t.Context(), "go", "build", "-trimpath", "-ldflags", "-X main.version="+instVersion+" -X main.commit=abc1234", "-o", bin, "../cmd/halotrcer")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	real, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	r := newInstRelease(t)
	r.add(t, "halotrcer", instVersion, "linux", "amd64", real)
	tok := func(v string) []byte { return []byte("#!/bin/sh\necho 'tokvera " + v + " (commit fake)'\n") }
	r.add(t, "tokvera", "v0.2.4", "linux", "amd64", tok("v0.2.4"))
	r.add(t, "tokvera", "v0.3.0", "linux", "amd64", tok("v0.3.0"))
	r.list["tokvera"] = []string{"v0.3.0", "v0.2.5-rc.1", "v0.2.4", "v0.2.1"}
	// sievant prints its version on stderr (sievant cmd/sievant/configcmd.go).
	r.add(t, "sievant", "v0.2.0", "linux", "amd64", []byte("#!/bin/sh\necho 'sievant v0.2.0' >&2\n"))

	shell := instShells()[0]
	res := instRun(t, shell, r, instEnv{})
	if res.code != 0 {
		t.Fatalf("exit %d:\n%s", res.code, res.stderr)
	}
	if got := firstLine(t, res.dir, "halotrcer"); !strings.HasPrefix(got, "halotrcer "+instVersion) {
		t.Errorf("halotrcer %q", got)
	}
	if got := firstLine(t, res.dir, "tokvera"); got != "tokvera v0.2.4 (commit fake)" {
		t.Errorf("tokvera %q, want the newest in the matrix (v0.2.4)", got)
	}
	if got := firstLine(t, res.dir, "sievant"); got != "sievant v0.2.0" {
		t.Errorf("sievant %q", got)
	}
	for _, p := range []string{"/dl/tokvera/releases/download/v0.2.4/checksums.txt", "/dl/sievant/releases/download/v0.2.0/checksums.txt", "/api/tokvera/releases"} {
		if !slices.Contains(r.seen(), p) {
			t.Errorf("no request for %s", p)
		}
	}
	if slices.Contains(r.seen(), "/dl/tokvera/releases/download/v0.3.0/tokvera_0.3.0_linux_amd64.tar.gz") {
		t.Error("downloaded the out-of-matrix tokvera v0.3.0")
	}
	state, err := os.ReadFile(filepath.Join(res.home, ".local", "state", "halotrcer", "installed.json"))
	if err != nil {
		t.Fatalf("no state file: %v\n%s", err, res.stderr)
	}
	for _, s := range []string{`"tokvera"`, `"sievant"`, `"channel": "direct"`} {
		if !bytes.Contains(state, []byte(s)) {
			t.Errorf("state lacks %s:\n%s", s, state)
		}
	}
}
