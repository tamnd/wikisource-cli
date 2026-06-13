package cli

import (
	"github.com/spf13/cobra"
)

func (a *App) listCmd() *cobra.Command {
	var category string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Wikisource texts by category",
		RunE: func(cmd *cobra.Command, _ []string) error {
			n := a.effectiveLimit(20)
			if category != "" {
				a.progressf("listing category %q...", category)
			} else {
				a.progressf("listing texts...")
			}
			results, err := a.client.List(cmd.Context(), category, n)
			if err != nil {
				return mapFetchErr(err)
			}
			return a.renderOrEmpty(results, len(results))
		},
	}
	cmd.Flags().StringVar(&category, "category", "", "category name to list (e.g. Novels, Poetry)")
	return cmd
}
