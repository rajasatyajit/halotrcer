package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/rajasatyajit/halotrcer/internal/install"
	"github.com/rajasatyajit/halotrcer/internal/redact"
)

func script(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDoctor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fakes")
	}
	dir := t.TempDir()
	tokDoctor, _ := filepath.Abs("../../testdata/fixtures/tokvera-f2a7906/doctor.json")
	svDoctor, _ := filepath.Abs("../../testdata/fixtures/sievant-d8a8bcf/doctor.json")
	tok := script(t, dir, "tokvera", `case "$1" in version) echo "tokvera v0.2.0 (commit a)";; doctor) cat '`+tokDoctor+`';; esac`+"\n")
	sv := script(t, dir, "sievant", `case "$1" in version) echo "sievant v0.1.0-157-gd8a8bcf" >&2;; doctor) cat '`+svDoctor+`';; esac`+"\n")
	home := t.TempDir()
	sys := install.System{OS: "linux", Arch: "amd64", Home: home,
		Getenv:   func(string) string { return "" },
		LookPath: func(string) (string, error) { return "", errors.New("no") },
		Run:      func(context.Context, string, ...string) (int, string) { return 1, "" },
	}
	e := Env{
		Generator: "halotrcer test", Self: "/home/fixtureuser/.local/bin/halotrcer",
		Find: func(n string) (string, error) {
			if n == "tokvera" {
				return tok, nil
			}
			return sv, nil
		},
		Sys: sys, Cwd: t.TempDir(),
		Redactor: redact.New(redact.Options{Home: "/home/fixtureuser", Users: []string{"fixtureuser"}}),
	}
	r := Run(context.Background(), e)
	byName := map[string]Check{}
	for _, c := range r.Checks {
		byName[c.Name] = c
	}
	if byName["tokvera"].Status != OK || byName["sievant"].Status != Warn {
		t.Errorf("tool checks: %+v %+v", byName["tokvera"], byName["sievant"])
	}
	if !strings.Contains(byName["tokvera"].Detail, "installed by direct") {
		t.Errorf("owner missing: %s", byName["tokvera"].Detail)
	}
	if r.Tools["tokvera"] == nil || r.Tools["sievant"] == nil {
		t.Fatal("tools' doctor output not nested")
	}
	if byName["cosign"].Status != Info || byName["state"].Status != OK || byName["report directory"].Status != OK {
		t.Errorf("%+v", r.Checks)
	}
	var text, js bytes.Buffer
	r.WriteText(&text)
	if err := r.WriteJSON(&js); err != nil {
		t.Fatal(err)
	}
	var back Report
	if err := json.Unmarshal(js.Bytes(), &back); err != nil || back.Warnings != r.Warnings {
		t.Errorf("json: %v", err)
	}
	if strings.Contains(text.String()+js.String(), "fixtureuser") {
		t.Error("user name in doctor output")
	}
	if !strings.Contains(text.String(), "    [") {
		t.Errorf("tool checks not indented under their tool:\n%s", text.String())
	}
}

func TestDoctorMissingTool(t *testing.T) {
	sys := install.System{OS: "linux", Home: t.TempDir(), Getenv: func(string) string { return "" },
		LookPath: func(string) (string, error) { return "", errors.New("no") },
		Run:      func(context.Context, string, ...string) (int, string) { return 1, "" }}
	r := Run(context.Background(), Env{Find: func(string) (string, error) { return "", errors.New("not installed") }, Sys: sys, Cwd: t.TempDir()})
	if r.Failures != 2 {
		t.Errorf("failures %d: %+v", r.Failures, r.Checks)
	}
	if r.Checks[1].Fix != "halotrcer install tokvera" {
		t.Errorf("fix %q", r.Checks[1].Fix)
	}
}
