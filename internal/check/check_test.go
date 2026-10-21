package check

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rajasatyajit/halotrcer/internal/redact"
)

const fixtures = "../../testdata/fixtures"

// fakes writes fake tokvera and sievant scripts that replay the captured
// fixture outputs and append their arguments to a log.
type fakes struct {
	dir, log         string
	tokvera, sievant string
	tokveraVersion   string
	tokveraExit      int
	tokveraStderr    string
	sleep            string
}

func newFakes(t *testing.T) *fakes {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell fakes")
	}
	d := t.TempDir()
	return &fakes{dir: d, log: filepath.Join(d, "args.log"), tokveraVersion: "v0.2.0", sleep: "0"}
}

func abs(t *testing.T, p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func (f *fakes) write(t *testing.T) {
	t.Helper()
	tokReport := abs(t, filepath.Join(fixtures, "tokvera-f2a7906", "flaky.report.json"))
	svScan := abs(t, filepath.Join(fixtures, "sievant-d8a8bcf", "scan-smoke.json"))
	svReport := abs(t, filepath.Join(fixtures, "sievant-d8a8bcf", "report.json"))
	tok := `#!/bin/sh
echo "tokvera $*" >> '` + f.log + `'
if [ "$1" = version ]; then echo "tokvera ` + f.tokveraVersion + ` (commit abc1234)"; exit 0; fi
out=""; dry=0; prev=""
for a in "$@"; do
  [ "$prev" = "--out" ] && out="$a"
  [ "$a" = "--dry-run" ] && dry=1
  prev="$a"
done
if [ $dry = 1 ]; then echo "Scan plan for the target"; echo "dry run: nothing was sent."; exit 0; fi
sleep ` + f.sleep + `
if [ ` + strconv.Itoa(f.tokveraExit) + ` != 0 ]; then echo '` + f.tokveraStderr + `' >&2; exit ` + strconv.Itoa(f.tokveraExit) + `; fi
mkdir -p "$out" && cp '` + tokReport + `' "$out/report.json"
echo '{"level":"INFO","msg":"scan result"}' >&2
`
	sv := `#!/bin/sh
echo "sievant $*" >> '` + f.log + `'
case "$1" in
version) echo "sievant v0.2.1" >&2 ;;
scan) sleep ` + f.sleep + `; cat '` + svScan + `' ;;
report)
  prev=""; for a in "$@"; do [ "$prev" = "--out" ] && out="$a"; prev="$a"; done
  mkdir -p "$out" && cp '` + svReport + `' "$out/sievant-report-2026-10-20.json" ;;
esac
`
	f.tokvera = filepath.Join(f.dir, "tokvera")
	f.sievant = filepath.Join(f.dir, "sievant")
	for p, body := range map[string]string{f.tokvera: tok, f.sievant: sv} {
		if err := os.WriteFile(p, []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
}

func (f *fakes) args(t *testing.T) string {
	b, _ := os.ReadFile(f.log)
	return string(b)
}

func (f *fakes) env(stdout, stderr *bytes.Buffer, interactive bool, answer bool) Env {
	return Env{
		Stdout: stdout, Stderr: stderr,
		Environ:     []string{"PATH=/usr/bin:/bin", "HOME=/home/fixtureuser", "MY_LLM_KEY=sk-canary-abcdefghijk123456"}, // gitleaks:allow (synthetic canary)
		Generator:   "halotrcer test",
		Now:         func() time.Time { return time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC) },
		Interactive: interactive,
		Confirm:     func(string) bool { return answer },
		Find: func(tool string) (string, error) {
			if tool == "tokvera" {
				return f.tokvera, nil
			}
			return f.sievant, nil
		},
		Redactor: redact.New(redact.Options{Home: "/home/fixtureuser", Users: []string{"fixtureuser"}, Secrets: []string{"sk-canary-abcdefghijk123456"}}),
	}
}

func opts(t *testing.T) Options {
	return Options{
		Target: "https://u:p@llm.example.com/v1?key=x", APIKeyEnv: "MY_LLM_KEY", Suite: "quick", Yes: true,
		MCPConfigs: []string{"a.json", "b.jsonc"}, NoProject: true,
		Out: filepath.Join(t.TempDir(), "out"), Formats: []string{"html", "pdf", "json"},
		Timeout: time.Minute, MonthlyRequests: 1000000,
	}
}

func TestCheckEndToEnd(t *testing.T) {
	f := newFakes(t)
	f.write(t)
	var out, errb bytes.Buffer
	o := opts(t)
	o.GatewayReport = true
	if code := Run(context.Background(), o, f.env(&out, &errb, false, false)); code != ExitOK {
		t.Fatalf("exit %d\nstderr: %s", code, errb.String())
	}
	args := f.args(t)
	for _, want := range []string{
		"tokvera --json scan --no-cta --out ",
		"--target https://u:p@llm.example.com/v1?key=x --api-key-env MY_LLM_KEY --suite quick --yes",
		"sievant scan --json --config a.json --config b.jsonc --no-project",
		"sievant report --format json --no-cta --out ",
		"--client-config a.json --client-config b.jsonc --no-project",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("args lack %q:\n%s", want, args)
		}
	}
	if strings.Contains(args, "--dry-run") {
		t.Error("--yes still ran the dry-run prompt")
	}
	for _, ext := range []string{"html", "pdf", "json"} {
		b, err := os.ReadFile(filepath.Join(o.Out, "report."+ext))
		if err != nil {
			t.Fatal(err)
		}
		if ext == "html" {
			for _, s := range []string{"Cost &amp; performance", "Agent security", "Gateway coverage", "30-day fix plan"} {
				if !strings.Contains(string(b), s) {
					t.Errorf("html lacks %q", s)
				}
			}
		}
		if l := redact.Leaks(string(b), "u:p@", "key=x", "sk-canary", "fixtureuser"); len(l) > 0 {
			t.Errorf("report.%s leaks %v", ext, l)
		}
	}
	so := out.String()
	for _, s := range []string{"Efficiency grade (Tokvera): F (Provisional scoring 0.9.0)", "Agent security (Sievant): 1 critical", "Reliability concern", "Report: ", "spend, not a saving"} {
		if !strings.Contains(so, s) {
			t.Errorf("stdout lacks %q:\n%s", s, so)
		}
	}
	if l := redact.Leaks(so+errb.String(), "u:p@", "key=x", "sk-canary", "fixtureuser"); len(l) > 0 {
		t.Errorf("terminal leaks %v", l)
	}
}

func TestParallel(t *testing.T) {
	f := newFakes(t)
	f.sleep = "1"
	f.write(t)
	var out, errb bytes.Buffer
	start := time.Now()
	if code := Run(context.Background(), opts(t), f.env(&out, &errb, false, false)); code != ExitOK {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if d := time.Since(start); d > 1900*time.Millisecond {
		t.Errorf("took %v: the scans did not run in parallel", d)
	}
}

func TestConfirmation(t *testing.T) {
	f := newFakes(t)
	f.write(t)
	o := opts(t)
	o.Yes = false
	var out, errb bytes.Buffer
	if code := Run(context.Background(), o, f.env(&out, &errb, false, false)); code != ExitUsage {
		t.Errorf("no terminal, no --yes: exit %d", code)
	}
	if strings.Contains(f.args(t), " scan --no-cta") {
		t.Error("load sent without confirmation")
	}
	errb.Reset()
	if code := Run(context.Background(), o, f.env(&out, &errb, true, false)); code != ExitFailure {
		t.Errorf("declined: exit %d", code)
	}
	if !strings.Contains(f.args(t), "--dry-run") || strings.Count(f.args(t), "--yes") != 0 {
		t.Errorf("declined run: %s", f.args(t))
	}
	if code := Run(context.Background(), o, f.env(&out, &errb, true, true)); code != ExitOK {
		t.Errorf("confirmed: exit %d: %s", code, errb.String())
	}
	if strings.Count(f.args(t), "--yes") != 1 {
		t.Errorf("confirmed run: %s", f.args(t))
	}
}

func TestPartial(t *testing.T) {
	f := newFakes(t)
	f.tokveraExit = 1
	f.tokveraStderr = `{"level":"ERROR","msg":"scan failed","error":"dial https://llm.example.com/v1 with sk-canary-abcdefghijk123456 at /home/fixtureuser/x"}`
	f.write(t)
	o := opts(t)
	var out, errb bytes.Buffer
	if code := Run(context.Background(), o, f.env(&out, &errb, false, false)); code != ExitFailure {
		t.Errorf("exit %d", code)
	}
	b, err := os.ReadFile(filepath.Join(o.Out, "report.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "Tokvera did not complete") || !strings.Contains(string(b), "Agent security") {
		t.Error("partial report lacks the failure or the Sievant section")
	}
	if l := redact.Leaks(string(b)+errb.String(), "sk-canary", "fixtureuser"); len(l) > 0 {
		t.Errorf("leaks %v", l)
	}
	o.AllowPartial = true
	if code := Run(context.Background(), o, f.env(&out, &errb, false, false)); code != ExitOK {
		t.Errorf("--allow-partial: exit %d", code)
	}
}

func TestCompatWarning(t *testing.T) {
	f := newFakes(t)
	f.tokveraVersion = "v0.1.0-dev"
	f.write(t)
	o := opts(t)
	var out, errb bytes.Buffer
	if code := Run(context.Background(), o, f.env(&out, &errb, false, false)); code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(errb.String(), "warning: tokvera v0.1.0-dev is outside") {
		t.Errorf("stderr: %s", errb.String())
	}
	b, _ := os.ReadFile(filepath.Join(o.Out, "report.json"))
	if !strings.Contains(string(b), `"compat_warning": "tokvera v0.1.0-dev is outside`) {
		t.Error("warning not in the report")
	}
}

func TestValidate(t *testing.T) {
	base := Options{Target: "https://x.example/v1", Formats: []string{"html"}, Timeout: time.Minute}
	cases := map[string]func(*Options){
		"no target":        func(o *Options) { o.Target = "" },
		"bad scheme":       func(o *Options) { o.Target = "ftp://x" },
		"demo and target":  func(o *Options) { o.Demo = true },
		"key value pasted": func(o *Options) { o.APIKeyEnv = "sk-live-0123456789" }, // gitleaks:allow (synthetic canary)
		"skip both":        func(o *Options) { o.SkipTokvera, o.SkipSievant = true, true },
		"negative volume":  func(o *Options) { o.MonthlyRequests = -1 },
		"no format":        func(o *Options) { o.Formats = nil },
		"zero timeout":     func(o *Options) { o.Timeout = 0 },
	}
	for name, mut := range cases {
		o := base
		mut(&o)
		err := o.Validate()
		if err == nil {
			t.Errorf("%s: accepted", name)
		} else if strings.Contains(err.Error(), "sk-live") {
			t.Errorf("%s: echoed the key", name)
		}
	}
	ok := base
	if err := ok.Validate(); err != nil {
		t.Error(err)
	}
	demo := Options{Demo: true, Formats: []string{"json"}, Timeout: time.Minute}
	if err := demo.Validate(); err != nil {
		t.Error(err)
	}
	sievantOnly := Options{SkipTokvera: true, Formats: []string{"json"}, Timeout: time.Minute}
	if err := sievantOnly.Validate(); err != nil {
		t.Error(err)
	}
}

func TestDemoArgs(t *testing.T) {
	o := Options{Demo: true}
	got := strings.Join(o.tokveraArgs("/o"), " ")
	if got != "--json scan --no-cta --out /o --demo --yes" {
		t.Errorf("%q", got)
	}
}

// The --timeout budget starts after the prompt: a slow "yes" must not
// leave the scans without time.
func TestTimeoutStartsAfterConfirmation(t *testing.T) {
	f := newFakes(t)
	f.write(t)
	o := opts(t)
	o.Yes = false
	o.Timeout = 2 * time.Second
	var out, errb bytes.Buffer
	e := f.env(&out, &errb, true, true)
	e.Confirm = func(string) bool { time.Sleep(2500 * time.Millisecond); return true }
	if code := Run(context.Background(), o, e); code != ExitOK {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
}
