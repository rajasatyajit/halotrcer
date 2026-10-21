// Package doctor checks a Halotrcer installation: its own checks (both
// tools present, their versions against the compatibility matrix, the
// channel that owns each, cosign, the state file, a writable report
// directory) and each tool's own `doctor --json`.
package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/rajasatyajit/halotrcer/internal/compat"
	"github.com/rajasatyajit/halotrcer/internal/ingest"
	"github.com/rajasatyajit/halotrcer/internal/install"
	"github.com/rajasatyajit/halotrcer/internal/redact"
	"github.com/rajasatyajit/halotrcer/internal/tools"
)

// Statuses, the same words both tools use.
const (
	OK   = "ok"
	Info = "info"
	Warn = "warn"
	Fail = "fail"
)

// Check is one result.
type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
}

// Report is the doctor's output.
type Report struct {
	Checks   []Check                   `json:"checks"`
	Tools    map[string]*ingest.Doctor `json:"tools,omitempty"`
	Warnings int                       `json:"warnings"`
	Failures int                       `json:"failures"`
}

// Env is what the doctor talks to.
type Env struct {
	Generator string
	Self      string // path of the running halotrcer
	Environ   []string
	Find      func(string) (string, error)
	Sys       install.System
	Cwd       string
	Redactor  *redact.Redactor
}

func (r *Report) add(name, status, detail, fix string) {
	r.Checks = append(r.Checks, Check{Name: name, Status: status, Detail: detail, Fix: fix})
	switch status {
	case Warn:
		r.Warnings++
	case Fail:
		r.Failures++
	}
}

// Run performs every check.
func Run(ctx context.Context, e Env) *Report {
	r := &Report{Tools: map[string]*ingest.Doctor{}}
	m := compat.Load()
	r.add("halotrcer", Info, e.Generator+" at "+e.Self, "")
	for _, name := range []string{tools.Tokvera, tools.Sievant} {
		t, _ := install.Lookup(name)
		p, err := e.Find(name)
		if err != nil {
			// Inside a container, installing into it is not the fix: the
			// image is (no install advice inside an image, PLAN.md M3).
			fix := "halotrcer install " + name
			if e.Sys.InContainer() {
				fix = "use the halotrcer image, which has all three: " + install.Image
			}
			r.add(name, Fail, "not installed", fix)
			continue
		}
		vctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		res := tools.Run(vctx, p, []string{"version"}, tools.Env(name, e.Environ), "")
		cancel()
		v, verr := compat.ParseVersionLine(name, string(res.Stdout)+"\n"+string(res.Stderr))
		owner := e.Sys.Owner(ctx, t, p)
		how, fix := "installed by "+owner, "halotrcer install "+name
		if owner == install.ChannelContainer {
			how, fix = "in the container image", "pull a newer image: "+install.Image
		}
		switch {
		case verr != nil:
			r.add(name, Fail, "at "+p+": "+verr.Error(), fix)
			continue
		case m.Check(name, v) != nil:
			r.add(name, Warn, fmt.Sprintf("%s at %s (%s): %v", v, p, how, m.Check(name, v)), fix)
		default:
			r.add(name, OK, fmt.Sprintf("%s at %s (%s)", v, p, how), "")
		}
		dctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		args := []string{"doctor", "--json"}
		if name == tools.Sievant {
			args = []string{"doctor", "--no-project", "--json"}
		}
		dres := tools.Run(dctx, p, args, tools.Env(name, e.Environ), "")
		cancel()
		d, derr := ingest.DecodeDoctor(bytes.NewReader(dres.Stdout))
		switch {
		case derr != nil:
			r.add(name+" doctor", Warn, "no doctor output: "+dres.Summary(), "")
		case d.Failures > 0:
			r.add(name+" doctor", Fail, fmt.Sprintf("%d failure(s), %d warning(s); see %s's checks", d.Failures, d.Warnings, name), "")
		case d.Warnings > 0:
			r.add(name+" doctor", Warn, fmt.Sprintf("%d warning(s); see %s's checks", d.Warnings, name), "")
		default:
			r.add(name+" doctor", OK, "no failures", "")
		}
		if d != nil {
			r.Tools[name] = d
		}
	}
	if p, err := e.Sys.LookPath("cosign"); err == nil {
		r.add("cosign", OK, "at "+p+": installs and updates verify release signatures", "")
	} else {
		r.add("cosign", Info, "not installed: installs check SHA-256 only (--require-cosign refuses)", "https://docs.sigstore.dev/cosign/system_config/installation/")
	}
	sp := e.Sys.StatePath()
	if st, err := install.LoadState(sp); err != nil {
		r.add("state", Fail, "cannot read "+sp+": "+err.Error(), "remove the file; halotrcer will not uninstall tools it has no record of")
	} else {
		// A record outlives a tool someone deleted by hand: say so rather
		// than report it as installed.
		var names, missing []string
		for _, n := range slices.Sorted(maps.Keys(st.Tools)) {
			rec := st.Tools[n]
			if where, ok := onDisk(e, n, rec); ok {
				names = append(names, n+" ("+rec.Channel+")")
			} else {
				names = append(names, n+" ("+rec.Channel+", recorded but missing"+where+")")
				missing = append(missing, n)
			}
		}
		detail := "Halotrcer installed: none"
		if len(names) > 0 {
			detail = "Halotrcer installed: " + strings.Join(names, ", ")
		}
		if len(missing) > 0 {
			var fixes []string
			for _, n := range missing {
				fixes = append(fixes, "halotrcer install "+n)
			}
			r.add("state", Warn, detail+" ("+sp+")", strings.Join(fixes, "; "))
		} else {
			r.add("state", OK, detail+" ("+sp+")", "")
		}
	}
	if f, err := os.CreateTemp(e.Cwd, ".halotrcer-doctor-*"); err != nil {
		r.add("report directory", Warn, e.Cwd+" is not writable", "run halotrcer check with --out DIR")
	} else {
		_ = f.Close()
		_ = os.Remove(f.Name())
		r.add("report directory", OK, filepath.Join(e.Cwd, "halotrcer-report")+" can be written", "")
	}
	redactReport(r, e.Redactor)
	return r
}

