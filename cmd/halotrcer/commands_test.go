package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rajasatyajit/halotrcer/internal/install"
)

func TestCommandUsageErrors(t *testing.T) {
	cases := []struct {
		args []string
		code int
		msg  string
	}{
		{[]string{"check"}, exitUsage, "--target URL is required"},
		{[]string{"check", "--demo", "--target", "https://x.example"}, exitUsage, "exclusive"},
		{[]string{"check", "--target", "https://x.example", "--format", "docx"}, exitUsage, "unknown format"},
		{[]string{"check", "--target", "https://x.example", "--monthly-requests", "lots"}, exitUsage, "positive whole number"},
		{[]string{"check", "--api-key-env"}, exitUsage, "NAME of an environment variable"},
		{[]string{"check", "--target", "https://x.example", "extra"}, exitUsage, "unexpected argument"},
		{[]string{"install", "everything"}, exitUsage, "unknown tool"},
		{[]string{"install", "--version", "v0.2.0"}, exitUsage, "names one tool's release"},
		{[]string{"install", "tokvera", "--bogus"}, exitUsage, "flag provided but not defined"},
		{[]string{"doctor", "--bogus"}, exitUsage, ""},
		{[]string{"update", "--bogus"}, exitUsage, ""},
		{[]string{"uninstall", "--bogus"}, exitUsage, ""},
	}
	for _, c := range cases {
		var out, errb bytes.Buffer
		if got := run(c.args, &out, &errb); got != c.code {
			t.Errorf("%q: exit %d, want %d (%s)", c.args, got, c.code, errb.String())
		}
		if !strings.Contains(errb.String(), c.msg) {
			t.Errorf("%q: stderr %q lacks %q", c.args, errb.String(), c.msg)
		}
	}
}

func TestCommandHelp(t *testing.T) {
	for _, cmd := range []string{"check", "install", "doctor", "update", "uninstall"} {
		var out, errb bytes.Buffer
		if got := run([]string{cmd, "-h"}, &out, &errb); got != exitOK || !strings.Contains(out.String(), "usage: halotrcer "+cmd) {
			t.Errorf("%s -h: exit %d, %q", cmd, got, out.String())
		}
	}
}

func TestNoKeyEchoed(t *testing.T) {
	var out, errb bytes.Buffer
	run([]string{"check", "--target", "https://x.example", "--api-key-env", "sk-live-SECRET123456"}, &out, &errb)
	if strings.Contains(out.String()+errb.String(), "SECRET123456") {
		t.Error("a pasted key was echoed")
	}
}

// TestRemoveSelfInContainer: uninstall inside the image removes nothing and
// says to remove the container or image instead.
func TestRemoveSelfInContainer(t *testing.T) {
	var out bytes.Buffer
	in := &install.Installer{Out: &out, Err: &out,
		Sys: install.System{OS: "linux", Getenv: func(string) string { return "" },
			LookPath: func(string) (string, error) { return "", errors.New("no") },
			Exists:   func(p string) bool { return p == "/.dockerenv" }},
		Exec: func(context.Context, []string) error { t.Error("ran a command"); return nil },
	}
	if err := removeSelf(context.Background(), in, "/usr/bin/halotrcer", false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "part of the container image ("+install.Image+"); remove the container or the image") {
		t.Errorf("output %q", out.String())
	}
}
