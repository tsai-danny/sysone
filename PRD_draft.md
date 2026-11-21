
user:
現在llama.cpp 以支援許多jev-like 的decision models，統一以systemone api 方式提供服務，但此類服務與一般llm 的流式、非同步、和非結構化輸出入格式不同，簡化成調用工具模組 提供step builder輸入、結構化輸出 有其價值，2026/11 的今天有哪些類似的實作?

agent:
截至 2026 年 11 月，隨著 llama.cpp 正式合併支援 Decision Models 的 /v1/systemone 端點 與 發布 v1.0.0 穩定版，以及 Hugging Face Transformers 推出標準化的 DecisionPipeline，整個生態系確實已經意識到「單次前向傳播（Single Forward Pass）、無生成文字、強制型別問題與 calibrated 機率輸出」與傳統對話式 LLM 的本質差異。
你的直覺非常準確——把 /v1/systemone 的原始 JSON 契約（state + choice/score/noul 題型）封裝成具備型別引導（Step Builder / Fluent API）且直接回傳強型別結果（Structured Output）的調用模組，正是目前社群與官方 SDK 演進的主流方向。
以下是目前（2026 年 11 月）已經存在、且最貼近你所描述的實作與框架整合：

1. 官方與主流架構的標準化實作


專案 / 框架	語言支援	核心設計與 Builder / 結構化特徵	適用場景
TypeSafe Official SDK (typesafe-sdk / @typesafe-ai/sdk)	Python, TypeScript, JS	/v1/systemone 契約的制定者。提供流暢的題型建構器（choice、score、noul），並直接透過修改 base_url 無縫對接本機的 llama-server。	追求與雲端 Jev 及本機 Kev-4B / OpenJev 100% 介面相容的標準開發。
PydanticAI (TypeSafeModel)	Python	最接近你構想的型別安全封裝。將 Agent 的 Python 強型別（如 bool、Literal、Enum 或 Pydantic Model）自動映射為 /v1/systemone 的問題契約，執行後直接反序列化為強型別物件，完全隱藏底層機率解析邏輯。	已經在使用 Python 強型別或 Agent 框架，希望用宣告式結構取代手寫 JSON 請求。
Hugging Face DecisionPipeline (transformers)	Python	2026 年 11 月初剛釋出的標準管線。將狀態輸入與問題定義標準化，封裝了前處理、單次前向傳播與機率正規化，提供一致的 pipeline("decision", ...) 調用介面。	需要同時在 Python 進程內（In-process）或透過 HTTP 服務切換不同系統一模型（如 GEV、Bekko）的團隊。


2. 針對 llama.cpp 的輕量級工具與轉接層

在 llama.cpp 原生支援 /v1/systemone 前後，社群也出現了許多強調「CLI / Builder 風格組裝問題」與「讓任意 GGUF 模型也能模擬 System One 行為」的工具：
• jevify (fidecastro/jevify)
	• 設計理念：提供極簡的宣告式與鏈式參數組裝（支援 --state、--choice、--score、--noul、--image），能將任何相容 OpenAI 格式的後端（包含 llama-server 或 vLLM）包裝成標準 /v1/systemone 服務，並自動透過 probe 校準單一 Token 的 logprobs。
• llamacpp-jev (NakliTechie/llamacpp-jev)
	• 設計理念：極薄的包裝層（Thin Wrapper），在底層透過 llama-server 的 GBNF 語法限制與 n_probs 實現批次型別問題查詢，將繁瑣的 grammar 建構與 softmax 正規化隱藏在模組內部。
• systemonemodels CLI & Studio
	• 設計理念：專為 System One 決策模型設計的開發者工具鏈，簡化了本機微調與結構化推論的調用流程。

3. 為什麼「Step Builder + 結構化輸出」在此領域特別有價值？

