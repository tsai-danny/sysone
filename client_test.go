package sysone

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestClient starts an httptest server with the given handler and returns
// a client pointed at it (full endpoint path included).
func newTestClient(t *testing.T, h http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := NewClient(Config{BaseURL: srv.URL + "/v1/systemone"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c, srv
}

const canonicalResponse = `{
  "model": "ggml-org/Kev-4B-GGUF",
  "answers": {
    "route": {"type":"choice","choice":"billing","probabilities":{"billing":0.9049,"shipping":0.0275,"technical":0.0676},"confidence":0.8574},
    "angry": {"type":"noul","noul":0.8208},
    "urgency": {"type":"score","score":2.2821,"legend":{"0":"can wait","1":"this week","2":"today","3":"right now"},"probabilities":{"0":0.036,"1":0.1937,"2":0.2225,"3":0.5478},"confidence":0.2821}
  },
  "usage": {"input_tokens":130,"output_tokens":0}
}`

func TestAsk_CanonicalRoundTrip(t *testing.T) {
	var got wireRequest
	var gotAuth, gotCT, gotUA string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		gotUA = r.Header.Get("User-Agent")
		if r.URL.Path != "/v1/systemone" {
			t.Errorf("path = %q, want /v1/systemone", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(canonicalResponse))
	})
	c.cfg.APIKey = "secret"

	resp, err := c.Ask(context.Background(), "Customer message: I was charged twice.", map[string]Question{
		"route": Choice{QID: "route", Instructions: "Which team?", Criteria: map[string]string{
			"billing": "payments", "shipping": "delivery", "technical": "bugs",
		}},
		"angry":   Noul{QID: "angry", Instructions: "Is the customer angry?"},
		"urgency": Score{QID: "urgency", Instructions: "How urgent?", Criteria: []string{"can wait", "this week", "today", "right now"}},
	})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if gotAuth != "Bearer secret" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q", gotCT)
	}
	if !strings.HasPrefix(gotUA, "systemone-go/") {
		t.Errorf("User-Agent = %q", gotUA)
	}
	if got.Model != "" {
		t.Errorf("model sent = %q, want omitted in single-model mode", got.Model)
	}
	if got.State != "Customer message: I was charged twice." {
		t.Errorf("state = %v", got.State)
	}
	if len(got.Questions) != 3 {
		t.Fatalf("sent %d questions, want 3", len(got.Questions))
	}

	// Typed answers.
	if a := resp.Answer("route"); a == nil || a.Choice != "billing" || a.Confidence != 0.8574 {
		t.Errorf("route answer = %+v", a)
	} else if a.Probabilities["billing"] != 0.9049 {
		t.Errorf("route probabilities = %v", a.Probabilities)
	}
	if a := resp.Answer("angry"); a == nil || a.Noul != 0.8208 || !a.Passed {
		t.Errorf("angry answer = %+v (noul 0.8208 ≥ 0.5 must pass)", a)
	}
	if a := resp.Answer("urgency"); a == nil || a.Score != 2.2821 || a.Legend["3"] != "right now" {
		t.Errorf("urgency answer = %+v", a)
	}
	if resp.Usage.InputTokens != 130 || resp.Usage.OutputTokens != 0 {
		t.Errorf("usage = %+v", resp.Usage)
	}
	if resp.Model != "ggml-org/Kev-4B-GGUF" {
		t.Errorf("model = %q", resp.Model)
	}
	if resp.Latency < 0 {
		t.Errorf("latency not measured: %v", resp.Latency)
	}
	if !resp.Passed() {
		t.Error("Passed() = false, want true")
	}
}

func TestAsk_ModelFieldSentInRouterMode(t *testing.T) {
	var got wireRequest
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(canonicalResponse))
	})
	c.cfg.Model = "ggml-org/Julia-1-GGUF:Q8_0"
	if _, err := c.Ask(context.Background(), "s", map[string]Question{
		"a": Noul{QID: "a", Instructions: "q?"},
	}); err != nil {
		t.Fatal(err)
	}
	if got.Model != "ggml-org/Julia-1-GGUF:Q8_0" {
		t.Errorf("model = %q", got.Model)
	}
}

