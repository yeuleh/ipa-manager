package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Run environment health checks (Go, macOS, keychain, devices, tunnel)",
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO(mission): run internal/doctor checks; do not auto-escalate sudo.
			return fmt.Errorf("doctor: not yet implemented")
		},
	}
}
