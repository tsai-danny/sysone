package sysone

import (
	"encoding/json"
	"fmt"
	"time"
)

// Question is the interface implemented by [Choice], [Score] and [Noul].
// Names are aligned with the official TypeSafe SDKs (Choice/Score/Noul,
// Instructions/Criteria) and the wire JSON of POST /v1/sysone.
type Question interface {
	// ID returns the question id used as the key in the questions map.
	ID() string
	// Validate checks the question against the API contract.
	Validate() error
	// toWire converts the question to its wire representation.
	toWire() wireQuestion
}

// Choice is a multiple-choice question: the model picks exactly one option.
// Criteria maps each option to a description; an empty description is sent
// as null on the wire. The criteria map must not be empty.
type Choice struct {
	QID          string
	Instructions string
	Criteria     map[string]string
}

func (q Choice) ID() string { return q.QID }

func (q Choice) Validate() error {
	if q.QID == "" {
		return invalid("choice question has an empty id",
			"give every question a non-empty id, e.g. \"route\"")
	}
	if len(q.Criteria) == 0 {
		return invalid(fmt.Sprintf("choice question %q has no options", q.QID),
			"add at least one option, e.g. Criteria: {\"billing\": \"payments, refunds\"}")
	}
	return nil
}

func (q Choice) toWire() wireQuestion {
	crit := make(map[string]*string, len(q.Criteria))
	for opt, desc := range q.Criteria {
		if desc == "" {
			crit[opt] = nil // null description on the wire
			continue
		}
		d := desc
		crit[opt] = &d
	}
	return wireQuestion{Type: "choice", Instructions: q.Instructions, Criteria: crit}
}

// Score is a graded question: Criteria holds 2–10 level descriptions ordered
// from lowest to highest. The answer is the probability-weighted expected
// level index (fractional), not a label.
type Score struct {
	QID          string
	Instructions string
	Criteria     []string
}

func (q Score) ID() string { return q.QID }

func (q Score) Validate() error {
	if q.QID == "" {
		return invalid("score question has an empty id",
			"give every question a non-empty id, e.g. \"urgency\"")
	}
	if n := len(q.Criteria); n < 2 || n > 10 {
		return invalid(fmt.Sprintf("score question %q has %d levels, want 2–10", q.QID, n),
			"provide 2–10 level descriptions ordered lowest → highest, e.g. [\"can wait\",\"this week\",\"today\",\"right now\"]")
	}
	return nil
}

func (q Score) toWire() wireQuestion {
	return wireQuestion{Type: "score", Instructions: q.Instructions, Criteria: q.Criteria}
}

// Noul is a yes/no question. The answer is the probability that the answer is
// true. Criteria is optional and may describe the true/false cases with the
// keys "true" and "false".
type Noul struct {
	QID          string
	Instructions string
	Criteria     map[string]string // optional; keys "true" and/or "false"
}

func (q Noul) ID() string { return q.QID }

func (q Noul) Validate() error {
	if q.QID == "" {
		return invalid("noul question has an empty id",
			"give every question a non-empty id, e.g. \"angry\"")
	}
	for k := range q.Criteria {
		if k != "true" && k != "false" {
			return invalid(fmt.Sprintf("noul question %q has criteria key %q, want \"true\" or \"false\"", q.QID, k),
				"noul criteria may only use the keys \"true\" and \"false\"")
		}
	}
	return nil
}

func (q Noul) toWire() wireQuestion {
	var crit any
	if len(q.Criteria) > 0 {
		crit = q.Criteria
	}
	return wireQuestion{Type: "noul", Instructions: q.Instructions, Criteria: crit}
}

// Answer is the typed result for one question.
type Answer struct {
	ID   string
	Type string // "choice" | "score" | "noul"

	// Choice fields:
	Choice        string             // winning option (highest probability)
	Probabilities map[string]float64 // choice: option → p (sums to 1); score: "0".."9" → p
	Confidence    float64            // 0–1; 0 means all options equally likely

	// Score fields:
	Score  float64           // expected level index (fractional)
	Legend map[string]string // level index → description

	// Noul field:
	Noul float64 // P(true)

	// Passed is judged client-side against the question's threshold
	// (noul default 0.5; choice: optional min-confidence; score: not applicable).
	Passed bool
}

