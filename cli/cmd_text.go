package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func (a *App) textCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "text <title>",
		Short: "Fetch and display a Wikisource text",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a.progressf("fetching %q...", args[0])
			text, err := a.client.Text(cmd.Context(), args[0])
			if err != nil {
				return mapFetchErr(err)
			}
			_, _ = fmt.Fprintln(os.Stdout, text)
			return nil
		},
	}
	return cmd
}
