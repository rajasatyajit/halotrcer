package scripts

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestSmokeWorkflow pins smoke.yml's structure (PLAN.md M4): dispatch only,
// with include_macos and include_windows defaulting to false; the macOS and
// Windows jobs gated on them and recorded as SKIP with the owner's reason
// otherwise; every channel step running whatever happened before it; and
// check-results.sh as each test job's last step.
func TestSmokeWorkflow(t *testing.T) {
	w, raw := readWorkflow(t, "smoke.yml")
	var on struct {
		On struct {
			Dispatch struct {
				Inputs map[string]struct {
					Type    string `yaml:"type"`
					Default any    `yaml:"default"`
				} `yaml:"inputs"`
			} `yaml:"workflow_dispatch"`
			Push, PullRequest, Schedule any
		} `yaml:"on"`
	}
	if err := yaml.Unmarshal([]byte(raw), &on); err != nil {
		t.Fatal(err)
	}
	for _, in := range []string{"include_macos", "include_windows"} {
		i, ok := on.On.Dispatch.Inputs[in]
		if !ok || i.Type != "boolean" || i.Default != false {
			t.Errorf("input %s: %+v (want a boolean defaulting to false)", in, i)
		}
	}
	if strings.Contains(raw, "\n  push:") || strings.Contains(raw, "\n  schedule:") || strings.Contains(raw, "\n  pull_request:") {
		t.Error("smoke.yml runs on something other than workflow_dispatch")
	}
	if g := w.Jobs["macos"].If; g != "${{ inputs.include_macos }}" {
		t.Errorf("macos runs if %q", g)
	}
	if g := w.Jobs["windows"].If; g != "${{ inputs.include_windows }}" {
		t.Errorf("windows runs if %q", g)
	}
	skip := w.Jobs["not-requested"]
	if skip.If != "${{ !inputs.include_macos || !inputs.include_windows }}" {
		t.Errorf("not-requested runs if %q", skip.If)
	}
	rec := skip.Steps[stepIndex(t, skip, "Record the skipped channels")].Run
	if !strings.Contains(rec, "not requested (owner approves macOS/Windows runs per release candidate)") || !strings.Contains(rec, "check-results.sh") {
		t.Errorf("not-requested does not record the owner's reason:\n%s", rec)
	}
	// Every Windows channel the windows job checks is recorded when skipped.
	win := w.Jobs["windows"]
	last := win.Steps[len(win.Steps)-1].Run
	for _, f := range strings.Fields(last) {
		if strings.Contains(f, "-ps") && !strings.Contains(rec, f) {
			t.Errorf("not-requested does not record %s", f)
		}
	}
	for _, name := range []string{"linux", "linux-host", "macos", "windows"} {
		j := w.Jobs[name]
		for i, s := range j.Steps {
			if i == 0 || s.Name == "Go version from go.mod's toolchain line" || strings.HasPrefix(s.Uses, "actions/setup-go@") {
				continue
			}
			if s.If != "${{ !cancelled() }}" {
				t.Errorf("%s step %d (%s%s) runs if %q, want ${{ !cancelled() }}", name, i, s.Name, s.Uses, s.If)
			}
		}
		if !strings.Contains(j.Steps[len(j.Steps)-1].Run, "scripts/smoke/check-results.sh") {
			t.Errorf("%s does not end with check-results.sh", name)
		}
	}
	if !strings.Contains(w.Jobs["snapshot"].Steps[stepIndex(t, w.Jobs["snapshot"], "Build the snapshot")].Run, "scripts/smoke/snapshot.sh") {
		t.Error("the snapshot job does not run snapshot.sh")
	}
	if strings.Count(raw, "curl -fsSLo") != strings.Count(raw, "| sha256sum -c -") {
		t.Error("a download in smoke.yml is not checksum-verified")
	}
}
