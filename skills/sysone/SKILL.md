---
name: sysone
description: Ask a llama.cpp System One decision model typed questions (choice / score / noul) about a state, from the shell. Use when routing requests, triaging tickets, gating agent steps, scoring risk/completeness, or yes/no checks — the model returns structured answers with probabilities, not generated text.
---

# sysone — decision-model CLI

`sysone` answers typed questions (choice / score / noul) about a *state* and
returns structured answers with probabilities. stdout is JSON only; stderr is
diagnostics only.

Discover the full contract from the CLI itself — do not guess flags:

```bash
sysone agent-help    # machine-readable spec: commands, flags, required args,
                     # envelope shape, exit codes, examples
sysone <cmd> --help  # per-command flags ([required] marks mandatory ones)
```

## Branching on exit codes

| code | meaning |
|---|---|
| 0 | every answer passed its threshold |
| 1 | valid answers, at least one **below threshold** — a *result*, not an error |
| 2 | usage error (fix the command per `--help`) |
| 3 | transport: connection failure / 5xx; 503 = model still loading, retry after a few seconds (`sysone health` to check) |
| 4 | contract error: backend 400/501, local request validation |

## Threshold guidance (not in help)

`--threshold` decides when exit 1 fires; pick it by how you act on the answer:

- **confidence-routing** (choice): threshold = the confidence below which you
  refuse to auto-route; exit 1 → fall back to a human/default queue (0.6–0.8).
- **companion-noul**: guard a choice with a safety noul in the same `batch`
  (one request, both answers); act only when both pass.
- **noul-screen** (yes/no gate): high-stakes "act?" checks use 0.8–0.9 so
  exit 0 means "clearly yes"; treat exit 1 as "no / ask".
