# CLAUDE.md

Working rules for coding agents in this repository. PLAN.md is the source of truth for scope, milestones and open decisions.

## What this is
- Repository `rajasatyajit/halotrcer`, module `github.com/rajasatyajit/halotrcer`, binary `./cmd/halotrcer`. The local folder is `~/workspace/halotrcer-installer`.
- Halotrcer bundles two separately released tools and merges their results into one report:
  - **Tokvera**: `~/workspace/tokvera`, `github.com/rajasatyajit/tokvera`.
  - **Sievant**: `~/workspace/aethergate`, `github.com/rajasatyajit/sievant`.
- Never copy or modify their code. Depend only on their released binaries and their documented JSON.
- Other Claude sessions own those repositories:
  - read them; never write to them;
  - never run a git command there that changes a branch, the index or tags;
  - never build inside them (if a build is approved, the output goes to the scratch directory).

## Commands
```sh
make build       # bin/halotrcer (CGO_ENABLED=0, -trimpath, stock toolchain from go.mod)
make gates       # vet (gofmt first), race tests, lint, vuln (toolchain-check first), secrets, actionlint, shellcheck
go test ./internal/report -update   # rewrite the report goldens; the diff is the review
```
- Layout:
  - `cmd/halotrcer` holds the CLI.
  - `internal/compat` holds the matrix and the schema gates.
  - `internal/ingest` decodes the tools' JSON.
  - `internal/scoring` does the ranking (docs/scoring.md).
  - `internal/redact` is the scrubber.
  - `internal/report` holds the model and the HTML, PDF and JSON renderers.
  - `internal/pdf` is the PDF writer.
  - `internal/tools` runs the binaries.
  - `internal/check`, `internal/install` and `internal/doctor` implement the commands.
- `testdata/fixtures/` holds the tools' real outputs from pinned commits (its README records how each was made).

## Facts about the tools
- Read every version, flag, field and name from their repositories, and cite the file. Never invent one.
- When a fact can't be found, write VERIFY and ask.
- Read only documented JSON fields and ignore unknown ones:
  - Tokvera: check the `schema_version` prefix `tokvera.scan-report/v1.`.
  - Sievant: check `report_version == 1`.
  - Warn on a version outside the embedded compatibility matrix.
- Never fabricate numbers, prices, customers or claims in code, docs or reports.
- Savings come only from measured data and prices the user supplies.

## Git
- One branch per milestone, one commit per logical change.
- Merge to `main` only when every gate below passes.
- Never push, tag, publish or delete branches. The owner pushes.
- After every push the owner makes, check CI for that commit (`gh run list --commit <sha>`) and report it before anything else. A red `main` blocks all other work.
- Commit messages end with the attribution line from the session's system reminder.

## Gates (run all of them immediately before every report; never claim "all gates pass" otherwise)
- `go vet ./...`
- `go test -race -count=1 ./...`
- golangci-lint at the pinned version: 0 issues.
- govulncheck at the pinned version, under the exact toolchain from go.mod (`GOTOOLCHAIN=<toolchain line>`).
  - Refuse a non-release Go build, such as this machine's `go1.27.1-X:nodwarf5` or any `X:` or distro build. `make toolchain-check` enforces this.
- `gitleaks` (detect and dir, `--redact`).
- actionlint at the pinned version.
- shellcheck `-s sh` at the CI version (digest-pinned image, 0.9.0) and at 0.11.0.
- `make gates` runs them all. Report each one's result, not a summary of them.

## Go
- Static binary: `CGO_ENABLED=0`, `-trimpath`. The toolchain is pinned in go.mod.
- No telemetry. Network access is limited to:
  - the scanned target;
  - release downloads during `install` and `update`;
  - `doctor --check-update`.
- stdout carries the command's result; logs and progress go to stderr.
- Exit codes are fixed once documented: 0 ok, 1 runtime failure, 2 usage. A new code needs an owner decision.
- Never run sudo or escalate privileges on the user's behalf. For apt, dnf and apk, print the exact commands and stop (PLAN.md D5).
- Apache-2.0 LICENSE and NOTICE. No dependency without a stated reason in the commit.

## Privacy and redaction
- Preserve both tools' masking.
- Never print, log or write:
  - URLs with userinfo, query, tokens or keys;
  - environment values (names only);
  - the user name.
- Paths under home are shown as `~`.
- Every renderer goes through `internal/redact`, and canary tests cover every output.

## CI
- GitHub Actions are pinned by full commit SHA, with the release in a comment, read from the action's releases page and never from memory.
- `permissions: contents: read` at the top level (`release.yml`: `{}`, plus per-job grants). Every checkout uses `persist-credentials: false`.
- Every downloaded tool is checksum-verified (`echo "<sha>  <file>" | sha256sum -c -`).
- Go comes from go.mod's toolchain line, with `GOTOOLCHAIN=local`.
- Jobs run on `ubuntu-24.04`. `ubuntu-latest` appears only in the smoke matrix, deliberately.
- An action's inputs reach scripts only through `env:`, never as `${{ }}` inside `run:`.

## Release (milestones 2 to 4)
- GoReleaser v2. Check every key against `goreleaser jsonschema` for the pinned version, and every channel's dependency syntax against that channel's own documentation.
- Release candidates (`-rc.N`) never go to Homebrew, Scoop, winget or the package repositories (`skip_upload: auto`, `!contains(tag,'-')`).
- `release.yml` has two phases: draft, then publish once the owner publishes the draft.
- Never `--skip=sign` in a real release.
- Shared package repositories: follow `~/workspace/aethergate/PLAN.md` §6l exactly.
  - `repo-meta.env` is read from `gh-pages`, validated and never executed.
  - Index every package and architecture in the pool.
  - On a push race, fetch, rebuild and re-sign, then retry: 3 attempts with 5 s and 10 s backoff.
  - A Halotrcer publish must leave Tokvera's and Sievant's packages and `repo-meta.env` byte for byte.
- Only public keys are committed (`packaging/keys/`); private key material never enters git.

## PowerShell
- Every native call goes through `Invoke-HalotrcerNative`, which is Process-based and safe on Windows PowerShell 5.1 when a tool writes to stderr.
- Every script-scope variable is prefixed `$script:Halotrcer*`, so dot-sourcing cannot collide (the `$Repo` bug).
- Preferences are set inside the entry function.
- Exit codes are explicit: `exit` only when `$PSCommandPath` is set, otherwise `$global:LASTEXITCODE`. Never leave a stale `$LASTEXITCODE` behind.
- Test under pwsh 7.4 and 7.1.5.

## Shared machine
- Other sessions run here. Before anything heavy, say what it is and wait for the owner's go-ahead. That includes:
  - snapshot builds;
  - `smoke-local`;
  - Docker or containers;
  - kind;
  - building either tool.

## Milestone reports
- Each milestone report ends with:
  - the acceptance table;
  - each gate's fresh result;
  - the manual steps and secrets the owner must provide.
- No tags or publishing.
