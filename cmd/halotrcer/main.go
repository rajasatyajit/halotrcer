// Command halotrcer installs Tokvera and Sievant and runs both against one
// AI product, merging their results into one report. It never contains
// either tool's code: it runs their released binaries and reads their
// documented JSON.
package main

import (
	"fmt"
	"io"
	"os"
)

// Exit codes. Fixed once documented (docs/check.md); a new one needs an
// owner decision.
const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

const usage = `halotrcer: one cost and security check for AI products.

Usage:
  halotrcer check --target URL [flags]    run Tokvera and Sievant, write one report
  halotrcer install [tokvera|sievant|all] install or upgrade the tools
  halotrcer doctor [--json]               check this installation
  halotrcer update                        replace halotrcer with a newer release
  halotrcer uninstall [--all]             remove halotrcer (and what it installed)
  halotrcer version                       print the version

Run 'halotrcer <command> -h' for a command's flags.
`

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = io.WriteString(stderr, usage)
		return exitUsage
	}
	switch args[0] {
	case "-h", "--help", "help":
		_, _ = io.WriteString(stdout, usage)
		return exitOK
	case "version", "--version":
		_, _ = fmt.Fprintf(stdout, "halotrcer %s (commit %s)\n", buildVersion(), buildCommit())
		return exitOK
	default:
		_, _ = fmt.Fprintf(stderr, "halotrcer: unknown command %q\n\n%s", args[0], usage)
		return exitUsage
	}
}
