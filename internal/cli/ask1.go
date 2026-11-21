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
				AddNoul("q", q, f.threshold, nil).
				Execute(cmd.Context())
			if err != nil {
				return err
			}
			return finish(rs, f, resp, true)
		},
	}
	f.register(cmd)
	cmd.Flags().StringVarP(&q, "q", "i", "", "question instructions (yes/no)")
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
			var co []systemone.ChoiceOption
			if f.thresholdSet {
				co = append(co, systemone.WithMinConfidence(f.threshold))
			}
			resp, err := f.applyState(f.startBuilder(c), state).
				AddChoice("q", q, opts, nil, co...).
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
				AddScore("q", q, levels, nil).
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
	return cmd
}
