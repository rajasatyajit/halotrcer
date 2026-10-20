// Package scripts holds the tests of the release configuration
// (.goreleaser.yaml, the Dockerfile, packaging/, charts/) and of the shell
// scripts under scripts/release.
package scripts

import (
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/rajasatyajit/halotrcer/internal/compat"
	"github.com/rajasatyajit/halotrcer/internal/install"
)

// root reads a file at the repository root (this package is scripts/).
func root(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type goreleaserConfig struct {
	Version     int    `yaml:"version"`
	ProjectName string `yaml:"project_name"`
	Builds      []struct {
		ID      string   `yaml:"id"`
		Main    string   `yaml:"main"`
		Env     []string `yaml:"env"`
		Goos    []string `yaml:"goos"`
		Goarch  []string `yaml:"goarch"`
		Flags   []string `yaml:"flags"`
		Ldflags []string `yaml:"ldflags"`
	} `yaml:"builds"`
	Archives []struct {
		NameTemplate string `yaml:"name_template"`
		Files        []struct {
			Src string `yaml:"src"`
		} `yaml:"files"`
	} `yaml:"archives"`
	Signs []struct {
		Artifacts string   `yaml:"artifacts"`
		Args      []string `yaml:"args"`
	} `yaml:"signs"`
	Nfpms []struct {
		PackageName  string   `yaml:"package_name"`
		Formats      []string `yaml:"formats"`
		Dependencies []string `yaml:"dependencies"`
		Overrides    map[string]struct {
			Dependencies []string `yaml:"dependencies"`
		} `yaml:"overrides"`
		Contents []struct {
			Src string `yaml:"src"`
			Dst string `yaml:"dst"`
		} `yaml:"contents"`
		Apk struct {
			Signature struct {
				KeyName string `yaml:"key_name"`
			} `yaml:"signature"`
		} `yaml:"apk"`
	} `yaml:"nfpms"`
	Dockers []struct {
		Dockerfile string            `yaml:"dockerfile"`
		BuildArgs  map[string]string `yaml:"build_args"`
		Images     []string          `yaml:"images"`
		Tags       []string          `yaml:"tags"`
		Platforms  []string          `yaml:"platforms"`
	} `yaml:"dockers_v2"`
	Casks []struct {
		Name         string `yaml:"name"`
		SkipUpload   string `yaml:"skip_upload"`
		Dependencies []struct {
			Cask string `yaml:"cask"`
		} `yaml:"dependencies"`
		Caveats    string `yaml:"caveats"`
		Repository struct {
			Owner string `yaml:"owner"`
			Name  string `yaml:"name"`
		} `yaml:"repository"`
	} `yaml:"homebrew_casks"`
	Scoops []struct {
		Name       string   `yaml:"name"`
		SkipUpload string   `yaml:"skip_upload"`
		Depends    []string `yaml:"depends"`
		Repository struct {
			Owner string `yaml:"owner"`
			Name  string `yaml:"name"`
		} `yaml:"repository"`
	} `yaml:"scoops"`
	Winget []struct {
		PackageIdentifier string `yaml:"package_identifier"`
		Publisher         string `yaml:"publisher"`
		SkipUpload        string `yaml:"skip_upload"`
		Dependencies      []struct {
			PackageIdentifier string `yaml:"package_identifier"`
			MinimumVersion    string `yaml:"minimum_version"`
		} `yaml:"dependencies"`
	} `yaml:"winget"`
	Release struct {
		Disable    string `yaml:"disable"`
		Draft      bool   `yaml:"draft"`
		Prerelease string `yaml:"prerelease"`
	} `yaml:"release"`
}

func loadGoreleaser(t *testing.T) (goreleaserConfig, string) {
	t.Helper()
	raw := root(t, ".goreleaser.yaml")
	var c goreleaserConfig
	if err := yaml.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	return c, string(raw)
}

// TestGoReleaserConfig pins the channel set, the naming and the bundle's
// shape that the install paths, the docs and both tools' conventions rely on.
func TestGoReleaserConfig(t *testing.T) {
	c, raw := loadGoreleaser(t)
	if c.Version != 2 || c.ProjectName != "halotrcer" {
		t.Fatalf("version %d project %q", c.Version, c.ProjectName)
	}
	if len(c.Builds) != 1 {
		t.Fatalf("want one build, got %d", len(c.Builds))
	}
	b := c.Builds[0]
	if b.Main != "./cmd/halotrcer" || !slices.Contains(b.Env, "CGO_ENABLED=0") || !slices.Contains(b.Flags, "-trimpath") {
		t.Errorf("build: main %q env %v flags %v; want ./cmd/halotrcer, CGO_ENABLED=0, -trimpath", b.Main, b.Env, b.Flags)
	}
	if !slices.Equal(b.Goos, []string{"linux", "darwin", "windows"}) || !slices.Equal(b.Goarch, []string{"amd64", "arm64"}) {
		t.Errorf("platforms %v × %v", b.Goos, b.Goarch)
	}
	// Every -X variable must exist in cmd/halotrcer (a renamed variable
	// would silently ship "dev").
	src := string(root(t, "cmd/halotrcer/buildinfo.go"))
	for _, m := range regexp.MustCompile(`-X main\.(\w+)=`).FindAllStringSubmatch(strings.Join(b.Ldflags, " "), -1) {
		if !regexp.MustCompile(`(?m)^\s+` + m[1] + `\s+=`).MatchString(src) {
			t.Errorf("ldflags set main.%s, which cmd/halotrcer/buildinfo.go does not declare", m[1])
		}
	}
	// The archive name internal/install downloads.
	if len(c.Archives) != 1 || c.Archives[0].NameTemplate != "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}" {
		t.Errorf("archives: %+v", c.Archives)
	}
	for _, f := range []string{"LICENSE", "NOTICE"} {
		if !slices.ContainsFunc(c.Archives[0].Files, func(x struct {
			Src string `yaml:"src"`
		}) bool {
			return x.Src == f
		}) {
			t.Errorf("archives do not ship %s", f)
		}
	}
	if len(c.Signs) != 1 || c.Signs[0].Artifacts != "checksum" {
		t.Errorf("signs: %+v; want cosign over the checksum file", c.Signs)
	}
	// The verification command shown in the comment and in the cask caveats
	// uses the identity internal/install verifies with.
	id := install.Halotrcer.CosignIdentity()
	if n := strings.Count(raw, "--certificate-identity-regexp '"+id+"'"); n != 2 {
		t.Errorf("want the identity %s twice (comment and caveats), found %d", id, n)
	}
	if c.Release.Disable != `{{ if eq (index .Env "RELEASE_PHASE") "publish" }}true{{ else }}false{{ end }}` || !c.Release.Draft || c.Release.Prerelease != "auto" {
		t.Errorf("release: %+v; want draft, prerelease auto, disabled in the publish phase", c.Release)
	}
}

// TestBundleDependencies: every package manager channel declares Tokvera and
// Sievant, so one install command installs all three (the brief), in each
// channel's own syntax (PLAN.md §2 M2 records the sources).
func TestBundleDependencies(t *testing.T) {
	c, _ := loadGoreleaser(t)
	if len(c.Casks) != 1 || len(c.Scoops) != 1 || len(c.Winget) != 1 || len(c.Nfpms) != 1 {
		t.Fatalf("want one cask, scoop, winget and nfpm entry")
	}
	var casks []string
	for _, d := range c.Casks[0].Dependencies {
		casks = append(casks, d.Cask)
	}
	if !slices.Equal(casks, []string{"rajasatyajit/tap/tokvera", "rajasatyajit/tap/sievant"}) {
		t.Errorf("cask dependencies %v", casks)
	}
	if c.Casks[0].Repository.Owner != "rajasatyajit" || c.Casks[0].Repository.Name != "homebrew-tap" {
		t.Errorf("cask repository %+v", c.Casks[0].Repository)
	}
	if !slices.Equal(c.Scoops[0].Depends, []string{"rajasatyajit/tokvera", "rajasatyajit/sievant"}) {
		t.Errorf("scoop depends %v", c.Scoops[0].Depends)
	}
	if c.Scoops[0].Repository.Owner != "rajasatyajit" || c.Scoops[0].Repository.Name != "scoop-bucket" {
		t.Errorf("scoop repository %+v", c.Scoops[0].Repository)
	}
	w := c.Winget[0]
	if w.PackageIdentifier != install.Halotrcer.WingetID || w.Publisher != "Halotrcer" {
		t.Errorf("winget identity %q publisher %q", w.PackageIdentifier, w.Publisher)
	}
	m := compat.Load()
	want := []struct{ id, min string }{
		{install.Tokvera.WingetID, m.Tools["tokvera"].Min},
		{install.Sievant.WingetID, m.Tools["sievant"].Min},
	}
	if len(w.Dependencies) != len(want) {
		t.Fatalf("winget dependencies %+v", w.Dependencies)
	}
	for i, d := range w.Dependencies {
		if d.PackageIdentifier != want[i].id || d.MinimumVersion != want[i].min {
			t.Errorf("winget dependency %d = %+v, want %s >= %s", i, d, want[i].id, want[i].min)
		}
	}
}

// TestPackageDependenciesMatchMatrix: the deb, rpm and apk constraints are
// the compatibility matrix's range, written in each format's syntax, and no
// format-neutral list could leak one syntax into another.
func TestPackageDependenciesMatchMatrix(t *testing.T) {
	c, _ := loadGoreleaser(t)
	n := c.Nfpms[0]
	if n.PackageName != "halotrcer" || !slices.Equal(n.Formats, []string{"deb", "rpm", "apk"}) {
		t.Errorf("nfpm %q formats %v", n.PackageName, n.Formats)
	}
	if len(n.Dependencies) != 0 {
		t.Errorf("top-level dependencies %v would apply one syntax to every format", n.Dependencies)
	}
	m := compat.Load()
	format := map[string]func(name, op, v string) string{
		"deb": func(name, op, v string) string {
			if op == "<" {
				op = "<<"
			}
			return name + " (" + op + " " + v + ")"
		},
		"rpm": func(name, op, v string) string { return name + " " + op + " " + v },
		"apk": func(name, op, v string) string { return name + op + v },
	}
	for f, render := range format {
		var want []string
		for _, tool := range []string{"tokvera", "sievant"} {
			r := m.Tools[tool]
			want = append(want, render(tool, ">=", r.Min), render(tool, "<", r.Below))
		}
		if got := n.Overrides[f].Dependencies; !slices.Equal(got, want) {
			t.Errorf("%s dependencies %q, want %q", f, got, want)
		}
	}
	if n.Apk.Signature.KeyName != "satyajit.rajaraman@halotrcer.com-halotrcer.rsa.pub" {
		t.Errorf("apk key_name %q", n.Apk.Signature.KeyName)
	}
}

// TestReleaseCandidatesSkipPackageManagers: -rc.N tags never reach Homebrew,
// Scoop or winget (skip_upload: auto), as in both tools; the image's latest
// tag is for stable releases only. The apt/yum gate is in release.yml
// (TestReleaseWorkflow).
func TestReleaseCandidatesSkipPackageManagers(t *testing.T) {
	c, _ := loadGoreleaser(t)
	for name, v := range map[string]string{"cask": c.Casks[0].SkipUpload, "scoop": c.Scoops[0].SkipUpload, "winget": c.Winget[0].SkipUpload} {
		if v != "auto" {
			t.Errorf("%s skip_upload = %q, want auto", name, v)
		}
	}
	if len(c.Dockers) != 1 || !slices.Contains(c.Dockers[0].Tags, "{{ if not .Prerelease }}latest{{ end }}") {
		t.Errorf("image tags %v; want latest for stable releases only", c.Dockers)
	}
}

// TestImageBundlesPinnedTools: the image takes tokvera and sievant from
// their released images by the build arguments release.yml sets from
// packaging/bundled-tools.env (after cosign), with no default, on a
// digest-pinned distroless base, as a non-root user.
func TestImageBundlesPinnedTools(t *testing.T) {
	c, _ := loadGoreleaser(t)
	d := c.Dockers[0]
	if d.Dockerfile != "Dockerfile" || !slices.Equal(d.Images, []string{"ghcr.io/rajasatyajit/halotrcer"}) || !slices.Equal(d.Platforms, []string{"linux/amd64", "linux/arm64"}) {
		t.Errorf("dockers_v2 %+v", d)
	}
	for _, arg := range []string{"TOKVERA_IMAGE", "SIEVANT_IMAGE"} {
		if d.BuildArgs[arg] != `{{ index .Env "`+arg+`" }}` {
			t.Errorf("build_args[%s] = %q", arg, d.BuildArgs[arg])
		}
	}
	df := string(root(t, "Dockerfile"))
	for _, line := range []string{
		"ARG TOKVERA_IMAGE\n", "ARG SIEVANT_IMAGE\n",
		"FROM ${TOKVERA_IMAGE} AS tokvera\n", "FROM ${SIEVANT_IMAGE} AS sievant\n",
		"COPY --from=tokvera /usr/bin/tokvera /usr/bin/tokvera\n",
		"COPY --from=sievant /usr/bin/sievant /usr/bin/sievant\n",
		"COPY --from=tokvera /usr/share/doc/tokvera/ /usr/share/doc/tokvera/\n",
		"COPY --from=sievant /usr/share/doc/sievant/ /usr/share/doc/sievant/\n",
		"USER 65532:65532\n", `ENTRYPOINT ["/usr/bin/halotrcer"]` + "\n",
	} {
		if !strings.Contains(df, line) {
			t.Errorf("Dockerfile lacks %q", line)
		}
	}
	froms := regexp.MustCompile(`(?m)^FROM (\S+)`).FindAllStringSubmatch(df, -1)
	if len(froms) != 3 || !regexp.MustCompile(`^gcr\.io/distroless/static-debian13:nonroot@sha256:[0-9a-f]{64}$`).MatchString(froms[2][1]) {
		t.Errorf("FROM lines %v; want the two tool stages, then distroless static nonroot by digest", froms)
	}
	if regexp.MustCompile(`(?m)^ARG (TOKVERA|SIEVANT)_IMAGE=`).MatchString(df) {
		t.Error("the tool images must have no default: an unpinned build has to fail")
	}
}

// TestBundledToolsInMatrix: once set, the pinned tool versions are inside
// the compatibility matrix this release embeds.
func TestBundledToolsInMatrix(t *testing.T) {
	m := compat.Load()
	vals := map[string]string{}
	for line := range strings.SplitSeq(string(root(t, "packaging/bundled-tools.env")), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok && !strings.HasPrefix(line, "#") {
			vals[k] = v
		}
	}
	for _, tool := range []string{"tokvera", "sievant"} {
		v := vals[strings.ToUpper(tool)+"_VERSION"]
		if v == "" {
			t.Logf("%s is not pinned yet (both tools are unreleased)", tool)
			continue
		}
		if err := m.Check(tool, v); err != nil {
			t.Errorf("packaging/bundled-tools.env pins %s %s: %v", tool, v, err)
		}
	}
}

// TestPublicSigningKeysAreCommitted: packaging/keys holds the two public
// keys and nothing private, and the apk key's fingerprint is the one its
// README publishes.
func TestPublicSigningKeysAreCommitted(t *testing.T) {
	entries, err := os.ReadDir("../packaging/keys")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b := root(t, "packaging/keys/"+e.Name())
		if regexp.MustCompile(`PRIVATE KEY`).Match(b) {
			t.Errorf("packaging/keys/%s holds private key material", e.Name())
		}
	}
	pubPEM := root(t, "packaging/keys/satyajit.rajaraman@halotrcer.com-halotrcer.rsa.pub")
	block, _ := pem.Decode(pubPEM)
	if block == nil || block.Type != "PUBLIC KEY" {
		t.Fatal("the apk key is not a PEM public key")
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	rk, ok := key.(*rsa.PublicKey)
	if !ok || rk.N.BitLen() != 4096 {
		t.Errorf("the apk key is not RSA-4096")
	}
	sum := sha256.Sum256(block.Bytes)
	if readme := string(root(t, "packaging/keys/README.md")); !strings.Contains(readme, hex.EncodeToString(sum[:])) {
		t.Errorf("packaging/keys/README.md does not publish the apk key's DER sha256 %x", sum)
	}
	if !strings.Contains(string(root(t, "packaging/keys/halotrcer-packages.asc")), "BEGIN PGP PUBLIC KEY BLOCK") {
		t.Error("halotrcer-packages.asc is not an armored public key")
	}
}

// TestSharedFilesAreTheTools': the repository seed and the shared signing
// key are byte-identical to Tokvera's and Sievant's copies (D7). The sha256
// sums were read from both repositories on 2026-10-21.
func TestSharedFilesAreTheTools(t *testing.T) {
	for name, want := range map[string]string{
		"packaging/repo-meta.env":               "6e40ce38a8baddc679ac5840717fd0a954c0f21ab189251c611c29be5c04f1bc",
		"packaging/keys/halotrcer-packages.asc": "e2ef7f0fa586614b739a3c2cd54034e33cd22b411fdb35b1e959220da38430ab",
	} {
		sum := sha256.Sum256(root(t, name))
		if got := hex.EncodeToString(sum[:]); got != want {
			t.Errorf("%s sha256 %s, want %s (the tools' copy)", name, got, want)
		}
	}
}
