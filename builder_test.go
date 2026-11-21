package systemone

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuilder_FullChain(t *testing.T) {
	var got wireRequest
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(canonicalResponse))
	})

	var route string
	var isAngry bool
	var urgency float64
	resp, err := c.Decide().
		WithTextState("Customer message: I was charged twice.").
		AddChoice("route", "Which team?", map[string]string{
			"billing": "payments", "shipping": "delivery", "technical": "bugs",
		}, &route).
		AddNoul("angry", "Is the customer angry?", 0.8, &isAngry).
		AddScore("urgency", "How urgent?", []string{"can wait", "this week", "today", "right now"}, &urgency).
		Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Wire request sanity.
	if got.State != "Customer message: I was charged twice." {
		t.Errorf("state = %q", got.State)
	}
	if len(got.Questions) != 3 {
		t.Errorf("questions = %d, want 3", len(got.Questions))
	}

	// Bound targets.
	if route != "billing" {
		t.Errorf("route = %q, want billing", route)
	}
	if !isAngry { // noul 0.8208 ≥ 0.8
		t.Error("isAngry = false, want true (0.8208 ≥ 0.8)")
	}
	if urgency != 2.2821 {
		t.Errorf("urgency = %v, want 2.2821", urgency)
	}

	// Query access on the typed response.
	if resp.Choice("route") != "billing" {
		t.Errorf("resp.Choice = %q", resp.Choice("route"))
	}
	if resp.Noul("angry") < 0.8 {
		t.Errorf("resp.Noul(angry) = %v, want ≥ 0.8", resp.Noul("angry"))
	}
	if a := resp.Answers["angry"]; !a.Passed {
		t.Error("angry.Passed = false, want true")
	}
}

func TestBuilder_NoulThresholds(t *testing.T) {
	// noul = 0.42: fails 0.8 and default 0.5, passes 0.4.
	body := `{"model":"m","answers":{"a":{"type":"noul","noul":0.42},"b":{"type":"noul","noul":0.42},"c":{"type":"noul","noul":0.42}},"usage":{"input_tokens":1,"output_tokens":0}}`
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	})

	var a, b, cc bool
	_, err := c.Decide().
		WithTextState("s").
		AddNoul("a", "q", 0.8, &a).
		AddNoul("b", "q", 0, &b). // default 0.5
		AddNoul("c", "q", 0.4, &cc).
		Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if a {
		t.Error("a: 0.42 ≥ 0.8 should fail")
	}
	if b {
		t.Error("b: 0.42 ≥ default 0.5 should fail")
	}
	if !cc {
		t.Error("c: 0.42 ≥ 0.4 should pass")
	}
}

func TestBuilder_MinConfidence(t *testing.T) {
	body := `{"model":"m","answers":{"route":{"type":"choice","choice":"billing","probabilities":{"billing":0.5,"shipping":0.5},"confidence":0.0}},"usage":{"input_tokens":1,"output_tokens":0}}`
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	})

	var route string
	resp, err := c.Decide().
		WithTextState("s").
		AddChoice("route", "q", map[string]string{"billing": "b", "shipping": "s"}, &route,
			WithMinConfidence(0.6)).
		Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if route != "" {
		t.Errorf("route = %q, want zero (below min confidence)", route)
	}
	if resp.Answers["route"].Passed {
		t.Error("Passed = true, want false below min confidence")
	}
	// The winning option is still readable for query access.
	if resp.Answers["route"].Choice != "billing" {
		t.Errorf("Choice = %q, want billing", resp.Answers["route"].Choice)
	}
}

func TestBuilder_LocalValidation(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("server must not be called for local validation failures")
	})
	ctx := context.Background()

	// No questions.
	_, err := c.Decide().WithTextState("s").Execute(ctx)
	if !errors.Is(err, ErrNoQuestions) {
		t.Errorf("no questions: err = %v", err)
	}
	// Empty state.
	_, err = c.Decide().WithTextState("").AddNoul("a", "q", 0.5, nil).Execute(ctx)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("empty state: err = %v", err)
	}
	// Invalid question (empty choice criteria).
	_, err = c.Decide().WithTextState("s").AddChoice("x", "q", map[string]string{}, nil).Execute(ctx)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("empty criteria: err = %v", err)
	}
	// Invalid question (score with 1 level).
	_, err = c.Decide().WithTextState("s").AddScore("x", "q", []string{"only"}, nil).Execute(ctx)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("one level: err = %v", err)
	}
}

