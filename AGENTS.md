# AGENTS.md

This file provides guidance to AI coding agents when working with code in this repository.

## Project Overview

`sysone` is a Go workspace implementing a type-safe client module and CLI for the llama.cpp
**System One decision-model API** (`POST /v1/systemone`, TypeSafe/Jev-compatible).

- **Go module `sysone`** (`github.com/tsai-danny/sysone`) — stdlib-only client library with a staged Step Builder API
- **CLI `sysone`** — agent-friendly command-line tool built on the module (cobra is the only external dependency)

The authoritative API contract lives in `M:\golang\systemone-chat\docs\systemone_api.md`
(sibling repo). The product requirements are in [PRD.md](./PRD.md). Read both before
changing request/response handling.

## Repository Layout

```
sysone/
  go.mod                  module github.com/tsai-danny/sysone (Go >= 1.22)
  *.go                    client, step builder, contract types, errors (package sysone)
  cmd/sysone/             CLI entrypoint (cobra)
  internal/cli/           CLI command implementations (thin wrappers over the module)
  testdata/               httptest fixtures mirroring systemone_api.md examples
  PRD.md                  product requirements (source of truth for behavior)
```

## Build, Test, and Lint Commands

```powershell
go build ./...                    # compile check only (binaries are discarded in multi-package mode)
go build -o bin/sysone.exe ./cmd/sysone   # produce the CLI executable
go install ./cmd/sysone           # install CLI to GOPATH\bin
go test ./...                     # run all tests
go test -run TestBuilder ./...    # run a single test / package subset
go vet ./...                      # vet (CI gate)
gofmt -l .                        # must print nothing (formatting gate)
go test -cover ./...              # coverage check
go install ./cmd/sysone           # install CLI locally
```

There is no Makefile; use plain `go` commands. All commands run from the repo root.

## Testing

- Backend is simulated with `net/http/httptest` — tests must not require a live llama-server.
- Contract tests must cover every documented error trigger: missing/empty `questions`,
  unknown `type`, empty `choice` criteria, `score` criteria with <2 or >10 levels,
  non-data-URL images, plus HTTP 400 / 501 / 503 mapping.
- Builder tests verify stage enforcement, pointer binding, and threshold semantics.
- Example tests (`Example*` in `example_test.go`) double as pkg.go.dev docs and must pass.

## Code Style

- **Idiomatic Go**: `gofmt`/`go vet` clean; table-driven tests; errors wrapped with `%w`.
- **stdlib only** in the core module — do not add third-party dependencies to the root module.
  The CLI may use `github.com/spf13/cobra` (and nothing else without approval).
- **Step Builder pattern**: stage interfaces (`StateStep` → `QuestionStep`) enforce
  state-first ordering at compile time. Never weaken a stage interface or merge stages;
  new question types follow the existing `Add*` shape (id, instructions, criteria, target pointer).
- Go methods cannot take their own type parameters — output binding uses target pointers
  (`*bool`, `*string`, `*float64`), not generics. Keep this convention.
- Local validation (non-empty state, non-empty questions, score 2–10 levels, non-empty
  choice criteria) happens **before** sending the request and returns wrapped `ErrInvalidRequest`.
- Remote errors surface as `*APIError` (OAI shape: code/message/type) usable with `errors.As`.
  Every error message includes an actionable `Hint:` line.
- Context: every network call takes `context.Context` and honors client timeout.
- API keys and image payloads must never appear in logs or error messages.

## CLI Conventions (agent-friendly contract — do not break casually)

- Subcommands: `noul`, `choice`, `score`, `batch`, `ask`, `health`, `models`, `props`, `agent-help`.
  (`ask` is the low-level escape hatch taking raw questions JSON, backed by `Client.Ask`.)
- stdout carries **only** JSON data; stderr carries **only** diagnostics. No colors, no prompts, no interactivity.
- Default output is compact JSON; `--pretty` indents, `--quiet` emits answer values only.
- Exit codes are a stable contract: `0` all passed, `1` passed=false on some answer,
  `2` argument/structure error, `3` connection/503, `4` backend 400/501.
- Missing/invalid flags must print a minimal working command example on stderr (not just usage).
- `sysone agent-help` prints the condensed machine-readable spec; keep it in sync with flags.

## Domain Cheat Sheet

- Question types: `choice` (criteria = `{option: description|null}`, non-empty),
  `score` (criteria = 2–10 level descriptions, **lowest → highest**),
  `noul` (optional `{"true": desc, "false": desc}`; answer = P(true)).
- `score` answer is an **expected index** (fractional), not a level label.
- `usage.output_tokens` is always 0; streaming is never used.
- `model` field is only meaningful in router mode; single-model servers ignore it.
- Images must be data URLs on the wire; the module converts local paths automatically.
- Probabilities are model-temperature-scaled and not guaranteed calibrated — never
  hardcode calibration logic; expose raw probabilities and confidence as-is.

## Environment for Manual Testing

Env vars: `SYSTEMONE_URL`, `SYSTEMONE_API_KEY`, `SYSTEMONE_MODEL` (CLI flags override).
Backend setup and model selection for manual tests: see [README.md](./README.md)
("Run a backend"). Tests themselves must never require a live server.

## Reference Implementations

Design precedents to mirror (do not vendor): Ent (staged builders), openai-go /
google.golang.org/genai (client conventions), `gh` CLI and Simon Willison's `llm`
(agent-friendly CLI), jevify / llamacpp-jev (flag naming).
