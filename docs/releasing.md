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
- [ ] **For a stable Halotrcer release:** both tools' winget manifests are merged into `microsoft/winget-pkgs`. winget-pkgs validation refuses a manifest whose dependencies are not in the catalog yet.
- [ ] **The repository is public.** The preflight refuses a private one: attestations need it, and every channel downloads release assets without credentials.
- [ ] **The `release` environment exists,** with you as the required reviewer.
- [ ] **These secrets are set** on `rajasatyajit/halotrcer`:
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
3. **Tag `vX.Y.Z`** on that commit and push the tag.
4. **The draft phase runs.** It checks the tag's shape, that the repository is public, that the tag is on `origin/main`, the secrets, that the keys match `packaging/keys/`, and the pins. It verifies the pinned images and charts and builds the umbrella chart from them. Then it runs the gates, builds, signs, attests and uploads the draft.
5. **Read the draft.** Check the archives, packages, SBOMs, `checksums.txt`, its `.sigstore.json` and the provenance bundle, then publish it.
6. **The publish phase runs, in this order:**
   1. It verifies the published `checksums.txt`, then the bundled tools' images and charts, and builds the umbrella chart from them. Nothing is published before these pass.
   2. It rebuilds, and every archive must be byte-identical to the published one and carry phase 1's provenance.
   3. It pushes the image (halotrcer, tokvera and sievant), the cask, the Scoop manifest and the winget pull request.
   4. It attests the image.
   5. It pushes, signs and attests the umbrella chart.
   6. For a stable tag only, it verifies the published deb and rpm packages and adds them to `rajasatyajit/packages` gh-pages:
      - it follows the shared repository contract;
      - it retries up to 3 times if Tokvera's or Sievant's publishing moves the branch.
7. **Check the results:**
   - the winget pull request passes validation;
   - `brew install --cask rajasatyajit/tap/halotrcer` installs all three;
   - the smoke matrix (`smoke.yml`, M4) is green for the tag.

## Checking it locally

- **`make release-check`** runs `goreleaser check` at 2.18.3.
- **`go test ./scripts/`** tests every release script, the config, the chart and the workflow.
- **`make repos-check`** builds fixture packages with nfpm and tests the shared apt and yum repositories in Docker. It then installs `halotrcer` with apt, dnf and apk and checks that both tools come with it. It takes minutes; ask before running it on a shared machine.
