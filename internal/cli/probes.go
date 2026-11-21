package cli

// Read-only probe commands (health / models / props) and the agent-help
// machine-readable contract (PRD §6.5). These share probeFlags and never
// touch the ask envelope pipeline.

import (
	"net/http"
	"time"

	"github.com/spf13/cobra"
	"github.com/tsai-danny/sysone"
)

// probeFlags carries the flags shared by the read-only probe commands.
type probeFlags struct {
	url     string
	apiKey  string
	model   string
	timeout time.Duration
	pretty  bool
}

func (f *probeFlags) register(cmd *cobra.Command) {
	fs := cmd.Flags()
	fs.StringVar(&f.url, "url", "", "endpoint URL (overrides SYSTEMONE_URL)")
	fs.StringVar(&f.apiKey, "api-key", "", "bearer token (overrides SYSTEMONE_API_KEY)")
	fs.StringVar(&f.model, "model", "", "model id (accepted for symmetry with ask commands)")
	fs.DurationVar(&f.timeout, "timeout", 30*time.Second, "request timeout")
	fs.BoolVar(&f.pretty, "pretty", false, "indent JSON output")
	fs.SetNormalizeFunc(normalizeKeyFlag)
}

func (f *probeFlags) client() (*sysone.Client, error) {
	cfg := sysone.Config{BaseURL: f.url, APIKey: f.apiKey, Model: f.model}
	if f.timeout > 0 {
		cfg.HTTPClient = &http.Client{Timeout: f.timeout}
	}
	return sysone.NewClient(cfg)
}

func newHealthCmd(rs *runState) *cobra.Command {
	f := &probeFlags{}
	cmd := &cobra.Command{
		Use:   "health",
		Short: "Check the server (GET /health)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := f.client()
			if err != nil {
				return err
			}
			if err := c.Health(cmd.Context()); err != nil {
				return err
			}
			return emitJSON(rs.stdout, map[string]any{"status": "ok"}, f.pretty)
		},
	}
	f.register(cmd)
	return cmd
}

func newModelsCmd(rs *runState) *cobra.Command {
	f := &probeFlags{}
	cmd := &cobra.Command{
		Use:   "models",
		Short: "List served models (GET /v1/models)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := f.client()
			if err != nil {
				return err
			}
			models, err := c.Models(cmd.Context())
			if err != nil {
				return err
			}
			return emitJSON(rs.stdout, map[string]any{"models": models}, f.pretty)
		},
	}
	f.register(cmd)
	return cmd
}

func newPropsCmd(rs *runState) *cobra.Command {
	f := &probeFlags{}
	cmd := &cobra.Command{
		Use:   "props",
		Short: "Show server properties incl. vision support (GET /props)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := f.client()
			if err != nil {
				return err
			}
			props, err := c.Props(cmd.Context())
			if err != nil {
				return err
			}
			out := map[string]any{"props": map[string]any(*props), "vision": props.Vision()}
			return emitJSON(rs.stdout, out, f.pretty)
		},
	}
	f.register(cmd)
	return cmd
}

// --- agent-help: machine-readable contract for scripted callers (PRD §6.5.2) ---

func newAgentHelpCmd(rs *runState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent-help",
		Short: "Print the machine-readable CLI contract (JSON)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return emitJSON(rs.stdout, agentHelpSpec(), false)
		},
	}
	return cmd
}

func agentHelpSpec() map[string]any {
	return map[string]any{
		"name":    "sysone",
		"version": "1",
		"contract": map[string]any{
			"stdout": "JSON only (compact; --pretty indents). Question commands emit an envelope: " +
				"{\"model\":str,\"answers\":{id:{id,type,passed,...}},\"usage\":{\"input_tokens\":int,\"output_tokens\":int},\"latency_ms\":int}. " +
				"answer fields by type: choice{choice,probabilities,confidence}, score{score,legend,probabilities,confidence}, noul{noul}.",
			"stderr":       "diagnostics only, never data",
			"quiet":        "--quiet prints bare values: one line per answer (\"id value\"; single-question commands print the bare value alone)",
			"content_type": "application/json",
		},
		"exit_codes": map[string]any{
			"0": "success, every answer passed its threshold",
			"1": "valid answers, at least one answer below threshold",
			"2": "usage error: bad flag, missing argument, unknown command",
			"3": "transport: connection failure, server 5xx, 503 model loading",
			"4": "contract error: backend 400/501, local request validation",
		},
		"commands": []any{
			map[string]any{"name": "noul", "args": "--state S --q Q [--threshold t]", "required": []string{"--state", "--q"}, "summary": "one yes/no question; noul is P(true) in [0,1]"},
			map[string]any{"name": "choice", "args": "--state S --q Q --options \"key:desc|key:desc\" [--threshold t]", "required": []string{"--state", "--q", "--options"}, "summary": "one multiple-choice question; --threshold sets min confidence"},
			map[string]any{"name": "score", "args": "--state S --q Q --scale \"l1,l2,...\"", "required": []string{"--state", "--q", "--scale"}, "summary": "one graded question; 2-10 levels ordered lowest to highest; score is the level index"},
			map[string]any{"name": "batch", "args": "--state S --noul \"id:instr\" --choice \"id:instr:opt1|opt2\" --score \"id:instr:l1:l2\"", "required": []string{"--state"}, "summary": "multiple questions in one request (specs repeatable; at least one question flag required)"},
			map[string]any{"name": "ask", "args": "--state S --questions @file.json", "required": []string{"--state", "--questions"}, "summary": "raw escape hatch: questions as JSON {id:{type,instructions,criteria}}"},
			map[string]any{"name": "health", "args": "", "summary": "GET /health; stdout {\"status\":\"ok\"}"},
			map[string]any{"name": "models", "args": "", "summary": "GET /v1/models; stdout {\"models\":[...]}"},
			map[string]any{"name": "props", "args": "", "summary": "GET /props; stdout {\"props\":{...},\"vision\":bool}"},
		},
		"flags": map[string]any{
			"--url":       "endpoint URL (env SYSTEMONE_URL; default http://localhost:8080/v1/systemone; a bare server root like http://host:8080 is accepted)",
			"--api-key":   "bearer token (env SYSTEMONE_API_KEY; alias --key)",
			"--model":     "model id for router mode (env SYSTEMONE_MODEL)",
			"--state":     "state: text, @file (suffix .json parsed as JSON), or \"-\" for stdin",
			"--image":     "image local path or data URL, repeatable (requires vision server)",
			"--threshold": "pass threshold in (0,1], default 0.5; noul: P(true) >= t; choice: min confidence",
			"--timeout":   "request timeout, Go duration, default 30s",
			"--pretty":    "indent JSON output",
			"--quiet":     "print bare answer values only",
		},
		"examples": []any{
			`sysone noul --state "OOMKilled in pod log" --q "Should the container restart?" --threshold 0.8`,
			`sysone choice --state @ticket.txt --q "Issue category" --options "billing:payments|tech:bugs|returns:refunds" --quiet`,
			`sysone batch --state @ctx.json --noul "is_urgent:Is it urgent?" --choice "route:Team:frontend|backend|sre" --score "conf:Completeness:low:mid:high"`,
		},
	}
}
