// Package check runs `halotrcer check`: Tokvera's scan of the inference
// endpoint and Sievant's scan of this machine's MCP client configuration,
// in parallel (they share nothing: one sends load to the target, the other
// reads local files without starting any server), then writes the combined
// report.
package check

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rajasatyajit/halotrcer/internal/compat"
	"github.com/rajasatyajit/halotrcer/internal/ingest"
	"github.com/rajasatyajit/halotrcer/internal/redact"
	"github.com/rajasatyajit/halotrcer/internal/report"
	"github.com/rajasatyajit/halotrcer/internal/tools"
)

// Exit codes (docs/check.md).
const (
	ExitOK      = 0
	ExitFailure = 1
	ExitUsage   = 2
)

// Options are the check's flags.
type Options struct {
	Target             string
	Demo               bool
	Kind               string
	Model              string
	APIKeyEnv          string
	Suite              string
	MaxRequests        string
	MaxCost            string
	RequestTimeout     string
	Pricing            string
	AllowPrivateTarget bool
	Yes                bool

	MCPConfigs    []string
	NoProject     bool
	GatewayReport bool

	MonthlyRequests int64
	Out             string
	Formats         []string
	NoCTA           bool
	SkipTokvera     bool
	SkipSievant     bool
	AllowPartial    bool
	Timeout         time.Duration
}

// Env is the outside world, injectable for tests.
type Env struct {
	Stdout, Stderr io.Writer
	Environ        []string
	Generator      string
	Now            func() time.Time
	// Interactive reports whether a person can answer a prompt.
	Interactive bool
	// Confirm asks a yes/no question; nil means no one can answer.
	Confirm func(question string) bool
	// Find locates a tool (tools.Find by default).
	Find func(tool string) (string, error)
	// Redactor scrubs everything shown; built from the environment when nil.
	Redactor *redact.Redactor
}

var envNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Validate checks the options before anything runs.
func (o *Options) Validate() error {
	if o.SkipTokvera && o.SkipSievant {
		return errors.New("--skip-tokvera and --skip-sievant together leave nothing to run")
	}
	if !o.SkipTokvera {
		switch {
		case o.Demo && o.Target != "":
			return errors.New("--demo and --target are exclusive: --demo scans Tokvera's built-in simulator")
		case !o.Demo && o.Target == "":
			return errors.New("--target URL is required (or --demo, or --skip-tokvera)")
		case o.Target != "":
			u, err := url.Parse(o.Target)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return errors.New("--target must be an http or https URL")
			}
		}
	}
	if o.APIKeyEnv != "" && !envNameRE.MatchString(o.APIKeyEnv) {
		// Never echo it: it may be a key pasted by mistake.
		return errors.New("--api-key-env takes the NAME of an environment variable (such as OPENAI_API_KEY), never the key itself")
	}
	if o.MonthlyRequests < 0 {
		return errors.New("--monthly-requests must be positive")
	}
	if len(o.Formats) == 0 {
		return errors.New("no report format")
	}
	if o.Timeout <= 0 {
		return errors.New("--timeout must be positive")
	}
	return nil
}

func (o *Options) tokveraArgs(out string) []string {
	args := []string{"--json", "scan", "--no-cta", "--out", out}
	if o.Demo {
		return append(args, "--demo", "--yes")
	}
	args = append(args, "--target", o.Target)
	for _, kv := range [][2]string{
		{"--kind", o.Kind}, {"--model", o.Model}, {"--api-key-env", o.APIKeyEnv}, {"--suite", o.Suite},
		{"--max-requests", o.MaxRequests}, {"--max-cost", o.MaxCost}, {"--request-timeout", o.RequestTimeout},
		{"--pricing", o.Pricing},
	} {
		if kv[1] != "" {
			args = append(args, kv[0], kv[1])
		}
	}
	if o.AllowPrivateTarget {
		args = append(args, "--allow-private-target")
	}
	return args
}

func (o *Options) sievantArgs() []string {
	args := []string{"scan", "--json"}
	for _, c := range o.MCPConfigs {
		args = append(args, "--config", c)
	}
	if o.NoProject {
		args = append(args, "--no-project")
	}
	return args
}

func (o *Options) gatewayArgs(out string) []string {
	args := []string{"report", "--format", "json", "--no-cta", "--out", out}
	for _, c := range o.MCPConfigs {
		args = append(args, "--client-config", c)
	}
	if o.NoProject {
		args = append(args, "--no-project")
	}
	return args
}

type toolState struct {
	name    string
	path    string
	version string
	run     report.ToolRun
}

