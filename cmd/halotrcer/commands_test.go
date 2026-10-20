package main

import (
	"bytes"
	"strings"
	"testing"
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