從你前幾輪提到的 Step Builder 視角來看，目前現有 SDK 大多停留在「Fluent Interface（方法鏈）」或「Schema 反射（如 Pydantic）」的層次，真正的編譯期 Step Builder 在這塊仍大有可為，原因有三：
1. 強制狀態前置（State-First Constraint）：System One 模型高度依賴 Shared Prefix（KV Cache 快取共享狀態）來平行評估多個問題。透過 Step Builder 強制要求「必須先設定 .withState(...)（支援純文字、JSON 或多模態截圖），下一步才能 .addChoice(...) 或 .addScore(...)」，能從 API 設計上引導開發者寫出符合硬體加速最佳實踐的請求。
2. 題型防呆與 Threshold 綁定：/v1/systemone 支援 choice（單選）、score（序數評分）、noul（布林是非）三種不同結構的題型，且各自對應不同的機率解析方式（例如 noul 直接取 yes 機率，choice 取 argmax 或設定信心門檻 fallback）。Step Builder 可以在新增問題的當下，就透過泛型（Generics）把該題的「輸入選項型別」與「最終 .execute() 回傳的強型別屬性」綁定在一起。
3. 同步與確定性（Deterministic Execution）：不像一般 LLM 需要處理 AsyncGenerator、Token Stream 或重試機制（Retry on malformed JSON），Decision Model 固定在單次前向傳播內完成（通常僅需數十至數百毫秒），非常適合封裝成同步/非同步皆可的純函數式工具模組（Pure Function Tool）。
如果你正打算為團隊或開源社群設計這套工具模組，可以告訴我：
• 你的主要開發語言（如 TypeScript、Python、Go 或 Rust）
• 偏好的型別綁定方式（例如透過泛型 Step Builder 動態推導回傳型別，或是基於 Schema / Class 反射）
我可以幫你設計這套 Step Builder 的介面雛形與泛型型別推導架構。
嘗試不使用個人化功能
• 
Hugging Face
New in llama.cpp: Decision Models - Hugging Face
2026年10月2日 — llama.cpp server now supports decision models through the /v1/systemone endpoint. You send a state (text, JSON, a screenshot) and ...
• 
BigGo 財經
llama.cpp 推出首個v1.0.0，歷經三年半正式承諾API 穩定性
2026年11月16日 — 開源大型語言模型(LLM)推論引擎 … 發布首個主要版本v1.0.0,距離2023年3月問世約三年半。機器學習張量函式庫「ggml」同日也發布v1.0.0
• 
GitHub
llamacpp-jev — TypeSafe Jev's /v1/systemone (typed ...
October 1, 2026 — # Verified (Qwen3.5-2B / 4B-Q8_0): Repo (MIT), design, benchmark and an honest competitor comparison: https://github.com/NakliTech...
顯示全部

主要開發語言: golnag
寫成module 供後續專案引用，快速組裝 decision語法物件 並通過 configurable endpoint (url) 查詢llama-server 相容systemone api 後端，輸出結果也要便於解析；
另外評估以此module 建立cli tool 供ai agent 透過skills 調用 做decision making，commands 、flags 與subcommands 的設計 同樣要能引導agent 操作(類似step builder 概念)；
是否已有類似專案或api 設計 可以抄?

截至 2026 年 11 月，在 Go (Golang) 生態系中，目前還沒有出現專為 llama.cpp 的 /v1/systemone 打造的標準化 Go SDK 或 CLI 工具（目前官方與社群重心全集中在 Python 與 TypeScript）。這代表你正處於一個極佳的切入點，非常適合建立一個乾淨、慣用（Idiomatic）的 Go Module 與 Agent-ready CLI。
雖然沒有直接針對 SystemOne 的 Go 套件，但你可以直接「借鑑/抄寫」三個在 Go 社群與 AI Agent 領域最成熟的設計典範：

一、 Go Module 設計：可參考的 Step Builder 與型別解析範例

在 Go 裡面要實現 Step Builder，標準做法是「定義多個非公開（unexported）的 struct，搭配公開（exported）的 Interface 來控制每個階段能呼叫的方法」。

1. API 設計參考來源

• Ent (Facebook/Meta 的 Go ORM)：Go 語言中最經典的 Fluent + Step Builder 實作。它的 Query Builder 透過介面切換，強制你在 .Save(ctx) 之前必須先完成必填欄位設定，且直接回傳強型別的 struct。
• Google GenAI Go SDK (google.golang.org/genai) 與 OpenAI Go SDK (openai-go)：學習它們如何處理 WithEndpoint(url)（支援切換本機 llama-server 與遠端服務）以及 context.Context 超時控制。

2. 推薦的 Go Step Builder 介面雛形

你可以把「設定 State」與「新增問題」拆成兩個階段，並把解析器（Parser/Decoder）綁定在問題定義上，讓呼叫端完全不用自己寫 if prob > 0.5 或找 argmax：
go
package systemone

// 階段 1：入口點，強制必須先給 State（確保 KV Cache Shared Prefix 語意清晰）
type Client struct { /* endpoint, httpClient */ }
func (c *Client) Decide() StateStep { ... }

