// Package tools finds the tokvera and sievant binaries and runs them as
// subprocesses: with a context deadline, a filtered environment, and output
// captured into bounded buffers. It never interprets their output; ingest
// does.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Names of the two tools.
const (
	Tokvera = "tokvera"
	Sievant = "sievant"
)

// MaxOutput bounds what is kept of a tool's stdout and stderr each.
const MaxOutput = 32 << 20

// overrideVar names the variable that points Halotrcer at a specific
// binary (tests, smoke, unusual installs): HALOTRCER_TOKVERA_BIN,
// HALOTRCER_SIEVANT_BIN.
func overrideVar(tool string) string { return "HALOTRCER_" + strings.ToUpper(tool) + "_BIN" }

// exeName adds .exe on Windows.
func exeName(tool string) string {
	if runtime.GOOS == "windows" {
		return tool + ".exe"
	}
	return tool
}

// KnownDirs are where the tools' own installers and the package managers
// put them, searched after PATH (tokvera docs/install.md, sievant
// docs/install.md).
func KnownDirs(tool string) []string {
	home, _ := os.UserHomeDir()
	var dirs []string
	add := func(p ...string) {
		if p[0] != "" {
			dirs = append(dirs, filepath.Join(p...))
		}
	}
	if gobin := os.Getenv("GOBIN"); gobin != "" {
		add(gobin)
	}
	if runtime.GOOS == "windows" {
		add(os.Getenv("LOCALAPPDATA"), "Programs", tool)
		add(os.Getenv("ProgramFiles"), tool)
		add(home, "scoop", "shims")
		add(os.Getenv("LOCALAPPDATA"), "Microsoft", "WinGet", "Links")
		add(home, "go", "bin")
		return dirs
	}
	add(home, ".local", "bin")
	add("/usr/local/bin")
	add("/usr/bin")
	add("/opt/homebrew/bin")
	add("/home/linuxbrew/.linuxbrew/bin")
	add(home, "go", "bin")
	return dirs
}

// ErrNotFound is returned when a tool is not installed.
var ErrNotFound = errors.New("not installed")

// Find returns the path of tool: the override variable if set, else PATH,
// else the known install directories.
func Find(tool string) (string, error) {
	if p := os.Getenv(overrideVar(tool)); p != "" {
		if isExecutable(p) {
			return p, nil
		}
		return "", fmt.Errorf("%s=%s is not an executable file", overrideVar(tool), p)
	}
	if p, err := exec.LookPath(tool); err == nil {
		return p, nil
	}
	for _, d := range KnownDirs(tool) {
		p := filepath.Join(d, exeName(tool))
		if isExecutable(p) {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s: %w (run: halotrcer install %s)", tool, ErrNotFound, tool)
}

func isExecutable(p string) bool {
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() {
		return false
	}
	return runtime.GOOS == "windows" || fi.Mode()&0o111 != 0
}

// Result is a finished run.
type Result struct {
	Stdout, Stderr []byte
	ExitCode       int
	// Err is set when the process could not run or was killed (deadline).
	Err error
}

// passThrough are the environment variables every tool run keeps: what the
// OS, TLS and proxies need, the home and XDG directories (where both tools
// keep their config and data), locale, and color settings.
var passThrough = map[string]bool{
	"PATH": true, "HOME": true, "USER": true, "LOGNAME": true, "SHELL": true, "TERM": true,
	"TMPDIR": true, "TMP": true, "TEMP": true, "LANG": true, "LC_ALL": true, "LC_CTYPE": true,
	"XDG_CONFIG_HOME": true, "XDG_DATA_HOME": true, "XDG_STATE_HOME": true, "XDG_CACHE_HOME": true, "XDG_RUNTIME_DIR": true,
	"NO_COLOR": true, "SSL_CERT_FILE": true, "SSL_CERT_DIR": true,
	"HTTP_PROXY": true, "HTTPS_PROXY": true, "NO_PROXY": true, "http_proxy": true, "https_proxy": true, "no_proxy": true,
	// Windows.
	"SYSTEMROOT": true, "SYSTEMDRIVE": true, "WINDIR": true, "COMSPEC": true, "PATHEXT": true,
	"USERPROFILE": true, "USERNAME": true, "APPDATA": true, "LOCALAPPDATA": true, "PROGRAMDATA": true,
	"PROGRAMFILES": true, "HOMEDRIVE": true, "HOMEPATH": true,
}

// Env builds the environment for tool: passThrough, the tool's own
// documented prefix (TOKVERA_ or SIEVANT_, which each tool reads), and the
// extra variables named (such as the --api-key-env variable).
func Env(tool string, environ []string, extra ...string) []string {
	prefix := strings.ToUpper(tool) + "_"
	keep := map[string]bool{}
	for _, k := range extra {
		keep[k] = true
	}
	var out []string
	for _, kv := range environ {
		k, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		ku := strings.ToUpper(k)
		if passThrough[k] || passThrough[ku] || strings.HasPrefix(ku, prefix) || keep[k] {
			out = append(out, kv)
		}
	}
	return out
}

// boundedBuffer keeps the first max bytes written to it. It does not embed
// bytes.Buffer: io.Copy would use the embedded ReadFrom and skip the bound.
type boundedBuffer struct {
	buf       bytes.Buffer
	max       int
	truncated bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if room := b.max - b.buf.Len(); room < len(p) {
		if room > 0 {
			b.buf.Write(p[:room])
		}
		b.truncated = true
		return len(p), nil
	}
	return b.buf.Write(p)
}

// waitDelay bounds how long a killed tool's children may hold its output
// pipes open.
const waitDelay = 2 * time.Second

// Run runs path with args under ctx, in dir (or the current directory when
// empty), with env.
func Run(ctx context.Context, path string, args []string, env []string, dir string) Result {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = env
	cmd.Dir = dir
	cmd.Stdin = nil
	cmd.WaitDelay = waitDelay
	var stdout, stderr = &boundedBuffer{max: MaxOutput}, &boundedBuffer{max: MaxOutput}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	res := Result{Stdout: stdout.buf.Bytes(), Stderr: stderr.buf.Bytes()}
	var exit *exec.ExitError
	switch {
	case ctx.Err() != nil:
		res.ExitCode = -1
		res.Err = fmt.Errorf("stopped: %w", ctx.Err())
	case errors.As(err, &exit):
		res.ExitCode = exit.ExitCode()
	case err != nil:
		res.ExitCode = -1
		res.Err = err
	}
	if stdout.truncated || stderr.truncated {
		res.Err = errors.Join(res.Err, errors.New("output exceeded the capture limit"))
	}
	return res
}

// Summary is a short, one-line description of a failed run for messages
// and the report (redacted by the caller): the run error, or the exit code
// and the tool's last stderr line. A JSON log line (Tokvera's --json slog)
// is reduced to its msg and error fields.
func (r Result) Summary() string {
	if r.Err != nil {
		return r.Err.Error()
	}
	line := lastLine(r.Stderr)
	var rec map[string]any
	if json.Unmarshal([]byte(line), &rec) == nil {
		var parts []string
		for _, k := range []string{"msg", "error", "err"} {
			if v, ok := rec[k].(string); ok && v != "" {
				parts = append(parts, v)
			}
		}
		line = strings.Join(parts, ": ")
	}
	if len(line) > 300 {
		line = line[:300] + "…"
	}
	if line == "" {
		return fmt.Sprintf("exited %d", r.ExitCode)
	}
	return fmt.Sprintf("exited %d: %s", r.ExitCode, line)
}

func lastLine(b []byte) string {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return ""
}
