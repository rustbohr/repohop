package cli

import (
	"runtime"

	"github.com/spf13/cobra"
)

func newVersionCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the repohop version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.Printf("repohop %s (%s/%s, %s)\n", version, runtime.GOOS, runtime.GOARCH, runtime.Version())
			return nil
		},
	}
}
