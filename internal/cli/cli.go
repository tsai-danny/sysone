// Package cli implements the sysone command-line interface: thin cobra
// wrappers over the systemone module with a stable machine-readable output
// contract (JSON on stdout, diagnostics on stderr, fixed exit codes).
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/sysone/systemone"
)

// Exit codes ??a stable contract for scripted (agent) callers (PRD 禮6.4).
const (
	ExitOK        = 0 // success, every answer passed its threshold
	ExitFailed    = 1 // valid answers, at least one answer below threshold
	ExitUsage     = 2 // flag / argument / structure error
	ExitTransport = 3 // connection failure, server error, 503 loading
	ExitContract  = 4 // contract error: backend 400/501, local validation
)

// usageError marks argument/structure problems that map to ExitUsage.
type usageError struct {
	msg     string
	cmd     *cobra.Command
	example string
}

func (e *usageError) Error() string { return e.msg }

// runState carries per-invocation streams and the resolved exit code.
type runState struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	exit   int
	help   bool
}

// Execute runs the CLI with the given args (without program name) and returns
// the process exit code. It never calls os.Exit and never writes data to
// stderr or diagnostics to stdout.
func Execute(args []string, stdout, stderr io.Writer) int {
	return execute(args, os.Stdin, stdout, stderr)
}

func execute(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	rs := &runState{stdin: stdin, stdout: stdout, stderr: stderr, exit: ExitOK}
	root := newRoot(rs)
	root.SetArgs(args)
	root.SetIn(stdin)

	err := root.Execute()
	if rs.help {
		return ExitOK
	}
	if err == nil {
		return rs.exit
	}

	var ue *usageError
	if errors.As(err, &ue) {
		fmt.Fprintf(stderr, "sysone: %s\n", ue.msg)
		if ue.example != "" {
			fmt.Fprintf(stderr, "Example:\n  %s\n", ue.example)
		}
		if ue.cmd != nil {
			fmt.Fprint(stderr, ue.cmd.UsageString())
		}
		return ExitUsage
	}
	// Cobra's own command-resolution errors (unknown command, extra args).
	if strings.HasPrefix(err.Error(), "unknown command") {
		fmt.Fprintf(stderr, "sysone: %s\n", err)
		fmt.Fprint(stderr, root.UsageString())
		return ExitUsage
	}
	fmt.Fprintf(stderr, "sysone: %s\n", err)
	return exitCodeForError(err)
}

// exitCodeForError maps a runtime error to its contract exit code.
func exitCodeForError(err error) int {
	var ae *systemone.APIError
	if errors.As(err, &ae) {
		if ae.IsContract() {
			return ExitContract
		}
		return ExitTransport // 503 loading and all other server errors
	}
	if errors.Is(err, systemone.ErrInvalidRequest) || errors.Is(err, systemone.ErrNoQuestions) {
		return ExitContract
	}
	return ExitTransport
}

func newRoot(rs *runState) *cobra.Command {
	root := &cobra.Command{
		Use:   "sysone",
		Short: "Agent-friendly CLI for the llama.cpp System One decision-model API",
		Long: "sysone asks typed questions (choice / score / noul) about a state\n" +
			"using a System One decision model served by llama.cpp (POST /v1/systemone).\n\n" +
			"stdout is JSON only; stderr is diagnostics only. Run `sysone agent-help`\n" +
			"for the full machine-readable contract.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return &usageError{cmd: c, msg: "no command given",
				example: `sysone noul --state "OOMKilled in log" -i "Should the container restart?" --threshold 0.8`}
		},
	}
	root.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return &usageError{cmd: c, msg: err.Error()}
	})
	root.SetHelpFunc(func(c *cobra.Command, _ []string) {
		rs.help = true
		fmt.Fprint(rs.stdout, c.UsageString())
	})
	root.CompletionOptions.DisableDefaultCmd = true

	root.AddCommand(
		newNoulCmd(rs),
		newChoiceCmd(rs),
		newScoreCmd(rs),
		newBatchCmd(rs),
		newAskCmd(rs),
		newHealthCmd(rs),
		newModelsCmd(rs),
		newPropsCmd(rs),
		newAgentHelpCmd(rs),
	)
	return root
}

// --- output contract helpers ---

// emitJSON writes v to w as compact JSON (indented when pretty) with a
// trailing newline.
func emitJSON(w io.Writer, v any, pretty bool) error {
	enc := json.NewEncoder(w)
	if pretty {
		enc.SetIndent("", "  ")
	}
	return enc.Encode(v)
}

// envelope builds the stable question-command output:
// {"model":...,"answers":{id:{...}},"usage":{...},"latency_ms":N}.
func envelope(resp *systemone.Response) map[string]any {
	answers := make(map[string]any, len(resp.Answers))
	for id, a := range resp.Answers {
		answers[id] = answerJSON(a)
	}
	return map[string]any{
		"model":      resp.Model,
		"answers":    answers,
		"usage":      resp.Usage,
		"latency_ms": resp.Latency.Milliseconds(),
	}
}

// answerJSON serialises one answer with the core Answer field names,
// including only the fields meaningful for its type.
func answerJSON(a *systemone.Answer) map[string]any {
	m := map[string]any{"id": a.ID, "type": a.Type, "passed": a.Passed}
	switch a.Type {
	case "choice":
		m["choice"] = a.Choice
		m["probabilities"] = a.Probabilities
		m["confidence"] = a.Confidence
	case "score":
		m["score"] = a.Score
		m["legend"] = a.Legend
		m["probabilities"] = a.Probabilities
		m["confidence"] = a.Confidence
	case "noul":
		m["noul"] = a.Noul
	}
	return m
}

// quietValue renders the bare answer value for --quiet shell piping.
func quietValue(a *systemone.Answer) string {
	switch a.Type {
	case "choice":
		return a.Choice
	case "score":
		return strconv.FormatFloat(a.Score, 'g', -1, 64)
	default: // noul
		return strconv.FormatFloat(a.Noul, 'g', -1, 64)
	}
}

func sortedIDs(resp *systemone.Response) []string {
	ids := make([]string, 0, len(resp.Answers))
	for id := range resp.Answers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// finish writes the response (JSON envelope, or bare values when quiet) and
// records the exit code from the passed judgement. single prints one bare
// value without the id prefix under --quiet.
func finish(rs *runState, f *qFlags, resp *systemone.Response, single bool) error {
	if f.quiet {
		ids := sortedIDs(resp)
		for _, id := range ids {
			if single && len(ids) == 1 {
				fmt.Fprintln(rs.stdout, quietValue(resp.Answers[id]))
				continue
			}
			fmt.Fprintf(rs.stdout, "%s %s\n", id, quietValue(resp.Answers[id]))
		}
	} else if err := emitJSON(rs.stdout, envelope(resp), f.pretty); err != nil {
		return err
	}
	if resp.Passed() {
		rs.exit = ExitOK
	} else {
		rs.exit = ExitFailed
	}
	return nil
}
