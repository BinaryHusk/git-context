package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aanogueira/git-context/internal/config"
)

func TestDirAddAssignsAndRegenerates(t *testing.T) {
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
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	paths, _ := config.NewPaths()

	cfg := config.NewConfig()
	cfg.Profiles["work"] = &config.Profile{User: config.UserConfig{Name: "X"}}

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
