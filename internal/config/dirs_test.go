package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeDir(t *testing.T) {
	t.Parallel()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir failed: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"absolute path gets trailing slash", "/Users/x/projects/work", "/Users/x/projects/work/"},
		{
			"absolute path keeps existing trailing slash",
			"/Users/x/projects/work/",
			"/Users/x/projects/work/",
		},
		{"tilde expands to home", "~/projects/work", filepath.Join(home, "projects", "work") + "/"},
		{"relative resolves against cwd", "./foo", filepath.Join(cwd, "foo") + "/"},
		{"single-star glob passes through unchanged", "~/work/*/repo", "~/work/*/repo"},
		{"double-star glob passes through unchanged", "~/work/**", "~/work/**"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := NormalizeDir(tc.in)
			if err != nil {
				t.Fatalf("NormalizeDir(%q) error: %v", tc.in, err)
			}

			if got != tc.want {
				t.Errorf("NormalizeDir(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeDirEmpty(t *testing.T) {
	t.Parallel()

	if _, err := NormalizeDir(""); err == nil {
		t.Error("expected error for empty path, got nil")
	}
}
