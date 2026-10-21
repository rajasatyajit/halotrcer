# Compatibility

Halotrcer runs the released `tokvera` and `sievant` binaries and reads their documented JSON. Each Halotrcer release embeds a **compatibility matrix**: the tool versions and report schemas it was built and tested against (`internal/compat/matrix.json`).

## This release

| Tool | Versions | Report it reads | Schema gate |
|---|---|---|---|
| Tokvera | `>= 0.2.0, < 0.3.0` | `tokvera --json scan`'s `report.json` | `schema_version` starts with `tokvera.scan-report/v1.` (tested against `v1.3.0`) |
| Sievant | `>= 0.2.0, < 0.3.0` | `sievant scan --json`; with `--gateway-report`, `sievant report --format json` | `report_version` is `1` |

Versions are compared on major, minor and patch only, so a tool's release candidates (`v0.2.0-rc.1`) count as inside the range.

## What happens outside it

**A tool version outside the range is a warning, not an error.** `halotrcer check` prints it, writes it into the report's header (and into `tools[].compat_warning` in `report.json`), and carries on. `halotrcer doctor` shows the same warning. The results may be incomplete: a newer tool may report something this release doesn't show yet.

**A report schema with an unknown major version is refused.** If Tokvera's `schema_version` isn't `tokvera.scan-report/v1.*`, or Sievant's `report_version` isn't `1`, that tool's part of the check fails with "upgrade halotrcer". Its fields can no longer be trusted to mean what they meant, and Halotrcer would rather show nothing than misread them. The other tool's part is still reported.

Within a supported major, a newer minor version is read the same way and gives no warning: the tools only ever add fields. Halotrcer reads only the fields their documentation lists and ignores the rest.

## How the tools' versions are chosen

- **A direct install** (the install scripts, or `halotrcer install` when no package manager owns the tool) takes the newest stable release **inside the matrix**, passing over any newer one outside it. `--version` installs a specific release.
- **Homebrew, Scoop and winget** install their own newest release of the tool. `halotrcer doctor` then shows whether it's inside the matrix.
- **Packages** state the same range as dependencies:
  - deb, rpm and apk: `tokvera (>= 0.2.0), tokvera (<< 0.3.0)` and the equivalents in each format;
  - winget: `MinimumVersion 0.2.0`, the only bound it supports;
  - Homebrew casks and Scoop: by name only, since they can't express a version.

  Outside those, `halotrcer doctor` is the check.
- **The container image and the Helm chart** carry the exact tool versions this release pinned. Their binaries and charts are taken from the tools' own signed releases and verified before use.

## Checking yours

```sh
halotrcer doctor
```

It lists both tools, where each was found and which channel owns it, its version, and whether that version is inside the matrix. It then runs each tool's own `doctor`. `halotrcer doctor --json` gives the same as data.

## When a tool moves on

A Tokvera or Sievant release outside the range needs a Halotrcer release with a new matrix. That release refreshes the fixtures from the new version's real output and reruns every test against them (`testdata/fixtures/README.md` records how each fixture was made). Until then the older matrix keeps working, with the warning.
