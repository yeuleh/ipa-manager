package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/yeuleh/ipa-manager/internal/device"
)

// deviceCmd is the unified device command group (replaces the old top-level
// 'devices' command and the 'install' group, both of which were stubs).
// Subcommands are added incrementally: T1 list, T2 apps, T3 install, T5 uninstall.
func deviceCmd(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "device",
		Short: "Manage connected iOS devices (list / apps / install / uninstall)",
	}
	cmd.AddCommand(deviceListCmd(deps), deviceAppsCmd(deps), deviceInstallCmd(deps))
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

// deviceAppsCmd implements `device apps` (US-05). Lists user-installed apps on
// the resolved device (excludes system apps).
//
// NOTE: --profile is intentionally NOT registered (AC-09-5): device apps is
// account-agnostic; only `device install` accepts --profile.
func deviceAppsCmd(deps Deps) *cobra.Command {
	var udidFlag string
	cmd := &cobra.Command{
		Use:   "apps",
		Short: "List user-installed apps on a device",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			dev, err := resolveDevice(deps, udidFlag)
			if err != nil {
				if isCancelled(err) {
					fmt.Fprintln(out, "cancelled")
					return nil
				}
				return err
			}
			apps, err := deps.DeviceService.ListInstalledApps(dev.UDID)
			if err != nil {
				if errors.Is(err, device.ErrTunnelRequired) {
					return fmt.Errorf("iOS 17+ tunnel required; run: sudo ios tunnel start")
				}
				return err
			}
			if len(apps) == 0 {
				fmt.Fprintf(out, "no user apps installed on device '%s'\n", orDash(dev.Name))
				return nil
			}
			fmt.Fprintln(out, "BUNDLE-ID\tNAME\tVERSION")
			for _, a := range apps {
				fmt.Fprintf(out, "%s\t%s\t%s\n", a.BundleID, orDash(a.Name), orDash(a.Version))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&udidFlag, "udid", "", "target device UDID (default: auto-select or prompt when multiple)")
	return cmd
}
