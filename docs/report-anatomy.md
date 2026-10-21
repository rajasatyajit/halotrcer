# Reading the report

`halotrcer check` writes three files into `--out` (default `halotrcer-report/`):

| File | What it is |
|---|---|
| `report.html` | one self-contained page: inline styles, no script, nothing loaded from the network, light and dark themes. It opens offline and can be emailed as it is |
| `report.pdf` | the same content, for sharing and printing |
| `report.json` | the same content as data, `schema_version` `halotrcer.check-report/v1.0.0` |

All three come from one model, and every string in it has passed through Halotrcer's redaction (see [Privacy](#privacy) below).

## The header

The target, when and by what the report was made, and each tool with its version and status: `ok`, `failed` or `skipped`.

Two kinds of banner can appear here:
- **A version outside the compatibility matrix:** that tool's results may be incomplete ([compatibility.md](compatibility.md)).
- **A tool that didn't complete,** with its error. The report then says it is *partial*: everything shown is real, but one side is missing.

## Executive summary

One screen, made of tiles and then a list.

The tiles:
- **Efficiency grade (Tokvera):** Tokvera's own grade, A to F, unchanged. While Tokvera's scoring is provisional, its label ("Provisional scoring") sits under the grade, with Tokvera's note in the Cost & performance section. A D or F also shows a **reliability concern**. That's a flag, not deal risk, and it moves nothing in the ranking.
- **Cost per 1,000 requests:** measured by Tokvera at its rate sheet's date, or *not computed*, with Tokvera's reason.
- **At your volume:** shown only when you passed `--monthly-requests`. It's measured cost × your number, as monthly **spend**. It is never a saving.
- **Agent security (Sievant):** critical and high counts, and how many MCP configuration files were read.
- **Deal risk:** how many findings a customer security review would flag ([scoring.md](scoring.md#deal-risk)).

Then **the top 5 findings, ranked by business impact,** across both tools. Each one shows:
- its title and its source: the tool and its own identifier, such as `Tokvera metrics_endpoint` or `Sievant AUTH001`;
- its dimension (cost, latency, operational or security), and the deal-risk tag where it applies;
- **why it ranks here**, in one line, such as "deal risk: a customer security review would flag Sievant AUTH001 (high)";
- where it is (server, field) and the tool's own fix.

[scoring.md](scoring.md) gives the full rules.

## Cost & performance (Tokvera)

Everything here is Tokvera's, from its `report.json`:

- the target and model;
- the grade, with Tokvera's provisional label and note, word for word;
- Tokvera's caps on the scan;
- requests sent, succeeded and failed, and the duration;
- a table per concurrency level: requests, failures, time to first token at p50, p95 and p99, request duration at p95, and output tokens per second at p50.

**Cost** gives cost per 1,000 requests, the basis Tokvera used, and its rate sheet with date and source. A stale rate sheet is flagged.

**Findings** gives each Tokvera finding with:
- its status (`fail` or `warn`) and its check;
- the grade points it cost and Tokvera's effort estimate;
- its evidence, why it matters, and the fix.

A `preview` finding is one Tokvera doesn't count in its grade yet.

A **Simulated** banner means the numbers come from Tokvera's built-in demo server (`--demo`) and describe no real endpoint.

Tokvera's own privacy lines close the section.

## Agent security (Sievant)

Everything here is Sievant's, from `sievant scan --json`.

First, a table of the MCP configuration files read. Each row gives the client, the file (home shown as `~`), its scope, how many servers it defines, and how many of those are not routed through Sievant's gateway.

Then the counts by severity, and each finding with:
- its severity, its rule (such as `SEC001`) and the deal-risk tag where it applies;
- the client, file, server and field it was found in;
- the evidence, **masked by Sievant** (a secret shows at most its first few characters and its length, such as `xoxb… (28 chars)`; a URL's secret part becomes `{masked:N}`), Sievant's detail, and its fix.

With `--gateway-report` there's a further part, **Gateway coverage**, from `sievant report`. It gives the coverage, the controls enforced, requests and blocked calls over the period, the gaps with their fixes, and Sievant's own disclaimer.

## 30-day fix plan

Four weeks, built from the whole ranked list, not only the top 5:

| Week | What |
|---|---|
| 1 | the top tier (critical security, deal risk at high), plus Tokvera fixes estimated at under a day |
| 2 | Tokvera's costliest cost and latency failures |
| 3 | the remaining high security findings |
| 4 | everything else |

Each item's fix is the tool's own text. Halotrcer writes no remediation advice.

## How to read this report

Two notes close the report:
- Halotrcer's ranking is provisional (scoring version 0.1.0): its tiers are judgement, not calibration.
- Halotrcer's privacy statement.

The footer has the link to book an **AI Cost & Security Audit** (https://www.halotrcer.com/book) unless you passed `--no-cta`, then the generator and schema version.

## `report.json`

Top-level fields:

| Field | Content |
|---|---|
| `schema_version`, `generator`, `generated_at`, `target` | what made the report, and when |
| `tools[]` | `name`, `version`, `status`, `error`, `compat_warning` |
| `summary` | `top[]` (the five), `grade`, `grade_provisional`, `usd_per_1k_requests`, `spend`, `security_counts`, `deal_risk` |
| `cost_performance` | Tokvera's part: grade, `scoring_version`, `provisional`, `provisional_label`, `provisional_note`, `levels[]`, `cost`, `spend`, `findings[]`, `privacy[]` |
| `agent_security` | Sievant's part: `files[]`, `counts`, `findings[]`, `notes[]`, and the gateway part when asked for |
| `fix_plan[]` | `week`, `title`, `items[]` |
| `ranking` | `scoring_version`, `provisional`, `note` |
| `privacy` | Halotrcer's privacy statement |
| `cta` | `text` and `url`, absent with `--no-cta` |

Every finding has `source`, `ref`, `title`, `dimension`, `tier`, `weight`, `deal_risk`, `why` and `week`, plus the fields of its tool. `cost_performance` is absent when Tokvera didn't run or failed, and `agent_security` when Sievant didn't.

Fields are only ever added within `v1`. A program reading the file should ignore fields it doesn't know.

## Privacy

Halotrcer contacts only the target you give it (through Tokvera) and reads only the MCP configuration files Sievant reads, on this machine. It sends no telemetry. Secrets stay masked as each tool masks them; on top of that, Halotrcer drops URL credentials and query strings, hides environment values, shows your home directory as ~ and replaces your user name with <user>.
