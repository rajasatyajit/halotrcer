package install

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/rajasatyajit/halotrcer/internal/compat"
)

// Options for one install.
type Options struct {
	Via           string // a channel, or "" for the default
	Version       string // a tag for direct and go installs, or "" for the latest
	Dir           string // direct installs only
	RequireCosign bool
	NoVerify      bool // skip the signature and SHA-256 checks (direct installs)
	DryRun        bool
}

// Actions an install can end in.
const (
	ActionInstalled = "installed"
	ActionUpgraded  = "upgraded"
	ActionUpToDate  = "up to date"
	ActionManual    = "manual" // root-only channel: commands printed
	ActionDryRun    = "dry run"
)

// Outcome is what an install did.
type Outcome struct {
	Tool      string
	Channel   string
	Action    string
	Path      string
	Version   string
	Signature string
}

// Installer carries everything install talks to.
type Installer struct {
	Sys    System
	DL     *Downloader
	Cosign Verifier // nil: cosign not installed
	Out    io.Writer
	Err    io.Writer
	// Exec runs a package manager in the foreground, its output shown.
	Exec func(ctx context.Context, argv []string) error
	// Find locates an installed tool; an error means not installed.
	Find func(tool string) (string, error)
	// Version runs `<path> version` and returns the vX.Y.Z it prints, or "".
	Version func(ctx context.Context, tool Tool, path string) string
	// StatePath is where the state file lives.
	StatePath string
}

func (in *Installer) say(format string, a ...any) { _, _ = fmt.Fprintf(in.Out, format+"\n", a...) }

// Install installs t or, when present, upgrades it through the channel
// that owns it.
func (in *Installer) Install(ctx context.Context, t Tool, o Options) (Outcome, error) {
	out := Outcome{Tool: t.Name}
	tag, err := NormalizeVersion(o.Version)
	if err != nil {
		return out, err
	}
	if o.Via != "" && !validChannel(o.Via) {
		return out, fmt.Errorf("--via %q: want one of %s", o.Via, strings.Join(Channels, ", "))
	}
	existing, ferr := in.Find(t.Name)
	upgrade := ferr == nil
	ch := o.Via
	if upgrade {
		owner := in.Sys.Owner(ctx, t, existing)
		if owner == ChannelContainer {
			return out, fmt.Errorf("%s at %s is part of the container image: pull a newer image (%s) instead", t.Name, existing, Image)
		}
		if o.Via != "" && o.Via != owner {
			return out, fmt.Errorf("%s is already installed by %s at %s: upgrade it there, or uninstall it first", t.Name, owner, existing)
		}
		ch = owner
	} else if ch == "" {
		ch = in.Sys.DefaultChannel()
	}
	out.Channel = ch

	switch {
	case Manual(ch):
		return in.manual(ctx, t, ch, tag, upgrade, out)
	case ch == ChannelDirect:
		return in.direct(ctx, t, o, tag, existing, upgrade, out)
	default:
		return in.packageManager(ctx, t, ch, tag, o.DryRun, upgrade, out)
	}
}

// latest is the release a direct, go or apk install takes when no --version
// is given. For Tokvera and Sievant it is the newest stable release inside
// the compatibility matrix this halotrcer was built with: GitHub's latest when
// that is in range, otherwise the newest stable release in range, so a tool
// release this halotrcer does not support is never chosen for the user. For
// halotrcer itself it is GitHub's latest.
func (in *Installer) latest(ctx context.Context, t Tool) (string, error) {
	tag, err := in.DL.Latest(ctx, t)
	m := compat.Load()
	rng, bundled := m.Tools[t.Name]
	if !bundled || (err == nil && m.Check(t.Name, tag) == nil) {
		return tag, err
	}
	return in.DL.LatestIn(ctx, t, func(tag string) bool { return m.Check(t.Name, tag) == nil },
		fmt.Sprintf(">=%s <%s", rng.Min, rng.Below))
}

func validChannel(ch string) bool {
	for _, c := range Channels {
		if c == ch {
			return true
		}
	}
	return false
}

