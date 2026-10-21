// Command tokvera is a STAND-IN for Tokvera in Halotrcer's smoke matrix
// (PLAN.md M4, K1), used only until Tokvera's first release is pinned in
// packaging/bundled-tools.env. It is not Tokvera and contains none of its
// code: it answers the calls halotrcer makes and replays Tokvera's real
// outputs, captured from a build of Tokvera f2a7906 and committed in
// testdata/fixtures/tokvera-f2a7906/ (TestStandinsReplayTheFixtures keeps
// these copies byte for byte).
//
//	tokvera version
//	tokvera doctor --json
//	tokvera --json scan ... --out DIR ...   writes DIR/report.json
//
// It never contacts the target. Every run says on stderr that it is a
// stand-in.
package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// version is set at build time (-X main.version); v0.2.0 is inside the
// compatibility matrix.
var version = "v0.2.0"

var (
	//go:embed flaky.report.json
	report []byte
	//go:embed doctor.json
	doctor []byte
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fmt.Fprintln(os.Stderr, "STAND-IN for tokvera (Halotrcer smoke matrix): replaying the Tokvera f2a7906 fixtures; not Tokvera")
	// The global --json comes before the command (tokvera --json scan).
	args = slices.DeleteFunc(slices.Clone(args), func(a string) bool { return a == "--json" })
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: tokvera version|doctor|scan")
		return 2
	}
	switch args[0] {
	case "version":
		// The first line `tokvera version` prints (tokvera scripts/install.sh).
		fmt.Fprintf(os.Stdout, "tokvera %s (commit standin)\n", version)
		return 0
	case "doctor":
		_, _ = os.Stdout.Write(doctor)
		return 0
	case "scan":
		out := ""
		for i, a := range args {
			if a == "--out" && i+1 < len(args) {
				out = args[i+1]
			}
		}
		if out == "" {
			fmt.Fprintln(os.Stderr, "tokvera scan: --out DIR is required by this stand-in")
			return 2
		}
		if err := os.MkdirAll(out, 0o750); err != nil { //nolint:gosec // G703: --out is the directory halotrcer chose for this run
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := os.WriteFile(filepath.Join(out, "report.json"), report, 0o600); err != nil { //nolint:gosec // G703: --out is the directory halotrcer chose for this run
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(os.Stderr, "tokvera: %s is not something this stand-in does\n", args[0])
	return 2
}
