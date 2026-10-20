package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunExitCodes(t *testing.T) {
	cases := []struct {
		args []string
		code int
		out  string
	}{
		{nil, exitUsage, ""},
		{[]string{"help"}, exitOK, "halotrcer check --target URL"},
		{[]string{"version"}, exitOK, "halotrcer "},
		{[]string{"nope"}, exitUsage, ""},
	}
	for _, c := range cases {
		var stdout, stderr bytes.Buffer
		if got := run(c.args, &stdout, &stderr); got != c.code {
			t.Errorf("run(%q) = %d, want %d (stderr %q)", c.args, got, c.code, stderr.String())
		}
		if !strings.Contains(stdout.String(), c.out) {
			t.Errorf("run(%q) stdout = %q, want it to contain %q", c.args, stdout.String(), c.out)
		}
	}
}
