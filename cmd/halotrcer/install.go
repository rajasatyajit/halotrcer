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
	"path/filepath"
	"runtime"
	"strings"

	"github.com/rajasatyajit/halotrcer/internal/doctor"
	"github.com/rajasatyajit/halotrcer/internal/install"
	"github.com/rajasatyajit/halotrcer/internal/redact"
	"github.com/rajasatyajit/halotrcer/internal/tools"
)

const installUsage = `usage: halotrcer install [tokvera|sievant|all] [flags]

Installs the tools, or upgrades them through the channel that owns them.
A fresh install uses brew (macOS) or scoop, then winget (Windows) when
present, else downloads the release and verifies it: the cosign
signature on checksums.txt when cosign is installed, then the archive's
SHA-256. Halotrcer never runs sudo: for apt, dnf and apk it prints the
commands to run.

flags:
  --via CHANNEL      brew, scoop, winget, go, apt, dnf, apk or direct
  --version V        a release tag (direct and go installs)
  --dir DIR          where a direct install goes (default ~/.local/bin)
  --require-cosign   refuse to install without verifying the signature
  --dry-run          show what would happen; change nothing
`

func runInstall(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var o install.Options
	fs.StringVar(&o.Via, "via", "", "")
	fs.StringVar(&o.Version, "version", "", "")
	fs.StringVar(&o.Dir, "dir", "", "")
	fs.BoolVar(&o.RequireCosign, "require-cosign", false, "")
	fs.BoolVar(&o.DryRun, "dry-run", false, "")
	which, rest := "all", args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		which, rest = args[0], args[1:]
	}
	if err := fs.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, _ = io.WriteString(stdout, installUsage)
			return exitOK
		}
		_, _ = fmt.Fprintf(stderr, "halotrcer install: %v\n\n%s", err, installUsage)
		return exitUsage
	}
	var targets []install.Tool
	switch which {
	case "all":
		targets = []install.Tool{install.Tokvera, install.Sievant}
	case "tokvera":
		targets = []install.Tool{install.Tokvera}
	case "sievant":
		targets = []install.Tool{install.Sievant}
	default:
		_, _ = fmt.Fprintf(stderr, "halotrcer install: unknown tool %q (want tokvera, sievant or all)\n", which)
		return exitUsage
	}
	if o.Version != "" && len(targets) > 1 {
		_, _ = fmt.Fprintln(stderr, "halotrcer install: --version names one tool's release; give the tool (halotrcer install tokvera --version v0.2.0)")
		return exitUsage
	}
	in, err := newInstaller(stdout, stderr)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "halotrcer install: %v\n", err)
		return exitUsage
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	code := exitOK
	for _, t := range targets {
		out, err := in.Install(ctx, t, o)
		if err != nil {
			_, _ = fmt.Fprintf(in.Err, "halotrcer install %s: %v\n", t.Name, err)
			code = exitFailure
			continue
		}
		printOutcome(in.Out, out)
	}
	if code == exitOK && !o.DryRun {
		_, _ = fmt.Fprintln(in.Out, "next: halotrcer check --target <inference-url>")
	}
	return code
}

const updateUsage = `usage: halotrcer update [flags]

Replaces this halotrcer with a newer release, through the channel that
installed it (verified as halotrcer install verifies). The tools are
upgraded with halotrcer install.

flags:
  --check            only report whether a newer release exists
  --version V        a release tag instead of the latest
  --require-cosign   refuse without verifying the signature
  --dry-run          show what would happen; change nothing
`

func runUpdate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var o install.Options
	var checkOnly bool
	fs.BoolVar(&checkOnly, "check", false, "")
	fs.StringVar(&o.Version, "version", "", "")
	fs.BoolVar(&o.RequireCosign, "require-cosign", false, "")
	fs.BoolVar(&o.DryRun, "dry-run", false, "")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, _ = io.WriteString(stdout, updateUsage)
			return exitOK
		}
		_, _ = fmt.Fprintf(stderr, "halotrcer update: %v\n\n%s", err, updateUsage)
		return exitUsage
	}
	in, err := newInstaller(stdout, stderr)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "halotrcer update: %v\n", err)
		return exitUsage
	}
	self, err := os.Executable()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "halotrcer update: %v\n", err)
		return exitFailure
	}
	in.Find = func(string) (string, error) { return self, nil }
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if checkOnly {
		latest, err := in.DL.Latest(ctx, install.Halotrcer)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "halotrcer update: %v\n", err)
			return exitFailure
		}
		cur := buildVersion()
		if cur == latest {
			_, _ = fmt.Fprintf(stdout, "halotrcer %s is the latest release\n", cur)
		} else {
			_, _ = fmt.Fprintf(stdout, "halotrcer %s is installed; %s is available (halotrcer update)\n", cur, latest)
		}
		return exitOK
	}
	out, err := in.Install(ctx, install.Halotrcer, o)
	if err != nil {
		_, _ = fmt.Fprintf(in.Err, "halotrcer update: %v\n", err)
		return exitFailure
	}
	printOutcome(in.Out, out)
	return exitOK
}