func (in *Installer) manual(ctx context.Context, t Tool, ch, tag string, upgrade bool, out Outcome) (Outcome, error) {
	if ch == ChannelApk && tag == "" {
		var err error
		if tag, err = in.latest(ctx, t); err != nil {
			return out, err
		}
	}
	cmds := ManualCommands(ch, t, upgrade, in.aptConfigured(), tag, ApkArch(in.Sys.Arch))
	verb := "install"
	if upgrade {
		verb = "upgrade"
	}
	in.say("%s: %s needs root, and Halotrcer never runs sudo. To %s %s, run:", t.Name, ch, verb, t.Name)
	for _, c := range cmds {
		in.say("  %s", c)
	}
	out.Action = ActionManual
	return out, nil
}

// aptConfigured reports whether an apt source for the shared repository
// already exists (the setup lines are then left out, so apt does not see
// the same source twice).
func (in *Installer) aptConfigured() bool {
	files, _ := filepath.Glob("/etc/apt/sources.list.d/*")
	files = append(files, "/etc/apt/sources.list")
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err == nil && strings.Contains(string(b), PackagesBase+"/apt") {
			return true
		}
	}
	return false
}

func (in *Installer) packageManager(ctx context.Context, t Tool, ch, tag string, dryRun, upgrade bool, out Outcome) (Outcome, error) {
	bin := ch
	if !in.Sys.has(bin) {
		return out, fmt.Errorf("%s is not installed (choose another --via, or leave --via out)", bin)
	}
	// go install @latest would follow the module's newest tag, in the
	// matrix or not; the tag is resolved the same way as a download's.
	if ch == ChannelGo && tag == "" && in.DL != nil {
		var err error
		if tag, err = in.latest(ctx, t); err != nil {
			return out, err
		}
	}
	var argvs [][]string
	if ch == ChannelScoop && !upgrade {
		if code, list := in.Sys.Run(ctx, "scoop", "bucket", "list"); code != 0 || !strings.Contains(list, ScoopBucketName) {
			argvs = append(argvs, []string{"scoop", "bucket", "add", ScoopBucketName, ScoopBucketURL})
		}
	}
	if upgrade {
		argvs = append(argvs, upgradeArgv(ch, t, tag)...)
	} else {
		argvs = append(argvs, installArgv(ch, t, tag)...)
	}
	for _, argv := range argvs {
		if dryRun {
			in.say("would run: %s", strings.Join(argv, " "))
			continue
		}
		in.say("running: %s", strings.Join(argv, " "))
		if err := in.Exec(ctx, argv); err != nil {
			return out, fmt.Errorf("%s: %w", strings.Join(argv, " "), err)
		}
	}
	if dryRun {
		out.Action = ActionDryRun
		return out, nil
	}
	out.Action = ActionInstalled
	if upgrade {
		out.Action = ActionUpgraded
	}
	if p, err := in.Find(t.Name); err == nil {
		out.Path = p
		out.Version = in.Version(ctx, t, p)
	}
	return out, in.record(t, out, !upgrade)
}

func (in *Installer) direct(ctx context.Context, t Tool, o Options, tag, existing string, upgrade bool, out Outcome) (Outcome, error) {
	dir := o.Dir
	if dir == "" {
		dir = in.Sys.DefaultDir(t)
		if upgrade {
			dir = filepath.Dir(existing)
		}
	}
	var err error
	if tag == "" {
		if tag, err = in.latest(ctx, t); err != nil {
			return out, err
		}
	}
	out.Version = tag
	if upgrade && in.Version(ctx, t, existing) == tag {
		out.Action, out.Path = ActionUpToDate, existing
		in.say("%s %s is already installed at %s", t.Name, tag, existing)
		return out, nil
	}
	p := in.Sys.Platform(ctx)
	if o.DryRun {
		in.say("would download %s (%s), verify it, and install %s into %s", ArchiveName(t, tag, p), tag, t.Name, dir)
		out.Action = ActionDryRun
		return out, nil
	}
	work, err := os.MkdirTemp("", "halotrcer-install-*")
	if err != nil {
		return out, err
	}
	defer func() { _ = os.RemoveAll(work) }()
	in.say("%s: downloading %s", t.Name, ArchiveName(t, tag, p))
	v, err := in.DL.Fetch(ctx, t, tag, p, in.Cosign, o.RequireCosign, o.NoVerify, work)
	if err != nil {
		return out, err
	}
	out.Signature = v.Signature
	if o.NoVerify {
		in.say("%s: WARNING: --no-verify: the signature and SHA-256 were NOT checked", t.Name)
	} else {
		in.say("%s: signature %s; SHA-256 matches checksums.txt", t.Name, v.Signature)
	}
	// Check the new binary runs before it replaces anything.
	staged, err := Place(work, in.Sys.ExeName(t), v.Binary)
	if err != nil {
		return out, err
	}
	if got := in.Version(ctx, t, staged); got != tag {
		return out, fmt.Errorf("the downloaded %s reports version %q, not %s; not installed", t.Name, got, tag)
	}
	if out.Path, err = Place(dir, in.Sys.ExeName(t), v.Binary); err != nil {
		return out, err
	}
	out.Action = ActionInstalled
	if upgrade {
		out.Action = ActionUpgraded
	}
	in.say("%s %s %s at %s", t.Name, tag, out.Action, out.Path)
	if !onPath(in.Sys.Getenv("PATH"), dir) {
		in.say("note: %s is not on your PATH; add it, or run %s by its full path", dir, t.Name)
	}
	return out, in.record(t, out, !upgrade)
}

