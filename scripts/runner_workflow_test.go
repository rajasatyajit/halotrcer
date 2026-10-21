package scripts

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// linuxRunner is the only runs-on that may name LINUX_RUNNER: the variable
// counts only while the repository is private (docs/development.md).
const linuxRunner = "${{ github.event.repository.private && vars.LINUX_RUNNER || 'ubuntu-24.04' }}"

// TestSelfHostedRunnerRules: a job that can run on the owner's self-hosted
// runner uses exactly the private-only expression and first checks the host
// tools docs/development.md lists for it; release.yml can never run there
// (its provenance is verified with --deny-self-hosted-runners).
func TestSelfHostedRunnerRules(t *testing.T) {
	doc := string(root(t, "docs/development.md"))
	files, err := filepath.Glob("../.github/workflows/*.yml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no workflows: %v", err)
	}
	tools := regexp.MustCompile(`^sh scripts/ci/runner-tools\.sh (.+)$`)
	moved := 0
	for _, f := range files {
		name := filepath.Base(f)
		w, raw := readWorkflow(t, name)
		release := name == "release.yml"
		if release && strings.Contains(raw, "LINUX_RUNNER") {
			t.Error("release.yml reads LINUX_RUNNER: a release never runs on a self-hosted runner")
		}
		for jn, j := range w.Jobs {
			on, _ := j.RunsOn.(string)
			if release {
				if on != "ubuntu-24.04" {
					t.Errorf("release.yml %s: runs-on %v, want GitHub-hosted ubuntu-24.04", jn, j.RunsOn)
				}
				continue
			}
			if !strings.Contains(on, "LINUX_RUNNER") {
				continue
			}
			moved++
			if on != linuxRunner {
				t.Errorf("%s %s: runs-on %q, want %q", name, jn, on, linuxRunner)
			}
			found := -1
			for i, s := range j.Steps {
				m := tools.FindStringSubmatch(strings.TrimSpace(s.Run))
				if m == nil {
					continue
				}
				found = i
				if s.If != "runner.environment == 'self-hosted'" {
					t.Errorf("%s %s: the runner-tools step runs if %q, want only on a self-hosted runner", name, jn, s.If)
				}
				if !strings.Contains(doc, "```\n"+m[1]+"\n```") {
					t.Errorf("%s %s checks %q, which docs/development.md does not list", name, jn, m[1])
				}
			}
			// First after the checkout that brings the script.
			if found != 1 {
				t.Errorf("%s %s: runner-tools.sh is step %d, want the first after checkout", name, jn, found)
			}
		}
	}
	if moved != 4 {
		t.Errorf("%d jobs read LINUX_RUNNER, want 4 (ci; smoke's snapshot and not-requested; integration; docs/development.md)", moved)
	}
}

// TestRunnerTools: the check passes when every tool is there and fails
// naming each missing one, including a docker that cannot reach a daemon.
func TestRunnerTools(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.Symlink(sh, filepath.Join(bin, "sh")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(bin, "present"), "#!/bin/sh\n", 0o755)
	writeFile(t, filepath.Join(bin, "docker"), "#!/bin/sh\n[ \"$1\" = info ] && exit 1\n", 0o755)
	run := func(args ...string) (string, int) {
		cmd := exec.CommandContext(t.Context(), sh, append([]string{"ci/runner-tools.sh"}, args...)...)
		cmd.Env = []string{"PATH=" + bin}
		out, err := cmd.CombinedOutput()
		code := 0
		if err != nil {
			ee := (*exec.ExitError)(nil)
			if !errors.As(err, &ee) {
				t.Fatal(err)
			}
			code = ee.ExitCode()
		}
		return string(out), code
	}
	if out, code := run("present", "docker"); code != 0 || !strings.Contains(out, "all 2 present") {
		t.Errorf("all present: %d %s", code, out)
	}
	out, code := run("present", "absent", "docker-daemon", "gone")
	if code != 1 || !strings.Contains(out, "this runner lacks: absent docker-daemon gone") {
		t.Errorf("missing: %d %s", code, out)
	}
	if _, code := run(); code != 2 {
		t.Errorf("no arguments: exit %d, want 2", code)
	}
}
