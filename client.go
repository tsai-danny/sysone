package systemone

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// DefaultBaseURL is the llama-server default endpoint.
const DefaultBaseURL = "http://localhost:8080/v1/systemone"

// DefaultTimeout applies when Config.HTTPClient is nil.
const DefaultTimeout = 30 * time.Second

// DefaultMaxImageBytes is the per-image size cap (20 MB) applied to local
// files loaded by the builder.
const DefaultMaxImageBytes = 20 * 1 << 20

// Config configures a [Client]. Zero values fall back to environment
// variables and then to defaults:
//
//	BaseURL → SYSTEMONE_URL → DefaultBaseURL
//	APIKey  → SYSTEMONE_API_KEY
//	Model   → SYSTEMONE_MODEL
type Config struct {
	// BaseURL is the full POST endpoint, e.g. http://localhost:8080/v1/systemone.
	BaseURL string
	// APIKey is sent as "Authorization: Bearer <key>" when non-empty.
	APIKey string
	// Model is only used in router mode (multi-model server).
	Model string
	// HTTPClient defaults to a client with a 30s timeout.
	HTTPClient *http.Client
	// UserAgent defaults to "systemone-go/<version>".
	UserAgent string
	// MaxImageBytes caps the size of each local image loaded by the builder.
	// Defaults to DefaultMaxImageBytes (20 MB).
	MaxImageBytes int64
}

// Client is a concurrency-safe System One API client. All requests share the
// configured HTTP connection pool.
type Client struct {
	cfg Config
}

// NewClient validates cfg (after env fallback) and returns a ready client.
func NewClient(cfg Config) (*Client, error) {
	c := Client{cfg: cfg}

	if c.cfg.BaseURL == "" {
		c.cfg.BaseURL = os.Getenv("SYSTEMONE_URL")
	}
	if c.cfg.BaseURL == "" {
		c.cfg.BaseURL = DefaultBaseURL
	}
	if c.cfg.APIKey == "" {
		c.cfg.APIKey = os.Getenv("SYSTEMONE_API_KEY")
	}
	if c.cfg.Model == "" {
		c.cfg.Model = os.Getenv("SYSTEMONE_MODEL")
	}
	if c.cfg.HTTPClient == nil {
		c.cfg.HTTPClient = &http.Client{Timeout: DefaultTimeout}
	}
	if c.cfg.UserAgent == "" {
		c.cfg.UserAgent = "systemone-go/1"
	}
	if c.cfg.MaxImageBytes <= 0 {
		c.cfg.MaxImageBytes = DefaultMaxImageBytes
	}

	if _, err := url.Parse(c.cfg.BaseURL); err != nil {
		return nil, invalid(fmt.Sprintf("BaseURL %q is not a valid URL: %v", c.cfg.BaseURL, err),
			"set BaseURL (or SYSTEMONE_URL) to the full endpoint, e.g. http://localhost:8080/v1/systemone")
	}
	if !strings.HasPrefix(c.cfg.BaseURL, "http://") && !strings.HasPrefix(c.cfg.BaseURL, "https://") {
		return nil, invalid(fmt.Sprintf("BaseURL %q must start with http:// or https://", c.cfg.BaseURL),
			"include the scheme, e.g. http://localhost:8080/v1/systemone")
	}
	return &c, nil
}

// BaseURL returns the configured POST endpoint.
func (c *Client) BaseURL() string { return c.cfg.BaseURL }

// origin returns the server root (scheme://host[:port]) derived from BaseURL,
// used for the GET probe endpoints.
func (c *Client) origin() string {
	u, err := url.Parse(c.cfg.BaseURL)
	if err != nil {
		return c.cfg.BaseURL
	}
	return u.Scheme + "://" + u.Host
}

// newRequest builds an authorised request against the given absolute URL.
func (c *Client) newRequest(ctx context.Context, method, u string, body []byte) (*http.Request, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, invalid(fmt.Sprintf("cannot build request: %v", err),
			"check the endpoint URL")
	}
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}
	return req, nil
}

