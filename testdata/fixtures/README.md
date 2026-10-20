# Tool output fixtures

Real outputs of Tokvera and Sievant, captured for Halotrcer's tests before
either tool has a release (PLAN.md D1). They are replaced by outputs of the
released binaries once `v0.2.0-rc.1` of each exists.

Neither tool's source is copied here. Each tool was built from `git archive`
of the commit below into a scratch directory outside its repository, with
its own pinned stock toolchain, and run with a scratch `HOME` and XDG
directories. The version strings are what each tool's own Makefile would
stamp on that commit.

| Directory | Repository | Commit | Toolchain | Version printed |
|---|---|---|---|---|
| `tokvera-b97e278/` | github.com/rajasatyajit/tokvera | `b97e278381ee` | go1.27.2 | `tokvera v0.1.0-dev (commit b97e278)` |
| `sievant-d8a8bcf/` | github.com/rajasatyajit/sievant | `d8a8bcf0694e` | go1.26.9 | `sievant v0.1.0-157-gd8a8bcf` |

Both version strings are outside the compatibility matrix (>=0.2.0), so
tests that load these fixtures also exercise the out-of-range warning.

## Tokvera (`tokvera-b97e278/`)

| File | Command |
|---|---|
| `version.stdout` | `tokvera version` |
| `doctor.json` | `tokvera doctor --json` |
| `demo.*` | `tokvera --json scan --demo --yes --no-cta --out demo` (simulated; grade D; no cost: the embedded sheet has no rate for the demo model) |
| `sim.*` | `tokvera --json scan --target http://127.0.0.1:18089/v1 --model tokvera-sim-batched --suite quick --yes --no-cta --no-gpu --pricing fixture-pricing.yaml --out sim`, against `tokvera sim serve --addr 127.0.0.1:18089` (grade A; cost computed) |
| `flaky.*` | the same with `--model tokvera-sim-flaky` (grade F) |
| `fixture-pricing.yaml` | the `--pricing` sheet the two sim scans used: **invented rates** for the simulator's models, standing in for a user-supplied sheet. Not any provider's prices. |

`<name>.report.json` is `<out>/report.json`, `<name>.stdout` the terminal
summary, `<name>.stderr` the `--json` slog records (the last is
`"msg":"scan result"`).

## Sievant (`sievant-d8a8bcf/`)

Run from the root of the exported source, so `file` paths are relative,
against Sievant's own synthetic configurations in its `testdata/scan/`
(every token in them is fake).

| File | Command |
|---|---|
| `version.stderr` | `sievant version` (it prints on stderr) |
| `version.json` | `sievant version --json` |
| `doctor.json` | `sievant doctor --no-project --json` |
| `scan-all.json` | `sievant scan --config` each of `claude_desktop_config.json claude.json cursor-mcp.json vscode-mcp.jsonc windsurf-mcp_config.json` `--no-project --json` |
| `scan-smoke.json` | `sievant scan --config cursor-mcp.json --config vscode-mcp.jsonc --no-project --json` (the pair Sievant's smoke uses) |
| `scan-broken.json` | `sievant scan --config broken.json --no-project --json` |
| `report.json` | `sievant report --no-clients --no-project --no-cta --format json --out <dir>` (no audit store: configuration-only, with gaps) |

## The one edit

The two `doctor.json` files printed absolute paths of the scratch build
machine, including its user name. In those two files only, the scratch
prefixes and the real home directory were replaced with the fictional
`/home/fixtureuser` (binaries at `/home/fixtureuser/.local/bin`). No other
byte of any fixture was changed.
