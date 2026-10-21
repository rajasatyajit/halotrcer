# FAQ

## Is Halotrcer a third scanner?

No. Halotrcer measures nothing itself. It installs [Tokvera](https://github.com/rajasatyajit/tokvera) and [Sievant](https://github.com/rajasatyajit/sievant), runs both, and merges their results into one report. Every number and every fix in the report comes from one of the two tools and names which. What Halotrcer adds is the ranking across them, the 30-day plan and the one-screen summary ([scoring.md](scoring.md)).

## What does it send, and where?

Halotrcer contacts only the target you give it (through Tokvera) and reads only the MCP configuration files Sievant reads, on this machine. It sends no telemetry. Secrets stay masked as each tool masks them; on top of that, Halotrcer drops URL credentials and query strings, hides environment values, shows your home directory as ~ and replaces your user name with <user>.

That covers `halotrcer check`. Installing and updating (`install`, `update`, `update --check`) contact GitHub to find and download releases, or hand the work to the package manager that owns a tool.

## Will it hammer my endpoint?

Tokvera sends a bounded load: the `quick` suite by default, within its request and cost caps (`--max-requests`, `--max-cost`). Before anything is sent, `halotrcer check` shows Tokvera's own plan and asks. Without a terminal it sends nothing unless you pass `--yes`. Use a staging endpoint if you have one.

## Does it change my MCP configuration?

No. `halotrcer check` runs `sievant scan`, which reads configuration files and starts no server. Routing your servers through Sievant's gateway (`sievant wrap`) is a separate step you'd run yourself.

## Where does my API key go?

To Tokvera only, as an environment variable. You pass its **name** with `--api-key-env OPENAI_API_KEY`; Halotrcer refuses anything that isn't a variable name, and never prints the value. Tokvera gets that one variable plus the basics (`PATH`, `HOME`, locale, proxy settings), not the rest of your environment.

## Is the dollar figure what I'd save?

No. Halotrcer never estimates savings. The report shows what Tokvera measured: the cost per 1,000 requests at its rate sheet's date. With `--monthly-requests N` it also shows that cost times your volume, labelled as spend. You can give Tokvera your own prices with `--pricing FILE`.

## Why does the grade say "Provisional scoring"?

That label is Tokvera's. Its weights and thresholds haven't been calibrated against real pilot scans yet, and Halotrcer carries the label and note through unchanged. Halotrcer's own ranking is labelled provisional too (scoring version 0.1.0).

## What is "deal risk"?

A tag on the Sievant findings a customer's security review would flag:
- credentials or secrets in a configuration file;
- remote servers with no credential, or over plain HTTP;
- a server given the filesystem root or your whole home directory.

These rank first. A poor Tokvera latency grade is shown as a *reliability concern* instead, because security reviews don't ask about latency grades. [scoring.md](scoring.md#deal-risk) lists the rules.

## I only care about one side.

`--skip-sievant` runs only Tokvera; `--skip-tokvera` runs only Sievant. `install.sh --only tokvera` (or `sievant`) installs just one tool.

## No endpoint to hand?

`halotrcer check --demo` runs Tokvera against its built-in simulator: no endpoint, no key and no confirmation. The report says clearly that the numbers are simulated.

## Does it need root?

No. Halotrcer never runs sudo. With no package manager, it installs into `~/.local/bin`. When apt, dnf or apk own a tool, it prints the exact commands for you to run and stops.

## I already installed Tokvera or Sievant myself.

Halotrcer uses the copy it finds on `PATH`, or in the usual install directories. `halotrcer install` upgrades a tool through the channel that already owns it. `halotrcer uninstall --all` removes only the tools Halotrcer itself installed; yours stay.

## My tool version is newer than Halotrcer supports.

The check still runs, with a warning in the terminal and the report. A report schema with a major version Halotrcer doesn't know is refused rather than misread. See [compatibility.md](compatibility.md).

## Can I run it in CI or a container?

Yes. In CI, pass `--yes` (and `--allow-partial` if a missing side shouldn't fail the job). The exit codes are in [check.md](check.md#exit-codes). The image `ghcr.io/rajasatyajit/halotrcer` carries all three binaries, distroless and non-root. Inside it, `halotrcer doctor` treats the tools as part of the image.

## Who do I talk to about fixing what it found?

The report's footer links to the **AI Cost & Security Audit**: https://www.halotrcer.com/book. `--no-cta` removes the link.
