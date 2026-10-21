package scripts

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/rajasatyajit/halotrcer/internal/install"
)

type workflowStep struct {
	Name string            `yaml:"name"`
	Uses string            `yaml:"uses"`
	If   string            `yaml:"if"`
	Run  string            `yaml:"run"`
	With map[string]any    `yaml:"with"`
	Env  map[string]string `yaml:"env"`
}

type workflowJob struct {
	If          string            `yaml:"if"`
	Needs       any               `yaml:"needs"`
	RunsOn      any               `yaml:"runs-on"`
	Environment string            `yaml:"environment"`
	Permissions map[string]string `yaml:"permissions"`
	Concurrency map[string]any    `yaml:"concurrency"`
	Steps       []workflowStep    `yaml:"steps"`
}

type workflow struct {
	Permissions any                    `yaml:"permissions"`
	Jobs        map[string]workflowJob `yaml:"jobs"`
}

func readWorkflow(t *testing.T, name string) (workflow, string) {
	t.Helper()
	raw := root(t, ".github/workflows/"+name)
	var w workflow
	if err := yaml.Unmarshal(raw, &w); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return w, string(raw)
}

// stepIndex is the index of the first step whose name or run contains sub.
func stepIndex(t *testing.T, job workflowJob, sub string) int {
	t.Helper()
	for i, s := range job.Steps {
		if strings.Contains(s.Name, sub) || strings.Contains(s.Run, sub) {
			return i
		}
	}
	t.Fatalf("no step with %q", sub)
	return -1
}

// TestWorkflowRules applies CLAUDE.md's CI rules to every workflow: actions
// pinned by full commit SHA with the release in a comment, every checkout
// without persisted credentials, and no ${{ }} expression inside a run:
// script (inputs reach scripts through env:). Runners are
// TestSelfHostedRunnerRules'.
func TestWorkflowRules(t *testing.T) {
	files, err := filepath.Glob("../.github/workflows/*.yml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no workflows: %v", err)
	}
	usesLine := regexp.MustCompile(`(?m)^\s+(?:-\s+)?uses:\s*(\S+)(.*)$`)
	pinned := regexp.MustCompile(`^[\w.-]+/[\w.-]+(/[\w./-]+)?@[0-9a-f]{40}$`)
	for _, f := range files {
		name := filepath.Base(f)
		w, raw := readWorkflow(t, name)
		for _, m := range usesLine.FindAllStringSubmatch(raw, -1) {
			if !pinned.MatchString(m[1]) || !regexp.MustCompile(`^\s+# v\d`).MatchString(m[2]) {
				t.Errorf("%s: %s%s is not pinned by commit SHA with its release in a comment", name, m[1], m[2])
			}
		}
		for jn, j := range w.Jobs {
			for _, s := range j.Steps {
				if strings.Contains(s.Run, "${{") {
					t.Errorf("%s/%s step %q has ${{ }} inside run:", name, jn, s.Name)
				}
				if strings.HasPrefix(s.Uses, "actions/checkout@") && s.With["persist-credentials"] != false {
					t.Errorf("%s/%s checkout persists credentials", name, jn)
				}
			}
		}
	}
}

