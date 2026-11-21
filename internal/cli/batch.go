package cli

import (
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tsai-danny/sysone"
)

func newBatchCmd(rs *runState) *cobra.Command {
	f := &qFlags{}
	var nouls, choices, scores []string
	cmd := &cobra.Command{
		Use:   "batch",
		Short: "Multiple questions in one request",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			f.markThreshold(cmd)
			state, err := f.resolveState(rs)
			if err != nil {
				return err
			}
			if len(nouls) == 0 && len(choices) == 0 && len(scores) == 0 {
				return &usageError{cmd: cmd,
					msg: "batch needs at least one of --noul / --choice / --score",
					example: `sysone batch --state @ctx.json --noul "is_urgent:是否緊急?" ` +
						`--choice "route:分派:前端|後端|SRE" --score "conf:完整度:低:中:高"`}
			}
			c, err := f.client()
			if err != nil {
				return err
			}
			step := f.applyState(f.startBuilder(c), state)
			for _, spec := range nouls {
				id, instr, rest, ok := splitSpec(spec, 1)
				if !ok {
					return &usageError{cmd: cmd, msg: `--noul needs "id:instructions", got ` + spec}
				}
				_ = rest
				step = step.AddNoul(sysone.NoulQ{ID: id, Instructions: instr, Threshold: f.threshold})
			}
			for _, spec := range choices {
				id, instr, optsSpec, ok := splitSpec(spec, 2)
				if !ok {
					return &usageError{cmd: cmd, msg: `--choice needs "id:instructions:opt1|opt2", got ` + spec}
				}
				opts, err := parseOptions(optsSpec)
				if err != nil {
					return err
				}
				minConf := 0.0
				if f.thresholdSet {
					minConf = f.threshold
				}
				step = step.AddChoice(sysone.ChoiceQ{ID: id, Instructions: instr, Options: opts, MinConfidence: minConf})
			}
			for _, spec := range scores {
				id, instr, rest, ok := splitSpec(spec, 2)
				if !ok {
					return &usageError{cmd: cmd, msg: `--score needs "id:instructions:level1:level2:...", got ` + spec}
				}
				levels, err := parseScale(strings.ReplaceAll(rest, ":", ","))
				if err != nil {
					return err
				}
				step = step.AddScore(sysone.ScoreQ{ID: id, Instructions: instr, Levels: levels})
			}
			resp, err := step.Execute(cmd.Context())
			if err != nil {
				return err
			}
			return finish(rs, f, resp, false)
		},
	}
	f.register(cmd)
	cmd.Flags().StringArrayVar(&nouls, "noul", nil, `noul question "id:instructions" (repeatable)`)
	cmd.Flags().StringArrayVar(&choices, "choice", nil, `choice question "id:instructions:opt1:desc|opt2:desc" (repeatable)`)
	cmd.Flags().StringArrayVar(&scores, "score", nil, `score question "id:instructions:level1:level2:..." (repeatable)`)
	markRequired(cmd, "state")
	return cmd
}

// splitSpec splits s on ":" into id, instructions, and the remainder
// (everything after the n-th colon, colons preserved). ok=false when the
// separators are missing.
func splitSpec(s string, n int) (id, instr, rest string, ok bool) {
	parts := strings.SplitN(s, ":", n+1)
	if len(parts) < n+1 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", "", false
	}
	if n == 1 {
		return parts[0], parts[1], "", true
	}
	return parts[0], parts[1], parts[2], true
}

func newAskCmd(rs *runState) *cobra.Command {
	f := &qFlags{}
	var questionsFile string
	cmd := &cobra.Command{
		Use:   "ask",
		Short: "Raw escape hatch: questions from a JSON file (maps to module Ask)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if questionsFile == "" {
				return &usageError{cmd: cmd,
					msg:     "--questions is required (JSON file, @file, or - for stdin)",
					example: `sysone ask --state @ctx.json --questions @questions.json`}
			}
			state, err := f.resolveState(rs)
			if err != nil {
				return err
			}
			qs, err := loadQuestions(rs, questionsFile)
			if err != nil {
				return err
			}
			c, err := f.client()
			if err != nil {
				return err
			}
			resp, err := c.Ask(cmd.Context(), state, qs)
			if err != nil {
				return err
			}
			return finish(rs, f, resp, false)
		},
	}
	f.register(cmd)
	cmd.Flags().StringVar(&questionsFile, "questions", "", `questions JSON: @file, "-" for stdin, or inline JSON`)
	markRequired(cmd, "state", "questions")
	return cmd
}

// loadQuestions reads the questions spec:
//
//	{"route": {"type":"choice","instructions":"...","criteria":{"a":"..."}},
//	 "ok":    {"type":"noul","instructions":"Did it work?"},
//	 "risk":  {"type":"score","instructions":"...","criteria":["低","中","高"]}}
func loadQuestions(rs *runState, spec string) (map[string]sysone.Question, error) {
	var raw []byte
	switch {
	case spec == "-":
		b, err := io.ReadAll(rs.stdin)
		if err != nil {
			return nil, &usageError{msg: "reading stdin: " + err.Error()}
		}
		raw = b
	case strings.HasPrefix(spec, "@"):
		b, err := os.ReadFile(spec[1:])
		if err != nil {
			return nil, &usageError{msg: "reading questions file: " + err.Error()}
		}
		raw = b
	default:
		raw = []byte(spec)
	}
	var wire map[string]struct {
		Type         string          `json:"type"`
		Instructions string          `json:"instructions"`
		Criteria     json.RawMessage `json:"criteria"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, &usageError{msg: "questions JSON is invalid: " + err.Error()}
	}
	qs := make(map[string]sysone.Question, len(wire))
	for id, w := range wire {
		switch w.Type {
		case "choice":
			var crit map[string]string
			if err := json.Unmarshal(w.Criteria, &crit); err != nil {
				return nil, &usageError{msg: `question "` + id + `": choice criteria must be {"option":"desc"}`}
			}
			qs[id] = sysone.Choice{QID: id, Instructions: w.Instructions, Criteria: crit}
		case "score":
			var crit []string
			if err := json.Unmarshal(w.Criteria, &crit); err != nil {
				return nil, &usageError{msg: `question "` + id + `": score criteria must be ["level",...]`}
			}
			qs[id] = sysone.Score{QID: id, Instructions: w.Instructions, Criteria: crit}
		case "noul":
			var crit map[string]string
			if len(w.Criteria) > 0 {
				if err := json.Unmarshal(w.Criteria, &crit); err != nil {
					return nil, &usageError{msg: `question "` + id + `": noul criteria must be {"true":"...","false":"..."}`}
				}
			}
			qs[id] = sysone.Noul{QID: id, Instructions: w.Instructions, Criteria: crit}
		default:
			return nil, &usageError{msg: `question "` + id + `": type must be "choice", "score" or "noul"`}
		}
	}
	return qs, nil
}
