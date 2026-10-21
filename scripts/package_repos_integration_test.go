//go:build integration

// The shared-repository contract (Sievant PLAN.md §6l) and the bundle's
// package dependencies, tested in Docker: `make repos-check` (needs docker,
// gpg, git and nfpm v2.47.0 on PATH; it runs the pinned images several times
// and takes minutes).
package scripts

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The images and package pins of both tools' smoke matrices and
// package-repos-docker.sh.
const (
	ubuntuImage = "ubuntu@sha256:68ee22004a9109b44973313b02ff7b1de250f1de0f5ecb628d73f579aaff33ac"
	fedoraImage = "fedora@sha256:a651ddf48ea28a06ed4e1e6519f51c9f47e7a5a138722ade87369b8fbb7e5b42"
	alpineImage = "alpine@sha256:5291449c3df73caf6ed85e649dec1b9e818b39a5d8c871e97afc13e9cd5e8fa8"
)

type repoEnv struct {
	t     *testing.T
	work  string
	gnupg string
	pkgs  string // fixture packages, one directory per (name, version)
}

func newRepoEnv(t *testing.T) *repoEnv {
	t.Helper()
	for _, tool := range []string{"docker", "gpg", "git", "nfpm"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not on PATH", tool)
		}
	}
	work := t.TempDir()
	e := &repoEnv{t: t, work: work, gnupg: filepath.Join(work, "gnupg"), pkgs: filepath.Join(work, "pkgs")}
	if err := os.Mkdir(e.gnupg, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"r", "other"} {
		e.sh("gpg --batch --quiet --pinentry-mode loopback --passphrase p --quick-gen-key '" + n + " <" + n + "@invalid>' rsa2048 sign 1d 2>/dev/null" +
			" && gpg --batch --pinentry-mode loopback --passphrase p --armor --export-secret-keys " + n + "@invalid > " + n + "-key.asc" +
			" && gpg --batch --armor --export " + n + "@invalid > " + n + "-pub.asc")
	}
	return e
}

func (e *repoEnv) sh(cmdline string, env ...string) string {
	e.t.Helper()
	out, err := e.try(cmdline, env...)
	if err != nil {
		e.t.Fatalf("%s: %v\n%s", cmdline, err, out)
	}
	return out
}

