package scripts

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
)

// These tests run scripts/install.ps1 under pwsh (skipped when pwsh is not on
// PATH; `make install-check` runs them in the pinned pwsh 7.1.5 and 7.4
// images), as Tokvera's and Sievant's installer tests do. Off Windows, a
// driver dot-sources the script, which defines its functions without
// running, replaces the seams that need Windows (the OS check, the CPU,
// elevation, running halotrcer.exe, and finding a tool on PATH), and calls
// the script's own entry function. Everything else - option checks, endpoint
// rules, the download, cosign, the SHA-256 check, the zip, the atomic install,
// the calls it makes to halotrcer - is the real code. Windows itself is
// covered by the smoke matrix (M4).

const halPS1Driver = `
param([string]$Script, [string]$OptionsJson)
. $Script
function Test-HalotrcerWindows { return $env:FAKE_WINDOWS -ne '0' }
if ($env:FAKE_ARCH) { function Get-HalotrcerArch { if ($env:FAKE_ARCH -eq 'bad') { throw "unsupported CPU: ARM (release builds exist for amd64 and arm64)" }; return $env:FAKE_ARCH } }
function Test-HalotrcerElevated { return $env:FAKE_ELEVATED -eq '1' }
# The fake halotrcer.exe is a text file holding the line 'halotrcer version' prints.
function Get-HalotrcerVersionLine([string]$Exe) { return [string](Get-Content -LiteralPath $Exe -TotalCount 1) }
# A tool "installed elsewhere": FAKE_OTHER names it.
function Find-HalotrcerTool([string]$Name) { if ($env:FAKE_OTHER -eq $Name) { return "C:\scoop\shims\$Name.exe" }; return $null }
# The fake halotrcer install: logs the call (and that the halotrcer it was
# given exists), drops a fake tool, prints halotrcer's "next:" line.
function Invoke-HalotrcerToolInstall([string]$Exe, [string[]]$Arguments, [string]$ToolDir) {
	Add-Content -LiteralPath $env:INST_LOG -Value ("exists=$(Test-Path -LiteralPath $Exe) " + ($Arguments -join ' '))
	if ($env:INST_FAIL) { return [pscustomobject]@{ ExitCode = 1; StdOut = ''; StdErr = "halotrcer install $($Arguments[1]): boom" } }
	Set-Content -LiteralPath (Join-Path $ToolDir "$($Arguments[1]).exe") -Value "$($Arguments[1]) v0.2.0 (commit fake)"
	return [pscustomobject]@{ ExitCode = 0; StdOut = "$($Arguments[1]): installed v0.2.0$([char]10)next: halotrcer check --target <inference-url>$([char]10)"; StdErr = '' }
}
$o = @{}
(ConvertFrom-Json $OptionsJson).PSObject.Properties | ForEach-Object { $o[$_.Name] = $_.Value }
exit (Invoke-HalotrcerInstallMain $o)
`

type halPSRelease struct {
	srv      *httptest.Server
	mu       sync.Mutex
	requests []string
	files    map[string][]byte // file name under /dl/halotrcer/releases/download/<tag>/
}

func halFakeZip(t *testing.T, version string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{
		"halotrcer.exe": fmt.Sprintf("halotrcer %s (commit abc1234)\n", version),
		"LICENSE":       "Apache\n",
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func newHalPSRelease(t *testing.T, arch string) *halPSRelease {
	t.Helper()
	name := fmt.Sprintf("halotrcer_1.2.3_windows_%s.zip", arch)
	z := halFakeZip(t, instVersion)
	r := &halPSRelease{files: map[string][]byte{
		name:                          z,
		"checksums.txt":               fmt.Appendf(nil, "%x  %s\n", sha256.Sum256(z), name),
		"checksums.txt.sigstore.json": []byte(`{}`),
	}}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		r.requests = append(r.requests, req.URL.Path)
		files := r.files
		r.mu.Unlock()
		if req.URL.Path == "/api/halotrcer/releases/latest" {
			_, _ = fmt.Fprintf(w, `{"tag_name":"%s"}`, instVersion)
			return
		}
		if b, ok := files[strings.TrimPrefix(req.URL.Path, "/dl/halotrcer/releases/download/"+instVersion+"/")]; ok {
			_, _ = w.Write(b)
			return
		}
		http.NotFound(w, req)
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *halPSRelease) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.requests)
}

