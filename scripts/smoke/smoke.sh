#!/bin/sh
# Smoke test for one install channel (PLAN.md M4), Linux and macOS:
#
#   SMOKE_DIST=dir sh scripts/smoke/smoke.sh CHANNEL
#
# SMOKE_DIST is scripts/smoke/snapshot.sh's output. The script starts
# testdata/mirror on loopback (the release mirror for halotrcer, Tokvera and
# Sievant, and the signed package repositories), installs all three through
# CHANNEL, then checks:
#   - each binary runs and is the snapshot's (halotrcer) or the snapshot's
#     one release of each tool (v0.2.0 for the stand-ins),
#     and `halotrcer doctor` finds both tools inside the compatibility matrix;
#   - `halotrcer check` writes the combined report (HTML, PDF and JSON) with
#     both sections, no external resource, no <script>, and none of the
#     planted secrets (the API key canary and testdata/mcp's canaries), and
#     pdftotext reads the PDF where it is installed;
#   - install to report took at most SMOKE_LIMIT seconds (default 300).
# With the stand-in tools (SMOKE_DIST/TOOLS = stand-in, PLAN.md K1) every
# RESULT line says so. Nothing leaves the machine except what a channel itself
# needs (a distro's package manager installing curl, Homebrew's metadata).
#
# Channels:
#   script         scripts/install.sh --version TAG (halotrcer, then the tools through it)
#   script-latest  scripts/install.sh with no --version (the mirror's latest)
#   deb | rpm      the three packages in one install: halotrcer's dependencies
#                  are satisfied by the tools' packages, never by a download
#   apk            the three apks in one `apk add`: refused without the
#                  signing keys, refused for halotrcer alone (its
#                  dependencies), installed with the keys
#   apt-repo | yum-repo   the signed repository built from the snapshot,
#                  served under /repo/: refused without the key, then
#                  `install halotrcer` pulls tokvera and sievant from it
#   brew           the three casks in a local rajasatyajit/tap, pointed at the
#                  mirror: installing halotrcer installs both dependencies
#   goinstall      go install ./cmd/halotrcer from the checkout, then
#                  `halotrcer install` for the tools
#   image          the release image, built from the snapshot binary and
#                  stand-in tool images, checking from inside the container
#   rosetta        macOS arm64 only: install.sh --dry-run under Rosetta 2
#                  must choose the native arm64 build
set -eu

channel="${1:?usage: smoke.sh CHANNEL}"
dist="${SMOKE_DIST:?set SMOKE_DIST}"
limit="${SMOKE_LIMIT:-300}"
repo="$(cd "$(dirname "$0")/../.." && pwd)"
dist="$(cd "$dist" && pwd)"
tag="$(cat "$dist/TAG")"
label=""
[ "$(cat "$dist/TOOLS" 2>/dev/null || true)" = stand-in ] && label=" (stand-in tools)"

say() { printf 'smoke[%s]: %s\n' "$channel" "$*" >&2; }
# Every channel ends with exactly one RESULT line, appended to $SMOKE_RESULTS
# too: PASS with the count of checks that held, SKIP with a reason, or FAIL
# with the reason (scripts/smoke/check-results.sh requires one per channel).
result() {
	printf 'RESULT %s %s%s\n' "$channel" "$*" "$label" >&2
	if [ -n "${SMOKE_RESULTS:-}" ]; then printf 'RESULT %s %s%s\n' "$channel" "$*" "$label" >>"$SMOKE_RESULTS"; fi
}
fail() {
	say "FAIL: $*"
	result "FAIL $*"
	exit 1
}
checks=0
pass() {
	checks=$((checks + 1))
	printf 'PASS %s: %s\n' "$channel" "$*" >&2
}

case "$(uname -s)" in Linux) os=linux ;; Darwin) os=darwin ;; *) fail "unsupported OS $(uname -s)" ;; esac
case "$(uname -m)" in x86_64 | amd64) arch=amd64 ;; aarch64 | arm64) arch=arm64 ;; *) fail "unsupported CPU $(uname -m)" ;; esac

