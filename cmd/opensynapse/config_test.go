package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfigDeadlockOnInvalidScene(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	cfgDir := filepath.Join(dir, "OpenSynapse")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// An invalid scene (empty name) triggers normalize error which calls tr() -> language() -> cfgMu.Lock()
	badCfg := []byte(`{"scenes":{"scenes":[{"name":"","apps":[]}]}}`)
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), badCfg, 0o644); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		loadConfig()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("loadConfig deadlocked on invalid scene")
	}
}

func TestLoadConfigCorruptedJSONBackedUp(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	cfgDir := filepath.Join(dir, "OpenSynapse")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(cfgDir, "config.json")
	if err := os.WriteFile(p, []byte(`{not valid json`), 0o644); err != nil {
		t.Fatal(err)
	}

	loadConfig()

	badPath := p + ".bad"
	if _, err := os.Stat(badPath); err != nil {
		t.Fatalf("corrupted config was not renamed to .bad: %v", err)
	}
}

func TestLoadConfigCorruptedRenameFailedBlocksSave(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	cfgDir := filepath.Join(dir, "OpenSynapse")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(cfgDir, "config.json")
	corrupted := []byte(`{not valid json`)
	if err := os.WriteFile(p, corrupted, 0o644); err != nil {
		t.Fatal(err)
	}
	// Create config.json.bad as a non-empty directory so rename fails.
	badDir := p + ".bad"
	if err := os.MkdirAll(filepath.Join(badDir, "blocker"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		saveMu.Lock()
		saveBlocked = false
		saveMu.Unlock()
	})

	loadConfig()

	if err := saveConfig(); err == nil {
		t.Fatal("expected saveConfig to fail when backup failed")
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(corrupted) {
		t.Fatalf("expected corrupted config to be preserved, got: %s", string(got))
	}
}

// writeConfig puts a settings file into a temporary APPDATA and returns its
// path; the settings in memory and the save block come back after the test.
func writeConfig(t *testing.T, content string) string {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	p := filepath.Join(dir, "OpenSynapse", "config.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgMu.Lock()
	saved := cfg
	cfg = defaultConfig()
	cfgMu.Unlock()
	t.Cleanup(func() {
		cfgMu.Lock()
		cfg = saved
		cfgMu.Unlock()
		saveMu.Lock()
		saveBlocked = false
		saveMu.Unlock()
	})
	return p
}

// One key that does not decode: the others load, and the file is kept as it
// was before a save writes the default over that key.
func TestLoadConfigUnreadKeyBackedUp(t *testing.T) {
	const content = `{"notify":false,"macros":"oops","keyboards":7}`
	p := writeConfig(t, content)

	loadConfig()

	cfgMu.Lock()
	notify := cfg.Notify
	cfgMu.Unlock()
	if notify {
		t.Fatal("the readable keys were not loaded")
	}
	if got, err := os.ReadFile(p + ".bad"); err != nil || string(got) != content {
		t.Fatalf("backup: %q, %v; want the file as it was", got, err)
	}
	if err := saveConfig(); err != nil {
		t.Fatalf("saving after the backup: %v", err)
	}
}

func TestLoadConfigUnreadKeyBackupFailedBlocksSave(t *testing.T) {
	const content = `{"notify":false,"macros":"oops"}`
	p := writeConfig(t, content)
	if err := os.MkdirAll(filepath.Join(p+".bad", "blocker"), 0o755); err != nil {
		t.Fatal(err)
	}

	loadConfig()

	if err := saveConfig(); err == nil {
		t.Fatal("saved over a file with an unread key that has no backup")
	}
	if got, err := os.ReadFile(p); err != nil || string(got) != content {
		t.Fatalf("settings file: %q, %v; want it untouched", got, err)
	}
}
