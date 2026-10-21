package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestCheckResults: one valid RESULT line per channel passes (PASS, or SKIP
// with a reason); a FAIL anywhere, a missing, duplicated or reasonless line
// fails the job.
func TestCheckResults(t *testing.T) {
	for name, c := range map[string]struct {
		results string
		code    int
		want    string
	}{
		"pass and skip":   {"RESULT a PASS (3 checks)\nRESULT b SKIP not requested\n", 0, "a                PASS (3 checks)"},
		"fail":            {"RESULT a PASS (3 checks)\nRESULT b FAIL boom\n", 1, "b                FAIL boom"},
		"fail after pass": {"RESULT a PASS (1 checks)\nRESULT a FAIL exited 1 after reporting a result\nRESULT b SKIP x\n", 1, "a                FAIL exited 1"},
		"missing":         {"RESULT a PASS (1 checks)\n", 1, "MISSING"},
		"skip no reason":  {"RESULT a PASS (1 checks)\nRESULT b SKIP \n", 1, "INVALID"},
		"duplicate":       {"RESULT a PASS (1 checks)\nRESULT a PASS (1 checks)\nRESULT b SKIP x\n", 1, "INVALID: 2 RESULT lines"},
		"other word":      {"RESULT a OK\nRESULT b SKIP x\n", 1, "INVALID"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			f := filepath.Join(dir, "results")
			writeFile(t, f, c.results, 0o644)
			summary := filepath.Join(dir, "summary")
			out, code := script(t, dir, []string{"GITHUB_STEP_SUMMARY=" + summary}, "../smoke/check-results.sh", f, "a", "b")
			if code != c.code || !strings.Contains(out, c.want) {
				t.Errorf("exit %d (want %d), output lacks %q:\n%s", code, c.code, c.want, out)
			}
			if b, _ := os.ReadFile(summary); !strings.Contains(string(b), "| `a` |") {
				t.Errorf("summary:\n%s", b)
			}
		})
	}
	if _, code := script(t, t.TempDir(), nil, "../smoke/check-results.sh", "/nonexistent", "a"); code != 1 {
		t.Errorf("no results file: exit %d", code)
	}
}

// TestRunRecordsAFailure: run.sh always exits 0; a channel that reported
// nothing, or exited non-zero after a PASS, gets a FAIL line; a clean PASS
// stays the only line.
func TestRunRecordsAFailure(t *testing.T) {
	dir := t.TempDir()
	results := filepath.Join(dir, "results")
	env := []string{"SMOKE_RESULTS=" + results}
	for _, c := range []struct{ ch, body string }{
		{"silent", "exit 0"},
		{"died", "exit 3"},
		{"late", `echo "RESULT late PASS (1 checks)" >> "$SMOKE_RESULTS"; exit 1`},
		{"clean", `echo "RESULT clean PASS (2 checks)" >> "$SMOKE_RESULTS"`},
		{"own", `echo "RESULT own FAIL its reason" >> "$SMOKE_RESULTS"; exit 1`},
	} {
		if _, code := script(t, dir, env, "../smoke/run.sh", c.ch, "sh", "-c", c.body); code != 0 {
			t.Errorf("%s: run.sh exited %d", c.ch, code)
		}
	}
	b, _ := os.ReadFile(results)
	got := string(b)
	for _, want := range []string{
		"RESULT silent FAIL exited 0 without reporting a result",
		"RESULT died FAIL exited 3 without reporting a result",
		"RESULT late FAIL exited 1 after reporting a result",
		"RESULT clean PASS (2 checks)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("results lack %q:\n%s", want, got)
		}
	}
	if strings.Count(got, "RESULT own ") != 1 || strings.Count(got, "RESULT clean ") != 1 {
		t.Errorf("a channel's own line was doubled:\n%s", got)
	}
}

// TestSmokeHidesCosign: the snapshot carries no signatures, so smoke.sh runs
// every channel with cosign hidden; everything else in cosign's directory
// stays on PATH. The fake mirror (the first program smoke.sh starts) records
// what it can see.
func TestSmokeHidesCosign(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("smoke.sh is the Linux and macOS smoke")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	dist := filepath.Join(dir, "dist")
	seen := filepath.Join(dir, "seen")
	writeFile(t, filepath.Join(bin, "cosign"), "#!/bin/sh\nexit 0\n", 0o755)
	writeFile(t, filepath.Join(bin, "beside-cosign"), "#!/bin/sh\nexit 0\n", 0o755)
	writeFile(t, filepath.Join(dist, "TAG"), "v0.0.1-snapshot-test\n", 0o644)
	writeFile(t, filepath.Join(dist, "TOOLS"), "stand-in\n", 0o644)
	arch := map[string]string{"amd64": "amd64", "arm64": "arm64"}[runtime.GOARCH]
	if arch == "" {
		t.Skip("unsupported test CPU")
	}
	writeFile(t, filepath.Join(dist, "mirror-"+runtime.GOOS+"-"+arch), `#!/bin/sh
{ command -v cosign || echo "no cosign"; command -v beside-cosign || echo "no beside-cosign"; } >`+seen+`
while [ "$1" != --url-file ]; do shift; done
echo http://127.0.0.1:1 >"$2"
exec sleep 30
`, 0o755)
	path, err := filepath.Abs("smoke/smoke.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "sh", path, "no-such-channel")
	cmd.Env = []string{"PATH=" + bin + ":" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "LC_ALL=C", "SMOKE_DIST=" + dist}
	out, _ := cmd.CombinedOutput()
	if !strings.Contains(string(out), "cosign hidden from PATH") || !strings.Contains(string(out), "unknown channel no-such-channel") {
		t.Fatalf("smoke.sh:\n%s", out)
	}
	b, err := os.ReadFile(seen)
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(strings.TrimSpace(string(b)), "\n"); len(lines) != 2 || lines[0] != "no cosign" || !strings.HasSuffix(lines[1], "/beside-cosign") {
		t.Errorf("the mirror saw:\n%s", b)
	}
}