// Run performs the check and returns the exit code.
func Run(ctx context.Context, o Options, e Env) int {
	if e.Now == nil {
		e.Now = time.Now
	}
	if e.Find == nil {
		e.Find = tools.Find
	}
	if e.Redactor == nil {
		var extra []string
		if o.APIKeyEnv != "" {
			extra = append(extra, o.APIKeyEnv)
		}
		e.Redactor = redact.New(redact.FromEnvironment(extra...))
	}
	rd := e.Redactor
	var logMu sync.Mutex // the two scans log from their own goroutines
	logf := func(format string, a ...any) {
		logMu.Lock()
		defer logMu.Unlock()
		_, _ = fmt.Fprintln(e.Stderr, rd.String(fmt.Sprintf(format, a...)))
	}

	if err := o.Validate(); err != nil {
		logf("halotrcer check: %v", err)
		return ExitUsage
	}
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()
	m := compat.Load()

	var tok, sv *toolState
	var states []*toolState
	prep := func(name string, skip bool) *toolState {
		st := &toolState{name: name, run: report.ToolRun{Name: name}}
		states = append(states, st)
		if skip {
			st.run.Status = "skipped"
			return nil
		}
		p, err := e.Find(name)
		if err != nil {
			st.run.Status, st.run.Error = "failed", err.Error()
			logf("halotrcer check: %v", err)
			return nil
		}
		st.path = p
		st.version, st.run.CompatWarning = probeVersion(ctx, name, p, e.Environ, m)
		st.run.Version = st.version
		if st.run.CompatWarning != "" {
			logf("warning: %s", st.run.CompatWarning)
		}
		return st
	}
	tok = prep(tools.Tokvera, o.SkipTokvera)
	sv = prep(tools.Sievant, o.SkipSievant)
	if tok == nil && sv == nil {
		logf("halotrcer check: neither tool can run")
		return ExitFailure
	}

	tmp, err := os.MkdirTemp("", "halotrcer-check-*")
	if err != nil {
		logf("halotrcer check: %v", err)
		return ExitFailure
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	tokEnv := tools.Env(tools.Tokvera, e.Environ, o.APIKeyEnv)
	if tok != nil && !o.Demo && !o.Yes {
		if code, ok := confirm(ctx, o, e, tok.path, tokEnv, filepath.Join(tmp, "plan"), rd); !ok {
			return code
		}
	}

	var (
		wg      sync.WaitGroup
		tokRep  *ingest.TokveraReport
		svRep   *ingest.SievantScan
		gateway *ingest.SievantGateway
	)
	if tok != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			logf("tokvera: scanning %s", targetLabel(o))
			out := filepath.Join(tmp, "tokvera")
			args := o.tokveraArgs(out)
			if !o.Demo {
				args = append(args, "--yes") // confirmed above, or --yes given
			}
			res := tools.Run(ctx, tok.path, args, tokEnv, "")
			if res.Err != nil || res.ExitCode != 0 {
				tok.run.Status, tok.run.Error = "failed", res.Summary()
				return
			}
			f, err := os.Open(filepath.Join(out, "report.json"))
			if err != nil {
				tok.run.Status, tok.run.Error = "failed", "no report.json: "+err.Error()
				return
			}
			defer func() { _ = f.Close() }()
			if tokRep, err = ingest.DecodeTokvera(f, m); err != nil {
				tok.run.Status, tok.run.Error = "failed", err.Error()
				return
			}
			tok.run.Status = "ok"
		}()
	}
	if sv != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			logf("sievant: scanning this machine's MCP client configuration")
			svEnv := tools.Env(tools.Sievant, e.Environ)
			res := tools.Run(ctx, sv.path, o.sievantArgs(), svEnv, "")
			if res.Err != nil || res.ExitCode != 0 {
				sv.run.Status, sv.run.Error = "failed", res.Summary()
				return
			}
			var err error
			if svRep, err = ingest.DecodeSievantScan(bytes.NewReader(res.Stdout), m); err != nil {
				sv.run.Status, sv.run.Error = "failed", err.Error()
				return
			}
			sv.run.Status = "ok"
			if o.GatewayReport {
				gateway = runGateway(ctx, o, sv.path, svEnv, filepath.Join(tmp, "sievant-report"), m, logf)
			}
		}()
	}
	wg.Wait()

	runs := make([]report.ToolRun, 0, len(states))
	failed := false
	for _, st := range states {
		if st.run.Status == "failed" {
			failed = true
			logf("%s: %s", st.name, st.run.Error)
		}
		runs = append(runs, st.run)
	}
	if tokRep == nil && svRep == nil {
		logf("halotrcer check: no tool produced a result; no report written")
		return ExitFailure
	}

	r := report.Build(report.Inputs{
		Generator: e.Generator, Now: e.Now(), Target: targetLabel(o),
		Tokvera: tokRep, Sievant: svRep, Gateway: gateway, Tools: runs,
		MonthlyRequests: o.MonthlyRequests, NoCTA: o.NoCTA, Redactor: rd,
	})
	paths, err := r.WriteFiles(o.Out, o.Formats)
	if err != nil {
		logf("halotrcer check: writing the report: %v", err)
		return ExitFailure
	}
	printSummary(e.Stdout, r, paths, rd)
	if failed && !o.AllowPartial {
		return ExitFailure
	}
	return ExitOK
}

