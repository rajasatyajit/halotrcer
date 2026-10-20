# Halotrcer bundle: plan

Halotrcer installs and runs two separately released tools and merges their results into one report:

- **Tokvera**: LLM inference cost and latency scanner.
  - Source: `~/workspace/tokvera`, `github.com/rajasatyajit/tokvera`.
- **Sievant**: MCP security gateway.
  - Source: `~/workspace/aethergate`, `github.com/rajasatyajit/sievant`.

This repository never copies or changes their code. It depends only on their released binaries and their documented JSON. Every fact about the tools below was read from those repositories at Tokvera `b97e278` and Sievant `d8a8bcf` (2026-10-20), and is cited by file. Anything not confirmed there is marked **VERIFY**.

Status: M1 merged (2026-10-20); I1–I5 approved (2026-10-21); M2 in progress on `m2-channels`.

## 0. Where the brief and the repositories disagree

These change the design. The brief's wording is kept in the requirements below wherever the repositories allow it. Where they do not, the plan follows the repositories.

| # | Brief | What the repositories say | Plan |
|---|---|---|---|
| C1 | "released binaries" | **Neither tool has a release.** Both repositories are private. Tokvera has a `v0.1.0` tag, which predates `scan` (tokvera docs/releasing.md:8). Sievant's `v0.1.0` exists only locally and predates `scan`, `report` and `doctor` (sievant README.md:24). Both plan `v0.2.0-rc.1`, then `v0.2.0`. Both preflights refuse to release from a private repository. `rajasatyajit/packages` has no `gh-pages` branch. `homebrew-tap` holds only a README. | The code and tests can be built now, against fixtures (D1). Halotrcer cannot be released, and its smoke matrix cannot pass, until both `v0.2.0` releases exist. |
| C2 | `tokvera scan --json` | There is **no `scan --json`**. The global `tokvera --json scan …` writes slog records to stderr. The report is always written to `<--out>/report.json` and `report.html` (tokvera cmd/tokvera/scan.go:305-327, 424-446). `--yes` is required without a terminal; otherwise it exits 2. | Run `tokvera --json scan --target URL --yes --no-cta --out <tmp>/tokvera …` and read `<tmp>/tokvera/report.json`. Use the final stderr `"scan result"` record only for progress. |
| C3 | `sievant scan --format json` | **`sievant scan --json`** prints JSON on stdout. Only `report` takes `--format` (sievant cmd/sievant/scan.go:51). `report` covers gateway and audit-store activity, writes files even with `--json`, and reports a missing audit store as a gap. | `check` runs `sievant scan --json [--config …] [--no-project]`, without `--inspect-tools`, which would start servers. `sievant report --format json --no-cta --out <tmp>` is optional, behind `--gateway-report` (D6). |
| C4 | "check `schema_version`" | Tokvera: `schema_version: "tokvera.scan-report/v1.2.0"`, and readers check the prefix `tokvera.scan-report/v1.` (tokvera pkg/scan/report.go:14, summary.go:24). Sievant: there is **no `schema_version`**. Its field is `report_version: 1` (sievant pkg/scan/report.go:15). Both only ever add fields. | Accept Tokvera's `tokvera.scan-report/v1.*` and Sievant's `report_version == 1`. Refuse anything else, with a message naming the compatibility matrix. |
| C5 | Findings with ids, severities, savings | Tokvera scan findings carry `rank, check_id, status (warn\|fail), preview, title, evidence, why_it_matters, suggested_fix, effort, impact` (grade points lost). They have no `severity` and no `savings`. Cost is `cost.usd_per_1k_requests`, which is absent when not computed. Sievant scan findings carry `rule, severity, title, client, file, server, where, evidence (masked), detail, fix`. They have no id and no score (sievant pkg/scan/report.go). | The scoring model (§3) works from these fields only. Dollar figures need a measured Tokvera cost plus a request volume the user supplies. No per-finding dollar savings are claimed, because neither tool measures them. |
| C6 | PDF "via the same approach Sievant used" | Sievant writes PDF with its own pure-Go writer in `internal/pdf`: standard Type1 Helvetica, WinAnsi, uncompressed and deterministic, with no dependency and no headless browser. It is `internal/` and must not be copied. Tokvera has no PDF; it states "browser Print to PDF is sufficient" (tokvera SPEC.md:378). | Halotrcer gets its own small `internal/pdf` written from the PDF 1.4 specification with the same technique: standard fonts, AFM widths, no dependency. The code is new, not copied. CI checks it with qpdf and pdftotext, as Sievant does. |
| C7 | "Verify checksums, then signatures" | Both installers verify the cosign signature over `checksums.txt` first, then the archive's SHA-256 against its line (tokvera scripts/install.sh:283-303; sievant the same). That order is the sound one: a checksum is only trusted once its file is signed. | Follow the tools: signature first (when cosign is present, or required with `--require-cosign`), then SHA-256. |
| C8 | Homebrew "formula/cask" | Both tools ship a **cask** in `rajasatyajit/homebrew-tap`, `Casks/` (`brew install --cask rajasatyajit/tap/tokvera`). | A Halotrcer cask that `depends_on cask:` both. **VERIFY** in M2 that GoReleaser v2.18.3's `homebrew_casks.dependencies` and Homebrew's cask DSL support this (against `goreleaser jsonschema` and the Cask Cookbook). |
| C9 | Smoke: "winget upgraded on the runner"; "kind" | Neither smoke workflow has a winget *upgrade* step. Both install a pinned winget (≥1.12.350; Sievant pins v1.29.380) when the runner's is older. Both run kind in `integration.yml` (`make helm-kind`), not in smoke. | The winget behaviour matches the brief's intent; keep their approach. kind runs in `integration.yml` (D9). |
| C10 | Gates: gitleaks, actionlint in CI | Neither tool runs actionlint in a workflow. Tokvera runs `actionlint@v1.7.12` locally. Gitleaks runs in Sievant's `make secrets`, and in Tokvera only in the pre-commit hook. Shellcheck: Tokvera uses the runner's 0.9.0 in CI and a digest-pinned 0.9.0 locally; Sievant digest-pins 0.9.0 and 0.11.0 in both. | Halotrcer runs every gate in CI as well as locally. That is stricter than either tool, which the brief asks for. Shellcheck uses Sievant's digest-pinned images. |
| C11 | Home as `~`, username hidden | Sievant `report` does this (`pkg/report/privacy.go`). Sievant `scan` JSON and HTML keep absolute paths. Tokvera strips URL userinfo, query and fragment, and has no home or username handling. | Halotrcer applies its own scrubber (`internal/redact`) to every field it renders or writes, in addition to both tools' masking, which it never undoes. See §4. |
| C12 | One toolchain | Tokvera pins `toolchain go1.27.2`; Sievant pins `go1.26.9`. golangci-lint is v2.14.0 in Tokvera and 2.13.2 in Sievant; govulncheck is v1.9.0 and v1.8.0. | Halotrcer pins `go 1.27.0` / `toolchain go1.27.2`, golangci-lint v2.14.0 and govulncheck v1.9.0, the newer of each pair; each verified against its upstream release page on 2026-10-20. |

