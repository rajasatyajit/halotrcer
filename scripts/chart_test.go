package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestChartDefaults: the umbrella installs nothing of its own, takes both
// tools' charts from their registry with a switch each, and both are off by
// default (D8).
func TestChartDefaults(t *testing.T) {
	var chart struct {
		Name         string `yaml:"name"`
		Dependencies []struct {
			Name       string `yaml:"name"`
			Version    string `yaml:"version"`
			Repository string `yaml:"repository"`
			Condition  string `yaml:"condition"`
		} `yaml:"dependencies"`
	}
	if err := yaml.Unmarshal(root(t, "charts/halotrcer/Chart.yaml"), &chart); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, d := range chart.Dependencies {
		names = append(names, d.Name)
		if d.Repository != "oci://ghcr.io/rajasatyajit/charts" || d.Condition != d.Name+".enabled" || d.Version != "0.0.0-dev" {
			t.Errorf("dependency %+v", d)
		}
	}
	if chart.Name != "halotrcer" || !slices.Equal(names, []string{"tokvera", "sievant"}) {
		t.Errorf("chart %q dependencies %v", chart.Name, names)
	}
	var values map[string]map[string]any
	if err := yaml.Unmarshal(root(t, "charts/halotrcer/values.yaml"), &values); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"tokvera", "sievant"} {
		if values[tool]["enabled"] != false {
			t.Errorf("%s.enabled = %v, want false by default (D8)", tool, values[tool]["enabled"])
		}
	}
	entries, err := os.ReadDir("../charts/halotrcer/templates")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "NOTES.txt" {
		t.Errorf("templates %v; the umbrella has no resources of its own", entries)
	}
}

func TestPushChart(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	fake := func(output string, code int) {
		writeFile(t, filepath.Join(bin, "helm"), "#!/bin/sh\nprintf '%s\\n' '"+output+"'\nexit "+string(rune('0'+code))+"\n", 0o755)
	}
	env := []string{"PATH=" + bin + ":" + os.Getenv("PATH")}
	run := func(repo string) (string, int) {
		cmd := exec.CommandContext(t.Context(), "sh", "release/push-chart.sh", "c.tgz", repo)
		cmd.Env = append(os.Environ(), env...)
		out, err := cmd.Output()
		if ee, ok := err.(*exec.ExitError); ok {
			return string(out), ee.ExitCode()
		}
		return string(out), 0
	}
	fake("Pushed: ghcr.io/x/charts/halotrcer:1.0.0\nDigest: "+digestA, 0)
	if out, code := run("oci://ghcr.io/x/charts"); code != 0 || out != digestA+"\n" {
		t.Errorf("push: exit %d stdout %q", code, out)
	}
	fake("Pushed: x\nDigest: "+digestA+"\nDigest: "+digestA, 0)
	if _, code := run("oci://ghcr.io/x/charts"); code != 1 {
		t.Errorf("two digest lines: exit %d, want 1", code)
	}
	fake("Error: denied", 1)
	if _, code := run("oci://ghcr.io/x/charts"); code != 1 {
		t.Errorf("failed push: exit %d, want 1", code)
	}
	if _, code := run("https://ghcr.io/x/charts"); code != 2 {
		t.Errorf("non-OCI repository: exit %d, want 2", code)
	}
}

// TestPackageChart vendors two fixture charts named like the tools' into the
// umbrella and packages it: the dependency versions are the pinned ones, the
// tools are off by default and render when switched on, the checkout is not
// modified, and a chart of the wrong name or version is refused.
func TestPackageChart(t *testing.T) {
	need(t, "helm")
	dir := t.TempDir()
	helm := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "helm", args...)
		cmd.Env = append(os.Environ(), "HELM_CONFIG_HOME="+filepath.Join(dir, "cfg"), "HELM_CACHE_HOME="+filepath.Join(dir, "cache"), "HELM_DATA_HOME="+filepath.Join(dir, "data"))
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("helm %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	sub := func(name, version string) string {
		src := filepath.Join(dir, "src", name+"-"+version, name)
		writeFile(t, filepath.Join(src, "Chart.yaml"), "apiVersion: v2\nname: "+name+"\nversion: "+version+"\n", 0o644)
		writeFile(t, filepath.Join(src, "templates", "cm.yaml"), "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: {{ .Release.Name }}-"+name+"\n", 0o644)
		out := filepath.Join(dir, "sub", name+"-"+version)
		helm("package", src, "--destination", out)
		return filepath.Join(out, name+"-"+version+".tgz")
	}
	tok, siv := sub("tokvera", "0.2.0"), sub("sievant", "0.2.1")
	before := string(root(t, "charts/halotrcer/Chart.yaml"))
	env := []string{"TOKVERA_CHART_VERSION=0.2.0", "SIEVANT_CHART_VERSION=0.2.1",
		"HELM_CONFIG_HOME=" + filepath.Join(dir, "cfg"), "HELM_CACHE_HOME=" + filepath.Join(dir, "cache"), "HELM_DATA_HOME=" + filepath.Join(dir, "data")}
	outDir := filepath.Join(dir, "out")
	if out, code := script(t, dir, env, "package-chart.sh", "v1.2.3", tok, siv, outDir); code != 0 {
		t.Fatalf("package-chart.sh (%d):\n%s", code, out)
	}
	tgz := filepath.Join(outDir, "halotrcer-1.2.3.tgz")
	if got := string(root(t, "charts/halotrcer/Chart.yaml")); got != before {
		t.Error("package-chart.sh modified the checkout's chart")
	}
	meta := helm("show", "chart", tgz)
	for _, want := range []string{"version: 1.2.3", "appVersion: 1.2.3", "name: tokvera\n  repository: oci://ghcr.io/rajasatyajit/charts\n  version: 0.2.0", "name: sievant\n  repository: oci://ghcr.io/rajasatyajit/charts\n  version: 0.2.1"} {
		if !strings.Contains(meta, want) {
			t.Errorf("packaged Chart.yaml lacks %q:\n%s", want, meta)
		}
	}
	if out := helm("template", "r", tgz); strings.Contains(out, "kind:") {
		t.Errorf("the default install renders resources (D8: both off):\n%s", out)
	}
	on := helm("template", "r", tgz, "--set", "tokvera.enabled=true", "--set", "sievant.enabled=true")
	if !strings.Contains(on, "name: r-tokvera") || !strings.Contains(on, "name: r-sievant") {
		t.Errorf("both enabled does not render both:\n%s", on)
	}
	only := helm("template", "r", tgz, "--set", "sievant.enabled=true")
	if strings.Contains(only, "r-tokvera") || !strings.Contains(only, "r-sievant") {
		t.Errorf("sievant alone:\n%s", only)
	}

	wrongVersion := sub("tokvera", "0.2.5")
	wrongName := sub("sievant", "0.2.0")
	for name, args := range map[string][]string{
		"wrong version": {"v1.2.3", wrongVersion, siv, filepath.Join(dir, "o2")},
		"swapped":       {"v1.2.3", siv, tok, filepath.Join(dir, "o3")},
		"sievant 0.2.0": {"v1.2.3", tok, wrongName, filepath.Join(dir, "o4")},
		"bad tag":       {"1.2.3", tok, siv, filepath.Join(dir, "o5")},
	} {
		if out, code := script(t, dir, env, "package-chart.sh", args...); code == 0 {
			t.Errorf("%s: packaged:\n%s", name, out)
		}
	}
}
