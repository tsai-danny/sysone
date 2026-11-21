# sysone

Type-safe Go client module and agent-friendly CLI for the llama.cpp
**System One decision-model API** (`POST /v1/systemone`, TypeSafe/Jev-compatible).

Decision models answer *typed questions* about a *state* in a single forward pass —
no token generation, no streaming. You get structured answers with probabilities:

| Question type | You give | You get |
|---|---|---|
| `choice` | options + descriptions | winning option, per-option probabilities, confidence |
| `score` | 2–10 level descriptions (low → high) | expected level index (fractional), probabilities, confidence |
| `noul` | optional true/false descriptions | P(true) |

Typical uses: request routing, content moderation, gating agent steps, triage.

- Requirements & design: [PRD.md](./PRD.md)
- API contract: `M:\golang\systemone-chat\docs\systemone_api.md` (sibling repo)
- Guidance for AI coding agents: [AGENTS.md](./AGENTS.md)

## Install

```powershell
go install github.com/<org>/sysone/cmd/sysone@latest
```

Use the library:

```powershell
go get github.com/<org>/systemone
```

## Run a backend

Start a decision model with llama.cpp (v1.0.0+):

```powershell
llama serve -hf ggml-org/Julia-1-GGUF    # serves http://localhost:8080/v1/systemone
```

Model notes:

| Model | Size | Languages | Vision |
|---|---|---|---|
| Julia-1 | 144M | 50+ | ❌ |
| Laya | 421M | English | ❌ |
| Kev-4B | 4B | English | ❌ |
| lev | 4B | English | ❌ |
| OpenJev | 27B | en/de/fr/hi/zh/ja | ✅ |

For Chinese-language tests use **Julia-1** or **OpenJev** (Laya / Kev-4B / lev are English-only).

## CLI quick start

```bash
sysone noul   --state "OOMKilled in log" --q "需要重啟容器?" --threshold 0.8
sysone choice --state @ticket.txt --q "問題分類" --options "帳務:payments|技術:bugs|退貨:refunds"
sysone score  --state @ctx.json --q "風險等級" --scale "低,中,高,極高"
sysone batch  --state @ctx.json --noul "is_urgent:是否緊急?" --choice "route:分派:前端|後端|SRE"
sysone health
sysone agent-help    # condensed machine-readable spec for AI agents
```

stdout is compact JSON only; exit codes: `0` all passed, `1` below threshold,
`2` bad arguments, `3` connection/503, `4` backend 400/501.

## Library quick start

```go
client, _ := systemone.NewClient(systemone.Config{})

var route string
var urgent bool
ans, err := client.Decide().
    WithTextState("Customer message: I was charged twice for my order.").
    AddChoice("route", "Which team should handle this?", map[string]string{
        "billing": "payments, refunds", "technical": "bugs, errors",
    }, &route).
    AddNoul("urgent", "Is this urgent?", 0.8, &urgent).
    Execute(ctx)
```

## Configuration

| Env var | Purpose | CLI override |
|---|---|---|
| `SYSTEMONE_URL` | endpoint (default `http://localhost:8080/v1/systemone`) | `--url` |
| `SYSTEMONE_API_KEY` | Bearer token (when server uses `--api-key`) | `--api-key` |
| `SYSTEMONE_MODEL` | model id (router mode only) | `--model` |

## License

Apache License 2.0.