const uninstallUsage = `usage: halotrcer uninstall [--all] [flags]

Removes halotrcer. With --all, also removes the tools halotrcer itself
installed; a tool you installed some other way is left alone.

flags:
  --all       also remove the tools halotrcer installed
  --yes       do not ask first
  --dry-run   show what would be removed; change nothing
`

func runUninstall(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var all, yes, dryRun bool
	fs.BoolVar(&all, "all", false, "")
	fs.BoolVar(&yes, "yes", false, "")
	fs.BoolVar(&dryRun, "dry-run", false, "")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, _ = io.WriteString(stdout, uninstallUsage)
			return exitOK
		}
		_, _ = fmt.Fprintf(stderr, "halotrcer uninstall: %v\n\n%s", err, uninstallUsage)
		return exitUsage
	}
	in, err := newInstaller(stdout, stderr)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "halotrcer uninstall: %v\n", err)
		return exitUsage
	}
	self, err := os.Executable()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "halotrcer uninstall: %v\n", err)
		return exitFailure
	}
	if !yes && !dryRun {
		if !isTerminal(os.Stdin) {
			_, _ = fmt.Fprintln(stderr, "halotrcer uninstall: no terminal to confirm; pass --yes (or --dry-run)")
			return exitUsage
		}
		q := "Remove halotrcer"
		if all {
			q += " and the tools it installed"
		}
		_, _ = fmt.Fprint(stderr, q+"? [y/N] ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
			_, _ = fmt.Fprintln(stderr, "not removed")
			return exitFailure
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	code := exitOK
	if all {
		for _, t := range []install.Tool{install.Tokvera, install.Sievant} {
			if _, err := in.Uninstall(ctx, t, dryRun); err != nil {
				_, _ = fmt.Fprintf(in.Err, "halotrcer uninstall %s: %v\n", t.Name, err)
				code = exitFailure
			}
		}
	}
	if err := removeSelf(ctx, in, self, dryRun); err != nil {
		_, _ = fmt.Fprintf(in.Err, "halotrcer uninstall: %v\n", err)
		code = exitFailure
	}
	return code
}

// removeSelf removes halotrcer through the channel that installed it.
func removeSelf(ctx context.Context, in *install.Installer, self string, dryRun bool) error {
	owner := in.Sys.Owner(ctx, install.Halotrcer, self)
	switch owner {
	case install.ChannelApt, install.ChannelDnf, install.ChannelApk:
		_, _ = fmt.Fprintf(in.Out, "halotrcer was installed by %s; remove it with: %s\n", owner, strings.Join(install.ManualRemove(owner, install.Halotrcer), "; "))
		return nil
	case install.ChannelBrew, install.ChannelScoop, install.ChannelWinget:
		argv := map[string][]string{
			install.ChannelBrew:   {"brew", "uninstall", "--cask", "halotrcer"},
			install.ChannelScoop:  {"scoop", "uninstall", "halotrcer"},
			install.ChannelWinget: {"winget", "uninstall", "-e", "--id", install.Halotrcer.WingetID},
		}[owner]
		if dryRun {
			_, _ = fmt.Fprintf(in.Out, "would run: %s\n", strings.Join(argv, " "))
			return nil
		}
		return in.Exec(ctx, argv)
	}
	if dryRun {
		_, _ = fmt.Fprintf(in.Out, "would remove %s\n", self)
		return nil
	}
	if runtime.GOOS == "windows" {
		// A running .exe cannot delete itself on Windows.
		_, _ = fmt.Fprintf(in.Out, "remove %s after this command exits:\n  Remove-Item -LiteralPath '%s'\n", self, self)
		return nil
	}
	if err := os.Remove(self); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(in.Out, "removed %s\n", self)
	st, err := install.LoadState(in.StatePath)
	if err == nil && len(st.Tools) == 0 {
		_ = os.Remove(in.StatePath)
		_ = os.Remove(filepath.Dir(in.StatePath))
	}
	return nil
}

func runDoctor(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	asJSON := fs.Bool("json", false, "")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, _ = io.WriteString(stdout, "usage: halotrcer doctor [--json]\n\nChecks this installation and runs both tools' doctor.\nExit codes: 0 no failure; 1 a check failed; 2 usage.\n")
			return exitOK
		}
		_, _ = fmt.Fprintf(stderr, "halotrcer doctor: %v\n", err)
		return exitUsage
	}
	self, _ := os.Executable()
	cwd, _ := os.Getwd()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	r := doctor.Run(ctx, doctor.Env{
		Generator: "halotrcer " + buildVersion(), Self: self, Environ: os.Environ(),
		Find: tools.Find, Sys: hostSystem(), Cwd: cwd,
		Redactor: redact.New(redact.FromEnvironment()),
	})
	if *asJSON {
		if err := r.WriteJSON(stdout); err != nil {
			return exitFailure
		}
	} else {
		r.WriteText(stdout)
	}
	if r.Failures > 0 {
		return exitFailure
	}
	return exitOK
}
