package main

import "runtime/debug"

// Set by -ldflags in release builds (GoReleaser and make build).
var (
	version = ""
	commit  = ""
)

// buildVersion falls back to the module version Go records for
// `go install …@vX.Y.Z`, so that channel does not print "dev" (Tokvera's
// lesson, recorded in Sievant PLAN.md §6l part 1).
func buildVersion() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

func buildCommit() string {
	if commit != "" {
		return commit
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" && len(s.Value) >= 7 {
				return s.Value[:7]
			}
		}
	}
	return "unknown"
}
