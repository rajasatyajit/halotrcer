# Releasing Halotrcer

A release is two phases of `.github/workflows/release.yml`, the same as Tokvera's and Sievant's:

1. **Draft.** Pushing a `v*` tag builds and signs everything and uploads a draft release. No package manager or registry changes.
2. **Publish.** When you publish the draft, the workflow:
   - verifies everything again;
   - pushes the image, the Homebrew cask, the Scoop manifest, the winget pull request and the Helm chart;
   - for a stable tag, adds the deb and rpm packages to the shared apt and yum repositories.

Release candidates (`vX.Y.Z-rc.N`) stop short of Homebrew, Scoop, winget and the apt and yum repositories. They do get the draft, the release assets, the versioned image tag and the chart.

## Before the first release

Halotrcer bundles two other releases, so it can only be released after both of them.

- [ ] **Tokvera and Sievant are released.** Each has a published release in the compatibility matrix (`internal/compat/matrix.json`, currently `>=0.2.0 <0.3.0`), with its image and chart pushed.
- [ ] **Tokvera's embedded price list is fresh.** Its `as_of` is 2026-10-11 (`pkg/scan/rates.yaml` in Tokvera), and Tokvera calls it stale more than 60 days later (`EmbeddedPricingStaleDays`, `pkg/scan/pricinginfo.go`). 2026-12-10 is the last clean day: from 2026-12-11, every scan, and so every Halotrcer report, carries the stale-prices warning. Have Tokvera refresh the sheet right before any Tokvera release that Halotrcer will pin.
- [ ] **For a stable Halotrcer release:** both tools' winget manifests are merged into `microsoft/winget-pkgs`. winget-pkgs validation refuses a manifest whose dependencies are not in the catalog yet.
- [ ] **The repository is public** (it is, since 2026-10-21). The preflight refuses a private one: attestations need it, and every channel downloads release assets without credentials. Tokvera's and Sievant's repositories must be public too: `halotrcer install` and every package manager download their releases without credentials.
- [ ] **Private vulnerability reporting is on** (Settings → Code security → Private vulnerability reporting). SECURITY.md offers it as a reporting channel.
- [ ] **The website serves what the docs promise:** `https://www.halotrcer.com/install.sh` and `/install.ps1` (the files from this release's tag, as for the tools' scripts) and `https://www.halotrcer.com/book`, where every report's footer links.
- [ ] **The `LINUX_RUNNER` repository variable is deleted** before the repository is made public. Never use a self-hosted runner on a public repository: a pull request's workflow would run its author's code on that machine. The `runs-on` expression already ignores the variable on a public repository; deleting it keeps the settings honest. `release.yml` itself always runs on GitHub-hosted `ubuntu-24.04` (docs/development.md).
- [ ] **The `release` environment exists,** with you as the required reviewer. Create it before the first tag: GitHub creates a missing environment the first time a job names it, with no protection rules.
- [ ] **These secrets are environment secrets of `release`,** not repository secrets (delete the repository copies). Both jobs of `release.yml` run in that environment, and no other workflow uses a secret:
  - `PKG_GPG_KEY`: the shared Halotrcer Packages OpenPGP private key.
  - `PKG_GPG_PASSPHRASE`.
  - `PKG_APK_KEY`: Halotrcer's apk RSA private key, the private half of `packaging/keys/satyajit.rajaraman@halotrcer.com-halotrcer.rsa.pub`.
  - `PUBLISHER_TOKEN`: it pushes to the tap, the bucket, your winget-pkgs fork and `rajasatyajit/packages`.

## Each release

