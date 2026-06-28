package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func installCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Download / install / uninstall / update apps to a device",
	}
	cmd.AddCommand(installDownloadCmd(), installPushCmd(), installUninstallCmd(), installUpdateCmd())
	return cmd
}

func installDownloadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "download <bundle-id>",
		Short: "Download an app's IPA (active profile, isolated per account)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("install download: not yet implemented")
		},
	}
}

func installPushCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "push <ipa-path>",
		Short: "Install a local IPA to a connected device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("install push: not yet implemented")
		},
	}
}

func installUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall <bundle-id>",
		Short: "Uninstall an app from a connected device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("install uninstall: not yet implemented")
		},
	}
}

func installUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update [bundle-id]",
		Short: "Check for and apply app updates",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("install update: not yet implemented")
		},
	}
}
