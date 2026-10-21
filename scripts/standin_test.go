package scripts

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The smoke matrix's stand-in tools (testdata/standin, PLAN.md M4 K1):
// they replay the committed real fixtures byte for byte, and the real
// halotrcer, built from this tree, runs `check` and `doctor` against them as
// it would against Tokvera and Sievant.

func buildGo(t *testing.T, out, pkg string, ldflags string) {
	t.Helper()
	args := []string{"build", "-trimpath", "-o", out}
	if ldflags != "" {
		args = append(args, "-ldflags", ldflags)
	}
	cmd := exec.CommandContext(t.Context(), "go", append(args, pkg)...)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build %s: %v\n%s", pkg, err, b)
	}
}

func TestStandinsReplayTheFixtures(t *testing.T) {
	for copyPath, fixture := range map[string]string{
		"tokvera/flaky.report.json": "tokvera-f2a7906/flaky.report.json",
		"tokvera/doctor.json":       "tokvera-f2a7906/doctor.json",
		"sievant/scan-smoke.json":   "sievant-d8a8bcf/scan-smoke.json",
		"sievant/doctor.json":       "sievant-d8a8bcf/doctor.json",
		"sievant/report.json":       "sievant-d8a8bcf/report.json",
	} {
		got := root(t, "testdata/standin/"+copyPath)
		want := root(t, "testdata/fixtures/"+fixture)
		if !bytes.Equal(got, want) {
			t.Errorf("testdata/standin/%s differs from testdata/fixtures/%s: copy it again", copyPath, fixture)
		}
	}
	// The sources name the fixture commits they replay.
	for tool, commit := range map[string]string{"tokvera": "f2a7906", "sievant": "d8a8bcf"} {
		if src := string(root(t, "testdata/standin/"+tool+"/main.go")); !strings.Contains(src, "STAND-IN") || !strings.Contains(src, commit) {
			t.Errorf("testdata/standin/%s/main.go does not say it is a stand-in replaying %s", tool, commit)
		}
	}
}

// TestHalotrcerRunsAgainstTheStandins: check writes the combined report with
// both sections from the stand-ins' outputs, and doctor finds both inside the
// compatibility matrix.
func TestHalotrcerRunsAgainstTheStandins(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix paths")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}
	bin := t.TempDir()
	hal := filepath.Join(bin, "halotrcer")
	buildGo(t, hal, "../cmd/halotrcer", "-X main.version=v1.2.3 -X main.commit=abc1234")
	buildGo(t, filepath.Join(bin, "tokvera"), "../testdata/standin/tokvera", "")
	buildGo(t, filepath.Join(bin, "sievant"), "../testdata/standin/sievant", "")

	home := t.TempDir()
	env := []string{"PATH=" + bin + ":/usr/bin:/bin", "HOME=" + home, "XDG_STATE_HOME=" + filepath.Join(home, "state")}
	out := filepath.Join(t.TempDir(), "report")
	cmd := exec.CommandContext(t.Context(), hal, "check", "--target", "http://127.0.0.1:9/v1", "--yes", "--no-project",
		"--mcp-config", filepath.Join(home, "none.json"), "--gateway-report", "--out", out)
	cmd.Env = env
	cmd.Dir = t.TempDir()
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("halotrcer check: %v\n%s", err, b)
	}
	// Each stand-in announces itself, and its banner never reads as a
	// version line ("<tool> vX.Y.Z", which Sievant prints on stderr).
	if strings.Contains(string(b), "unrecognized version") || strings.Contains(string(b), "outside the versions") {
		t.Errorf("a stand-in's version was misread:\n%s", b)
	}
	for _, tool := range []string{"tokvera", "sievant"} {
		vb, _ := exec.CommandContext(t.Context(), filepath.Join(bin, tool), "version").CombinedOutput()
		if !strings.Contains(string(vb), "STAND-IN for "+tool) || !strings.Contains(string(vb), tool+" v0.2.0") {
			t.Errorf("%s version:\n%s", tool, vb)
		}
	}
	html, err := os.ReadFile(filepath.Join(out, "report.html"))
	if err != nil {
		entries, _ := os.ReadDir(out)
		t.Fatalf("no HTML report (%v): %v\n%s", entries, err, b)
	}
	for _, s := range []string{"Cost &amp; performance", "Agent security", "30-day fix plan"} {
		if !strings.Contains(string(html), s) {
			t.Errorf("report lacks %q", s)
		}
	}
	for _, f := range []string{"report.pdf", "report.json"} {
		if fi, err := os.Stat(filepath.Join(out, f)); err != nil || fi.Size() == 0 {
			t.Errorf("%s: %v", f, err)
		}
	}

	doc := exec.CommandContext(t.Context(), hal, "doctor")
	doc.Env = env
	doc.Dir = t.TempDir()
	db, _ := doc.CombinedOutput()
	for _, want := range []string{"[ok] tokvera: v0.2.0", "[ok] sievant: v0.2.0"} {
		if !strings.Contains(string(db), want) {
			t.Errorf("doctor lacks %q:\n%s", want, db)
		}
	}
}
