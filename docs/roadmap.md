# Roadmap

What Halotrcer doesn't do yet. Nothing here has a date; this page lists what is missing so nobody has to discover it.

## Before the first release

- **The tools' first releases.** Halotrcer bundles Tokvera and Sievant `0.2.0`, and neither is released yet. Until both are, `halotrcer install` has nothing to install. The container image, the Helm chart and Halotrcer's own release can't be built. The smoke matrix runs against Halotrcer's stand-ins for the two tools.
- **A smoke run against the real tools** on every platform: Linux x64 and arm64, macOS on Intel and Apple Silicon, and Windows x64 and arm64 under PowerShell 5.1 and 7. This includes the Homebrew, Scoop and winget dependency installs, and the umbrella chart on kind. The scripts are written and the Linux channels pass locally with the stand-ins.

## Not built

- **Calibrated ranking.** Halotrcer's tiers (scoring version 0.1.0) are judgement, labelled provisional in every report. So is Tokvera's own scoring, which Halotrcer shows as Tokvera labels it. Calibrating either needs real scans.
- **Savings estimates.** Deliberately absent: neither tool measures what a fix would save, so the report shows only measured cost and, with `--monthly-requests`, monthly spend.
- **A `--check-update` flag on `halotrcer doctor`.** `halotrcer update --check` answers the same question today.
- **Shell completions and a man page** (Tokvera has both).
- **Signed reports.** Tokvera can sign its own report (`tokvera report sign`, per its docs/report-signing.md); Halotrcer's combined report isn't signed.
- **An MCP server or agent plugin for Halotrcer** (Tokvera has both).
- **Sievant's live tool inspection.** `halotrcer check` runs `sievant scan` without `--inspect-tools`, so it reads configurations and never starts a server. Inspecting the tools a server offers means starting it, which a read-only check shouldn't do on its own.
- **An apk repository.** Alpine packages are release files, installed together with one `apk add`, as the tools ship theirs.
- **Notarized macOS binaries.** Homebrew installs them with the checksum and cosign signature as the guarantee ([install.md](install.md#homebrew-macos)).

## Depends on the website

`www.halotrcer.com` serves `install.sh`, `install.ps1` and the `/book` page, all maintained outside this repository.