// onDisk reports whether the tool a state record names is still there: at
// the recorded path, or, for a record without one, wherever Find looks. On
// false, where is " at <path>" when a path was recorded.
func onDisk(e Env, name string, rec install.Record) (where string, ok bool) {
	if rec.Path != "" {
		_, err := os.Stat(rec.Path)
		return " at " + rec.Path, err == nil
	}
	_, err := e.Find(name)
	return "", err == nil
}

func redactReport(r *Report, rd *redact.Redactor) {
	for i := range r.Checks {
		r.Checks[i].Detail = rd.String(r.Checks[i].Detail)
		r.Checks[i].Fix = rd.String(r.Checks[i].Fix)
	}
	for _, d := range r.Tools {
		for i := range d.Checks {
			d.Checks[i].Detail = rd.String(d.Checks[i].Detail)
			d.Checks[i].Fix = rd.String(d.Checks[i].Fix)
		}
	}
}

// WriteText prints the report for a terminal.
func (r *Report) WriteText(w io.Writer) {
	var b strings.Builder
	line := func(indent string, c Check) {
		fmt.Fprintf(&b, "%s[%s] %s: %s\n", indent, c.Status, c.Name, c.Detail)
		if c.Fix != "" && c.Status != OK {
			fmt.Fprintf(&b, "%s       fix: %s\n", indent, c.Fix)
		}
	}
	for _, c := range r.Checks {
		line("", c)
		if d := r.Tools[strings.TrimSuffix(c.Name, " doctor")]; d != nil && strings.HasSuffix(c.Name, " doctor") {
			for _, tc := range d.Checks {
				line("    ", Check{Name: tc.Name, Status: tc.Status, Detail: tc.Detail, Fix: tc.Fix})
			}
		}
	}
	fmt.Fprintf(&b, "%d failure(s), %d warning(s)\n", r.Failures, r.Warnings)
	_, _ = io.WriteString(w, b.String())
}

// WriteJSON prints the report as JSON.
func (r *Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}
