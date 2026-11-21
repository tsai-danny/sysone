package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/tsai-danny/sysone"
)

// qFlags carries the common flags shared by every question command (PRD §6.2).
type qFlags struct {
	url       string
	apiKey    string
	model     string
	state     string
	images    []string
	threshold float64
	timeout   time.Duration
	pretty    bool
	quiet     bool
	// thresholdSet records whether --threshold was explicitly given
	// (choice: only then is a min-confidence gate applied).
	thresholdSet bool
}

// register adds the common flags to cmd.
func (f *qFlags) register(cmd *cobra.Command) {
	fs := cmd.Flags()
	fs.StringVar(&f.url, "url", "", "endpoint URL (overrides SYSTEMONE_URL)")
	fs.StringVar(&f.apiKey, "api-key", "", "bearer token (overrides SYSTEMONE_API_KEY)")
	fs.StringVar(&f.model, "model", "", "model id for router mode (overrides SYSTEMONE_MODEL)")
	fs.StringVar(&f.state, "state", "", `state: text, @file (.json → JSON state), or "-" for stdin`)
	fs.StringArrayVar(&f.images, "image", nil, "image: local path or data URL (repeatable)")
	fs.Float64Var(&f.threshold, "threshold", 0.5, "noul pass threshold; choice: min confidence")
	fs.DurationVar(&f.timeout, "timeout", 30*time.Second, "request timeout")
	fs.BoolVar(&f.pretty, "pretty", false, "indent JSON output")
	fs.BoolVar(&f.quiet, "quiet", false, "print bare answer values only (shell piping)")
	fs.SetNormalizeFunc(normalizeKeyFlag)
}

// markRequired declares the flags a command cannot run without. Cobra
// rejects the invocation before RunE when one is missing (execute() maps
// that error to ExitUsage) and we append "[required]" to the usage text so
// --help shows it (pflag does not render the annotation itself). Runtime
// validators (requireQ, resolveState, ...) stay as the second line of
// defense for flags that are present but empty.
func markRequired(cmd *cobra.Command, names ...string) {
	for _, n := range names {
		if f := cmd.Flags().Lookup(n); f != nil {
			f.Usage = f.Usage + " [required]"
		}
		_ = cmd.MarkFlagRequired(n)
	}
}

// normalizeKeyFlag accepts --key as a shorthand for --api-key.
func normalizeKeyFlag(_ *pflag.FlagSet, name string) pflag.NormalizedName {
	if name == "key" {
		name = "api-key"
	}
	return pflag.NormalizedName(name)
}

// client builds the module client from the flags (env fallback lives in the core).
func (f *qFlags) client() (*sysone.Client, error) {
	cfg := sysone.Config{BaseURL: f.url, APIKey: f.apiKey, Model: f.model}
	if f.timeout > 0 {
		cfg.HTTPClient = &http.Client{Timeout: f.timeout}
	}
	return sysone.NewClient(cfg)
}

// markThreshold records explicit --threshold usage after flag parsing.
func (f *qFlags) markThreshold(cmd *cobra.Command) {
	f.thresholdSet = cmd.Flags().Changed("threshold")
}

// resolveState turns the --state flag into the request state value.
func (f *qFlags) resolveState(rs *runState) (any, error) {
	if f.state == "" {
		return nil, &usageError{
			msg:     "--state is required",
			example: `sysone noul --state "OOMKilled in log" --q "Should the container restart?" --threshold 0.8`,
		}
	}
	if f.state == "-" {
		b, err := io.ReadAll(rs.stdin)
		if err != nil {
			return nil, &usageError{msg: "reading stdin: " + err.Error()}
		}
		return string(b), nil
	}
	if strings.HasPrefix(f.state, "@") {
		path := f.state[1:]
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, &usageError{msg: "reading state file: " + err.Error()}
		}
		if strings.HasSuffix(strings.ToLower(path), ".json") {
			var v any
			if err := json.Unmarshal(b, &v); err != nil {
				return nil, &usageError{msg: "state file is not valid JSON: " + err.Error()}
			}
			return v, nil
		}
		return string(b), nil
	}
	return f.state, nil
}

// applyState sets the state on a builder chain (JSON values keep structure).
func (f *qFlags) applyState(step sysone.StateStep, state any) sysone.QuestionStep {
	if s, ok := state.(string); ok {
		return step.WithTextState(s)
	}
	return step.WithJSONState(state)
}

// startBuilder returns the state stage with images attached.
func (f *qFlags) startBuilder(c *sysone.Client) sysone.StateStep {
	step := c.Decide()
	if len(f.images) > 0 {
		step = step.WithImages(f.images...)
	}
	return step
}

// parseOptions parses the choice --options value "key:desc|key:desc".
// Descriptions may be empty (sent as null on the wire).
func parseOptions(spec string) (map[string]string, error) {
	opts := map[string]string{}
	for _, part := range strings.Split(spec, "|") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, desc := part, ""
		if i := strings.Index(part, ":"); i >= 0 {
			key, desc = part[:i], part[i+1:]
		}
		if key == "" {
			return nil, &usageError{msg: "choice option has an empty key: " + part}
		}
		opts[key] = desc
	}
	if len(opts) == 0 {
		return nil, &usageError{
			msg:     "--options is required for choice",
			example: `sysone choice --state @ticket.txt --q "問題分類" --options "帳務:payments|技術:bugs|退貨:refunds"`,
		}
	}
	return opts, nil
}

// parseScale parses the score --scale value "低,中,高,極高" (2–10 levels, low → high).
func parseScale(spec string) ([]string, error) {
	var levels []string
	for _, l := range strings.Split(spec, ",") {
		l = strings.TrimSpace(l)
		if l != "" {
			levels = append(levels, l)
		}
	}
	if len(levels) < 2 || len(levels) > 10 {
		return nil, &usageError{
			msg:     "--scale needs 2–10 levels ordered lowest → highest",
			example: `sysone score --state @ctx.json --q "風險等級" --scale "低,中,高,極高"`,
		}
	}
	return levels, nil
}

// requireQ validates the --q instructions flag.
func requireQ(instr string, cmd *cobra.Command) error {
	if strings.TrimSpace(instr) == "" {
		return &usageError{cmd: cmd,
			msg:     "--q is required (the question instructions)",
			example: `sysone noul --state "disk at 97%" --q "Should we alert ops?" --threshold 0.8`}
	}
	return nil
}
