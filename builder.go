package systemone

import (
	"context"
	"fmt"
)

// Step Builder: staged Fluent API that enforces the correct assembly order at
// compile time — state first, then questions, then execute (the Ent-style
// stage-interface pattern). A builder is a single-use value; do not reuse one
// builder concurrently across goroutines.
//
//	client.Decide().
//	    WithTextState("...").
//	    AddChoice("route", "Which team?", map[string]string{"billing": "payments"}, &route).
//	    AddNoul("angry", "Is the customer angry?", 0.8, &isAngry).
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

// QuestionStep is the second builder stage: add questions, then Execute.
// Execute with zero questions fails locally with ErrNoQuestions.
type QuestionStep interface {
	// AddChoice adds a multiple-choice question. When target is non-nil it
	// receives the winning option; it stays zero when a min-confidence
	// threshold is not met (Passed=false).
	AddChoice(id, instructions string, criteria map[string]string, target *string, opts ...ChoiceOption) QuestionStep
	// AddScore adds a graded question; levels must be 2–10 descriptions
	// ordered lowest → highest. target (optional) receives the expected
	// level index.
	AddScore(id, instructions string, levels []string, target *float64) QuestionStep
	// AddNoul adds a yes/no question. threshold in (0,1] decides Passed
	// (P(true) ≥ threshold); values ≤ 0 use the default 0.5.
	// target (optional) receives Passed.
	AddNoul(id, instructions string, threshold float64, target *bool) QuestionStep
	// Execute validates locally, sends one request, and fills all bound
	// targets. The full typed Response is returned for query access
	// (ans.Choice("route"), ans.Noul("angry"), ans.Score("urgency"), …).
	Execute(ctx context.Context) (*Response, error)
}

// ChoiceOption customises a choice question added via [QuestionStep.AddChoice].
type ChoiceOption func(*choiceOpts)

type choiceOpts struct {
	minConfidence float64
	hasMinConf    bool
}

// WithMinConfidence requires the answer's confidence to be at least c for the
// answer to pass; below it, Passed=false and the bound target stays zero.
func WithMinConfidence(c float64) ChoiceOption {
	return func(o *choiceOpts) {
		o.minConfidence = c
		o.hasMinConf = true
	}
}

// Decide starts a Step Builder chain.
func (c *Client) Decide() StateStep {
	return &builder{client: c}
}

type boundQuestion struct {
	q       Question
	kind    string // choice|score|noul
	strTgt  *string
	fltTgt  *float64
	boolTgt *bool
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

func (b *builder) AddChoice(id, instructions string, criteria map[string]string, target *string, opts ...ChoiceOption) QuestionStep {
	b.ensureQuestions()
	co := &choiceOpts{}
	for _, o := range opts {
		o(co)
	}
	b.questions[id] = &boundQuestion{
		q:       Choice{QID: id, Instructions: instructions, Criteria: criteria},
		kind:    "choice",
		strTgt:  target,
		minConf: co.minConfidence,
		hasMin:  co.hasMinConf,
	}
	return b
}

func (b *builder) AddScore(id, instructions string, levels []string, target *float64) QuestionStep {
	b.ensureQuestions()
	b.questions[id] = &boundQuestion{
		q:      Score{QID: id, Instructions: instructions, Criteria: levels},
		kind:   "score",
		fltTgt: target,
	}
	return b
}

func (b *builder) AddNoul(id, instructions string, threshold float64, target *bool) QuestionStep {
	b.ensureQuestions()
	if threshold <= 0 {
		threshold = defaultNoulThreshold
	}
	b.questions[id] = &boundQuestion{
		q:       Noul{QID: id, Instructions: instructions},
		kind:    "noul",
		boolTgt: target,
		thr:     threshold,
	}
	return b
}

// Execute validates locally, resolves images to data URLs, sends the request,
// applies thresholds, and fills bound targets.
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

// apply judges thresholds and writes bound targets.
func (b *builder) apply(resp *Response) {
	for id, bq := range b.questions {
		a := resp.Answers[id]
		if a == nil {
			continue // server answered no such question; leave targets zero
		}
		switch bq.kind {
		case "choice":
			passed := true
			if bq.hasMin && a.Confidence < bq.minConf {
				passed = false // target stays zero (zero-value + Passed=false)
			}
			a.Passed = passed
			if bq.strTgt != nil && passed {
				*bq.strTgt = a.Choice
			}
		case "score":
			a.Passed = true
			if bq.fltTgt != nil {
				*bq.fltTgt = a.Score
			}
		case "noul":
			a.Passed = a.Noul >= bq.thr
			if bq.boolTgt != nil {
				*bq.boolTgt = a.Passed
			}
		}
	}
}
