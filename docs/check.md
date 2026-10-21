# `halotrcer check`

```sh
halotrcer check --target URL [flags]
halotrcer check --demo [flags]
```

One run of each tool, at the same time, merged into one report:

- **Tokvera** scans the inference endpoint at `--target`. It sends a bounded load and measures latency, throughput, cost per request and cache use.
- **Sievant** scans the MCP client configurations on this machine. It reads files only: it starts no MCP server and changes nothing.

The two share nothing (one talks to the network, the other reads local files), so they run in parallel, each in its own temporary directory. Halotrcer itself never contacts the target; only Tokvera does.

## What happens, in order

1. **The flags are checked.** A usage error exits with 2 before anything runs.
2. **Both tools are found.** Halotrcer looks at `HALOTRCER_TOKVERA_BIN` / `HALOTRCER_SIEVANT_BIN` if set, then `PATH`, then the directories the installers use (`~/.local/bin`, `/usr/local/bin`, `/usr/bin`, Homebrew's, `~/go/bin`; on Windows `%LOCALAPPDATA%\Programs\<tool>`, Scoop's shims and winget's links). A missing tool is reported with the command that installs it: `halotrcer install tokvera`.
3. **Each tool's version is checked** against the compatibility matrix ([compatibility.md](compatibility.md)). A version outside it is a warning, printed and written into the report. The run continues.
4. **You confirm Tokvera's plan.** Halotrcer runs Tokvera's own `scan --dry-run`, which sends no load, and shows you Tokvera's plan: what it will send and its caps. Nothing reaches the target until you answer yes. With `--yes` there's no question; without a terminal and without `--yes`, the check stops and sends nothing. `--demo` needs no confirmation.
5. **Both scans run,** within `--timeout` (default 5 minutes). The time you spend reading the plan doesn't count.
   - Tokvera: `tokvera --json scan --no-cta --out <tmp> --target URL …` with your pass-through flags, and its `report.json` is read.
   - Sievant: `sievant scan --json`, with `--config` for each `--mcp-config` and `--no-project` if you gave it.
   - With `--gateway-report`, Sievant's `report --format json --no-cta` runs as well. It adds Sievant's gateway coverage and gaps over the last 30 days.
6. **The report is written** to `--out`, and a short summary goes to the terminal: grade, measured cost, security counts and the top five.

Both tools always run with their own `--no-cta`, so the report has one call to action, Halotrcer's, which `--no-cta` removes.

## Flags

### Target (Tokvera)

| Flag | Meaning |
|---|---|
| `--target URL` | the inference endpoint, OpenAI-compatible or Anthropic. Required, unless `--demo` or `--skip-tokvera` |
| `--demo` | scan Tokvera's built-in simulator instead: no endpoint, no keys. Exclusive with `--target` |
| `--kind KIND` | `openai_compatible` or `anthropic`; Tokvera detects it otherwise |
| `--model NAME` | the model, when the server lists several |
| `--api-key-env NAME` | the **name** of the environment variable holding the API key. Anything that isn't a variable name is refused, and never echoed (it may be a key pasted by mistake) |
| `--suite SUITE` | `quick`, `standard` or `deep` (default `quick`) |
| `--max-requests N`, `--max-cost USD` | Tokvera's request and cost caps |
| `--request-timeout D` | per-request timeout (Tokvera's default is 60s) |
| `--pricing FILE` | your own rate sheet, handed to Tokvera unchanged |
| `--allow-private-target` | allow a link-local or cloud-metadata address (Tokvera refuses one by default) |
| `--yes` | don't ask before sending the load |

These are passed to Tokvera as they are. Tokvera's own documentation describes the suites, the caps and the rate sheet's format.

### Agent security (Sievant)

| Flag | Meaning |
|---|---|
| `--mcp-config PATH` | scan this MCP client configuration instead of discovering them (repeatable) |
| `--no-project` | skip the MCP files in the current directory |
| `--gateway-report` | also include `sievant report`: which servers run through Sievant's gateway, and the gaps |

By default Sievant finds the configurations of the MCP clients installed on this machine, plus the project files in the current directory.

### Report

| Flag | Meaning |
|---|---|
| `--out DIR` | where the report goes (default `halotrcer-report`) |
| `--format LIST` | `html`, `pdf`, `json`, comma-separated (default all three) |
| `--monthly-requests N` | your monthly request volume: the report then shows measured cost × N as monthly spend |
| `--no-cta` | leave out the footer link to the AI Cost & Security Audit |
| `--skip-tokvera`, `--skip-sievant` | run one tool only (not both) |
| `--allow-partial` | exit 0 when one tool failed but a report was still written |
| `--timeout D` | bound on the two scans together (default `5m`) |

## Exit codes

| Code | Meaning |
|---|---|
| 0 | the report was written, and every tool that ran succeeded (or `--allow-partial`) |
| 1 | a tool failed, or no tool produced a result, or the report could not be written |
| 2 | usage: a bad or conflicting flag |

When one tool fails and the other succeeds, the report is still written. It names the failed tool and its error, and the exit code is 1 unless you gave `--allow-partial`. When neither produces a result, no report is written.

## What each tool sees

Each tool runs with a cleaned environment:
- the basics: `PATH`, `HOME`, locale, temporary directories, proxy settings, and the Windows profile variables;
- its own variables (`TOKVERA_*` or `SIEVANT_*`);
- for Tokvera, the one variable you named with `--api-key-env`.

Nothing else from your environment reaches either tool.

## Examples

```sh
# An OpenAI-compatible endpoint, the key in OPENAI_API_KEY, no prompt
halotrcer check --target https://llm.internal.example/v1 --api-key-env OPENAI_API_KEY --yes

# Show monthly spend at 2 million requests a month
halotrcer check --target https://llm.internal.example/v1 --api-key-env OPENAI_API_KEY --monthly-requests 2000000

# Only the MCP side, from one configuration file, HTML only
halotrcer check --skip-tokvera --mcp-config ~/.cursor/mcp.json --format html

# Try it with nothing set up
halotrcer check --demo
```

[report-anatomy.md](report-anatomy.md) explains what comes out.
