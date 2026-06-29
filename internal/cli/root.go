// Package cli implements the cobra command tree for ipa-manager.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Execute builds and runs the root command with production dependencies.
func Execute(version string) {
	deps, err := newProductionDeps()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	root := newRootCmd(deps)
	root.Version = version
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd(deps Deps) *cobra.Command {
	root := &cobra.Command{
		Use:   "ipa-manager",
		Short: "Manage multiple Apple accounts' IPA downloads and device installs",
		Long: "ipa-manager orchestrates Apple ID login/switching, per-account IPA " +
			"download/isolation, and push-to-device install/update.\n\n" +
			"Account credentials are stored per-profile in the macOS Keychain " +
			"(via ipatool's keyring); device operations use go-ios.",
	}
	root.AddCommand(
		authCmd(deps),
		accountCmd(deps),
		appsCmd(),
		devicesCmd(),
		installCmd(),
		doctorCmd(),
	)
	return root
}
