package cli

import (
	"github.com/spf13/cobra"
	"github.com/tsai-danny/sysone"
)

func newNoulCmd(rs *runState) *cobra.Command {
	f := &qFlags{}
	var q string
	cmd := &cobra.Command{
		Use:   "noul",
		Short: "One yes/no question about a state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			f.markThreshold(cmd)
			if err := requireQ(q, cmd); err != nil {
				return err
			}
			state, err := f.resolveState(rs)
			if err != nil {
				return err
			}
			c, err := f.client()
			if err != nil {
				return err
			}
			resp, err := f.applyState(f.startBuilder(c), state).
				AddNoul(sysone.NoulQ{ID: "q", Instructions: q, Threshold: f.threshold}).
				Execute(cmd.Context())
			if err != nil {
				return err
			}
			return finish(rs, f, resp, true)
		},
	}
	f.register(cmd)
	cmd.Flags().StringVarP(&q, "q", "i", "", "question instructions (yes/no)")
	markRequired(cmd, "state", "q")
	return cmd
}

func newChoiceCmd(rs *runState) *cobra.Command {
	f := &qFlags{}
	var q, options string
	cmd := &cobra.Command{
		Use:   "choice",
		Short: "One multiple-choice question about a state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			f.markThreshold(cmd)
			if err := requireQ(q, cmd); err != nil {
				return err
			}
			opts, err := parseOptions(options)
			if err != nil {
				return err
			}
			state, err := f.resolveState(rs)
			if err != nil {
				return err
			}
			c, err := f.client()
			if err != nil {
				return err
			}
			minConf := 0.0
			if f.thresholdSet {
				minConf = f.threshold
			}
			resp, err := f.applyState(f.startBuilder(c), state).
				AddChoice(sysone.ChoiceQ{ID: "q", Instructions: q, Options: opts, MinConfidence: minConf}).
				Execute(cmd.Context())
			if err != nil {
				return err
			}
			return finish(rs, f, resp, true)
		},
	}
	f.register(cmd)
	cmd.Flags().StringVarP(&q, "q", "i", "", "question instructions")
	cmd.Flags().StringVar(&options, "options", "", `options "key:desc|key:desc" (desc may be empty)`)
	markRequired(cmd, "state", "q", "options")
	return cmd
}

func newScoreCmd(rs *runState) *cobra.Command {
	f := &qFlags{}
	var q, scale string
	cmd := &cobra.Command{
		Use:   "score",
		Short: "One graded question about a state (2–10 levels, low → high)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			f.markThreshold(cmd)
			if err := requireQ(q, cmd); err != nil {
				return err
			}
			levels, err := parseScale(scale)
			if err != nil {
				return err
			}
			state, err := f.resolveState(rs)
			if err != nil {
				return err
			}
			c, err := f.client()
			if err != nil {
				return err
			}
			resp, err := f.applyState(f.startBuilder(c), state).
				AddScore(sysone.ScoreQ{ID: "q", Instructions: q, Levels: levels}).
				Execute(cmd.Context())
			if err != nil {
				return err
			}
			return finish(rs, f, resp, true)
		},
	}
	f.register(cmd)
	cmd.Flags().StringVarP(&q, "q", "i", "", "question instructions")
	cmd.Flags().StringVar(&scale, "scale", "", `levels "低,中,高,極高" ordered lowest → highest`)
	markRequired(cmd, "state", "q", "scale")
	return cmd
}
