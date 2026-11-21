---
name: sysone
description: Ask a llama.cpp System One decision model typed questions (choice / score / noul) about a state, from the shell. Use when routing requests, triaging tickets, gating agent steps, scoring risk/completeness, or yes/no checks — the model returns structured answers with probabilities, not generated text.
---

# sysone — decision-model CLI

`sysone` asks typed questions about a *state* using a System One decision model
served by llama.cpp (`POST /v1/systemone`). One forward pass, no token
generation. Every answer is one of the options you supplied, with probabilities.

Run `sysone agent-help` for the full machine-readable contract (JSON).

## Output contract (stable)

- **stdout is JSON only** (compact; `--pretty` indents). stderr is diagnostics only.
- Question commands emit an envelope:
  `{"model":str,"answers":{id:{id,type,passed,...}},"usage":{input_tokens,output_tokens},"latency_ms":int}`
- Answer fields by type:
  - `choice` → `choice`, `probabilities` (option→p), `confidence`
  - `score` → `score` (expected level index, fractional), `legend`, `probabilities`, `confidence`
  - `noul` → `noul` (P(true) in [0,1])
- `--quiet` prints bare values: one line per answer (`id value`; single-question
  commands print the bare value alone) — pipe-friendly.

## Exit codes (branch on these)

| code | meaning |
|---|---|
| 0 | success, every answer passed its threshold |
| 1 | valid answers, at least one answer **below threshold** |
| 2 | usage error: bad flag, missing argument, unknown command |
| 3 | transport: connection failure, server 5xx, 503 model loading |
| 4 | contract error: backend 400/501, local request validation |

Exit 1 is a *result*, not an error — the model answered, it just said "no" /
low confidence. Exit 3 on a fresh server usually means the model is still
loading: retry after a few seconds and check `sysone health`.

## Commands

```bash
# yes/no — noul is P(true)
sysone noul --state "OOMKilled in pod log" --q "Should the container restart?" --threshold 0.8

# multiple choice — options are "key:desc|key:desc" (desc optional)
sysone choice --state @ticket.txt --q "Issue category" \
  --options "billing:payments|tech:bugs|returns:refunds" --quiet

# graded score — 2–10 levels ordered lowest → highest
sysone score --state @ctx.json --q "Risk level" --scale "low,medium,high,critical"

# several questions in ONE request (specs repeatable)
sysone batch --state @ctx.json \
  --noul  "is_urgent:Is it urgent?" \
  --choice "route:Team:frontend|backend|sre" \
  --score "conf:Completeness:low:mid:high"

# raw escape hatch — questions as JSON {id:{type,instructions,criteria}}
sysone ask --state @ctx.json --questions @questions.json

# probes
sysone health    # {"status":"ok"}
sysone models    # {"models":[...]}
sysone props     # {"props":{...},"vision":bool}
```

## Common flags

| flag | meaning |
|---|---|
| `--url` | endpoint (env `SYSTEMONE_URL`; default `http://localhost:8080/v1/systemone`) |
| `--api-key` | bearer token (env `SYSTEMONE_API_KEY`; alias `--key`) |
| `--model` | model id, router mode only (env `SYSTEMONE_MODEL`) |
| `--state` | text, `@file` (suffix `.json` parsed as JSON), or `-` for stdin |
| `--image` | local path or data URL, repeatable (needs a vision server — check `sysone props`) |
| `--threshold` | pass threshold (0,1], default 0.5. noul: P(true) ≥ t; choice: min confidence |
| `--timeout` | Go duration, default 30s |
| `--pretty` / `--quiet` | indent JSON / bare values only |

## Threshold guidance (TypeSafe patterns)

Pick `--threshold` by how you will act on the answer:

- **confidence-routing** (choice): set `--threshold` to the confidence below
  which you refuse to auto-route. Low confidence → the model is guessing;
  exit 1 tells you to fall back to a human/default queue. Typical 0.6–0.8.
- **companion-noul** (guard a choice): add a noul that checks the choice is
  safe to act on (e.g. "Is this refund within policy?"). Gate the action on
  BOTH passing — batch them so one request answers both.
- **noul-screen** (yes/no gate): for high-stakes "act?" checks use a high
  threshold (0.8–0.9) so exit 0 means "clearly yes"; treat exit 1 as "no /
  ask". For cheap/soft signals the 0.5 default is fine.

## Parsing from a script

```bash
# Route by choice, fall back when confidence is low (exit 1):
team=$(sysone choice --state "$ticket" --q "Team" \
        --options "billing:payments|tech:bugs|sre:incidents" \
        --threshold 0.7 --quiet) || team="triage"
```

```bash
# Read the full envelope with jq:
sysone batch --state @ctx.json --noul "urgent:Is it urgent?" --pretty \
  | jq -r '.answers.urgent.noul'
```

## Library (Go)

Prefer the Step Builder — it enforces state → questions → execute at compile
time and binds answers into your variables:

```go
client, _ := sysone.NewClient(sysone.Config{})
var route string
var urgent bool
ans, err := client.Decide().
    WithTextState(state).
    AddChoice("route", "Which team?", map[string]string{"billing": "payments", "technical": "bugs"}, &route).
    AddNoul("urgent", "Is this urgent?", 0.8, &urgent).
    Execute(ctx)
// ans.Passed() reports the combined judgement; ans.Choice("route"), ans.Noul("urgent")
```