// TestReleaseWorkflow pins release.yml's two phases, their permissions and
// the order of the checks before anything public moves. Each phase is one
// job in the release environment, its preflight checks its first steps, so
// no step that receives a secret runs before the owner approves.
func TestReleaseWorkflow(t *testing.T) {
	w, raw := readWorkflow(t, "release.yml")
	if p, ok := w.Permissions.(map[string]any); !ok || len(p) != 0 {
		t.Errorf("top-level permissions %v, want {}", w.Permissions)
	}
	want := map[string]map[string]string{
		"draft":   {"contents": "write", "id-token": "write", "attestations": "write"},
		"publish": {"contents": "read", "packages": "write", "id-token": "write", "attestations": "write"},
	}
	if len(w.Jobs) != len(want) {
		t.Errorf("jobs %v", w.Jobs)
	}
	for name, perms := range want {
		j, ok := w.Jobs[name]
		if !ok {
			t.Fatalf("no job %s", name)
		}
		if len(j.Permissions) != len(perms) {
			t.Errorf("%s permissions %v, want %v", name, j.Permissions, perms)
		}
		for k, v := range perms {
			if j.Permissions[k] != v {
				t.Errorf("%s permissions[%s] = %q, want %q", name, k, j.Permissions[k], v)
			}
		}
	}
	for name, j := range w.Jobs {
		if j.Environment != "release" {
			t.Errorf("%s runs outside the release environment", name)
		}
	}
	if w.Jobs["draft"].If != "github.event_name == 'push'" ||
		w.Jobs["publish"].If != "github.event_name == 'release' && startsWith(github.event.release.tag_name, 'v')" {
		t.Errorf("phase triggers: draft %q, publish %q", w.Jobs["draft"].If, w.Jobs["publish"].If)
	}
	if g := w.Jobs["publish"].Concurrency["group"]; g != "packages-gh-pages" {
		t.Errorf("publish concurrency group %v, want packages-gh-pages (the shared repositories)", g)
	}

	draft := w.Jobs["draft"]
	release := stepIndex(t, draft, "goreleaser release")
	if !strings.Contains(draft.Steps[release].Run, "--skip=docker,homebrew,scoop,winget") {
		t.Error("the draft phase publishes a channel before the owner has read the draft")
	}
	// The draft's checks, in order, all before anything is installed for
	// the build, keys are written or anything is built or uploaded.
	dPre := stepIndex(t, draft, "preflight.sh draft")
	dKeys := stepIndex(t, draft, "write-keys.sh --check")
	dBundled := stepIndex(t, draft, "scripts/release/bundled-chart.sh")
	dGates := stepIndex(t, draft, "Gates (at the tagged commit)")
	dBuild := stepIndex(t, draft, "Install goreleaser")
	dWrite := stepIndex(t, draft, "Write the package-signing keys")
	if dPre != 1 {
		t.Errorf("the draft phase does something before its preflight (step %d)", dPre)
	}
	if order := []int{dPre, dKeys, dBundled, dGates, dBuild, dWrite, release}; !slices.IsSorted(order) || len(slices.Compact(slices.Clone(order))) != len(order) {
		t.Errorf("draft order: preflight %d, keys %d, chart %d, gates %d, build tools %d, write keys %d, release %d", dPre, dKeys, dBundled, dGates, dBuild, dWrite, release)
	}
	for i, s := range draft.Steps[:dGates+1] {
		if _, ok := s.Env["GITHUB_TOKEN"]; ok || s.Env["GH_TOKEN"] != "" {
			t.Errorf("draft step %d %q gets a token before the gates pass", i, s.Name)
		}
	}

	pub := w.Jobs["publish"]
	// Preflight and the keys check come straight after the checkout.
	if stepIndex(t, pub, "preflight.sh publish") != 1 || stepIndex(t, pub, "write-keys.sh --check") != 2 {
		t.Error("the publish phase does something before its preflight")
	}
	verify := stepIndex(t, pub, "Verify the published checksums.txt")
	bundled := stepIndex(t, pub, "Verify the bundled Tokvera and Sievant artifacts")
	rebuild := stepIndex(t, pub, "Rebuild, publishing nothing")
	compare := stepIndex(t, pub, "Every archive is the published one")
	publish := stepIndex(t, pub, "Publish the image, the Homebrew cask")
	chart := stepIndex(t, pub, "Package the Helm chart")
	push := stepIndex(t, pub, "Push the Helm chart")
	repos := stepIndex(t, pub, "Publish the signed apt and yum repositories")
	// Nothing is published before the bundled artifacts verify and the
	// chart builds from them; the chart is pushed after the other channels.
	if order := []int{verify, bundled, chart, rebuild, compare, publish, push, repos}; !slices.IsSorted(order) || len(slices.Compact(slices.Clone(order))) != len(order) {
		t.Errorf("publish order: verify %d, bundled %d, chart %d, rebuild %d, compare %d, publish %d, push %d, repos %d", verify, bundled, chart, rebuild, compare, publish, push, repos)
	}
	for _, job := range []workflowJob{draft, pub} {
		i := stepIndex(t, job, "scripts/release/verify-bundled.sh")
		if job.Steps[i].Run == "" || !strings.Contains(job.Steps[i].Run, "bundled-tools.sh") {
			t.Error("the bundled artifacts are not verified from bundled-tools.sh's output")
		}
	}
	if src := string(root(t, "scripts/release/verify-bundled.sh")); !strings.Contains(src, `--certificate-identity "$4"`) || strings.Contains(src, "identity-regexp") {
		t.Error("the bundled artifacts are not verified against their exact release identity")
	}
	id := install.Halotrcer.CosignIdentity()
	if !strings.Contains(pub.Steps[verify].Run, "--certificate-identity-regexp '"+id+"'") {
		t.Errorf("checksums.txt is not verified against %s", id)
	}
	if !strings.Contains(pub.Steps[publish].Run, "--skip=nfpm,sbom") {
		t.Error("the publish phase rebuilds packages (their signatures would differ from the published ones)")
	}
	for i, s := range pub.Steps {
		if strings.Contains(s.Name, "apt and yum") || strings.Contains(s.Name, "published packages") || strings.Contains(s.Name, "repository signing key") {
			if s.If != "${{ !contains(github.event.release.tag_name, '-') }}" {
				t.Errorf("step %d %q lets a release candidate into the package repositories", i, s.Name)
			}
		}
	}
	for _, s := range pub.Steps {
		if strings.Contains(s.Run, "--skip=sign") && strings.Contains(s.Run, "goreleaser release --clean --skip=nfpm") {
			t.Error("a real release skips signing")
		}
	}
	if !strings.Contains(raw, "REPO_PUSH_ATTEMPTS: \"3\"") {
		t.Error("the shared repositories' push is not retried 3 times")
	}
	// Every downloaded tool is checksum-verified.
	if strings.Count(raw, "curl -fsSLo") != strings.Count(raw, "| sha256sum -c -") {
		t.Errorf("%d downloads, %d checksum checks", strings.Count(raw, "curl -fsSLo"), strings.Count(raw, "| sha256sum -c -"))
	}
}
