# Directory-Based Profile Assignment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make repos under user-configured paths automatically use a chosen git profile via git's native `includeIf "gitdir:..."` mechanism, while a configured default profile applies everywhere else.

**Architecture:** Per-profile gitconfig files generated at `~/.config/git-context/profiles/<name>.gitconfig`. `~/.gitconfig` becomes a thin manifest (header marker + unconditional `[include]` for the default + one `[includeIf]` per assigned directory). Every mutating command (`switch`, `add` with dirs, `remove`, `dir add/remove`) regenerates both the profile files and the manifest.

**Tech Stack:** Go 1.25, `cobra` (CLI), `yaml.v3`, `cockroachdb/errors`. Tests use the standard `testing` package and follow the existing `t.Parallel()` + `t.TempDir()` patterns in this repo.

**Spec:** `docs/superpowers/specs/2026-05-04-directory-based-profile-assignment-design.md`

---

## File Structure

**Created:**
- `internal/config/dirs.go` — directory normalization, lookup, assignment helpers
- `internal/config/dirs_test.go` — tests for the above
- `cmd/dir.go` — `dir` parent command + `add`/`remove`/`list` subcommands
- `cmd/dir_test.go` — tests for the above

**Modified:**
- `internal/config/config.go` — add `Directories []string` to `Profile`
- `internal/config/paths.go` — add `ProfilesDir` field
- `internal/git/git.go` — add `WriteProfileFile`, `WriteRootConfig`, `Regenerate`
- `internal/git/git_test.go` — tests for new methods
- `cmd/switch.go` — call `Regenerate` instead of `WriteConfig`
- `cmd/add.go` — optional "assign directories" prompt; trigger regenerate when used
- `cmd/remove.go` — confirm if profile has directories; regenerate on success
- `cmd/list.go` — add "Dirs" column
- `cmd/show.go` — display assigned directories
- `cmd/current.go` — show "Effective in <cwd>" line via `git config --show-origin user.email`
- `cmd/cmd_test.go` — tests for changed commands

---

## Task 1: Add `Directories` field to `Profile`

