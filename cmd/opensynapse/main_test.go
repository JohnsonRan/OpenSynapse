package main

import (
	"errors"
	"os"
	"testing"
)

// TestMain keeps every test off the real settings and the real desktop:
// anything that saves the config without its own temporary APPDATA would
// replace the user's.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "opensynapse-test-")
	if err != nil {
		panic(err)
	}
	os.Setenv("APPDATA", dir)
	// No test types into a window, starts a program or shows a notification.
	sendChar = func(rune) {}
	startTarget = func(target, args, dir string) error { return errors.New("a test started a program") }
	notifyMacro = func(Macro, string) {}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
