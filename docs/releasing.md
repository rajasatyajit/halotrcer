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
5. **The draft phase runs.** It checks the tag's shape, that the repository is public, that the tag is on `origin/main`, the secrets, that the keys match `packaging/keys/`, and the pins. It verifies the pinned images and charts and builds the umbrella chart from them. Then it runs the gates, builds, signs, attests and uploads the draft.
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

## Why each phase is one job

Every step that receives a secret runs in the `release` environment, after your approval. Each phase is one job:
- the preflight checks are its first steps, not a separate unprotected job;
- every check runs before anything is built, signed or uploaded.

`TestReleaseWorkflow` holds this.

Until 2026-10-21, `release.yml` had Tokvera's and Sievant's four-job layout: `draft-preflight`, then `draft`, and `publish-preflight`, then `publish`. The two preflight jobs ran outside the environment, and they were given the package-signing keys, the passphrase and (in `publish-preflight`) `PUBLISHER_TOKEN`, to check that the secrets are set and the keys match `packaging/keys/`. Anyone able to push a `v*` tag, or to publish a release, ran them without an approval. With the secrets moved into the environment, those jobs could no longer see them.

### The change, for Tokvera's and Sievant's `release.yml`

Halotrcer's `release.yml` and `scripts/release_workflow_test.go` are the worked example. The same edit applies to each tool:

1. **Draft.** Delete the `draft-preflight` job and move its steps into `draft`:
   - Give `draft` the preflight's `if: github.event_name == 'push'` and drop its `needs: draft-preflight`.
   - Keep `draft`'s `environment: release` and its permissions.
   - The steps, in order:
     1. one checkout (`persist-credentials: false`, `fetch-depth: 0`);
     2. the preflight's steps unchanged: `preflight.sh draft`, `write-keys.sh --check`, the Go setup, the tools the checks need, the bundled-artifact verification (Halotrcer only), and the gates;
     3. then `draft`'s own steps after its checkout and Go setup, which are now duplicates. Drop any tool installer that already ran, such as a second `cosign-installer`.
   - The timeout is the sum of the two (Halotrcer: 30 + 45 = 75 minutes).
2. **Publish.** Delete `publish-preflight` and do the same with `publish`:
   - Give `publish` the preflight's `if: github.event_name == 'release' && startsWith(github.event.release.tag_name, 'v')`, replacing its `needs:`.
   - Make `preflight.sh publish` and `write-keys.sh --check` the steps straight after the checkout. A `TAG` that `publish` already sets in its job `env:` can come out of the step's `env:`.
   - Add the preflight's timeout (Halotrcer: 10 + 45 = 55 minutes).
3. **Nothing else moves.**
   - No check is removed, and every secret stays in the step `env:` it was in.
   - No step before the upload gets `GITHUB_TOKEN` or `GH_TOKEN`.
   - The gates now run in a job that holds `contents: write` and `id-token: write`. That's the same tagged code the job then builds and signs. No step before the upload gets a token in its `env:`, and the checkout persists no credentials.
   - Any "unprivileged preflight job" comment, such as the one in `write-keys.sh`, becomes "preflight steps".
4. **The tests.** Change the workflow test to require:
   - exactly two jobs, `draft` and `publish`, each with `environment: release` and the phase's `if:`;
   - every job in `release.yml` in the environment;
   - preflight as step 1 of each job and the keys check as step 2;
   - in `draft`, the order preflight, keys, bundled chart, gates, build tools, write keys, `goreleaser release`;
   - no `GITHUB_TOKEN` or `GH_TOKEN` in any draft step up to the gates.

   Each rule should be checked by breaking it once: Halotrcer's test catches a job outside the environment, an extra secret-holding job, a token on the preflight step, a build tool installed before the preflight, and a step before the publish preflight.
5. **Settings, per repository:**
   - the `release` environment with the owner as required reviewer;
   - the four secrets moved into it, with the repository copies deleted;
   - optionally, a tag ruleset limiting who may create `v*` tags.

## Checking it locally

- **`make release-check`** runs `goreleaser check` at 2.18.3.
- **`go test ./scripts/`** tests every release script, the config, the chart and the workflow.
- **`make repos-check`** builds fixture packages with nfpm and tests the shared apt and yum repositories in Docker. It then installs `halotrcer` with apt, dnf and apk and checks that both tools come with it. It takes minutes; ask before running it on a shared machine.