**Files:**
- Modify: `internal/config/config.go:16-42`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/config/config_test.go`:

```go
func TestProfileYAMLRoundTripWithDirectories(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	cfg := NewConfig()
	cfg.Profiles["work"] = &Profile{
		User:        UserConfig{Name: "Andre", Email: "a@work.com"},
		Directories: []string{"/Users/andre/projects/work", "/Users/andre/Mollie"},
	}

	if err := cfg.SaveConfig(configFile); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	loaded, err := LoadConfig(configFile)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	got := loaded.Profiles["work"].Directories
	want := []string{"/Users/andre/projects/work", "/Users/andre/Mollie"}

	if len(got) != len(want) {
		t.Fatalf("Directories length: got %d, want %d", len(got), len(want))
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Directories[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestProfileYAMLRoundTripWithDirectories -v`
Expected: FAIL — `Profile` has no `Directories` field.

- [ ] **Step 3: Add the field**

In `internal/config/config.go`, add to `Profile` struct (insert before `URL`):

```go
	Directories []string       `yaml:"directories,omitempty"`
	URL         []URLConfig    `yaml:"url,omitempty"`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestProfileYAMLRoundTripWithDirectories -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit --signoff --gpg-sign -m "feat(config): add Directories field to Profile"
```

---

## Task 2: Path normalization helper

Goal: a single function that turns user input (`~/work`, `./foo`, `/abs/path`, `~/work/**`) into the form we'll embed in `gitdir:` directives.

**Files:**
- Create: `internal/config/dirs.go`
- Create: `internal/config/dirs_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/config/dirs_test.go`:

```go
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
		{"absolute path keeps existing trailing slash", "/Users/x/projects/work/", "/Users/x/projects/work/"},
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestNormalizeDir -v`
Expected: FAIL — `NormalizeDir` is undefined.

- [ ] **Step 3: Implement `NormalizeDir`**

Create `internal/config/dirs.go`:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -run TestNormalizeDir -v`
Expected: PASS for all cases including `TestNormalizeDirEmpty`.

- [ ] **Step 5: Commit**

```bash
git add internal/config/dirs.go internal/config/dirs_test.go
git commit --signoff --gpg-sign -m "feat(config): add NormalizeDir for includeIf path handling"
```

---

## Task 3: Reverse lookup and assignment map

Goal: given a normalized path, find which profile owns it; and produce a flat `path → profile` map for emitting `[includeIf]` blocks.

**Files:**
- Modify: `internal/config/dirs.go`
- Modify: `internal/config/dirs_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/config/dirs_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run "TestLookupDir|TestAssignmentMap" -v`
Expected: FAIL — methods undefined.

- [ ] **Step 3: Implement `LookupDir` and `AssignmentMap`**

Append to `internal/config/dirs.go`:

```go
// LookupDir returns the profile name that owns the given directory path,
// or ("", false) if no profile claims it. The path must already be in its
// normalized form.
func (c *Config) LookupDir(path string) (string, bool) {
	for name, profile := range c.Profiles {
		for _, dir := range profile.Directories {
			if dir == path {
				return name, true
			}
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -run "TestLookupDir|TestAssignmentMap" -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/dirs.go internal/config/dirs_test.go
git commit --signoff --gpg-sign -m "feat(config): add LookupDir and AssignmentMap"
```

---

## Task 4: Assign / unassign with conflict detection

Goal: a single entry point for adding and removing directory assignments that enforces "one profile per directory".

**Files:**
- Modify: `internal/config/dirs.go`
- Modify: `internal/config/dirs_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/config/dirs_test.go`:

```go
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
```

Add `"strings"` to the imports of `dirs_test.go` if not already present.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run "TestAssignDir|TestUnassignDir" -v`
Expected: FAIL — `AssignDir`/`UnassignDir` undefined.

- [ ] **Step 3: Implement assignment helpers**

Append to `internal/config/dirs.go`:

```go
// AssignDir adds `path` to the named profile's Directories list.
// Returns an error if:
//   - the profile does not exist, or
//   - the path is already assigned to a different profile.
//
// Re-assigning the same path to its current profile is a no-op.
func (c *Config) AssignDir(path, profileName string) error {
	profile, exists := c.Profiles[profileName]
	if !exists {
		return errors.Newf("profile %q does not exist", profileName)
	}

	if owner, ok := c.LookupDir(path); ok {
		if owner == profileName {
			return nil
		}

		return errors.Newf(
			"path %q is already assigned to profile %q; run 'dir remove' first",
			path, owner,
		)
	}

	profile.Directories = append(profile.Directories, path)

	return nil
}

// UnassignDir removes `path` from whichever profile owns it.
// Returns an error if no profile owns the path.
func (c *Config) UnassignDir(path string) error {
	owner, ok := c.LookupDir(path)
	if !ok {
		return errors.Newf("path %q is not assigned to any profile", path)
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -run "TestAssignDir|TestUnassignDir" -v`
Expected: PASS for all cases.

- [ ] **Step 5: Commit**

```bash
git add internal/config/dirs.go internal/config/dirs_test.go
git commit --signoff --gpg-sign -m "feat(config): add AssignDir/UnassignDir with conflict detection"
```

---

## Task 5: Add `ProfilesDir` to `Paths`

**Files:**
- Modify: `internal/config/paths.go`
- Modify: `internal/config/paths_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/config/paths_test.go`:

```go
func TestPathsHasProfilesDir(t *testing.T) {
	t.Parallel()

	paths, err := NewPaths()
	if err != nil {
		t.Fatalf("NewPaths failed: %v", err)
	}

	if paths.ProfilesDir == "" {
		t.Fatal("ProfilesDir is empty")
	}

	want := filepath.Join(paths.ConfigDir, "profiles")
	if paths.ProfilesDir != want {
		t.Errorf("ProfilesDir = %q, want %q", paths.ProfilesDir, want)
	}

	if _, err := os.Stat(paths.ProfilesDir); err != nil {
		t.Errorf("ProfilesDir was not created: %v", err)
	}
}
```

If `os` is not imported in `paths_test.go`, add it.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestPathsHasProfilesDir -v`
Expected: FAIL — `ProfilesDir` field does not exist.

- [ ] **Step 3: Add the field and ensure the directory exists**

In `internal/config/paths.go`, replace the `Paths` struct and `NewPaths`:

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestPathsHasProfilesDir -v`
Expected: PASS.

Also re-run the full config tests to confirm nothing else broke:

Run: `go test ./internal/config/ -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/paths.go internal/config/paths_test.go
git commit --signoff --gpg-sign -m "feat(config): add ProfilesDir to Paths"
```

---

## Task 6: `WriteProfileFile` — atomic write of one profile's gitconfig

Goal: a function that takes a target path and a `map[string]any` of git settings, and writes it atomically (temp file + rename). Reuses the existing `buildGitConfig` formatter.

**Files:**
- Modify: `internal/git/git.go`
- Modify: `internal/git/git_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/git/git_test.go`:

```go
func TestWriteProfileFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "work.gitconfig")

	g := NewGit(filepath.Join(tmpDir, ".gitconfig"))

	settings := map[string]any{
		"user.name":  "Andre",
		"user.email": "andre@work.com",
	}

	if err := g.WriteProfileFile(target, settings); err != nil {
		t.Fatalf("WriteProfileFile error: %v", err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}

	content := string(data)

	if !strings.Contains(content, "[user]") {
		t.Errorf("missing [user] section in:\n%s", content)
	}

	if !strings.Contains(content, "name = Andre") {
		t.Errorf("missing user.name in:\n%s", content)
	}

	if !strings.Contains(content, "email = andre@work.com") {
		t.Errorf("missing user.email in:\n%s", content)
	}
}

func TestWriteProfileFileAtomic(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "work.gitconfig")

	// Pre-create the file with old content to verify replace, not append.
	if err := os.WriteFile(target, []byte("OLD\n"), 0o644); err != nil {
		t.Fatalf("setup write failed: %v", err)
	}

	g := NewGit(filepath.Join(tmpDir, ".gitconfig"))

	if err := g.WriteProfileFile(target, map[string]any{"user.name": "New"}); err != nil {
		t.Fatalf("WriteProfileFile error: %v", err)
	}

	data, _ := os.ReadFile(target)
	if strings.Contains(string(data), "OLD") {
		t.Errorf("old content not replaced:\n%s", data)
	}

	// No leftover .tmp file.
	matches, _ := filepath.Glob(filepath.Join(tmpDir, "*.tmp"))
	if len(matches) > 0 {
		t.Errorf("temp files left behind: %v", matches)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -run TestWriteProfileFile -v`
Expected: FAIL — `WriteProfileFile` undefined.

- [ ] **Step 3: Implement `WriteProfileFile`**

In `internal/git/git.go`, append:

```go
// WriteProfileFile writes a flat key→value git config map to `path`
// using atomic temp-file-and-rename semantics.
func (g *Git) WriteProfileFile(path string, config map[string]any) error {
	content := buildGitConfig(config)

	return atomicWrite(path, []byte(content))
}

// atomicWrite writes data to a sibling `.tmp` file then renames it into place.
func atomicWrite(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return errors.Wrap(err, "failed to write temp file")
	}

	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return errors.Wrap(err, "failed to rename temp file into place")
	}

	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/git/ -run TestWriteProfileFile -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/git/git.go internal/git/git_test.go
git commit --signoff --gpg-sign -m "feat(git): add atomic WriteProfileFile"
```

---

## Task 7: `WriteRootConfig` — generate the `~/.gitconfig` manifest

Goal: write a manifest with a header marker, an unconditional `[include]` for the default profile (if any), and one `[includeIf]` block per assignment. Block order must be deterministic (sort by `gitdir` path string).

**Files:**
- Modify: `internal/git/git.go`
- Modify: `internal/git/git_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/git/git_test.go`:

```go
func TestWriteRootConfigDefaultAndAssignments(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, ".gitconfig")
	g := NewGit(rootPath)

	defaultProfilePath := filepath.Join(tmpDir, "profiles", "work.gitconfig")
	assignments := map[string]string{
		"/Users/x/projects/personal/": filepath.Join(tmpDir, "profiles", "personal.gitconfig"),
		"/Users/x/Mollie/":            filepath.Join(tmpDir, "profiles", "work.gitconfig"),
	}

	if err := g.WriteRootConfig(defaultProfilePath, assignments); err != nil {
		t.Fatalf("WriteRootConfig error: %v", err)
	}

	data, err := os.ReadFile(rootPath)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}

	content := string(data)

	if !strings.Contains(content, "Generated by git-context") {
		t.Errorf("missing header marker:\n%s", content)
	}

	if !strings.Contains(content, "[include]") {
		t.Errorf("missing [include] block:\n%s", content)
	}

	if !strings.Contains(content, "path = "+defaultProfilePath) {
		t.Errorf("missing default profile path:\n%s", content)
	}

	if !strings.Contains(content, `[includeIf "gitdir:/Users/x/Mollie/"]`) {
		t.Errorf("missing Mollie includeIf:\n%s", content)
	}

	if !strings.Contains(content, `[includeIf "gitdir:/Users/x/projects/personal/"]`) {
		t.Errorf("missing personal includeIf:\n%s", content)
	}
}

func TestWriteRootConfigDeterministicOrder(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, ".gitconfig")
	g := NewGit(rootPath)

	assignments := map[string]string{
		"/zzz/": "/profiles/c.gitconfig",
		"/aaa/": "/profiles/a.gitconfig",
		"/mmm/": "/profiles/b.gitconfig",
	}

	if err := g.WriteRootConfig("", assignments); err != nil {
		t.Fatalf("WriteRootConfig error: %v", err)
	}

	data, _ := os.ReadFile(rootPath)
	content := string(data)

	idxA := strings.Index(content, "gitdir:/aaa/")
	idxM := strings.Index(content, "gitdir:/mmm/")
	idxZ := strings.Index(content, "gitdir:/zzz/")

	if !(idxA < idxM && idxM < idxZ) {
		t.Errorf("includeIf blocks not sorted: aaa=%d mmm=%d zzz=%d", idxA, idxM, idxZ)
	}
}

func TestWriteRootConfigNoDefaultNoAssignments(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, ".gitconfig")
	g := NewGit(rootPath)

	if err := g.WriteRootConfig("", map[string]string{}); err != nil {
		t.Fatalf("WriteRootConfig error: %v", err)
	}

	if _, err := os.Stat(rootPath); !os.IsNotExist(err) {
		t.Errorf("expected no file written, got err=%v", err)
	}
}

func TestWriteRootConfigParseableByGit(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, ".gitconfig")
	g := NewGit(rootPath)

	defaultPath := filepath.Join(tmpDir, "default.gitconfig")
	assignments := map[string]string{"/Users/x/work/": filepath.Join(tmpDir, "work.gitconfig")}

	if err := g.WriteRootConfig(defaultPath, assignments); err != nil {
		t.Fatalf("WriteRootConfig error: %v", err)
	}

	cmd := exec.Command("git", "config", "-f", rootPath, "--list")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git config rejected file: %v\n%s", err, out)
	}
}
```

Add `"os/exec"` to the imports of `git_test.go` if not already present.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/git/ -run TestWriteRootConfig -v`
Expected: FAIL — `WriteRootConfig` undefined.

- [ ] **Step 3: Implement `WriteRootConfig`**

Append to `internal/git/git.go`:

```go
// rootConfigHeader is emitted at the top of the generated ~/.gitconfig.
// Used both to inform users that the file is managed and (optionally in
// the future) to detect prior generations.
const rootConfigHeader = "# Generated by git-context — do not edit. " +
	"Run `git-context` to manage.\n"

// WriteRootConfig generates the ~/.gitconfig manifest:
//   - a header marker
//   - one unconditional [include] block for `defaultProfilePath`, if set
//   - one [includeIf "gitdir:<path>"] block per (path → profile-file) entry
//
// `assignments` keys are normalized directory paths (with trailing `/`),
// values are absolute paths to per-profile gitconfig files. Blocks are
// emitted in sorted key order so output is deterministic.
//
// If the default is empty AND assignments is empty, no file is written
// (avoids clobbering an existing user-managed ~/.gitconfig before any
// git-context state exists).
func (g *Git) WriteRootConfig(defaultProfilePath string, assignments map[string]string) error {
	if defaultProfilePath == "" && len(assignments) == 0 {
		return nil
	}

	var b strings.Builder

	b.WriteString(rootConfigHeader)
	b.WriteString("\n")

	if defaultProfilePath != "" {
		fmt.Fprintf(&b, "[include]\n\tpath = %s\n\n", defaultProfilePath)
	}

	keys := make([]string, 0, len(assignments))
	for k := range assignments {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	for _, dir := range keys {
		fmt.Fprintf(&b, "[includeIf \"gitdir:%s\"]\n\tpath = %s\n\n", dir, assignments[dir])
	}

	return atomicWrite(g.globalConfigPath, []byte(b.String()))
}
```

Add `"sort"` to the imports of `git.go` if not present (`fmt`, `os`, `strings` already are).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/git/ -run TestWriteRootConfig -v`
Expected: all PASS, including the `git config -f --list` round-trip.

- [ ] **Step 5: Commit**

```bash
git add internal/git/git.go internal/git/git_test.go
git commit --signoff --gpg-sign -m "feat(git): add WriteRootConfig manifest writer"
```

---

## Task 8: `Regenerate` — orchestrator

Goal: one entry point that mutating commands call. It writes every profile to its file and then writes the root manifest.

**Files:**
- Modify: `internal/git/git.go`
- Modify: `internal/git/git_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/git/git_test.go`:

```go
func TestRegenerate(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	profilesDir := filepath.Join(tmpDir, "profiles")

	if err := os.MkdirAll(profilesDir, 0o755); err != nil {
		t.Fatalf("mkdir profiles: %v", err)
	}

	cfg := config.NewConfig()
	cfg.Profiles["work"] = &config.Profile{
		User:        config.UserConfig{Name: "Andre", Email: "a@work.com"},
		Directories: []string{"/Users/x/Mollie/"},
	}
	cfg.Profiles["personal"] = &config.Profile{
		User:        config.UserConfig{Name: "Andre", Email: "a@home.com"},
		Directories: []string{"/Users/x/projects/personal/"},
	}
	cfg.Current = "work"

	rootPath := filepath.Join(tmpDir, ".gitconfig")
	g := NewGit(rootPath)

	if err := g.Regenerate(cfg, profilesDir); err != nil {
		t.Fatalf("Regenerate error: %v", err)
	}

	// Per-profile files exist with expected content.
	for name, wantEmail := range map[string]string{"work": "a@work.com", "personal": "a@home.com"} {
		path := filepath.Join(profilesDir, name+".gitconfig")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}

		if !strings.Contains(string(data), "email = "+wantEmail) {
			t.Errorf("%s missing email=%s\n%s", name, wantEmail, data)
		}
	}

	// Root manifest references the right files.
	root, err := os.ReadFile(rootPath)
	if err != nil {
		t.Fatalf("read root: %v", err)
	}

	rootStr := string(root)
	wantInclude := filepath.Join(profilesDir, "work.gitconfig")

	if !strings.Contains(rootStr, "path = "+wantInclude) {
		t.Errorf("root missing default include for %q:\n%s", wantInclude, rootStr)
	}

	if !strings.Contains(rootStr, `gitdir:/Users/x/Mollie/`) {
		t.Errorf("root missing Mollie includeIf:\n%s", rootStr)
	}

	if !strings.Contains(rootStr, `gitdir:/Users/x/projects/personal/`) {
		t.Errorf("root missing personal includeIf:\n%s", rootStr)
	}
}

func TestRegenerateNoOpWhenNothingToWrite(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, ".gitconfig")
	profilesDir := filepath.Join(tmpDir, "profiles")

	if err := os.MkdirAll(profilesDir, 0o755); err != nil {
		t.Fatalf("mkdir profiles: %v", err)
	}

	cfg := config.NewConfig()
	cfg.Profiles["work"] = &config.Profile{User: config.UserConfig{Name: "X"}}
	// No Current, no Directories — nothing to write to root manifest.

	g := NewGit(rootPath)

	if err := g.Regenerate(cfg, profilesDir); err != nil {
		t.Fatalf("Regenerate error: %v", err)
	}

	if _, err := os.Stat(rootPath); !os.IsNotExist(err) {
		t.Errorf("expected no root manifest, got err=%v", err)
	}

	// Per-profile file still gets written, regardless of whether root manifest is.
	if _, err := os.Stat(filepath.Join(profilesDir, "work.gitconfig")); err != nil {
		t.Errorf("expected work.gitconfig to exist: %v", err)
	}
}
```

Add `"github.com/aanogueira/git-context/internal/config"` to the imports of `git_test.go`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/git/ -run TestRegenerate -v`
Expected: FAIL — `Regenerate` undefined; also `cfg.Merge` not used directly so no related compile errors anticipated.

- [ ] **Step 3: Implement `Regenerate`**

Append to `internal/git/git.go`:

```go
// Regenerate writes one gitconfig file per profile under `profilesDir`,
// then writes the root manifest at `g.globalConfigPath`. Called by every
// mutating command (`switch`, `add` with dirs, `remove`, `dir add/remove`).
//
// Per-profile files use the merged (global + profile) configuration so they
// behave identically to the file `switch` used to write inline.
//
// The root manifest is written by WriteRootConfig and may be a no-op if no
// default profile is set and no directories are assigned.
func (g *Git) Regenerate(cfg *config.Config, profilesDir string) error {
	for name := range cfg.Profiles {
		merged, err := cfg.Merge(name)
		if err != nil {
			return errors.Wrapf(err, "failed to merge profile %q", name)
		}

		path := filepath.Join(profilesDir, name+".gitconfig")
		if err := g.WriteProfileFile(path, profileToFlatConfig(merged)); err != nil {
			return errors.Wrapf(err, "failed to write profile file for %q", name)
		}
	}

	defaultPath := ""
	if cfg.Current != "" {
		defaultPath = filepath.Join(profilesDir, cfg.Current+".gitconfig")
	}

	assignments := make(map[string]string)
	for path, profileName := range cfg.AssignmentMap() {
		assignments[path] = filepath.Join(profilesDir, profileName+".gitconfig")
	}

	return g.WriteRootConfig(defaultPath, assignments)
}

// profileToFlatConfig is the same converter that `cmd.profileToGitConfig`
// uses. Keep them in sync; we host it in the git package because Regenerate
// needs it without dragging in cmd.
func profileToFlatConfig(profile *config.Profile) map[string]any {
	out := make(map[string]any)

	if profile.User.Name != "" {
		out["user.name"] = profile.User.Name
	}

	if profile.User.Email != "" {
		out["user.email"] = profile.User.Email
	}

	if profile.User.SigningKey != "" {
		out["user.signingkey"] = profile.User.SigningKey
	}

	for _, url := range profile.URL {
		key := fmt.Sprintf("url \"%s\".insteadOf", url.Pattern)
		out[key] = url.InsteadOf
	}

	for _, section := range config.ConfigSections {
		if sectionMap := profile.GetSection(section); sectionMap != nil {
			flattenInto(out, section, sectionMap)
		}
	}

	return out
}

// flattenInto walks a nested map and writes leaf values keyed by dotted path.
func flattenInto(out map[string]any, prefix string, values map[string]any) {
	for k, v := range values {
		key := prefix + "." + k
		if m, ok := v.(map[string]any); ok {
			flattenInto(out, key, m)
		} else {
			out[key] = v
		}
	}
}
```

Add `"path/filepath"` and `"github.com/aanogueira/git-context/internal/config"` to the imports of `git.go`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/git/ -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/git/git.go internal/git/git_test.go
git commit --signoff --gpg-sign -m "feat(git): add Regenerate orchestrator"
```

---

## Task 9: Switch over `cmd/switch.go` to use `Regenerate`

Goal: keep `switch` behavior unchanged from the user's perspective — it sets the default profile — but write through `Regenerate` so the new file layout takes over. Drop the now-redundant inline `profileToGitConfig` helper from `cmd/switch.go`.

**Files:**
- Modify: `cmd/switch.go`
- Test: existing `cmd/cmd_test.go`

- [ ] **Step 1: Update `cmd/switch.go`**

Replace the body of `runSwitch` between the "Build the merged configuration" comment and the `cfg.Current = profileName` line with the regenerate call. Specifically, replace lines 69-83 of `cmd/switch.go` with:

```go
	g := git.NewGit(paths.GitConfigFile)

	if err := g.BackupConfig(paths.GitConfigBackup); err != nil {
		ui.PrintWarning(fmt.Sprintf("Failed to backup git config: %v", err))
	} else {
		ui.PrintInfo("Backed up git config to " + paths.GitConfigBackup)
	}

	// Update current profile bookkeeping before regenerate so the new
	// default is reflected in the manifest.
	if cfg.Current != "" && cfg.Current != profileName {
		cfg.Previous = cfg.Current
	}

	cfg.Current = profileName

	if err := g.Regenerate(cfg, paths.ProfilesDir); err != nil {
		ui.PrintError(fmt.Sprintf("Failed to regenerate git config: %v", err))

		return errors.Wrap(err, "failed to regenerate git config")
	}
```

Then delete the `cfg.Current = profileName` line that previously came after `WriteConfig`, plus the now-orphaned `profileToGitConfig` / `addSectionToConfig` / `addSectionToConfigRecursive` helpers at the bottom of the file (lines 103-166). The git package owns flattening now.

The `g := git.NewGit(...)` and backup blocks already exist at lines 60-67; do not duplicate them. Delete the original lines 60-95 entirely and replace with the block above. The `if err := cfg.SaveConfig(...)` block at lines 91-95 stays (move it to immediately after the `Regenerate` call).

After editing, the body of `runSwitch` from "Create Git instance" downward should read:

```go
	g := git.NewGit(paths.GitConfigFile)

	if err := g.BackupConfig(paths.GitConfigBackup); err != nil {
		ui.PrintWarning(fmt.Sprintf("Failed to backup git config: %v", err))
	} else {
		ui.PrintInfo("Backed up git config to " + paths.GitConfigBackup)
	}

	if cfg.Current != "" && cfg.Current != profileName {
		cfg.Previous = cfg.Current
	}

	cfg.Current = profileName

	if err := g.Regenerate(cfg, paths.ProfilesDir); err != nil {
		ui.PrintError(fmt.Sprintf("Failed to regenerate git config: %v", err))

		return errors.Wrap(err, "failed to regenerate git config")
	}

	if err := cfg.SaveConfig(paths.ConfigFile); err != nil {
		ui.PrintError(fmt.Sprintf("Failed to save config: %v", err))

		return errors.Wrap(err, "failed to save config")
	}

	ui.PrintSuccess(fmt.Sprintf("Switched to profile '%s'", profileName))
	ui.PrintInfo(fmt.Sprintf("User: %s <%s>", profile.User.Name, profile.User.Email))

	return nil
```

Delete the trailing helpers (`profileToGitConfig`, `addSectionToConfig`, `addSectionToConfigRecursive`).

- [ ] **Step 2: Build to verify it compiles**

Run: `go build ./...`
Expected: success.

- [ ] **Step 3: Run the full cmd test suite**

Run: `go test ./cmd/ -v`
Expected: existing switch tests pass — `Regenerate` produces a `~/.gitconfig` whose effect on `git config user.email` is identical to before for a single-profile, no-dirs case (the "no assignments" path of `WriteRootConfig` writes only the `[include]` block, which git then reads from the per-profile file).

If a test was reading `~/.gitconfig` directly to verify content, update it to read the per-profile file at `paths.ProfilesDir/<name>.gitconfig` instead. Inspect any failures and adjust assertions in `cmd/cmd_test.go` accordingly.

- [ ] **Step 4: Commit**

```bash
git add cmd/switch.go cmd/cmd_test.go
git commit --signoff --gpg-sign -m "refactor(switch): write via Regenerate, emit manifest"
```

---

## Task 10: `remove` confirmation when profile has directories

**Files:**
- Modify: `cmd/remove.go`
- Modify: `cmd/cmd_test.go`

- [ ] **Step 1: Write the failing test**

Append to `cmd/cmd_test.go`:

```go
func TestRemoveRegeneratesAndDropsDirectories(t *testing.T) {
	t.Parallel()

	// Set up a tmp HOME so paths point at a sandbox.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	paths, err := config.NewPaths()
	if err != nil {
		t.Fatalf("NewPaths: %v", err)
	}

	cfg := config.NewConfig()
	cfg.Profiles["work"] = &config.Profile{
		User:        config.UserConfig{Name: "X", Email: "x@work"},
		Directories: []string{"/tmp/work/"},
	}
	cfg.Current = "work"

	if err := cfg.SaveConfig(paths.ConfigFile); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	// Pre-generate so a stale work.gitconfig exists.
	g := git.NewGit(paths.GitConfigFile)
	if err := g.Regenerate(cfg, paths.ProfilesDir); err != nil {
		t.Fatalf("Regenerate: %v", err)
	}

	// Remove the profile (auto-confirm via removeProfileForTest helper below).
	if err := removeProfileForTest(paths, "work"); err != nil {
		t.Fatalf("removeProfileForTest: %v", err)
	}

	// Reload and verify the profile + its directories are gone.
	loaded, err := config.LoadConfig(paths.ConfigFile)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if _, exists := loaded.Profiles["work"]; exists {
		t.Error("profile 'work' still present after remove")
	}

	// Root manifest should no longer reference Mollie or work.gitconfig.
	if _, err := os.Stat(paths.GitConfigFile); err == nil {
		data, _ := os.ReadFile(paths.GitConfigFile)
		if strings.Contains(string(data), "/tmp/work/") {
			t.Errorf("root manifest still references removed dir:\n%s", data)
		}
	}
}
```

`removeProfileForTest` is a small helper that mirrors `runRemove` but skips the interactive confirmation. Add it to `cmd/cmd_test.go`:

```go
// removeProfileForTest performs the same regeneration logic as runRemove
// but without the interactive prompt — used so tests can exercise the
// post-confirmation code path.
func removeProfileForTest(paths *config.Paths, name string) error {
	cfg, err := config.LoadConfig(paths.ConfigFile)
	if err != nil {
		return err
	}

	if err := cfg.RemoveProfile(name); err != nil {
		return err
	}

	if err := cfg.SaveConfig(paths.ConfigFile); err != nil {
		return err
	}

	return git.NewGit(paths.GitConfigFile).Regenerate(cfg, paths.ProfilesDir)
}
```

Add the necessary imports (`os`, `strings`, `github.com/aanogueira/git-context/internal/git`) at the top of `cmd_test.go` if not already there.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ -run TestRemoveRegeneratesAndDropsDirectories -v`
Expected: FAIL — `runRemove` does not yet call `Regenerate`, so the manifest still contains the assignment.

- [ ] **Step 3: Update `cmd/remove.go`**

Replace the body of `runRemove` after `cfg.GetProfile` succeeds with:

```go
	hasDirs := len(cfg.Profiles[profileName].Directories) > 0

	prompt := fmt.Sprintf("Are you sure you want to remove profile '%s'?", profileName)
	if hasDirs {
		prompt = fmt.Sprintf(
			"Profile '%s' has %d assigned director%s. Remove anyway?",
			profileName,
			len(cfg.Profiles[profileName].Directories),
			plural(len(cfg.Profiles[profileName].Directories), "y", "ies"),
		)
	}

	confirm, err := ui.PromptConfirm(prompt)
	if err != nil {
		ui.PrintWarning("Removal canceled")

		return errors.Wrap(err, "failed to confirm removal")
	}

	if !confirm {
		ui.PrintWarning("Removal canceled")

		return nil
	}

	if err := cfg.RemoveProfile(profileName); err != nil {
		ui.PrintError(fmt.Sprintf("Failed to remove profile: %v", err))

		return errors.Wrap(err, "failed to remove profile")
	}

	// If the removed profile was the current default, clear it.
	if cfg.Current == profileName {
		cfg.Current = ""
	}

	if cfg.Previous == profileName {
		cfg.Previous = ""
	}

	if err := cfg.SaveConfig(paths.ConfigFile); err != nil {
		ui.PrintError(fmt.Sprintf("Failed to save config: %v", err))

		return errors.Wrap(err, "failed to save config")
	}

	g := git.NewGit(paths.GitConfigFile)
	if err := g.Regenerate(cfg, paths.ProfilesDir); err != nil {
		ui.PrintError(fmt.Sprintf("Failed to regenerate git config: %v", err))

		return errors.Wrap(err, "failed to regenerate git config")
	}

	ui.PrintSuccess(fmt.Sprintf("Profile '%s' removed successfully", profileName))

	return nil
```

Add the `plural` helper at the bottom of `remove.go`:

```go
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}

	return many
}
```

Add `"github.com/aanogueira/git-context/internal/git"` to the imports.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/ -v`
Expected: PASS, including the new test and all existing remove tests.

- [ ] **Step 5: Commit**

```bash
git add cmd/remove.go cmd/cmd_test.go
git commit --signoff --gpg-sign -m "feat(remove): regenerate manifest, prompt mentions assigned dirs"
```

---

## Task 11: `list` shows a "Dirs" column

**Files:**
- Modify: `cmd/list.go`
- Modify: `cmd/cmd_test.go`

- [ ] **Step 1: Write the failing test**

Append to `cmd/cmd_test.go`:

```go
func TestListProfilesShowsDirsColumn(t *testing.T) {
	t.Parallel()

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	paths, _ := config.NewPaths()

	cfg := config.NewConfig()
	cfg.Profiles["work"] = &config.Profile{
		User:        config.UserConfig{Email: "x@work"},
		Directories: []string{"/a/", "/b/"},
	}
	cfg.Profiles["personal"] = &config.Profile{User: config.UserConfig{Email: "x@home"}}

	if err := cfg.SaveConfig(paths.ConfigFile); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	out := captureStdout(t, func() {
		if err := runList(nil, nil); err != nil {
			t.Fatalf("runList: %v", err)
		}
	})

	if !strings.Contains(out, "Dirs") {
		t.Errorf("output missing Dirs header:\n%s", out)
	}

	if !strings.Contains(out, "2") {
		t.Errorf("output missing dir count of 2 for work:\n%s", out)
	}
}
```

If `captureStdout` doesn't already exist in `cmd_test.go`, add it:

```go
// captureStdout runs fn and returns whatever it wrote to os.Stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	old := os.Stdout
	os.Stdout = w

	done := make(chan string)

	go func() {
		var buf bytes.Buffer

		_, _ = buf.ReadFrom(r)
		done <- buf.String()
	}()

	fn()

	w.Close()
	os.Stdout = old

	return <-done
}
```

Add `"bytes"` to the imports if needed.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ -run TestListProfilesShowsDirsColumn -v`
Expected: FAIL — table headers don't include "Dirs".

- [ ] **Step 3: Update `cmd/list.go`**

Replace the rows construction and `PrintTable` call with:

```go
	rows := make([][]string, len(profiles))
	for i, profile := range profiles {
		status := ""
		if profile == cfg.Current {
			status = "● (active)"
		}

		p, _ := cfg.GetProfile(profile)

		email := ""

		dirs := "0"

		if p != nil {
			if p.User.Email != "" {
				email = p.User.Email
			}

			dirs = fmt.Sprintf("%d", len(p.Directories))
		}

		rows[i] = []string{profile, email, dirs, status}
	}

	ui.PrintTable([]string{"Profile", "Email", "Dirs", "Status"}, rows)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/list.go cmd/cmd_test.go
git commit --signoff --gpg-sign -m "feat(list): show assigned-directory count column"
```

---

## Task 12: `show` displays assigned directories

**Files:**
- Modify: `cmd/show.go`
- Modify: `cmd/cmd_test.go`

- [ ] **Step 1: Write the failing test**

Append to `cmd/cmd_test.go`:

```go
func TestShowDisplaysDirectories(t *testing.T) {
	t.Parallel()

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	paths, _ := config.NewPaths()

	cfg := config.NewConfig()
	cfg.Profiles["work"] = &config.Profile{
		User:        config.UserConfig{Name: "X", Email: "x@work"},
		Directories: []string{"/Users/x/work/", "/Users/x/Mollie/"},
	}

	if err := cfg.SaveConfig(paths.ConfigFile); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	out := captureStdout(t, func() {
		if err := runShow(nil, []string{"work"}); err != nil {
			t.Fatalf("runShow: %v", err)
		}
	})

	if !strings.Contains(out, "Directories") {
		t.Errorf("output missing Directories label:\n%s", out)
	}

	if !strings.Contains(out, "/Users/x/work/") {
		t.Errorf("output missing assigned dir:\n%s", out)
	}

	if !strings.Contains(out, "/Users/x/Mollie/") {
		t.Errorf("output missing assigned dir:\n%s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ -run TestShowDisplaysDirectories -v`
Expected: FAIL — `runShow` does not print directories.

- [ ] **Step 3: Update `cmd/show.go`**

Append, after the URL rewrites block:

```go
	if len(profile.Directories) > 0 {
		fmt.Println()
		ui.PrintInfo("Directories:")

		for _, dir := range profile.Directories {
			ui.PrintInfo("  " + dir)
		}
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/ -run TestShowDisplaysDirectories -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/show.go cmd/cmd_test.go
git commit --signoff --gpg-sign -m "feat(show): list assigned directories"
```

---

## Task 13: `current` shows the effective profile in `$PWD`

Goal: after the existing "Current Profile" output, run `git config --show-origin user.email` from `$PWD` and, if the resolved file is one of our profile files, print which profile is effective here.

**Files:**
- Modify: `cmd/current.go`
- Modify: `cmd/cmd_test.go`

- [ ] **Step 1: Write the failing test**

Append to `cmd/cmd_test.go`:

```go
func TestCurrentShowsEffectiveProfileInCwd(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	paths, _ := config.NewPaths()

	cfg := config.NewConfig()
	cfg.Profiles["work"] = &config.Profile{
		User:        config.UserConfig{Name: "Andre", Email: "a@work.com"},
		Directories: []string{},
	}
	cfg.Profiles["personal"] = &config.Profile{
		User:        config.UserConfig{Name: "Andre", Email: "a@home.com"},
		Directories: []string{},
	}
	cfg.Current = "work"

	// Create a fake repo dir and assign it to personal.
	repoDir := filepath.Join(tmpHome, "personal-repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	if out, err := exec.Command("git", "-C", repoDir, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	cfg.Profiles["personal"].Directories = []string{repoDir + "/"}

	if err := cfg.SaveConfig(paths.ConfigFile); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	g := git.NewGit(paths.GitConfigFile)
	if err := g.Regenerate(cfg, paths.ProfilesDir); err != nil {
		t.Fatalf("Regenerate: %v", err)
	}

	// Chdir into the assigned repo and check current.
	oldDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(oldDir) })

	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	out := captureStdout(t, func() {
		if err := runCurrent(nil, nil); err != nil {
			t.Fatalf("runCurrent: %v", err)
		}
	})

	if !strings.Contains(out, "Effective in") {
		t.Errorf("output missing 'Effective in' line:\n%s", out)
	}

	if !strings.Contains(out, "personal") {
		t.Errorf("expected 'personal' to be effective in this dir:\n%s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ -run TestCurrentShowsEffectiveProfileInCwd -v`
Expected: FAIL — `runCurrent` doesn't shell out to git.

- [ ] **Step 3: Update `cmd/current.go`**

Replace the file's contents with:

```go
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/aanogueira/git-context/internal/config"
	"github.com/aanogueira/git-context/internal/ui"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
)

var currentCmd = &cobra.Command{
	Use:   "current",
	Short: "Show the currently active profile",
	Long:  `Display which git configuration profile is currently active globally and effective in the current directory.`,
	RunE:  runCurrent,
}

func runCurrent(cmd *cobra.Command, args []string) error {
	paths, err := config.NewPaths()
	if err != nil {
		ui.PrintError(fmt.Sprintf("Failed to get paths: %v", err))

		return errors.Wrap(err, "failed to get paths")
	}

	cfg, err := config.LoadConfig(paths.ConfigFile)
	if err != nil {
		ui.PrintError(fmt.Sprintf("Failed to load config: %v", err))

		return errors.Wrap(err, "failed to load config")
	}

	ui.PrintHeader("Current Profile")

	if cfg.Current == "" {
		ui.PrintWarning("No active profile set")
	} else {
		profile, err := cfg.GetProfile(cfg.Current)
		if err != nil {
			ui.PrintError(fmt.Sprintf("Active profile not found: %v", err))

			return errors.Wrap(err, "failed to get active profile")
		}

		ui.PrintInfo("Default: " + cfg.Current)
		ui.PrintInfo("Name: " + profile.User.Name)
		ui.PrintInfo("Email: " + profile.User.Email)
	}

	if effective := effectiveProfileInCwd(paths, cfg); effective != "" {
		ui.PrintInfo("Effective in " + currentDir() + ": " + effective)
	}

	return nil
}

// effectiveProfileInCwd asks git which file user.email comes from in the
// current working directory. If the answer is one of our per-profile files,
// returns the profile name; otherwise returns "".
func effectiveProfileInCwd(paths *config.Paths, cfg *config.Config) string {
	cmd := exec.Command("git", "config", "--show-origin", "user.email")

	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	// Output format: "file:/path/to/source\tvalue"
	line := strings.TrimSpace(string(out))

	parts := strings.SplitN(line, "\t", 2)
	if len(parts) == 0 {
		return ""
	}

	origin := strings.TrimPrefix(parts[0], "file:")

	for name := range cfg.Profiles {
		profilePath := filepath.Join(paths.ProfilesDir, name+".gitconfig")
		if origin == profilePath {
			return name
		}
	}

	return ""
}

func currentDir() string {
	d, err := os.Getwd()
	if err != nil {
		return "."
	}

	return d
}

func init() {
	rootCmd.AddCommand(currentCmd)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/ -run TestCurrent -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/current.go cmd/cmd_test.go
git commit --signoff --gpg-sign -m "feat(current): show effective profile in cwd"
```

---

## Task 14: Optional "assign directories" prompt in `add`

Goal: after the URL-rewrites prompt, ask whether the user wants to assign directories to the new profile. If yes, accept paths in a loop, normalize them, and store them on the profile. Trigger `Regenerate` only when at least one directory was added.

**Files:**
- Modify: `cmd/add.go`
- Modify: `cmd/cmd_test.go`

- [ ] **Step 1: Update `cmd/add.go`**

Right before `if err := cfg.AddProfile(profileName, profile); err != nil {`, add the directory prompt block:

```go
	addDirs, _ := ui.PromptConfirm("Assign directories to this profile?")
	if addDirs {
		for {
			path, err := ui.PromptText("Directory path (leave empty to stop)", "")
			if err != nil || path == "" {
				break
			}

			normalized, err := config.NormalizeDir(path)
			if err != nil {
				ui.PrintWarning(fmt.Sprintf("Skipping %q: %v", path, err))

				continue
			}

			profile.Directories = append(profile.Directories, normalized)

			more, _ := ui.PromptConfirm("Add another directory?")
			if !more {
				break
			}
		}
	}
```

After the existing `cfg.SaveConfig(...)` call succeeds, add:

```go
	if len(profile.Directories) > 0 || cfg.Current != "" {
		g := git.NewGit(paths.GitConfigFile)
		if err := g.Regenerate(cfg, paths.ProfilesDir); err != nil {
			ui.PrintError(fmt.Sprintf("Failed to regenerate git config: %v", err))

			return errors.Wrap(err, "failed to regenerate git config")
		}
	}
```

Add `"github.com/aanogueira/git-context/internal/git"` to the imports.

- [ ] **Step 2: Build to verify it compiles**

Run: `go build ./...`
Expected: success.

- [ ] **Step 3: Run existing add tests**

Run: `go test ./cmd/ -run TestAdd -v`
Expected: existing tests still pass. Adjust assertions if a test was relying on the absence of the new prompt (the prompt is interactive — if the test stubs prompts, ensure the new one returns "No" by default).

- [ ] **Step 4: Commit**

```bash
git add cmd/add.go cmd/cmd_test.go
git commit --signoff --gpg-sign -m "feat(add): optional directory-assignment prompt"
```

---

## Task 15: New `dir` command (`add` / `remove` / `list`)

Goal: the user-facing surface for managing directory assignments.

**Files:**
- Create: `cmd/dir.go`
- Create: `cmd/dir_test.go`

- [ ] **Step 1: Write the failing tests**

Create `cmd/dir_test.go`:

```go
package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aanogueira/git-context/internal/config"
)

func TestDirAddAssignsAndRegenerates(t *testing.T) {
	t.Parallel()

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	paths, _ := config.NewPaths()

	cfg := config.NewConfig()
	cfg.Profiles["work"] = &config.Profile{User: config.UserConfig{Name: "X", Email: "x@work"}}
	cfg.Current = "work"

	if err := cfg.SaveConfig(paths.ConfigFile); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	if err := runDirAdd(nil, []string{"/tmp/myrepo", "work"}); err != nil {
		t.Fatalf("runDirAdd: %v", err)
	}

	loaded, _ := config.LoadConfig(paths.ConfigFile)
	if got := loaded.Profiles["work"].Directories; len(got) != 1 || got[0] != "/tmp/myrepo/" {
		t.Errorf("Directories = %v, want [/tmp/myrepo/]", got)
	}

	root, err := os.ReadFile(paths.GitConfigFile)
	if err != nil {
		t.Fatalf("read root: %v", err)
	}

	if !strings.Contains(string(root), `gitdir:/tmp/myrepo/`) {
		t.Errorf("root manifest missing includeIf:\n%s", root)
	}
}

func TestDirAddRejectsDuplicate(t *testing.T) {
	t.Parallel()

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	paths, _ := config.NewPaths()

	cfg := config.NewConfig()
	cfg.Profiles["work"] = &config.Profile{Directories: []string{"/tmp/x/"}}
	cfg.Profiles["personal"] = &config.Profile{}

	if err := cfg.SaveConfig(paths.ConfigFile); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	err := runDirAdd(nil, []string{"/tmp/x", "personal"})
	if err == nil {
		t.Fatal("expected error for duplicate, got nil")
	}

	if !strings.Contains(err.Error(), "already assigned") {
		t.Errorf("error = %q, want it to mention 'already assigned'", err.Error())
	}
}

func TestDirAddWarnsWhenNoDefaultProfile(t *testing.T) {
	t.Parallel()

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	paths, _ := config.NewPaths()

	cfg := config.NewConfig()
	cfg.Profiles["work"] = &config.Profile{User: config.UserConfig{Name: "X"}}
	// cfg.Current intentionally left empty.

	if err := cfg.SaveConfig(paths.ConfigFile); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	out := captureStdout(t, func() {
		if err := runDirAdd(nil, []string{"/tmp/x", "work"}); err != nil {
			t.Fatalf("runDirAdd: %v", err)
		}
	})

	if !strings.Contains(out, "no default profile set") {
		t.Errorf("missing default-profile warning:\n%s", out)
	}
}

func TestDirRemoveUnassignsAndRegenerates(t *testing.T) {
	t.Parallel()

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	paths, _ := config.NewPaths()

	cfg := config.NewConfig()
	cfg.Profiles["work"] = &config.Profile{
		User:        config.UserConfig{Name: "X", Email: "x@work"},
		Directories: []string{"/tmp/myrepo/"},
	}
	cfg.Current = "work"

	if err := cfg.SaveConfig(paths.ConfigFile); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	if err := runDirRemove(nil, []string{"/tmp/myrepo"}); err != nil {
		t.Fatalf("runDirRemove: %v", err)
	}

	loaded, _ := config.LoadConfig(paths.ConfigFile)
	if got := loaded.Profiles["work"].Directories; len(got) != 0 {
		t.Errorf("Directories = %v, want empty", got)
	}

	if data, err := os.ReadFile(paths.GitConfigFile); err == nil {
		if strings.Contains(string(data), "/tmp/myrepo") {
			t.Errorf("manifest still references removed dir:\n%s", data)
		}
	}
}

func TestDirListShowsAssignments(t *testing.T) {
	t.Parallel()

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	paths, _ := config.NewPaths()

	existsDir := filepath.Join(tmpHome, "exists")
	_ = os.MkdirAll(existsDir, 0o755)

	cfg := config.NewConfig()
	cfg.Profiles["work"] = &config.Profile{
		Directories: []string{existsDir + "/", "/nonexistent/path/"},
	}

	if err := cfg.SaveConfig(paths.ConfigFile); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	out := captureStdout(t, func() {
		if err := runDirList(nil, nil); err != nil {
			t.Fatalf("runDirList: %v", err)
		}
	})

	if !strings.Contains(out, existsDir) {
		t.Errorf("output missing existing dir:\n%s", out)
	}

	if !strings.Contains(out, "/nonexistent/path/") {
		t.Errorf("output missing nonexistent dir:\n%s", out)
	}

	if !strings.Contains(out, "✓") {
		t.Errorf("expected ✓ for existing dir:\n%s", out)
	}

	if !strings.Contains(out, "✗") {
		t.Errorf("expected ✗ for missing dir:\n%s", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/ -run TestDir -v`
Expected: FAIL — `runDirAdd`/`runDirRemove`/`runDirList` undefined.

- [ ] **Step 3: Implement `cmd/dir.go`**

Create `cmd/dir.go`:

```go
package cmd

import (
	"fmt"
	"os"
	"sort"

	"github.com/aanogueira/git-context/internal/config"
	"github.com/aanogueira/git-context/internal/git"
	"github.com/aanogueira/git-context/internal/ui"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
)

var dirCmd = &cobra.Command{
	Use:   "dir",
	Short: "Manage directory-to-profile assignments",
	Long:  `Assign filesystem paths to git profiles. When inside an assigned directory, git uses that profile via includeIf.`,
}

var dirAddCmd = &cobra.Command{
	Use:   "add [path] [profile]",
	Short: "Assign a directory to a profile",
	Args:  cobra.ExactArgs(2),
	RunE:  runDirAdd,
}

var dirRemoveCmd = &cobra.Command{
	Use:   "remove [path]",
	Short: "Remove a directory assignment",
	Args:  cobra.ExactArgs(1),
	RunE:  runDirRemove,
}

var dirListCmd = &cobra.Command{
	Use:   "list",
	Short: "List directory assignments",
	RunE:  runDirList,
}

func runDirAdd(cmd *cobra.Command, args []string) error {
	rawPath, profileName := args[0], args[1]

	paths, err := config.NewPaths()
	if err != nil {
		return errors.Wrap(err, "failed to get paths")
	}

	cfg, err := config.LoadConfig(paths.ConfigFile)
	if err != nil {
		return errors.Wrap(err, "failed to load config")
	}

	normalized, err := config.NormalizeDir(rawPath)
	if err != nil {
		ui.PrintError(err.Error())

		return err
	}

	if err := cfg.AssignDir(normalized, profileName); err != nil {
		ui.PrintError(err.Error())

		return err
	}

	if _, err := os.Stat(normalized); os.IsNotExist(err) {
		ui.PrintWarning(fmt.Sprintf("Directory %s does not exist yet (assignment saved anyway)", normalized))
	}

	if err := cfg.SaveConfig(paths.ConfigFile); err != nil {
		return errors.Wrap(err, "failed to save config")
	}

	g := git.NewGit(paths.GitConfigFile)
	if err := g.Regenerate(cfg, paths.ProfilesDir); err != nil {
		return errors.Wrap(err, "failed to regenerate git config")
	}

	ui.PrintSuccess(fmt.Sprintf("Assigned %s → %s", normalized, profileName))

	if cfg.Current == "" {
		ui.PrintWarning("no default profile set; run 'switch <name>' to apply one outside assigned directories")
	}

	return nil
}

func runDirRemove(cmd *cobra.Command, args []string) error {
	rawPath := args[0]

	paths, err := config.NewPaths()
	if err != nil {
		return errors.Wrap(err, "failed to get paths")
	}

	cfg, err := config.LoadConfig(paths.ConfigFile)
	if err != nil {
		return errors.Wrap(err, "failed to load config")
	}

	normalized, err := config.NormalizeDir(rawPath)
	if err != nil {
		ui.PrintError(err.Error())

		return err
	}

	if err := cfg.UnassignDir(normalized); err != nil {
		ui.PrintError(err.Error())

		return err
	}

	if err := cfg.SaveConfig(paths.ConfigFile); err != nil {
		return errors.Wrap(err, "failed to save config")
	}

	g := git.NewGit(paths.GitConfigFile)
	if err := g.Regenerate(cfg, paths.ProfilesDir); err != nil {
		return errors.Wrap(err, "failed to regenerate git config")
	}

	ui.PrintSuccess(fmt.Sprintf("Removed assignment for %s", normalized))

	return nil
}

func runDirList(cmd *cobra.Command, args []string) error {
	paths, err := config.NewPaths()
	if err != nil {
		return errors.Wrap(err, "failed to get paths")
	}

	cfg, err := config.LoadConfig(paths.ConfigFile)
	if err != nil {
		return errors.Wrap(err, "failed to load config")
	}

	assignments := cfg.AssignmentMap()
	if len(assignments) == 0 {
		ui.PrintWarning("No directory assignments. Use 'git-context dir add <path> <profile>' to add one.")

		return nil
	}

	keys := make([]string, 0, len(assignments))
	for k := range assignments {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	rows := make([][]string, 0, len(keys))

	for _, dir := range keys {
		mark := "✓"
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			mark = "✗"
		}

		rows = append(rows, []string{dir, assignments[dir], mark})
	}

	ui.PrintHeader("Directory Assignments")
	ui.PrintTable([]string{"Directory", "Profile", "Exists"}, rows)

	return nil
}

func init() {
	dirCmd.AddCommand(dirAddCmd)
	dirCmd.AddCommand(dirRemoveCmd)
	dirCmd.AddCommand(dirListCmd)
	rootCmd.AddCommand(dirCmd)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/ -run TestDir -v`
Expected: all PASS.

Then run the full suite to confirm nothing else broke:

Run: `go test ./... -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/dir.go cmd/dir_test.go
git commit --signoff --gpg-sign -m "feat(dir): add 'dir add/remove/list' subcommands"
```

---

## Task 16: End-to-end integration test

Goal: drive the full lifecycle through the CLI surfaces and verify the resulting `~/.gitconfig` is what git would actually consume.

**Files:**
- Create: `cmd/integration_test.go`

- [ ] **Step 1: Write the integration test**

Create `cmd/integration_test.go`:

```go
package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aanogueira/git-context/internal/config"
)

// TestEndToEndDirectoryAssignment exercises the full lifecycle: switch to a
// default profile, assign a directory to a different profile, verify that
// inside the assigned directory git resolves the right user.email.
func TestEndToEndDirectoryAssignment(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	paths, err := config.NewPaths()
	if err != nil {
		t.Fatalf("NewPaths: %v", err)
	}

	// Set up two profiles in config.yaml.
	cfg := config.NewConfig()
	cfg.Profiles["work"] = &config.Profile{
		User: config.UserConfig{Name: "Andre", Email: "a@work.com"},
	}
	cfg.Profiles["personal"] = &config.Profile{
		User: config.UserConfig{Name: "Andre", Email: "a@home.com"},
	}

	if err := cfg.SaveConfig(paths.ConfigFile); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	// Switch makes 'work' the default and regenerates.
	if err := runSwitch(nil, []string{"work"}); err != nil {
		t.Fatalf("runSwitch: %v", err)
	}

	// Create a real git repo and assign it to 'personal'.
	repo := filepath.Join(tmpHome, "personal-repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	if out, err := exec.Command("git", "-C", repo, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	if err := runDirAdd(nil, []string{repo, "personal"}); err != nil {
		t.Fatalf("runDirAdd: %v", err)
	}

	// Outside the repo (use HOME), default 'work' should be effective.
	out, err := exec.Command("git", "-C", tmpHome, "config", "user.email").Output()
	if err != nil {
		t.Fatalf("git config user.email outside: %v", err)
	}

	if got := strings.TrimSpace(string(out)); got != "a@work.com" {
		t.Errorf("default user.email = %q, want a@work.com", got)
	}

	// Inside the repo, 'personal' should override.
	out, err = exec.Command("git", "-C", repo, "config", "user.email").Output()
	if err != nil {
		t.Fatalf("git config user.email inside: %v", err)
	}

	if got := strings.TrimSpace(string(out)); got != "a@home.com" {
		t.Errorf("override user.email = %q, want a@home.com", got)
	}

	// dir remove undoes the override.
	if err := runDirRemove(nil, []string{repo}); err != nil {
		t.Fatalf("runDirRemove: %v", err)
	}

	out, err = exec.Command("git", "-C", repo, "config", "user.email").Output()
	if err != nil {
		t.Fatalf("git config user.email after remove: %v", err)
	}

	if got := strings.TrimSpace(string(out)); got != "a@work.com" {
		t.Errorf("after dir remove, user.email = %q, want a@work.com", got)
	}

	// Sanity: profile files exist on disk.
	for _, name := range []string{"work", "personal"} {
		if _, err := os.Stat(filepath.Join(paths.ProfilesDir, name+".gitconfig")); err != nil {
			t.Errorf("expected %s.gitconfig: %v", name, err)
		}
	}

	// Sanity: per-profile file content matches the profile.
	workFile := filepath.Join(paths.ProfilesDir, "work.gitconfig")
	data, _ := os.ReadFile(workFile)
	if !strings.Contains(string(data), "a@work.com") {
		t.Errorf("work.gitconfig missing email:\n%s", data)
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./cmd/ -run TestEndToEndDirectoryAssignment -v`
Expected: PASS. If `git` is not on PATH, the test skips.

- [ ] **Step 3: Run the full suite**

Run: `go test ./... -v`
Expected: all PASS (or skipped where git isn't available).

- [ ] **Step 4: Run linter**

Run: `make lint`
Expected: no issues. If the linter complains about `nestif`/`gocognit` thresholds in new files, address by extracting helpers or apply the existing repo-style `//nolint:` directive used in `git.go`.

- [ ] **Step 5: Commit**

```bash
git add cmd/integration_test.go
git commit --signoff --gpg-sign -m "test: end-to-end directory-based profile lifecycle"
```

---

## Task 17: README — document `dir` commands and the new file layout

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Add a "Directory-Based Profiles" section**

Insert a new section in `README.md` after "All Available Commands" (around line 187), and update the command table.

In the command table, add three rows:

```markdown
| `git-context dir add <path> <profile>` | Assign a directory to a profile (auto-applied via includeIf) |
| `git-context dir remove <path>`        | Remove a directory assignment |
| `git-context dir list`                 | List all directory assignments |
```

Then add a new section:

```markdown
### Directory-Based Profiles

Assign filesystem paths to profiles so git applies the right identity automatically when you're inside them — no need to remember to `switch`.

```bash
# Make 'work' the default profile (used everywhere unless overridden)
git-context switch work

# Assign specific directories to other profiles
git-context dir add ~/projects/personal personal
git-context dir add ~/Mollie work

# See all assignments
git-context dir list
```

Under the hood git-context generates one gitconfig file per profile under `~/.config/git-context/profiles/` and rewrites `~/.gitconfig` as a thin manifest of `[include]` and `[includeIf "gitdir:..."]` blocks. Every mutating command regenerates these files, so the YAML at `~/.config/git-context/config.yaml` is always the source of truth.

> Note: git-context owns `~/.gitconfig` end-to-end — don't run `git config --global` directly. Edit the YAML or use the CLI instead.
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit --signoff --gpg-sign -m "docs: document dir commands and new file layout"
```

---

## Self-Review Checklist (run before handoff)

- [ ] Every spec section has at least one task implementing it:
  - File layout → Tasks 5, 6, 7, 8
  - Configuration schema → Tasks 1, 2
  - Path normalization rules → Task 2
  - Validation (conflict + nonexistent warning) → Task 4 (config), Task 15 (CLI warning)
  - `dir add/remove/list` → Task 15
  - Changed `switch` → Task 9
  - Changed `current` → Task 13
  - Changed `add` → Task 14
  - Changed `remove` → Task 10
  - Changed `list` → Task 11
  - Changed `show` → Task 12
  - Atomicity (`*.tmp` + rename) → Task 6
  - Migration (implicit on first regenerate) → covered by Task 9 behavior
  - Edge case: no default profile + `dir add` warning → Task 15 test
  - Testing additions → Tasks 2-15 each ship tests
  - Out-of-scope items → not implemented (correct)
- [ ] No placeholders (`TBD`, "implement later", "add error handling") remain
- [ ] Function and method names are consistent across tasks (`Regenerate`, `WriteProfileFile`, `WriteRootConfig`, `NormalizeDir`, `LookupDir`, `AssignmentMap`, `AssignDir`, `UnassignDir`, `runDirAdd`, `runDirRemove`, `runDirList`)
- [ ] Every "Run:" step has an "Expected:" outcome
- [ ] Every code-modifying step shows the actual code