func TestAsk_JSONStateAndImages(t *testing.T) {
	var got wireRequest
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(canonicalResponse))
	})
	state := []map[string]any{
		{"role": "user", "content": []map[string]any{
			{"type": "image_url", "image_url": map[string]string{"url": "data:image/png;base64,AAAA"}},
			{"type": "text", "text": "classify"},
		}},
	}
	_, err := c.ask(context.Background(), "", state, []string{"data:image/png;base64,BBBB"}, map[string]Question{
		"kind": Choice{QID: "kind", Instructions: "What is this?", Criteria: map[string]string{"invoice": ""}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Images) != 1 || got.Images[0] != "data:image/png;base64,BBBB" {
		t.Errorf("images = %v", got.Images)
	}
	// choice with empty description must serialise as null
	var raw struct {
		Questions map[string]struct {
			Criteria map[string]*string `json:"criteria"`
		} `json:"questions"`
	}
	json.Unmarshal(mustMarshal(t, got), &raw)
	crit := raw.Questions["kind"].Criteria
	if len(crit) != 1 || crit["invoice"] != nil {
		t.Errorf("criteria = %#v, want invoice:null", crit)
	}
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Contract error triggers per systemone_api.md §4.
func TestAsk_RemoteErrors(t *testing.T) {
	cases := []struct {
		name         string
		status       int
		body         string
		wantType     string
		wantLoad     bool
		wantContract bool
	}{
		{"400 empty questions", 400, `{"error":{"code":400,"message":"questions must not be empty","type":"invalid_request_error"}}`, "invalid_request_error", false, true},
		{"400 score levels", 400, `{"error":{"code":400,"message":"score criteria must have 2-10 levels","type":"invalid_request_error"}}`, "invalid_request_error", false, true},
		{"400 non data URL image", 400, `{"error":{"code":400,"message":"image must be a data URL","type":"invalid_request_error"}}`, "invalid_request_error", false, true},
		{"501 not a decision model", 501, `{"error":{"code":501,"message":"loaded model is not a decision model","type":"not_supported_error"}}`, "not_supported_error", false, true},
		{"501 images unsupported", 501, `{"error":{"code":501,"message":"model does not support images","type":"not_supported_error"}}`, "not_supported_error", false, true},
		{"503 loading", 503, `{"error":{"code":503,"message":"model is loading","type":"server_error"}}`, "server_error", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			})
			_, err := c.Ask(context.Background(), "state", map[string]Question{
				"a": Noul{QID: "a", Instructions: "q?"},
			})
			var ae *APIError
			if !errors.As(err, &ae) {
				t.Fatalf("err %v is not *APIError", err)
			}
			if ae.Code != tc.status {
				t.Errorf("Code = %d, want %d", ae.Code, tc.status)
			}
			if ae.Type != tc.wantType {
				t.Errorf("Type = %q, want %q", ae.Type, tc.wantType)
			}
			if ae.IsLoading() != tc.wantLoad {
				t.Errorf("IsLoading() = %v, want %v", ae.IsLoading(), tc.wantLoad)
			}
			if ae.IsContract() != tc.wantContract {
				t.Errorf("IsContract() = %v, want %v", ae.IsContract(), tc.wantContract)
			}
			if !strings.Contains(err.Error(), "Hint:") {
				t.Errorf("error must carry a Hint: %v", err)
			}
		})
	}
}

func TestAsk_LocalValidation(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("request must not be sent for locally invalid input")
	})
	ctx := context.Background()
	q := map[string]Question{"a": Noul{QID: "a", Instructions: "q?"}}

	if _, err := c.Ask(ctx, "s", nil); !errors.Is(err, ErrNoQuestions) {
		t.Errorf("empty questions: %v", err)
	}
	if _, err := c.Ask(ctx, "", q); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("empty state: %v", err)
	}
	if _, err := c.Ask(ctx, nil, q); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("nil state: %v", err)
	}
	if _, err := c.Ask(ctx, "s", map[string]Question{
		"c": Choice{QID: "c", Instructions: "q?"},
	}); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("empty choice criteria: %v", err)
	}
	if _, err := c.Ask(ctx, "s", map[string]Question{
		"s": Score{QID: "s", Instructions: "q?", Criteria: []string{"only one"}},
	}); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("score with 1 level: %v", err)
	}
	if _, err := c.Ask(ctx, "s", map[string]Question{
		"s": Score{QID: "s", Instructions: "q?", Criteria: make([]string, 11)},
	}); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("score with 11 levels: %v", err)
	}
	if _, err := c.Ask(ctx, "s", map[string]Question{
		"n": Noul{QID: "n", Instructions: "q?", Criteria: map[string]string{"yes": "d"}},
	}); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("noul bad criteria key: %v", err)
	}
}

