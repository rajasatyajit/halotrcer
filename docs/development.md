# Developing Halotrcer

## Gates

`make gates` runs every gate in CLAUDE.md: vet (gofmt first), race tests, golangci-lint, govulncheck under the stock toolchain from go.mod, gitleaks, actionlint, and shellcheck at 0.9.0 and 0.11.0. CI (`.github/workflows/ci.yml`) runs the same `make` targets, plus `make release-check`.

Some checks need more than Go, so `make gates` doesn't run them:

- **`make release-check`** needs goreleaser 2.18.3.
- **`make install-check`** needs Docker. It runs the install.ps1 tests under the pinned pwsh 7.1.5 and 7.4 images, and the install.sh tests under Alpine's busybox. dash runs in CI, where the test fails without it; the pwsh images have dash but no curl or wget.
- **`make repos-check`** needs Docker and nfpm. It builds packages and tests the shared apt and yum repositories, and installs with apt, dnf and apk in containers.

## Where the Linux jobs run: `LINUX_RUNNER`

The `ci` job (ci.yml), smoke.yml's `snapshot` and `not-requested` jobs, and integration.yml's `integration` job read a repository variable, `LINUX_RUNNER` (Settings → Secrets and variables → Actions → Variables):

```yaml
runs-on: ${{ github.event.repository.private && vars.LINUX_RUNNER || 'ubuntu-24.04' }}
```

- **Unset:** it runs on GitHub's `ubuntu-24.04`.
- **Set** to the label of your self-hosted runner (for example `self-hosted`): it runs there, but only while the repository is private.
- **Never a self-hosted runner on a public repository.** Anyone can open a pull request, and its workflow would run their code on your machine. The expression enforces this by itself: on a public repository it evaluates to `ubuntu-24.04` even if the variable was left behind. Delete the variable before going public anyway, so the settings say what actually happens.

`TestSelfHostedRunnerRules` keeps every job that names `LINUX_RUNNER` on exactly this expression, and checks that each one runs `scripts/ci/runner-tools.sh` first with the tool list below.

Not covered by the variable:

- **`release.yml` never runs on a self-hosted runner.** It runs only on the public repository, and the publish phase verifies the draft's provenance with `--deny-self-hosted-runners`, so a release built on a self-hosted runner could never be published. Its jobs name GitHub-hosted `ubuntu-24.04`, and the test refuses `LINUX_RUNNER` anywhere in it.
- **The smoke matrix** (M4) keeps `ubuntu-latest` deliberately.
- **macOS, Windows and arm64 jobs** stay on GitHub's runners.

## What a self-hosted runner needs

Each job installs its own Go (`setup-go`, from go.mod's toolchain line), golangci-lint, gitleaks, actionlint, goreleaser and helm, all pinned and checksum-verified. The rest comes from the host. GitHub's `ubuntu-24.04` image has all of it once ci.yml installs the PDF checkers there. On a self-hosted runner, the job's first step after checkout runs `scripts/ci/runner-tools.sh`, which fails naming anything missing. Without that check, the PDF tests would skip a missing tool and the run would pass while testing less. The job doesn't use `sudo` there.

`ci` (ci.yml):

```
git make gcc curl wget tar gzip sha256sum awk sed grep base64 jq gpg openssl qpdf pdftotext dash busybox pwsh docker docker-daemon
```

`docker-daemon` means `docker info` works for the runner's user: the daemon runs and the user is in the `docker` group. `make shellcheck` runs the digest-pinned shellcheck images.

On Ubuntu 24.04 these come from:

- **Ubuntu packages:** `git make gcc curl wget tar gzip coreutils gawk sed grep jq gnupg openssl qpdf poppler-utils dash busybox-static` (the installer tests run install.sh under sh, dash and busybox, and through wget as well as curl).
- **Docker:** Docker Engine (Docker's own install guide), with the runner's user added to the `docker` group.
- **PowerShell:** `pwsh` from Microsoft's package repository or a release archive (the install.ps1 tests need it).

Also needed:

- **x86-64 Linux.** Every tool the job downloads is the `linux-amd64` build.
- **gcc,** because `go test -race` builds with cgo.
- **Network access** to github.com, go.dev, proxy.golang.org, sum.golang.org, vuln.go.dev, get.helm.sh and Docker Hub.

A self-hosted runner keeps state between jobs, such as the Go module cache in its home directory. Nothing in CI depends on it.

`snapshot` (smoke.yml; it builds the snapshot, runs the package-repository image, and needs no PDF tools or shells beyond these):

```
git curl tar gzip sha256sum awk sed grep openssl gpg docker docker-daemon
```

`not-requested` (smoke.yml; it records the macOS and Windows channels as skipped):

```
grep sed head
```

`integration` (integration.yml; kind, the package repositories and the installer images):

```
git make curl tar gzip sha256sum awk sed grep jq openssl gpg kubectl docker docker-daemon
```

`kubectl` comes from Kubernetes' package repository or its release binary; kind itself runs as `go run` at its pinned version.

## The smoke matrix (smoke.yml)

`smoke.yml` runs only when dispatched. It has two inputs:

| Input | Type | Default |
|---|---|---|
| `include_macos` | boolean | `false` |
| `include_windows` | boolean | `false` |

The macOS and Windows jobs run only when their input is true. Otherwise the `not-requested` job records each of their channels as `SKIP not requested (owner approves macOS/Windows runs per release candidate)`. That's a SKIP with its reason, which `check-results.sh` accepts, so the run still names every channel.

Until both tools' releases are pinned in `packaging/bundled-tools.env`, the snapshot carries Halotrcer's stand-ins for Tokvera and Sievant (`testdata/standin`, PLAN.md K1), and every RESULT line ends with `(stand-in tools)`.

To run one channel locally (it needs Docker; ask first on a shared machine):

```sh
sh scripts/smoke/snapshot.sh smoke-dist
SMOKE_RESULTS=/tmp/results sh scripts/smoke/run.sh script sh scripts/smoke/linux.sh "<image>" script smoke-dist
```

## Fixtures

`testdata/fixtures/README.md` records how each tool output was made: which commit, which toolchain, which command. Building either tool is heavy work on a shared machine; ask first.
