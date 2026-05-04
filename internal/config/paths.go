package config

import (
	"os"
	"path/filepath"

	"github.com/cockroachdb/errors"
)

// Paths contains all the important path locations for git-context.
type Paths struct {
	ConfigDir       string
	ConfigFile      string
	ProfilesDir     string
	GitConfigFile   string
	GitConfigBackup string
}

// NewPaths initializes and creates paths with proper defaults.
func NewPaths() (*Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get user home directory")
	}

	configDir := filepath.Join(home, ".config", "git-context")
	configFile := filepath.Join(configDir, "config.yaml")
	profilesDir := filepath.Join(configDir, "profiles")
	gitConfigFile := filepath.Join(home, ".gitconfig")
	gitConfigBackup := filepath.Join(home, ".gitconfig.bak")

	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return nil, errors.Wrap(err, "failed to create config directory")
	}

	if err := os.MkdirAll(profilesDir, 0o755); err != nil {
		return nil, errors.Wrap(err, "failed to create profiles directory")
	}

	return &Paths{
		ConfigDir:       configDir,
		ConfigFile:      configFile,
		ProfilesDir:     profilesDir,
		GitConfigFile:   gitConfigFile,
		GitConfigBackup: gitConfigBackup,
	}, nil
}