func TestAsk_NoulBelowThresholdFails(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"answers":{"a":{"type":"noul","noul":0.31}},"usage":{}}`))
	})
	resp, err := c.Ask(context.Background(), "s", map[string]Question{
		"a": Noul{QID: "a", Instructions: "q?"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Answer("a").Passed {
		t.Error("noul 0.31 < 0.5 default threshold must not pass")
	}
	if resp.Passed() {
		t.Error("Response.Passed() must be false")
	}
}

func TestHealth(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/health" {
				t.Errorf("path = %q", r.URL.Path)
			}
			w.Write([]byte(`{"status":"ok"}`))
		})
		if err := c.Health(context.Background()); err != nil {
			t.Fatalf("Health: %v", err)
		}
	})
	t.Run("loading", func(t *testing.T) {
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(503)
			w.Write([]byte(`{"error":{"code":503,"message":"loading","type":"server_error"}}`))
		})
		err := c.Health(context.Background())
		var ae *APIError
		if !errors.As(err, &ae) || !ae.IsLoading() {
			t.Fatalf("Health 503: %v", err)
		}
	})
}

func TestModels(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Write([]byte(`{"object":"list","data":[{"id":"ggml-org/Kev-4B-GGUF","object":"model","owned_by":"ggml-org"}]}`))
	})
	ms, err := c.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || ms[0].ID != "ggml-org/Kev-4B-GGUF" {
		t.Errorf("models = %+v", ms)
	}
}

func TestProps_Vision(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/props" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Write([]byte(`{"modalities":{"vision":true},"add_bos_token":true}`))
	})
	p, err := c.Props(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !p.Vision() {
		t.Error("Vision() = false, want true")
	}
}

func TestNewClient_EnvFallbackAndDefaults(t *testing.T) {
	t.Setenv("SYSTEMONE_URL", "http://example.test:9/v1/systemone")
	t.Setenv("SYSTEMONE_API_KEY", "k")
	t.Setenv("SYSTEMONE_MODEL", "m")
	c, err := NewClient(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL() != "http://example.test:9/v1/systemone" {
		t.Errorf("BaseURL = %q", c.BaseURL())
	}
	if c.cfg.APIKey != "k" || c.cfg.Model != "m" {
		t.Errorf("env fallback failed: key=%q model=%q", c.cfg.APIKey, c.cfg.Model)
	}
	if c.cfg.HTTPClient == nil || c.cfg.HTTPClient.Timeout != DefaultTimeout {
		t.Error("default HTTP client timeout not applied")
	}
	if c.cfg.MaxImageBytes != DefaultMaxImageBytes {
		t.Errorf("MaxImageBytes = %d", c.cfg.MaxImageBytes)
	}
}

func TestNewClient_BadURL(t *testing.T) {
	if _, err := NewClient(Config{BaseURL: "not a url"}); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("bad URL: %v", err)
	}
	if _, err := NewClient(Config{BaseURL: "ftp://x/v1/systemone"}); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("non-http scheme: %v", err)
	}
}

func TestNewClient_RootURLTolerance(t *testing.T) {
	for _, in := range []string{"http://host:8080", "http://host:8080/"} {
		c, err := NewClient(Config{BaseURL: in})
		if err != nil {
			t.Fatalf("NewClient(%q): %v", in, err)
		}
		if c.BaseURL() != "http://host:8080/v1/systemone" {
			t.Errorf("NewClient(%q): BaseURL = %q", in, c.BaseURL())
		}
	}
	// A custom path (reverse proxy) is preserved as-is.
	c, err := NewClient(Config{BaseURL: "https://gw.example.test/proxy/sysone"})
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL() != "https://gw.example.test/proxy/sysone" {
		t.Errorf("custom path: BaseURL = %q", c.BaseURL())
	}
}

func TestConnectionFailure_HintPresent(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close() // port now closed → connection refused
	c, err := NewClient(Config{BaseURL: srv.URL + "/v1/systemone"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Ask(context.Background(), "s", map[string]Question{"a": Noul{QID: "a", Instructions: "q?"}})
	if err == nil {
		t.Fatal("want connection error")
	}
	if !strings.Contains(err.Error(), "Hint:") {
		t.Errorf("connection error must carry Hint: %v", err)
	}
}

func TestContextCancellation(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Ask(ctx, "s", map[string]Question{"a": Noul{QID: "a", Instructions: "q?"}}); err == nil {
		t.Fatal("want context cancellation error")
	}
}
