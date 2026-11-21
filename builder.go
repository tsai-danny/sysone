package sysone

import (
	"context"
	"fmt"
)

// Step Builder: staged Fluent API that enforces the correct assembly order at
// compile time — state first, then questions, then execute (the Ent-style
// stage-interface pattern). Each question is a named-field struct literal, so
// every value at the call site carries its own label. A builder is a
// single-use value; do not reuse one builder concurrently across goroutines.
//
//	client.Decide().
//	    WithTextState("...").
//	    AddChoice(sysone.ChoiceQ{
//	        ID: "route", Instructions: "Which team?",
//	        Options: []sysone.Option{{Key: "billing", Description: "payments"}},
//	    }).
//	    AddNoul(sysone.NoulQ{ID: "angry", Instructions: "Is the customer angry?", Threshold: 0.8}).
//	    Execute(ctx)

// StateStep is the first builder stage: provide the state (and optionally
// images) before any question can be added.
type StateStep interface {
	// WithTextState sets a plain-text state and advances to the question stage.
	WithTextState(text string) QuestionStep
	// WithJSONState sets a structured state; non-string values are serialised
	// to JSON text for the model. Advances to the question stage.
	WithJSONState(v any) QuestionStep
	// WithMessages sets chat-message structured state. image_url parts
	// (data URLs only) inside message content are treated as images by the
	// backend. Advances to the question stage.
	WithMessages(msgs []map[string]any) QuestionStep
	// WithImageState attaches one image (local file path or data URL; local
	// files are base64-encoded automatically) and advances to the question
	// stage. If no text state is set, a generic image-evaluation state is used.
	WithImageState(pathOrDataURL string) QuestionStep
	// WithImages attaches images (local paths and/or data URLs) and stays in
	// the state stage so a text/JSON state can follow.
	WithImages(pathsOrDataURLs ...string) StateStep
}

// Option is one choice option: Key is the answer value returned on a pass,
// Description helps the model judge the option (may be empty).
type Option struct {
	Key         string
	Description string
}

// ChoiceQ is a multiple-choice question for [QuestionStep.AddChoice].
type ChoiceQ struct {
	ID           string
	Instructions string
	// Options are ordered and must contain at least one entry.
	Options []Option
	// MinConfidence requires the answer's confidence to be at least this
	// value to pass (Passed=false below it). 0 means no minimum.
	MinConfidence float64
}

// ScoreQ is a graded question for [QuestionStep.AddScore]. The answer is the
// probability-weighted expected level index (fractional); read it with
// [Response.Score].
type ScoreQ struct {
	ID           string
	Instructions string
	// Levels are 2–10 descriptions ordered lowest → highest.
	Levels []string
}

// NoulQ is a yes/no question for [QuestionStep.AddNoul]. The answer is
// P(true); read it with [Response.Noul].
type NoulQ struct {
	ID           string
	Instructions string
	// Threshold in (0,1] decides Passed (P(true) ≥ threshold).
	// Values ≤ 0 use the default 0.5.
	Threshold float64
}

// QuestionStep is the second builder stage: add questions, then Execute.
// Execute with zero questions fails locally with ErrNoQuestions. Answers are
// read from the returned [Response] (ans.Choice("route"), ans.Noul("angry"),
// ans.Score("urgency"), ans.Answers[id].Passed).
type QuestionStep interface {
	// AddChoice adds a multiple-choice question.
	AddChoice(q ChoiceQ) QuestionStep
	// AddScore adds a graded question.
	AddScore(q ScoreQ) QuestionStep
	// AddNoul adds a yes/no question.
	AddNoul(q NoulQ) QuestionStep
	// Execute validates locally, sends one request, and applies thresholds.
	Execute(ctx context.Context) (*Response, error)
}

// Decide starts a Step Builder chain.
func (c *Client) Decide() StateStep {
	return &builder{client: c}
}

type boundQuestion struct {
	q       Question
	kind    string  // choice|score|noul
	thr     float64 // noul threshold (already defaulted)
	minConf float64
	hasMin  bool
}

