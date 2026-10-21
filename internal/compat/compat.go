// Package compat holds the compatibility matrix this Halotrcer release was
// built and tested against (PLAN.md D2), the parsers for each tool's version
// line, and the schema gates for their JSON reports (PLAN.md C4).
//
// A tool version outside the matrix is a warning: the run continues. A
// report whose schema major Halotrcer does not know is refused, because its
// fields can no longer be trusted to mean what they meant.
package compat

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

//go:embed matrix.json
var matrixJSON []byte

// Matrix is the embedded compatibility matrix.
type Matrix struct {
	MatrixVersion int             `json:"matrix_version"`
	Tools         map[string]Tool `json:"tools"`
}

// Tool is one tool's supported range: Min <= version < Below, compared on
// major.minor.patch only, so the release candidates of a supported version
// (0.2.0-rc.1) are in range (D2).
type Tool struct {
	Min          string `json:"min"`
	Below        string `json:"below"`
	SchemaPrefix string `json:"schema_prefix,omitempty"`
	// SchemaTested is the schema this release's tests and fixtures were
	// built against. Any other minor of the same major is read the same
	// way, without a warning: fields are only ever added.
	SchemaTested  string `json:"schema_tested,omitempty"`
	ReportVersion int    `json:"report_version,omitempty"`
}

// Load returns the embedded matrix. It panics only if the embedded file is
// broken, which a unit test rules out.
func Load() Matrix {
	var m Matrix
	if err := json.Unmarshal(matrixJSON, &m); err != nil {
		panic("compat: embedded matrix.json: " + err.Error())
	}
	return m
}

// Version is a parsed vMAJOR.MINOR.PATCH[-PRE].
type Version struct {
	Major, Minor, Patch int
	Pre                 string
}

var versionRE = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z.-]+))?$`)

// ParseVersion parses "v0.2.0", "0.2.0" or "v0.2.0-rc.1".
func ParseVersion(s string) (Version, error) {
	m := versionRE.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return Version{}, fmt.Errorf("not a version: %q", s)
	}
	var v Version
	v.Major, _ = strconv.Atoi(m[1])
	v.Minor, _ = strconv.Atoi(m[2])
	v.Patch, _ = strconv.Atoi(m[3])
	v.Pre = m[4]
	return v, nil
}

func (v Version) String() string {
	s := fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Pre != "" {
		s += "-" + v.Pre
	}
	return s
}

// coreCmp compares major.minor.patch only.
func coreCmp(a, b Version) int {
	for _, d := range [][2]int{{a.Major, b.Major}, {a.Minor, b.Minor}, {a.Patch, b.Patch}} {
		if d[0] != d[1] {
			if d[0] < d[1] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// Check reports whether version is in tool's supported range. The error is
// nil when it is; otherwise it is the warning to show.
func (m Matrix) Check(tool, version string) error {
	t, ok := m.Tools[tool]
	if !ok {
		return fmt.Errorf("%s is not in the compatibility matrix", tool)
	}
	v, err := ParseVersion(version)
	if err != nil {
		return fmt.Errorf("%s reported an unrecognized version %q; this release supports >=%s <%s", tool, version, t.Min, t.Below)
	}
	lo, _ := ParseVersion(t.Min)
	hi, _ := ParseVersion(t.Below)
	if coreCmp(v, lo) < 0 || coreCmp(v, hi) >= 0 {
		return fmt.Errorf("%s %s is outside the versions this release supports (>=%s <%s); results may be incomplete (see docs/compatibility.md)", tool, v, t.Min, t.Below)
	}
	return nil
}

// tokvera version prints "tokvera vX.Y.Z (commit …)" as its first line on
// stdout (tokvera scripts/install.sh:259-263). sievant version prints
// "sievant vX.Y.Z" on stderr (sievant cmd/sievant/configcmd.go:55-66).
var (
	tokveraLineRE = regexp.MustCompile(`(?m)^tokvera (\S+)`)
	sievantLineRE = regexp.MustCompile(`(?m)^sievant (\S+)`)
)

// ParseVersionLine extracts the version token from a tool's `version`
// output (stdout and stderr concatenated by the caller). A token that is not
// vX.Y.Z (a source build prints "dev") is returned as is, for Check to
// report as unrecognized.
func ParseVersionLine(tool, out string) (string, error) {
	var re *regexp.Regexp
	switch tool {
	case "tokvera":
		re = tokveraLineRE
	case "sievant":
		re = sievantLineRE
	default:
		return "", fmt.Errorf("unknown tool %q", tool)
	}
	m := re.FindStringSubmatch(out)
	if m == nil {
		return "", fmt.Errorf("%s version: no version line in its output", tool)
	}
	return m[1], nil
}

// CheckTokveraSchema gates a Tokvera report.json on its schema_version
// ("tokvera.scan-report/v1.2.0"; readers check the prefix, tokvera
// pkg/scan/summary.go:24).
func (m Matrix) CheckTokveraSchema(schemaVersion string) error {
	prefix := m.Tools["tokvera"].SchemaPrefix
	if strings.HasPrefix(schemaVersion, prefix) {
		return nil
	}
	if strings.HasPrefix(schemaVersion, "tokvera.scan-report/v") {
		return fmt.Errorf("tokvera report schema %q has a major version this release does not read (it reads %s*); upgrade halotrcer", schemaVersion, prefix)
	}
	return fmt.Errorf("not a tokvera scan report (schema_version %q)", schemaVersion)
}

// CheckSievantReport gates a Sievant scan report on report_version (1; fields
// are only ever added, sievant pkg/scan/report.go:13-15).
func (m Matrix) CheckSievantReport(reportVersion int) error {
	want := m.Tools["sievant"].ReportVersion
	if reportVersion == want {
		return nil
	}
	if reportVersion == 0 {
		return fmt.Errorf("not a sievant scan report (no report_version)")
	}
	return fmt.Errorf("sievant report_version %d is not one this release reads (it reads %d); upgrade halotrcer", reportVersion, want)
}
