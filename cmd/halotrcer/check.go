package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/rajasatyajit/halotrcer/internal/check"
	"github.com/rajasatyajit/halotrcer/internal/report"
)

const checkUsage = `usage: halotrcer check (--target URL | --demo) [flags]

Runs Tokvera against the inference endpoint and Sievant against this
machine's MCP client configuration, in parallel, and writes one combined
report (HTML, PDF and JSON) into --out.

Tokvera sends load to the target: halotrcer shows Tokvera's plan and asks
first, or give --yes. Sievant only reads configuration files.

target (Tokvera):
  --target URL              the inference endpoint (OpenAI-compatible or Anthropic)
  --demo                    scan Tokvera's built-in simulator instead (no endpoint, no keys)
  --kind KIND               openai_compatible or anthropic (Tokvera detects it otherwise)
  --model NAME              the model, when the server lists several
  --api-key-env NAME        the NAME of the variable holding the API key (never the key)
  --suite SUITE             quick, standard or deep (default quick)
  --max-requests N          Tokvera's request cap
  --max-cost USD            Tokvera's cost cap
  --request-timeout D       per-request timeout (Tokvera's default 60s)
  --pricing FILE            your rate sheet, passed to Tokvera unchanged
  --allow-private-target    allow a link-local or metadata address (Tokvera's flag)
  --yes                     do not ask before sending the load

agent security (Sievant):
  --mcp-config PATH         scan this MCP client configuration instead of discovery (repeatable)
  --no-project              skip MCP files in the current directory
  --gateway-report          also include sievant report (gateway coverage and gaps)

report:
  --out DIR                 where to write the report (default halotrcer-report)
  --format LIST             html, pdf, json, comma-separated (default html,pdf,json)
  --monthly-requests N      your monthly request volume: shows measured cost x volume
  --no-cta                  leave out the footer call to action
  --skip-tokvera            run Sievant only
  --skip-sievant            run Tokvera only
  --allow-partial           exit 0 when one tool failed but a report was written
  --timeout D               bound on the whole check (default 5m)

Exit codes: 0 report written; 1 a tool or the report failed; 2 usage.
`

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func runCheck(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var o check.Options
	var formats, monthly string
	var configs multiFlag
	fs.StringVar(&o.Target, "target", "", "")
	fs.BoolVar(&o.Demo, "demo", false, "")
	fs.StringVar(&o.Kind, "kind", "", "")
	fs.StringVar(&o.Model, "model", "", "")
	fs.StringVar(&o.APIKeyEnv, "api-key-env", "", "")
	fs.StringVar(&o.Suite, "suite", "quick", "")
	fs.StringVar(&o.MaxRequests, "max-requests", "", "")
	fs.StringVar(&o.MaxCost, "max-cost", "", "")
	fs.StringVar(&o.RequestTimeout, "request-timeout", "", "")
	fs.StringVar(&o.Pricing, "pricing", "", "")
	fs.BoolVar(&o.AllowPrivateTarget, "allow-private-target", false, "")
	fs.BoolVar(&o.Yes, "yes", false, "")
	fs.Var(&configs, "mcp-config", "")
	fs.BoolVar(&o.NoProject, "no-project", false, "")
	fs.BoolVar(&o.GatewayReport, "gateway-report", false, "")
	fs.StringVar(&o.Out, "out", "halotrcer-report", "")
	fs.StringVar(&formats, "format", "html,pdf,json", "")
	fs.StringVar(&monthly, "monthly-requests", "", "")
	fs.BoolVar(&o.NoCTA, "no-cta", false, "")
	fs.BoolVar(&o.SkipTokvera, "skip-tokvera", false, "")
	fs.BoolVar(&o.SkipSievant, "skip-sievant", false, "")
	fs.BoolVar(&o.AllowPartial, "allow-partial", false, "")
	fs.DurationVar(&o.Timeout, "timeout", 5*time.Minute, "")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, _ = io.WriteString(stdout, checkUsage)
			return exitOK
		}
		// The flag package quotes the offending value; never echo a value
		// given to --api-key-env (it may be a pasted key).
		msg := err.Error()
		if strings.Contains(msg, "api-key-env") {
			msg = "--api-key-env needs the NAME of an environment variable"
		}
		_, _ = fmt.Fprintf(stderr, "halotrcer check: %s\n\n%s", msg, checkUsage)
		return exitUsage
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "halotrcer check: unexpected argument %q\n\n%s", fs.Arg(0), checkUsage)
		return exitUsage
	}
	o.MCPConfigs = configs
	var err error
	if o.Formats, err = report.ParseFormats(formats); err != nil {
		_, _ = fmt.Fprintf(stderr, "halotrcer check: --format: %v\n", err)
		return exitUsage
	}
	if monthly != "" {
		n, err := strconv.ParseInt(strings.ReplaceAll(monthly, "_", ""), 10, 64)
		if err != nil || n <= 0 {
			_, _ = fmt.Fprintln(stderr, "halotrcer check: --monthly-requests takes a positive whole number")
			return exitUsage
		}
		o.MonthlyRequests = n
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return check.Run(ctx, o, check.Env{
		Stdout: stdout, Stderr: stderr, Environ: os.Environ(),
		Generator:   "halotrcer " + buildVersion(),
		Interactive: isTerminal(os.Stdin) && isTerminal(os.Stderr),
		Confirm:     askYesNo(stderr),
	})
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func askYesNo(w io.Writer) func(string) bool {
	return func(q string) bool {
		_, _ = io.WriteString(w, q)
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		a := strings.ToLower(strings.TrimSpace(line))
		return a == "y" || a == "yes"
	}
}
