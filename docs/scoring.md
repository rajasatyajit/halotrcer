# How Halotrcer ranks findings

> **Provisional.** Halotrcer's ranking model is at version 0.1.0 (`scoring_version`
> in the combined JSON). Its tiers are judgement, not calibrated against real
> results, and may change before 1.0.0.

`halotrcer check` merges Tokvera's and Sievant's findings into one list. The executive summary shows the first five, and the 30-day fix plan is built from the whole list.

The model only **orders** what the two tools measured:
- It computes no new measurement.
- It assigns no dollar value to a finding.
- It never estimates savings.

Each tool's own grading is left exactly as it reported it: Tokvera's Efficiency Score and its provisional label, and Sievant's severities.

The code is `internal/scoring`, and `internal/scoring/scoring_test.go` pins every rule below.

## Inputs

Only these documented fields are read. Every item in the combined report carries them unchanged, with its source tool.

| Tool | Fields |
|---|---|
| Tokvera (`report.json`) | `findings[].check_id`, `status` (`warn` or `fail`), `impact` (grade points lost), `rank`, `effort`, `preview`, `suggested_fix`; and `checks[].pillar` for the finding's check |
| Sievant (`scan --json`) | `findings[].rule`, `severity` (`critical`, `high`, `medium`, `low`, `info`), `fix` |

## Dimension

| Source | Dimension |
|---|---|
| Tokvera check in pillar `cost_and_cache` | cost |
| Tokvera check in pillar `latency`, `capacity` or `reliability` | latency |
| Tokvera check in pillar `observability` (or any other) | operational |
| Every Sievant finding | security |

## Weight

Weight orders items within a tier.

- **Tokvera:** `impact` divided by the largest `impact` in the report. A `warn` counts half as much as a `fail` with the same impact.
- **Sievant:** critical 1.0, high 0.75, medium 0.5, low 0.25, info 0.

## Deal risk

Deal risk is a tag, not a score. It applies to Sievant findings with rules that a customer security review would flag:

| Rule | Meaning (Sievant's) |
|---|---|
| SEC001, SEC002, SEC003, SEC004 | a credential, secret value, URL password or capability URL in a configuration file |
| AUTH001 | a remote server with no credential |
| NET001 | a remote server over plain HTTP |
| PERM001, PERM002 | the filesystem root or the whole home directory given to a server |

Only those rules carry it. Tokvera findings never do.

## Reliability concern

When Tokvera grades the endpoint **D** or **F**, the report shows a *reliability concern* beside the grade and in the summary. It is not deal risk and does not move any item.

## Tiers

Items are ordered by tier, then by weight.

| Tier | Items |
|---|---|
| 1 | Sievant critical findings; deal-risk findings at high severity |
| 2 | Tokvera `fail` on cost or latency with `impact` at or above the median impact of the report's Tokvera findings |
| 3 | the remaining Sievant high findings |
| 4 | the remaining Tokvera `fail` findings |
| 5 | everything else: warnings, medium and lower security findings, and Tokvera preview checks (which Tokvera does not count in its grade) |

Ties are broken deterministically:
1. Tokvera items come before Sievant items.
2. Tokvera items are then ordered by Tokvera's `rank`.
3. Sievant items are then ordered by rule, file, server and field.

Each item states why it got its tier, such as "deal risk: a customer security review would flag Sievant AUTH001 (high)".

## 30-day fix plan

| Week | Items |
|---|---|
| 1 | tier 1, plus any Tokvera fix whose `effort` upper bound is under one day (for example "0.5 days") |
| 2 | tier 2 |
| 3 | tier 3 |
| 4 | the rest |

The fix text is the tool's own: Tokvera's `suggested_fix` or Sievant's `fix`. Halotrcer writes no remediation advice of its own.

## Money

A dollar figure appears in exactly one case: Tokvera computed `cost.usd_per_1k_requests`, and you passed `--monthly-requests N`.

- The report then shows *measured cost per 1,000 requests × your volume*, with Tokvera's rate-sheet date (`pricing_as_of`), source and estimate flag.
- `--pricing FILE` is handed to Tokvera unchanged. Halotrcer never reads prices itself.
- Without both inputs the report shows "not computed", with Tokvera's reason.
- There is no savings figure anywhere, because neither tool measures what a fix would save.