// do performs req and returns the body, mapping non-2xx to *APIError and
// transport failures to a wrapped error (safe to test with errors.Is/As).
func (c *Client) do(req *http.Request) ([]byte, error) {
	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("systemone: request to %s failed: %w\nHint: is llama-server running? start it with `llama serve -hf ggml-org/Julia-1-GGUF`, check --url / SYSTEMONE_URL, and try `sysone health`",
			req.URL, redactTransportErr(err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, fmt.Errorf("systemone: reading response from %s: %w\nHint: the server closed the connection early — check server logs",
			req.URL, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, parseAPIError(resp.StatusCode, body)
	}
	return body, nil
}

// redactTransportErr strips any URL userinfo so credentials can never leak
// into logs (net/url errors can echo the URL).
func redactTransportErr(err error) error {
	msg := err.Error()
	if i := strings.Index(msg, "://"); i > 0 {
		if j := strings.LastIndex(msg[:i+3], "//"); j >= 0 {
			seg := strings.SplitN(msg[i+3:], "/", 2)[0]
			if at := strings.Index(seg, "@"); at >= 0 {
				msg = strings.Replace(msg, seg, seg[at+1:], 1)
			}
		}
	}
	return errors.New(msg)
}

// Ask sends a single request with wire-level question definitions, skipping
// the builder. state is required and may be a string, any JSON value, or chat
// messages. questions must be non-empty and keyed by question id.
func (c *Client) Ask(ctx context.Context, state any, questions map[string]Question) (*Response, error) {
	return c.ask(ctx, c.cfg.Model, state, nil, questions)
}

// ask is the shared implementation used by the builder as well.
func (c *Client) ask(ctx context.Context, model string, state any, images []string, questions map[string]Question) (*Response, error) {
	if len(questions) == 0 {
		return nil, fmt.Errorf("%w\nHint: add at least one question, e.g. AddNoul(\"ok\", \"Did the step succeed?\", 0.8, &ok)", ErrNoQuestions)
	}
	if isEmptyState(state) {
		return nil, invalid("state is required and must not be empty",
			"pass the content to evaluate: a string, a JSON value, or chat messages")
	}
	wq := make(map[string]wireQuestion, len(questions))
	for id, q := range questions {
		if err := q.Validate(); err != nil {
			return nil, err
		}
		wq[id] = q.toWire()
	}
	wr := wireRequest{Model: model, State: state, Images: images, Questions: wq}
	body, err := marshalRequest(wr)
	if err != nil {
		return nil, err
	}
	req, err := c.newRequest(ctx, http.MethodPost, c.cfg.BaseURL, body)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	respBody, err := c.do(req)
	if err != nil {
		return nil, err
	}
	var wire wireResponse
	if err := json.Unmarshal(respBody, &wire); err != nil {
		return nil, fmt.Errorf("systemone: decoding response: %w\nHint: the server returned a non-JSON body — verify the endpoint is llama.cpp /v1/systemone", err)
	}
	return wire.toResponse(time.Since(start)), nil
}

// isEmptyState reports nil/empty-string/empty-collection states.
func isEmptyState(state any) bool {
	switch v := state.(type) {
	case nil:
		return true
	case string:
		return v == ""
	case []byte:
		return len(v) == 0
	case []any:
		return len(v) == 0
	case []map[string]any:
		return len(v) == 0
	case map[string]any:
		return len(v) == 0
	}
	return false
}

// Health probes GET /health. A nil error means the model is ready.
// A 503 surfaces as an *APIError with IsLoading() == true.
func (c *Client) Health(ctx context.Context) error {
	req, err := c.newRequest(ctx, http.MethodGet, c.origin()+"/health", nil)
	if err != nil {
		return err
	}
	_, err = c.do(req)
	return err
}

// Models lists model ids served by the backend (GET /v1/models).
// In router mode this lists every model available for the "model" field.
func (c *Client) Models(ctx context.Context) ([]Model, error) {
	req, err := c.newRequest(ctx, http.MethodGet, c.origin()+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	body, err := c.do(req)
	if err != nil {
		return nil, err
	}
	var env struct {
		Object string  `json:"object"`
		Data   []Model `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("systemone: decoding /v1/models: %w\nHint: verify the endpoint is a llama.cpp server", err)
	}
	return env.Data, nil
}

// Props fetches server-wide properties (GET /props), including
// modalities.vision for image support.
func (c *Client) Props(ctx context.Context) (*Props, error) {
	req, err := c.newRequest(ctx, http.MethodGet, c.origin()+"/props", nil)
	if err != nil {
		return nil, err
	}
	body, err := c.do(req)
	if err != nil {
		return nil, err
	}
	p := Props{}
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("systemone: decoding /props: %w\nHint: verify the endpoint is a llama.cpp server", err)
	}
	return &p, nil
}
