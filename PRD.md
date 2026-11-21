# PRD — `systemone` Go Module 與 `sysone` CLI

> 版本：v1.1（2026-11-21）— 納入 TypeSafe Official SDK 評估結論（見 §8.1）
> 狀態：草案，待評審
> 相關資料：
> - API 規格：[systemone_api.md](file:///m:/golang/systemone-chat/docs/systemone_api.md)（`POST /v1/systemone`，TypeSafe-compatible / Jev-like）
> - 需求草稿：[PRD_draft.md](file:///m:/golang/sysone/PRD_draft.md)
> - 上游參考：llama.cpp PR #29818、[HF 決策模型文章](https://huggingface.co/blog/ggml-org/decision-models-in-llamacpp)、[TypeSafe API docs](https://docs.typesafe.ai/api)

---

## 1. 背景與動機

llama.cpp v1.0.0 起正式支援決策模型（Decision Models），以 `POST /v1/systemone` 提供
TypeSafe（Jev）相容服務。決策模型與聊天 LLM 的本質差異：

| 特性 | 聊天 LLM | System One 決策模型 |
|---|---|---|
| 推理 | 逐 token 生成 | **單次 forward pass** 對選項打分 |
| 輸出 | 非結構化文字，需解析 | **結構化答案＋機率**，`output_tokens` 恆 0 |
| 互動 | 流式、非同步 | 同步、確定性（數十 ms 級） |
| 答案 | 自由文本 | 永遠是給定選項之一（choice/score/noul） |

Go 生態目前**沒有**針對 `/v1/systemone` 的 SDK 或 CLI（社群集中在 Python/TypeScript），
本專案填補此空缺，並額外提供 AI Agent 友善的 CLI。

## 2. 目標與非目標

### 2.1 目標

1. **Go Module `systemone`**：輕量、慣用的型別安全客戶端
   - 可配置 endpoint（URL）連到任意 llama-server 相容後端
   - **Step Builder**（階段式 Fluent API）引導正確組裝：State 先行 → 加問題 → 執行
   - 強型別結構化輸出：呼叫端不需手寫 `if prob > 0.5` / argmax 解析
2. **CLI `sysone`**（基於該 module）：供 AI Agent 透過 skills 調用做決策
   - 指令結構即文件；錯誤訊息即可執行修正提示（模擬編譯期防呆）
   - 預設機器可讀 JSON 輸出、標準化 exit code
3. 與 `systemone-chat` 測試台行為一致（同一份契約，方便交叉驗證）

### 2.2 非目標（v1）

- 不做模型管理/啟動（由 llama-server 負責）
- 不做流式、多輪對話、重試策略（契約不支援 streaming）
- 不做 confidence 校準工具（僅暴露 raw probabilities / confidence）
- 不做 TUI；CLI 為純單向輸出工具

## 3. 用戶與使用情境

| 角色 | 情境 |
|---|---|
| Go 開發者 | 在服務中嵌入請求路由、內容審核、agent 步驟成功判定 |
| AI Agent（Claude/Copilot 等） | 透過 Bash/skill 調用 `sysone`，讀 JSON 結果決定下一步 |
| 平台/維運 | 批次評估工單（共享 state 多題）、健康探測 |

典型情境：
- 工單路由：`choice` 分派團隊 + `noul` 判斷是否憤怒 + `score` 評估緊急度（單次請求批量提問）
- Agent 守門：執行動作前 `sysone noul --state "<log>" --q "前一步是否成功？" --threshold 0.8`，依 exit code 分支
- 文件分類：OpenJev + 圖片 `sysone choice --image shot.png --options "invoice,receipt,contract"`

## 4. API 契約摘要（依 systemone-chat 規格）

**請求**：`POST {endpoint}`，`Content-Type: application/json`
```json
{
  "model": "ggml-org/Kev-4B-GGUF",
  "state": "字串 | 物件 | 陣列（含 chat messages）",
  "images": ["data:image/png;base64,..."],
  "questions": {
    "qid": { "type": "choice|score|noul", "instructions": "...", "criteria": "..." }
  }
}
```
- `state` 必填；非字串以 JSON 文字餵模型；chat messages 中的 `image_url` part（僅 data URL）視為圖片
- `questions` 必填非空；`criteria` 形狀依 type：
  - `choice`：`{option: description|null}`，空物件 → 400；選項數受模型上限（openjev 52 / laya 255）
  - `score`：2–10 個等級描述，**由低到高**；1 個或 >10 → 400
  - `noul`：選填 `{"true": desc, "false": desc}`
- `model` 僅 router 模式使用；認證：`Authorization: Bearer <key>`（server 有 `--api-key` 時）

**回應（200）**：
- `answers.<qid>`：
  - choice → `{type, choice, probabilities{sum=1}, confidence}`
  - score → `{type, score(期望索引，小數), legend, probabilities, confidence}`
  - noul → `{type, noul: P(true)}`
- `usage`: `{input_tokens, output_tokens: 0}`

**錯誤**：OAI 格式 `{"error":{"code","message","type"}}`；400 invalid_request / 501 not_supported（非決策模型或圖片不支持）/ 503 載入中。

**探測端點**：`GET /health`、`GET /v1/models`、`GET /props`（`modalities.vision`）。

## 5. 功能需求 — Go Module（`github.com/<org>/systemone`）

### 5.1 Client 配置

```go
type Config struct {
    BaseURL    string        // 預設 http://localhost:8080/v1/systemone
    APIKey     string        // 選填，Bearer
    Model      string        // 選填，router 模式
    HTTPClient *http.Client  // 選填，預設 30s timeout
    UserAgent  string
}
func NewClient(cfg Config) (*Client, error)
```
- 環境變數 fallback：`SYSTEMONE_URL`、`SYSTEMONE_API_KEY`、`SYSTEMONE_MODEL`
- 輔助方法：`Health(ctx) error`、`Models(ctx) ([]Model, error)`、`Props(ctx) (*Props, error)`
- **低階逃生門 API**（與 TypeSafe 官方 SDK 扁平呼叫形狀對齊，方便雲端用戶遷移與進階用法）：

```go
// Ask 直接以 wire 級 questions 定義單次請求，跳過 builder
func (c *Client) Ask(ctx context.Context, state any, questions map[string]Question) (*Response, error)
// Question 為 Choice / Score / Noul 結構體（命名對齊官方 SDK）
```
  Step Builder 為推薦主路徑；`Ask` 供逃生門與 CLI `ask` 子命令使用。

### 5.2 Step Builder（核心防呆）

Go 方法不支援獨立泛型參數，採「階段介面 + 指標綁定」：

```go
ans, err := client.Decide().
    WithTextState("Customer message: charged twice...").   // 或 WithJSONState(v) / WithImageState(path|dataURL) / WithMessages(msgs)
    AddChoice("route", "Which team?", map[string]string{   // 選項+描述
        "billing": "payments, refunds", "technical": "bugs",
    }, &route).                                             // *string → 勝出選項
    AddNoul("angry", "Is the customer angry?", 0.8, &isAngry). // *bool，threshold 判定
    AddScore("urgency", "How urgent?", []string{"can wait","this week","today","right now"}, &urgency). // *float64 期望分
    Execute(ctx)
// ans 為完整 RawResponse；route/isAngry/urgency 已自動填入
```

規則：
- **命名對齊 TypeSafe 官方 SDK**：題型型別 `Choice` / `Score` / `Noul`，欄位 `Instructions` / `Criteria` / `Confidence` / `Probabilities`，確保與官方 Python/TS SDK 及 wire JSON 語意一致
- 階段強制順序：`Decide() → StateStep → QuestionStep → Execute`；無問題時 `Execute` 回 `ErrNoQuestions`（本地攔截，不發請求）
- 本地驗證（發出前）：score 等級 2–10、choice 選項非空、state 非空 → 回 `ErrInvalidRequest` 包裝錯誤，訊息含修正建議
- 每個 `Add*` 亦可選擇不綁指標，改由 `ans.Choice("route")` / `ans.Noul("angry")` / `ans.Score("urgency")` 查詢
- 圖片：`WithImageState` 接受本機路徑或 data URL，自動轉 base64；多圖 `WithImages(...)`
- 冪等純函數語意：同一 builder 不可併發復用（文件註明）

### 5.3 型別化結果

```go
type Answer struct {
    ID string
    Type string          // choice|score|noul
    Choice string        // choice: 勝出選項
    Probabilities map[string]float64 // choice: 選項→機率；score: "0".."9"→機率
    Confidence float64
    Score float64        // score: 期望索引
    Legend map[string]string
    Noul float64         // noul: P(true)
    Passed bool          // 依各題 threshold 判定（預設 choice=無/noul=0.5/score 不適用）
}
type Response struct {
    Model string
    Answers map[string]*Answer
    Usage Usage            // InputTokens, OutputTokens
    Latency time.Duration  // 客戶端量測
}
```
- 自訂 threshold：`AddNoul(..., threshold, target)`；`AddChoice` 可選 `WithMinConfidence(c)` 選項 → 低於門檻時 `Passed=false` 且 target 不填（或填 fallback，見 Open Questions）

### 5.4 錯誤模型

```go
var ErrNoQuestions, ErrInvalidRequest ...   // 本地
type APIError struct { Code int; Message, Type string } // 遠端 OAI 錯誤
```
- 400/501/503 映射為可判別錯誤（`errors.As`）；503 提供 `IsLoading()` 便利方法
- 所有錯誤訊息附「如何修正」提示字串（Agent 友善）

### 5.5 依賴與品質

- 依賴：**僅 stdlib**（可選 `golang.org/x/sync` 不引入）
- 測試：`net/http/httptest` 模擬後端，覆蓋契約全部 400/501/503 觸發條件、builder 階段、threshold 語意、圖片編碼
- 文件：`doc.go` + pkg.go.dev 範例（Example tests）；README 含 llama-server 啟動指令
- Go 版本：≥ 1.22

## 6. 功能需求 — CLI `sysone`

### 6.1 指令結構（subcommand = step builder 的動詞化）

```bash
# 快速通道（單題高頻）
sysone noul   --state "OOMKilled in log" --q "需要重啟容器?" --threshold 0.8
sysone choice --state @ticket.txt --q "問題分類" --options "帳務:payments|技術:bugs|退貨:refunds"
sysone score  --state @ctx.json --q "風險等級" --scale "低,中,高,極高"
sysone batch  --state @ctx.json \
    --noul  "is_urgent:是否緊急?" \
    --choice "route:分派:前端|後端|SRE" \
    --score "conf:完整度:低:中:高"

# 逃生門（直接吃原始 questions JSON，對應 module Ask()）
sysone ask --state @ctx.json --questions @questions.json

# 探測與輔助
sysone health            # GET /health
sysone models            # GET /v1/models
sysone props             # GET /props（含 vision 支援）
sysone agent-help        # 輸出精簡 JSON 規格 + 3 個範例（給 Agent skill 文件用）
```

### 6.2 通用 flags

| Flag | 說明 |
|---|---|
| `--url` | endpoint（覆寫 `SYSTEMONE_URL`） |
| `--api-key` | Bearer token |
| `--model` | router 模式模型 id |
| `--state` | 文字、`@file`（檔名副檔名 .json → JSON state）、或 `-`（stdin） |
| `--image` | 可重複；本機路徑或 data URL |
| `--threshold` | noul 判定門檻（預設 0.5）；choice 為最低 confidence |
| `--timeout` | 請求逾時（預設 30s） |
| `--pretty` | JSON 縮排（預設 compact） |
| `--quiet` | 只輸出答案值（供 shell 串接） |

### 6.3 輸出契約（預設 JSON，機器可讀）

```json
{
  "answers": { "route": {"type":"choice","choice":"帳務","confidence":0.86,"probabilities":{...},"passed":true} },
  "usage": {"input_tokens":130,"output_tokens":0},
  "latency_ms": 42
}
```

### 6.4 Exit codes（Agent 程式化分支）

| Code | 意義 |
|---|---|
| 0 | 成功且所有題 `passed` |
| 1 | 成功但有題未過門檻（Agent 據此轉人工/降級） |
| 2 | 參數/結構錯誤（stderr 為結構化修正提示） |
| 3 | 連線失敗 / 503 模型載入中 |
| 4 | 後端 400/501（契約錯誤，stderr 帶 OAI error message） |

### 6.5 Agent 友善設計（必做）

1. 缺 flag 時 stderr 輸出完整最小可用指令範例（非僅 usage 行）
2. `sysone agent-help` 一指令取得全部規格（省 system prompt token）
3. 無互動式提示、無顏色依賴、stdout 只放資料、stderr 只放診斷
4. 錯誤訊息含 `Hint:` 行給 Agent 自我修正

### 6.6 實作

- 框架：`cobra`（唯一外部依賴）；`sysone` 只呼叫 module，不重複實作邏輯
- 安裝：`go install github.com/<org>/sysone/cmd/sysone@latest`；發布附各平台二進位

## 7. 非功能需求

| 項目 | 要求 |
|---|---|
| 延時 | 客戶端開銷 < 1 ms（不含後端）；單請求典型 < 100 ms |
| 併發 | Client 執行緒安全；多請求共用 HTTP 連線池 |
| 相容性 | 契約變更跟隨 llama.cpp master；語意化版本，`/v1` 契約視為穩定 |
| 安全 | API key 不落日誌；圖片 base64 有大小上限（預設 20 MB/圖，可調） |
| 授權 | Apache-2.0（與 systemone-chat 一致） |

## 8. 可借鑑的既有設計（「抄」清單）

| 來源 | 借鑑點 |
|---|---|
| Ent（Meta ORM） | Go 階段式 builder 的介面切換防呆 |
| openai-go / google.golang.org/genai | endpoint 配置、context、錯誤包裝慣例 |
| `gh` CLI | `<resource> <action> --flag` 一致性、可執行錯誤提示 |
| Simon Willison `llm` CLI | stdin pipe 吃 state、`-o json` 資料流 |
| kubectl/gcloud | `--output json` + 嚴格 exit code |
| jevify / llamacpp-jev（社群） | `--state/--choice/--score/--noul` flag 命名、probe 校準思路 |
| PydanticAI TypeSafeModel | 型別→契約自動映射概念（Go 版以指標綁定替代） |
| TypeSafe 官方 SDK（`typesafe-sdk` / `@typesafe-ai/sdk`） | 見 §8.1 評估：抄命名與扁平 `Ask` 逃生門，不抄整體設計 |
| TypeSafe 官方 Patterns / Cookbooks | confidence-routing、companion-noul、noul-screen 等使用模式 → 寫入 README 與 `agent-help` 的 threshold 建議 |
| TypeSafe Agent Skill（SKILL.md 分發） | 印證 `agent-help` 方向；M4 順帶產出本專案 SKILL.md |

### 8.1 TypeSafe Official SDK 評估（v1.1 納入）

查證 `docs.typesafe.ai` 後對官方 SDK 的結論：

| 面向 | 官方 SDK 現況 | 本專案決策 |
|---|---|---|
| 語言 | 僅 Python/JS/TS，**無 Go 版** | 自行實作，無可依賴 |
| API 形狀 | 扁平單呼叫 `system_one(state, questions)`，1:1 鏡射 wire JSON | **抄**：補低階 `Ask()` 逃生門（§5.1） |
| 命名 | `Choice/Score/Noul`、`Instructions/Criteria/Confidence` | **抄**：全面對齊（§5.2） |
| Step Builder / 階段強制 | 無 | **不抄**：本專案差異化價值，保留 |
| threshold / 判定語意 | 無（只回 raw 機率） | **不抄**：保留 `Passed` + exit code 設計 |
| 答案存取 | 分桶（`response.choices[id]`） | **不抄**：維持統一 `Answers map[string]*Answer`（與 wire `answers` 一致） |
| `box` 題型（圖片定位） | 有 | **v1 不做**：llama.cpp `/v1/systemone` 尚未支援（見開放問題 #6） |
| 定位 | 雲端 `api.typesafe.ai` + `TYPESAFE_API_KEY` | 本專案以 local llama-server 為第一目標 |

要點：官方 SDK 的「成熟」是把 wire JSON 鏡射成類別，在 Go 中等同薄包裝、無附加價值；
真正值得借鑑的是其**模式層文件**（Patterns/Cookbooks）與 **Agent Skill 分發做法**。

## 9. 里程碑

| 里程碑 | 內容 | 驗收 |
|---|---|---|
| M1 | Module：client + 契約型別 + 錯誤模型 + httptest 測試 | `go test ./...` 全綠；契約對照 systemone_api.md 逐條核對 |
| M2 | Module：Step Builder + 指標綁定 + threshold + 圖片 | Example tests 通過；builder 階段編譯期防呆示範 |
| M3 | CLI：noul/choice/score/batch + JSON 輸出 + exit codes | 對本機 llama-server（Julia-1/Kev-4B）端到端通過；錯誤路徑 stderr 檢查 |
| M4 | CLI：health/models/props/agent-help/ask + 安裝文件 + README + **SKILL.md**（Agent skill 格式，參照 TypeSafe skills 分發做法）+ README 引用官方 patterns（confidence-routing / companion-noul / noul-screen）作 threshold 建議 | `sysone agent-help` 輸出可被 Agent 一次成功率 ≥ 95%（抽測 20 指令） |

## 10. 開放問題（待產品決策）

1. `AddChoice` 低於 confidence 門檻時：target 填 fallback 值、保持零值、還是回 sentinel error？（建議：保持零值 + `Passed=false`）
2. `batch` 是否支援多 state 檔案批次（每檔一請求）？（建議 v1 不做）
3. ~~是否需要 `sysone ask` 直接吃原始 questions JSON（逃生門）？~~ **已決策（v1.1）**：納入，M4 實作 `sysone ask --questions <json>`，對應 module `Ask()`（§5.1）
4. Module 公開路徑與 repo 名稱（`github.com/<org>/systemone`？CLI repo 同 repo 或分離？）
5. noul 預設 threshold 0.5 是否過低（參考規格 §8：confidence 需按模型自測）？
6. llama.cpp 若後續支援 TypeSafe 的 `box`（圖片定位）primitive，是否跟進？（v1 不做，見 §8.1）

## 11. 參考來源

- llama.cpp server README — TypeSafe-compatible API Endpoints
- llama.cpp PR #29818；HF blog "New in llama.cpp: Decision Models"
- TypeSafe API：<https://docs.typesafe.ai/api>
- OpenJev multimodal API：<https://jev-skills.github.io/openjev-multimodal/api>
- 本地：`systemone-chat/docs/systemone_api.md`、`model_capability_test_report.md`（模型語言能力：中文用 Julia-1/OpenJev）