1. **Pin the tools.** Fill in `packaging/bundled-tools.env` with the Tokvera and Sievant release this version bundles: the tag, the image digest and the chart digest of each.
   - Read the digests with `docker buildx imagetools inspect ghcr.io/rajasatyajit/<tool>:vX.Y.Z` and `oras resolve ghcr.io/rajasatyajit/charts/<tool>:X.Y.Z`.
   - `TestBundledToolsInMatrix` refuses a version outside the compatibility matrix.
   - Both phases check that each image digest names an image index and each chart digest a Helm chart, and cosign-verify each against the identity of that tag's release workflow (`https://github.com/rajasatyajit/<tool>/.github/workflows/release.yml@refs/tags/vX.Y.Z`). An artifact signed for any other tag, or by anything else, is refused.
   - A stable Halotrcer tag must pin stable tool releases; a release candidate may pin release candidates.
2. **Merge to `main`** with every gate green, CI included.
3. **Run the smoke matrix on that commit.** With the tools pinned, `smoke.yml` runs in release mode: it downloads both tools' pinned releases, verifies them with cosign and `checksums.txt`, and scans `tokvera sim serve`. Dispatch it with `include_macos` and `include_windows` for a release candidate you'd ship, and dispatch `integration.yml` for kind. The tagged commit must be the one these passed on.
4. **Tag a release candidate first.** Tag `vX.Y.Z-rc.1` on that commit and push the tag. It runs the whole pipeline, but stays off Homebrew, Scoop, winget and the package repositories. Fix whatever breaks (`-rc.2`, and so on), and only then tag `vX.Y.Z` on the same kind of green commit.
5. **The draft phase runs,** as two jobs:
   1. `draft-gates`, unprivileged: it checks the pins, verifies the pinned images and charts, builds the umbrella chart from them, and runs the gates.
   2. `draft`, after you approve it in the `release` environment: it checks the tag's shape, that the repository is public, that the tag is on `origin/main`, the secrets, and that the keys match `packaging/keys/`. Then it builds, signs, attests and uploads the draft.
6. **Read the draft.** Check the archives, packages, SBOMs, `checksums.txt`, its `.sigstore.json` and the provenance bundle, then publish it.
7. **The publish phase runs, in this order:**
   1. It verifies the published `checksums.txt`, then the bundled tools' images and charts, and builds the umbrella chart from them. Nothing is published before these pass.
   2. It rebuilds, and every archive must be byte-identical to the published one and carry phase 1's provenance.
   3. It pushes the image (halotrcer, tokvera and sievant), the cask, the Scoop manifest and the winget pull request.
   4. It attests the image.
   5. It pushes, signs and attests the umbrella chart.
   6. For a stable tag only, it verifies the published deb and rpm packages and adds them to `rajasatyajit/packages` gh-pages:
      - it follows the shared repository contract;
      - it retries up to 3 times if Tokvera's or Sievant's publishing moves the branch.
8. **Check the results:**
   - the winget pull request passes validation;
   - `brew install --cask rajasatyajit/tap/halotrcer` installs all three;
   - the smoke matrix (`smoke.yml`, M4) is green for the tag. For a release candidate, you decide whether to run the macOS and Windows jobs: dispatch `smoke.yml` with `include_macos` and `include_windows` set to true. Both default to false, and then those jobs record `SKIP not requested (owner approves macOS/Windows runs per release candidate)`.

## What runs where in `release.yml`

Two rules shape the jobs:
- **No secret outside the `release` environment.** Every step that receives a secret runs after your approval.
- **No code that isn't ours in a job that can sign.** A job given `id-token: write` puts `ACTIONS_ID_TOKEN_REQUEST_URL` and `_TOKEN` in every step's environment, so any step in it can mint an OIDC token, and with it a keyless signature or provenance in Halotrcer's name. The race tests run dependency code, so no such job runs a test.

| Job | Environment | Permissions | Secrets | Runs |
|---|---|---|---|---|
| `draft-gates` (tag push) | none | `contents: read` | none | the bundled images and charts verified, the umbrella chart built from them (helm), the gates (`make vet`, `make test`, `make vuln`) |
| `draft` (needs `draft-gates`) | `release` | `contents: write`, `id-token: write`, `attestations: write` | package keys, passphrase, `GITHUB_TOKEN` | preflight and the keys check first, then GoReleaser (build, sign, upload) and provenance. No test. |
| `publish` (release published) | `release` | `contents: read`, `packages: write`, `id-token: write`, `attestations: write` | all four, `GITHUB_TOKEN` | preflight and the keys check first, then the verify, rebuild, compare and publish steps. No test. |

