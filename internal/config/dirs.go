package config

import (
	"os"
	"path/filepath"
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