type halPSEnv struct {
	arch     string // "" = the real CPU check
	windows  string // "0" = not Windows
	elevated bool
	cosign   string // "", "ok", "bad"
	dlBase   string
	other    string // a tool "installed elsewhere"
	fail     bool   // the fake halotrcer install fails
}

type halPSResult struct {
	code   int
	out    string
	cosign string
	calls  []string
	root   string
}

// needPwsh returns pwsh's path, skipping the test without it, except in
// CI, which must run every PowerShell test (scripts/ci/runner-tools.sh
// checks for pwsh too).
func needPwsh(t *testing.T) string {
	t.Helper()
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("pwsh is required in CI (the install.ps1 tests)")
		}
		t.Skip("pwsh not on PATH")
	}
	return pwsh
}

func runHalPS(t *testing.T, r *halPSRelease, e halPSEnv, opts string) halPSResult {
	t.Helper()
	pwsh := needPwsh(t)
	root := t.TempDir()
	driver := filepath.Join(root, "driver.ps1")
	if err := os.WriteFile(driver, []byte(halPS1Driver), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(root, "cosign.log")
	if e.cosign != "" {
		exitCode := 0
		if e.cosign == "bad" {
			exitCode = 1
		}
		// Like the real cosign, it reports on stderr.
		if err := os.WriteFile(filepath.Join(bin, "cosign"), fmt.Appendf(nil, "#!/bin/sh\nprintf '%%s\\n' \"$@\" > '%s'\necho 'Verified OK' >&2\nexit %d\n", log, exitCode), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	calls := filepath.Join(root, "calls.log")
	script, _ := filepath.Abs("install.ps1")
	cmd := exec.CommandContext(t.Context(), pwsh, "-NoProfile", "-NonInteractive", "-File", driver, "-Script", script, "-OptionsJson", opts)
	// PATH holds only the fake cosign (when present) and pwsh's own directory.
	cmd.Env = []string{
		"PATH=" + bin + string(os.PathListSeparator) + filepath.Dir(pwsh),
		"HOME=" + root, "LOCALAPPDATA=" + filepath.Join(root, "localappdata"), "ProgramFiles=" + filepath.Join(root, "programfiles"),
		"FAKE_ARCH=" + e.arch, "FAKE_WINDOWS=" + e.windows, "FAKE_OTHER=" + e.other, "INST_LOG=" + calls,
	}
	if e.elevated {
		cmd.Env = append(cmd.Env, "FAKE_ELEVATED=1")
	}
	if e.fail {
		cmd.Env = append(cmd.Env, "INST_FAIL=1")
	}
	if r != nil {
		dl := r.srv.URL + "/dl"
		if e.dlBase != "" {
			dl = e.dlBase
		}
		cmd.Env = append(cmd.Env, "HALOTRCER_INSTALL_DOWNLOAD_BASE="+dl, "HALOTRCER_INSTALL_API_BASE="+r.srv.URL+"/api")
	}
	out, err := cmd.CombinedOutput()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	cl, _ := os.ReadFile(log)
	cb, _ := os.ReadFile(calls)
	var callLines []string
	if s := strings.TrimSpace(string(cb)); s != "" {
		callLines = strings.Split(s, "\n")
	}
	return halPSResult{code: code, out: string(out), cosign: string(cl), calls: callLines, root: root}
}

func firstFileLine(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimRight(strings.SplitN(string(b), "\n", 2)[0], "\r")
}

func TestPS1InstallsAllAndIsIdempotent(t *testing.T) {
	r := newHalPSRelease(t, "arm64")
	dir := filepath.Join(t.TempDir(), "hal")
	res := runHalPS(t, r, halPSEnv{arch: "arm64"}, fmt.Sprintf(`{"Dir":%q}`, dir))
	if res.code != 0 {
		t.Fatalf("exit %d:\n%s", res.code, res.out)
	}
	if got := firstFileLine(filepath.Join(dir, "halotrcer.exe")); got != "halotrcer v1.2.3 (commit abc1234)" {
		t.Fatalf("installed %q:\n%s", got, res.out)
	}
	want := []string{"/api/halotrcer/releases/latest", "/dl/halotrcer/releases/download/v1.2.3/halotrcer_1.2.3_windows_arm64.zip", "/dl/halotrcer/releases/download/v1.2.3/checksums.txt"}
	if got := r.seen(); !slices.Equal(got, want) {
		t.Errorf("requests %v, want %v", got, want)
	}
	wantCalls := []string{
		"exists=True install tokvera --via direct --dir " + dir,
		"exists=True install sievant --via direct --dir " + dir,
	}
	if !slices.Equal(res.calls, wantCalls) {
		t.Errorf("calls %q, want %q", res.calls, wantCalls)
	}
	for _, s := range []string{"SHA-256 verified", "cosign not found", "is not on your PATH", "tokvera: installed v0.2.0", "next: halotrcer check --target <inference-url>"} {
		if !strings.Contains(res.out, s) {
			t.Errorf("output lacks %q:\n%s", s, res.out)
		}
	}
	if n := strings.Count(res.out, "next: "); n != 1 {
		t.Errorf("%d next: lines:\n%s", n, res.out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".halotrcer.new.exe")); !os.IsNotExist(err) {
		t.Error("the staged .halotrcer.new.exe was left behind")
	}

	before := len(r.seen())
	again := runHalPS(t, r, halPSEnv{arch: "arm64"}, fmt.Sprintf(`{"Dir":%q,"Version":"1.2.3"}`, dir))
	if again.code != 0 || !strings.Contains(again.out, "halotrcer v1.2.3 is already installed") {
		t.Fatalf("second run exit %d:\n%s", again.code, again.out)
	}
	if len(r.seen()) != before {
		t.Errorf("second run downloaded halotrcer again: %v", r.seen()[before:])
	}
	if len(again.calls) != 2 {
		t.Errorf("the tools were not handed to halotrcer again: %q", again.calls)
	}
}

func TestPS1DefaultDirsAndRealArch(t *testing.T) {
	// The real Get-HalotrcerArch on this machine's CPU; each program in its
	// own directory under LOCALAPPDATA\Programs, as each tool's installer does.
	arch := map[string]string{"amd64": "amd64", "arm64": "arm64"}[runtime.GOARCH]
	if arch == "" {
		t.Skip("unsupported test CPU")
	}
	r := newHalPSRelease(t, arch)
	res := runHalPS(t, r, halPSEnv{}, `{"Version":"v1.2.3"}`)
	if res.code != 0 {
		t.Fatalf("exit %d:\n%s", res.code, res.out)
	}
	programs := filepath.Join(res.root, "localappdata", "Programs")
	if got := firstFileLine(filepath.Join(programs, "halotrcer", "halotrcer.exe")); !strings.HasPrefix(got, "halotrcer v1.2.3") || !strings.Contains(res.out, "windows/"+arch) {
		t.Fatalf("halotrcer %q:\n%s", got, res.out)
	}
	for i, tool := range []string{"tokvera", "sievant"} {
		want := "install " + tool + " --via direct --dir " + filepath.Join(programs, tool)
		if i >= len(res.calls) || !strings.HasSuffix(res.calls[i], want) {
			t.Errorf("calls %q, want …%s", res.calls, want)
		}
	}
}

func TestPS1OnlyKeepsNoHalotrcer(t *testing.T) {
	r := newHalPSRelease(t, "amd64")
	res := runHalPS(t, r, halPSEnv{arch: "amd64"}, `{"Only":"tokvera","Version":"v1.2.3"}`)
	if res.code != 0 {
		t.Fatalf("exit %d:\n%s", res.code, res.out)
	}
	if _, err := os.Stat(filepath.Join(res.root, "localappdata", "Programs", "halotrcer")); !os.IsNotExist(err) {
		t.Error("-Only left a halotrcer directory behind")
	}
	if len(res.calls) != 1 || !strings.HasPrefix(res.calls[0], "exists=True install tokvera ") {
		t.Errorf("calls %q", res.calls)
	}
	if !strings.Contains(res.out, "next: tokvera scan --target <inference-url>") || strings.Contains(res.out, "halotrcer check") {
		t.Errorf("next step:\n%s", res.out)
	}
}

func TestPS1LeavesAToolInstalledElsewhere(t *testing.T) {
	r := newHalPSRelease(t, "amd64")
	res := runHalPS(t, r, halPSEnv{arch: "amd64", other: "sievant"}, `{"Version":"v1.2.3"}`)
	if res.code != 0 {
		t.Fatalf("exit %d:\n%s", res.code, res.out)
	}
	if len(res.calls) != 1 || !strings.Contains(res.calls[0], "install tokvera ") {
		t.Errorf("calls %q, want only tokvera", res.calls)
	}
	if !strings.Contains(res.out, `sievant is already installed at C:\scoop\shims\sievant.exe; leaving it`) {
		t.Errorf("output:\n%s", res.out)
	}
}

func TestPS1ToolFailureFails(t *testing.T) {
	r := newHalPSRelease(t, "amd64")
	res := runHalPS(t, r, halPSEnv{arch: "amd64", fail: true}, `{"Version":"v1.2.3"}`)
	if res.code != 1 || !strings.Contains(res.out, "halotrcer install tokvera: boom") || !strings.Contains(res.out, "not everything was installed (see above): tokvera, sievant") {
		t.Fatalf("exit %d:\n%s", res.code, res.out)
	}
}

func TestPS1RefusesABadChecksum(t *testing.T) {
	for name, c := range map[string]struct {
		file string
		body []byte
		msg  string
	}{
		"mismatch":   {"halotrcer_1.2.3_windows_amd64.zip", nil, "SHA-256 mismatch for halotrcer_1.2.3_windows_amd64.zip"},
		"not listed": {"checksums.txt", []byte("00  other.zip\n"), "halotrcer_1.2.3_windows_amd64.zip is not listed in checksums.txt"},
	} {
		t.Run(name, func(t *testing.T) {
			r := newHalPSRelease(t, "amd64")
			if c.body == nil {
				c.body = halFakeZip(t, "v6.6.6")
			}
			r.files[c.file] = c.body
			dir := filepath.Join(t.TempDir(), "hal")
			res := runHalPS(t, r, halPSEnv{arch: "amd64"}, fmt.Sprintf(`{"Dir":%q,"Version":"v1.2.3"}`, dir))
			if res.code != 1 || !strings.Contains(res.out, c.msg) {
				t.Fatalf("exit %d, output lacks %q:\n%s", res.code, c.msg, res.out)
			}
			if _, err := os.Stat(filepath.Join(dir, "halotrcer.exe")); !os.IsNotExist(err) {
				t.Error("installed after a failed check")
			}
			if len(res.calls) != 0 {
				t.Errorf("tools installed after a failed check: %q", res.calls)
			}
		})
	}
}

func TestPS1Cosign(t *testing.T) {
	r := newHalPSRelease(t, "amd64")
	res := runHalPS(t, r, halPSEnv{arch: "amd64", cosign: "ok"}, fmt.Sprintf(`{"Dir":%q,"Version":"v1.2.3","RequireCosign":true}`, filepath.Join(t.TempDir(), "a")))
	if res.code != 0 || !strings.Contains(res.out, "cosign: checksums.txt is signed") {
		t.Fatalf("exit %d:\n%s", res.code, res.out)
	}
	args := strings.Split(strings.TrimSpace(res.cosign), "\n")
	if len(args) != 8 || args[0] != "verify-blob" || args[1] != "--bundle" || !strings.HasSuffix(args[2], "checksums.txt.sigstore.json") ||
		args[4] != `^https://github\.com/rajasatyajit/halotrcer/\.github/workflows/release\.yml@refs/tags/v` ||
		args[6] != "https://token.actions.githubusercontent.com" || !strings.HasSuffix(args[7], "checksums.txt") {
		t.Errorf("cosign args %q", args)
	}
	for _, c := range res.calls {
		if !strings.HasSuffix(c, " --require-cosign") {
			t.Errorf("halotrcer not told --require-cosign: %q", c)
		}
	}

	bad := runHalPS(t, r, halPSEnv{arch: "amd64", cosign: "bad"}, fmt.Sprintf(`{"Dir":%q,"Version":"v1.2.3"}`, filepath.Join(t.TempDir(), "b")))
	if bad.code != 1 || !strings.Contains(bad.out, "could not verify checksums.txt") || len(bad.calls) != 0 {
		t.Fatalf("rejected signature: exit %d:\n%s", bad.code, bad.out)
	}

	before := len(r.seen())
	req := runHalPS(t, r, halPSEnv{arch: "amd64"}, `{"Version":"v1.2.3","RequireCosign":true}`)
	if req.code != 1 || !strings.Contains(req.out, "cosign is not installed") || len(r.seen()) != before {
		t.Fatalf("require without cosign: exit %d, requests %v:\n%s", req.code, r.seen()[before:], req.out)
	}
}

func TestPS1NoVerify(t *testing.T) {
	r := newHalPSRelease(t, "amd64")
	r.files["checksums.txt"] = []byte("00  halotrcer_1.2.3_windows_amd64.zip\n")
	dir := filepath.Join(t.TempDir(), "hal")
	res := runHalPS(t, r, halPSEnv{arch: "amd64", cosign: "bad"}, fmt.Sprintf(`{"Dir":%q,"Version":"v1.2.3","NoVerify":true}`, dir))
	if res.code != 0 || !strings.Contains(res.out, "NOT checked") || res.cosign != "" {
		t.Fatalf("exit %d, cosign log %q:\n%s", res.code, res.cosign, res.out)
	}
	if slices.Contains(r.seen(), "/dl/halotrcer/releases/download/v1.2.3/checksums.txt") {
		t.Error("checksums.txt downloaded under -NoVerify")
	}
	for _, c := range res.calls {
		if !strings.HasSuffix(c, " --no-verify") {
			t.Errorf("halotrcer not told --no-verify: %q", c)
		}
	}
}

func TestPS1DryRunAndUsage(t *testing.T) {
	r := newHalPSRelease(t, "amd64")
	res := runHalPS(t, r, halPSEnv{arch: "arm64"}, `{"DryRun":true,"Global":true}`)
	if res.code != 0 || !strings.Contains(res.out, "windows/arm64") || len(r.seen()) != 0 ||
		!strings.Contains(res.out, filepath.Join("programfiles", "halotrcer", "halotrcer.exe")) ||
		!strings.Contains(res.out, "halotrcer install sievant --via direct --dir "+filepath.Join(res.root, "programfiles", "sievant")) {
		t.Fatalf("dry run: exit %d, requests %v:\n%s", res.code, r.seen(), res.out)
	}

	for name, c := range map[string]struct {
		opts string
		env  halPSEnv
		code int
		msg  string
	}{
		"unknown tool":        {`{"Only":"halotrcer"}`, halPSEnv{}, 2, "-Only must be tokvera or sievant"},
		"dir and global":      {`{"Dir":"x","Global":true}`, halPSEnv{}, 2, "mutually exclusive"},
		"noverify and req":    {`{"NoVerify":true,"RequireCosign":true}`, halPSEnv{}, 2, "mutually exclusive"},
		"bad version":         {`{"Version":"latest"}`, halPSEnv{}, 2, "-Version must look like"},
		"remote http":         {`{"DryRun":true}`, halPSEnv{dlBase: "http://example.com/dl"}, 2, "HALOTRCER_INSTALL_DOWNLOAD_BASE must be an https URL"},
		"lookalike loopback":  {`{"DryRun":true}`, halPSEnv{dlBase: "http://127.0.0.1.example.com/dl"}, 2, "HALOTRCER_INSTALL_DOWNLOAD_BASE must be an https URL"},
		"not windows":         {`{"DryRun":true}`, halPSEnv{windows: "0"}, 1, "use install.sh"},
		"unsupported cpu":     {`{"DryRun":true}`, halPSEnv{arch: "bad"}, 1, "unsupported CPU"},
		"global not elevated": {`{"Global":true,"Version":"v1.2.3"}`, halPSEnv{arch: "amd64"}, 1, "needs an elevated PowerShell"},
	} {
		t.Run(name, func(t *testing.T) {
			before := len(r.seen())
			res := runHalPS(t, r, c.env, c.opts)
			if res.code != c.code || !strings.Contains(res.out, c.msg) {
				t.Fatalf("exit %d (want %d), output lacks %q:\n%s", res.code, c.code, c.msg, res.out)
			}
			if len(r.seen()) != before {
				t.Errorf("requests: %v", r.seen()[before:])
			}
		})
	}
}

// TestPS1ParsesAndRunsAsAFile: the script parses, and run as a file (no
// dot-sourcing, no seams) it exits with the usage code.
func TestPS1ParsesAndRunsAsAFile(t *testing.T) {
	pwsh := needPwsh(t)
	script, _ := filepath.Abs("install.ps1")
	parse := `$t=$null;$e=$null;[System.Management.Automation.Language.Parser]::ParseFile('` + strings.ReplaceAll(script, "'", "''") +
		`',[ref]$t,[ref]$e)|Out-Null;if($e){$e|ForEach-Object{$_.ToString()};exit 1}`
	if out, err := exec.CommandContext(t.Context(), pwsh, "-NoProfile", "-Command", parse).CombinedOutput(); err != nil {
		t.Fatalf("parse errors:\n%s", out)
	}
	out, err := exec.CommandContext(t.Context(), pwsh, "-NoProfile", "-NonInteractive", "-File", script, "-Dir", "x", "-Global").CombinedOutput()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 2 || !strings.Contains(string(out), "mutually exclusive") {
		t.Fatalf("run as a file: %v:\n%s", err, out)
	}
}

// TestPS1ThroughIexKeepsTheShell: `irm … | iex` runs the script in the
// caller's scope. It must not call exit (that would close the user's shell),
// must report through $LASTEXITCODE, and must leave the session's
// preferences alone. A remote-http override makes it stop with a usage error
// before any network call.
func TestPS1ThroughIexKeepsTheShell(t *testing.T) {
	pwsh := needPwsh(t)
	script, _ := filepath.Abs("install.ps1")
	cmd := exec.CommandContext(t.Context(), pwsh, "-NoProfile", "-NonInteractive", "-Command",
		`$LASTEXITCODE = 7; Get-Content -Raw -LiteralPath '`+strings.ReplaceAll(script, "'", "''")+`' | Invoke-Expression; `+
			`"after: code=$LASTEXITCODE eap=$ErrorActionPreference progress=$ProgressPreference"`)
	cmd.Env = append(os.Environ(), "HALOTRCER_INSTALL_DOWNLOAD_BASE=http://example.com/dl")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("pwsh: %v:\n%s", err, out)
	}
	// The stale 7 is replaced: the exit code is always this run's.
	if !strings.Contains(string(out), "after: code=2 eap=Continue progress=Continue") {
		t.Fatalf("the shell did not survive, or its preferences changed:\n%s", out)
	}
}

// TestPS1NativeCallsSurviveStderrUnderStop: Invoke-HalotrcerNative returns
// the exit code with stdout and stderr captured apart, never throws because a
// program wrote to stderr (Windows PowerShell 5.1 aborts on that under
// $ErrorActionPreference = 'Stop'), passes every argument through intact
// (spaces, quotes and trailing backslashes included), and sets an
// environment variable for the child without touching the caller's.
func TestPS1NativeCallsSurviveStderrUnderStop(t *testing.T) {
	pwsh := needPwsh(t)
	dir := t.TempDir()
	prog := filepath.Join(dir, "native")
	if err := os.WriteFile(prog, []byte("#!/bin/sh\nfor a in \"$@\"; do printf '[%s]\\n' \"$a\"; done\necho \"child=$HALOTRCER_T\"\necho 'progress on stderr' >&2\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	script, _ := filepath.Abs("install.ps1")
	driver := `
. '` + strings.ReplaceAll(script, "'", "''") + `'
$ErrorActionPreference = 'Stop'
$r = Invoke-HalotrcerNative '` + prog + `' @('plain', 'a b', 'q"uote', 'trail\', 'back\\"slash', '', 'tab	x') @{ HALOTRCER_T = 'set' }
"exit=$($r.ExitCode)"
"stderr=$($r.StdErr.Trim())"
"caller=[$env:HALOTRCER_T]"
$r.StdOut
`
	out, err := exec.CommandContext(t.Context(), pwsh, "-NoProfile", "-NonInteractive", "-Command", driver).CombinedOutput()
	if err != nil {
		t.Fatalf("pwsh: %v\n%s", err, out)
	}
	want := "exit=3\nstderr=progress on stderr\ncaller=[]\n[plain]\n[a b]\n[q\"uote]\n[trail\\]\n[back\\\\\"slash]\n[]\n[tab\tx]\nchild=set\n"
	if strings.TrimRight(string(out), "\n") != strings.TrimRight(want, "\n") {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

// TestPS1EveryNativeCallUsesTheHelper: install.ps1 invokes no native program
// with the call operator, which Windows PowerShell 5.1 would abort on at the
// program's first stderr line; Invoke-HalotrcerNative is the only way out.
func TestPS1EveryNativeCallUsesTheHelper(t *testing.T) {
	raw, err := os.ReadFile("install.ps1")
	if err != nil {
		t.Fatal(err)
	}
	call := regexp.MustCompile(`(^|[=(|;]\s*)&\s*[$"'\w]`)
	for i, line := range strings.Split(string(raw), "\n") {
		code := strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if call.MatchString(code) {
			t.Errorf("install.ps1:%d calls a program with & : %s", i+1, strings.TrimSpace(line))
		}
	}
	// And no Start-Process, Invoke-Expression or backtick-free shortcut
	// around the helper.
	for _, bad := range []string{"Start-Process", "Invoke-Expression", "cmd /c", "cmd.exe"} {
		if strings.Contains(string(raw), bad) {
			t.Errorf("install.ps1 uses %s", bad)
		}
	}
}

// TestPS1DotSourcingKeepsTheCallersVariables: dot-sourcing install.ps1 (as a
// smoke script does, for Invoke-HalotrcerNative) may add or change only
// Halotrcer-prefixed variables and its own parameters. PowerShell variable
// names are case-insensitive, so Tokvera's installer `$script:Repo` once
// overwrote its smoke script's `$repo` (Tokvera smoke run 39022846757).
func TestPS1DotSourcingKeepsTheCallersVariables(t *testing.T) {
	pwsh := needPwsh(t)
	script, _ := filepath.Abs("install.ps1")
	driver := `
$repo = 'caller'; $tools = 'caller'; $dir = 'caller'; $code = 'caller'
$before = @{}; Get-Variable | ForEach-Object { $before[$_.Name] = "$($_.Value)" }
. '` + strings.ReplaceAll(script, "'", "''") + `'
Get-Variable | Where-Object { $_.Name -notin @('before', '_', 'PSItem', '?', '^', '$', 'args', 'input', 'MyInvocation', 'PSBoundParameters', 'PSCmdlet', 'StackTrace', 'Error', 'LASTEXITCODE') } |
	Where-Object { -not $before.ContainsKey($_.Name) -or $before[$_.Name] -ne "$($_.Value)" } |
	ForEach-Object { $_.Name }
"repo=$repo tools=$tools code=$code"
`
	out, err := exec.CommandContext(t.Context(), pwsh, "-NoProfile", "-NonInteractive", "-Command", driver).CombinedOutput()
	if err != nil {
		t.Fatalf("pwsh: %v\n%s", err, out)
	}
	params := map[string]bool{"Only": true, "Version": true, "Dir": true, "Global": true, "DryRun": true, "NoVerify": true, "RequireCosign": true}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if last := strings.TrimSpace(lines[len(lines)-1]); last != "repo=caller tools=caller code=caller" {
		t.Errorf("the caller's variables changed: %s", last)
	}
	for _, name := range lines[:len(lines)-1] {
		name = strings.TrimSpace(name)
		// $Dir is a parameter: dot-sourcing binds it, as Tokvera's does; the
		// caller's own $dir is the case the parameter names must avoid in
		// smoke scripts (none exists yet; M4 adds them under this rule).
		if name != "" && !params[name] && !strings.HasPrefix(name, "Halotrcer") {
			t.Errorf("dot-sourcing install.ps1 sets $%s: name it $script:Halotrcer..., so it cannot overwrite a caller's variable", name)
		}
	}
}