Notes on the table:
- **Nothing passes between the two draft jobs.** `draft` builds nothing from the umbrella chart; `draft-gates` builds it only to prove the pins work, and `publish` builds the one it pushes.
- **GoReleaser only compiles.** It has no hooks and `CGO_ENABLED=0`, so it runs no code from this repository's packages or their dependencies. The image's Dockerfile only copies files.
- **The privileged jobs still run our own release shell scripts** (`scripts/release/*.sh`), `helm` and `goreleaser`, all at the tagged commit.

`TestReleaseWorkflow` holds the rules. It requires:
- `draft-gates` with no secret, no `id-token` and no environment;
- no job with `id-token` running a test step;
- every job that names a secret in the `release` environment;
- each privileged job's preflight as its first step after the checkout.

Each rule was checked by breaking it:
- a secret, `id-token: write`, or an environment added to `draft-gates`;
- `go test`, or the gates step, moved into `draft`;
- a secret-holding job outside the environment;
- `draft` without its environment.

### How it got here

Until 2026-10-21, `release.yml` had Tokvera's and Sievant's layout: `draft-preflight`, then `draft`, and `publish-preflight`, then `publish`.
- The preflight jobs ran outside the environment. They were given the package-signing keys, the passphrase and (in `publish-preflight`) `PUBLISHER_TOKEN`, to check that the secrets are set and the keys match `packaging/keys/`. Anyone able to push a `v*` tag or publish a release ran them without an approval, and once the secrets are environment secrets they can't see them at all.
- The fix moves the secret-holding checks into the privileged jobs, and keeps the gates in an unprivileged job of their own.

### The change, for Tokvera's and Sievant's `release.yml`

Both tools already run their gates in `draft-preflight`, a job without `id-token`. So the test half of this is already right there; only the secrets are in the wrong job.

Their version of the change:
1. **Draft.**
   - Take `preflight.sh draft` and `write-keys.sh --check` out of `draft-preflight`, and make them the first steps of `draft` after its checkout.
   - What's left of `draft-preflight` (checkout, Go, the gates) names no secret. Renaming it `draft-gates` makes that plain, and `draft` keeps `needs:` on it.
   - `draft` runs no test today (only `make toolchain-check`), and must stay that way.
2. **Publish.**
   - Delete `publish-preflight`. Make its two steps the first steps of `publish` after the checkout.
   - Give `publish` the preflight's `if: github.event_name == 'release' && startsWith(github.event.release.tag_name, 'v')` in place of its `needs:`.
   - Add the preflight's timeout to `publish`'s.
   - `publish-preflight` runs no test, so nothing else moves.
3. **What changes in practice.**
   - The non-secret preflight checks (the tag's shape, a public repository, the tag on `origin/main`) now run after the gates and your approval, not before them. A bad tag costs one gates run before it's refused.
   - No check is removed, and every secret stays in the step `env:` it was in.
4. **The tests.** Each tool's workflow test should require the same three rules as `TestReleaseWorkflow` (no secret, `id-token` or environment on the gates job; no test in a job with `id-token`; no secret outside the environment), plus preflight as each privileged job's first step. Break each rule once to see it caught.
5. **Settings, per repository:**
   - the `release` environment with the owner as required reviewer;
   - the four secrets moved into it, with the repository copies deleted;
   - optionally, a tag ruleset limiting who may create `v*` tags.

## Checking it locally

- **`make release-check`** runs `goreleaser check` at 2.18.3.
- **`go test ./scripts/`** tests every release script, the config, the chart and the workflow.
- **`make repos-check`** builds fixture packages with nfpm and tests the shared apt and yum repositories in Docker. It then installs `halotrcer` with apt, dnf and apk and checks that both tools come with it. It takes minutes; ask before running it on a shared machine.
