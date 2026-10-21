# Developing Halotrcer

## Gates

`make gates` runs every gate in CLAUDE.md: vet (gofmt first), race tests, golangci-lint, govulncheck under the stock toolchain from go.mod, gitleaks, actionlint, and shellcheck at 0.9.0 and 0.11.0. CI (`.github/workflows/ci.yml`) runs the same `make` targets, plus `make release-check`.

Some checks need more than Go, so `make gates` doesn't run them:

- **`make release-check`** needs goreleaser 2.18.3.
- **`make repos-check`** needs Docker and nfpm. It builds packages and tests the shared apt and yum repositories, and installs with apt, dnf and apk in containers.

## Where the Linux jobs run: `LINUX_RUNNER`

The `ci` job (ci.yml) reads a repository variable, `LINUX_RUNNER` (Settings → Secrets and variables → Actions → Variables):

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
git make gcc curl wget tar gzip sha256sum awk sed grep base64 jq gpg openssl qpdf pdftotext dash busybox docker docker-daemon
```

`docker-daemon` means `docker info` works for the runner's user: the daemon runs and the user is in the `docker` group. `make shellcheck` runs the digest-pinned shellcheck images.

On Ubuntu 24.04 these come from:

- **Ubuntu packages:** `git make gcc curl wget tar gzip coreutils gawk sed grep jq gnupg openssl qpdf poppler-utils dash busybox-static` (the installer tests run install.sh under sh, dash and busybox, and through wget as well as curl).
- **Docker:** Docker Engine (Docker's own install guide), with the runner's user added to the `docker` group.

Also needed:

- **x86-64 Linux.** Every tool the job downloads is the `linux-amd64` build.
- **gcc,** because `go test -race` builds with cgo.
- **Network access** to github.com, go.dev, proxy.golang.org, sum.golang.org, vuln.go.dev, get.helm.sh and Docker Hub.

A self-hosted runner keeps state between jobs, such as the Go module cache in its home directory. Nothing in CI depends on it.

M4 adds jobs that can use `LINUX_RUNNER` (the smoke `snapshot` job and `integration.yml`). Each gets its own list here.

## smoke.yml inputs (M4)

`smoke.yml` doesn't exist yet. When M4 adds it, it will have two `workflow_dispatch` inputs:

| Input | Type | Default |
|---|---|---|
| `include_macos` | boolean | `false` |
| `include_windows` | boolean | `false` |

The macOS and Windows jobs run only when their input is true. Otherwise each records `SKIP: not requested (owner approves macOS/Windows runs per release candidate)`, and the final step counts that as a SKIP, not a failure.

## Fixtures

`testdata/fixtures/README.md` records how each tool output was made: which commit, which toolchain, which command. Building either tool is heavy work on a shared machine; ask first.
