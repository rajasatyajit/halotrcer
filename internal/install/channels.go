// Package install installs, upgrades and removes Tokvera and Sievant (and
// Halotrcer itself): through the channel that already owns a tool, through
// a package manager that needs no root (brew, scoop, winget, go install),
// or by a direct download verified exactly as each tool's own installer
// verifies it (cosign over checksums.txt when cosign is present or
// required, then the archive's SHA-256).
//
// Halotrcer never runs sudo (PLAN.md D5): for apt, dnf and apk it prints the
// exact commands, repository setup included, and stops.
package install

import (
	"fmt"
	"regexp"
	"strings"
)

// Tool describes one installable product.
type Tool struct {
	Name     string // binary and package name
	Repo     string // github.com/rajasatyajit/<Repo>
	WingetID string
}

// Products. Names and identifiers from each tool's .goreleaser.yaml and
// docs/install.md.
var (
	Tokvera   = Tool{Name: "tokvera", Repo: "tokvera", WingetID: "Halotrcer.Tokvera"}
	Sievant   = Tool{Name: "sievant", Repo: "sievant", WingetID: "Halotrcer.Sievant"}
	Halotrcer = Tool{Name: "halotrcer", Repo: "halotrcer", WingetID: "Halotrcer.Halotrcer"}
)

// Lookup returns the tool named name.
func Lookup(name string) (Tool, bool) {
	for _, t := range []Tool{Tokvera, Sievant, Halotrcer} {
		if t.Name == name {
			return t, true
		}
	}
	return Tool{}, false
}

// CosignIssuer is the OIDC issuer of every release signature (GitHub
// Actions), as both tools' installers pin it.
const CosignIssuer = "https://token.actions.githubusercontent.com"

// CosignIdentity is the certificate identity regexp for t's releases: its
// own release workflow at a v* tag (tokvera scripts/install.sh:36-37,
// sievant scripts/install.sh:37-38).
func (t Tool) CosignIdentity() string {
	return `^https://github\.com/rajasatyajit/` + regexp.QuoteMeta(t.Repo) + `/\.github/workflows/release\.yml@refs/tags/v`
}

// GoPackage is the go install path.
func (t Tool) GoPackage() string {
	return "github.com/rajasatyajit/" + t.Repo + "/cmd/" + t.Name
}

// Channels.
const (
	ChannelBrew   = "brew"
	ChannelScoop  = "scoop"
	ChannelWinget = "winget"
	ChannelGo     = "go"
	ChannelApt    = "apt"
	ChannelDnf    = "dnf"
	ChannelApk    = "apk"
	ChannelDirect = "direct"
	// ChannelContainer is not an install channel: the binary is part of the
	// container image (ghcr.io/rajasatyajit/halotrcer), which is updated by
	// pulling a newer image. Owner reports it; --via never accepts it.
	ChannelContainer = "container"
)

// Image is the bundle's container image, with all three binaries.
const Image = "ghcr.io/rajasatyajit/halotrcer"

// Channels lists every channel --via accepts.
var Channels = []string{ChannelBrew, ChannelScoop, ChannelWinget, ChannelGo, ChannelApt, ChannelDnf, ChannelApk, ChannelDirect}

// Manual reports whether a channel needs root, so Halotrcer only prints
// its commands.
func Manual(ch string) bool { return ch == ChannelApt || ch == ChannelDnf || ch == ChannelApk }

// ScoopBucket is the shared bucket.
const (
	ScoopBucketName = "rajasatyajit"
	ScoopBucketURL  = "https://github.com/rajasatyajit/scoop-bucket"
)

// Commands a channel runs (argv) for an action.
func installArgv(ch string, t Tool, version string) [][]string {
	switch ch {
	case ChannelBrew:
		return [][]string{{"brew", "install", "--cask", "rajasatyajit/tap/" + t.Name}}
	case ChannelScoop:
		return [][]string{{"scoop", "install", ScoopBucketName + "/" + t.Name}}
	case ChannelWinget:
		return [][]string{{"winget", "install", "-e", "--id", t.WingetID}}
	case ChannelGo:
		v := "latest"
		if version != "" {
			v = version
		}
		return [][]string{{"go", "install", t.GoPackage() + "@" + v}}
	}
	return nil
}

