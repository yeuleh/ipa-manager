package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// deviceCmd is the unified device command group (replaces the old top-level
// 'devices' command and the 'install' group, both of which were stubs).
// Subcommands are added incrementally: T1 list, T2 apps, T3 install, T5 uninstall.
func deviceCmd(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "device",
		Short: "Manage connected iOS devices (list / apps / install / uninstall)",
	}
	cmd.AddCommand(deviceListCmd(deps))
	return cmd
}

// deviceListCmd implements `device list` (US-01).
//
// NOTE: --profile is intentionally NOT registered. device list / apps /
// uninstall are account-agnostic; only `device install` accepts --profile
// (AC-09-5: passing --profile here yields a cobra "unknown flag" error).
func deviceListCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List connected iOS devices",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			devices, err := deps.DeviceService.ListConnected()
			if err != nil {
				return fmt.Errorf("failed to list devices: %w", err)
			}
			if len(devices) == 0 {
				fmt.Fprintln(out, "no connected device")
				return nil
			}
			fmt.Fprintln(out, "UDID\tNAME\tIOS-VERSION\tCONNECTION-TYPE")
			for _, d := range devices {
				fmt.Fprintf(out, "%s\t%s\t%s\t%s\n", d.UDID, orDash(d.Name), orDash(d.IOSVersion), d.ConnectionType)
			}
			return nil
		},
	}
}

// orDash returns s, or "-" when s is empty (placeholder for unavailable
// lockdown fields on untrusted / tunnel-less devices).
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
