// Package cli implements the cobra command tree for ipa-manager.
package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// Execute builds and runs the root command with production dependencies.
func Execute(version string) {
	os.Exit(execute(version, os.Args[1:], newProductionDeps, os.Stdout, os.Stderr))
}

// execute is the testable core of Execute. It returns the exit code instead of
// calling os.Exit, and accepts injectable args / deps / writers so the wrapper-
// level error rendering (DD-09: cobra prints "Error: <msg>" once, execute must
// NOT print again) can be regression-tested.
func execute(version string, args []string, depsFn func() (Deps, error), out, errOut io.Writer) int {
	deps, err := depsFn()
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	root := newRootCmd(deps)
	root.Version = version
	root.SetOut(out)
	root.SetErr(errOut)
	root.SetArgs(args)
	// cobra already prints "Error: <msg>" (SilenceErrors=false); do NOT print
	// again (design DD-09: single clean line, no duplicate).
	if err := root.Execute(); err != nil {
		return 1
	}
	return 0
}

func newRootCmd(deps Deps) *cobra.Command {
	root := &cobra.Command{
		Use:   "ipa-manager",
		Short: "Manage multiple Apple accounts' IPA downloads and device installs",
		Long: "ipa-manager orchestrates Apple ID login/switching, per-account IPA " +
			"download/isolation, and push-to-device install/update.\n\n" +
			"Account credentials are stored per-profile in the macOS Keychain " +
			"(via ipatool's keyring); device operations use go-ios.",
		// DD-09: operational (RunE) errors must not print Usage (cobra anti-pattern).
		// Flag/arg parse errors happen BEFORE this hook runs, so they still show
		// Usage; RunE errors (after the hook) get SilenceUsage=true → no Usage.
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			return nil
		},
	}
	root.AddCommand(
		authCmd(deps),
		accountCmd(deps),
		appCmd(deps),
		libraryCmd(deps),
		deviceCmd(deps),
		doctorCmd(),
	)
	return root
}
