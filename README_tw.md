# sysone（繁體中文）

[English](./README.md)

llama.cpp **System One 決策模型 API**（`POST /v1/systemone`，TypeSafe/Jev 相容）的
型別安全 Go client module 與 Agent 友善 CLI。

決策模型在**單次前向推論**中回答關於*狀態（state）*的*型別化問題*——
不生成 token、不串流。你得到的是附帶機率的結構化答案：

| 問題型別 | 你提供 | 你得到 |
|---|---|---|
| `choice` | 選項 + 描述 | 勝出選項、各選項機率、confidence |
| `score` | 2–10 個等級描述（由低到高） | 期望等級索引（小數）、機率、confidence |
| `noul` | 選填 true/false 描述 | P(true) |

典型用途：請求分派、內容審核、Agent 步驟閘控、分流（triage）。

- 需求與設計：[PRD.md](./PRD.md)
- API 契約：`M:\golang\systemone-chat\docs\systemone_api.md`（sibling repo）
- 給 AI coding agent 的指引：[AGENTS.md](./AGENTS.md)

## 安裝

```powershell
go install github.com/tsai-danny/sysone/cmd/sysone@latest
```

作為函式庫使用：

```powershell
go get github.com/tsai-danny/sysone
```

## 啟動後端

用 llama.cpp（v1.0.0+）啟動決策模型：

```powershell
llama serve -hf ggml-org/Julia-1-GGUF    # 服務 http://localhost:8080/v1/systemone
```

模型一覽：

| 模型 | 尺寸 | 語言 | Vision |
|---|---|---|---|
| Julia-1 | 144M | 50+ | ❌ |
| Laya | 421M | 英文 | ❌ |
| Kev-4B | 4B | 英文 | ❌ |
| lev | 4B | 英文 | ❌ |
| OpenJev | 27B | en/de/fr/hi/zh/ja | ✅ |

中文測試請用 **Julia-1** 或 **OpenJev**（Laya / Kev-4B / lev 僅支援英文）。

## CLI 快速上手

```bash
sysone noul   --state "OOMKilled in log" --q "需要重啟容器?" --threshold 0.8
sysone choice --state @ticket.txt --q "問題分類" --options "帳務:payments|技術:bugs|退貨:refunds"
sysone score  --state @ctx.json --q "風險等級" --scale "低,中,高,極高"
sysone batch  --state @ctx.json --noul "is_urgent:是否緊急?" --choice "route:分派:前端|後端|SRE"
sysone health
sysone agent-help    # 給 AI agent 的濃縮機器可讀規格
```

stdout 只輸出緊湊 JSON；exit codes：`0` 全數通過、`1` 有答案低於門檻、
`2` 參數錯誤、`3` 連線失敗/503、`4` 後端 400/501。

要用非預設伺服器請指定 `--url`（或環境變數 `SYSTEMONE_URL`）。`--model` 與
`--api-key` 是**選填**：`--model` 只在 router 模式（多模型伺服器）需要，
`--api-key` 只在伺服器以 `--api-key` 啟動時需要。本機單模型伺服器兩者都不需要。

每個問題指令在 `--help` 中把必要旗標標示 `[required]`（缺少 → exit `2`）：
`noul`/`score` 需要 `--state --q`；`choice` 另需 `--options`；
`batch` 需要 `--state` 加至少一個問題旗標；`ask` 需要 `--state --questions`。
其餘皆為選填。

AI agent 請使用 [skills/sysone/](skills/sysone/) 的 Agent skill
（agentskills.io 格式）——它教你何時該用這個 CLI，完整契約則查
`sysone agent-help` / `sysone <cmd> --help`。

### Threshold 指引（TypeSafe patterns）

依「你如何依據答案行動」來選 `--threshold`：

- **confidence-routing**（choice）：把 `--threshold` 設為「低於此值就拒絕
  自動分派」的 confidence；exit `1` 代表「改走人工/預設佇列」。常用 0.6–0.8。
- **companion-noul**：在同一個 `batch` 用安全 noul 守護 choice
  （例：「這筆退款符合政策嗎？」），兩者都通過才行動。
- **noul-screen**（是非閘門）：高風險的「要做嗎？」檢查用 0.8–0.9，
  讓 exit `0` 代表「明確 yes」；exit `1` 視為「no / 轉人工」。
  軟性訊號用預設 0.5 即可。

## 函式庫快速上手

```go
client, _ := sysone.NewClient(sysone.Config{})

ans, err := client.Decide().
    WithTextState("客戶訊息：我的訂單被重複扣款了。").
    AddChoice(sysone.ChoiceQ{
        ID: "route", Instructions: "Which team should handle this?",
        Options: []sysone.Option{
            {Key: "billing", Description: "payments, refunds"},
            {Key: "technical", Description: "bugs, errors"},
        },
    }).
    AddNoul(sysone.NoulQ{ID: "urgent", Instructions: "Is this urgent?", Threshold: 0.8}).
    AddScore(sysone.ScoreQ{
        ID: "severity", Instructions: "How severe is this?",
        Levels: []string{"minor", "moderate", "major", "critical"}, // 由低到高，2–10
    }).
    Execute(ctx)
// 讀答案：ans.Choice("route")、ans.Noul("urgent")、ans.Score("severity")、
//         ans.Answers["urgent"].Passed
```

問題以具名欄位結構字面量（`ChoiceQ` / `ScoreQ` / `NoulQ`）建立，呼叫處每個值
都自帶標籤；答案一律從回傳的 `Response` 讀取。階段順序
（`Decide() → 狀態 → 問題 → Execute`）由編譯期介面強制。

## 設定

| 環境變數 | 用途 | CLI 旗標 |
|---|---|---|
| `SYSTEMONE_URL` | endpoint（預設 `http://localhost:8080/v1/systemone`） | `--url` |
| `SYSTEMONE_API_KEY` | Bearer token（伺服器啟用 `--api-key` 時） | `--api-key` |
| `SYSTEMONE_MODEL` | model id（僅 router 模式） | `--model` |

## 授權

Apache License 2.0.
