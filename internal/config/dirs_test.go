package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// toFwd mirrors NormalizeDir's backslash-to-slash canonicalization so test
// expectations built via filepath.Join (which uses OS-native separators)
// stay comparable on Windows.
func toFwd(p string) string { return strings.ReplaceAll(p, `\`, `/`) }

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

	type tcase struct {
		name string
		in   string
		want string
	}

	tests := []tcase{
		{"tilde expands to home", "~/projects/work", toFwd(filepath.Join(home, "projects", "work")) + "/"},
		{"relative resolves against cwd", "./foo", toFwd(filepath.Join(cwd, "foo")) + "/"},
		{"single-star glob passes through unchanged", "~/work/*/repo", "~/work/*/repo"},
		{"double-star glob passes through unchanged", "~/work/**", "~/work/**"},
	}

	// POSIX-rooted absolute paths (`/Users/x/...`) are not absolute on
	// Windows — `filepath.Abs` resolves them against the cwd's drive,
	// producing `D:/Users/x/...`. Skip those rows on Windows; the
	// equivalent semantics are exercised by the tilde + relative cases
	// above, which build absolute paths via filepath.Join.
	if runtime.GOOS != "windows" {
		tests = append(tests,
			tcase{"absolute path gets trailing slash", "/Users/x/projects/work", "/Users/x/projects/work/"},
			tcase{
				"absolute path keeps existing trailing slash",
				"/Users/x/projects/work/",
				"/Users/x/projects/work/",
			},
		)
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

func TestAssignDirSuccess(t *testing.T) {
	t.Parallel()

	cfg := NewConfig()
	cfg.Profiles["work"] = &Profile{User: UserConfig{Name: "X"}}

	if err := cfg.AssignDir("/Users/x/work/", "work"); err != nil {
		t.Fatalf("AssignDir error: %v", err)
	}

	if got := cfg.Profiles["work"].Directories; len(got) != 1 || got[0] != "/Users/x/work/" {
		t.Errorf("Directories = %v, want [/Users/x/work/]", got)
	}
}

func TestAssignDirRejectsUnknownProfile(t *testing.T) {
	t.Parallel()

	cfg := NewConfig()

	if err := cfg.AssignDir("/Users/x/work/", "ghost"); err == nil {
		t.Error("expected error for unknown profile, got nil")
	}
}

func TestAssignDirRejectsDuplicateAcrossProfiles(t *testing.T) {
	t.Parallel()

	cfg := NewConfig()
	cfg.Profiles["work"] = &Profile{Directories: []string{"/Users/x/shared/"}}
	cfg.Profiles["personal"] = &Profile{}

	err := cfg.AssignDir("/Users/x/shared/", "personal")
	if err == nil {
		t.Fatal("expected error for duplicate path, got nil")
	}

	if !strings.Contains(err.Error(), "already assigned") {
		t.Errorf("error = %q, want it to mention 'already assigned'", err.Error())
	}
}

func TestAssignDirIdempotentSameProfile(t *testing.T) {
	t.Parallel()

	cfg := NewConfig()
	cfg.Profiles["work"] = &Profile{Directories: []string{"/Users/x/work/"}}

	if err := cfg.AssignDir("/Users/x/work/", "work"); err != nil {
		t.Fatalf("AssignDir error: %v", err)
	}

	if got := len(cfg.Profiles["work"].Directories); got != 1 {
		t.Errorf("Directories len = %d, want 1 (no duplication)", got)
	}
}

func TestUnassignDirSuccess(t *testing.T) {
	t.Parallel()

	cfg := NewConfig()
	cfg.Profiles["work"] = &Profile{
		Directories: []string{"/Users/x/work/", "/Users/x/Mollie/"},
	}

	if err := cfg.UnassignDir("/Users/x/work/"); err != nil {
		t.Fatalf("UnassignDir error: %v", err)
	}

	got := cfg.Profiles["work"].Directories
	if len(got) != 1 || got[0] != "/Users/x/Mollie/" {
		t.Errorf("Directories = %v, want [/Users/x/Mollie/]", got)
	}
}

func TestUnassignDirNotFound(t *testing.T) {
	t.Parallel()

	cfg := NewConfig()

	if err := cfg.UnassignDir("/Users/x/missing/"); err == nil {
		t.Error("expected error for missing path, got nil")
	}
}
