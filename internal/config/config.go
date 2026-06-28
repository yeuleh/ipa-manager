package config

// Config is the global (non-account) configuration, persisted as JSON.
//
// NOTE: per the Stage 4 design, IPA inventories live in internal/library, not
// here — Config only holds cross-account state like the active profile.
type Config struct {
	ActiveProfileID string `json:"active_profile_id"`
}

// Load reads the global config from disk.
//
// TODO(mission): read from Paths.Config.
func Load() (Config, error) { return Config{}, nil }

// Save writes the global config to disk.
//
// TODO(mission): write to Paths.Config.
func Save(c Config) error { return nil }
