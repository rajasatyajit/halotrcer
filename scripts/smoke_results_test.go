package scripts

import (
	"os"
	"path/filepath"
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
