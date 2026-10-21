# Halotrcer

**One cost and security check for AI products.**

`halotrcer check` runs two separately released tools at once and merges what they find into one report:

- **[Tokvera](https://github.com/rajasatyajit/tokvera)** measures your LLM inference endpoint: cost per request, latency under load, cache use, reliability. It gives an Efficiency Score.
- **[Sievant](https://github.com/rajasatyajit/sievant)** reads the MCP client configurations on this machine (Claude Desktop, Claude Code, Cursor, VS Code and Windsurf) and flags exposed credentials, unauthenticated remote servers and over-broad file access.

The report puts the five findings that matter most for the business on one screen, ranked across both tools. Then it gives a section for each tool and a 30-day fix plan. Halotrcer adds no measurements of its own: every number and every fix comes from one of the two tools, and says which.

## Install

Linux and macOS:

```sh
curl -fsSL https://www.halotrcer.com/install.sh | sh
```

Windows (PowerShell):

```powershell
irm https://www.halotrcer.com/install.ps1 | iex
```

Either one installs `halotrcer`, `tokvera` and `sievant`, each from its own GitHub release. Each is checked against its release's `checksums.txt`, and against the release workflow's cosign signature when cosign is installed. Nothing runs as root. `--only tokvera` or `--only sievant` installs just one tool.

Package managers install the two tools as dependencies:

| | |
|---|---|
| Homebrew | `brew install --cask rajasatyajit/tap/halotrcer` |
| Scoop | `scoop bucket add rajasatyajit https://github.com/rajasatyajit/scoop-bucket`, then `scoop install rajasatyajit/halotrcer` |
| winget | `winget install -e --id Halotrcer.Halotrcer` |
| apt, dnf | the signed repositories in [docs/install.md](docs/install.md#the-apt-and-yum-repositories) |
| Go | `go install github.com/rajasatyajit/halotrcer/cmd/halotrcer@latest`, then `halotrcer install` |

[docs/install.md](docs/install.md) covers every channel, the Alpine packages, the container image, the Helm chart and how to check each signature yourself.

## Check

```sh
halotrcer check --target https://your-endpoint.example/v1 --api-key-env OPENAI_API_KEY
```

- `--target` is your OpenAI-compatible or Anthropic endpoint. Tokvera sends it a bounded load: it shows you the plan, with its request and cost caps, and asks before sending anything.
- `--api-key-env` takes the **name** of the variable that holds the key, never the key itself.
- Sievant only reads configuration files. It starts no server and changes nothing.

The report lands in `halotrcer-report/` as `report.html`, `report.pdf` and `report.json`. The HTML is one self-contained file that opens offline.

No endpoint yet? `halotrcer check --demo` scans Tokvera's built-in simulator instead: no endpoint and no keys.

To see what your monthly spend would be at today's measured cost, add `--monthly-requests N`. That's measured cost times your volume, with the price list's date beside it. Halotrcer never estimates savings.

[docs/check.md](docs/check.md) has every flag, the exit codes and what runs when. [docs/report-anatomy.md](docs/report-anatomy.md) walks through the report, section by section.

## Other commands

```sh
halotrcer install [tokvera|sievant|all]   # install or upgrade the tools
halotrcer doctor                          # check this installation and both tools
halotrcer update                          # a newer halotrcer, through the channel that installed it
halotrcer uninstall [--all]               # --all also removes the tools halotrcer installed
```

`halotrcer install` upgrades a tool through whichever channel already owns it (Homebrew, Scoop, winget, apt, dnf, apk or `go install`). Otherwise it downloads and verifies the release into `~/.local/bin`. It never runs sudo: for apt, dnf and apk it prints the exact commands and stops. `uninstall --all` never removes a tool you installed yourself.

## Privacy

Halotrcer contacts only the target you give it (through Tokvera) and reads only the MCP configuration files Sievant reads, on this machine. It sends no telemetry. Secrets stay masked as each tool masks them; on top of that, Halotrcer drops URL credentials and query strings, hides environment values, shows your home directory as ~ and replaces your user name with <user>.

That sentence is about `halotrcer check`. `install`, `update` and `update --check` also contact GitHub, to find and download releases. When Homebrew, Scoop, winget or `go install` owns a tool, Halotrcer hands the upgrade to that program instead.

## How findings are ranked

Halotrcer only orders what the tools measured. Critical security findings, and the findings a customer security review would flag ("deal risk"), come first. Next come Tokvera's costliest cost and latency failures, then the rest. The model is provisional (version 0.1.0) and documented rule by rule in [docs/scoring.md](docs/scoring.md). Tokvera's own "Provisional scoring" label is carried into the report unchanged.

## Compatibility

This release reads Tokvera and Sievant `>= 0.2.0, < 0.3.0`. `halotrcer doctor` and every report warn about a tool outside that range. It refuses a report whose schema has a major version it doesn't know, rather than misreading it. See [docs/compatibility.md](docs/compatibility.md).

## Want this fixed?

The report ends with a link to book an **AI Cost & Security Audit**: https://www.halotrcer.com/book. Pass `--no-cta` to leave it out.

## More

- [docs/faq.md](docs/faq.md): common questions
- [docs/roadmap.md](docs/roadmap.md): what is not built yet
- [SECURITY.md](SECURITY.md): reporting a vulnerability, and what a release proves
- [docs/development.md](docs/development.md) and [docs/releasing.md](docs/releasing.md): for contributors

Halotrcer is licensed under Apache-2.0 ([LICENSE](LICENSE), [NOTICE](NOTICE)). Tokvera and Sievant are separate projects with their own licences and releases; Halotrcer runs their released binaries and reads only their documented JSON.