## 1. Owner decisions (2026-10-20)

- **D1:** before the tools' release candidates exist, build each tool at a pinned commit into the scratch directory, never inside its repository. Commit their outputs as `testdata/fixtures/<tool>-<commit>/` with the commit recorded. Tell the owner before starting the builds. Replace the fixtures with outputs from the released binaries once the rcs exist.
- **D2:** Tokvera `>=0.2.0 <0.3.0` with schema `tokvera.scan-report/v1.*`; Sievant `>=0.2.0 <0.3.0` with `report_version 1`. rcs of those versions count as in range. Outside the range: warn and continue. An unknown schema major: refuse.
- **D3:** the ranking order in §3 stands.
  - **Deal risk** comes from Sievant rules only: SEC001–SEC004, AUTH001, NET001, PERM001 and PERM002.
  - A Tokvera grade of D or F shows as a **reliability concern**, not as deal risk.
- **D4:** a dollar figure is only measured `cost.usd_per_1k_requests` × the user's `--monthly-requests`, shown with Tokvera's `pricing.as_of` and `pricing.source`. `--pricing` is passed straight to Tokvera. **No savings figure anywhere.**
- **D5:** Halotrcer **never runs sudo.**
  - brew, scoop, winget and `go install` (with `--via go`) run directly.
  - For apt, dnf and apk, it prints the exact commands, repository setup included, and stops.
  - Without a package manager, the default is the direct verified download into `~/.local/bin` (Windows: `%LOCALAPPDATA%\Programs\<tool>`), which needs no root.