work="$(mktemp -d)"
cleanup() {
	if [ -n "${mirror_pid:-}" ]; then kill "$mirror_pid" 2>/dev/null || true; fi
	if [ -n "${sim_pid:-}" ]; then kill "$sim_pid" 2>/dev/null || true; fi
	rm -rf "$work" 2>/dev/null || true
}
trap cleanup EXIT

# --- No cosign ---------------------------------------------------------------------
# halotrcer's snapshot is never signed (snapshot.sh drops the cosign blocks:
# keyless signing would publish to Sigstore's log), nor are the stand-ins,
# and install.sh and `halotrcer install` rightly refuse a release whose
# signature is missing when cosign is installed. So every channel runs with
# cosign hidden: each PATH directory holding one is replaced by a directory
# of links to everything else in it. Signatures are verified by snapshot.sh's
# release mode (the tools' real releases), release.yml and the installers'
# tests against a fake signed release.
hidden=0
newpath=""
old_ifs=$IFS
IFS=:
for d in $PATH; do
	if [ -n "$d" ] && [ -e "$d/cosign" ]; then
		hidden=$((hidden + 1))
		shadow="$work/path$hidden"
		mkdir -p "$shadow"
		for f in "$d"/*; do
			[ "${f##*/}" = cosign ] || ln -s "$f" "$shadow/"
		done
		d="$shadow"
	fi
	newpath="${newpath:+$newpath:}$d"
done
IFS=$old_ifs
PATH="$newpath"
export PATH
if command -v cosign >/dev/null 2>&1; then fail "could not hide cosign ($(command -v cosign))"; fi
[ "$hidden" -eq 0 ] || say "cosign hidden from PATH: the snapshot carries no signatures"

start=$(date +%s)

# --- The world: the mirror -----------------------------------------------------
mirror="$dist/mirror-$os-$arch"
[ -x "$mirror" ] || chmod +x "$mirror"
"$mirror" --dist "$dist" --url-file "$work/url" --latest "halotrcer=$tag" >"$work/mirror.log" 2>&1 &
mirror_pid=$!
i=0
while [ ! -s "$work/url" ]; do
	i=$((i + 1))
	[ "$i" -le 100 ] || fail "the mirror did not start: $(cat "$work/mirror.log")"
	sleep 0.1
done
base="$(cat "$work/url")"
export HALOTRCER_INSTALL_DOWNLOAD_BASE="$base/dl"
export HALOTRCER_INSTALL_API_BASE="$base/api"
say "mirror at $base (halotrcer $tag$label)"

rel() { printf '%s/releases/%s\n' "$dist" "$1"; } # rel REPO -> its releases directory
tok_tag="$(ls "$dist/releases/tokvera")" # the one tag the snapshot holds for each tool
siv_tag="$(ls "$dist/releases/sievant")"
one() { # one GLOB: exactly one file
	# shellcheck disable=SC2086 # the glob is expanded on purpose
	set -- $1
	if [ "$#" -ne 1 ] || [ ! -f "$1" ]; then fail "want one file, found: $*"; fi
	printf '%s\n' "$1"
}
as_root() { if [ "$(id -u)" -eq 0 ]; then "$@"; else sudo "$@"; fi; }

# --- Install through the channel ---------------------------------------------------
bin=""
case "$channel" in
script)
	sh "$repo/scripts/install.sh" --version "$tag" --dir "$work/bin"
	bin="$work/bin/halotrcer"
	;;
script-latest)
	sh "$repo/scripts/install.sh" --dir "$work/bin"
	bin="$work/bin/halotrcer"
	;;
