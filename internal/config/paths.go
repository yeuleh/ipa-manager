// Package config defines filesystem paths and global (non-account) configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Paths holds resolved filesystem locations under the app root.
type Paths struct {
	Root     string // ~/.ipa-manager
	Profiles string // ~/.ipa-manager/profiles
	Config   string // ~/.ipa-manager/config.json
	Library  string // ~/.ipa-manager/library
}

// Default resolves the default paths from the user's home directory.
// Returns an error if the home directory cannot be determined.
func Default() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("failed to get home directory: %w", err)
	}
	root := filepath.Join(home, ".ipa-manager")
	return Paths{
		Root:     root,
		Profiles: filepath.Join(root, "profiles"),
		Config:   filepath.Join(root, "config.json"),
		Library:  filepath.Join(root, "library"),
	}, nil
}
