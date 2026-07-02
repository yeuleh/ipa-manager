package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/yeuleh/ipa-manager/internal/apperr"
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
	cmd.AddCommand(deviceListCmd(deps), deviceAppsCmd(deps), deviceInstallCmd(deps), deviceUninstallCmd(deps))
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

// deviceUninstallCmd implements `device uninstall` (US-04). Removes an app from
// the resolved device with a confirmation prompt (destructive). Non-interactive
// mode refuses (safe default). Account-agnostic (no --profile).
func deviceUninstallCmd(deps Deps) *cobra.Command {
	var udidFlag string
	cmd := &cobra.Command{
		Use:   "uninstall <bundle-id>",
		Short: "Uninstall an app from a device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			bundleID := args[0]

			dev, err := resolveDevice(deps, udidFlag)
			if err != nil {
				if isCancelled(err) {
					fmt.Fprintln(out, "cancelled")
					return nil
				}
				return err
			}

			// AC-04-4: non-interactive mode refuses destructive uninstall.
			if !checkInteractive() {
				return fmt.Errorf("confirmation required in non-interactive mode; cannot proceed")
			}

			// AC-04-1: confirmation prompt (destructive).
			fmt.Fprintf(out, "uninstall '%s' from device '%s'? [y/N]\n", bundleID, orDash(dev.Name))
			confirmed, err := deps.UI.Confirm("confirm uninstall?")
			if err != nil {
				return fmt.Errorf("failed to prompt: %w", err)
			}
			if !confirmed {
				fmt.Fprintln(out, "cancelled")
				return nil
			}

			if err := deps.DeviceService.Uninstall(dev.UDID, bundleID); err != nil {
				if errors.Is(err, device.ErrTunnelRequired) {
					return fmt.Errorf("iOS 17+ tunnel required; run: sudo ios tunnel start")
				}
				if errors.Is(err, apperr.ErrAppNotInstalled) {
					return fmt.Errorf("app '%s' not installed on device", bundleID) // AC-04-3
				}
				return err
			}
			fmt.Fprintf(out, "✓ Uninstalled %s from %s\n", bundleID, orDash(dev.Name))
			return nil
		},
	}
	cmd.Flags().StringVar(&udidFlag, "udid", "", "target device UDID (default: auto-select or prompt when multiple)")
	return cmd
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
