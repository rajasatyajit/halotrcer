// Command sievant is a STAND-IN for Sievant in Halotrcer's smoke matrix
// (PLAN.md M4, K1), used only until Sievant's first release is pinned in
// packaging/bundled-tools.env. It is not Sievant and contains none of its
// code: it answers the calls halotrcer makes and replays Sievant's real
// outputs, captured from a build of Sievant d8a8bcf and committed in
// testdata/fixtures/sievant-d8a8bcf/ (TestStandinsReplayTheFixtures keeps
// these copies byte for byte).
//
//	sievant version                         prints on stderr, as Sievant does
//	sievant doctor --no-project --json
//	sievant scan --json ...                 the scan-smoke.json fixture
//	sievant report --format json --out DIR  writes DIR/sievant-report-standin.json
//
// It reads no MCP configuration. Every run says on stderr that it is a
// stand-in.
package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

// version is set at build time (-X main.version); v0.2.0 is inside the
// compatibility matrix.
var version = "v0.2.0"

var (
	//go:embed scan-smoke.json
	scan []byte
	//go:embed doctor.json
	doctor []byte
	//go:embed report.json
	report []byte
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fmt.Fprintln(os.Stderr, "STAND-IN for sievant (Halotrcer smoke matrix): replaying the Sievant d8a8bcf fixtures; not Sievant")
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: sievant version|doctor|scan|report")
		return 2
	}
	switch args[0] {
	case "version":
		// sievant prints its version line on stderr (sievant
		// cmd/sievant/configcmd.go).
		fmt.Fprintf(os.Stderr, "sievant %s\n", version)
		return 0
	case "doctor":
		_, _ = os.Stdout.Write(doctor)
		return 0
	case "scan":
		_, _ = os.Stdout.Write(scan)
		return 0
	case "report":
		out := ""
		for i, a := range args {
			if a == "--out" && i+1 < len(args) {
				out = args[i+1]
			}
		}
		if out == "" {
			fmt.Fprintln(os.Stderr, "sievant report: --out DIR is required by this stand-in")
			return 2
		}
		if err := os.MkdirAll(out, 0o750); err != nil { //nolint:gosec // G703: --out is the directory halotrcer chose for this run
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := os.WriteFile(filepath.Join(out, "sievant-report-standin.json"), report, 0o600); err != nil { //nolint:gosec // G703: --out is the directory halotrcer chose for this run
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(os.Stderr, "sievant: %s is not something this stand-in does\n", args[0])
	return 2
}