func targetLabel(o Options) string {
	if o.Demo {
		return "Tokvera built-in demo server (simulated)"
	}
	return o.Target
}

// probeVersion runs `<tool> version` and checks the matrix. The warning is
// empty when the version is supported.
func probeVersion(ctx context.Context, name, path string, environ []string, m compat.Matrix) (string, string) {
	vctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	res := tools.Run(vctx, path, []string{"version"}, tools.Env(name, environ), "")
	v, err := compat.ParseVersionLine(name, string(res.Stdout)+"\n"+string(res.Stderr))
	if err != nil {
		return "", err.Error()
	}
	if err := m.Check(name, v); err != nil {
		return v, err.Error()
	}
	return v, ""
}

// confirm shows Tokvera's own plan (scan --dry-run: nothing is sent) and
// asks before any load goes to the target, as Tokvera itself would.
func confirm(ctx context.Context, o Options, e Env, path string, env []string, out string, rd *redact.Redactor) (int, bool) {
	if !e.Interactive || e.Confirm == nil {
		_, _ = fmt.Fprintln(e.Stderr, "halotrcer check: Tokvera sends load to the target; with no terminal to confirm, pass --yes")
		return ExitUsage, false
	}
	res := tools.Run(ctx, path, append(o.tokveraArgs(out), "--dry-run"), env, "")
	if res.Err != nil || res.ExitCode != 0 {
		_, _ = fmt.Fprintln(e.Stderr, rd.String("tokvera: the scan plan failed: "+res.Summary()))
		return ExitFailure, false
	}
	_, _ = io.WriteString(e.Stderr, rd.String(string(res.Stdout)))
	if !e.Confirm("Send this load to the target? [y/N] ") {
		_, _ = fmt.Fprintln(e.Stderr, "halotrcer check: not confirmed; nothing was sent")
		return ExitFailure, false
	}
	return ExitOK, true
}

func runGateway(ctx context.Context, o Options, path string, env []string, out string, m compat.Matrix, logf func(string, ...any)) *ingest.SievantGateway {
	res := tools.Run(ctx, path, o.gatewayArgs(out), env, "")
	if res.Err != nil || res.ExitCode != 0 {
		logf("sievant report: %s (the gateway section is left out)", res.Summary())
		return nil
	}
	matches, _ := filepath.Glob(filepath.Join(out, "sievant-report-*.json"))
	if len(matches) == 0 {
		logf("sievant report: wrote no JSON file (the gateway section is left out)")
		return nil
	}
	sort.Strings(matches)
	b, err := os.ReadFile(matches[len(matches)-1])
	if err != nil {
		logf("sievant report: %v", err)
		return nil
	}
	gw, err := ingest.DecodeSievantGateway(bytes.NewReader(b), m)
	if err != nil {
		logf("sievant report: %v (the gateway section is left out)", err)
		return nil
	}
	return gw
}

func printSummary(w io.Writer, r *report.Report, paths []string, rd *redact.Redactor) {
	var b strings.Builder
	s := r.Summary
	if c := r.CostPerformance; c != nil {
		g := "Efficiency grade (Tokvera): " + gradeText(s.Grade)
		if s.GradeProvisional {
			g += " (" + report.TokveraProvisionalLabel + " " + c.ScoringVersion + ")"
		}
		b.WriteString(g + "\n")
		if s.USDPer1kRequests != nil {
			fmt.Fprintf(&b, "Cost per 1,000 requests: $%g (measured; rates as of %s)\n", *s.USDPer1kRequests, c.Cost.PricingAsOf)
		} else {
			b.WriteString("Cost per 1,000 requests: not computed\n")
		}
		if s.Spend != nil {
			fmt.Fprintf(&b, "At %d requests/month: $%.2f a month (spend, not a saving)\n", s.Spend.MonthlyRequests, s.Spend.MonthlyUSD)
		}
		if s.ReliabilityConcern != "" {
			b.WriteString("Reliability concern: " + s.ReliabilityConcern + "\n")
		}
	}
	if a := r.AgentSecurity; a != nil {
		fmt.Fprintf(&b, "Agent security (Sievant): %d critical, %d high, %d medium; deal risk: %d\n",
			a.Counts["critical"], a.Counts["high"], a.Counts["medium"], s.DealRisk)
	}
	if len(s.Top) > 0 {
		b.WriteString("Top findings:\n")
		for i, it := range s.Top {
			fmt.Fprintf(&b, "  %d. %s (%s %s)\n", i+1, it.Title, it.Source, it.Ref)
		}
	}
	for _, p := range paths {
		b.WriteString("Report: " + p + "\n")
	}
	_, _ = io.WriteString(w, rd.String(b.String()))
}

func gradeText(g string) string {
	if g == "insufficient_evidence" {
		return "no grade (insufficient evidence)"
	}
	return g
}