type StateStep interface {
    WithTextState(text string) QuestionStep
    WithJSONState(v any) QuestionStep
    WithImageState(base64OrPath string) QuestionStep // 支援多模態決策模型
}

// 階段 2：加入各種題型，並透過指標或泛型直接綁定輸出變數
type QuestionStep interface {
    // 綁定指標，Execute 後自動填入 bool（例如 prob_yes >= threshold）
    AddNoul(id, question string, threshold float64, target *bool) QuestionStep
    
    // 綁定指標，Execute 後自動填入勝出的選項字串
    AddChoice(id, question string, options []string, target *string) QuestionStep
    
    // 綁定指標，Execute 後自動填入加權分數
    AddScore(id, question string, min, max int, target *float64) QuestionStep
    
    // 只有進入此階段才能執行
    Execute(ctx context.Context) (*RawResponse, error)
}
請謹慎使用程式碼。
為什麼這樣設計最貼合 Go？ 因為 Go 的方法（Method）不支援獨立的泛型型別參數（Type Parameters），透過傳入指標（target *bool、target *string），開發者在組裝問題的當下就把「輸入問題」與「輸出變數」綁死了，呼叫 .Execute(ctx) 完畢後變數直接可用，完全省去手動解析 JSON map 的痛苦。

二、 給 AI Agent 調用的 CLI 設計：如何讓 Subcommands 具備「Step Builder 引導力」？

要讓 LLM / AI Agent 透過 Skill（或 Bash Tool）無痛調用 CLI，核心原則是：「指令結構即文件（Self-documenting CLI）」與「錯誤訊息即提示（Actionable Error Messages）」。

1. 可以直接參考的「Agent 友善 CLI」設計標竿

• gh (GitHub CLI)：業界公認最適合 LLM 操作的 CLI。它的語法高度一致（gh <resource> <action> --flag），而且當 Agent 少傳參數時，它會回傳極度明確的提示告訴 Agent 缺了什麼。
• llm (Simon Willison 的 CLI 工具)：極簡的資料流設計，支援透過標準輸入（stdin pipe）吃進上下文狀態，再用 flags 定義結構化任務。
• Google Cloud CLI (gcloud) / kubectl：利用 --output json 與嚴格的 Exit Code（例如 0 代表決策成功、2 代表參數結構錯誤、3 代表低於信心門檻）讓 Agent 程式化判斷下一步。

2. 模仿 Step Builder 的 CLI Subcommand 架構

在 CLI 中無法像 IDE 一樣彈出自動補全，因此要改用「動詞導向的 Subcommands」搭配「漸進式探索（Progressive Disclosure）」來引導 Agent：
bash
# 1. 單一決策快速通道（最適合單題高頻呼叫，Agent 幾乎不會用錯）
sysone noul   --state "系統日誌顯示 OOMKilled" --q "是否需要立即重啟容器？" --threshold 0.8
sysone choice --state @ticket.txt --q "客戶問題分類" --options "帳務,技術支援,退貨"
sysone score  --state "程式碼變更 500 行" --q "評估潛在風險等級" --scale 1-5

# 2. 多題批次決策（共享 State 的 KV Cache，適合複雜狀態評估）
cat context.json | sysone batch \
  --noul "is_urgent:是否緊急?" \
  --choice "route:分派部門:前端|後端|SRE" \
  --score "confidence:資訊完整度:1:10"
請謹慎使用程式碼。

3. 讓 Agent 自動修正的 3 個 CLI 設計細節（必抄技巧）

1. 標準化 JSON 輸出（預設即機器可讀）：
不要輸出給人類看的 ASCII 表格，預設直接輸出精簡 JSON（包含 answer、probability、raw_probs、latency_ms），方便 Agent 直接解析或用 jq 串接。
2. 設計專門給 Agent 看的 --schema / --agent-help 旗標：
在 Skill 文件中只需寫一句：「執行 sysone --agent-help 獲取完整指令規格」。該指令會吐出極度濃縮、無廢話的 JSON Schema 與 3 個標準範例，省下大量 System Prompt 的 Token。
3. 防呆錯誤引導（模擬 Step Builder 的編譯期檢查）：
當 Agent 忘記傳 --state 或格式錯誤時，回傳的 stderr 必須是結構化的修正建議，例如：
Error: missing --state. Usage: sysone choice --state "<text>" --q "<question>" --options "A,B,C"
現代的 Coding Agent 看到這行錯誤訊息，在下一輪 Tool Call 就會 100% 自動修正並成功執行。
