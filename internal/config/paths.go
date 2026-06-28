// Package config defines filesystem paths and global (non-account) configuration.
package config

// Paths holds resolved filesystem locations under the app root.
type Paths struct {
	Root     string // ~/.ipa-manager
	Profiles string // ~/.ipa-manager/profiles
	Config   string // ~/.ipa-manager/config.json
	Library  string // ~/.ipa-manager/library
}

// Default resolves the default paths from the user's home directory.
//
// TODO(mission): resolve via os.UserHomeDir() / config root override flag.
func Default() Paths { return Paths{} }
