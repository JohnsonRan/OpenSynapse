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
