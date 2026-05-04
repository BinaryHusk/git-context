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

func TestLookupDir(t *testing.T) {
	t.Parallel()

	cfg := NewConfig()
	cfg.Profiles["work"] = &Profile{
		Directories: []string{"/Users/x/work/", "/Users/x/Mollie/"},
	}
	cfg.Profiles["personal"] = &Profile{
		Directories: []string{"/Users/x/personal/"},
	}

	if got, ok := cfg.LookupDir("/Users/x/work/"); !ok || got != "work" {
		t.Errorf("LookupDir(work) = (%q, %v), want (\"work\", true)", got, ok)
	}

	if got, ok := cfg.LookupDir("/Users/x/personal/"); !ok || got != "personal" {
		t.Errorf("LookupDir(personal) = (%q, %v), want (\"personal\", true)", got, ok)
	}

	if _, ok := cfg.LookupDir("/Users/x/none/"); ok {
		t.Error("LookupDir(none) ok = true, want false")
	}
}

func TestAssignmentMap(t *testing.T) {
	t.Parallel()

	cfg := NewConfig()
	cfg.Profiles["work"] = &Profile{
		Directories: []string{"/Users/x/work/", "/Users/x/Mollie/"},
	}
	cfg.Profiles["personal"] = &Profile{
		Directories: []string{"/Users/x/personal/"},
	}

	got := cfg.AssignmentMap()

	if len(got) != 3 {
		t.Fatalf("AssignmentMap len = %d, want 3", len(got))
	}

	wants := map[string]string{
		"/Users/x/work/":     "work",
		"/Users/x/Mollie/":   "work",
		"/Users/x/personal/": "personal",
	}

	for path, wantProfile := range wants {
		if got[path] != wantProfile {
			t.Errorf("AssignmentMap[%q] = %q, want %q", path, got[path], wantProfile)
		}
	}
}