func TestBuilder_Images(t *testing.T) {
	var got wireRequest
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"model":"m","answers":{"ok":{"type":"noul","noul":0.9}},"usage":{"input_tokens":1,"output_tokens":0}}`))
	})

	dir := t.TempDir()
	png := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(png, []byte{0x89, 'P', 'N', 'G'}, 0o644); err != nil {
		t.Fatal(err)
	}
	dataURL := "data:image/gif;base64,R0lGODdh"

	var ok bool
	_, err := c.Decide().
		WithImages(png, dataURL).
		WithTextState("Describe the screenshot.").
		AddNoul("ok", "Is it a screenshot?", 0.5, &ok).
		Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(got.Images) != 2 {
		t.Fatalf("images = %d, want 2", len(got.Images))
	}
	want := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte{0x89, 'P', 'N', 'G'})
	if got.Images[0] != want {
		t.Errorf("image[0] = %q, want %q", got.Images[0], want)
	}
	if got.Images[1] != dataURL {
		t.Errorf("image[1] = %q, want passthrough", got.Images[1])
	}
	if got.State != "Describe the screenshot." {
		t.Errorf("state = %q", got.State)
	}
}

func TestBuilder_ImageStateDefaultText(t *testing.T) {
	var got wireRequest
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"model":"m","answers":{"ok":{"type":"noul","noul":0.9}},"usage":{"input_tokens":1,"output_tokens":0}}`))
	})
	_, err := c.Decide().
		WithImageState("data:image/png;base64,AAAA").
		AddNoul("ok", "q", 0.5, nil).
		Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if s, _ := got.State.(string); !strings.Contains(s, "image") {
		t.Errorf("state = %q, want generic image-evaluation text", got.State)
	}
}

func TestBuilder_ImageErrors(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("server must not be called for image failures")
	})

	// Missing file.
	_, err := c.Decide().WithImageState("nope.png").AddNoul("ok", "q", 0.5, nil).Execute(context.Background())
	if !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("missing file: err = %v", err)
	}

	// Unknown extension.
	dir := t.TempDir()
	txt := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(txt, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = c.Decide().WithImageState(txt).AddNoul("ok", "q", 0.5, nil).Execute(context.Background())
	if !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("unknown ext: err = %v", err)
	}
	if !strings.Contains(err.Error(), "Hint:") {
		t.Errorf("error missing Hint: %v", err)
	}

	// Over the size cap.
	c2, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {})
	c2.cfg.MaxImageBytes = 2
	png := filepath.Join(dir, "big.png")
	if err := os.WriteFile(png, []byte("1234567890"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = c2.Decide().WithImageState(png).AddNoul("ok", "q", 0.5, nil).Execute(context.Background())
	if !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("oversize: err = %v", err)
	}
	if !strings.Contains(err.Error(), "MaxImageBytes") {
		t.Errorf("oversize hint should mention MaxImageBytes: %v", err)
	}
}

func TestBuilder_JSONStateAndMessages(t *testing.T) {
	var got wireRequest
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"model":"m","answers":{"ok":{"type":"noul","noul":0.9}},"usage":{"input_tokens":1,"output_tokens":0}}`))
	})

	// JSON state.
	_, err := c.Decide().
		WithJSONState(map[string]any{"invoice": "INV-9", "total": 42}).
		AddNoul("ok", "q", 0.5, nil).
		Execute(context.Background())
	if err != nil {
		t.Fatalf("json state: %v", err)
	}
	m, ok := got.State.(map[string]any)
	if !ok || m["invoice"] != "INV-9" {
		t.Errorf("json state = %v", got.State)
	}

	// Messages state.
	_, err = c.Decide().
		WithMessages([]map[string]any{{"role": "user", "content": "hi"}}).
		AddNoul("ok", "q", 0.5, nil).
		Execute(context.Background())
	if err != nil {
		t.Fatalf("messages state: %v", err)
	}
	arr, ok := got.State.([]any)
	if !ok || len(arr) != 1 {
		t.Errorf("messages state = %v", got.State)
	}
}

func TestBuilder_RemoteErrorPropagates(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":{"code":503,"message":"model is loading","type":"loading_error"}}`))
	})
	_, err := c.Decide().WithTextState("s").AddNoul("ok", "q", 0.5, nil).Execute(context.Background())
	var api *APIError
	if !errors.As(err, &api) {
		t.Fatalf("err = %v, want APIError", err)
	}
	if !api.IsLoading() {
		t.Error("IsLoading = false")
	}
}

func TestBuilder_MissingAnswerLeavesTargetZero(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"model":"m","answers":{},"usage":{"input_tokens":1,"output_tokens":0}}`))
	})
	var route string
	_, err := c.Decide().
		WithTextState("s").
		AddChoice("route", "q", map[string]string{"a": "a"}, &route).
		Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if route != "" {
		t.Errorf("route = %q, want zero", route)
	}
}