func onPath(pathEnv, dir string) bool {
	for _, p := range filepath.SplitList(pathEnv) {
		if filepath.Clean(p) == filepath.Clean(dir) {
			return true
		}
	}
	return false
}

// record notes a tool Halotrcer installed (fresh=true), or refreshes the
// version of one it had installed before. A tool the user installed some
// other way is never recorded, so uninstall --all never removes it.
func (in *Installer) record(t Tool, out Outcome, fresh bool) error {
	if in.StatePath == "" {
		return nil
	}
	st, err := LoadState(in.StatePath)
	if err != nil {
		return err
	}
	rec, had := st.Tools[t.Name]
	if !fresh && !had {
		return nil
	}
	if fresh || !had {
		rec = Record{Channel: out.Channel, InstalledAt: time.Now().UTC().Truncate(time.Second)}
	}
	rec.Path, rec.Version = out.Path, out.Version
	st.Tools[t.Name] = rec
	return st.Save(in.StatePath)
}

// Uninstall removes t only if the state file says Halotrcer installed it.
// It returns false (and leaves the tool alone) otherwise.
func (in *Installer) Uninstall(ctx context.Context, t Tool, dryRun bool) (bool, error) {
	st, err := LoadState(in.StatePath)
	if err != nil {
		return false, err
	}
	rec, ok := st.Tools[t.Name]
	if !ok {
		if p, err := in.Find(t.Name); err == nil {
			in.say("%s at %s was not installed by Halotrcer: left alone", t.Name, p)
		}
		return false, nil
	}
	switch rec.Channel {
	case ChannelDirect, ChannelGo:
		if rec.Path != "" {
			if dryRun {
				in.say("would remove %s", rec.Path)
			} else if err := os.Remove(rec.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return false, err
			} else {
				in.say("removed %s", rec.Path)
			}
		}
	case ChannelBrew, ChannelScoop, ChannelWinget:
		for _, argv := range uninstallArgv(rec.Channel, t) {
			if dryRun {
				in.say("would run: %s", strings.Join(argv, " "))
				continue
			}
			in.say("running: %s", strings.Join(argv, " "))
			if err := in.Exec(ctx, argv); err != nil {
				return false, fmt.Errorf("%s: %w", strings.Join(argv, " "), err)
			}
		}
	default: // a root-only channel: Halotrcer never installs through one
		in.say("%s: remove it with: %s", t.Name, strings.Join(ManualRemove(rec.Channel, t), "; "))
		return false, nil
	}
	if dryRun {
		return true, nil
	}
	delete(st.Tools, t.Name)
	return true, st.Save(in.StatePath)
}

// ParseVersionOutput finds "<name> vX.Y.Z" in a version command's output.
func ParseVersionOutput(name, out string) string {
	m := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + ` (v[0-9][^\s]*)`).FindStringSubmatch(out)
	if m == nil {
		return ""
	}
	return m[1]
}