func (e *repoEnv) try(cmdline string, env ...string) (string, error) {
	cmd := exec.CommandContext(e.t.Context(), "sh", "-c", cmdline)
	cmd.Dir = e.work
	cmd.Env = append(append(os.Environ(), "GNUPGHOME="+e.gnupg, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// pkg builds a fixture package with nfpm into a directory of its own and
// returns that directory. deps maps a format to its dependency strings.
func (e *repoEnv) pkg(name, version, arch string, formats []string, deps map[string][]string) string {
	e.t.Helper()
	dir := filepath.Join(e.pkgs, name+"-"+version+"-"+arch)
	bin := filepath.Join(e.work, "bin-"+name+"-"+version)
	writeFile(e.t, bin, "#!/bin/sh\necho '"+name+" v"+version+"'\n", 0o755)
	var b strings.Builder
	b.WriteString("name: " + name + "\narch: " + arch + "\nplatform: linux\nversion: " + version +
		"\nmaintainer: Fixture <fixture@invalid>\ndescription: fixture package\nlicense: Apache-2.0\nmtime: 2026-10-21T00:00:00Z\n" +
		"contents:\n  - src: " + bin + "\n    dst: /usr/bin/" + name + "\n    file_info:\n      mode: 0755\n      mtime: 2026-10-21T00:00:00Z\n")
	if len(deps) > 0 {
		b.WriteString("overrides:\n")
		for _, f := range []string{"deb", "rpm", "apk"} {
			if len(deps[f]) == 0 {
				continue
			}
			b.WriteString("  " + f + ":\n    depends:\n")
			for _, d := range deps[f] {
				b.WriteString("      - \"" + d + "\"\n")
			}
		}
	}
	cfg := filepath.Join(e.work, "nfpm-"+name+"-"+version+"-"+arch+".yaml")
	writeFile(e.t, cfg, b.String(), 0o644)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		e.t.Fatal(err)
	}
	for _, f := range formats {
		e.sh("nfpm package --config " + cfg + " --packager " + f + " --target " + dir + " >/dev/null")
	}
	return dir
}

// halotrcerDeps are the dependency lists .goreleaser.yaml gives nfpm.
func halotrcerDeps(t *testing.T) map[string][]string {
	c, _ := loadGoreleaser(t)
	deps := map[string][]string{}
	for f, o := range c.Nfpms[0].Overrides {
		deps[f] = o.Dependencies
	}
	return deps
}

func (e *repoEnv) publish(site, pkgs string, env ...string) (string, error) {
	return e.try("sh "+abs(e.t, "release/package-repos-docker.sh")+" "+site+" "+pkgs,
		append([]string{"REPO_GPG_KEY_FILE=" + filepath.Join(e.work, "r-key.asc"), "REPO_GPG_PUB=" + filepath.Join(e.work, "r-pub.asc"), "REPO_GPG_PASSPHRASE=p"}, env...)...)
}

func abs(t *testing.T, p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// tree maps every file under dir to its sha256.
func tree(t *testing.T, dir string) map[string]string {
	t.Helper()
	m := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		rel, _ := filepath.Rel(dir, p)
		m[rel] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	if out, err := exec.CommandContext(t.Context(), "cp", "-a", src+"/.", dst).CombinedOutput(); err != nil {
		t.Fatalf("cp: %v\n%s", err, out)
	}
}

func readSite(t *testing.T, site, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(site, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestSharedRepositories starts from a site as Tokvera and Sievant leave it
// (their packages in their pools, a repo-meta.env they wrote with a key this
// script does not know, their .repo files) and checks that Halotrcer's
// publishes keep every one of their files byte for byte, index every
// product and architecture in the pool, are idempotent, refuse what the
// contract refuses with the site untouched and nothing executed, and seed an
// empty site with the shared seed.
func TestSharedRepositories(t *testing.T) {
	e := newRepoEnv(t)
	tok := e.pkg("tokvera", "0.2.0", "amd64", []string{"deb", "rpm", "apk"}, nil)
	tokArm := e.pkg("tokvera", "0.2.0", "arm64", []string{"deb"}, nil)
	tokRiscv := e.pkg("tokvera", "0.2.0", "riscv64", []string{"deb"}, nil)
	siv := e.pkg("sievant", "0.2.0", "amd64", []string{"deb", "rpm", "apk"}, nil)
	h1 := e.pkg("halotrcer", "0.1.0", "amd64", []string{"deb", "rpm", "apk"}, halotrcerDeps(t))
	h2 := e.pkg("halotrcer", "0.1.1", "amd64", []string{"deb", "rpm"}, halotrcerDeps(t))

	site := filepath.Join(e.work, "site")
	place := func(src, glob, dst string) {
		matches, _ := filepath.Glob(filepath.Join(src, glob))
		for _, m := range matches {
			b, _ := os.ReadFile(m)
			writeFile(t, filepath.Join(site, dst, filepath.Base(m)), string(b), 0o644)
		}
	}
	place(tok, "*.deb", "apt/pool/main/t/tokvera")
	place(tokArm, "*.deb", "apt/pool/main/t/tokvera")
	place(tokRiscv, "*.deb", "apt/pool/main/t/tokvera")
	place(tok, "*.rpm", "rpm")
	place(siv, "*.deb", "apt/pool/main/s/sievant")
	place(siv, "*.rpm", "rpm")
	toolsMeta := "# written by another product\nREPO_ORIGIN=Halotrcer\nREPO_LABEL=Halotrcer\nREPO_DESCRIPTION=Halotrcer packages (Tokvera, Sievant), signed by Halotrcer Packages\nREPO_ARCHITECTURES=amd64\nREPO_FUTURE_KEY=kept as is\n"
	writeFile(t, filepath.Join(site, "repo-meta.env"), toolsMeta, 0o644)
	writeFile(t, filepath.Join(site, "rpm", "tokvera.repo"), "[tokvera]\nname=tokvera (Halotrcer)\n", 0o644)
	writeFile(t, filepath.Join(site, "rpm", "sievant.repo"), "[sievant]\nname=Sievant (Halotrcer)\n", 0o644)
	theirs := tree(t, site)

	keptByteForByte := func(stage string) {
		t.Helper()
		now := tree(t, site)
		for f, sum := range theirs {
			if now[f] != sum {
				t.Errorf("%s: %s changed (the other products' file)", stage, f)
			}
		}
	}

	// 1. The first Halotrcer publish on the tools' site.
	if out, err := e.publish(site, h1); err != nil {
		t.Fatalf("first publish: %v\n%s", err, out)
	}
	keptByteForByte("first publish")
	rel := readSite(t, site, "apt/dists/stable/Release")
	for _, line := range []string{"Origin: Halotrcer", "Label: Halotrcer", "Description: Halotrcer packages (Tokvera, Sievant), signed by Halotrcer Packages", "Architectures: amd64 arm64 riscv64"} {
		if !strings.Contains(rel, line+"\n") {
			t.Errorf("Release lacks %q:\n%s", line, rel)
		}
	}
	for arch, want := range map[string][]string{
		"amd64":   {"Package: tokvera", "Package: sievant", "Package: halotrcer"},
		"arm64":   {"Package: tokvera"},
		"riscv64": {"Package: tokvera"},
	} {
		idx := readSite(t, site, "apt/dists/stable/main/binary-"+arch+"/Packages")
		for _, w := range want {
			if !strings.Contains(idx, w+"\n") {
				t.Errorf("binary-%s/Packages lacks %s", arch, w)
			}
		}
	}
	if idx := readSite(t, site, "apt/dists/stable/main/binary-amd64/Packages"); !strings.Contains(idx, "Depends: tokvera (>= 0.2.0), tokvera (<< 0.3.0), sievant (>= 0.2.0), sievant (<< 0.3.0)\n") {
		t.Errorf("halotrcer's Depends is not the matrix range:\n%s", idx)
	}
	if repo := readSite(t, site, "rpm/halotrcer.repo"); !strings.Contains(repo, "[halotrcer]\n") || !strings.Contains(repo, "repo_gpgcheck=1\n") {
		t.Errorf("rpm/halotrcer.repo:\n%s", repo)
	}

	// 2. A second release beside the first.
	if out, err := e.publish(site, h2); err != nil {
		t.Fatalf("second publish: %v\n%s", err, out)
	}
	keptByteForByte("second publish")
	if idx := readSite(t, site, "apt/dists/stable/main/binary-amd64/Packages"); strings.Count(idx, "Package: halotrcer\n") != 2 {
		t.Error("the earlier release left the index")
	}

	// 3. The same release again: nothing changes.
	before := tree(t, site)
	if out, err := e.publish(site, h2); err != nil || !strings.Contains(out, "0 new of") {
		t.Fatalf("re-run: %v\n%s", err, out)
	}
	if after := tree(t, site); !mapsEqual(before, after) {
		t.Error("a re-run changed the site")
	}

	// 4. Refusals, each on a copy: the copy is left exactly as it was and
	// nothing in a value runs.
	canary := "/site/executed"
	for name, c := range map[string]struct {
		meta string
		pkgs string
		env  []string
	}{
		"substitution":  {meta: strings.Replace(toolsMeta, "REPO_LABEL=Halotrcer", "REPO_LABEL=$(touch "+canary+")", 1)},
		"backticks":     {meta: strings.Replace(toolsMeta, "REPO_ORIGIN=Halotrcer", "REPO_ORIGIN=`touch "+canary+"`", 1)},
		"not key=value": {meta: toolsMeta + "touch " + canary + "\n"},
		"missing key":   {meta: strings.Replace(toolsMeta, "REPO_ARCHITECTURES=amd64\n", "", 1)},
		"bad arches":    {meta: strings.Replace(toolsMeta, "REPO_ARCHITECTURES=amd64", "REPO_ARCHITECTURES=amd64;rm", 1)},
		"replaced package": {pkgs: func() string {
			d := filepath.Join(e.work, "replaced")
			m, _ := filepath.Glob(filepath.Join(h2, "*.deb"))
			writeFile(t, filepath.Join(d, filepath.Base(m[0])), "not the published bytes", 0o644)
			return d
		}()},
		"wrong key": {env: []string{"REPO_GPG_KEY_FILE=" + filepath.Join(e.work, "other-key.asc")}},
	} {
		cp := filepath.Join(e.work, "copy-"+strings.ReplaceAll(name, " ", "-"))
		if err := os.Mkdir(cp, 0o755); err != nil {
			t.Fatal(err)
		}
		copyTree(t, site, cp)
		if c.meta != "" {
			writeFile(t, filepath.Join(cp, "repo-meta.env"), c.meta, 0o644)
		}
		was := tree(t, cp)
		pkgs := h2
		if c.pkgs != "" {
			pkgs = c.pkgs
		}
		if out, err := e.publish(cp, pkgs, c.env...); err == nil {
			t.Errorf("%s: accepted:\n%s", name, out)
		}
		if now := tree(t, cp); !mapsEqual(was, now) {
			t.Errorf("%s: the site changed", name)
		}
		if _, err := os.Stat(filepath.Join(cp, "executed")); err == nil {
			t.Errorf("%s: a value was executed", name)
		}
	}

	// 5. A changed description re-signs with no new package.
	changed := strings.Replace(toolsMeta, "signed by Halotrcer Packages", "signed by Halotrcer Packages (renamed)", 1)
	writeFile(t, filepath.Join(site, "repo-meta.env"), changed, 0o644)
	if out, err := e.publish(site, h2); err != nil {
		t.Fatalf("re-describe: %v\n%s", err, out)
	}
	if rel := readSite(t, site, "apt/dists/stable/Release"); !strings.Contains(rel, "(renamed)\n") {
		t.Error("the Release did not follow the changed repo-meta.env")
	}
	if got := readSite(t, site, "repo-meta.env"); got != changed {
		t.Error("repo-meta.env was rewritten")
	}

	// 6. An empty site is seeded with the shared seed, byte for byte.
	empty := filepath.Join(e.work, "empty")
	if out, err := e.publish(empty, h1); err != nil {
		t.Fatalf("empty site: %v\n%s", err, out)
	}
	if got := readSite(t, empty, "repo-meta.env"); got != string(root(t, "packaging/repo-meta.env")) {
		t.Errorf("seeded repo-meta.env:\n%s", got)
	}

	// 7. The bundle's dependencies, with each package manager: installing
	// halotrcer from the repositories pulls in both tools.
	writeFile(t, filepath.Join(site, "repo-meta.env"), toolsMeta, 0o644)
	if out, err := e.publish(site, h2); err != nil {
		t.Fatalf("restore: %v\n%s", err, out)
	}
	pub := filepath.Join(e.work, "r-pub.asc")
	apt := e.sh("docker run --rm -v " + site + ":/site:ro -v " + pub + ":/etc/apt/keyrings/halotrcer-packages.asc:ro " + ubuntuImage + ` sh -c '
		set -eu
		echo "deb [signed-by=/etc/apt/keyrings/halotrcer-packages.asc] file:///site/apt stable main" > /etc/apt/sources.list.d/halotrcer.list
		apt-get update -qq -o Dir::Etc::sourcelist=/etc/apt/sources.list.d/halotrcer.list -o Dir::Etc::sourceparts=- >/dev/null
		DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends halotrcer >/dev/null
		halotrcer; tokvera; sievant'`)
	for _, want := range []string{"halotrcer v0.1.1", "tokvera v0.2.0", "sievant v0.2.0"} {
		if !strings.Contains(apt, want) {
			t.Errorf("apt: %q missing:\n%s", want, apt)
		}
	}
	dnf := e.sh("docker run --rm -v " + site + ":/site:ro -v " + pub + ":/pub.asc:ro " + fedoraImage + ` sh -c '
		set -eu
		printf "[halotrcer]\nname=Halotrcer\nbaseurl=file:///site/rpm\nenabled=1\ngpgcheck=0\nrepo_gpgcheck=1\ngpgkey=file:///pub.asc\n" > /etc/yum.repos.d/halotrcer.repo
		dnf -q -y --disablerepo="*" --enablerepo=halotrcer install halotrcer >/dev/null
		halotrcer; tokvera; sievant'`)
	for _, want := range []string{"halotrcer v0.1.1", "tokvera v0.2.0", "sievant v0.2.0"} {
		if !strings.Contains(dnf, want) {
			t.Errorf("dnf: %q missing:\n%s", want, dnf)
		}
	}
	// apk has no repository (both tools ship apks as release assets): one
	// `apk add` of the three files resolves the dependency, and halotrcer's
	// apk alone is refused for want of the tools.
	apks := filepath.Join(e.work, "apks")
	for _, d := range []string{tok, siv, h1} {
		m, _ := filepath.Glob(filepath.Join(d, "*.apk"))
		for _, f := range m {
			b, _ := os.ReadFile(f)
			writeFile(t, filepath.Join(apks, filepath.Base(f)), string(b), 0o644)
		}
	}
	names, _ := filepath.Glob(filepath.Join(apks, "*.apk"))
	slices.Sort(names)
	var base []string
	for _, n := range names {
		base = append(base, "/apks/"+filepath.Base(n))
	}
	var h string
	for _, b := range base {
		if strings.Contains(b, "halotrcer") {
			h = b
		}
	}
	apk := e.sh("docker run --rm --network none -v " + apks + ":/apks:ro " + alpineImage + ` sh -c '
		if apk add --no-network --allow-untrusted ` + h + ` >/dev/null 2>&1; then echo "ALONE INSTALLED"; fi
		apk add --no-network --allow-untrusted ` + strings.Join(base, " ") + ` >/dev/null && halotrcer && tokvera && sievant'`)
	if strings.Contains(apk, "ALONE INSTALLED") {
		t.Error("apk installed halotrcer without its dependencies")
	}
	for _, want := range []string{"halotrcer v0.1.0", "tokvera v0.2.0", "sievant v0.2.0"} {
		if !strings.Contains(apk, want) {
			t.Errorf("apk: %q missing:\n%s", want, apk)
		}
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// TestPublishReposRetriesARace: a fake git pushes another product's commit
// to gh-pages just before each of this run's pushes. With one race, the
// second attempt (from the new tip) pushes, and the other commit is kept;
// when every push races, it gives up after 3 attempts with nothing of its
// own pushed.
func TestPublishReposRetriesARace(t *testing.T) {
	e := newRepoEnv(t)
	h := e.pkg("halotrcer", "0.1.0", "amd64", []string{"deb", "rpm"}, halotrcerDeps(t))
	realGit, _ := exec.LookPath("git")
	remote := filepath.Join(e.work, "remote.git")
	e.sh("git init -q --bare -b gh-pages " + remote)
	fakeBin := filepath.Join(e.work, "fakebin")
	writeFile(t, filepath.Join(fakeBin, "git"), `#!/bin/sh
case " $* " in
*" push "*)
	n=$(cat "$RACE_COUNT" 2>/dev/null || echo 0)
	if [ "$n" -lt "$RACE_PUSHES" ]; then
		echo $((n + 1)) >"$RACE_COUNT"
		d=$(mktemp -d)
		if "`+realGit+`" ls-remote --exit-code --heads "$RACE_REMOTE" gh-pages >/dev/null; then
			"`+realGit+`" clone -q --branch gh-pages "$RACE_REMOTE" "$d"
		else
			"`+realGit+`" init -q -b gh-pages "$d"
			"`+realGit+`" -C "$d" remote add origin "$RACE_REMOTE"
		fi
		echo "$n" >"$d/other-publisher-$n.txt"
		"`+realGit+`" -C "$d" add -A
		"`+realGit+`" -C "$d" -c user.name=t -c user.email=t@invalid commit -qm "other publisher $n"
		"`+realGit+`" -C "$d" push -q origin gh-pages
		rm -rf "$d"
	fi
	;;
esac
exec "`+realGit+`" "$@"
`, 0o755)
	run := func(pushes string) (string, error) {
		return e.try("sh "+abs(t, "release/publish-repos.sh")+" v0.1.0 "+h,
			"PATH="+fakeBin+":"+os.Getenv("PATH"), "REPO_REMOTE="+remote, "RACE_REMOTE="+remote,
			"RACE_COUNT="+filepath.Join(e.work, "count-"+pushes), "RACE_PUSHES="+pushes,
			"REPO_GPG_KEY_FILE="+filepath.Join(e.work, "r-key.asc"), "REPO_GPG_PUB="+filepath.Join(e.work, "r-pub.asc"), "REPO_GPG_PASSPHRASE=p")
	}
	log := func() string { return e.sh("git --git-dir=" + remote + " log --format=%s gh-pages") }

	out, err := run("1")
	if err != nil || !strings.Contains(out, "attempt 1 of 3: gh-pages moved") || !strings.Contains(out, "(attempt 2 of 3)") {
		t.Fatalf("one race: %v\n%s", err, out)
	}
	if l := log(); !strings.Contains(l, "halotrcer v0.1.0\n") || !strings.Contains(l, "other publisher 0\n") {
		t.Errorf("after one race the branch is:\n%s", l)
	}
	if tip := e.sh("git --git-dir=" + remote + " show gh-pages:repo-meta.env"); tip != string(root(t, "packaging/repo-meta.env")) {
		t.Errorf("seeded repo-meta.env on the branch:\n%s", tip)
	}

	// Every push races: the run gives up and pushes nothing of its own.
	e.sh("git --git-dir=" + remote + " update-ref -d refs/heads/gh-pages")
	out, err = run("3")
	if err == nil || !strings.Contains(out, "gave up after 3 attempts") {
		t.Fatalf("every push races: %v\n%s", err, out)
	}
	if l := log(); strings.Contains(l, "halotrcer") || strings.Count(l, "other publisher") != 3 {
		t.Errorf("after giving up the branch is:\n%s", l)
	}
}