// Usage reports prompt accounting. OutputTokens is always 0 for decision models.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Response is the typed result of one /v1/systemone request.
type Response struct {
	Model   string
	Answers map[string]*Answer
	Usage   Usage
	Latency time.Duration // client-measured round trip
}

// Answer returns the answer for the question id, or nil if absent.
func (r *Response) Answer(id string) *Answer {
	if r == nil {
		return nil
	}
	return r.Answers[id]
}

// Choice returns the winning option of a choice answer ("" if absent).
func (r *Response) Choice(id string) string {
	if a := r.Answer(id); a != nil {
		return a.Choice
	}
	return ""
}

// Score returns the expected level index of a score answer (0 if absent).
func (r *Response) Score(id string) float64 {
	if a := r.Answer(id); a != nil {
		return a.Score
	}
	return 0
}

// Noul returns P(true) of a noul answer (0 if absent).
func (r *Response) Noul(id string) float64 {
	if a := r.Answer(id); a != nil {
		return a.Noul
	}
	return 0
}

// Passed reports whether every answer passed its threshold.
func (r *Response) Passed() bool {
	if r == nil {
		return false
	}
	for _, a := range r.Answers {
		if !a.Passed {
			return false
		}
	}
	return true
}

// Model describes one model served by the backend (GET /v1/models).
type Model struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	OwnedBy string `json:"owned_by"`
}

// Props is the server-wide property map (GET /props).
type Props map[string]any

// Vision reports whether the server advertises image support
// (modalities.vision).
func (p Props) Vision() bool {
	m, _ := p["modalities"].(map[string]any)
	v, _ := m["vision"].(bool)
	return v
}

// --- wire representations ---

type wireRequest struct {
	Model     string                  `json:"model,omitempty"`
	State     any                     `json:"state"`
	Images    []string                `json:"images,omitempty"`
	Questions map[string]wireQuestion `json:"questions"`
}

type wireQuestion struct {
	Type         string `json:"type"`
	Instructions any    `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

type wireAnswer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice"`
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    float64            `json:"confidence"`
	Score         float64            `json:"score"`
	Legend        map[string]string  `json:"legend"`
	Noul          float64            `json:"noul"`
}

type wireResponse struct {
	Model   string                `json:"model"`
	Answers map[string]wireAnswer `json:"answers"`
	Usage   Usage                 `json:"usage"`
}

// toResponse converts the wire payload into the typed [Response], applying
// default thresholds: noul passes at P(true) ≥ 0.5; choice and score pass
// unless a builder-set threshold says otherwise.
func (w wireResponse) toResponse(latency time.Duration) *Response {
	resp := &Response{
		Model:   w.Model,
		Answers: make(map[string]*Answer, len(w.Answers)),
		Usage:   w.Usage,
		Latency: latency,
	}
	for id, wa := range w.Answers {
		a := &Answer{
			ID:            id,
			Type:          wa.Type,
			Choice:        wa.Choice,
			Probabilities: wa.Probabilities,
			Confidence:    wa.Confidence,
			Score:         wa.Score,
			Legend:        wa.Legend,
			Noul:          wa.Noul,
			Passed:        true,
		}
		if wa.Type == "noul" {
			a.Passed = wa.Noul >= defaultNoulThreshold
		}
		resp.Answers[id] = a
	}
	return resp
}

const defaultNoulThreshold = 0.5

// marshalRequest serialises a wireRequest; it fails only for unmarshalable
// state values (e.g. channels, funcs).
func marshalRequest(wr wireRequest) ([]byte, error) {
	b, err := json.Marshal(wr)
	if err != nil {
		return nil, invalid(fmt.Sprintf("state cannot be serialised to JSON: %v", err),
			"use a string, a JSON-serialisable value, or chat messages as state")
	}
	return b, nil
}