func upgradeArgv(ch string, t Tool, version string) [][]string {
	switch ch {
	case ChannelBrew:
		return [][]string{{"brew", "upgrade", "--cask", "rajasatyajit/tap/" + t.Name}}
	case ChannelScoop:
		return [][]string{{"scoop", "update", t.Name}}
	case ChannelWinget:
		return [][]string{{"winget", "upgrade", "-e", "--id", t.WingetID}}
	case ChannelGo:
		return installArgv(ch, t, version)
	}
	return nil
}

func uninstallArgv(ch string, t Tool) [][]string {
	switch ch {
	case ChannelBrew:
		return [][]string{{"brew", "uninstall", "--cask", t.Name}}
	case ChannelScoop:
		return [][]string{{"scoop", "uninstall", t.Name}}
	case ChannelWinget:
		return [][]string{{"winget", "uninstall", "-e", "--id", t.WingetID}}
	}
	return nil
}

// Repository locations shared by every Halotrcer product.
const (
	PackagesBase  = "https://rajasatyajit.github.io/packages"
	PackagesKey   = PackagesBase + "/halotrcer-packages.asc"
	AptRepoLine   = "deb [signed-by=/etc/apt/keyrings/halotrcer-packages.asc] " + PackagesBase + "/apt stable main"
	AptKeyringDir = "/etc/apt/keyrings"
)

// ManualCommands are the exact shell commands for a root-only channel,
// from each tool's docs/install.md ("The apt and yum repositories",
// "Alpine (signed apk)"). aptConfigured skips the repository setup when an
// apt source for the shared repository already exists. version and arch are
// needed for apk only (no apk repository exists: the signed package comes
// from the release).
func ManualCommands(ch string, t Tool, upgrade, aptConfigured bool, version, apkArch string) []string {
	switch ch {
	case ChannelApt:
		var c []string
		if !aptConfigured {
			c = append(c,
				"sudo install -d -m 0755 "+AptKeyringDir,
				"sudo curl -fsSLo "+AptKeyringDir+"/halotrcer-packages.asc "+PackagesKey,
				`echo "`+AptRepoLine+`" | sudo tee /etc/apt/sources.list.d/`+t.Name+".list",
			)
		}
		return append(c, "sudo apt update && sudo apt install "+t.Name)
	case ChannelDnf:
		if upgrade {
			return []string{"sudo dnf upgrade " + t.Name}
		}
		return []string{
			"sudo curl -fsSLo /etc/yum.repos.d/" + t.Name + ".repo " + PackagesBase + "/rpm/" + t.Name + ".repo",
			"sudo dnf install " + t.Name,
		}
	case ChannelApk:
		ver := strings.TrimPrefix(version, "v")
		key := "satyajit.rajaraman@halotrcer.com-" + t.Name + ".rsa.pub"
		file := fmt.Sprintf("%s_%s_%s.apk", t.Name, ver, apkArch)
		return []string{
			"wget -O /etc/apk/keys/" + key + " https://raw.githubusercontent.com/rajasatyajit/" + t.Repo + "/main/packaging/keys/" + key,
			"wget https://github.com/rajasatyajit/" + t.Repo + "/releases/download/" + version + "/" + file,
			"apk add ./" + file,
		}
	}
	return nil
}

// ManualRemove are the documented removal commands for root-only channels.
func ManualRemove(ch string, t Tool) []string {
	switch ch {
	case ChannelApt:
		return []string{"sudo apt remove " + t.Name}
	case ChannelDnf:
		return []string{"sudo dnf remove " + t.Name}
	case ChannelApk:
		return []string{"apk del " + t.Name}
	}
	return nil
}