deb)
	a="$(dpkg --print-architecture)"
	as_root apt-get install -y "$(one "$(rel halotrcer)/*/halotrcer_*_$a.deb")" \
		"$(one "$(rel tokvera)/*/tokvera_*_$a.deb")" "$(one "$(rel sievant)/*/sievant_*_$a.deb")" >/dev/null ||
		fail "apt-get could not install the three packages together"
	depends="$(dpkg-query -W -f='${Depends}' halotrcer)"
	case "$depends" in *"tokvera (>= 0.2.0)"*"sievant (>= 0.2.0)"*) ;; *) fail "halotrcer's Depends: $depends" ;; esac
	pass "the three debs installed together; halotrcer depends on $depends"
	bin=/usr/bin/halotrcer
	;;
rpm)
	m="$(uname -m)"
	set -- "$(one "$(rel halotrcer)/*/halotrcer-*.$m.rpm")" "$(one "$(rel tokvera)/*/tokvera-*.$m.rpm")" "$(one "$(rel sievant)/*/sievant-*.$m.rpm")"
	# Offline: the tools' packages satisfy halotrcer's dependencies.
	as_root dnf install -y --disablerepo='*' "$@" >/dev/null || fail "dnf could not install the three packages together"
	requires="$(rpm -qR halotrcer | tr '\n' ' ')"
	case "$requires" in *"tokvera >= 0.2.0"*"sievant >= 0.2.0"*) ;; *) fail "halotrcer requires: $requires" ;; esac
	pass "the three rpms installed together; halotrcer requires tokvera and sievant in range"
	bin=/usr/bin/halotrcer
	;;
