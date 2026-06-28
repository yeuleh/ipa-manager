package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func devicesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "devices",
		Short: "List connected iOS devices",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("devices: not yet implemented")
		},
	}
}
