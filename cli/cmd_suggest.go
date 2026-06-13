package cli

import (
	"github.com/spf13/cobra"
)

func (a *App) suggestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "suggest <prefix>",
		Short: "Title suggestions for a prefix",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n := a.effectiveLimit(10)
			a.progressf("suggesting titles for %q...", args[0])
			results, err := a.client.Suggest(cmd.Context(), args[0], n)
			if err != nil {
				return mapFetchErr(err)
			}
			return a.renderOrEmpty(results, len(results))
		},
	}
	return cmd
}
