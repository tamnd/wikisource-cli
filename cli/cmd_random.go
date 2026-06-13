package cli

import (
	"github.com/spf13/cobra"
)

func (a *App) randomCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "random",
		Short: "Show random Wikisource texts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			n := a.effectiveLimit(5)
			a.progressf("fetching %d random texts...", n)
			results, err := a.client.Random(cmd.Context(), n)
			if err != nil {
				return mapFetchErr(err)
			}
			return a.renderOrEmpty(results, len(results))
		},
	}
	return cmd
}