- **D6:** `sievant report` is not run by default; it is available behind `--gateway-report`.
- **D7:** the shared repository description stays unchanged. Halotrcer ships the byte-identical `repo-meta.env` seed. Its packages go to `apt/pool/main/h/halotrcer` and to `rpm/`, with `rpm/halotrcer.repo`.
- **D8:** in the umbrella chart, `tokvera.enabled` and `sievant.enabled` both default to false. NOTES explains what to set.
- **D9:** kind runs in `integration.yml`, as it does in both tools.
- **D10:** names:
  - The repository is `rajasatyajit/halotrcer` and the module is `github.com/rajasatyajit/halotrcer`. Users type `go install github.com/rajasatyajit/halotrcer/cmd/halotrcer@latest`. The local folder stays `~/workspace/halotrcer-installer`.
  - winget: `Halotrcer.Halotrcer`, publisher `Halotrcer`.
  - The cask, Scoop app and deb/rpm/apk package are all `halotrcer`.
  - Image `ghcr.io/rajasatyajit/halotrcer`; chart `oci://ghcr.io/rajasatyajit/charts/halotrcer`.
  - apk key `satyajit.rajaraman@halotrcer.com-halotrcer.rsa.pub`.
- **D11:** the NOTICE copyright line is "Satyajit Rajaraman / Halotrcer".
- **C12:** `toolchain go1.27.2`, golangci-lint v2.14.0, govulncheck v1.9.0, actionlint v1.7.12 and gitleaks v8.30.1. Each was checked against its upstream release page on 2026-10-20.

### Implementation decisions taken in M1 (approved 2026-10-21)