apk)
	m="$(uname -m)"
	hal="$(one "$(rel halotrcer)/*/halotrcer_*_$m.apk")"
	tok="$(one "$(rel tokvera)/*/tokvera_*_$m.apk")"
	siv="$(one "$(rel sievant)/*/sievant_*_$m.apk")"
	# Without the keys, apk must refuse: the install below then proves the
	# signatures were checked, not merely present.
	if as_root apk add --no-network "$hal" "$tok" "$siv" >/dev/null 2>&1; then
		fail "apk: installed without the signing keys"
	fi
	pass "apk refused the packages without their signing keys"
	as_root cp "$dist"/apk-keys/*.rsa.pub /etc/apk/keys/
	# halotrcer alone: its dependencies are in no repository (docs/install.md).
	if as_root apk add --no-network "$hal" >/dev/null 2>&1; then
		fail "apk: installed halotrcer without tokvera and sievant"
	fi
	pass "apk refused halotrcer alone (its dependencies are not available)"
	# No --allow-untrusted: every signature verifies against its key name.
	as_root apk add --no-network "$hal" "$tok" "$siv" >/dev/null || fail "apk: the three apks did not install together"
	pass "apk verified the three signatures and installed them in one apk add"
	bin=/usr/bin/halotrcer
	;;
apt-repo)
	[ -f "$dist/repo/apt/dists/stable/InRelease" ] || fail "the snapshot has no signed apt repository"
	list=/etc/apt/sources.list.d/halotrcer.list
	only="-o Dir::Etc::sourcelist=$list -o Dir::Etc::sourceparts=-" # this repository alone
	as_root mkdir -p /etc/apt/keyrings
	echo "deb [signed-by=/etc/apt/keyrings/halotrcer-absent.asc] $base/repo/apt stable main" | as_root tee "$list" >/dev/null
	# shellcheck disable=SC2086 # only is deliberately split into words
	if as_root apt-get $only update >"$work/apt-nokey.log" 2>&1; then
		fail "apt: the repository was accepted without its signing key"
	fi
	grep -Eqi 'NO_PUBKEY|not signed|signature|public key' "$work/apt-nokey.log" ||
		fail "apt: refused the repository for another reason: $(cat "$work/apt-nokey.log")"
	pass "apt refused the repository without its signing key"
	as_root cp "$dist/repo-key.asc" /etc/apt/keyrings/halotrcer-packages.asc
	echo "deb [signed-by=/etc/apt/keyrings/halotrcer-packages.asc] $base/repo/apt stable main" | as_root tee "$list" >/dev/null
	# shellcheck disable=SC2086
	as_root apt-get $only update >"$work/apt.log" 2>&1 || fail "apt: the signed repository does not verify: $(cat "$work/apt.log")"
	# shellcheck disable=SC2086
	as_root apt-get $only install -y halotrcer >"$work/apt-install.log" 2>&1 || fail "apt: install halotrcer failed: $(tail -n 5 "$work/apt-install.log")"
	dpkg-query -W tokvera sievant >/dev/null 2>&1 || fail "apt: tokvera and sievant did not come with halotrcer"
	pass "apt verified InRelease and installed halotrcer, pulling in tokvera and sievant"
	bin=/usr/bin/halotrcer
	;;
yum-repo)
	[ -f "$dist/repo/rpm/repodata/repomd.xml.asc" ] || fail "the snapshot has no signed yum repository"
	cp "$dist/repo-key.asc" "$work/repo-key.asc"
	keys="file://$work/repo-key.asc"
	# Release mode: the tools' rpms carry their real signatures, by the shared key.
	if [ -f "$dist/packages-key.asc" ]; then
		cp "$dist/packages-key.asc" "$work/packages-key.asc"
		keys="$keys file://$work/packages-key.asc"
	fi
	repofile() { # GPGKEY
		printf '[halotrcer]\nname=Halotrcer\nbaseurl=%s/repo/rpm\nenabled=1\ngpgcheck=1\nrepo_gpgcheck=1\ngpgkey=%s\n' "$base" "$1" | as_root tee /etc/yum.repos.d/halotrcer.repo >/dev/null
	}
	repofile "file://$work/absent.asc"
	if as_root dnf install -y --disablerepo='*' --enablerepo=halotrcer halotrcer >"$work/dnf-nokey.log" 2>&1; then
		fail "dnf: installed without the signing key"
	fi
	grep -Eqi 'gpg|key|signature' "$work/dnf-nokey.log" ||
		fail "dnf: refused the repository for another reason: $(tail -n 5 "$work/dnf-nokey.log")"
	pass "dnf refused the repository without its signing key"
	repofile "$keys"
	as_root dnf install -y --disablerepo='*' --enablerepo=halotrcer halotrcer >"$work/dnf.log" 2>&1 ||
		fail "dnf: install from the signed repository failed: $(tail -n 5 "$work/dnf.log")"
	rpm -q tokvera sievant >/dev/null || fail "dnf: tokvera and sievant did not come with halotrcer"
	pass "dnf verified repomd.xml and the signatures and installed halotrcer, pulling in tokvera and sievant"
	bin=/usr/bin/halotrcer
	;;
brew)
	[ "$os" = darwin ] || fail "brew: macOS only"
	# A local tap under the real tap's name, so the cask's dependencies
	# (rajasatyajit/tap/tokvera, .../sievant) resolve to the casks here.
	brew tap-new --no-git rajasatyajit/tap >/dev/null
	casks="$(brew --repo rajasatyajit/tap)/Casks"
	mkdir -p "$casks"
	for p in halotrcer tokvera sievant; do
		t="$(ls "$(rel "$p")")" # the one tag the snapshot holds for it
		# The same files and sha256s the casks check, from the mirror.
		sed -E "s#https://github.com/rajasatyajit/$p/releases/download/[^/\"]+/#$base/dl/$p/releases/download/$t/#" \
			"$dist/casks/$p.rb" >"$casks/$p.rb"
		grep -q "$base/dl/$p/releases/download/$t/" "$casks/$p.rb" || fail "brew: could not point the $p cask at the mirror"
	done
	HOMEBREW_NO_AUTO_UPDATE=1 HOMEBREW_NO_INSTALL_FROM_API=1 brew install --cask rajasatyajit/tap/halotrcer
	for p in tokvera sievant; do
		brew list --cask "rajasatyajit/tap/$p" >/dev/null 2>&1 || fail "brew: $p did not come with halotrcer"
	done
	pass "the halotrcer cask installed, with the tokvera and sievant casks as its dependencies"
	bin="$(brew --prefix)/bin/halotrcer"
	for p in halotrcer tokvera sievant; do
		f="$(readlink -f "$(brew --prefix)/bin/$p" 2>/dev/null || echo "$(brew --prefix)/bin/$p")"
		if xattr -p com.apple.quarantine "$f" >/dev/null 2>&1; then
			fail "brew: $p still carries com.apple.quarantine"
		fi
	done
	pass "com.apple.quarantine is set on none of the three binaries"
	;;
goinstall)
	# As a user would: no -ldflags, so the version comes from the build info.
	(cd "$repo" && GOBIN="$work/bin" go install ./cmd/halotrcer)
	bin="$work/bin/halotrcer"
	head_commit="$(git -C "$repo" rev-parse --short=7 HEAD)"
	"$bin" install --via direct --dir "$work/bin" >"$work/install.log" 2>&1 || fail "halotrcer install: $(tail -n 5 "$work/install.log")"
	pass "halotrcer install put tokvera and sievant beside it, from their releases"
	;;
image)
	[ "$os" = linux ] || fail "image: Linux only"
	# The tools' images: built from their release archives for this smoke
	# (in a release, packaging/bundled-tools.env pins and verifies them).
	for p in tokvera sievant; do
		ctx="$work/$p-image"
		mkdir -p "$ctx"
		tar -xzf "$(one "$(rel "$p")/*/${p}_*_linux_$arch.tar.gz")" -C "$ctx" "$p" LICENSE
		printf 'FROM scratch\nCOPY %s /usr/bin/%s\nCOPY LICENSE /usr/share/doc/%s/LICENSE\n' "$p" "$p" "$p" >"$ctx/Dockerfile"
		docker build -q --platform "linux/$arch" -t "halotrcer-smoke-$p:$tag" "$ctx" >/dev/null
	done
	# The release image, laid out as GoReleaser's dockers_v2 does.
	ctx="$work/ctx"
	mkdir -p "$ctx/linux/$arch"
	tar -xzf "$(one "$(rel halotrcer)/*/halotrcer_*_linux_$arch.tar.gz")" -C "$ctx/linux/$arch" halotrcer
	cp "$repo/Dockerfile" "$repo/LICENSE" "$repo/NOTICE" "$ctx/"
	docker build -q --platform "linux/$arch" --build-arg "TOKVERA_IMAGE=halotrcer-smoke-tokvera:$tag" \
		--build-arg "SIEVANT_IMAGE=halotrcer-smoke-sievant:$tag" -t "halotrcer-smoke:$tag" "$ctx" >/dev/null
	img="halotrcer-smoke:$tag"
	docker run --rm --entrypoint /bin/sh "$img" -c true 2>/dev/null && fail "image: the image has a shell (it must be distroless)"
	pass "the image has no shell (distroless)"
	[ "$(docker image inspect -f '{{.Config.User}}' "$img")" = "65532:65532" ] || fail "image: the image does not run as 65532:65532"
	pass "the image runs as 65532:65532"
	docker run --rm "$img" doctor >"$work/image-doctor.txt" 2>&1 || true
	grep -q "tokvera: $tok_tag at /usr/bin/tokvera (in the container image)" "$work/image-doctor.txt" ||
		fail "image: doctor does not see tokvera as part of the image: $(cat "$work/image-doctor.txt")"
	if grep -q "halotrcer install" "$work/image-doctor.txt"; then
		fail "image: doctor gives install advice inside the image"
	fi
	pass "doctor inside the image reports the tools as part of it, with no install advice"
	# The check runs as this user, so its report stays removable here.
	mkdir -p "$work/bin"
	printf '#!/bin/sh\nexec docker run --rm --network host --user %s -e SMOKE_CANARY_KEY -v "%s:%s" -v "%s:%s:ro" -w "%s" %s "$@"\n' \
		"$(id -u):$(id -g)" "$work" "$work" "$repo/testdata/mcp" "$repo/testdata/mcp" "$work" "$img" >"$work/bin/halotrcer"
	chmod +x "$work/bin/halotrcer"
	bin="$work/bin/halotrcer"
	;;
rosetta)
	if [ "$os" != darwin ] || [ "$arch" != arm64 ]; then fail "rosetta: macOS arm64 only"; fi
	if ! arch -x86_64 /usr/bin/true 2>/dev/null; then
		result "SKIP Rosetta 2 is not installed on this runner"
		exit 0
	fi
	out="$(arch -x86_64 /bin/sh "$repo/scripts/install.sh" --version "$tag" --dry-run 2>&1)"
	printf '%s\n' "$out" | grep -q "Rosetta 2 detected" || fail "rosetta: no Rosetta message: $out"
	printf '%s\n' "$out" | grep -q "platform:     darwin/arm64" || fail "rosetta: did not choose arm64: $out"
	pass "under Rosetta 2 (x86_64) install.sh reported Rosetta and chose darwin/arm64"
	result "PASS ($checks checks)"
	exit 0
	;;
*) fail "unknown channel $channel" ;;
esac

# --- The three binaries run, at the right versions --------------------------------
[ -x "$bin" ] || fail "no halotrcer at $bin"
PATH="$(dirname "$bin"):$PATH"
export PATH
got="$("$bin" version | head -n 1)"
if [ "$channel" = goinstall ]; then
	case "$got" in *"(commit $head_commit)"*) ;; *) fail "go install printed '$got', want commit $head_commit" ;; esac
else
	case "$got" in "halotrcer $tag "*) ;; *) fail "installed '$got', want halotrcer $tag" ;; esac
fi
pass "installed $got"
if [ "$channel" != image ]; then
	"$bin" doctor >"$work/doctor.txt" 2>&1 || true
	for pv in "tokvera $tok_tag" "sievant $siv_tag"; do
		grep -q "^\[ok\] ${pv% *}: ${pv#* } " "$work/doctor.txt" || fail "doctor: ${pv% *} is not ${pv#* } inside the matrix: $(cat "$work/doctor.txt")"
	done
	pass "doctor: tokvera $tok_tag and sievant $siv_tag, inside the compatibility matrix"
fi

# --- One check, and the combined report --------------------------------------------
# A planted API key: its value must reach no output (the name goes to Tokvera).
SMOKE_CANARY_KEY="sk-smoke-canary-$$-not-a-key"
export SMOKE_CANARY_KEY
out="$work/report"
# The endpoint. The stand-ins replay fixtures and never contact it (nor does
# halotrcer itself); the real Tokvera (release mode) scans Tokvera's own
# simulator, `tokvera sim serve`, run from its release archive for this
# machine, so every channel, the image's included, scans the same server.
target="$base/v1"
if [ "$(cat "$dist/TOOLS")" = release ]; then
	mkdir -p "$work/sim"
	tar -xzf "$(one "$(rel tokvera)/*/tokvera_*_${os}_$arch.tar.gz")" -C "$work/sim" tokvera
	"$work/sim/tokvera" sim serve --addr 127.0.0.1:0 >"$work/sim.log" 2>&1 &
	sim_pid=$!
	i=0
	while ! grep -q '^tokvera sim serve listening on ' "$work/sim.log"; do
		i=$((i + 1))
		[ "$i" -le 100 ] || fail "tokvera sim serve did not start: $(cat "$work/sim.log")"
		sleep 0.1
	done
	target="http://$(sed -n 's/^tokvera sim serve listening on \([^ ]*\).*/\1/p' "$work/sim.log")/v1"
	say "scanning tokvera sim serve at $target"
