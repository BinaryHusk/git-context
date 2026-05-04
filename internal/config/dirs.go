package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cockroachdb/errors"
)

// NormalizeDir prepares a user-supplied directory path for use in a
// `gitdir:` includeIf directive.
//
//   - Empty input is rejected.
//   - Inputs containing `*` are passed through unchanged (treated as a
//     git-style glob).
//   - `~` is expanded to the user's home directory.
//   - Relative paths are resolved against the current working directory.
//   - A trailing slash is always appended so the directive matches the
//     whole subtree, not just the directory itself.
func NormalizeDir(path string) (string, error) {
	if path == "" {
		return "", errors.New("directory path is empty")
	}

	if strings.Contains(path, "*") {
		return path, nil
	}

	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errors.Wrap(err, "failed to get user home directory")
		}

		path = filepath.Join(home, strings.TrimPrefix(path, "~"))
	}

	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", errors.Wrap(err, "failed to resolve relative path")
		}

		path = abs
	}

	if !strings.HasSuffix(path, "/") {
		path += "/"
	}

	return path, nil
}

// LookupDir returns the profile name that owns the given directory path,
// or ("", false) if no profile claims it. The path must already be in its
// normalized form.
func (c *Config) LookupDir(path string) (string, bool) {
	for name, profile := range c.Profiles {
		if slices.Contains(profile.Directories, path) {
			return name, true
		}
	}

	return "", false
}

// AssignmentMap returns a flat path-to-profile map across all profiles.
// Used to emit the [includeIf] block list in deterministic order.
func (c *Config) AssignmentMap() map[string]string {
	out := make(map[string]string)

	for name, profile := range c.Profiles {
		for _, dir := range profile.Directories {
			out[dir] = name
		}
	}

	return out
}

// AssignDir adds `path` to the named profile's Directories list.
// Returns an error if:
//   - the profile does not exist, or
//   - the path is already assigned to a different profile.
//
// Re-assigning the same path to its current profile is a no-op.
func (c *Config) AssignDir(path, profileName string) error {
	profile, exists := c.Profiles[profileName]
	if !exists {
		return errors.WithStack(errors.Newf("profile %q does not exist", profileName))
	}

	if owner, ok := c.LookupDir(path); ok {
		if owner == profileName {
			return nil
		}

		return errors.WithStack(errors.Newf(
			"path %q is already assigned to profile %q; run 'dir remove' first",
			path, owner,
		))
	}

	profile.Directories = append(profile.Directories, path)

	return nil
}

// UnassignDir removes `path` from whichever profile owns it.
// Returns an error if no profile owns the path.
func (c *Config) UnassignDir(path string) error {
	owner, ok := c.LookupDir(path)
	if !ok {
		return errors.WithStack(errors.Newf("path %q is not assigned to any profile", path))
	}

	profile := c.Profiles[owner]
	filtered := profile.Directories[:0]

	for _, d := range profile.Directories {
		if d != path {
			filtered = append(filtered, d)
		}
	}

	profile.Directories = filtered

	return nil
}