- **I1: the install channel follows D5.** A tool that is already installed is upgraded through the channel that owns it. Ownership is detected from a brew Caskroom path, the scoop and winget directories, dpkg/rpm/apk file ownership, or GOBIN, as `tokvera doctor` reports install kinds. A fresh install uses brew on macOS, then scoop and winget on Windows, when present: none of them needs root. Otherwise it uses the direct verified download. apt, dnf and apk are never the default. On Linux they are almost always present, so choosing them would turn every `halotrcer install` into printed commands; they are used only when they already own the tool, or with `--via`, and then their commands are printed (D5).
- **I2: `check` keeps Tokvera's confirmation.** Tokvera refuses to send load without a terminal unless `--yes` is given. `halotrcer check` therefore runs Tokvera's own `--dry-run`, shows the plan and asks, and passes `--yes` only after a yes or with `halotrcer check --yes`. `--demo` (Tokvera's built-in simulator) needs no confirmation.
- **I3: Tokvera's provisional label.** report.json carries `score.provisional` and `scoring_version`, not the words. The words come from Tokvera's documentation (docs/scoring.md:94), and the report shows them verbatim. **Done in Tokvera `0f37696` (schema v1.3.0, 2026-10-21):** report.json carries `score.provisional_label` and `score.provisional_note` while scoring is provisional. Halotrcer shows those words, and keeps its copy only as the fallback for a report older than v1.3.0 (`TestTokveraProvisionalWords`).
- **I4: mirrors.** `HALOTRCER_INSTALL_DOWNLOAD_BASE` and `HALOTRCER_INSTALL_API_BASE` are owner-level bases, with `/<repo>/…` appended, because Halotrcer installs from three repositories.
- **I5: PDF metrics.** Glyph widths come from Adobe's Core 14 AFM files, with their notice and licence kept (internal/pdf/AFM-LICENSE.txt, NOTICE).

## 2. Milestones

Each milestone is one branch (`m1-cli`, `m2-channels`, `m3-installers`, `m4-smoke`, `m5-docs`) with one commit per logical change. A milestone merges to `main` only when every gate in CLAUDE.md passes. I stop and report at each milestone's end. I never push, tag or publish.

### M1: the `halotrcer` CLI (thin orchestrator)

Parts, one commit each:

1. **Skeleton.**
   - `go.mod` (toolchain pinned, C12), Apache-2.0 LICENSE and NOTICE, `.golangci.yml` (the tools' v2 config shape), Makefile targets `build vet test lint vuln toolchain-check secrets actionlint shellcheck gates`, and `.gitignore` and `.gitleaksignore`.
   - `ci.yml` with every gate (C10), on ubuntu-24.04, with SHA-pinned actions copied from the tools' pins and re-read from each action's releases page. `contents: read`, `persist-credentials: false`, and checksum-verified tool downloads.
   - No telemetry: there is no network access except the scanned target, release downloads during install/update, and `doctor --check-update`.
2. **`internal/compat`.**
   - The embedded compatibility matrix (D2) and the version probes:
     - `tokvera version`: the first line on **stdout**, `tokvera vX.Y.Z (commit …)`.
     - `sievant version`: on **stderr**, `sievant vX.Y.Z`, or `sievant version --json` on stdout.
   - The schema gates (C4).
3. **`internal/tools`.**
   - Locate the binaries on PATH or in the known install directories, and run each as a subprocess with a context timeout.
   - Clean environment: pass through only what each tool documents. For Tokvera that is the `--api-key-env` variable the user names, `TOKVERA_*` and `HOME`; for Sievant, `SIEVANT_*`, `HOME` and the XDG/APPDATA variables. Never log values.
   - Capture output into bounded buffers.
4. **`internal/ingest`.**
   - Decode only the documented fields of each report; unknown fields are ignored (no `DisallowUnknownFields`).
   - Fuzzed decoders.
5. **`internal/scoring`** (§3) and `docs/scoring.md`, table-driven tests.
6. **`internal/redact`** (§4), with canary tests: planted token, key, env value, home path and username in fixture inputs must not appear in any output byte.
7. **`internal/report`.**
   - The combined model, then:
     - JSON with `schema_version: "halotrcer.check-report/v1.0.0"`;
     - self-contained HTML (`html/template`, inline CSS, a CSP of `default-src 'none'; style-src 'unsafe-inline'`, no scripts, light and dark themes);
     - PDF through the new `internal/pdf` (C6).
   - Sections:
     1. executive summary with the top 5 findings;
     2. Cost & performance (Tokvera), with grade, `score.provisional` and Tokvera's label "Provisional scoring" plus `ProvisionalNote`, verbatim from its report fields, never retyped;
     3. Agent security (Sievant);
     4. 30-day fix plan;
     5. footer CTA to `https://www.halotrcer.com/book`, "AI Cost & Security Audit", omitted with `--no-cta`.
   - Both tools are always run with their own `--no-cta`, so there is one CTA.
   - Golden files for all three formats.
8. **`halotrcer check`.**
   - Flags:
     - `--target URL` (required unless `--skip-tokvera`)
     - the pass-throughs `--kind --model --api-key-env --suite (default quick) --max-requests --max-cost --pricing --allow-private-target`
     - `--mcp-config PATH` (repeatable, becomes Sievant `--config`), `--no-project`
     - `--monthly-requests`
     - `--out DIR`, `--format html,pdf,json`, `--no-cta`, `--skip-tokvera`/`--skip-sievant`, `--timeout` (default 5m)
   - Tokvera (network to the target) and Sievant `scan` (local files, no `--inspect-tools`) run **in parallel**: they share no files, and each gets its own temp directory.
   - Exit codes: 0 report written; 1 runtime or tool failure; 2 usage. A tool that fails yields a partial report with the failure stated, and exit 1 unless `--allow-partial`.
   - The terminal output prints paths with `~`.
9. **`halotrcer install [tokvera|sievant|all]`** (D5: never sudo). Detection order:
   - brew (cask), scoop and winget run directly;
   - for apt, dnf and apk, print the exact commands (repository setup included) and stop;
   - `go install`, only when Go is present and the user asks with `--via go`;
   - otherwise, the direct verified download into `~/.local/bin`: signature-then-checksum (C7), Rosetta and musl detection, atomic install.
   - It records what it installed and how in `~/.local/state/halotrcer/installed.json` (Windows: `%LOCALAPPDATA%\halotrcer`).
   - It upgrades through the same channel that installed the tool.
10. **`halotrcer doctor [--json]`.**
    - Runs `tokvera doctor --json` and `sievant doctor --json`; both return `{checks[],warnings,failures}`.
    - Adds its own checks: tools found, versions against the matrix, cosign present, writable output directory, state file readable, and the channel that owns each tool.
    - Exit 0 when nothing failed, 1 when something did.
11. **`halotrcer update`** (itself, same mechanics as each tool's `update`), and **`halotrcer uninstall [--all]`**.
    - `uninstall` removes Halotrcer.
    - `--all` also removes a tool only when the state file says Halotrcer installed it. A tool installed separately is reported and left alone.

Risks:
- Tokvera's `quick` suite duration against a slow target: the 5-minute budget is enforced with `--timeout` and Tokvera's own `--max-requests`.
- The tools change their JSON before v0.2.0: mitigated by refreshing fixtures from the rcs.

### M2: bundle channels

Owner input (2026-10-21): the apk public key is `packaging/keys/satyajit.rajaraman@halotrcer.com-halotrcer.rsa.pub` (RSA-4096, DER sha256 `8fd0bd73…15c4`); `PKG_APK_KEY` and the shared release secrets are set on `rajasatyajit/halotrcer`. Tokvera is adding the provisional label and note to report.json (a schema minor bump): Halotrcer reads them from there once that lands and keeps its copy (I3) only as the fallback for older reports.

Corrections found in M2:

| # | Brief or plan | What the repositories say | M2 |
|---|---|---|---|
| C13 | "deb/rpm/apk into the shared signed repos" | The §6l contract covers apt and yum only. Neither tool publishes an apk repository: their apks are release assets, installed with `apk add ./file.apk` (both docs/install.md). | deb and rpm go to the shared repositories; the apk is a release asset, as theirs. Its `depends` resolve when the three apks are added in one `apk add` (`make repos-check` tests it; not yet run), which M3's installer and docs must do. |
| C14 | Package dependencies with a version range | Homebrew cask `depends_on cask:` and Scoop `depends` take no version (Cask Cookbook; Scoop lib/depends.ps1). winget has `MinimumVersion` only (schema 1.12.0). deb, rpm and apk each take their own syntax; nfpm v2.47.0 writes strings unchanged. | cask and Scoop: names only (`rajasatyajit/tap/<tool>`, `rajasatyajit/<tool>`); winget: `MinimumVersion 0.2.0`; deb/rpm/apk: `>= 0.2.0` and `< 0.3.0` in each format's syntax (`TestPackageDependenciesMatchMatrix`). Outside those, `halotrcer doctor` warns on a version outside the matrix. |

Dependency syntax, verified against each channel's own source (2026-10-21):
- **Homebrew cask:** docs.brew.sh Cask Cookbook, `depends_on`: "cask: required Homebrew cask tokens as string or array". A `tap/token` loads from that tap (Homebrew `cask/cask_loader.rb` FromTapLoader); an unqualified token searches every tap and can be ambiguous. GoReleaser v2.18.3 renders `dependencies: [{cask: …}]` verbatim, sorted (`internal/pipe/cask/template.go`).
- **Scoop:** `depends` accepts `bucket/app` (Scoop `lib/core.ps1` parse_app; `lib/depends.ps1` aborts when the bucket is not added). Users add the bucket as `rajasatyajit`, as both tools document.
- **winget:** `Dependencies.PackageDependencies` with `PackageIdentifier` and `MinimumVersion` (GoReleaser v2.18.3 emits manifest 1.12.0). The client installs package dependencies since 1.6 (release v1.6.2631 notes). winget-pkgs: "Dependencies … must come from the same source" (installer schema 1.12.0) and validation checks that each dependency exists in the catalog (doc/Validation.md), so the tools' manifests must be merged first (docs/releasing.md).
- **deb/rpm/apk:** Debian Policy 7.1 (`<<`, `>=`), rpmpack relations (`<`, `>=`), apk-world(5) (`<`, `>=`); one constraint per entry, per-format `overrides`, no top-level list.

Built (branch `m2-channels`):
- **`.goreleaser.yaml`** (GoReleaser v2.18.3, `goreleaser check` clean): archives `halotrcer_{{ .Version }}_{{ .Os }}_{{ .Arch }}` with LICENSE, NOTICE and README; `checksums.txt`; syft SBOMs; keyless cosign bundle; deb/rpm/apk (apk key name `satyajit.rajaraman@halotrcer.com-halotrcer.rsa.pub`); `dockers_v2`; cask, Scoop and winget with the dependencies above and `skip_upload: auto`; draft release disabled in the publish phase.
- **Image:** distroless static-debian13 :nonroot by the tools' digest, user 65532. halotrcer from the build; tokvera, sievant and their LICENSE/NOTICE copied out of their released images.
- **I6, bundled tools:** `packaging/bundled-tools.env` pins each tool's release tag, image digest and chart digest. It is parsed strictly, never sourced (`bundled-tools.sh`), and is empty until both tools release; the preflight refuses an empty or malformed file. The publish job cosign-verifies each digest against the **exact** identity of that tag's release run (`…/release.yml@refs/tags/vX.Y.Z`), not the tag-prefix regexp, so a digest is accepted only from the release it claims. Nothing is built from them before that.
- **I7, umbrella chart** `charts/halotrcer`: no resources of its own; `tokvera` and `sievant` dependencies from `oci://ghcr.io/rajasatyajit/charts`, `condition: <tool>.enabled`, both false (D8); NOTES says what each needs. Helm never resolves the dependencies: `fetch-chart.sh` fetches each chart by its verified digest with oras and checks both the manifest and the layer digest itself, and `package-chart.sh` vendors them, checks each chart's name and version against the pins, lints with both off and both on, and packages. Pushed, signed and attested in the publish job only.
- **Shared repositories:** `package-repos.sh`, `package-repos-docker.sh` and `publish-repos.sh` implement §6l: the same parsing, regexes, architecture union, re-sign on a changed description, idempotence, pinned image and tool versions, 3 attempts with 5 s/10 s backoff from a fresh clone, only on a moved branch. Halotrcer's pool is `apt/pool/main/h/halotrcer`, `rpm/halotrcer.repo`, commit "halotrcer TAG". **I8:** on a failure path it is stricter than the tools' scripts: the settings, the signing key and every replaced package are checked before the site is written (theirs refuse at the same points, after copying), so a refused publish leaves the site exactly as it was. Behaviour on success is the same.
- **`release.yml`:** the tools' two phases and job set (`draft-preflight`, `draft`, `publish-preflight`, `publish` in `packages-gh-pages`), with the steps above. No air-gapped bundle (Sievant's own feature).
- **Tests:** `scripts/` (config, keys, seed sha256 equal to both tools' copies, pins, preflight, write-keys, compare-archives, image-subject, push-chart, package-chart with fixture subcharts, workflow rules for every workflow, release.yml order and gates), mutation-checked. `make repos-check` (Docker, `-tags integration`): a site as the tools leave it stays byte for byte through two Halotrcer publishes, a riscv64 Tokvera package is indexed, a re-run changes nothing, malformed, `$(…)`, backtick, incomplete and bad-architecture `repo-meta.env`, a replaced package and a wrong key are each refused with the site unchanged and nothing executed, a changed description re-signs, an empty site gets the seed; then apt (Ubuntu), dnf (Fedora) and apk (Alpine) install `halotrcer` and both tools come with it; and the push race (one race retried, three races give up with nothing pushed).
- **Waits for the owner's go-ahead (Docker):** `make shellcheck` (digest-pinned images), `make repos-check`, a `goreleaser release --snapshot` to prove the archives rebuild byte-identically and the packages and image build. The snapshot image needs tool images; until they exist it uses locally built stand-ins.
- Risks: winget reviewers may question a bundle manifest (no rule against it found; UNVERIFIED beyond doc/Validation.md). The image and chart cannot be built for real until both tools publish images and charts.

### M3: universal installer scripts

- `scripts/install.sh` (POSIX sh) and `scripts/install.ps1`, to be served at `https://www.halotrcer.com/install.sh` and `/install.ps1`.
- Default: Halotrcer, Tokvera and Sievant. `--only tokvera|sievant` installs just one.
- Flags exactly as the tools': `--version`, `--dir`, `--global`, `--dry-run`, `--no-verify`, `--require-cosign`, `-h`. `--version` applies to Halotrcer; the tool versions come from the release's embedded matrix file.
- Mirror overrides: `HALOTRCER_INSTALL_DOWNLOAD_BASE` and `HALOTRCER_INSTALL_API_BASE`, https or loopback http only. Each tool is fetched from its own release with its own cosign identity regexp, as in each tool's script.
- OS, arch, Rosetta and musl detection; signature then checksum (C7); idempotent; atomic install.
- At the end it prints `halotrcer check --target <url>`.
- PowerShell:
  - every native call goes through `Invoke-HalotrcerNative` (Process-based, safe on 5.1 when stderr is written);
  - every script-scope variable is `$script:Halotrcer*`;
  - preferences are set inside the function;
  - exit codes come from `Invoke-HalotrcerInstallMain`: `exit` only with `$PSCommandPath`, otherwise `$global:LASTEXITCODE`, never a leftover value.
- Tests mirror the tools':
  - sh, dash and busybox;
  - pwsh 7.4 and 7.1.5 images;
  - every native call through the helper;
  - dot-sourcing keeps the caller's `$repo`;
  - `iex` keeps the shell.

### M4: smoke matrix

- `smoke.yml`, `workflow_dispatch` only, in the tools' structure. The jobs are `snapshot`, `linux`, `linux-host`, `macos` and `windows`, with the same distro and channel table:

  | Distro | Channels |
  |---|---|
  | ubuntu-24.04 | script, script-latest, deb, apt-repo |
  | debian-13 | script, deb, apt-repo |
  | fedora-43 | script, rpm, yum-repo |
  | amazonlinux-2023 | rpm, yum-repo |
  | alpine-3.22 | script, apk |

  - Linux runs on `[ubuntu-latest, ubuntu-24.04-arm]`. macOS runs on macos-latest (with a Rosetta channel) and macos-15-intel.
  - Windows runs on `[windows-latest, windows-11-arm]` × `native/script/iex` under PowerShell 5.1 and 7, plus `scoop-ps7` and `winget-ps7` (a pinned winget when the runner's is older).
- Every step uses `if: !cancelled()` and appends `RESULT <channel> PASS|SKIP <reason>|FAIL <reason>`. `check-results.sh` alone decides the job.
- Per channel:
  - the three versions are checked against the matrix;
  - `halotrcer check` runs against a mock LLM server and the fixture MCP configs;
  - the report must exist (html, pdf, json), contain no `http(s)://` resource reference and no `<script`, contain both section headings, and carry no planted secret;
  - `pdftotext` must succeed where present.
- Two of these are **VERIFY**:
  - **Mock LLM:** Tokvera's `testdata/mockllm` is test code in their repository, not a released binary. Proposal: use the released `tokvera sim serve`, which is a shipped command that serves the fake OpenAI-compatible API.
  - **Fixture MCP configs:** Sievant's fixture configs are not shipped. Halotrcer gets its own `testdata/mcp/` written to the same documented client format, with fake tokens.
- `integration.yml` job `helm-kind` (D9; push to main, nightly, dispatch): install the umbrella chart with both components enabled, and wait for the Tokvera CronJob run and the Sievant gateway Deployment to be Ready.

### M5: docs

- `README.md`, under 250 lines: what Halotrcer is, a one-line install, a one-line check, links to both tools, the privacy statement and `/book`.
- `SECURITY.md`, with a 5-business-day acknowledgement aim and GitHub private reporting.
- `docs/` pages: `install.md`, `check.md`, `report-anatomy.md`, `scoring.md` (written in M1, refined here), `compatibility.md`, `faq.md` and `roadmap.md`.
- No figures, customers or claims that the code and the two tools' outputs do not support. A test pins the README's commands to the CLI's real flags, as Tokvera's `docs_test.go` does.

## 3. Impact-scoring model (`internal/scoring`)

The model only ranks things. It never computes new measurements. Inputs are documented fields only.

**Tokvera findings.** Inputs: `status` (warn or fail), `impact` (grade points lost), `check_id`, and the pillar of the check from `checks[].pillar`.
- Dimension:
  - `cost_and_cache` maps to **cost**;
  - `latency` and `capacity` map to **latency**;
  - `reliability` maps to **latency**;
  - a grade of D or F adds a **reliability concern** tag to the Tokvera section and the summary (D3), never deal risk;
  - `observability` maps to **operational**.
- Weight: `impact`, normalized to 0..1 over the report's maximum. `fail` scores above `warn` at equal impact.

**Sievant findings.** Input: `severity`. The weights are critical 1.0, high 0.75, medium 0.5, low 0.25, info 0.
- Dimension: **security exposure**, plus the deal-risk tag for the rules in D3.

**Ranking across tools.** Tiers are compared first, in this order:
1. critical security, or a deal-risk finding at critical or high severity;
2. Tokvera `fail` on cost or latency with `impact` ≥ the median;
3. the remaining high security findings;
4. the remaining Tokvera fails;
5. everything else.

Within a tier, findings are ordered by weight. Ties go to Tokvera `rank`, then Sievant `rule`, so the order is deterministic.

The top 5 make up the executive summary. Every entry carries its source tool, its source field values, and why it got its tier.

**30-day fix plan.**
- Week 1: tier 1, plus any fix with Tokvera `effort` under 1 day.
- Weeks 2–3: tiers 2–3.
- Week 4: the rest.
- Each item uses the tool's own `suggested_fix` / `fix` text. Halotrcer writes no new remediation advice.

**Money.** See D4. A dollar figure appears only as measured `usd_per_1k_requests` × the user's `--monthly-requests`, with the pricing date and source. There is never a savings estimate.

**Provisional label.** While Tokvera's `score.provisional` is true, its label and note are shown next to the grade. Halotrcer's own tiering is versioned `scoring_version` 0.1.0 and labelled provisional too, so that it is not presented as calibrated.

## 4. Redaction

- Both tools' masking is preserved; Halotrcer never re-reads raw configuration files.
- On top of that, every string Halotrcer renders passes through `internal/redact`:
  - URLs keep scheme, host and path only. Userinfo, query and fragment are dropped, and token-like path segments are masked.
  - The value of any environment variable is never printed. Only names appear, such as the `--api-key-env` name.
  - Paths under home become `~`.
  - The current user's name becomes `<user>` (from the home directory's basename, `USER`, `USERNAME` and `LOGNAME`, whole words, with system names exempt, as Sievant's report does).
- Tests plant canaries in the fixture inputs and in the environment, and grep all outputs, logs and errors for them.

## 5. Cross-cutting risks

1. Both tools are unreleased and private (C1). Halotrcer's release is blocked on two other repositories' owner-gated releases.
2. Shared `gh-pages` races across three products. The concurrency group is per repository, so only `publish-repos.sh`'s fetch, rebuild and retry protects against them. The tests prove it with a fake `git`.
3. The two toolchains diverge, Tokvera on 1.27 and Sievant on 1.26. That doesn't matter at run time, since the tools are separate binaries, but govulncheck must run under Halotrcer's own pinned toolchain, never this machine's `go1.27.1-X:nodwarf5`. `make toolchain-check` refuses it.
4. This machine is shared with other sessions. Snapshot builds, Docker, kind, `smoke-local` and building the tools for fixtures (D1) all wait for your go-ahead.

## 6. Acceptance (reported as a table at each milestone's end)

| Criterion | Proven by | Earliest |
|---|---|---|
| One command on a fresh machine installs all three | `script` and `iex` smoke channels | M4, after both tools' v0.2.0 |
| `halotrcer check` gives the combined report in under 5 minutes | smoke timing assertion, plus a unit test with `--suite quick` | M1 (local), M4 |
| brew, scoop, winget, apt and dnf pull in both tools | smoke `brew`, `scoop-ps7`, `winget-ps7`, `apt-repo`, `yum-repo` | M4, after first publish |
| The umbrella chart installs both on kind | `integration.yml` `helm-kind` | M4 |
| All signatures verify | cosign on checksums, image and chart; `gh attestation verify` | M2 snapshot (unsigned), first rc |
| All gates clean | `make gates`, run immediately before each report | every milestone |