type builder struct {
	client    *Client
	model     string
	state     any
	images    []string
	questions map[string]*boundQuestion
}

func (b *builder) WithTextState(text string) QuestionStep {
	b.state = text
	return b
}

func (b *builder) WithJSONState(v any) QuestionStep {
	b.state = v
	return b
}

func (b *builder) WithMessages(msgs []map[string]any) QuestionStep {
	b.state = msgs
	return b
}

func (b *builder) WithImageState(pathOrDataURL string) QuestionStep {
	b.addImage(pathOrDataURL)
	if isEmptyState(b.state) {
		b.state = "Evaluate the provided image(s)."
	}
	return b
}

func (b *builder) WithImages(pathsOrDataURLs ...string) StateStep {
	for _, p := range pathsOrDataURLs {
		b.addImage(p)
	}
	return b
}

func (b *builder) addImage(p string) {
	if b.images == nil {
		b.images = []string{}
	}
	b.images = append(b.images, p) // resolved to data URLs at Execute
}

func (b *builder) ensureQuestions() {
	if b.questions == nil {
		b.questions = map[string]*boundQuestion{}
	}
}

func (b *builder) AddChoice(q ChoiceQ) QuestionStep {
	b.ensureQuestions()
	criteria := make(map[string]string, len(q.Options))
	for _, o := range q.Options {
		criteria[o.Key] = o.Description
	}
	b.questions[q.ID] = &boundQuestion{
		q:       Choice{QID: q.ID, Instructions: q.Instructions, Criteria: criteria},
		kind:    "choice",
		minConf: q.MinConfidence,
		hasMin:  q.MinConfidence > 0,
	}
	return b
}

func (b *builder) AddScore(q ScoreQ) QuestionStep {
	b.ensureQuestions()
	b.questions[q.ID] = &boundQuestion{
		q:    Score{QID: q.ID, Instructions: q.Instructions, Criteria: q.Levels},
		kind: "score",
	}
	return b
}

func (b *builder) AddNoul(q NoulQ) QuestionStep {
	b.ensureQuestions()
	thr := q.Threshold
	if thr <= 0 {
		thr = defaultNoulThreshold
	}
	b.questions[q.ID] = &boundQuestion{
		q:    Noul{QID: q.ID, Instructions: q.Instructions},
		kind: "noul",
		thr:  thr,
	}
	return b
}

// Execute validates locally, resolves images to data URLs, sends the request,
// and applies thresholds.
func (b *builder) Execute(ctx context.Context) (*Response, error) {
	if len(b.questions) == 0 {
		return nil, fmt.Errorf("%w\nHint: add at least one question with AddChoice / AddScore / AddNoul before Execute", ErrNoQuestions)
	}
	// Resolve images (local paths → data URLs) before any network work.
	images := make([]string, 0, len(b.images))
	for _, img := range b.images {
		dataURL, err := resolveImage(ctx, img, b.client.cfg.MaxImageBytes)
		if err != nil {
			return nil, err
		}
		images = append(images, dataURL)
	}
	plain := make(map[string]Question, len(b.questions))
	for id, bq := range b.questions {
		plain[id] = bq.q
	}
	resp, err := b.client.ask(ctx, b.client.cfg.Model, b.state, images, plain)
	if err != nil {
		return nil, err
	}
	b.apply(resp)
	return resp, nil
}

// apply judges thresholds: noul uses the local threshold, choice uses the
// optional minimum confidence. Answers are read from the Response.
func (b *builder) apply(resp *Response) {
	for id, bq := range b.questions {
		a := resp.Answers[id]
		if a == nil {
			continue // server answered no such question
		}
		switch bq.kind {
		case "choice":
			a.Passed = !bq.hasMin || a.Confidence >= bq.minConf
		case "score":
			a.Passed = true
		case "noul":
			a.Passed = a.Noul >= bq.thr
		}
	}
}
