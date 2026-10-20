package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/rajasatyajit/halotrcer/internal/install"
	"github.com/rajasatyajit/halotrcer/internal/redact"
	"github.com/rajasatyajit/halotrcer/internal/tools"
)

func hostSystem() install.System {
	home, _ := os.UserHomeDir()
	return install.Host(os.Getenv, home, exec.LookPath, quietRun)
}

// quietRun runs a short probe (dpkg -S, sysctl, scoop bucket list).
func quietRun(ctx context.Context, name string, args ...string) (int, string) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	var ee *exec.ExitError
	switch {
	case errors.As(err, &ee):
		return ee.ExitCode(), string(out)
	case err != nil:
		return -1, ""
	}
	return 0, string(out)
}

// redactingWriter scrubs everything shown in the terminal.
type redactingWriter struct {
	w  io.Writer
	rd *redact.Redactor
}

func (r redactingWriter) Write(p []byte) (int, error) {
	if _, err := io.WriteString(r.w, r.rd.String(string(p))); err != nil {
		return 0, err
	}
	return len(p), nil
}

func newInstaller(stdout, stderr io.Writer) (*install.Installer, error) {
	sys := hostSystem()
	dl, err := install.NewDownloader(os.Getenv)
	if err != nil {
		return nil, err
	}
	rd := redact.New(redact.FromEnvironment())
	in := &install.Installer{
		Sys: sys, DL: dl,
		Out: redactingWriter{stdout, rd}, Err: redactingWriter{stderr, rd},
		Exec: func(ctx context.Context, argv []string) error {
			cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
			cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, stdout, stderr
			return cmd.Run()
		},
		Find:      tools.Find,
		Version:   probeVersion,
		StatePath: sys.StatePath(),
	}
	if p, err := exec.LookPath("cosign"); err == nil {
		in.Cosign = func(ctx context.Context, args []string) error {
			res := tools.Run(ctx, p, args, os.Environ(), "")
			if res.Err != nil || res.ExitCode != 0 {
				return errors.New(res.Summary())
			}
			return nil
		}
	}
	return in, nil
}

// probeVersion runs `<path> version`; Sievant prints it on stderr, Tokvera
// and Halotrcer on stdout, so both are read.
func probeVersion(ctx context.Context, t install.Tool, path string) string {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, path, "version")
	cmd.Stdout, cmd.Stderr = &out, &out
	_ = cmd.Run()
	return install.ParseVersionOutput(t.Name, out.String())
}

func printOutcome(w io.Writer, o install.Outcome) {
	switch o.Action {
	case install.ActionManual, install.ActionDryRun:
		return
	}
	s := fmt.Sprintf("%s: %s via %s", o.Tool, o.Action, o.Channel)
	if o.Version != "" {
		s += " (" + o.Version + ")"
	}
	_, _ = fmt.Fprintln(w, strings.TrimSpace(s))
}
