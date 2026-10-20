package tools

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeTool writes an executable shell script (Unix only).
func fakeTool(t *testing.T, dir, name, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell fakes")
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFind(t *testing.T) {
	dir := t.TempDir()
	p := fakeTool(t, dir, "tokvera", "exit 0")
	t.Setenv("HALOTRCER_TOKVERA_BIN", p)
	if got, err := Find(Tokvera); err != nil || got != p {
		t.Errorf("override: %q %v", got, err)
	}
	t.Setenv("HALOTRCER_TOKVERA_BIN", filepath.Join(dir, "missing"))
	if _, err := Find(Tokvera); err == nil {
		t.Error("missing override accepted")
	}
	t.Setenv("HALOTRCER_TOKVERA_BIN", "")
	t.Setenv("PATH", dir)
	if got, err := Find(Tokvera); err != nil || got != p {
		t.Errorf("PATH: %q %v", got, err)
	}
	empty := t.TempDir()
	t.Setenv("PATH", empty)
	t.Setenv("HOME", empty)
	t.Setenv("GOBIN", "")
	if _, err := Find("halotrcer-test-no-such-tool"); err == nil || !strings.Contains(err.Error(), "halotrcer install") {
		t.Errorf("not found: %v", err)
	}
	local := filepath.Join(empty, ".local", "bin")
	if err := os.MkdirAll(local, 0o750); err != nil {
		t.Fatal(err)
	}
	q := fakeTool(t, local, "sievant", "exit 0")
	if got, err := Find(Sievant); err != nil || got != q {
		t.Errorf("known dir: %q %v", got, err)
	}
}

func TestEnv(t *testing.T) {
	in := []string{"PATH=/bin", "HOME=/h", "TOKVERA_SCAN_SUITE=quick", "SIEVANT_OUTPUT=json", "OPENAI_API_KEY=sk-x", "AWS_SECRET_ACCESS_KEY=y", "MY_KEY=z", "Path=C:\\x", "SystemRoot=C:\\Windows"}
	got := Env(Tokvera, in, "MY_KEY")
	for _, want := range []string{"PATH=/bin", "HOME=/h", "TOKVERA_SCAN_SUITE=quick", "MY_KEY=z", "Path=C:\\x", "SystemRoot=C:\\Windows"} {
		if !slices.Contains(got, want) {
			t.Errorf("tokvera env lacks %s: %v", want, got)
		}
	}
	for _, not := range []string{"SIEVANT_OUTPUT=json", "OPENAI_API_KEY=sk-x", "AWS_SECRET_ACCESS_KEY=y"} {
		if slices.Contains(got, not) {
			t.Errorf("tokvera env has %s", not)
		}
	}
	sv := Env(Sievant, in)
	if !slices.Contains(sv, "SIEVANT_OUTPUT=json") || slices.Contains(sv, "TOKVERA_SCAN_SUITE=quick") || slices.Contains(sv, "MY_KEY=z") {
		t.Errorf("sievant env: %v", sv)
	}
}

func TestRun(t *testing.T) {
	dir := t.TempDir()
	ok := fakeTool(t, dir, "ok", `echo out; echo err >&2; echo "$FOO" "$PWD"`)
	res := Run(context.Background(), ok, nil, []string{"FOO=bar"}, dir)
	if res.Err != nil || res.ExitCode != 0 || !strings.Contains(string(res.Stdout), "out\nbar "+dir) || string(res.Stderr) != "err\n" {
		t.Errorf("%+v %q %q", res, res.Stdout, res.Stderr)
	}
	bad := fakeTool(t, dir, "bad", `echo '{"time":"x","level":"ERROR","msg":"scan failed","error":"connection refused"}' >&2; exit 1`)
	res = Run(context.Background(), bad, nil, nil, "")
	if res.ExitCode != 1 || res.Summary() != "exited 1: scan failed: connection refused" {
		t.Errorf("%+v %q", res, res.Summary())
	}
	plain := fakeTool(t, dir, "plain", `echo 'first' >&2; echo 'Error: no such target' >&2; exit 2`)
	if s := Run(context.Background(), plain, nil, nil, "").Summary(); s != "exited 2: Error: no such target" {
		t.Errorf("%q", s)
	}
	slow := fakeTool(t, dir, "slow", `sleep 5`)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	res = Run(ctx, slow, nil, nil, "")
	if res.Err == nil || !strings.Contains(res.Summary(), "stopped") {
		t.Errorf("deadline: %+v", res)
	}
	big := fakeTool(t, dir, "big", `head -c 40000000 /dev/zero`)
	res = Run(context.Background(), big, nil, nil, "")
	if len(res.Stdout) != MaxOutput || res.Err == nil {
		t.Errorf("bounded: %d %v", len(res.Stdout), res.Err)
	}
}