fi
"$bin" check --target "$target" --yes --no-project --api-key-env SMOKE_CANARY_KEY \
	--mcp-config "$repo/testdata/mcp/smoke-mcp.json" --out "$out" >"$work/check.log" 2>&1 ||
	fail "check failed: $(tail -n 20 "$work/check.log")"
for f in report.html report.pdf report.json; do
	[ -s "$out/$f" ] || fail "check wrote no $f"
done
html="$out/report.html"
grep -q 'Cost &amp; performance' "$html" || fail "the report has no Cost & performance section"
grep -q 'Agent security' "$html" || fail "the report has no Agent security section"
grep -qi '<script' "$html" && fail "the report has a <script>"
grep -Eqi "(src|srcset)=[\"']?https?://|url\([\"']?https?://|<link[^>]*href=[\"']?https?://" "$html" &&
	fail "the report loads an external resource (it must open offline)"
for s in "$SMOKE_CANARY_KEY" smoke-canary-in-url-0000 smoke-canary-env-value-0000; do
	if grep -qF "$s" "$out/report.html" "$out/report.json" "$out/report.pdf" "$work/check.log"; then
		fail "a planted secret reached the report or the output"
	fi
done
pass "check wrote report.html, .pdf and .json: both sections, self-contained, no planted secret"
if command -v pdftotext >/dev/null 2>&1; then
	pdftotext "$out/report.pdf" "$work/report.txt" || fail "pdftotext could not read report.pdf"
	grep -q "Agent security" "$work/report.txt" || fail "the PDF text has no Agent security section"
	pass "pdftotext read the PDF"
fi

elapsed=$(($(date +%s) - start))
[ "$elapsed" -le "$limit" ] || fail "install to report took ${elapsed}s, over the ${limit}s limit"
pass "install to report in ${elapsed}s (limit ${limit}s)"
result "PASS ($checks checks)"
