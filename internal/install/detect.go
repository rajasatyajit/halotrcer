package install

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
)

// System is what install needs from the machine, injectable for tests.
type System struct {
	OS, Arch string
	Home     string
	Getenv   func(string) string
	LookPath func(string) (string, error)
	// Run runs a command quietly and returns its exit code and output.
	Run func(ctx context.Context, name string, args ...string) (int, string)
	// EvalSymlinks resolves a path (filepath.EvalSymlinks by default).
	EvalSymlinks func(string) (string, error)
}

func (s System) has(name string) bool {
	_, err := s.LookPath(name)
	return err == nil
}

// Owner reports which channel installed the binary at path, the way
// `tokvera doctor` reports its install kind: brew (a Caskroom path), scoop
// and winget (their directories), the system package managers (they own
// the file), go install (GOBIN or GOPATH/bin), else a plain file.
func (s System) Owner(ctx context.Context, tool Tool, path string) string {
	resolved := path
	if s.EvalSymlinks != nil {
		if r, err := s.EvalSymlinks(path); err == nil {
			resolved = r
		}
	}
	norm := filepath.ToSlash(resolved)
	lower := strings.ToLower(norm)
	switch {
	case strings.Contains(norm, "/Caskroom/"):
		return ChannelBrew
	case strings.Contains(lower, "/scoop/"):
		return ChannelScoop
	case strings.Contains(lower, "/microsoft/winget/"):
		return ChannelWinget
	}
	if s.OS == "linux" {
		for _, pm := range []struct {
			ch, bin string
			args    []string
		}{
			{ChannelApt, "dpkg", []string{"-S", resolved}},
			{ChannelDnf, "rpm", []string{"-qf", resolved}},
			{ChannelApk, "apk", []string{"info", "--who-owns", resolved}},
		} {
			if s.has(pm.bin) {
				if code, _ := s.Run(ctx, pm.bin, pm.args...); code == 0 {
					return pm.ch
				}
			}
		}
	}
	for _, d := range s.goBinDirs() {
		if d != "" && filepath.Dir(resolved) == filepath.Clean(d) {
			return ChannelGo
		}
	}
	return ChannelDirect
}

func (s System) goBinDirs() []string {
	dirs := []string{s.Getenv("GOBIN")}
	if gp := s.Getenv("GOPATH"); gp != "" {
		for _, p := range filepath.SplitList(gp) {
			dirs = append(dirs, filepath.Join(p, "bin"))
		}
	} else if s.Home != "" {
		dirs = append(dirs, filepath.Join(s.Home, "go", "bin"))
	}
	return dirs
}

// DefaultChannel is the channel a fresh install uses when --via is not
// given: brew on macOS when present; scoop, then winget on Windows when
// present; otherwise the direct verified download, which needs no root.
// apt, dnf and apk are never chosen by default: they need root (D5).
func (s System) DefaultChannel() string {
	switch s.OS {
	case "darwin":
		if s.has("brew") {
			return ChannelBrew
		}
	case "windows":
		if s.has("scoop") {
			return ChannelScoop
		}
		if s.has("winget") {
			return ChannelWinget
		}
	}
	return ChannelDirect
}

// LinuxPackageManager names the system package manager present (for the
// printed alternative), or "".
func (s System) LinuxPackageManager() string {
	if s.OS != "linux" {
		return ""
	}
	for _, pm := range []struct{ ch, bin string }{{ChannelApt, "apt"}, {ChannelDnf, "dnf"}, {ChannelApk, "apk"}} {
		if s.has(pm.bin) {
			return pm.ch
		}
	}
	return ""
}

// Platform is the release to download. An x86_64 process on Apple Silicon
// (Rosetta 2, sysctl.proc_translated = 1) gets the arm64 build, as the
// tools' installers do.
func (s System) Platform(ctx context.Context) Platform {
	p := Platform{OS: s.OS, Arch: s.Arch}
	if s.OS == "darwin" && s.Arch == "amd64" && s.has("sysctl") {
		if code, out := s.Run(ctx, "sysctl", "-n", "sysctl.proc_translated"); code == 0 && strings.TrimSpace(out) == "1" {
			p.Arch = "arm64"
		}
	}
	return p
}

// DefaultDir is where a direct install goes: ~/.local/bin, or
// %LOCALAPPDATA%\Programs\<tool> on Windows (the tools' installers'
// defaults).
func (s System) DefaultDir(t Tool) string {
	if s.OS == "windows" {
		base := s.Getenv("LOCALAPPDATA")
		if base == "" {
			base = filepath.Join(s.Home, "AppData", "Local")
		}
		return filepath.Join(base, "Programs", t.Name)
	}
	return filepath.Join(s.Home, ".local", "bin")
}

// ExeName adds .exe on Windows.
func (s System) ExeName(t Tool) string {
	if s.OS == "windows" {
		return t.Name + ".exe"
	}
	return t.Name
}

// ApkArch is the architecture in apk file names (nfpm's conventional
// names: x86_64, aarch64).
func ApkArch(goarch string) string {
	if goarch == "arm64" {
		return "aarch64"
	}
	return "x86_64"
}

// Host returns the real machine.
func Host(getenv func(string) string, home string, lookPath func(string) (string, error), run func(ctx context.Context, name string, args ...string) (int, string)) System {
	return System{OS: runtime.GOOS, Arch: runtime.GOARCH, Home: home, Getenv: getenv, LookPath: lookPath, Run: run, EvalSymlinks: filepath.EvalSymlinks}
}
