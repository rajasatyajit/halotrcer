# Installing Halotrcer

Halotrcer is one command, `halotrcer`, that runs two separately released tools and merges what they find:

- [Tokvera](https://github.com/rajasatyajit/tokvera), the LLM inference cost and latency scanner;
- [Sievant](https://github.com/rajasatyajit/sievant), the MCP security gateway.

Every channel below installs all three by default. Each comes from its own GitHub release, verified against its own release workflow's signature. Each release carries `checksums.txt` (the SHA-256 of every archive and package) and a keyless [cosign](https://docs.sigstore.dev/) signature of that file, `checksums.txt.sigstore.json`, made by that repository's release workflow on a version tag.

| Platform | Quickest | Package manager |
| --- | --- | --- |
| Linux | `curl -fsSL https://www.halotrcer.com/install.sh \| sh` | apt, dnf, apk (below) |
| macOS | `curl -fsSL https://www.halotrcer.com/install.sh \| sh` | `brew install --cask rajasatyajit/tap/halotrcer` |
| Windows | `irm https://www.halotrcer.com/install.ps1 \| iex` | `scoop install rajasatyajit/halotrcer`, `winget install -e --id Halotrcer.Halotrcer` |
| Any (Go) | `go install github.com/rajasatyajit/halotrcer/cmd/halotrcer@latest`, then `halotrcer install` | |
| Container | `docker run --rm ghcr.io/rajasatyajit/halotrcer:latest version` | |

Then run one check:

```sh
halotrcer check --target <inference-url>
```

The install scripts are `scripts/install.sh` and `scripts/install.ps1` in this repository; `www.halotrcer.com` serves the same files. To read a script before you run it, download it first:

```sh
curl -fsSLO https://raw.githubusercontent.com/rajasatyajit/halotrcer/main/scripts/install.sh
less install.sh && sh install.sh
```

## Which versions of the tools

Each Halotrcer release embeds a compatibility matrix: the Tokvera and Sievant versions it was built and tested against (docs/compatibility.md). Without a version you name, `halotrcer install`, and the scripts through it, take each tool's latest stable release only when the matrix supports it. Otherwise they take the newest stable release inside the matrix. A tool release this Halotrcer does not support is never chosen for you.

Brew, Scoop and winget install each tool's latest release, because those channels cannot express an upper version bound. apt, dnf and apk packages declare both bounds. `halotrcer doctor` warns when an installed tool is outside the matrix.

## The install scripts

Both scripts follow the same steps, each commented in the source:

1. Detect the platform. On Apple Silicon a shell running under Rosetta 2 still gets the native arm64 build. On Windows on Arm, an emulated x64 PowerShell does too. musl Linux (Alpine) gets the standard Linux build, which is statically linked.
2. Resolve the Halotrcer version: the latest **published** release, unless you name one.
3. Download halotrcer's archive and `checksums.txt`, unless that version is already installed. Running a script again is safe.
4. Verify. When `cosign` is installed, first check that `checksums.txt` was signed by Halotrcer's release workflow on a version tag. Then check the archive's SHA-256 against its line in `checksums.txt`. Any failure stops the install before anything is written.
5. Install halotrcer atomically: it is written beside the destination, then renamed over it.
6. Have that halotrcer install Tokvera and Sievant, through `halotrcer install <tool> --via direct --dir <dir>`.
   - Each tool is downloaded from its own release and verified the same way, against its own release workflow's identity.
   - Each is recorded, so `halotrcer uninstall --all` can remove it later.
   - A tool already installed somewhere else on PATH (Homebrew, Scoop, winget, a package, `go install`) is left as it is. Its own channel updates it.
7. Print a PATH hint when a directory is not on PATH, and the next command. The scripts never edit PATH or shell profiles themselves.

**They never elevate.** `install.sh` never runs `sudo`, and `install.ps1` never asks for administrator rights. A security tool should not escalate privileges on your behalf. To install system-wide, run the script as root, or from a PowerShell you started as administrator.

They contact only GitHub (`api.github.com` for versions, `github.com` for release files) and send nothing else. With `--dry-run` they contact nothing at all.

### `install.sh` (Linux, macOS)

POSIX `sh`; its tests run it under sh, dash and busybox. It needs `curl` or `wget`, `tar`, `mktemp`, `uname`, and one of `sha256sum`, `shasum` or `openssl`.

| Option | Meaning |
| --- | --- |
| `--only tokvera` / `--only sievant` | install just that tool. A halotrcer in a scratch directory installs it and is removed afterwards. |
| `--version VERSION` | install this Halotrcer release, e.g. `vX.Y.Z` (default: latest). The tool versions come from its compatibility matrix. |
| `--dir DIR` | install all three into `DIR` (default: `$HOME/.local/bin`) |
| `--global` | install into `/usr/local/bin`. Run the script as root for that; if the directory is not writable, it stops and says how. |
| `--dry-run` | print the plan; download and change nothing |
| `--no-verify` | skip the SHA-256 and signature checks for all three (not recommended) |
| `--require-cosign` | fail unless cosign is installed and every signature verifies |

Pass options through a pipe with `sh -s --`:

```sh
curl -fsSL https://www.halotrcer.com/install.sh | sh -s -- --require-cosign
curl -fsSL https://www.halotrcer.com/install.sh | sudo sh -s -- --global
```

Exit codes: `0` installed, already installed, or dry run; `1` failure; `2` usage error.

### `install.ps1` (Windows)

Windows PowerShell 5.1 or PowerShell 7. By default each program goes where its own installer puts it: `halotrcer.exe` in `$env:LOCALAPPDATA\Programs\halotrcer`, and `tokvera.exe` and `sievant.exe` in `...\Programs\tokvera` and `...\Programs\sievant`.

| Parameter | Meaning |
| --- | --- |
| `-Only tokvera` / `-Only sievant` | install just that tool (as `--only` above) |
| `-Version VERSION` | install this Halotrcer release (default: latest) |
| `-Dir DIR` | install all three into `DIR` |
| `-Global` | install each into `$env:ProgramFiles\<name>`. Run PowerShell as administrator; the script never elevates. |
| `-DryRun` | print the plan; download and change nothing |
| `-NoVerify` | skip the SHA-256 and signature checks (not recommended) |
| `-RequireCosign` | fail unless cosign is installed and every signature verifies |

Parameters cannot follow `| iex`; use a script block instead:

```powershell
& ([scriptblock]::Create((irm https://www.halotrcer.com/install.ps1))) -RequireCosign
```

The script runs every program (cosign, halotrcer) in a way Windows PowerShell 5.1 cannot abort when the program writes to stderr. Judge a command's result by its exit code, not by red text.

Run as a file (`powershell -ExecutionPolicy Bypass -File install.ps1 …`), the exit codes match `install.sh`. Through `iex` or a script block, the script never calls `exit`, which would close your shell; it sets `$LASTEXITCODE` instead.

### Mirrors

Two variables point both scripts, and `halotrcer install`, at a mirror with GitHub's layout, for every repository of the owner:

- `HALOTRCER_INSTALL_DOWNLOAD_BASE` (default `https://github.com/rajasatyajit`): release files at `<base>/<repo>/releases/download/<tag>/<file>`;
- `HALOTRCER_INSTALL_API_BASE` (default `https://api.github.com/repos/rajasatyajit`): versions at `<base>/<repo>/releases/latest`.

An override must be `https`, or plain `http` to a loopback address only; anything else is a usage error. The checks are the same, so a mirror cannot substitute a binary unless it can also forge the checksum file (and, with cosign installed, its signature).

## Homebrew (macOS)

```sh
brew install --cask rajasatyajit/tap/halotrcer
```

The cask depends on the `rajasatyajit/tap/tokvera` and `rajasatyajit/tap/sievant` casks, so this one command installs all three.

**The macOS binaries are not notarized by Apple.** Homebrew checks each archive's SHA-256 against its cask. Each cask then removes macOS's quarantine flag from the one binary it installed, so Gatekeeper does not refuse it as "damaged". Apple has not vouched for the binary; the guarantee is the checksum and the cosign signature. The cask's caveats show how to check Halotrcer's signature yourself:

```sh
TAG=vX.Y.Z   # the version you installed: halotrcer version
curl -fsSLO "https://github.com/rajasatyajit/halotrcer/releases/download/$TAG/checksums.txt"
curl -fsSLO "https://github.com/rajasatyajit/halotrcer/releases/download/$TAG/checksums.txt.sigstore.json"
cosign verify-blob --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github\.com/rajasatyajit/halotrcer/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
```

The tools' own install docs show the same check for their releases.

## Scoop and winget (Windows)

```powershell
scoop bucket add rajasatyajit https://github.com/rajasatyajit/scoop-bucket
scoop install rajasatyajit/halotrcer

winget install -e --id Halotrcer.Halotrcer
```

Both declare Tokvera and Sievant as dependencies: Scoop installs `rajasatyajit/tokvera` and `rajasatyajit/sievant` from the same bucket, and winget installs `Halotrcer.Tokvera` and `Halotrcer.Sievant`. The winget manifest reaches the community repository through a pull request that Microsoft reviews, so a new version can appear there later than on GitHub.

## Linux packages

Each release has `.deb`, `.rpm` and `.apk` packages for amd64 and arm64. They install `/usr/bin/halotrcer` and the README, LICENSE and NOTICE under `/usr/share/doc/halotrcer/`. Each package depends on `tokvera` and `sievant`, at least the matrix's lowest version and below its upper bound, so the package manager installs them too.

`halotrcer install --via apt|dnf|apk` prints these commands rather than running them: Halotrcer never runs `sudo`.

### The apt and yum repositories

Halotrcer's packages are served from the same signed repositories as Tokvera's and Sievant's, at `https://rajasatyajit.github.io/packages/`. The repository metadata (`InRelease`, `repomd.xml`) and every package are signed with the OpenPGP key `halotrcer-packages.asc` (fingerprint `D2D9 6618 831A C55B 81E2  4DA3 039B 5DA0 D3E3 9071`; the same file is in `packaging/keys/`). Release candidates never enter these repositories.

Debian, Ubuntu (apt installs `tokvera` and `sievant` from the same repository):

```sh
sudo install -d -m 0755 /etc/apt/keyrings
sudo curl -fsSLo /etc/apt/keyrings/halotrcer-packages.asc \
  https://rajasatyajit.github.io/packages/halotrcer-packages.asc
echo "deb [signed-by=/etc/apt/keyrings/halotrcer-packages.asc] https://rajasatyajit.github.io/packages/apt stable main" |
  sudo tee /etc/apt/sources.list.d/halotrcer.list
sudo apt update && sudo apt install halotrcer
```

Fedora, RHEL, Amazon Linux (dnf checks the metadata and every package against the key, which it offers to import on first use):

```sh
sudo curl -fsSLo /etc/yum.repos.d/halotrcer.repo https://rajasatyajit.github.io/packages/rpm/halotrcer.repo
sudo dnf install halotrcer
```

If you already added the repository for Tokvera or Sievant, it serves Halotrcer too. Skip the setup lines, or apt sees the same source twice.

### Alpine (signed apk)

No apk repository exists; the apks are release assets, as Tokvera's and Sievant's are. Each project signs its own apks with its own RSA key.

**Add the three apks in one `apk add`**, so apk resolves Halotrcer's dependencies from the files you give it. Halotrcer's apk added on its own fails: apk finds no `tokvera` or `sievant` to satisfy it. Install the three public keys once, then download the three apks from their releases (for the Tokvera and Sievant versions in Halotrcer's matrix, docs/compatibility.md):

```sh
for p in halotrcer tokvera sievant; do
  wget -O "/etc/apk/keys/satyajit.rajaraman@halotrcer.com-$p.rsa.pub" \
    "https://raw.githubusercontent.com/rajasatyajit/$p/main/packaging/keys/satyajit.rajaraman@halotrcer.com-$p.rsa.pub"
done
# halotrcer_<version>_x86_64.apk, tokvera_<version>_x86_64.apk and
# sievant_<version>_x86_64.apk, each from its own release (aarch64 on arm64)
apk add ./halotrcer_*_x86_64.apk ./tokvera_*_x86_64.apk ./sievant_*_x86_64.apk
```

`make repos-check` tests this resolution, and the apt and dnf ones, in containers.

On Alpine the install script is the simpler path: the binaries are static, so `install.sh` puts the standard Linux builds in `~/.local/bin` without root.

## Container image

`ghcr.io/rajasatyajit/halotrcer` holds all three programs: halotrcer, plus Tokvera's and Sievant's binaries copied from their own released images. Those images are pinned by digest and verified before the build. It is a distroless, non-root (uid 65532) image for linux/amd64 and linux/arm64, cosign-signed by Halotrcer's release workflow:

```sh
cosign verify ghcr.io/rajasatyajit/halotrcer:vX.Y.Z \
  --certificate-identity-regexp '^https://github\.com/rajasatyajit/halotrcer/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

Inside the image, `halotrcer doctor` reports the tools as part of the image. Its fix is a newer image, never an install. `halotrcer install`, `update` and `uninstall` leave the image's binaries alone.

## Helm chart

`oci://ghcr.io/rajasatyajit/charts/halotrcer` is an umbrella chart with Tokvera's and Sievant's charts vendored by digest. Both are off by default; switch them on with `--set tags.tokvera=true` and `--set tags.sievant=true`, and set each chart's own values under `tokvera.` and `sievant.`. The chart's NOTES list what each one needs.

## Build provenance (SLSA)

Every archive, package and SBOM in `checksums.txt`, the container image and the Helm chart carry a SLSA v1 build provenance attestation. Halotrcer's release workflow made it on the version tag. Check it with the GitHub CLI:

```sh
gh attestation verify halotrcer_<version>_linux_amd64.tar.gz --repo rajasatyajit/halotrcer \
  --signer-workflow rajasatyajit/halotrcer/.github/workflows/release.yml
gh attestation verify oci://ghcr.io/rajasatyajit/halotrcer:vX.Y.Z --repo rajasatyajit/halotrcer \
  --signer-workflow rajasatyajit/halotrcer/.github/workflows/release.yml
```

Without access to GitHub's API (an air-gapped host), download the release's `halotrcer_<version>_provenance.sigstore.json` beside the archive and add `--bundle halotrcer_<version>_provenance.sigstore.json` to the first command.

## Uninstall

```sh
halotrcer uninstall          # halotrcer only
halotrcer uninstall --all    # also the tools halotrcer itself installed
```

`--all` removes a tool only when Halotrcer's state file says Halotrcer installed it, through the install scripts or `halotrcer install`. A tool you installed some other way is reported and left alone. Add `--dry-run` to see what would be removed.

Through a package manager, remove the bundle there: `brew uninstall --cask halotrcer`, `scoop uninstall halotrcer`, `winget uninstall Halotrcer.Halotrcer`, `sudo apt remove halotrcer`, `sudo dnf remove halotrcer`, `apk del halotrcer`. Package managers keep the dependencies they pulled in until you remove them too (`apt autoremove`, `dnf autoremove`) or uninstall them by name.
